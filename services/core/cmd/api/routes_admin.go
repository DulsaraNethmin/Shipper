package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/admin"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/delivery"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/passwords"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/profiles"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/ratelimit"
)

// The admin domain's routes (SHIP-163 onwards).
//
// This file exists so that adding a domain adds a file and edits none. cmd/api/routes.go,
// manifest.go and main.go are shared surfaces (Docs/10 §9.2); a route registration that had to go
// into one of them is a line every concurrent branch also touches, and a badly resolved conflict
// there drops an endpoint with no compile error and no failing test.
//
// # The first route in M6, and it is not an administrative one
//
// Docs/04 §7 has three stages and the first is a party to a delivery reporting that something went
// wrong. So this route declares **RequireUser**, and deliberately not RequireAdmin: the caller is a
// customer or a provider, authenticated the ordinary way. Administrator sign-in is a separate system
// a user token can never reach (SHIP-147), the guards map in routes.go has no entry for that class,
// and a route declaring it stops the process at startup rather than being served open — which is the
// right outcome and not one to work around. SHIP-164's administrative half arrives with the
// middleware.
//
// # The route is under /jobs and the handler is in admin, and that is not a contradiction
//
// A URL is a client's map of the product, not a diagram of the packages behind it. Reporting a
// problem is something a client does *to a job*, so it lives under the job — the same reading that
// puts SHIP-106's driver and SHIP-111's milestones there. What decides which domain serves it is who
// owns the rule, and the rules here are `admin`'s: Docs/04 §7 is the intake specification, Docs/04
// §5's sixth queue is where the result lands, and Docs/04 §9's controls govern what happens next.
// SHIP-82 settled the general form of this — a route is declared where its answer is decided, not
// where its path points.
// # The other three routes are the first in the service to declare RequireAdmin (SHIP-147)
//
// They land in the same change that fills `newAdminGuard` (adminauth.go), and that is not a
// convenience — SHIP-15r's seam makes it compulsory. A class with no guard is **absent** from the
// map rather than mapped to something that refuses, so a route declaring RequireAdmin before the
// guard exists stops the process at startup naming the class. Declaring the routes and filling the
// constructor are therefore one commit or a service that will not start.
func init() {
	register(
		Route{
			Method:  http.MethodPost,
			Pattern: "/jobs/{id}/disputes",
			Group:   GroupV1,

			// RequireUser. See the file header: the complainant is a customer or a provider,
			// and the administrative stages of Docs/04 §7 are a separate ticket behind a
			// separate credential.
			Auth:    RequireUser,
			Limit:   LimitWrite,
			Handler: func(d Deps) http.Handler { return adminHandler(d).RaiseDispute() },
		},

		Route{
			Method:  http.MethodPost,
			Pattern: "/jobs/{id}/reports",
			Group:   GroupV1,

			// SHIP-155a — the producing end of Docs/04 §5's second moderation queue.
			//
			// RequireUser, for the reason the dispute route above records: the reporter is a
			// customer or a provider on the ordinary access token, and the administrative
			// stages of Docs/04 §6 are separate tickets behind a separate credential.
			//
			// **Under /jobs rather than /admin**, and the line is the one `GET /v1/admin/jobs`
			// drew: this is a party reporting their own job, scoped to the caller by
			// construction. SHIP-156's queue is every report on the platform, scoped to
			// nothing, and belongs under /admin when it is written.
			//
			// LimitWrite like every other state-changing route. It is worth naming here
			// because a report endpoint is the one a bad actor would most like to run in a
			// loop, and Docs/12's write class is what stands between them and a queue full of
			// noise — there is deliberately no "one report per job" rule to lean on
			// (`000805`).
			Auth:    RequireUser,
			Limit:   LimitWrite,
			Handler: func(d Deps) http.Handler { return adminHandler(d).RaiseReport() },
		},

		Route{
			Method:  http.MethodPost,
			Pattern: "/admin/sessions",
			Group:   GroupV1,

			// Public, and on manifest_test.go's publicMutatingRoutes allow-list because of it.
			// An endpoint that hands out a credential cannot require one; what makes it safe
			// is that it is rate limited per account and per address, which is the property
			// every entry on that list shares.
			Auth:    Public,
			Limit:   LimitCredential,
			Handler: func(d Deps) http.Handler { return adminHandler(d).SignIn() },
		},

		Route{
			Method:  http.MethodDelete,
			Pattern: "/admin/sessions/current",
			Group:   GroupV1,
			Auth:    RequireAdmin,
			Limit:   LimitWrite,
			Handler: func(d Deps) http.Handler { return adminHandler(d).SignOut() },
		},

		Route{
			Method:  http.MethodGet,
			Pattern: "/admin/me",
			Group:   GroupV1,
			Auth:    RequireAdmin,
			Limit:   LimitRead,
			Handler: func(d Deps) http.Handler { return adminHandler(d).Me() },
		},

		Route{
			Method:  http.MethodGet,
			Pattern: "/admin/moderation/exceptions",
			Group:   GroupV1,

			// RequireAdmin, and `moderation.read` inside the handler (SHIP-117, SHIP-148).
			// Every role holds that permission: reading a queue is what the least-privileged
			// role exists to be able to do.
			Auth:    RequireAdmin,
			Limit:   LimitRead,
			Handler: func(d Deps) http.Handler { return adminHandler(d).ExceptionQueue() },
		},

		Route{
			Method:  http.MethodGet,
			Pattern: "/admin/moderation/cancellations",
			Group:   GroupV1,

			// Docs/04 §5's fifth queue (SHIP-158), beside its fourth rather than inside it:
			// the document numbers its queues, and this one asks who withdrew from a
			// commitment rather than which delivery is going wrong.
			//
			// RequireAdmin, and `moderation.read` inside the handler, like every other queue.
			Auth:    RequireAdmin,
			Limit:   LimitRead,
			Handler: func(d Deps) http.Handler { return adminHandler(d).CancellationQueue() },
		},

		Route{
			Method:  http.MethodGet,
			Pattern: "/admin/users",
			Group:   GroupV1,

			// RequireAdmin is the credential; `users.read` is the permission, checked in the
			// handler (SHIP-148, SHIP-151). Every role holds it — searching is what the
			// least-privileged role exists to be able to do, and Docs/01 §4.6 lists it first.
			//
			// A collection under /admin rather than /users, because the shape is the
			// administrator's view of an account and not the account holder's. There is no
			// endpoint by which a user reads another user, and this is not one arrived at
			// through a different credential.
			Auth:    RequireAdmin,
			Limit:   LimitRead,
			Handler: func(d Deps) http.Handler { return adminHandler(d).SearchUsers() },
		},

		Route{
			Method:  http.MethodGet,
			Pattern: "/admin/jobs",
			Group:   GroupV1,

			// RequireAdmin is the credential; `jobs.read` is the permission, checked in the
			// handler (SHIP-148, SHIP-152). Every role holds it — Docs/01 §4.6 lists searching
			// first, and looking is what the least-privileged role exists to be able to do.
			//
			// Under /admin rather than /jobs, and not merely because the handler is admin's.
			// `GET /v1/jobs` is the customer's own jobs and `GET /v1/fleet/jobs` is the
			// provider's eligible feed; both are scoped to the caller by construction. This
			// one is scoped to nothing, which is a different resource wearing the same noun,
			// and putting it under /jobs would make the scope a property of the credential
			// rather than of the path.
			Auth:    RequireAdmin,
			Limit:   LimitRead,
			Handler: func(d Deps) http.Handler { return adminHandler(d).SearchJobs() },
		},

		Route{
			Method:  http.MethodGet,
			Pattern: "/admin/jobs/{id}",
			Group:   GroupV1,

			// SHIP-152's second half — "open any job with its full bid and status history".
			// One shape carrying all three, because a console making three calls to draw one
			// screen is three chances to show a job beside somebody else's bids.
			Auth:    RequireAdmin,
			Limit:   LimitRead,
			Handler: func(d Deps) http.Handler { return adminHandler(d).OpenJob() },
		},

		Route{
			Method:  http.MethodGet,
			Pattern: "/admin/audit",
			Group:   GroupV1,

			// RequireAdmin is the credential; `audit.read` is the permission, checked in the
			// handler (SHIP-148, SHIP-165). Every role holds it, including the least
			// privileged — a trail only the people it records can read is not a control.
			//
			// **A GET and nothing else, for ever.** There is no POST, PATCH or DELETE on this
			// path and no permission that would authorise one: CLAUDE.md's invariant is that
			// audit entries are append-only and ordinary administrators cannot delete them,
			// and `000003`s triggers refuse both from any connection. Entries are written by
			// the actions that cause them, in the same transaction, never by a route.
			Auth:    RequireAdmin,
			Limit:   LimitRead,
			Handler: func(d Deps) http.Handler { return adminHandler(d).AuditTrail() },
		},

		Route{
			Method:  http.MethodPost,
			Pattern: "/admin/jobs/{id}/unpublish",
			Group:   GroupV1,

			// RequireAdmin is the credential; `jobs.unpublish` is the permission, checked in
			// the handler (SHIP-148, SHIP-160). **Not the same permission as reading the job**
			// — `support` may open any job and may not remove one, which is Docs/04 §9's
			// least-privilege control expressed as two permissions on two endpoints.
			//
			// Five segments, which is safe. A four-segment `GET /v1/jobs/{id}/<literal>` used
			// to collide with `GET /v1/jobs/open/{id}` at registration; SHIP-83a moved that
			// feed to `/v1/fleet/jobs/{id}` and freed the space. This route never depended on
			// it — it is under /admin, is a POST, and has no literal sibling at its depth.
			//
			// POST rather than DELETE, because nothing is deleted: the job moves to
			// `Cancelled` through the guarded transition and the row stays where it is
			// (Docs/05 §3.1). A DELETE verb would describe the opposite of what happens.
			Auth:    RequireAdmin,
			Limit:   LimitWrite,
			Handler: func(d Deps) http.Handler { return adminHandler(d).UnpublishJob() },
		},

		Route{
			Method:  http.MethodPost,
			Pattern: "/admin/users/{id}/standing",
			Group:   GroupV1,

			// RequireAdmin is the credential; `users.restrict` is the permission (SHIP-148,
			// SHIP-161). `support` holds `users.read` and not this one.
			//
			// A named sub-resource rather than PATCH on the account, because the only field an
			// administrator may set is this one — the role is fixed at registration by trigger
			// (SHIP-45) and the contact details are the account holder's. A PATCH would invite
			// a body that grows keys.
			Auth:    RequireAdmin,
			Limit:   LimitWrite,
			Handler: func(d Deps) http.Handler { return adminHandler(d).SetStanding() },
		},

		Route{
			Method:  http.MethodPost,
			Pattern: "/admin/users/{id}/suspension",
			Group:   GroupV1,

			// Docs/04 §9's two-person review, first half (SHIP-166). `users.restrict`, the
			// same permission the approval needs — the control is that there are two people,
			// not that either holds something the other does not.
			//
			// A sibling of `/standing` rather than a value on it: the two answer different
			// shapes and different statuses, and one endpoint that sometimes changed an
			// account and sometimes filed a request is one a console has to branch inside.
			Auth:    RequireAdmin,
			Limit:   LimitWrite,
			Handler: func(d Deps) http.Handler { return adminHandler(d).RequestSuspension() },
		},

		Route{
			Method:  http.MethodGet,
			Pattern: "/admin/suspensions",
			Group:   GroupV1,

			// The queue a second administrator finds a request in (SHIP-166). `users.read`,
			// which every role holds including `support`: seeing that a suspension has been
			// proposed is looking, and acting on it is the other endpoint's permission.
			//
			// **Without this route the control does not work** — a review nobody can see is a
			// review nobody approves.
			Auth:    RequireAdmin,
			Limit:   LimitRead,
			Handler: func(d Deps) http.Handler { return adminHandler(d).PendingSuspensions() },
		},

		Route{
			Method:  http.MethodPost,
			Pattern: "/admin/suspensions/{id}/approval",
			Group:   GroupV1,

			// The second half, and the one the control turns on (SHIP-166). A *different*
			// administrator agrees and the suspension is applied in the same transaction.
			//
			// The approver is taken from the verified session and never from the body, which
			// is why the endpoint accepts no body at all: a two-person control whose second
			// signature a client could name has one participant.
			Auth:    RequireAdmin,
			Limit:   LimitWrite,
			Handler: func(d Deps) http.Handler { return adminHandler(d).ApproveSuspension() },
		},

		Route{
			Method:  http.MethodPost,
			Pattern: "/admin/notes",
			Group:   GroupV1,

			// RequireAdmin is the credential; `notes.write` is the permission (SHIP-148,
			// SHIP-162). `moderator` and `owner` hold it and `support` does not — and reading
			// the notes needs *less*, which is the right way round: a support administrator
			// reads the history and a moderator adds to it.
			//
			// A collection of its own rather than a sub-resource of the subject, because a
			// note is about a user *or* a job and one endpoint that takes the kind is one
			// place the "never user-visible" rule has to hold. Two sub-resources under
			// /admin/users/{id}/notes and /admin/jobs/{id}/notes would be two.
			Auth:    RequireAdmin,
			Limit:   LimitWrite,
			Handler: func(d Deps) http.Handler { return adminHandler(d).AddNote() },
		},

		Route{
			Method:  http.MethodGet,
			Pattern: "/admin/moderation/expiring-documents",
			Group:   GroupV1,

			// Docs/04 §5's **seventh** and last queue (SHIP-159) — "expiring or expired
			// provider verification records" — beside the delivery exceptions and the
			// post-award cancellations, which are §5's fourth and fifth.
			//
			// **Under /admin/moderation/ rather than under /admin/verifications/**, which is
			// where the ticket's neighbours might suggest it belongs. The two queues under this
			// prefix are the precedent and `moderation.read` is the permission
			// permissions.go already names SHIP-159 for; `/admin/verifications` is the review
			// queue of *providers*, and this is a list of *documents* whose subject happens to
			// be the same people. Putting it there would also have made it a literal sibling of
			// the `{id}` slot the decision and the evidence viewer both use.
			//
			// RequireAdmin, and `moderation.read` inside the handler (SHIP-148). Every role
			// holds it: reading a queue is what the least-privileged role exists to be able to
			// do.
			//
			// **It writes nothing**, unlike the evidence viewer one queue over. This is dates
			// and contact details, not images and not credentials — so it is under the rule
			// admin.AuditActions states rather than the exception SHIP-155 argues for.
			Auth:    RequireAdmin,
			Limit:   LimitRead,
			Handler: func(d Deps) http.Handler { return adminHandler(d).ExpiringDocumentsQueue() },
		},

		Route{
			Method:  http.MethodGet,
			Pattern: "/admin/notes",
			Group:   GroupV1,

			// The permission here is the **subject's** — `users.read` or `jobs.read`, chosen
			// in the handler from the query. permissions.go records why there is no
			// `notes.read`: notes never leave the console, so what needs distinguishing is
			// who may add one.
			Auth:    RequireAdmin,
			Limit:   LimitRead,
			Handler: func(d Deps) http.Handler { return adminHandler(d).ReadNotes() },
		},

		Route{
			Method:  http.MethodGet,
			Pattern: "/admin/verifications",
			Group:   GroupV1,

			// Docs/04 §5's **first** queue (SHIP-153), and the endpoint that ends the state
			// SHIP-81a left the marketplace in: every provider backfilled `Pending`, and no
			// way for anybody to see who was waiting.
			//
			// RequireAdmin is the credential; `verifications.read` is the permission, checked
			// in the handler (SHIP-148). Every role holds it, including `support` — Docs/01
			// §4.6 lists "review provider verification status" second, and looking is what the
			// least-privileged role exists to be able to do.
			//
			// Under /admin and not under /provider. `GET /v1/provider/verification` is the
			// provider's own record, scoped to the caller by construction and with no
			// identifier anywhere in it; this is every provider's, scoped to nothing, which is
			// a different resource wearing the same noun. routes_profiles.go's header settled
			// that before either endpoint existed.
			Auth:    RequireAdmin,
			Limit:   LimitRead,
			Handler: func(d Deps) http.Handler { return adminHandler(d).VerificationQueue() },
		},

		Route{
			Method:  http.MethodPost,
			Pattern: "/admin/verifications/{id}/decision",
			Group:   GroupV1,

			// SHIP-154, and the act `profiles.Service.Decide` was exported and left unrouted
			// for. **This is the only route to it in the service, and that is the point**:
			// routes_profiles.go declines to put one on the user credential because Docs/04
			// §9's least-privilege requirement makes a second way to reach the same act on the
			// wrong credential the thing to avoid.
			//
			// `verifications.decide` is the permission, which `moderator` and `owner` hold and
			// `support` does not — the same split `jobs.read` against `jobs.unpublish` makes.
			//
			// `{id}` is the **provider's account identifier**. There is one verification record
			// per provider and `provider_verifications.provider_id` is its primary key, so
			// there is no second identifier to name — `000200` records why a separate `id`
			// column would only make "which of these is current" a question.
			//
			// Five segments, which is safe: the four-segment collision SHIP-83a cleared was
			// `GET /v1/jobs/{id}/<literal>`, this is a POST under /admin, and it has no literal
			// sibling at its depth.
			//
			// A named sub-resource rather than PATCH on the record: the state is not a settable
			// field — `provider_verification_change_is_guarded` refuses any UPDATE that no
			// decision describes — so a verb that reads as "edit these columns" would describe
			// the opposite of what happens.
			Auth:    RequireAdmin,
			Limit:   LimitWrite,
			Handler: func(d Deps) http.Handler { return adminHandler(d).DecideVerification() },
		},

		Route{
			Method:  http.MethodGet,
			Pattern: "/admin/verifications/{id}/documents",
			Group:   GroupV1,

			// SHIP-155, and the screen that sits between the queue above and the decision
			// above that. Docs/04 §3 defines the review as an administrator looking at the
			// images "by eye for obvious validity"; SHIP-81b built the table and the
			// provider's own endpoint over it, and until this route there was no way for the
			// person doing the reviewing to see anything.
			//
			// RequireAdmin is the credential; `verifications.read` is the permission, checked
			// in the handler (SHIP-148). The same permission the queue takes, which is a
			// decision rather than an inheritance — the handler's own comment argues it
			// against a narrower `verifications.evidence`, and the reason it is defensible is
			// that **every read here writes an audit entry naming who made it**.
			//
			// **A GET that writes, which is the one thing about this route worth pausing on.**
			// The write is an access record rather than a state change: the resource is
			// unchanged, a repeat is safe, and what accumulates is the trail. It is therefore
			// deliberately *not* in `auditedAdminMutations` — that table is the served
			// surface's mutating half and a read listed there fails its own reverse check.
			// `auditedAdminReads` in routes_admin_test.go is the tripwire for this one.
			//
			// Five segments with the wildcard fourth, the same shape as the decision route
			// above and safe for the same reason: no literal sibling at that depth. The
			// four-segment sibling `GET /v1/admin/verifications/expiring` (SHIP-159) is a
			// different length and does not overlap.
			//
			// `{id}` is the **provider's account identifier**, as it is on the decision: one
			// verification record per provider, `provider_verifications.provider_id` its
			// primary key, and a provider's file addressed by the provider.
			Auth:    RequireAdmin,
			Limit:   LimitRead,
			Handler: func(d Deps) http.Handler { return adminHandler(d).VerificationEvidence() },
		},

		Route{
			Method:  http.MethodGet,
			Pattern: "/admin/disputes",
			Group:   GroupV1,

			// Docs/04 §5's **sixth** queue (SHIP-164), and the endpoint `idx_disputes_open`
			// was built for in `000800` and read by nothing for four waves. Without it an
			// administrator cannot find a dispute at all: `GET /v1/admin/jobs` searches jobs
			// and would need them to already know which job to look at, which is the question
			// this queue answers.
			//
			// RequireAdmin is the credential; `disputes.read` is the permission, checked in
			// the handler (SHIP-148). Every role holds it including `support` — looking is
			// what the least-privileged role exists to be able to do, and deciding what is in
			// the queue is a different permission on a different endpoint.
			//
			// Under /admin rather than /jobs, and not merely because the handler is admin's.
			// `POST /v1/jobs/{id}/disputes` is a party raising one about their own delivery,
			// scoped to the caller by construction; this is every dispute on the platform,
			// scoped to nothing — a different resource wearing the same noun, which is the
			// line `GET /v1/admin/jobs` drew against `GET /v1/jobs`.
			Auth:    RequireAdmin,
			Limit:   LimitRead,
			Handler: func(d Deps) http.Handler { return adminHandler(d).DisputeQueue() },
		},

		Route{
			Method:  http.MethodGet,
			Pattern: "/admin/disputes/{id}",
			Group:   GroupV1,

			// Docs/04 §7's investigation read (SHIP-164): the complaint, the desired outcome
			// and the evidence, which were reachable from nowhere. The rest of what §7 asks a
			// reviewer to look at is already served — `GET /v1/admin/jobs/{id}` carries the
			// listing, every bid and every transition (SHIP-152).
			//
			// **`{id}` is the dispute, not the job.** One job may accumulate several disputes
			// over its life while never having two open at once, so a route keyed by the job
			// could not name a settled one. `disputes.read` is the permission.
			Auth:    RequireAdmin,
			Limit:   LimitRead,
			Handler: func(d Deps) http.Handler { return adminHandler(d).OpenDispute() },
		},

		Route{
			Method:  http.MethodPost,
			Pattern: "/admin/disputes/{id}/resolution",
			Group:   GroupV1,

			// Docs/04 §7's outcome stage (SHIP-164), and the act the ticket's *Done when* is
			// about: a documented outcome that unfreezes the job. `disputes.resolve` is the
			// permission, which `moderator` and `owner` hold and `support` does not — the
			// same split `jobs.read` against `jobs.unpublish` makes.
			//
			// Five segments, which is safe: the four-segment collision SHIP-83a cleared was
			// `GET /v1/jobs/{id}/<literal>`, this is a POST under /admin, and it has no
			// literal sibling at its depth.
			//
			// A named sub-resource rather than PATCH on the dispute: recording an outcome
			// writes three columns `ck_disputes_resolution` binds together *and* moves a job
			// through the one guarded transition, so a verb that reads as "edit these
			// columns" would describe something narrower than what happens.
			Auth:    RequireAdmin,
			Limit:   LimitWrite,
			Handler: func(d Deps) http.Handler { return adminHandler(d).ResolveDispute() },
		},

		Route{
			Method:  http.MethodPost,
			Pattern: "/admin/administrators",
			Group:   GroupV1,

			// RequireAdmin is the credential; `admins.manage` is the permission, and it is
			// checked in the handler rather than declared here (SHIP-148). The manifest's
			// auth class says which *credential system* serves a route, and there is no
			// fifth class per permission — twelve permissions would be twelve classes, and
			// guardsFor is finished.
			Auth:    RequireAdmin,
			Limit:   LimitWrite,
			Handler: func(d Deps) http.Handler { return adminHandler(d).CreateAdministrator() },
		},
	)
}

// adminHandler builds the domain's handler from what Deps already carries.
//
// Nothing admin needs is missing from Deps, which is the test Docs/10 §9.2 sets for whether a domain
// has been written the way the shared-surface rules ask: the job service, the outbox writer and the
// two ports are all pure functions of the pool, the clock and the configuration, so no field had to
// be added to a shared struct and no line to its literal in main.go.
//
// It panics for the same reason jobsHandler and deliveryHandler do: it runs during attach, at
// startup, and every failure it can report is a wiring mistake that will still be there after a
// restart. The pool is deliberately not checked — it may be nil because the database was unreachable
// at startup, which is a transient condition the service is built to survive, and the handlers
// answer 503 for as long as it lasts.
// newJobService is routes_delivery.go's, and it is shared rather than copied deliberately: it is
// already written as "a job service for a domain that needs to move a job but is not `jobs`", and a
// second constructor differing only in which file it sits in is a second place for the geocoder
// decision to be made differently.
// The argon2id profile is d.Config.Passwords.Argon2, and that is deliberate rather than borrowed.
// SHIP-15r moved argon2id into `internal/passwords` precisely so that a second domain needing to
// hash a password would not be the reason for a second implementation, and a second *cost knob*
// would be the same mistake one level up: two profiles that agree by comment until somebody raises
// one. There is one platform password cost and this is where it is configured.
//
// **It was `d.Config.Identity.Argon2` until SHIP-147a**, which was the platform's cost spelled as
// one domain's — narrower than its meaning from the moment this call site was written. The field
// and its `IDENTITY_ARGON2_*` variables now read `Passwords` and `PASSWORDS_ARGON2_*`; identity's
// hasher in routes_identity.go reads the same one, and there is no second knob to raise instead.
func adminHandler(d Deps) *admin.Handler {
	svc := admin.NewService(disputeLifecycle{jobs: newJobService(d)}, jobPartiesLookup{}, d.Clock)

	// SHIP-155a. **The same `jobPartiesLookup` the dispute intake takes, deliberately**: the
	// ticket's *Done when* resolves the reporter's side "from the job and its accepted bid",
	// which is that adapter's query verbatim. It takes no `disputeLifecycle` — a report moves no
	// job status, and the absence of the collaborator is what makes that true rather than
	// intended (admin/report.go).
	reports := admin.NewReports(jobPartiesLookup{}, jobMessageLookup{})

	hasher, err := passwords.NewHasher(passwords.Argon2Profile{
		MemoryKiB:   d.Config.Passwords.Argon2.MemoryKiB,
		Iterations:  d.Config.Passwords.Argon2.Iterations,
		Parallelism: d.Config.Passwords.Argon2.Parallelism,
	})
	if err != nil {
		panic("cmd/api: admin password hasher: " + err.Error())
	}

	// The same prefix identity's limiter uses, so both live in one keyspace a person can scan;
	// the *keys* are prefixed `admin-signin:` by the domain, so an administrator's failures and a
	// user's failures against the same address are separate allowances.
	limiter, err := ratelimit.New(d.Redis, "rl:v1:", d.Clock)
	if err != nil {
		panic("cmd/api: admin rate limiter: " + err.Error())
	}

	// SHIP-150. Built from d.Clock rather than from a clock of its own, which is the whole of
	// Docs/11 §9's "one row, one clock": an audit entry has to sort against the session row and the
	// status-history row it describes, and both of those take this clock.
	auditor, err := admin.NewAuditor(d.Clock)
	if err != nil {
		panic("cmd/api: admin audit writer: " + err.Error())
	}

	creds, err := admin.NewCredentials(d.Pool, hasher, limiter, d.Clock, auditor)
	if err != nil {
		panic("cmd/api: admin credentials: " + err.Error())
	}

	// SHIP-157. The threshold is read from `internal/delivery`, which owns Docs/02 §3.1's
	// twenty-four hours, and handed to a domain that must not hold a second copy of it — the
	// same arrangement the status vocabulary takes below. **This import is what makes the number
	// have one authority**: a constant in `internal/admin` would agree with this one by comment
	// until somebody moved one, which is Docs/10 §3.4's failure applied to a duration.
	moderation, err := admin.NewModeration(
		exceptionQueueLookup{}, delivery.UnsyncedAlertThreshold, d.Pool)
	if err != nil {
		panic("cmd/api: admin moderation: " + err.Error())
	}

	// SHIP-158. Docs/04 §5's fifth queue, over `job_status_history`, `jobs` and `bids` — three
	// tables belonging to two domains `internal/admin` may not import, which is why the statement
	// is a port implemented here rather than a method on that domain's store.
	cancellations, err := admin.NewCancellations(cancellationQueueLookup{}, d.Pool)
	if err != nil {
		panic("cmd/api: admin cancellations: " + err.Error())
	}

	// SHIP-166. Docs/04 §9's two-person review. It needs the auditor and nothing else: the table
	// is this domain's own, in its own migration block, so there is no port and no statement in
	// this file — which postgres_users.go's rule predicts.
	suspensions, err := admin.NewSuspensions(auditor, d.Pool)
	if err != nil {
		panic("cmd/api: admin suspensions: " + err.Error())
	}

	// SHIP-151. It takes the pool alone: the search reads `users`, which is a shared table this
	// domain may read directly, and there is no port to supply. See internal/admin/postgres_users.go
	// for why that is not the arrangement jobPartiesLookup and exceptionQueueLookup use.
	users, err := admin.NewUsers(d.Pool)
	if err != nil {
		panic("cmd/api: admin account search: " + err.Error())
	}

	// SHIP-152. The status list is supplied rather than copied: `admin` may not import `jobs`,
	// and the alternative to passing it here is a hand-written twelve-value list in that package
	// shadowing one generated from `contracts/statuses.yaml` (SHIP-56a). This is the composition
	// root, where both vocabularies are visible — the same place `actorFor` maps one domain's
	// actor kinds onto another's.
	statuses := make([]string, 0, len(jobs.Statuses))
	for _, s := range jobs.Statuses {
		statuses = append(statuses, s.String())
	}

	jobConsole, err := admin.NewJobConsole(jobDirectory{}, statuses, d.Pool)
	if err != nil {
		panic("cmd/api: admin job search: " + err.Error())
	}

	// SHIP-165. It takes the pool alone: `audit_log` is a shared table this domain writes and
	// now reads, and there is no port to supply. Deliberately a *second* type rather than a
	// method on the auditor above — one can only append and the other can only read, which is
	// what makes the append-only invariant structural rather than a habit.
	trail, err := admin.NewAuditTrail(d.Pool)
	if err != nil {
		panic("cmd/api: admin audit trail: " + err.Error())
	}

	// SHIP-160, SHIP-161. It takes the same lifecycle adapter the dispute workflow does — one
	// `admin.Jobs` implementation with a method per move this domain needs, rather than one per
	// caller — plus the auditor built above, so every action it performs shares the clock the
	// rest of the transaction uses.
	enforcement, err := admin.NewEnforcement(
		disputeLifecycle{jobs: newJobService(d)}, auditor, d.Pool)
	if err != nil {
		panic("cmd/api: admin enforcement: " + err.Error())
	}

	// SHIP-162. It takes the auditor above rather than one of its own, so a note and the audit
	// entry describing it share an instant — Docs/11 §9's one row, one clock, across the two rows
	// one action writes.
	notes, err := admin.NewNotes(auditor, d.Pool)
	if err != nil {
		panic("cmd/api: admin notes: " + err.Error())
	}

	// SHIP-153, SHIP-154. The five outcomes are supplied rather than copied, exactly as the job
	// statuses are above: `admin` may not import `profiles`, and the alternative to passing them
	// here is a hand-written five-value list in that package shadowing one already held to
	// `ck_provider_verifications_state` by a test in both directions. This is the composition root,
	// where both vocabularies are visible.
	verificationStates := make([]string, 0, len(profiles.States))
	for _, s := range profiles.States {
		verificationStates = append(verificationStates, s.String())
	}

	verifications, err := admin.NewVerifications(
		providerVerifications{svc: profiles.NewService(d.Clock)}, verificationStates, auditor, d.Pool)
	if err != nil {
		panic("cmd/api: admin verification console: " + err.Error())
	}

	// SHIP-155. It takes its own `profiles.Documents`, built from the same signer
	// routes_profiles.go hands the provider's own endpoints — the seam that file opened in
	// advance, so that the console reaches the evidence without `profiles.NewService` widening to
	// carry a signer for the queue and the decision as well. The auditor is the one built above,
	// so the access entry shares the clock and the transaction of the read it records.
	evidence, err := admin.NewEvidence(
		providerEvidence{documents: verificationDocuments(d)}, auditor, d.Pool)
	if err != nil {
		panic("cmd/api: admin verification document viewer: " + err.Error())
	}

	// SHIP-159. Docs/04 §5's seventh queue. The lead times come from `internal/config` with no
	// default and are translated into this domain's own [profiles.Kind] here — the composition
	// root, where both vocabularies are visible — because configuration is infrastructure and may
	// not import a domain (SHIP-15c). `profiles.NewExpiry` refuses a kind the platform does not
	// have, which is what stops a typo in the variable becoming configuration that silently does
	// nothing.
	expiry, err := admin.NewExpiryQueue(
		expiringDocuments{expiry: profiles.NewExpiry(d.Clock, verificationLeadTimes(d))}, d.Pool)
	if err != nil {
		panic("cmd/api: admin document expiry queue: " + err.Error())
	}

	// SHIP-164. It takes the same lifecycle adapter intake and enforcement do — one
	// `admin.Jobs` implementation with a method per move this domain needs — and the auditor
	// built above, so the dispute row, the job's transition and the audit entry all share the
	// clock and all commit together.
	disputeWorkflow, err := admin.NewDisputeWorkflow(
		disputeLifecycle{jobs: newJobService(d)}, auditor, d.Clock, d.Pool)
	if err != nil {
		panic("cmd/api: admin dispute workflow: " + err.Error())
	}

	handler, err := admin.NewHandler(admin.HandlerServices{
		Disputes:      svc,
		Credentials:   creds,
		Moderation:    moderation,
		Cancellations: cancellations,
		Users:         users,
		Jobs:          jobConsole,
		Trail:         trail,
		Enforcement:   enforcement,
		Notes:         notes,
		Suspensions:   suspensions,
		Verifications: verifications,
		Evidence:      evidence,
		Expiry:        expiry,

		DisputeWorkflow: disputeWorkflow,
		Reports:         reports,
	}, d.Pool, d.Logger)
	if err != nil {
		panic("cmd/api: admin handler: " + err.Error())
	}
	return handler
}

// disputeLifecycle implements admin.Jobs over the guarded transition (SHIP-57).
//
// The same seam routes_delivery.go describes working as intended: `admin` names what it needs in its
// own ports.go, `jobs` knows nothing about `admin`, and the two are joined here by a type that
// translates one vocabulary into the other. Go satisfies the interface structurally, so neither
// package imports the other.
//
// # The translation is of errors into outcomes, and of a party into an actor
//
// admin cannot call errors.Is against jobs' sentinels — that is an import — so the refusals come
// back as an admin.JobMove instead. Everything jobs treats as a refusal becomes an outcome with a
// nil error; everything else stays an error, because a failing database is not an answer.
//
// The second translation is admin.Party into jobs.ActorType. Both packages need a word for "the
// customer" and neither may use the other's, so the mapping lives here — the composition root, where
// both vocabularies are visible. **This is the only place in the service that knows the two lists
// correspond.**
type disputeLifecycle struct {
	jobs *jobs.Service
}

// MoveToDisputed runs Docs/02 §2's `Awarded through Delivered → Disputed` through the one guarded
// function, inside the caller's transaction.
//
// The actor is the complainant, recorded as the side of the job they were on — which is what Docs/02
// §1 names as the primary actors for this status ("a customer, provider, or admin has raised an
// unresolved issue") and what job_status_history records.
//
// The reason is the dispute's category. Docs/01 §3 requires one only of an administrator, so this is
// not obligatory — it is here because the alternative is a job_status_history row saying a job froze
// and not saying why, when the answer was already in hand.
//
// RecordedAt is left zero, so jobs.Transition uses the platform's clock for both. That is correct
// here: the request is made online, by a person at a screen, so there is only one clock for the act
// of freezing the job. When the *incident* happened is a different fact and is recorded on the
// dispute, where it is not mistaken for the time somebody reported it.
func (l disputeLifecycle) MoveToDisputed(
	ctx context.Context,
	r db.Runner,
	jobID, actorID uuid.UUID,
	party admin.Party,
	reason string,
) (admin.JobMove, error) {
	actor, err := actorFor(party)
	if err != nil {
		return admin.JobMoveUnrecognised, err
	}

	_, err = l.jobs.Transition(ctx, r, jobs.Move{
		JobID:  jobID,
		To:     jobs.StatusDisputed,
		Actor:  jobs.User(actor, actorID),
		Reason: reason,
	})

	switch {
	case err == nil:
		return admin.JobMoved, nil
	case errors.Is(err, jobs.ErrJobNotFound):
		return admin.JobNotFound, nil
	case errors.Is(err, jobs.ErrAlreadyInStatus):
		return admin.JobAlreadyDisputed, nil
	case errors.Is(err, jobs.ErrTransitionNotPermitted):
		return admin.JobNotDisputable, nil
	default:
		return admin.JobMoveUnrecognised, err
	}
}

// Unpublish runs Docs/02 §2's `Draft / Open / Negotiating → Cancelled` through the one guarded
// function, inside the caller's transaction (SHIP-160).
//
// # The actor is an administrator, and that is the whole difference from MoveToDisputed
//
// `jobs.ActorAdmin` rather than a party, so `job_status_history` records that the platform's staff
// moved the job rather than its customer. The reason is **required** of that actor by
// `ck_job_status_history_admin_reason` and by `Move.validate`, and `admin` refuses an empty one a
// layer earlier so the failure names the field.
//
// # Which statuses this can move from is Docs/02 §2's decision, not this adapter's
//
// The guard's own table permits Cancelled from Draft, Open, Negotiating and Disputed. This method
// names no source status and checks none: it asks for the move and translates the refusal. An
// awarded job therefore comes back as JobNotRemovable, because the document has no such row — a
// provider has committed, and Docs/02 §6.2 makes ending it after that a support case. Encoding that
// list here would be the transition table's second copy.
//
// # 'Disputed' is a source the guard permits and this route will not see
//
// A disputed job is unfrozen by SHIP-164 resolving the dispute, which is a different action with
// different evidence and its own audit entry. Nothing here prevents the move; what prevents it is
// that a moderator working the queue reaches a disputed job through the dispute. If that turns out
// to matter, the check belongs in `admin` beside the permission, not in this translation.
//
// RecordedAt is left zero, so jobs.Transition uses the platform's clock for both. Correct here: an
// administrator is a person at a screen, online, and there is only one clock for the act.
func (l disputeLifecycle) Unpublish(
	ctx context.Context,
	r db.Runner,
	jobID, actorID uuid.UUID,
	reason string,
) (admin.JobMove, error) {
	_, err := l.jobs.Transition(ctx, r, jobs.Move{
		JobID:  jobID,
		To:     jobs.StatusCancelled,
		Actor:  jobs.User(jobs.ActorAdmin, actorID),
		Reason: reason,
	})

	switch {
	case err == nil:
		return admin.JobMoved, nil
	case errors.Is(err, jobs.ErrJobNotFound):
		return admin.JobNotFound, nil
	case errors.Is(err, jobs.ErrAlreadyInStatus):
		return admin.JobAlreadyRemoved, nil
	case errors.Is(err, jobs.ErrTransitionNotPermitted):
		return admin.JobNotRemovable, nil
	default:
		return admin.JobMoveUnrecognised, err
	}
}

// ResolveAsCompleted runs Docs/02 §2's `Disputed → Completed` — "admin resolves dispute with
// delivery accepted" — through the one guarded function, inside the caller's transaction (SHIP-164).
//
// # This and its sibling are what unfreeze a job, and there is nothing else that does
//
// `Docs/02` §3: "a dispute freezes automatic completion until an administrator resolves it". The
// freeze is not a flag and there is no pause to lift — the job simply sits in `Disputed`, where the
// 72-hour auto-complete (Docs/02 §6.1) has no row to run on. These two methods are the only
// transitions out of it in the platform, and `admin.DisputeWorkflow` runs one of them in the same
// transaction that records the outcome.
//
// # The actor is an administrator, like Unpublish and unlike MoveToDisputed
//
// `jobs.ActorAdmin`, so `job_status_history` records that the platform's staff resolved the dispute
// rather than that a party did. The reason is **required** of that actor by
// `ck_job_status_history_admin_reason` and by `Move.validate`, and `admin` refuses an empty one a
// layer earlier so the failure names the field.
//
// # No new domain event, deliberately
//
// The transition emits `job.status_changed` inside this transaction, and
// `notifications.StatusRules` already routes `Completed` and `Cancelled` to the job's customer and
// its awarded provider — `rules.go` says so of this ticket by name. A `dispute.resolved` event would
// be a second announcement of one state change, which is the failure those rules are written to
// prevent, and it would need a routing rule in a package this change does not own. SHIP-160 recorded
// the same finding for the same reason.
//
// RecordedAt is left zero, so jobs.Transition uses the platform's clock for both. Correct here: an
// administrator is a person at a screen, online, and there is one clock for the act of resolving.
func (l disputeLifecycle) ResolveAsCompleted(
	ctx context.Context,
	r db.Runner,
	jobID, actorID uuid.UUID,
	reason string,
) (admin.JobMove, error) {
	return l.resolve(ctx, r, jobID, actorID, jobs.StatusCompleted, reason)
}

// ResolveAsCancelled runs Docs/02 §2's `Disputed → Cancelled` — "admin resolves as cancelled/failed
// delivery" — through the one guarded function, inside the caller's transaction (SHIP-164).
//
// See [disputeLifecycle.ResolveAsCompleted] for the reasoning both share.
//
// **It ends in the same status as [disputeLifecycle.Unpublish] and is not the same act.** Unpublish
// removes a job nobody has committed to, from Draft, Open or Negotiating; this ends a delivery
// somebody has committed to, from `Disputed`, after an administrator has read a complaint and made a
// finding. Two audit actions, two reasons in `job_status_history`, and a trail that can tell a
// policy removal from a resolved dispute — which one method serving both would have made impossible.
func (l disputeLifecycle) ResolveAsCancelled(
	ctx context.Context,
	r db.Runner,
	jobID, actorID uuid.UUID,
	reason string,
) (admin.JobMove, error) {
	return l.resolve(ctx, r, jobID, actorID, jobs.StatusCancelled, reason)
}

// resolve is the transition and the translation both resolutions share.
//
// Unexported and taking a `jobs.Status`, which is exactly the shape `admin.Jobs` must not have — and
// that is the point of the boundary rather than a hole in it. **The status is chosen here, in the
// composition root, by a method whose name is the move**; the port above it has two methods and no
// status parameter, so `internal/admin` cannot name a destination Docs/02 §2 does not offer. A
// private helper on this side of the seam is a shared switch statement, not a widened contract.
func (l disputeLifecycle) resolve(
	ctx context.Context,
	r db.Runner,
	jobID, actorID uuid.UUID,
	to jobs.Status,
	reason string,
) (admin.JobMove, error) {
	_, err := l.jobs.Transition(ctx, r, jobs.Move{
		JobID:  jobID,
		To:     to,
		Actor:  jobs.User(jobs.ActorAdmin, actorID),
		Reason: reason,
	})

	switch {
	case err == nil:
		return admin.JobMoved, nil
	case errors.Is(err, jobs.ErrJobNotFound):
		return admin.JobNotFound, nil
	case errors.Is(err, jobs.ErrAlreadyInStatus):
		return admin.JobAlreadyResolved, nil
	case errors.Is(err, jobs.ErrTransitionNotPermitted):
		return admin.JobNotResolvable, nil
	default:
		return admin.JobMoveUnrecognised, err
	}
}

// actorFor maps the party a complainant was on to the actor kind job_status_history records.
//
// A switch rather than a cast, even though the strings are identical today. They are two closed
// lists owned by two packages — admin.Parties has two values and jobs.ActorTypes has five — and a
// cast would compile for ever after either list changed. This is the one place the correspondence is
// asserted, so it is the one place it can be wrong, and a value with no case is an error rather than
// a silently mistyped actor.
func actorFor(party admin.Party) (jobs.ActorType, error) {
	switch party {
	case admin.PartyCustomer:
		return jobs.ActorCustomer, nil
	case admin.PartyProvider:
		return jobs.ActorProvider, nil
	default:
		return "", fmt.Errorf("cmd/api: %q is not a party a transition can be attributed to", party)
	}
}

// jobPartiesLookup implements admin.JobParties by reading the job and its accepted bid.
//
// # Why this query is here and not in a domain
//
// It spans `jobs` and `bidding`, and `admin` may import neither. The composition root is where a
// dependency between domains is visible to anyone reading how the service is wired, rather than
// buried in `admin/postgres.go` where `jobs` and `bids` would read as tables admin owns.
//
// It is the same call routes_delivery.go's acceptedBids makes, and it will move for the same reason:
// when `bidding` grows a store at SHIP-92, the accepted-bid half of this statement becomes a method
// on it. The customer half belongs to `jobs`, and the join between them belongs here regardless.
//
// # 'Accepted' is the awarded provider, and the index is why that is safe to assume
//
// uq_bids_one_accepted_per_job is partial on `status = 'Accepted'`, so the LEFT JOIN cannot multiply
// rows however many bids a job carries (SHIP-80, SHIP-91). Docs/02 §3 — "awarding a job atomically
// marks one bid accepted and all others closed" — is the statement it enforces.
type jobPartiesLookup struct{}

// PartyOn is which side of the job this account is on, if either.
//
// No lock. The transition that follows in the same transaction takes the job row FOR UPDATE and
// re-reads its status, so a job that moved in the instant between the two statements is refused
// there rather than here.
//
// A job that does not exist and a job the caller has nothing to do with are the same answer, and
// admin turns both into one 404. See admin.JobParties.
//
// **The customer is checked before the provider, and on a job where one account is both, customer
// wins.** Nothing in the platform makes that possible today — ck_users_role fixes the role at
// registration and SHIP-45's trigger keeps it fixed — so the ordering is a decision recorded rather
// than one relied on.
func (jobPartiesLookup) PartyOn(
	ctx context.Context,
	r db.Runner,
	jobID, userID uuid.UUID,
) (admin.Party, bool, error) {
	const q = `
		SELECT CASE
		           WHEN j.customer_id = $2 THEN 'customer'
		           WHEN b.provider_id = $2 THEN 'provider'
		       END
		FROM jobs j
		LEFT JOIN bids b ON b.job_id = j.id AND b.status = 'Accepted'
		WHERE j.id = $1`

	var party *string
	err := r.QueryRow(ctx, q, jobID, userID).Scan(&party)
	switch {
	case errors.Is(err, db.ErrNoRows):
		// No such job. Deliberately the same answer as "not your job" below.
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("cmd/api: reading who is party to %s: %w", jobID, err)
	}
	if party == nil {
		return "", false, nil
	}
	return admin.Party(*party), true, nil
}

// jobMessageLookup implements admin.JobMessages over `internal/bidding`s rows (SHIP-155a).
//
// # Why the query is here rather than in a domain
//
// It reads `job_messages`, which is `internal/bidding`s table (`000506`). `admin` may import that
// domain and the boundary lint refuses it, so the statement lives in the composition root beside
// jobPartiesLookup and exceptionQueueLookup — which is the only place a dependency between two
// domains is visible to somebody reading how the service is assembled.
//
// # What it establishes, and what it deliberately does not
//
// **That the message is on the job, and nothing else.** No foreign key can say so — it is a
// comparison between two tables, and a CHECK may not contain the subquery it would need — which is
// why this port exists at all rather than the schema carrying the rule.
//
// It does **not** check who may read the message. `admin.Reports` calls this only after
// jobPartiesLookup has established the caller as the job's customer or its awarded provider, and on
// this job's rows those are the two accounts a conversation can involve. admin.JobMessages records
// what has to change if a later ticket widens who counts as a party.

type jobMessageLookup struct{}

// MessageOnJob is whether the message belongs to the job.
//
// `SELECT EXISTS` rather than reading the row: the answer is a boolean, the caller has no use for
// the body or the author, and a message a moderator will open is opened by SHIP-156 through its own
// query. A port is what a domain needs rather than what the other domain has.
//
// No lock. Nothing deletes a `job_messages` row — `000506` records that a negotiation stays readable
// after it ends — so a message that exists now exists when the insert lands, and
// `fk_reports_message` is what would refuse it if that ever stopped being true.
func (jobMessageLookup) MessageOnJob(
	ctx context.Context,
	r db.Runner,
	jobID, messageID uuid.UUID,
) (bool, error) {
	const q = `SELECT EXISTS (SELECT 1 FROM job_messages WHERE id = $1 AND job_id = $2)`

	var onJob bool
	if err := r.QueryRow(ctx, q, messageID, jobID).Scan(&onJob); err != nil {
		return false, fmt.Errorf("cmd/api: reading whether %s is a message on %s: %w",
			messageID, jobID, err)
	}
	return onJob, nil
}

// Compile-time proof that the two adapters satisfy the ports admin declared, which is the only place
// in the build where that can be established — admin names neither type and neither type names
// admin, so nothing else links them.
var (
	_ admin.Jobs                  = disputeLifecycle{}
	_ admin.JobParties            = jobPartiesLookup{}
	_ admin.JobMessages           = jobMessageLookup{}
	_ admin.ExceptionQueue        = exceptionQueueLookup{}
	_ admin.CancellationQueue     = cancellationQueueLookup{}
	_ admin.JobDirectory          = jobDirectory{}
	_ admin.ProviderVerifications = providerVerifications{}
	_ admin.ProviderEvidence      = providerEvidence{}
	_ admin.ExpiringDocuments     = expiringDocuments{}
)

// exceptionQueueLookup implements admin.ExceptionQueue over `delivery`s and `jobs`' rows
// (SHIP-117, SHIP-157).
//
// # Why the query is here rather than in a domain
//
// It reads `proofs` and `milestones`, which are `internal/delivery`s, and `jobs`, which is
// `internal/jobs`'. `admin` may import neither, and the boundary lint refuses both. The composition
// root is where a dependency between domains is visible to somebody reading how the service is
// wired, rather than buried in `admin/postgres.go` where `proofs` would read as a table admin owns.
// jobPartiesLookup above is the same arrangement for the same reason.
//
// **`internal/delivery` and `internal/jobs` are untouched by both tickets**, which is the property
// that made SHIP-117 buildable in a wave where another track owned that package and makes SHIP-157
// buildable in one where two other lanes do. The queue is a read of rows those domains already
// write, and nothing about recording an exception, a milestone or a window changes.
//
// # The vocabulary is not translated, and that is deliberate
//
// `exception_reason`, `milestone` and `jobs.status` come back as the strings the database holds.
// Two of them are Docs/02s own values and the third is generated from `contracts/statuses.yaml`
// (SHIP-56a), so a translation here would be a third copy of a list whose whole point is that there
// is one. `delivery.ProofExceptionReason.Wire()` is the identity function on the stored form, and
// Docs/10 §4.7s mapping for the other two belongs to the endpoints that publish them.
//
// **The one vocabulary this file does name is the *ground*, and it is `admin`s own** — see
// [admin.ExceptionGround]. A ground names which of Docs/04 §5s situations a row is evidence of,
// which is a moderation concept rather than a delivery one, and the literals below are the only
// place SQL and Go have to agree about it. TestTheExceptionGroundsAreTheOnesTheDomainDeclares is
// what holds them together.
type exceptionQueueLookup struct{}

// The status sets each window ground selects on.
//
// **Written out rather than expressed as "not yet delivered", because the complement is wrong.** A
// `Cancelled` job is not overdue for a pickup that will never happen, a `Disputed` one is already in
// front of somebody, and a `Draft`, `Open` or `Negotiating` job past its pickup window is SHIP-68s
// expiry sweep rather than a delivery exception — it has no provider to be late. So each set names
// the statuses where a job is *committed and still moving*, and a status added to Docs/02 §1 in
// future is deliberately absent from both until somebody decides which it belongs in.
//
// The two differ by `Picked up` and `In transit`: a job in either has been collected, so it cannot
// be overdue for its pickup and can still be late for its delivery.
//
// They are single-line constants on purpose: internal/admin's copy of this statement is held to it
// by TestTheTestDoubleRunsTheStatementCmdApiRuns, which reads this file and resolves the
// concatenation by name — and a constant split across two string literals is one that guard would
// have to parse rather than read.
const (
	overduePickupStatuses = `('Awarded', 'Driver assigned', 'En route to pickup')`

	delayedDeliveryStatuses = `('Awarded', 'Driver assigned', 'En route to pickup', 'Picked up', 'In transit')`
)

// ExceptionsAwaitingReview reads one page of the queue, oldest first, across all four grounds.
//
// # One statement rather than four reads merged in Go
//
// A `UNION ALL` of four selects with one ordering and one LIMIT over the top. Four reads would need
// a transaction to describe one moment, would each need their own limit chosen without knowing how
// the others would fill the page, and would put the merge in the handler — where a cursor over four
// streams stops being expressible at all.
//
// # The ordering is total, and the cursor is why it needs three columns
//
// `(recorded_at, ground, entry_id)`. Two deliveries recorded in the same millisecond would make a
// single-column cursor either skip an entry or repeat one, and a moderation queue that can hide a
// row is worse than one that shows it twice. **The ground is SHIP-157s addition and it is
// load-bearing**: `overdue_pickup` and `delayed_delivery` are both keyed by the *job*, so a job
// whose pickup and drop-off windows end at the same instant produces two rows agreeing on the other
// two columns.
//
// # The platform's clock throughout, and `now()` is the platform's clock
//
// `p.created_at` and `m.server_recorded_at` rather than `m.actor_recorded_at`, which is the decision
// Docs/02 §3.1 records: the actor's clock is a handset's, it syncs late and it can be wrong, and a
// queue ordered by it could be reordered by a device. The two window grounds compare against
// `now()`, evaluated by PostgreSQL at the start of the transaction — **not the injectable clock**,
// deliberately: `internal/clock` exists so that a *domain rule* can be tested at a chosen instant,
// and this is a read whose entire content is "what is late as of now". A test moves the window, not
// the clock.
//
// # The unsynced threshold is a parameter, never a literal
//
// `$1` is `delivery.UnsyncedAlertThreshold`, handed down from the composition root. Writing
// `interval '24 hours'` here would be a second authority for Docs/02 §3.1s number that agrees with
// the first by comment — and `internal/delivery`s own store already passes it the same way, so the
// predicate here and the predicate there are the same expression over the same value.
//
// # Indexes
//
// `idx_proofs_exception` is partial on `exception_reason IS NOT NULL` and serves the third ground.
// The other three are sequential scans: the unsynced predicate is over a computed difference and
// `internal/delivery`s store already records why no expression index is prepaid for it, and the two
// window grounds are bounded by a status set that is a small fraction of `jobs` at pilot volume.
// **The trigger for adding one is named rather than left to judgement: the first caller that reads
// this on a schedule rather than on a person's request.**
func (exceptionQueueLookup) ExceptionsAwaitingReview(
	ctx context.Context,
	r db.Runner,
	q admin.QueueQuery,
	unsyncedThreshold time.Duration,
) ([]admin.ExceptionEntry, error) {
	const query = `
		WITH entries AS (
		    -- Docs/04 §5, ground one: committed, past its pickup window, not collected.
		    SELECT '` + string(admin.GroundOverduePickup) + `'::text AS ground,
		           j.id AS entry_id, j.id AS job_id,
		           ''::text AS milestone, ''::text AS reason, ''::text AS note,
		           j.pickup_window_end AS recorded_at
		    FROM jobs j
		    WHERE j.pickup_window_end IS NOT NULL
		      AND j.pickup_window_end < now()
		      AND j.status IN ` + overduePickupStatuses + `

		    UNION ALL

		    -- Ground two: past its drop-off window and not delivered.
		    SELECT '` + string(admin.GroundDelayedDelivery) + `'::text,
		           j.id, j.id, ''::text, ''::text, ''::text, j.dropoff_window_end
		    FROM jobs j
		    WHERE j.dropoff_window_end IS NOT NULL
		      AND j.dropoff_window_end < now()
		      AND j.status IN ` + delayedDeliveryStatuses + `

		    UNION ALL

		    -- Ground three (SHIP-117): evidenced by a reason rather than a photograph.
		    --
		    -- The join does not filter on the milestone kind. Docs/01 §4.4 is about delivery and
		    -- proofs does not restrict itself to one milestone, so filtering to 'Delivered'
		    -- would silently drop an exception recorded against a pickup the day somebody allows
		    -- one. The milestone is reported instead.
		    SELECT '` + string(admin.GroundFailedProof) + `'::text,
		           p.id, p.job_id, m.milestone, p.exception_reason, coalesce(m.reason, ''),
		           p.created_at
		    FROM proofs p
		    JOIN milestones m ON m.id = p.milestone_id
		    WHERE p.exception_reason IS NOT NULL

		    UNION ALL

		    -- Ground four (SHIP-128, surfaced here by SHIP-157): an update that reached the
		    -- platform more than $1 after the driver recorded it. >= rather than > because
		    -- Docs/02 §3.1 says "24 hours — operations alert", so a row exactly on the threshold
		    -- is over it; PostgreSQL compares intervals exactly, so this is a real boundary.
		    SELECT '` + string(admin.GroundUnsyncedMilestone) + `'::text,
		           m.id, m.job_id, m.milestone, ''::text, coalesce(m.reason, ''),
		           m.server_recorded_at
		    FROM milestones m
		    WHERE m.server_recorded_at - m.actor_recorded_at >= $1
		)
		SELECT e.ground, e.entry_id, e.job_id, e.milestone, e.reason, e.note,
		       e.recorded_at, j.status
		FROM entries e
		JOIN jobs j ON j.id = e.job_id
		WHERE ($2 = '' OR e.ground = $2)
		  AND ($3::timestamptz IS NULL
		       OR (e.recorded_at, e.ground, e.entry_id) > ($3, $4, $5))
		ORDER BY e.recorded_at, e.ground, e.entry_id
		LIMIT $6`

	// A nil rather than a zero time for the first page: `> (NULL, …)` is NULL rather than true,
	// so the predicate has to be skipped rather than satisfied, and the `$3 IS NULL` guard above
	// is what does it. Passing the zero time would work today and would stop working the first
	// time somebody backdated a fixture.
	var (
		after   any
		ground  any
		afterID any
	)
	if !q.After.Zero() {
		after, ground, afterID = q.After.RecordedAt, q.After.Ground.String(), q.After.EntryID
	}

	rows, err := r.Query(ctx, query,
		unsyncedThreshold, q.Ground.String(), after, ground, afterID, q.Limit)
	if err != nil {
		return nil, fmt.Errorf("cmd/api: reading the delivery-exception queue: %w", err)
	}
	defer rows.Close()

	var out []admin.ExceptionEntry
	for rows.Next() {
		var e admin.ExceptionEntry
		if err := rows.Scan(&e.Ground, &e.EntryID, &e.JobID, &e.Milestone, &e.Reason,
			&e.Note, &e.RecordedAt, &e.JobStatus); err != nil {
			return nil, fmt.Errorf("cmd/api: reading a delivery-exception entry: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cmd/api: reading the delivery-exception queue: %w", err)
	}
	return out, nil
}

// --- SHIP-158: the post-award cancellation queue --------------------------------------------------

// cancellationQueueLookup implements admin.CancellationQueue over `jobs`, `job_status_history` and
// `bids`.
//
// Here rather than in a domain for the reason exceptionQueueLookup and jobDirectory are: the
// statement spans two other domains' tables, `admin` may import neither, and the composition root is
// where a dependency between domains is visible to somebody reading how the service is wired.
type cancellationQueueLookup struct{}

// providerCommitmentStatuses is where a job stands once a provider has undertaken to carry it.
//
// Docs/02 §2 permits `Awarded / Driver assigned → Open` and nothing later: after `Picked up` the
// goods are in somebody's vehicle and §6.2 makes ending it a support case rather than a transition.
// So the set is exactly the two statuses from which a provider may still walk away.
//
// Single-line, like the exception queue's sets, so internal/admin's copy of this statement can be
// held to it by a guard that resolves the constant by name.
const providerCommitmentStatuses = `('Awarded', 'Driver assigned')`

// CancelledAfterAward reads one page of the queue, most recent first.
//
// # "After award" is a fact about the history, and Docs/02 §2 is why
//
// **There is no `Awarded → Cancelled` transition.** The guard refuses it, and the document is
// deliberate about that: `Open / Negotiating → Cancelled` is the ordinary route out of the
// marketplace, and after award a job either goes back on the market or ends through a dispute. A
// queue that read `to_status = 'Cancelled'` and stopped there would therefore return the *pre*-award
// cancellations and none of the post-award ones — which is the exact inverse of this ticket.
//
// So the queue reads two shapes, and an entry says which:
//
//   - **`returned_to_market`** — `Awarded / Driver assigned → Open`. Docs/02 §6.2 calls this a
//     provider cancellation after award, closes every bid on the job and says the cancellation "is
//     **recorded against the provider**… the reliability signal that Phase 2 reputation will be
//     built from, and it cannot be reconstructed later if it is not captured now". This queue is
//     where it is read.
//   - **`ended`** — a transition into `Cancelled` on a job that had earlier reached `Awarded`, which
//     under Docs/02 §2 can only arrive through `Disputed → Cancelled`: an administrator resolving a
//     dispute as a cancelled or failed delivery.
//
// The earlier-award test is `(server_recorded_at, id)` rather than the timestamp alone, because one
// transaction can write two history rows at the same instant and `now()` is transaction start time.
//
// # Neither shape has an endpoint yet, and the queue is still correct
//
// Nothing in the platform currently drives either transition: provider cancellation has no ticket,
// and dispute resolution is SHIP-164. **The queue is a query over rows the guard already permits**,
// so it fills the moment either arrives and needs no change when it does — which is the same
// position `admin.ExceptionQueue` takes and the reason neither has a flag column. What it must not
// do in the meantime is quietly list pre-award cancellations so that the screen looks populated.
//
// # The provider comes from the accepted bid, and there is a named gap
//
// `uq_bids_one_accepted_per_job` makes that join single-valued. **On the `returned_to_market` shape
// the accepted bid is closed by the same operation** (Docs/02 §6.2: "all prior bids are closed, not
// restored"), so once that path is built the join will find nothing and the entry will report no
// provider. Closing that needs `bids` to record which offer *was* accepted after it stops being
// accepted — a column in `internal/bidding`'s migration block, which is not this ticket's to take.
// It is written up in Docs/11 §4 rather than papered over with a guess.
//
// # The two provider counts are aggregates over the whole table, deliberately
//
// `history` and `completions` are computed once per query rather than per row. That is more work
// than a correlated subquery on a small page and far less on a large one, and it keeps the counts
// consistent with each other. **The trigger for revisiting it is named**: the first time this
// endpoint is read on a schedule rather than on a person's request, or the first `bids` table where
// a full aggregate is not free — at which point the shape to add is a partial index on
// `bids (provider_id) WHERE status = 'Accepted'`.
func (cancellationQueueLookup) CancelledAfterAward(
	ctx context.Context,
	r db.Runner,
	q admin.QueueQuery,
) ([]admin.CancellationEntry, error) {
	const query = `
		WITH cancellations AS (
		    SELECT h.job_id, h.id AS transition_id, h.server_recorded_at AS cancelled_at,
		           h.from_status, h.actor_type, coalesce(h.reason, '') AS reason,
		           CASE WHEN h.to_status = 'Open'
		                THEN '` + string(admin.OutcomeReturnedToMarket) + `'
		                ELSE '` + string(admin.OutcomeEnded) + `' END AS outcome
		    FROM job_status_history h
		    WHERE (h.to_status = 'Open' AND h.from_status IN ` + providerCommitmentStatuses + `)
		       OR (h.to_status = 'Cancelled'
		           AND EXISTS (SELECT 1
		                         FROM job_status_history a
		                        WHERE a.job_id = h.job_id
		                          AND a.to_status = 'Awarded'
		                          AND (a.server_recorded_at, a.id) < (h.server_recorded_at, h.id)))
		),
		carrier AS (
		    SELECT c.transition_id, b.provider_id
		    FROM cancellations c
		    LEFT JOIN bids b ON b.job_id = c.job_id AND b.status = 'Accepted'
		),
		walked AS (
		    SELECT provider_id, count(*) AS cancellations
		    FROM carrier
		    WHERE provider_id IS NOT NULL
		    GROUP BY provider_id
		),
		completions AS (
		    SELECT b.provider_id, count(*) AS completions
		    FROM bids b
		    JOIN jobs j ON j.id = b.job_id
		    WHERE b.status = 'Accepted' AND j.status = 'Completed'
		    GROUP BY b.provider_id
		)
		SELECT c.job_id, c.transition_id,
		       coalesce(r.provider_id, '00000000-0000-0000-0000-000000000000'::uuid),
		       c.outcome, c.from_status, c.actor_type, c.reason, c.cancelled_at,
		       coalesce(w.cancellations, 0), coalesce(m.completions, 0)
		FROM cancellations c
		JOIN carrier r      ON r.transition_id = c.transition_id
		LEFT JOIN walked w      ON w.provider_id = r.provider_id
		LEFT JOIN completions m ON m.provider_id = r.provider_id
		WHERE ($1::timestamptz IS NULL OR (c.cancelled_at, c.transition_id) < ($1, $2))
		ORDER BY c.cancelled_at DESC, c.transition_id DESC
		LIMIT $3`

	// A nil rather than a zero time for the first page: `< (NULL, …)` is NULL rather than true,
	// so the predicate has to be skipped rather than satisfied. The comparison is `<` because
	// the order is descending — the next page is what happened *before* this one's last row.
	var (
		after   any
		afterID any
	)
	if !q.After.Zero() {
		after, afterID = q.After.RecordedAt, q.After.EntryID
	}

	rows, err := r.Query(ctx, query, after, afterID, q.Limit)
	if err != nil {
		return nil, fmt.Errorf("cmd/api: reading the post-award cancellation queue: %w", err)
	}
	defer rows.Close()

	var out []admin.CancellationEntry
	for rows.Next() {
		var e admin.CancellationEntry
		if err := rows.Scan(&e.JobID, &e.TransitionID, &e.ProviderID, &e.Outcome,
			&e.FromStatus, &e.ActorType, &e.Reason, &e.CancelledAt,
			&e.ProviderCancellations, &e.ProviderCompletions); err != nil {
			return nil, fmt.Errorf("cmd/api: reading a cancellation entry: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cmd/api: reading the post-award cancellation queue: %w", err)
	}
	return out, nil
}

// --- SHIP-152: the administrator's view of jobs, bids and status history ---------------------------

// jobDirectory implements admin.JobDirectory over `jobs`, `bids` and `job_status_history`.
//
// # Why the statements are here and not in a domain
//
// They span `internal/jobs`' two tables and `internal/bidding`'s one, and `admin` may import
// neither — the boundary lint refuses both directions. The composition root is where a dependency
// between domains is visible to somebody reading how the service is wired, rather than buried in
// `admin/postgres.go` where `jobs` and `bids` would read as tables that domain owns.
// jobPartiesLookup and exceptionQueueLookup are the same arrangement for the same reason, and
// postgres_users.go records the line: a statement spanning **two other domains'** tables belongs
// here, and one spanning a single shared table (`users`) belongs in the domain that reads it.
//
// # This will move, and the trigger is named
//
// When `bidding` grows a store at SHIP-92 the bid half becomes a method on it, and the job half
// belongs to `jobs` — exactly as acceptedBids and jobPartiesLookup will. What stays here is the
// composition: three reads assembled into one administrator-facing shape.
//
// # No budget column is selected, in either statement
//
// `jobs.budget` (`000405`) is not in either column list and there is nowhere on admin.JobRecord to
// put it. Docs/01 §4.3's invariant names *providers*, so an administrator is not the audience it
// protects the number from — this is a decision rather than a rule, argued in
// internal/admin/jobsearch.go, and made structural here so that a later change to the response shape
// cannot quietly acquire one. SHIP-164 is the named revisit.
type jobDirectory struct{}

// adminJobColumns is the job facts an administrator's list and detail view both carry.
//
// One constant rather than the same list written twice, so that a column added to the shape cannot
// be added to one statement and forgotten in the other — the scan is shared too, which turns that
// into a compile-time mismatch rather than a screen with an empty field on one route.
//
// The bid count is a correlated subquery rather than a `LEFT JOIN … GROUP BY`. It counts **every**
// bid in every status, which is the question a list answers: whether anybody engaged with the job at
// all, where three withdrawn offers and no offers are very different facts.
const adminJobColumns = `j.id, j.customer_id, j.status, coalesce(j.goods_description, ''),
	(SELECT count(*) FROM bids b WHERE b.job_id = j.id),
	j.expires_at, j.created_at, j.updated_at`

// SearchJobs returns one page of jobs, newest first.
//
// # The predicate
//
// Three optional filters, each written `$n = ” OR column = $n` rather than assembled by
// concatenation: one statement means one plan and one place the disclosure rules can be read, and
// building SQL from a query string is the injection parameterisation exists to make impossible.
//
// **The term is escaped before it becomes a LIKE pattern**, by admin.LikePattern — a bare `%` would
// otherwise return every job in the marketplace to the least-privileged role from one character in a
// search box. `goods_description` is nullable, and `NULL ILIKE …` is NULL rather than false, so a
// draft that never reached the details step simply does not match a term. That is the right answer:
// it has no description to match.
//
// # The ordering is total
//
// `(created_at DESC, id DESC)`, because `created_at` is not unique and a single-column cursor over a
// non-unique key either skips a row or repeats one. `<` rather than `>` because the order is
// descending: the next page is what was created *before* the last row of this one.
func (jobDirectory) SearchJobs(
	ctx context.Context,
	r db.Runner,
	q admin.JobQuery,
) ([]admin.JobRecord, error) {
	const query = `
		SELECT ` + adminJobColumns + `
		FROM jobs j
		WHERE ($1 = '' OR j.goods_description ILIKE $2)
		  AND ($3 = '' OR j.status = $3)
		  AND ($4::uuid IS NULL OR j.customer_id = $4)
		  AND ($5::timestamptz IS NULL OR (j.created_at, j.id) < ($5, $6))
		ORDER BY j.created_at DESC, j.id DESC
		LIMIT $7`

	// A nil rather than a zero value for every absent bound. `< (NULL, …)` is NULL rather than
	// true, so the predicate has to be skipped rather than satisfied — the trap every cursor in
	// this service records, and the one that would work today and stop working the first time
	// somebody backdated a fixture.
	var after, afterID any
	if !q.After.Zero() {
		after, afterID = q.After.CreatedAt, q.After.JobID
	}

	var customer any
	if q.CustomerID != uuid.Nil {
		customer = q.CustomerID
	}

	rows, err := r.Query(ctx, query,
		q.Term, admin.LikePattern(q.Term), q.Status, customer, after, afterID, q.Limit)
	if err != nil {
		return nil, fmt.Errorf("cmd/api: searching jobs: %w", err)
	}
	defer rows.Close()

	var out []admin.JobRecord
	for rows.Next() {
		record, err := scanAdminJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cmd/api: searching jobs: %w", err)
	}
	return out, nil
}

// OpenJob is one job with every bid and every recorded transition.
//
// Three statements, run inside the transaction admin.JobConsole.Open opens — so the header, the
// offers and the history are one snapshot. A console that rendered an `Awarded` job beside a bid
// list with nothing accepted would be worse than a stale one, because somebody acts on it.
//
// No locks. It is read-only, and a job that moves during the three statements is simply the next
// page load.
func (jobDirectory) OpenJob(
	ctx context.Context,
	r db.Runner,
	jobID uuid.UUID,
) (admin.JobDetail, bool, error) {
	const jobQuery = `SELECT ` + adminJobColumns + ` FROM jobs j WHERE j.id = $1`

	var detail admin.JobDetail
	record, err := scanAdminJob(r.QueryRow(ctx, jobQuery, jobID))
	switch {
	case errors.Is(err, db.ErrNoRows):
		// No such job. Unlike jobPartiesLookup's identical-looking answer this is not a
		// disclosure decision: the caller is an administrator holding `jobs.read` and every
		// job is theirs to open.
		return admin.JobDetail{}, false, nil
	case err != nil:
		return admin.JobDetail{}, false, err
	}
	detail.Job = record

	if detail.Bids, err = adminBidsFor(ctx, r, jobID); err != nil {
		return admin.JobDetail{}, false, err
	}
	if detail.History, err = adminHistoryFor(ctx, r, jobID); err != nil {
		return admin.JobDetail{}, false, err
	}
	return detail, true, nil
}

// adminBidsFor is every offer on the job, oldest first.
//
// **Every offer, in every status**, which is what Docs/02 §4's "bid history remains visible to the
// customer, bidding provider, and administrators" asks for — and it is the third reader SHIP-96
// enumerated and could not serve, because there was no administrator to serve it to. Unlike the two
// views that ticket built, this one is not scoped to one provider's chain: an administrator looking
// at a disputed award needs the offers it was chosen over.
//
// The amount is converted in SQL, `(amount * 100)::bigint`, matching internal/bidding's own store.
// COALESCE to 0 is unambiguous because `ck_bids_amount` refuses a non-positive amount, so 0 and NULL
// cannot be confused in either direction.
//
// Ordered by `(created_at, id)` and not by the supersession chain: a chain is a rendering, and two
// offers written in the same millisecond would make an ordering that followed it non-deterministic.
func adminBidsFor(ctx context.Context, r db.Runner, jobID uuid.UUID) ([]admin.BidRecord, error) {
	const query = `
		SELECT id, provider_id, status, offered_by,
		       coalesce((amount * 100)::bigint, 0),
		       pickup_at, deliver_by, coalesce(message, ''), superseded_by,
		       created_at, updated_at
		FROM bids
		WHERE job_id = $1
		ORDER BY created_at, id`

	rows, err := r.Query(ctx, query, jobID)
	if err != nil {
		return nil, fmt.Errorf("cmd/api: reading the bids on %s: %w", jobID, err)
	}
	defer rows.Close()

	out := []admin.BidRecord{}
	for rows.Next() {
		var (
			b                   admin.BidRecord
			pickupAt, deliverBy *time.Time
			supersededBy        *uuid.UUID
		)
		if err := rows.Scan(&b.ID, &b.ProviderID, &b.Status, &b.OfferedBy, &b.AmountCents,
			&pickupAt, &deliverBy, &b.Message, &supersededBy,
			&b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, fmt.Errorf("cmd/api: reading a bid on %s: %w", jobID, err)
		}

		// NULL is "the row carries none", which the zero value says just as well — so there is
		// one representation of absence rather than a pointer every reader has to check.
		if pickupAt != nil {
			b.PickupAt = *pickupAt
		}
		if deliverBy != nil {
			b.DeliverBy = *deliverBy
		}
		if supersededBy != nil {
			b.SupersededBy = *supersededBy
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cmd/api: reading the bids on %s: %w", jobID, err)
	}
	return out, nil
}

// adminHistoryFor is every recorded transition on the job, oldest first.
//
// Ordered by `(server_recorded_at, id)` — the **platform's** clock rather than the actor's.
// Docs/02 §3.1 keeps the two apart because a driver records a milestone out of signal and the device
// syncs later; a timeline ordered by the device's clock could be reordered by a handset with the
// wrong time, which for a support screen means an update appearing before the one it followed.
// Both clocks are carried, so a reader can see the gap and reason about it, which is the whole point
// of there being two.
func adminHistoryFor(ctx context.Context, r db.Runner, jobID uuid.UUID) ([]admin.StatusEvent, error) {
	const query = `
		SELECT id, from_status, to_status, actor_type, actor_id, coalesce(reason, ''),
		       actor_recorded_at, server_recorded_at
		FROM job_status_history
		WHERE job_id = $1
		ORDER BY server_recorded_at, id`

	rows, err := r.Query(ctx, query, jobID)
	if err != nil {
		return nil, fmt.Errorf("cmd/api: reading the status history of %s: %w", jobID, err)
	}
	defer rows.Close()

	out := []admin.StatusEvent{}
	for rows.Next() {
		var (
			e       admin.StatusEvent
			actorID *uuid.UUID
		)
		if err := rows.Scan(&e.ID, &e.From, &e.To, &e.ActorType, &actorID, &e.Reason,
			&e.ActorRecordedAt, &e.ServerRecordedAt); err != nil {
			return nil, fmt.Errorf("cmd/api: reading a transition of %s: %w", jobID, err)
		}

		// NULL is the platform acting as itself, which `000401` requires to have no account.
		if actorID != nil {
			e.ActorID = *actorID
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cmd/api: reading the status history of %s: %w", jobID, err)
	}
	return out, nil
}

// scanAdminJob reads one row of [adminJobColumns].
//
// One function rather than the Scan written at each call site, matching admin's own scanUser: a
// column added to the list and not here is a scan mismatch at the first call, which is the failure
// worth having.
func scanAdminJob(row interface{ Scan(...any) error }) (admin.JobRecord, error) {
	var (
		j         admin.JobRecord
		expiresAt *time.Time
	)

	if err := row.Scan(&j.ID, &j.CustomerID, &j.Status, &j.GoodsDescription, &j.BidCount,
		&expiresAt, &j.CreatedAt, &j.UpdatedAt); err != nil {
		return admin.JobRecord{}, err
	}
	if expiresAt != nil {
		j.ExpiresAt = *expiresAt
	}
	return j, nil
}

// --- SHIP-153, SHIP-154: the provider verification queue and its decision -------------------------

// providerVerifications implements admin.ProviderVerifications over `internal/profiles`.
//
// # An adapter over a domain service, not a statement written here
//
// This is disputeLifecycle's shape rather than jobDirectory's, and the line between them is one
// postgres_users.go already drew: a query spanning **two other domains'** tables belongs in the
// composition root, because that is the only place both are visible; a query over one domain's own
// tables belongs to that domain. `provider_verifications` and `provider_verification_decisions` are
// `internal/profiles`', and the reads and the transition are methods on its service.
//
// What lives here is the translation, and there are three of them:
//
//   - **the page shapes.** `profiles.QueueEntry` and `admin.VerificationEntry` are two packages'
//     own types with the same fields, and neither may name the other's — the boundary lint refuses
//     both directions. Go satisfies the interface structurally, so this file is the only thing in
//     the build that knows the two correspond.
//   - **the refusals.** `admin` cannot call errors.Is against `profiles`' sentinels, so
//     ErrNoSuchProvider and ErrAlreadyInState become an admin.VerificationMove with a nil error.
//     Everything else stays an error, because a failing database is not an answer.
//   - **the actor.** `profiles.Actor` distinguishes an administrator from the platform acting as
//     itself, and `admin` supplies only the first. **This comment used to say SHIP-159's expiry
//     sweep would supply the second, and SHIP-159 built no sweep** — deliberately: moving a
//     provider to `Restricted` because a date passed would be the platform enforcing Docs/04 §3's
//     renewal cadence, which is Track-X row X-4 and is unanswered. That ticket surfaces expiring
//     documents on a queue and a person decides. Nothing supplies `ActorSystem` today.
//
// # Why the domain service is built here rather than shared with profilesHandler
//
// `profiles.NewService` takes the clock and holds nothing else: no pool, no connection, no state. So
// a second instance is a struct with one field, and passing one between two route files would be a
// shared surface built for no gain — profilesHandler builds its own for exactly the same reason.
type providerVerifications struct {
	svc *profiles.Service
}

// VerificationsAwaitingReview is one page of the queue, oldest first.
//
// The Runner is the caller's — a pool, because a queue is a read that owns no invariant and opens no
// transaction (admin.Verifications.AwaitingReview records why).
//
// An unrecognised state cannot reach here: admin.Verifications validates against the list this file
// handed it, built from profiles.States. The domain refuses one anyway, which is the layer that
// survives somebody calling the service directly.
func (p providerVerifications) VerificationsAwaitingReview(
	ctx context.Context,
	r db.Runner,
	q admin.VerificationQuery,
) ([]admin.VerificationEntry, error) {

	found, err := p.svc.AwaitingReview(ctx, r, profiles.QueueQuery{
		State: profiles.State(q.State),
		Limit: q.Limit,
		After: profiles.QueueCursor{
			SubmittedAt: q.After.SubmittedAt,
			ProviderID:  q.After.ProviderID,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("cmd/api: reading the %s verification queue: %w", q.State, err)
	}

	out := make([]admin.VerificationEntry, 0, len(found))
	for _, e := range found {
		out = append(out, admin.VerificationEntry{
			ProviderID:  e.ProviderID,
			Name:        e.Name,
			Email:       e.Email,
			Phone:       e.Phone,
			State:       string(e.State),
			SubmittedAt: e.SubmittedAt,
		})
	}
	return out, nil
}

// DecideVerification runs `profiles`' one guarded transition inside the caller's transaction.
//
// # The Runner must be a transaction and this method does not check that
//
// `profiles.Service.Decide` refuses a pool with its own sentinel, and `provider_verification_decide`
// would refuse one underneath that — the transaction-local setting the trigger reads lasts only for
// the statement that set it outside a transaction. A third check here would be a third opinion about
// a rule two layers already hold. admin.Verifications.Decide opens the transaction, which is where
// the audit entry joins it.
//
// # A refusal is an outcome and a failure is an error
//
// The same split disputeLifecycle takes. profiles.ErrNoSuchProvider and profiles.ErrNotProvider are
// one outcome, because `000200` gives every provider a record at registration and an account with no
// record is either not a provider or not an account — indistinguishable from the record's side, and
// the same answer to whoever asked.
//
// The validation failures — a state Docs/04 §4 does not have, a reason that is blank or a novel —
// stay errors deliberately: `admin` has already refused both a layer earlier against a stricter
// bound, so one arriving here is a defect in this translation rather than something a caller did.
func (p providerVerifications) DecideVerification(
	ctx context.Context,
	r db.Runner,
	d admin.VerificationDecision,
) (admin.VerificationMove, admin.VerificationChange, error) {

	decision, err := p.svc.Decide(ctx, r, d.ProviderID, profiles.State(d.To),
		profiles.Actor{Type: profiles.ActorAdmin, ID: d.ActorID}, d.Reason)

	switch {
	case err == nil:
		return admin.VerificationDecided, admin.VerificationChange{
			ProviderID: d.ProviderID,
			From:       string(decision.From),
			To:         string(decision.Verification.State),
		}, nil

	case errors.Is(err, profiles.ErrNoSuchProvider), errors.Is(err, profiles.ErrNotProvider):
		return admin.VerificationProviderNotFound, admin.VerificationChange{}, nil

	case errors.Is(err, profiles.ErrAlreadyInState):
		return admin.VerificationAlreadyInState, admin.VerificationChange{}, nil

	default:
		return admin.VerificationMoveUnrecognised, admin.VerificationChange{}, err
	}
}

// --- SHIP-155: the document viewer for private evidence ------------------------------------------

// providerEvidence implements admin.ProviderEvidence over `internal/profiles`.
//
// # The capability, not the service, is what crosses
//
// `internal/profiles`' documents.go said in advance what this adapter would be: *"SHIP-155 composes
// with this rather than against it. The administrator's document viewer constructs its own
// [Documents] from `cmd/api/routes_admin.go`, hands it the same two ports, and reaches the reader
// below — it does not need a wider [Service]."* That is what happens here, and the property it buys
// is worth stating rather than assuming: `profiles.NewService` is untouched, so the administrator's
// queue and decision endpoints still hold no signer, and this adapter holds a signer and cannot move
// a verification state.
//
// The `*profiles.Documents` is built from `profileDocuments(d)` — the same `*storage.S3` value
// routes_profiles.go builds for the provider's own endpoints, satisfying the same two ports. One
// deployment, one bucket, one credential; the two kinds of object are kept apart by their key
// prefix, and the two *readers* are kept apart by which credential reached them.
//
// # The translation, and there are two of them
//
//   - **the shapes.** `profiles.DocumentLink` and `admin.EvidenceDocument` are two packages' own
//     types and neither may name the other's. Go satisfies the interface structurally, so this file
//     is the only thing in the build that knows the two correspond.
//   - **the refusal.** `admin` cannot call errors.Is against `profiles`' sentinels, so a caller who
//     is not a provider account becomes found=false with a nil error. Everything else stays an
//     error, because a failing database is not an answer. That is [providerVerifications]' division
//     and [admin.ProviderEvidence] records the same reasoning from the other side.
//
// # Why the documents service is built per request rather than once
//
// It is built inside `adminHandler`, which `attach` calls once per route at startup — the same place
// `profilesHandler` builds its own. The value holds configuration and a signer and no connection, so
// a second is a struct rather than a resource, and threading one between two route files would be a
// field on `Deps` that two domains would then both have to agree about.
type providerEvidence struct {
	documents *profiles.Documents
}

// EvidenceFor is every document a provider has submitted, each with a URL minted on this call.
//
// The Runner is the caller's, and here it is a *transaction*: admin.Evidence.For opens one so that
// the read and the audit entry recording it commit together. That is safe to hold across this call —
// `profiles.Documents.For` presigns, and presigning is an HMAC over a string rather than a request
// to the store. The one method in that package that does reach the network is `Submit`'s `Stored`,
// and nothing here calls it.
//
// A caller who is not a provider account — no such account, or a customer — is found=false. Both
// come back from `profiles` as ErrNotProvider, which is that domain's single answer to the same two
// conditions for the same reason `000200` makes them indistinguishable.
func (p providerEvidence) EvidenceFor(
	ctx context.Context,
	r db.Runner,
	providerID uuid.UUID,
) ([]admin.EvidenceDocument, bool, error) {

	links, err := p.documents.For(ctx, r, providerID)
	switch {
	case errors.Is(err, profiles.ErrNotProvider), errors.Is(err, profiles.ErrNoSuchProvider):
		return nil, false, nil
	case err != nil:
		return nil, false, fmt.Errorf("cmd/api: reading the verification evidence of %s: %w",
			providerID, err)
	}

	out := make([]admin.EvidenceDocument, 0, len(links))
	for _, link := range links {
		out = append(out, admin.EvidenceDocument{
			ID:            link.ID,
			Kind:          string(link.Kind),
			ContentType:   link.ContentType,
			ContentLength: link.ContentLength,
			ETag:          link.ETag,
			SubmittedAt:   link.SubmittedAt,
			URL:           link.URL,
			ExpiresAt:     link.URLExpiresAt,
		})
	}
	return out, true, nil
}

// --- SHIP-159: Docs/04 §5's expiry queue ---------------------------------------------------------

// verificationLeadTimes turns the configured horizons into this domain's own vocabulary.
//
// `internal/config` keys them by string because it is infrastructure and may not import a domain
// (SHIP-15c); `internal/profiles` keys them by [profiles.Kind] because that is the closed list held
// to `ck_provider_verification_documents_kind` by a test in both directions. **This is the only
// place in the build that knows the two correspond**, which is the same arrangement the verification
// states are already under one function up.
//
// A kind the platform does not have is *not* dropped here. It is passed through and refused by
// `profiles.NewExpiry`, which panics at startup naming the value — because a typo silently ignored
// is configuration set in staging that changes nothing, and the obvious conclusion is that the
// feature is broken rather than that the name was wrong.
func verificationLeadTimes(d Deps) map[profiles.Kind]time.Duration {
	out := make(map[profiles.Kind]time.Duration, len(d.Config.Verification.ExpiryLeadTimes))
	for kind, ahead := range d.Config.Verification.ExpiryLeadTimes {
		out[profiles.Kind(kind)] = ahead
	}
	return out
}

// expiringDocuments implements admin.ExpiringDocuments over `internal/profiles`.
//
// # It holds an expiry reader and no signer, which is the property the port exists for
//
// `profiles.Expiry` cannot mint a download URL — it takes neither of the two object-store ports —
// so a console listing whose insurance has lapsed cannot thereby read anybody's insurance. The
// viewer that can (SHIP-155) writes an access entry on every read; this queue correctly writes
// nothing, and the reason those two facts are consistent is that they are two types.
//
// # The translation, and there are two of them
//
//   - **the shapes.** `profiles.ExpiringDocument` and `admin.ExpiringDocumentEntry` are two
//     packages' own types and neither may name the other's. Go satisfies the interface structurally,
//     so this file is the only thing in the build that knows they correspond.
//   - **the vocabulary.** The document kind and the verification state are closed types in
//     `profiles` and plain strings in `admin`, which is [providerVerifications]' division and the
//     reason there is no copy of either list in the console.
//
// There is no refusal to translate. Everything this read can fail at is a failing database, and a
// failing database is not an answer.
type expiringDocuments struct {
	expiry *profiles.Expiry
}

// DocumentsNearingExpiry is one page of current documents at or past their horizon, soonest first.
//
// The Runner is the caller's — a pool, because a queue is a read that owns no invariant and opens no
// transaction (admin.ExpiryQueue.Due records why).
func (e expiringDocuments) DocumentsNearingExpiry(
	ctx context.Context,
	r db.Runner,
	q admin.DocumentExpiryQuery,
) ([]admin.ExpiringDocumentEntry, error) {

	found, err := e.expiry.Due(ctx, r, profiles.ExpiryQuery{
		Limit: q.Limit,
		After: profiles.ExpiryCursor{
			ExpiresAt:  q.After.ExpiresAt,
			DocumentID: q.After.DocumentID,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("cmd/api: reading the document expiry queue: %w", err)
	}

	out := make([]admin.ExpiringDocumentEntry, 0, len(found))
	for _, d := range found {
		out = append(out, admin.ExpiringDocumentEntry{
			DocumentID:        d.DocumentID,
			ProviderID:        d.ProviderID,
			Name:              d.Name,
			Email:             d.Email,
			Phone:             d.Phone,
			VerificationState: string(d.VerificationState),
			Kind:              string(d.Kind),
			ExpiresAt:         d.ExpiresAt,
			Expired:           d.Expired,
			SubmittedAt:       d.SubmittedAt,
		})
	}
	return out, nil
}

// ExpiryLeadTimes is the configured horizon per document kind, keyed by the stored spelling.
//
// Read back off the domain rather than off `Deps`, deliberately: it is the same map the query was
// answered with, so the console cannot be shown a horizon the queue did not use. Two reads of
// `internal/config` would be two copies of one fact that could disagree after a refactor, and the
// disagreement would be invisible — a page of entries beside a legend that explains a different
// page.
func (e expiringDocuments) ExpiryLeadTimes() map[string]time.Duration {
	held := e.expiry.LeadTimes()

	out := make(map[string]time.Duration, len(held))
	for kind, ahead := range held {
		out[string(kind)] = ahead
	}
	return out
}
