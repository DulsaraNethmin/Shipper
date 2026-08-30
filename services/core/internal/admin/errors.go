package admin

import (
	"errors"
	"fmt"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// The sentinel errors this domain raises, and the error codes its endpoints answer with.
//
// Docs/10 §2.1 puts the sentinels here rather than beside the code that returns them, so a caller
// deciding what to do about a failure has one file to read.
//
// The codes below are what reaches a client, and they are a separate list on purpose: a sentinel
// says what happened, a code says what the client should do about it. [ErrJobNotFound] and
// [ErrNotAParty] deliberately share `not_found`, because telling a caller which of the two it was
// would confirm that somebody else's job exists.
var (
	// ErrJobNotFound means no job with that identifier exists — or none this caller has any
	// business knowing about. The endpoint answers 404 either way.
	ErrJobNotFound = errors.New("admin: no such job")

	// ErrNotAParty means the job exists and the caller is neither its customer nor the provider
	// who won it.
	//
	// Distinct from ErrJobNotFound in Go and indistinguishable from it on the wire, which is the
	// arrangement `jobs`, `fleet` and `delivery` all use: the distinction is worth keeping so a
	// test asserting "the stranger was refused" fails if the job silently stopped existing
	// instead.
	//
	// **The refusal is explicit.** Wave 5 found six of eight fleet endpoints scoping their query
	// to the caller rather than refusing an outsider, which leaks nothing and answers 200 to
	// somebody who had no business asking. This endpoint asks who the caller is on the job and
	// refuses when the answer is nobody, which is a different thing from writing a row nobody
	// can see.
	ErrNotAParty = errors.New("admin: that job is not this caller's to dispute")

	// ErrJobNotDisputable means Docs/02 §2 has no route from the job's status to 'Disputed'.
	//
	// Narrower than "that transition is not permitted" on purpose, in the same spirit as
	// jobs.ErrJobNotCancellable: somebody pressing "report a problem" asked for an outcome
	// rather than for a named move, and the code they get back should describe the outcome they
	// wanted.
	//
	// Docs/02 §2 permits it from Awarded through Delivered, which is the span during which there
	// is a delivery to dispute. A job nobody has been awarded is cancelled rather than disputed;
	// a job that has completed or been cancelled has left the lifecycle.
	ErrJobNotDisputable = errors.New("admin: this job cannot be disputed in its current status")

	// ErrDisputeAlreadyOpen means the job already has a dispute awaiting an outcome.
	//
	// This domain's reading of uq_disputes_open_per_job, which is partial on resolved_at being
	// NULL. The index is what makes the rule true — two callers racing both find nothing open
	// and both write, and only the index is right about that — and this sentinel is what makes
	// the refusal legible, because a caller handed an index name learns nothing they can act on.
	//
	// It is a different answer from the retry below, and the difference matters to the client: a
	// retry gets the dispute it already raised, and this is told that somebody has already raised
	// one. On a job with two parties, the somebody is frequently the other one.
	ErrDisputeAlreadyOpen = errors.New("admin: this job already has an open dispute")

	// ErrNoIdempotencyKey means an intake arrived with no key to record it against.
	//
	// SHIP-15's middleware refuses a state-changing request without one, so this is unreachable
	// through the served route. It is checked anyway, because uq_disputes_idempotency is
	// *partial*: a row with a NULL key falls outside the index, and the guarantee that a retry
	// raises no second dispute would be absent rather than broken. A guarantee that can be
	// removed by omitting a header is one this domain should refuse to write without.
	ErrNoIdempotencyKey = errors.New("admin: a dispute must carry the key it was raised under")

	// ErrIdempotencyKeyReused means the key has already raised a different dispute on this job.
	//
	// The database's version of the fingerprint check httpx.Idempotent makes in Redis, and it
	// answers with the same code. Replaying the first dispute would tell a client that something
	// it never sent had been raised.
	ErrIdempotencyKeyReused = errors.New("admin: that idempotency key already raised a different dispute")

	// ErrDisputeVanished means the unique index refused a duplicate and no row exists for the key
	// that caused it.
	//
	// Nothing in the platform can produce this: intake has no DELETE path, and the row that
	// caused the conflict is committed by the time the conflict is visible. It is here so that an
	// impossible state becomes a 500 with a cause in the log rather than a reply that invents an
	// answer.
	ErrDisputeVanished = errors.New("admin: a dispute was refused as a duplicate of a row that is not there")

	// ErrMessageNotOnJob means a report named a message that is not on the job in the path
	// (SHIP-155a).
	//
	// The check `fk_reports_message` cannot make: the foreign key establishes that the message
	// exists, and whether it belongs to *this* job is a comparison between two tables. `000506`'s
	// header records `internal/bidding` in the same position.
	//
	// **Reachable only by somebody already established as party to the job**, which is what makes
	// it safe to disclose plainly — see [JobMessages]. A stranger never gets this far; they get
	// [ErrNotAParty] first, whatever message id they sent.
	//
	// Answered as a field-level problem naming `message_id` rather than as a 404, and the
	// distinction is which thing was not found: the job *was* found and the caller is party to
	// it, so what is wrong is a field in the body (Docs/10 §4.6). It takes no code of its own,
	// which is [ErrNoteSubjectMissing]'s treatment of the same shape — a subject id that names
	// the wrong thing is a validation failure, and the message says which field and why.
	ErrMessageNotOnJob = errors.New("admin: that message is not on that job")

	// ErrReportVanished means the unique index refused a duplicate report and no row exists for
	// the key that caused it.
	//
	// [ErrDisputeVanished] one table over, and impossible for the same reason: intake has no
	// DELETE path, and the row that caused the conflict is committed by the time the conflict is
	// visible. It is here so that an impossible state becomes a 500 with a cause in the log
	// rather than a reply that invents an answer.
	ErrReportVanished = errors.New("admin: a report was refused as a duplicate of a row that is not there")

	// ErrReportNotFound means there is no report with that identifier (SHIP-156).
	//
	// **Disclosed plainly, unlike intake's [ErrNotAParty]**, and the difference is who is asking.
	// A stranger raising a report about somebody else's job gets one answer for "no such job" and
	// "not your job", because telling them apart would confirm that the job exists. The caller
	// here holds `moderation.read` over every report on the platform, and there is nothing being
	// kept from them. [ErrDisputeNotFound] takes the same position for the same reason.
	ErrReportNotFound = errors.New("admin: no such report")

	// ErrReportedJobVanished means the [ReportedJobs] port answered without a job a report names
	// (SHIP-156).
	//
	// Impossible rather than unlikely: `fk_reports_job` is ON DELETE RESTRICT (`000805`), so
	// nothing in this platform can remove a job while a report points at it. It is here so that
	// the impossible state becomes a 500 with a cause in the log rather than a queue entry
	// carrying a blank status, which would read as a rendering fault and be looked for in the
	// console. [ErrReportVanished] is the same treatment of the same kind of contradiction.
	ErrReportedJobVanished = errors.New("admin: a report names a job the job lookup did not answer for")

	// ErrNotInTransaction means a service method that writes two tables was handed a connection
	// pool rather than a transaction.
	//
	// Checked here rather than left to the database, even though the status guard would refuse
	// the transition on its own (000402's setting is transaction-local): by then the dispute row
	// would have committed alone, leaving a complaint recorded against a job that never froze.
	ErrNotInTransaction = errors.New("admin: this must run inside a transaction")

	// ErrPartyUnrecognised means the [JobParties] port said the caller is party to the job and
	// then named a side that is not one of [Parties].
	//
	// A wiring failure rather than a request failure, so it becomes an opaque 500.
	// ck_disputes_complainant_party would refuse the row anyway; this refuses it a statement
	// earlier, with a cause a reader can act on rather than a constraint name.
	ErrPartyUnrecognised = errors.New("admin: the party lookup named a side this domain does not recognise")

	// ErrJobMoveUnrecognised means the [Jobs] port answered with a [JobMove] this domain has no
	// case for.
	//
	// A wiring failure rather than a request failure, so it becomes an opaque 500. It exists
	// because the alternative — treating an unrecognised answer as success — would record a
	// dispute on a job whose status nobody moved.
	ErrJobMoveUnrecognised = errors.New("admin: the job lifecycle answered with an outcome this domain does not recognise")
)

// The error codes this domain's endpoints answer with (Docs/10 §4.4).
//
// Registered rather than declared as constants, so that the generated Docs/10-api-error-codes.md
// describes them and cmd/api's uniqueness test can see them. Named <domain>_<condition>, which is
// what stops two domains meaning different things by one string.
//
// There are deliberately two. A caller who is not party to the job gets `not_found`, a malformed
// intake gets `validation_failed` with details, and a key that raised something else gets the
// protocol's own `idempotency_key_reused` — because the database enforces it here (000800) and the
// middleware enforces it in Redis, and a client that had to tell the two apart would be branching
// on where the platform happened to catch it. A domain code earns its place only where a client
// would otherwise parse a message to know what to do, and these two lead to different screens.
var (
	// CodeJobNotDisputable is returned when the job is in no status a dispute can be raised
	// from.
	//
	// 409 rather than 403: the caller is permitted and the request contradicts the state the job
	// is in. The client reloads the job and offers what is actually available — which, for a job
	// still open for bids, is cancelling it, and for a job that completed a month ago is
	// contacting support.
	CodeJobNotDisputable = httpx.RegisterCode("admin_job_not_disputable",
		"A dispute can only be raised on a job that has been awarded and has not yet been "+
			"completed or cancelled. Reload the job to see its current status.")

	// CodeDisputeAlreadyOpen is returned when the job already has a dispute awaiting an outcome.
	//
	// 409 for the same reason, and a distinct code because the client's response is different
	// again: show the dispute that is already open rather than offer the form a second time. On a
	// job with two parties this is the answer the *other* party gets, and "somebody has already
	// raised this" is what they need to be told.
	CodeDisputeAlreadyOpen = httpx.RegisterCode("admin_dispute_already_open",
		"This job already has a dispute waiting on an outcome. Open it rather than raising "+
			"another — one job is disputed once at a time.")
)

// The sentinels administrator authentication raises (SHIP-147, SHIP-148).
//
// A second block rather than additions to the one above, because they are a different subject: the
// ones above are about a dispute a customer raised, and these are about who is allowed to be here
// at all. `errors.Is` does not care and a reader does.
var (
	// ErrAdminCredentialsInvalid is every way a sign-in fails to identify an administrator.
	//
	// **One sentinel for two cases on purpose**: no account with that address, and the right
	// address with the wrong password. Two would make an unauthenticated endpoint an oracle for
	// which addresses are administrators, which is a more valuable list than which addresses have
	// accounts. See [CodeAdminCredentialsInvalid], and see credentials.go's header for the two
	// other halves of that disclosure — the equalised response time and the limit that counts
	// unknown addresses.
	ErrAdminCredentialsInvalid = errors.New("admin: those administrator credentials do not match")

	// ErrAdminAccountDisabled means the password was right and the account is out of service.
	//
	// Disclosed at sign-in, where the caller has just proved they hold the account, and **not** at
	// session resolution, where they hold only a token that may have been taken.
	// [Authenticator.Resolve] keeps the sentinel and answers with the same code an unrecognised
	// credential gets — identity.Service.Refresh takes exactly this position, for exactly this
	// reason.
	ErrAdminAccountDisabled = errors.New("admin: that administrator account is disabled")

	// ErrAdminEmailTaken means an address already has an administrator account.
	//
	// This is `uq_admin_users_email` refusing the row, which is where the rule actually lives: two
	// concurrent creations both find nothing and both insert, and only the index is right about
	// that.
	//
	// It is safe to disclose here and it would not be at sign-in. The caller is an administrator
	// holding `admins.manage`, not an anonymous request — they are entitled to know the account
	// they are trying to create already exists, and refusing to say so would make the endpoint
	// unusable.
	ErrAdminEmailTaken = errors.New("admin: an administrator already exists with that address")

	// ErrNoAdminSession means no credential was presented at all.
	//
	// Distinct from [ErrAdminSessionInvalid] because the answers differ in their challenge header:
	// nothing presented gets a bare `Bearer`, because there is no error to report yet.
	ErrNoAdminSession = errors.New("admin: no administrator session was presented")

	// ErrAdminSessionInvalid means the credential is not one this platform recognises — or is one
	// it has stopped recognising.
	//
	// Unknown, revoked, and belonging to a disabled account are all this on the wire. The Go
	// distinction is kept where it exists so that a test can tell "the signed-out session was
	// refused" from "the session silently stopped existing".
	ErrAdminSessionInvalid = errors.New("admin: that administrator session is not valid")

	// ErrAdminSessionExpired means the session lapsed — idle for too long, or past its cap.
	//
	// The one refusal worth its own code, because it is the one an administrator can act on
	// without wondering whether something is broken: sign in again. Which of the two limits it
	// hit is deliberately not said; it is information only somebody probing has a use for.
	ErrAdminSessionExpired = errors.New("admin: that administrator session has expired")

	// ErrAdminPermissionDenied means the administrator is known and the permission is not theirs
	// (SHIP-148).
	//
	// A 403 rather than a 404: the caller is a verified administrator, the endpoint's existence is
	// not a secret from them, and telling them "no such thing" would send them to look for a
	// routing fault instead of asking for the permission. That is the opposite of the reasoning on
	// [ErrNotAParty], and the difference is who is asking.
	ErrAdminPermissionDenied = errors.New("admin: that action needs a permission this administrator does not hold")

	// ErrAdminUnavailable means a dependency this request needs is not answering.
	//
	// PostgreSQL unreachable, or the rate limiter's Redis unreachable — the second **fails closed**
	// (see [Credentials.SignIn]), because a limiter an attacker turns off by taking Redis down is
	// not a limiter. 503 rather than 500 in both cases: retrying is the right advice.
	ErrAdminUnavailable = errors.New("admin: this cannot be completed right now")
)

// ThrottledError means the caller has been refused for making too many sign-in attempts.
//
// It carries the wait because the handler puts it in a `Retry-After` header, and a client told to
// come back later without being told when will either give up or poll. A struct rather than a
// sentinel for that reason alone — everything else about it is the transport's to decide.
type ThrottledError struct {
	RetryAfter time.Duration
}

func (e *ThrottledError) Error() string {
	return fmt.Sprintf("admin: too many administrator sign-in attempts; retry in %s", e.RetryAfter)
}

// The error codes administrator authentication answers with (SHIP-147, SHIP-148).
//
// Four rather than one, and each earns its place by leading somewhere different in the console:
// re-enter the password, contact whoever runs the platform, sign in again, ask for a permission.
var (
	// CodeAdminCredentialsInvalid is every failed sign-in that is not the account's own standing.
	//
	// 400 rather than 401, matching identity's: 401 invites a client to present a *better*
	// credential for the request it just made, and this request had no credential to improve —
	// the body was the credential.
	CodeAdminCredentialsInvalid = httpx.RegisterCode("admin_credentials_invalid",
		"That email address and password do not match an administrator account. Deliberately one "+
			"code for both halves, so this endpoint cannot be used to find out which addresses "+
			"are administrators.")

	// CodeAdminAccountDisabled means the password was right and the account is out of service.
	//
	// Said only at sign-in, after the password verified. A disabled administrator presenting an
	// old session token gets `unauthenticated` instead — see [ErrAdminAccountDisabled].
	CodeAdminAccountDisabled = httpx.RegisterCode("admin_account_disabled",
		"This administrator account has been disabled. Ask whoever administers the platform to "+
			"restore it; signing in again will not help.")

	// CodeAdminSessionExpired means the administrator session lapsed.
	//
	// Distinct from `unauthenticated` so the console can say "your session timed out" and put the
	// person back where they were, rather than treating it as a credential that was never any
	// good. The same distinction delivery.CodeDriverLinkExpired makes for a driver.
	CodeAdminSessionExpired = httpx.RegisterCode("admin_session_expired",
		"The administrator session has ended, through inactivity or by reaching its maximum "+
			"length. Sign in again.")

	// CodeAdminEmailTaken means an administrator already exists with that address.
	CodeAdminEmailTaken = httpx.RegisterCode("admin_email_taken",
		"An administrator account already exists with that email address.")

	// CodeAdminPermissionDenied means the administrator is authenticated and unauthorised
	// (SHIP-148).
	//
	// It names no permission, and that is deliberate: the message a client shows should send
	// somebody to ask for access rather than to enumerate what the platform can do. The
	// permission that was missing is in the log, against the request id.
	CodeAdminPermissionDenied = httpx.RegisterCode("admin_permission_denied",
		"This administrator account does not have permission to do that. Ask whoever administers "+
			"the platform if you need it.")
)

// The sentinels the administrative outcomes of Docs/04 §6 raise (SHIP-160, SHIP-161).
//
// A third block, because they are a third subject: the first is a dispute a customer raised, the
// second is who may be here at all, and these are an administrator acting on somebody else's job or
// account. `errors.Is` does not care and a reader does.
var (
	// ErrReasonRequired means a privileged action arrived with nothing recorded about why.
	//
	// Both *Done when* lines say "with a recorded reason", `ck_job_status_history_admin_reason`
	// requires one of any administrator's transition, and this is the field a later reader has no
	// way to reconstruct. Checked before a transaction opens, so the failure names the field
	// rather than a constraint.
	ErrReasonRequired = errors.New("admin: this action must record why it was taken")

	// ErrReasonTooShort means a reason was supplied and says nothing.
	//
	// **A floor rather than a formality.** A single character satisfies a required field without
	// recording anything, which is the shape a required field acquires the moment somebody is in
	// a hurry — and unlike an empty reason it looks, in the trail, exactly like a reason.
	ErrReasonTooShort = errors.New("admin: that reason is too short to be a record of anything")

	// ErrReasonTooLong means a reason is longer than the trail should carry.
	//
	// The ceiling stops `audit_log` becoming a document store. An administrator with more to say
	// writes an internal note (SHIP-162) and the reason references it.
	ErrReasonTooLong = errors.New("admin: that reason is longer than this field holds")

	// ErrJobNotUnpublishable means Docs/02 §2 has no route from the job's status to 'Cancelled'.
	//
	// The ordinary case is a job that has been **awarded**. A provider has committed and may have
	// travelled, and Docs/02 §6.2 makes ending it after that a support case rather than a status
	// change — so the administrator's path is a dispute they then resolve (SHIP-164), which
	// records both sides. The other case is a job that has already completed or been cancelled
	// and has left the lifecycle.
	//
	// Narrower than "that transition is not permitted", in the same spirit as
	// [ErrJobNotDisputable]: somebody pressing "unpublish" asked for an outcome rather than for a
	// named move.
	ErrJobNotUnpublishable = errors.New("admin: this job cannot be unpublished in its current status")

	// ErrJobAlreadyUnpublished means the job was already off the marketplace.
	//
	// Distinct from the refusal above because the console's response differs: reload and show
	// who removed it, rather than offer a different action. It is the ordinary outcome of two
	// moderators reading the same queue.
	ErrJobAlreadyUnpublished = errors.New("admin: this job has already been unpublished")

	// ErrUserNotFound means no account with that identifier exists.
	//
	// **Disclosed plainly, unlike [ErrJobNotFound].** The caller is an authenticated
	// administrator holding `users.restrict`, every account is theirs to act on, and there is
	// nothing here being kept from them — the 404 on dispute intake is a customer being told
	// nothing about somebody else's job, which is a different question with a different asker.
	ErrUserNotFound = errors.New("admin: no such account")

	// ErrExceptionGroundUnrecognised means a `ground` filter outside [ExceptionGrounds] (SHIP-157).
	//
	// Refused rather than ignored, on the reasoning the standing filter records: an ignored
	// filter answers with every entry, which reads exactly like "every exception is on this
	// ground" to somebody who mistyped one — and on a moderation queue that mistake is the
	// difference between "nothing is overdue" and "I asked the wrong question".
	ErrExceptionGroundUnrecognised = errors.New("admin: that is not a delivery-exception ground")

	// ErrStandingUnrecognised means a standing outside `ck_users_status`s three.
	ErrStandingUnrecognised = errors.New("admin: that is not an account standing")

	// ErrNoteSubjectUnrecognised means a note named a kind of thing outside
	// `ck_admin_notes_subject_type`s two.
	ErrNoteSubjectUnrecognised = errors.New("admin: a note can only be about a user or a job")

	// ErrNoteSubjectMissing means a note named no subject at all.
	ErrNoteSubjectMissing = errors.New("admin: a note must say what it is about")

	// ErrNoteEmpty means a note records nothing.
	//
	// Measured after trimming, so four thousand spaces is not a note. `ck_admin_notes_body`
	// refuses it too; this refuses it a statement earlier, with a message a person can act on
	// rather than a constraint name.
	ErrNoteEmpty = errors.New("admin: a note with nothing in it records nothing")

	// ErrNoteTooLong means a note is longer than the column holds.
	ErrNoteTooLong = errors.New("admin: that note is longer than this field holds")

	// ErrSuspensionNeedsReview means somebody tried to suspend an account through the standing
	// endpoint, which one administrator may no longer do alone (SHIP-166).
	//
	// A typed error rather than a validation failure on the field: `suspended` is a standing the
	// platform has and the caller has not mistyped anything. What they need is the other route.
	ErrSuspensionNeedsReview = errors.New(
		"admin: a permanent suspension needs a second administrator's approval")

	// ErrSameAdministrator means one administrator tried to complete a two-person review alone
	// (SHIP-166).
	//
	// **This is the whole of Docs/04 §9's control**, and it is refused in two places: here, one
	// statement before the write, and by `ck_suspension_reviews_two_people` in the database. The
	// constraint is what makes it a control — application logic refusing to write is a convention,
	// and a convention does not apply to a repair script or a psql prompt.
	ErrSameAdministrator = errors.New(
		"admin: the administrator who requested a suspension cannot be the one who approves it")

	// ErrSuspensionReviewNotFound means no review with that identifier exists.
	//
	// Disclosed plainly, like [ErrUserNotFound] and for the same reason: the caller is an
	// authenticated administrator holding `users.restrict`, and there is nothing here being kept
	// from them.
	ErrSuspensionReviewNotFound = errors.New("admin: no such suspension review")

	// ErrSuspensionReviewSettled means the review has already been approved or withdrawn.
	//
	// The ordinary outcome of two moderators reading the same queue, and the console's right
	// response is to reload — most often because the other one got there first, which is the
	// control working rather than failing.
	ErrSuspensionReviewSettled = errors.New("admin: that suspension review has already been settled")

	// ErrSuspensionReviewOutstanding means the account already has a review waiting.
	//
	// Refused by `uq_suspension_reviews_one_pending` rather than by a check-then-insert, which
	// two moderators on one account lose in practice. Two pending reviews would also let one
	// administrator approve the other's request while their own waited — the letter of a
	// two-person rule with one person driving both halves.
	ErrSuspensionReviewOutstanding = errors.New(
		"admin: this account already has a suspension review waiting for a second administrator")

	// ErrStandingUnchanged means the account already holds the standing it was being moved to.
	//
	// Refused rather than recorded. An entry saying "changed from suspended to suspended" is
	// noise in the one table whose value is that everything in it happened, and the console's
	// right response is to reload — most often because another administrator got there first.
	ErrStandingUnchanged = errors.New("admin: that account already holds that standing")
)

// The error codes the administrative outcomes answer with (SHIP-160, SHIP-161).
//
// Three, and each earns its place by leading somewhere different in the console: raise a dispute
// instead, reload and see who got there first, reload and see the current standing. Everything else
// these actions can refuse is already served by a code that exists — a missing reason is
// `validation_failed` with the field named, and an account that does not exist is `not_found`.
var (
	// CodeJobNotUnpublishable is returned when Docs/02 §2 permits no move to 'Cancelled'.
	//
	// 409 rather than 403: the administrator is permitted and the request contradicts the state
	// the job is in. The console reloads and offers what is actually available, which for an
	// awarded job is raising a dispute.
	CodeJobNotUnpublishable = httpx.RegisterCode("admin_job_not_unpublishable",
		"A job can only be unpublished before it is awarded. Once a provider has committed, "+
			"ending it is a dispute an administrator resolves — reload the job to see its "+
			"current status.")

	// CodeJobAlreadyUnpublished is returned when the job is already off the marketplace.
	//
	// A distinct code because the client's response is different: show who removed it and why,
	// rather than offer the action again. On a queue two moderators are reading, this is the
	// answer the second one gets.
	CodeJobAlreadyUnpublished = httpx.RegisterCode("admin_job_already_unpublished",
		"This job has already been unpublished. Reload it to see who removed it and why.")

	// CodeUserStandingUnchanged is returned when the account already holds that standing.
	//
	// 409 for the same reason, and a distinct code because an administrator seeing it has
	// learned something specific: somebody else has already acted, and the trail will say who.
	CodeUserStandingUnchanged = httpx.RegisterCode("admin_user_standing_unchanged",
		"This account already has that standing. Reload it — another administrator may have "+
			"changed it already, and the audit trail will say who.")

	// --- Docs/04 §9's two-person review (SHIP-166) -----------------------------------------------

	// CodeSuspensionNeedsReview is returned when somebody suspends through the standing endpoint.
	//
	// 409 rather than 422: `suspended` is a standing the platform has and nothing was mistyped.
	// What changed is *who may do it alone*, and a console branching on this code sends the same
	// reason to the review endpoint rather than asking the moderator to retype it.
	CodeSuspensionNeedsReview = httpx.RegisterCode("admin_suspension_needs_review",
		"A permanent suspension needs a second administrator's approval. Request one instead, "+
			"and another administrator can approve it.")

	// CodeSameAdministrator is the whole of the control, as a client sees it (SHIP-166).
	//
	// A distinct code because the console's response is specific and is not "try again": the
	// person reading it must fetch somebody else. It is deliberately **not** a 403 — the caller
	// holds `users.restrict`, and a permission error would read as "you may not approve
	// suspensions", which is the wrong thing to learn.
	CodeSameAdministrator = httpx.RegisterCode("admin_same_administrator",
		"A suspension must be approved by a different administrator from the one who requested "+
			"it. Docs/04 §9 requires two people.")

	// CodeSuspensionReviewSettled is returned when the review has already been approved or
	// withdrawn.
	//
	// The ordinary outcome of two moderators reading one queue, and the console's right response
	// is to reload — most often because the other one got there first, which is the control
	// working rather than failing.
	CodeSuspensionReviewSettled = httpx.RegisterCode("admin_suspension_review_settled",
		"That suspension review has already been settled. Reload the queue — another "+
			"administrator may have approved it.")

	// CodeSuspensionReviewOutstanding is returned when the account already has one waiting.
	//
	// Distinct from the code above because the action differs: this one says *find the existing
	// review and approve it*, and a second request would be a second thing for somebody to
	// approve. `uq_suspension_reviews_one_pending` is what makes it a refusal rather than a race.
	CodeSuspensionReviewOutstanding = httpx.RegisterCode("admin_suspension_review_outstanding",
		"This account already has a suspension waiting for a second administrator. Approve the "+
			"existing request rather than making another.")
)

// --- SHIP-153, SHIP-154: the verification queue and its decision ---------------------------------

// The sentinels the verification console raises.
//
// Four, and none of them is `profiles`'. That domain has its own — `ErrNoSuchProvider`,
// `ErrAlreadyInState` and the rest — and this package may not import it to match them, which is why
// [ProviderVerifications] answers refusals as a [VerificationMove] and these are what the outcome is
// turned into. Docs/10 §2.1 keeps the sentinels beside each other so that a caller deciding what to
// do about a failure has one file to read.
var (
	// ErrVerificationNotFound means there is no verification record for that identifier.
	//
	// One sentinel for two conditions — no such account, and an account that is not a provider —
	// because `000200` gives every provider a record at registration and the two are
	// indistinguishable from the record's side. **Not a disclosure decision** (see
	// [VerificationProviderNotFound]): the caller holds a permission over verifications and
	// nothing is being kept from them.
	ErrVerificationNotFound = errors.New("admin: no such provider verification record")

	// ErrVerificationUnchanged means the provider already holds that outcome.
	//
	// Refused rather than recorded, which is [ErrStandingUnchanged]'s position applied to a
	// second vocabulary: an entry saying "changed from Verified to Verified" is noise in the one
	// table whose value is that everything in it happened, and on a queue two moderators are
	// reading the console's right response is to reload.
	ErrVerificationUnchanged = errors.New("admin: that provider already holds that verification outcome")

	// ErrVerificationStateUnrecognised means the outcome asked for is not one of Docs/04 §4's.
	//
	// Raised by the queue as well as by the decision, and refused in both rather than ignored: an
	// ignored filter answers an empty page, and an empty review queue is what "nobody is waiting"
	// looks like to somebody who mistyped a state.
	ErrVerificationStateUnrecognised = errors.New("admin: that is not a verification outcome")

	// ErrVerificationMoveUnrecognised means the port answered with something that is not an
	// outcome.
	//
	// [VerificationMoveUnrecognised]'s counterpart, and it exists for [ErrJobMoveUnrecognised]'s
	// reason: a half-written adapter returning the zero value must not be read as success. It is
	// a wiring fault rather than anything a caller did, so it stays an opaque 500 with the cause
	// logged.
	ErrVerificationMoveUnrecognised = errors.New("admin: the verification port answered with no outcome")
)

// The error codes the verification console answers with (SHIP-153, SHIP-154).
//
// **One**, and the restraint is the same judgement errors.go applies throughout: everything else
// these two endpoints can refuse is already served by a code that exists — a state Docs/04 §4 does
// not have is `validation_failed` naming the field, a reason too short to record anything is the
// same, a provider who does not exist is `not_found`, and a permission the role lacks is
// `admin_permission_denied`. A domain code earns its place only where a client would otherwise have
// to parse a message to know what to do next.
var (
	// CodeVerificationUnchanged is returned when the provider already holds that outcome.
	//
	// 409, and a distinct code because an administrator seeing it has learned something specific:
	// somebody else has already decided this one, and the trail will say who. It is the ordinary
	// outcome of two moderators working the same queue, which is why it is worth telling apart
	// from a validation failure at all.
	CodeVerificationUnchanged = httpx.RegisterCode("admin_verification_unchanged",
		"This provider already has that verification outcome. Reload the queue — another "+
			"administrator may have decided it already, and the audit trail will say who.")
)

// --- Docs/04 §7's investigation and outcome stages (SHIP-164) -------------------------------------
//
// The refusals a dispute workflow makes, and they divide the same way SHIP-163's do: what is wrong
// with the *request* is a validation failure naming a field, and what is wrong with the *state* is a
// conflict with a code. A console reaching the second has not made a mistake — it has read a queue
// that has since moved.
var (
	// ErrDisputeNotFound means there is no dispute with that identifier.
	//
	// **Disclosed plainly, unlike intake's [ErrNotAParty]**, and the difference is who is asking.
	// Intake refuses a stranger with the same answer a missing job gets, because telling them
	// apart would confirm that somebody else's job exists. The caller here holds
	// `disputes.read` on the administrator credential and there is nothing being kept from them;
	// a 404 that could not be told from a 403 would only make a mistyped identifier look like a
	// permissions problem.
	ErrDisputeNotFound = errors.New("admin: no such dispute")

	// ErrDisputeAlreadyResolved means somebody has already recorded an outcome against it.
	//
	// The ordinary outcome of two moderators reading one queue, and refused rather than absorbed
	// on [ErrVerificationUnchanged]'s reasoning: a resolution is *an act by a person* that
	// Docs/04 §6 step 6 requires be recorded with a reason, and a second one would overwrite the
	// first administrator's finding with nothing to say which stands.
	//
	// It is raised from two places one statement apart — the row lock's read, which produces a
	// message naming the outcome already recorded, and the `UPDATE`'s own
	// `WHERE resolved_at IS NULL`, which is what is true of the table however it is written to.
	ErrDisputeAlreadyResolved = errors.New("admin: that dispute has already been resolved")

	// ErrDisputeOutcomeUnrecognised means the finding asked for is not one of Docs/04 §7's five.
	ErrDisputeOutcomeUnrecognised = errors.New("admin: that is not a dispute outcome")

	// ErrJobOutcomeUnrecognised means the destination asked for is not one of Docs/02 §2's two.
	//
	// A separate sentinel from the one above rather than one covering both, because they are two
	// vocabularies — `000804`s whole argument — and a client that sent a good outcome with a bad
	// destination must be told which of the two fields is wrong.
	ErrJobOutcomeUnrecognised = errors.New("admin: that is not a destination for a resolved dispute")

	// ErrDisputeStateUnrecognised means the queue was asked for a half that does not exist.
	//
	// Refused rather than ignored, on [ErrVerificationStateUnrecognised]'s reasoning: an ignored
	// filter answers a page that looks like an answer to the question somebody meant to ask.
	ErrDisputeStateUnrecognised = errors.New("admin: that is not a dispute queue")

	// ErrJobNotResolvable means Docs/02 §2 has no row from the job's status to the destination
	// the resolution named.
	//
	// [JobNotResolvable] and [JobAlreadyResolved] both land here — the second with the status it
	// already holds appended — because a console's response to either is the same: reload, the
	// job has moved out from under this dispute. Telling them apart matters to the trail rather
	// than to the caller, and the trail records neither, because a refused resolution writes
	// nothing at all.
	ErrJobNotResolvable = errors.New("admin: this job cannot be resolved to that status")
)

// The error codes the dispute workflow answers with (SHIP-164).
//
// **Two**, on the restraint the verification block above records. A dispute that does not exist is
// `not_found`, an outcome Docs/04 §7 does not have is `validation_failed` naming the field, a reason
// too short to record anything is the same, and a role without `disputes.resolve` is
// `admin_permission_denied`. These two are the states a console has to *do* something different
// about, and in both cases the something is "reload; the world has moved".
var (
	// CodeDisputeAlreadyResolved is returned when another administrator got there first.
	//
	// 409 rather than 422: the caller holds the permission and the request is well formed, and
	// what is refused is the state. [CodeSameAdministrator] records the same reading of the same
	// distinction.
	CodeDisputeAlreadyResolved = httpx.RegisterCode("admin_dispute_already_resolved",
		"This dispute has already been resolved. Reload it — another administrator may have "+
			"recorded an outcome already, and the audit trail will say who.")

	// CodeJobNotResolvable is returned when the job is no longer where Docs/02 §2 needs it to be.
	//
	// Distinct from [CodeDisputeAlreadyResolved] because the two are opposite halves of the same
	// drift and want opposite next steps: there, the dispute has moved and the job has not; here,
	// the job has moved and the dispute has not — which is the state SHIP-163's `service.go`
	// warned about, and the one an administrator most needs told plainly rather than as a 500.
	CodeJobNotResolvable = httpx.RegisterCode("admin_job_not_resolvable",
		"This job is no longer waiting on a dispute. Reload it — its status has moved since "+
			"this dispute was opened.")
)
