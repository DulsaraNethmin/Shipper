package bidding

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
)

// EventSink is where this domain's events go (SHIP-136).
//
// Declared here rather than taken as *events.Outbox for the reason jobs.EventSink is: Docs/06 §4.1
// makes the consuming domain the one that names the interface. The concrete writer is
// infrastructure and this domain may import it either way — what the interface buys is that a test
// can watch what was emitted without a table, and that the publisher can change the writer without
// touching a domain.
//
// Emit takes the same db.Runner the state change is using, and that is the entire point of the
// outbox: an event written in a different transaction from the change it describes can commit when
// the change does not (Docs/06 §4.0, Docs/10 §6.1). It matters more here than anywhere else in the
// platform, because the transaction it has to join is the award — five statements across two
// domains, and the one thing two parties are then committed to.
type EventSink interface {
	Emit(ctx context.Context, r db.Runner, e events.Event) error
}

// What this domain needs of other domains, declared by the consumer (Docs/06 §4.1, Docs/10 §2.3).
//
// internal/bidding imports no domain and the boundary lint refuses one. cmd/api holds the
// implementations and is the only place the packages meet.
//
// # There are three ports, and each one arrived with the endpoint that could not be written without
// it
//
// Everything before SHIP-87 was provider-only, so "who may do this" was one question with one
// answer — [Eligibility]. Docs/02 §4 lets *either* party counter, so the domain now has to recognise
// a customer as well, and "is this account the customer who owns that job, and can that job still be
// awarded" are `jobs`' facts rather than this domain's. [Negotiation] is how it asks.
//
// SHIP-92 adds [Awarding], and it is the first port here that *writes*. The award moves the job to
// 'Awarded', which is a transition and therefore `jobs`' one guarded function (Docs/02 §2,
// CLAUDE.md) — and Docs/10 §3.2 is explicit that "the award is one transaction and `bidding` owns
// it, even though it also moves the job". So the transaction is opened here, and the move happens
// inside it through a port rather than through an import.
//
// # There is one port for the provider side, and the reason it exists is that SHIP-81 already
// answered the question
//
// SHIP-84's *Done when* opens with "a verified, eligible provider", and eligibility is
// `internal/fleet`'s: Docs/01 §4.3's four filters — service area, vehicle capability, verification
// state, job status — are one SQL predicate in eligibility.go, and that file's own header says the
// second reader of it is "what SHIP-84 needs before it accepts a bid". So this domain does not decide
// who may bid. It asks.
//
// **Reimplementing the filter here would have been the defect, not the import.** Two definitions of
// who may bid is one more than Docs/07 §3 permits — the platform decides server-side in exactly one
// place — and the two would part company the first time one of them was corrected. The endpoint that
// shows a provider a job and the endpoint that accepts their bid on it would then disagree, which is
// the worst available outcome: a provider prices a job the feed offered them and is refused after
// doing the work.
//
// # A bool, and why the seam is this narrow
//
// [Eligibility] hands back a boolean and nothing else. A caller deciding whether to *permit*
// something wants a boolean, not a row — the argument fleet's own eligibility.go makes when it keeps
// `EligibleFor` distinct from the read SHIP-83 serves. A port that returned the job would put a
// provider-facing copy of a job inside this package, which is a second shape for Docs/01 §4.3's
// budget rule to be broken through, and this domain has no use for one: nothing it writes reads a
// field of the job.
//
// It also means the port needs no adapter at all. A port whose answer is a bool has no vocabulary to
// translate, so `cmd/api` satisfies it structurally and writes no translation type — unlike
// delivery.Jobs, where four transition outcomes had to be carried across without naming a `jobs`
// sentinel. The compile-time assertion in cmd/api/routes_bidding.go is the whole of the wiring.
type Eligibility interface {
	// EligibleFor reports whether this provider may bid on this job, right now.
	//
	// It takes the caller's [db.Runner] so the answer is read inside the transaction that writes the
	// bid (Docs/10 §3.2). A bid authorised by a check made in a different transaction is a bid
	// nobody checked: the job can be cancelled, expire, or be awarded in between.
	//
	// A job that does not exist is `false` rather than an error, and so is a job this provider may
	// not bid on. The two are deliberately one answer — distinguishing them would take a second
	// query whose only product is the knowledge that some identifier exists, which is the disclosure
	// this domain's 404 exists to prevent.
	//
	// A non-nil error is a failure of the mechanism rather than a refusal.
	EligibleFor(ctx context.Context, r db.Runner, providerID, jobID uuid.UUID) (bool, error)
}

// Negotiation is what `jobs` knows about the customer's side of a bid (SHIP-87, SHIP-88).
//
// # Two bools, and neither is derivable from the other
//
// They answer different questions at different moments, and collapsing them would make one of the
// two endpoints wrong.
//
//   - **A counter needs both.** The caller has to be the customer, *and* the job has to be one a
//     counter-offer could still lead anywhere.
//   - **Reading the chain needs only the first, and needs it to keep working afterwards.** SHIP-88's
//     *Done when* is "full chain remains readable", and a negotiation is at its most worth reading
//     once the job is over — the customer who awarded elsewhere, the provider reconstructing what
//     they offered, the administrator handling a dispute (Docs/02 §4). A read gated on the job still
//     being awardable would go dark at exactly the moment the record matters.
//
// # Why they are bools, and why that is what keeps this seam cheap
//
// The same reading [Eligibility] takes: a caller deciding whether to *permit* something wants a
// boolean, not a row. A port returning the job would put a customer-facing copy of a job inside this
// package — a second shape for Docs/01 §4.3's budget rule to be broken through — and this domain has
// no use for one, because nothing it writes reads a field of the job.
//
// It also means neither method needs an adapter *type* to translate a vocabulary, only a small one
// in cmd/api to turn `jobs`' sentinels into the answers below. That is the arrangement SHIP-84
// found: "a port is only as expensive as the vocabulary it has to carry."
type Negotiation interface {
	// CustomerOf reports whether this account is the customer who owns this job.
	//
	// **Whatever the job's status**, deliberately: this is what makes a chain readable to its parties
	// after the job has been awarded, cancelled or completed.
	//
	// A job that does not exist is `false` rather than an error, exactly as
	// [Eligibility.EligibleFor] treats one, and for the same reason — distinguishing the two would
	// take a second query whose only product is the knowledge that some identifier exists.
	CustomerOf(ctx context.Context, r db.Runner, userID, jobID uuid.UUID) (bool, error)

	// AwardableBy reports whether this customer's job could still be awarded.
	//
	// **That is the question a counter-offer turns on, and it is deliberately not "is the job
	// biddable".** The two happen to select the same two statuses today, and they are different
	// questions: a provider asks whether they may *offer*, which Docs/01 §4.3's four filters answer;
	// a customer countering is asking whether the negotiation can still end in an award. Answering
	// the second by copying `fleet`'s list of biddable statuses would put a third copy of that list
	// in the service — after `jobs`' own transition table and `fleet.biddableStatuses` — and the
	// third copy is the one nobody would remember to correct.
	//
	// The implementation in cmd/api reads `jobs`' authoritative transition table instead, so this
	// answer moves if and only if Docs/02 §2 does.
	//
	// It takes the customer as well as the job because a job nobody may see is not a job whose
	// status this domain has any business learning. `false` covers "not yours", "no such job" and
	// "past negotiating" together, which is the one answer a refusal is allowed to disclose.
	//
	// **It takes no lock, and it must not start taking one.** Docs/11 §3's SHIP-88 entry records
	// that everything in this domain apart from the award locks only its own `bids` rows and never
	// a `jobs` row, and that this is what makes the ordering acyclic — `jobs` → `bids` in one
	// direction and nothing going back. A counter takes its bid lock first (see
	// [postgresStore.lockBid]), so a `jobs` lock added here would close the cycle and the first
	// deadlock would be between a counter and an award.
	AwardableBy(ctx context.Context, r db.Runner, customerID, jobID uuid.UUID) (bool, error)
}

// JobAward is what the job lifecycle said about an award, in terms this domain can act on.
//
// # It carries an outcome rather than a bool, which is the delivery.Jobs case rather than the
// Eligibility case
//
// The two ports above hand back booleans, and this file argues at length that a caller deciding
// whether to *permit* something wants a boolean rather than a row. That argument still holds and
// this port is not an exception to it — it is a different question. [Eligibility] and [Negotiation]
// ask "may this happen"; [Awarding] both asks and *acts*, and the answers to "may this job be
// awarded" and "did the transition run" have more than two outcomes worth telling apart.
//
// A refusal that is "not yours or no such job" leads a client to a 404, and one that is "the job has
// moved on" leads to a 409 naming what became of it. A boolean would collapse the two, and this
// domain would then have to choose one answer for both — which is the disclosure choice it makes
// deliberately elsewhere and would here be making by accident.
//
// `error` could not carry it either: matching `errors.Is` against `jobs`' sentinels is an import by
// another name. So this is delivery.JobMove's shape, and for delivery.JobMove's reason.
type JobAward int

const (
	// JobAwardUnrecognised is the zero value and is never a valid answer.
	//
	// First on purpose, exactly as delivery.JobMoveUnrecognised is. An implementation that returns
	// nothing useful — a stub, a half-written adapter, a switch with a missing case — returns this,
	// and [Service.AwardBid] refuses it rather than reading silence as permission to award. The
	// alternative ordering would make a forgotten return look like a job that could be awarded.
	JobAwardUnrecognised JobAward = iota

	// JobAwardable means the job is this customer's, is held under the lock, and Docs/02 §2 permits
	// it to move to 'Awarded'. Only [Awarding.LockForAward] answers it.
	JobAwardable

	// JobAwarded means the job is now 'Awarded' and the transition was recorded. Only
	// [Awarding.MoveToAwarded] answers it.
	//
	// Kept apart from [JobAwardable] rather than sharing one "yes", because the two are different
	// facts about different moments — "this may happen" and "this happened". One value for both
	// would let an implementation that never ran the transition report success.
	JobAwarded

	// JobAwardNoSuchJob means there is no such job, or it belongs to another customer.
	//
	// **Deliberately one value for both**, which is the collapse [Negotiation.CustomerOf] and
	// [Eligibility.EligibleFor] already make: telling the two apart would take a second query whose
	// only product is the knowledge that somebody else's job exists.
	JobAwardNoSuchJob

	// JobAwardNotPermitted means Docs/02 §2 has no move to 'Awarded' from where the job stands.
	//
	// The ordinary case is a job this customer has already awarded — which is CLAUDE.md's "exactly
	// one accepted bid per job" met from the job's side rather than the index's. It also covers a
	// job that was cancelled, expired, or has not been published.
	JobAwardNotPermitted
)

func (a JobAward) String() string {
	switch a {
	case JobAwardable:
		return "awardable"
	case JobAwarded:
		return "awarded"
	case JobAwardNoSuchJob:
		return "no such job"
	case JobAwardNotPermitted:
		return "not permitted"
	default:
		return "unrecognised"
	}
}

// JobPresentation is what Docs/02 §2 said about a presentation move, in terms this domain can act
// on (SHIP-90).
//
// An outcome rather than a bool, for [JobAward]'s reason and one of its own. The award's port both
// asks and acts, and so does this one — but here the interesting distinction is not "may this
// happen": it is **which of three things happened**, and only one of the three is a refusal a
// caller should act on. A bool would have collapsed "the job is already Negotiating", which is the
// ordinary case on every bid after the first, into the same answer as "this job cannot take a bid
// at all", which has to become a 404.
type JobPresentation int

const (
	// JobPresentationUnrecognised is the zero value and is never a valid answer.
	//
	// First on purpose, exactly as [JobAwardUnrecognised] is: a stub, a half-written adapter or a
	// switch with a missing case returns this, and the caller refuses it rather than reading
	// silence as "the job moved".
	JobPresentationUnrecognised JobPresentation = iota

	// JobPresentationMoved means the transition ran and is recorded.
	JobPresentationMoved

	// JobPresentationAlreadyThere means the job is already in the status asked for.
	//
	// **The ordinary answer, not an edge case**, and the reason this is an enumeration. Every bid
	// after the first on one job meets it, and so does every withdrawal on a job that was never
	// moved. Nothing is wrong and nothing is written.
	JobPresentationAlreadyThere

	// JobPresentationClosed means Docs/02 §2 has no such move from where the job stands, or there
	// is no such job.
	//
	// **One value for both**, which is the collapse every port in this file makes: telling them
	// apart would take a second query whose only product is the knowledge that somebody else's job
	// exists.
	JobPresentationClosed

	// JobPresentationHeld means another transaction holds the job row, so no presentation change
	// was attempted. Only [Presentation.LeaveNegotiation] answers it.
	//
	// See that method for why it does not wait.
	JobPresentationHeld
)

func (p JobPresentation) String() string {
	switch p {
	case JobPresentationMoved:
		return "moved"
	case JobPresentationAlreadyThere:
		return "already there"
	case JobPresentationClosed:
		return "closed"
	case JobPresentationHeld:
		return "held elsewhere"
	default:
		return "unrecognised"
	}
}

// Presentation is Docs/02 §1's Negotiating status, which is this domain's to move (SHIP-90).
//
// Docs/02 §2 has two rows nothing could reach until now: `Open → Negotiating` on "first bid or
// counter-offer submitted", and `Negotiating → Open` on "all active bids expire, are withdrawn, or
// are rejected". Both conditions are facts about `bids`, so this domain is the only one that can
// know when they hold — and neither is a status this domain may write, because a job's status
// passes one guarded function in `jobs` (Docs/02 §2, CLAUDE.md). So it asks, in the transaction
// that made the condition true.
//
// **Deliberately not "move this job to any status I name"**, which is [Awarding]'s rule and the
// same rule: a port shaped that way would be Docs/02 §2's table acquiring a second opinion through
// the back door. The two methods name the two rows.
//
// # Only a placement enters, and that is not an omission
//
// Docs/02 §2 says "first bid **or counter-offer** submitted", and [Service.CounterOffer] does not
// call [Presentation.EnterNegotiation]. It cannot need to: a counter answers a *live* offer, a
// live offer is one [Service.PlaceBid] wrote, and that placement is what moved the job. **A
// counter can never be the first thing to happen in a negotiation**, which is what "first" is
// doing in that sentence.
//
// # The two methods take the job row differently, and that is the whole of the lock ordering
//
// Docs/11 §3's SHIP-88 entry records the ordering this domain keeps: `jobs` → `bids`, in one
// direction, with nothing coming back. [Presentation.EnterNegotiation] is called before any bid
// row is touched, so it takes an ordinary blocking lock and the ordering holds.
// [Presentation.LeaveNegotiation] cannot: it is called once the offer has closed, which is after
// the bid is locked — and in the expiry sweep the bid was locked by the *claim*, before this
// domain was reached at all. A blocking lock there would close the cycle, and the first deadlock
// would be between a sweep and an award.
type Presentation interface {
	// EnterNegotiation runs Docs/02 §2's `Open → Negotiating` through the one guarded function,
	// inside the caller's transaction, as the platform.
	//
	// **The platform, and not the provider who bid.** Docs/02 §1 calls Negotiating "a useful
	// presentation status" in as many words, and this move is the platform's reading of a
	// condition rather than an act anybody performed: a provider placing an offer asked for their
	// offer to exist, not for the job to move, and `job_status_history` records who acted.
	//
	// It takes an ordinary blocking `FOR UPDATE`, because it runs before this domain has touched
	// a `bids` row. **That is a strengthening rather than a cost**: the eligibility answer a
	// placement is authorised by is read without a lock, so a job awarded in the window between
	// the two used to acquire a fresh offer. Holding the row and reading Docs/02 §2 under it
	// closes that window — [JobPresentationClosed] is a job no bid may be placed on, and
	// [Service.PlaceBid] refuses it with the 404 an ineligible provider gets.
	EnterNegotiation(ctx context.Context, r db.Runner, jobID uuid.UUID) (JobPresentation, error)

	// LeaveNegotiation runs Docs/02 §2's `Negotiating → Open` through the one guarded function,
	// inside the caller's transaction, as the platform.
	//
	// Called only once the caller has established that no live offer remains on the job.
	//
	// # It does not wait for the job row, and answers [JobPresentationHeld] rather than blocking
	//
	// The implementation takes the row `FOR UPDATE SKIP LOCKED` before running the transition, so
	// a job somebody else is holding is skipped rather than queued behind. Two reasons, and the
	// first is structural:
	//
	//   - **The caller already holds `bids` rows.** A blocking lock here would be `bids` → `jobs`,
	//     against the `jobs` → `bids` an award takes, and the cycle would deadlock a sweep against
	//     an award. A lock attempt that cannot wait cannot deadlock.
	//   - **Whatever holds the row is making a real change, and it supersedes a presentational
	//     one.** The transactions that hold a `jobs` row are an award, a cancellation, an expiry,
	//     a publication and an extension. In the first three the job is leaving Negotiating for
	//     somewhere this move has no opinion about; in the others the next bid or withdrawal moves
	//     it correctly. Docs/02 §1's own sentence is the licence: this is presentation, and a
	//     presentation change must never block a real one.
	//
	// The cost is stated rather than hidden: under contention a job can sit at Negotiating with no
	// live offer until the next thing happens to it. It remains awardable-by-nobody and biddable
	// by everybody, which is what Negotiating means anyway.
	LeaveNegotiation(ctx context.Context, r db.Runner, jobID uuid.UUID) (JobPresentation, error)
}

// Awarding is the job lifecycle, as far as the award reaches into it (SHIP-92).
//
// Deliberately not "move this job to any status I name". A port shaped that way would be Docs/02
// §2's transition table acquiring a second opinion through the back door — this domain would choose
// the target status, and the one thing CLAUDE.md says about job status is that nothing outside the
// guard chooses it. The methods name the one move the award makes, and a domain that needs another
// declares another. That is delivery/ports.go's argument, and it is the same argument.
//
// # Two methods, because the award has to hold the job while it works on the bid
//
// The lock ordering is recorded in Docs/11 §3's SHIP-88 entry and this port is shaped by it: the
// `jobs` row is taken `FOR UPDATE` **first**, the bid second, the accept third, the rejection sweep
// fourth (SHIP-93) and the transition last. One method could not express that — a single
// `AwardJob(…)` would have to take the lock and run the transition in one call, leaving nowhere for
// the two statements about `bids` that have to happen in between.
//
// So [Awarding.LockForAward] is the outermost lock and the answer read under it, and
// [Awarding.MoveToAwarded] is the transition at the end. Both run in the caller's transaction, which
// is what makes them one act: a job at 'Awarded' with no accepted bid, or an accepted bid on a job
// that never moved, are both states nothing downstream knows how to read.
//
// # Why the lock is a port method rather than a statement in this package's postgres.go
//
// `jobs` is another domain's table. This package may not import it, and reaching into its rows from
// `bidding/postgres.go` would be the import's effect without the import's visibility — a second
// place that knows what `jobs.status` means, in a file whose header says it is the `bids` table in
// SQL. The composition root is where a dependency between two domains is allowed to be visible, and
// cmd/api/routes_delivery.go's `acceptedBids` is the same arrangement pointing the other way.
type Awarding interface {
	// LockForAward takes the job row for the rest of the caller's transaction and reports what may
	// be done with it.
	//
	// **The lock is the point, and the answer is a consequence of having taken it.** A read that
	// reported "this job can be awarded" and then let go would be answering about a job that can be
	// cancelled, disputed or awarded to somebody else before the caller writes anything.
	//
	// It locks even when the answer is a refusal, because the answer cannot be known without the
	// read, and a lock held for the remainder of a transaction that is about to roll back costs
	// nothing.
	//
	// A non-nil error is a failure of the mechanism rather than a refusal. A refusal comes back as a
	// [JobAward] with a nil error, because "Docs/02 does not permit this" is an answer.
	LockForAward(ctx context.Context, r db.Runner, customerID, jobID uuid.UUID) (JobAward, error)

	// MoveToAwarded runs Docs/02 §2's `Open / Negotiating → Awarded` through the one guarded
	// function, inside the caller's transaction, with the customer as the recorded actor.
	//
	// The customer, because Docs/02 §3's first control is "only the customer can award a job" and
	// `job_status_history` records who acted rather than who benefited. There is no reason: Docs/01
	// §3 requires one only of an administrator.
	//
	// A refusal here is very nearly unreachable — [Awarding.LockForAward] has held the row since
	// before the bid was touched — and it is still reported as an outcome rather than assumed away,
	// because the alternative is [Service.AwardBid] treating "the job did not move" as success.
	MoveToAwarded(ctx context.Context, r db.Runner, jobID, customerID uuid.UUID) (JobAward, error)
}

// Vehicles is the one fact this domain needs about a vehicle before it will store an offer made
// with one (SHIP-102a).
//
// # This is the port migration 000501 named and declined to write
//
// That file deferred `bids.vehicle_id` under the ticket `???` and gave the reason in as many words:
// "Validating it needs a second `fleet` fact — that the vehicle is the caller's and in service — and
// therefore a second port, which is more design than a three-point ticket should be taking on
// somebody else's behalf." This is that port, taken by the ticket the column exists for.
//
// # A bool, for [Eligibility]'s reason and a privacy one of its own
//
// A caller deciding whether to *permit* something wants a boolean rather than a row. Returning a
// `fleet.Vehicle` would put a copy of another domain's shape inside this package, and this domain
// has no use for one: nothing it writes reads a field of the vehicle, and the make and model a
// customer compares are read at the edge through [Directory] rather than stored here.
//
// It also collapses the two refusals a placement is allowed to make, exactly as
// [Eligibility.EligibleFor] collapses "no such job" and "not for you": a vehicle that does not
// exist, one belonging to another provider, and one out of service are one answer. Telling them
// apart would let a provider enumerate a competitor's fleet one identifier at a time, which is the
// disclosure `contracts/paths/fleet.yaml` already refuses on `GET /v1/fleet/vehicles/{id}`.
type Vehicles interface {
	// Usable reports whether this provider may offer this vehicle, right now.
	//
	// It takes the caller's [db.Runner] so the answer is read inside the transaction that writes the
	// offer, for [Eligibility.EligibleFor]'s reason: a vehicle can be deactivated between two
	// statements, and an offer authorised by a check made in a different transaction is an offer
	// nobody checked.
	//
	// **In service as well as owned.** Docs/01 §4.2 makes deactivation the act that stops a vehicle
	// carrying new work, and an offer is new work. A vehicle deactivated *after* the offer was made
	// keeps standing on it — 000504's `ON DELETE RESTRICT` is the same sentence from the schema's
	// side — which is why this is asked when the column is written and never when it is read.
	//
	// A non-nil error is a failure of the mechanism rather than a refusal.
	Usable(ctx context.Context, r db.Runner, providerID, vehicleID uuid.UUID) (bool, error)
}

// ProviderSummary is what a customer comparing offers may know about the provider who made one
// (SHIP-102a).
//
// # A closed set declared here, which is what makes Docs/01 §4.3 structural rather than remembered
//
// The invariant SHIP-102a's *Done when* states from the other end — "no provider's service area,
// specialties or other jobs" — is held by this struct having no field for any of them. The adapter
// in `cmd/api` reads whatever it likes and can only hand back this, so a field cannot travel by
// being forgotten about; adding one is an edit here, in a type whose doc comment says why each
// field is allowed.
//
// **It is deliberately thin, and the thinness is a finding rather than an omission.** `internal/
// profiles` — which Docs/06 §3 gives "profile detail" and the five verification states of Docs/04
// §4 — is `doc.go` and nothing else at wave 10, and the only provider profile that exists is
// `fleet.Profile`, whose two fields are precisely the two this response may not carry. So there was
// no trading name, no rating and no completed-job count to show, and this carried the two facts the
// database could actually answer.
//
// **SHIP-79a created the trading name rather than finding one**, adding `provider_profiles` (000303)
// and `fleet.PublicProfile` — the closed set a customer may be shown, defined once in the domain
// that owns the declaration. A rating and a completed-job count are still absent and still declined
// on the record: the first implies a review mechanism nobody has specified, the second a definition
// that belongs to Docs/02 and data that belongs to another domain.
type ProviderSummary struct {
	// ID is the provider the negotiation is with.
	//
	// The customer already holds it — every offer in the page names one — and it is carried anyway
	// because it is what a client hands back to any later provider-facing endpoint.
	ID uuid.UUID

	// Verified is whether this account has cleared the platform's automated verification: email and
	// phone confirmed, on an account in good standing.
	//
	// **The same predicate `fleet`'s eligibility filter reads**, rather than a second definition —
	// 000002's own comment says provider verification will live in `profiles` and this is the
	// automated row of it that exists today. A customer choosing between offers is choosing between
	// strangers, and Docs/01 §1 makes "verified transport providers" the product's promise; a
	// comparison screen that could not show it would be hiding the one thing the promise is about.
	Verified bool

	// MemberSince is when the provider's account was created.
	//
	// Not a rating and not a job count — neither exists, and "other jobs" is a thing this response
	// may not carry at all. How long somebody has been on the platform is a fact about the account
	// rather than about their work.
	MemberSince time.Time

	// DisplayName is who the provider trades as, and OperatesAs is whether that is a person or a
	// business (SHIP-79a).
	//
	// **The two fields that made this summary say something about the provider rather than about
	// their account.** Until SHIP-79a a customer comparing offers could be told an identifier,
	// whether an email address had been confirmed, and a date — none of which is a fact about who
	// is offering to carry their sofa. `fleet.PublicProfile` is where the closed set is defined and
	// `cmd/api` maps it here; this package deliberately does not own the list, so a second
	// disclosure adds a mapping rather than a second opinion about what a customer may see.
	//
	// Empty when the provider has not declared them, which is an ordinary state: a provider can
	// bid before finishing their profile, and the screen renders "not stated". Never filled in from
	// an email address or a phone number — Docs/01 §7 minimises how far those travel, and a
	// fallback that leaked one would be worse than a blank.
	//
	// **Still not the service area and still not the specialties.** SHIP-102a's *Done when* forbids
	// both by name, and widening this summary is exactly the moment somebody would add them.
	DisplayName string
	OperatesAs  string
}

// VehicleSummary is what a customer comparing offers may know about the vehicle one is made with
// (SHIP-102a).
//
// Docs/01 §4.3's "vehicle, and declared capability", which is why the capacity is here in full: it
// is the half of the sentence a customer with a two-seater sofa is actually reading.
//
// **The registration is deliberately absent.** A plate identifies a vehicle in the physical world
// and nothing on a comparison screen needs one — the customer is choosing between offers, not
// meeting a driver, and the awarded provider's vehicle reaches them again through `delivery` once
// there is something to meet. `contracts/paths/fleet.yaml` already treats a competitor's fleet as
// commercial information; a losing bidder's plate is the same fact with a customer in the middle.
type VehicleSummary struct {
	ID uuid.UUID

	// Type is `fleet`'s vehicle type, carried as the string that domain publishes rather than as a
	// second enumeration. This package has no opinion about the list and must not acquire one:
	// `contracts/statuses.yaml` generates the eight bid statuses because they are this domain's,
	// and vehicle types are not.
	Type string

	Make  string
	Model string

	// MaxWeightKg and the three dimensions are Docs/01 §4.3's "declared capability".
	//
	// Carried as they are declared, including the zeroes. A provider who stated no maximum weight
	// has a vehicle whose capacity is unstated rather than nil, which is `fleet.Capacity`'s own
	// reading of the same columns, and a client renders the difference.
	MaxWeightKg float64
	LengthCm    int
	WidthCm     int
	HeightCm    int
}

// Offeror names one negotiation's provider and, when the offer states one, the vehicle it is made
// with (SHIP-102a).
type Offeror struct {
	ProviderID uuid.UUID

	// VehicleID is [uuid.Nil] when the offer named no vehicle, and [Directory.Describe] answers with
	// a zero [OfferorDetail.Vehicle] for it.
	VehicleID uuid.UUID
}

// OfferorDetail is one offer's provider and vehicle, described for the customer comparing it.
type OfferorDetail struct {
	Provider ProviderSummary

	// Vehicle is the vehicle the offer is made with, and HasVehicle says whether there is one.
	//
	// A flag rather than a nil pointer, for the reason [Bid.SupersededBy] is [uuid.Nil] rather than
	// a pointer: an offer either states a vehicle or does not, and a pointer would add a third state
	// — "not looked up" — that a client would have to tell apart from "none stated" and could not.
	Vehicle    VehicleSummary
	HasVehicle bool
}

// Directory is how the customer's comparison reaches the two things about an offer that are not in
// `bids` (SHIP-102a).
//
// # Why it is a port and not a join
//
// `provider_profiles`, `vehicles` and `users` are other domains' tables. Reading them from
// `bidding/postgres.go` would be the import's effect without the import's visibility — a second
// place that knows what `vehicles.deactivated_at` means, in a file whose header says it is the
// `bids` table in SQL. The composition root is where a dependency between two domains is allowed to
// be visible, which is the argument [Awarding] makes about locking a `jobs` row and
// `cmd/api/routes_delivery.go`'s `acceptedBids` makes pointing the other way.
//
// # One call for the whole page, which is the difference between this and a lookup
//
// The alternative shape — describe one provider, describe one vehicle — reads two rows per offer,
// so a page of a hundred offers costs two hundred round trips to render one screen. This takes the
// page and answers the page. It is also what lets the adapter answer with one statement per table
// however many offers there are.
//
// # It reads and never decides
//
// Nothing here is an authorisation answer. Whether this caller may see this page is settled before
// [Directory.Describe] is reached — [Service.Offers] asks [Negotiation.CustomerOf] first — so an
// implementation of this port cannot widen what a caller sees, only describe what they were already
// permitted to read.
type Directory interface {
	// Describe returns a detail for each offer, keyed by the bid identifier it was asked under.
	//
	// **Keyed by bid rather than by provider**, because a page reached with `?status=` can carry
	// several rows of one negotiation — a superseded offer and the counter that displaced it — and
	// they may name different vehicles. A map keyed on the provider would silently answer one row
	// with another's vehicle.
	//
	// An offer whose provider or vehicle has no row is absent from the result rather than an error,
	// and [Service.Offers] renders it with what it has. A page of offers is not the place to fail
	// because one of them names something unreadable.
	Describe(ctx context.Context, r db.Runner, offers map[uuid.UUID]Offeror) (map[uuid.UUID]OfferorDetail, error)
}
