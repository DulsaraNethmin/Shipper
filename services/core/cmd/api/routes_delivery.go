package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/delivery"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/platform/storage"
)

// The delivery domain's routes (SHIP-106 onwards).
//
// This file exists so that adding a domain adds a file and edits none. cmd/api/routes.go,
// manifest.go and main.go are shared surfaces (Docs/10 §9.2); a route registration that had to go
// into one of them is a line every concurrent branch also touches, and a badly resolved conflict
// there drops an endpoint with no compile error and no failing test.
//
// # This is also where three domains meet, which is the only place they may
//
// `delivery` declares two ports and imports neither `jobs` nor `bidding` (Docs/06 §4.1). The
// implementations are below, and they are the reason this file is longer than routes_jobs.go: the
// composition root is doing the one thing only it can do, which is knowing about more than one
// domain at a time.
//
// # The route is under /jobs and the handler is in delivery, and that is not a contradiction
//
// A URL is a client's map of the product, not a diagram of the packages behind it. Assigning a
// driver is something a client does *to a job*, so it lives under the job — the same reading that
// puts SHIP-111's milestones there. What decides which domain serves it is who owns the rule, and
// the rule here is delivery's: one live driver per job, and a status move that only the awarded
// provider may ask for.
func init() {
	register(
		Route{
			Method:  http.MethodPost,
			Pattern: "/jobs/{id}/driver",
			Group:   GroupV1,

			// RequireUser, and deliberately **not** RequireDriverToken. The caller is the
			// provider, authenticated the ordinary way. The driver's job-scoped token is a
			// separate system that grants exactly one job and cannot be exchanged for a
			// session in either direction (Docs/10 §5).
			//
			// **This route mints one (SHIP-107) and still does not accept one**, which is
			// exactly the right pair: the provider is handed the link to forward, and the
			// route that spends it is the third one below (SHIP-108).
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return deliveryHandler(d).AssignDriver() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/jobs/{id}/driver/link",
			Group:   GroupV1,

			// SHIP-109: another link for the driver already on the job, ending the previous one.
			//
			// RequireUser, and the same pairing as the route above: it mints a driver token and
			// does not accept one. **A driver cannot reissue their own link**, which is the whole
			// point of a credential the provider controls — a forwarded link that reached the
			// wrong person could otherwise be used to keep itself alive.
			//
			// # Why this is not a method on /jobs/{id}/driver
			//
			// PUT there would say a second call replaces the driver, and it deliberately does
			// not: 000600 makes an assignment's identity immutable because `milestones.actor_id`
			// names it, and replacing a driver is ending one row and inserting another. This
			// replaces the *link* and leaves the driver, the row and every milestone recorded
			// against it exactly where they are.
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return deliveryHandler(d).ReissueDriverLink() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/jobs/{id}/milestones",
			Group:   GroupV1,

			// RequireUser again, and the reason bears repeating because this is the route
			// whose name most invites the other answer. A driver records milestones from a
			// link-authenticated portal — but **that route is SHIP-121's, not this one**.
			// This is the awarded provider recording their own delivery, checked against the
			// accepted bid. When the driver's own milestone route arrives it will declare
			// RequireDriverToken and sit under /driver, beside the read below.
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return deliveryHandler(d).RecordMilestone() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/jobs/{id}/proof-uploads",
			Group:   GroupV1,

			// RequireUser, and the third route in this file to explain why it is not the
			// driver's. The caller is the awarded provider, checked against the accepted
			// bid.
			//
			// **SHIP-122 needs the driver's version of this and it is not here**, which is a
			// gap recorded in Docs/11 §3 rather than a decision. A driver-token route is
			// served with the idempotency scope `anonymous` — the scope is computed
			// group-wide, outside the middleware, while a guard runs per route inside it —
			// and this response carries a URL that can write into the evidence bucket. That
			// is the SHIP-44 shape the whole service was fixed for, and it stays shut until
			// SHIP-121 settles the scope.
			//
			// # A pre-signed URL is minted here and spent nowhere in this service
			//
			// The same pairing as `/jobs/{id}/driver` above: this route hands out a
			// credential and no route in the manifest accepts one. The bytes go straight to
			// the object store (Docs/06 §5.2), so there is no upload endpoint to declare.
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return deliveryHandler(d).PresignProofUpload() },
		},
		Route{
			Method: http.MethodGet,

			// **`/jobs/{id}/proof` was the intended pattern and it cannot be served**, which
			// is a finding SHIP-115 made rather than a preference. `GET /jobs/open/{id}`
			// (SHIP-83) puts a literal in the `{id}` position, so it and any three-segment
			// `GET /jobs/{id}/<literal>` both match `/jobs/open/proof` with neither more
			// specific — Go's ServeMux refuses the pair at registration and the process does
			// not start. `POST /jobs/{id}/proof-uploads` is unaffected only because the other
			// route is a GET.
			//
			// So the delivery domain takes a shelf of its own under the job, and that turns
			// out to be worth having rather than a workaround: **every read this domain adds
			// under a job would have hit the same wall** — SHIP-116's exception, SHIP-133's
			// tracking view, a milestone timeline — and each now has somewhere to go that
			// composes. Docs/11 §3 records the collision for whoever owns `/jobs/open/{id}`,
			// because it is the shape that will keep costing tickets an hour.
			Pattern: "/jobs/{id}/delivery/proof",
			Group:   GroupV1,

			// **The route above hands out a credential to write into the evidence bucket;
			// this one hands out credentials to read out of it** (SHIP-115), and the pairing
			// is why the auth class alone is not the access control here. RequireUser gets a
			// caller as far as the handler, and the handler asks the database which of two
			// parties they are — the customer who owns the job, or the provider on its
			// accepted bid. Being neither answers exactly what a job that does not exist
			// answers.
			//
			// A proof photograph identifies an address and a recipient, so there is no public
			// read path to one anywhere in this service and no route that serves the bytes.
			// What this returns is short-lived signed URLs at the object store, issued after
			// the check and never before.
			//
			// **Not RequireDriverToken, and not a second route under /driver/ either.** The
			// assigned driver is deliberately not a reader yet — internal/delivery's
			// Service.ProofFor argues it, and SHIP-122 is where it gets revisited with a
			// driver who can actually capture proof in front of it.
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return deliveryHandler(d).ProofOnJob() },
		},
		Route{
			Method: http.MethodGet,

			// **Five segments, and four would stop the process** (SHIP-115a). This is the shelf
			// the route above opened, extended by the ticket `Docs/09` wrote for it — and the
			// row in that document names both paths in full rather than describing a shelf,
			// precisely because building either as `GET /jobs/{id}/delivery` is not a failing
			// test but a ServeMux panic at registration: `make run` dies at startup.
			//
			// # Two operations rather than one composite
			//
			// The alternative was one `GET /jobs/{id}/delivery` answering both, which the
			// segment constraint forbids anyway — but it would also have been wrong on its own
			// terms. The assignment is a single resource and the milestones are a collection
			// that pages; putting a paging collection inside a resource means a client asking
			// for the second page re-fetches the resource, and the envelope of Docs/10 §4.5 has
			// nowhere to sit.
			Pattern: "/jobs/{id}/delivery/detail",
			Group:   GroupV1,

			// RequireUser, and the class is not the access control here either: the handler
			// asks the database which of the two parties the caller is, and being neither
			// answers exactly what a missing job answers.
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return deliveryHandler(d).DeliveryDetail() },
		},
		Route{
			Method:  http.MethodGet,
			Pattern: "/jobs/{id}/delivery/milestones",
			Group:   GroupV1,

			// The second half of SHIP-115a, and the one that has no other source. **A milestone
			// that moved nothing appears here and nowhere else**: a repeated pickup attempt
			// (Docs/02 §5) and SHIP-112's absorbed late milestone each write a `milestones` row
			// and no `job_status_history` row, so a timeline derived from the job's status —
			// which is what SHIP-77 has to do today (Docs/11 §9) — cannot show either.
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return deliveryHandler(d).MilestonesOnJob() },
		},
		Route{
			Method:  http.MethodGet,
			Pattern: "/driver/jobs/{id}",
			Group:   GroupV1,

			// **The first route in the service served on the driver's credential**
			// (SHIP-108), and the one that closes SHIP-15m's seam: filling in
			// newDriverTokenGuard maps this class, and until something did, declaring it
			// here would have stopped the process at startup rather than serving the route
			// open. That failure direction is unchanged — it is what a route declaring
			// RequireAdmin still gets today.
			//
			// # Why /driver/jobs/{id} rather than a second method on /jobs/{id}
			//
			// The manifest allows one auth class per method, so `GET /jobs/{id}` under a
			// driver token was available and is the wrong shape twice over. It would put two
			// credential systems on one path, where a client that presented the wrong one
			// gets an answer that reads as a permissions problem; and it would claim a URL
			// the customer's and the provider's own job view already own (SHIP-65).
			// `/driver/...` says whose surface this is, which is what SHIP-120 and SHIP-121
			// extend rather than negotiate with.
			//
			// # The {id} is checked, not decorative
			//
			// delivery.RequireDriverToken compares it with the job inside the token and
			// refuses a mismatch before the handler runs. That comparison is the auth class,
			// so a route declaring the class cannot skip it — see internal/delivery's
			// driverauth.go for why that is the design rather than a convenience.
			Auth:    RequireDriverToken,
			Handler: func(d Deps) http.Handler { return deliveryHandler(d).DriverJob() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/driver/jobs/{id}/milestones",
			Group:   GroupV1,

			// **The first write in the service served on a credential that names no account**
			// (SHIP-120a), and the route three separate lanes of wave 7 each specified and none
			// built — every one of them correctly finding it belonged to another lane's ticket
			// (Docs/11 §9). A gap three independent readings arrive at is a specification.
			//
			// # Why a second route rather than a second auth class on POST /jobs/{id}/milestones
			//
			// That one is RequireUser and always will be. Serving both classes on one path would
			// put two credential systems on one URL, where presenting the wrong one reads as a
			// permissions problem rather than as the wrong door — and the manifest allows one
			// class per method in any case. So the driver's route sits under `/driver` beside the
			// read, and the two entry points meet in `internal/delivery`'s service rather than in
			// a guard that has been taught to accept either credential.
			//
			// # The idempotency scope, which Docs/11 §9 has held open since SHIP-15m for this route
			//
			// `anonymous`, and **nothing declared here can change it**: the scope is computed
			// group-wide outside the middleware while this class is enforced per route inside it.
			// The decision is argued in [delivery.Handler.RecordDriverMilestone] and it is §9's
			// second option — the platform's "once per key" guarantee is
			// `uq_milestones_idempotency (job_id, idempotency_key)`, which 000602 scoped to the
			// job in advance and named this exact case while doing it.
			//
			// **Unlike POST /jobs/{id}/proof-uploads, this response carries no credential**, which
			// is the whole reason this route can be served today and the driver's upload cannot: a
			// replay hands back a milestone that both parties to the delivery may read anyway,
			// not a URL that writes into the evidence bucket.
			Auth:    RequireDriverToken,
			Handler: func(d Deps) http.Handler { return deliveryHandler(d).RecordDriverMilestone() },
		},
	)
}

// deliveryHandler builds the domain's handler from what Deps already carries.
//
// Nothing delivery needs is missing from Deps, which is the test Docs/10 §9.2 sets for whether a
// domain has been written the way the shared-surface rules ask: the job service, the outbox writer,
// the two ports and — since SHIP-107 — the driver token issuer are all pure functions of the pool,
// the clock and the configuration, so no field had to be added to a shared struct and no line to its
// literal in main.go.
//
// It panics for the same reason jobsHandler does: it runs during attach, at startup, and every
// failure it can report is a wiring mistake that will still be there after a restart. The pool is
// deliberately not checked — it may be nil because the database was unreachable at startup, which is
// a transient condition the service is built to survive, and the handlers answer 503 for as long as
// it lasts.
func deliveryHandler(d Deps) *delivery.Handler {
	// One signer, handed to the domain as two ports (SHIP-115). `delivery` asks for permission to
	// upload through one and reads an object back through the other, and the two are separate
	// interfaces so that a path holding the reader cannot mint permission to write. That they are
	// satisfied by the same value is this file's business and nothing the domain can see.
	store := proofUploads(d)

	svc := delivery.NewService(
		events.NewOutbox(),
		jobLifecycle{jobs: newJobService(d)},
		acceptedBids{},
		jobCustomers{jobs: newJobService(d)},
		driverTokenIssuer(d),
		store,
		store,
		delivery.UploadPolicy{
			MaxBytes:             d.Config.Storage.MaxUploadBytes,
			AcceptedContentTypes: d.Config.Storage.AcceptedContentTypes,
			UploadTTL:            d.Config.Storage.PresignTTL,
			DownloadTTL:          d.Config.Storage.DownloadTTL,
		},
		d.Clock,
	)

	handler, err := delivery.NewHandler(svc, d.Pool, d.Logger)
	if err != nil {
		panic("cmd/api: delivery handler: " + err.Error())
	}
	return handler
}

// proofUploads builds the signer behind POST /v1/jobs/{id}/proof-uploads (SHIP-114).
//
// # This is the only place the domain and the adapter meet, and neither names the other
//
// `delivery` declares delivery.ProofUploads in its own ports.go and imports nothing from
// internal/platform; `storage` knows what a bucket is and has never heard of a job. Go satisfies
// the interface structurally, so the two are joined by the assignment below and by the compile-time
// assertion at the foot of this file — which is, as with the two ports above, the only place in the
// build where that can be established at all.
//
// # Six of the ten configured values are read here and the other four in the policy above
//
// The split is not arbitrary: everything below describes the *store* — where it is, what it is
// called, how to authenticate to it — and everything in [delivery.UploadPolicy] describes what the
// platform will allow into it, and for how long. The first is the adapter's business and the second
// is the domain's, which is Docs/06 §4.1's division written as two structs.
//
// The tenth arrived at SHIP-15r: STORAGE_DOWNLOAD_TTL, which is the policy's rather than the
// adapter's for the same reason the upload lifetime is — how long a link to a photograph stays live
// is a decision about evidence, and the signer will sign whatever window it is handed.
//
// It panics for the reason driverTokenIssuer does: it runs during attach, from a Handler closure
// with nowhere to put an error, and every failure it can report is a configuration fault that will
// still be there after a restart. config.Load has already refused an unparseable endpoint, a
// loopback host outside development, an implausible bucket name and an empty credential, so
// reaching this means configuration produced something it validates against.
func proofUploads(d Deps) *storage.S3 {
	signer, err := storage.NewS3(storage.Options{
		Endpoint:        d.Config.Storage.Endpoint,
		Bucket:          d.Config.Storage.Bucket,
		Region:          d.Config.Storage.Region,
		AccessKeyID:     d.Config.Storage.AccessKeyID,
		SecretAccessKey: d.Config.Storage.SecretAccessKey,
		UsePathStyle:    d.Config.Storage.UsePathStyle,
		Clock:           d.Clock,
	})
	if err != nil {
		panic("cmd/api: proof upload signer: " + err.Error())
	}
	return signer
}

// driverTokenIssuer builds the signer of the driver's job-scoped link (SHIP-107).
//
// # Why this is a panic and newDriverTokenGuard is an error
//
// The two look like they should agree and they should not. That constructor is called from main.go
// before the router exists, where a configuration failure can be *returned* and reported beside every
// other one; this runs during attach, from a Handler closure that has nowhere to put an error. Both
// stop the process, which is the outcome that matters: a keyset that cannot be built will still be
// unbuildable after the next restart, and a service that came up unable to sign a driver token would
// answer every assignment with a 500 while reporting itself healthy.
//
// config.Load has already refused a short key, an active identifier naming no key, and a keyset
// sharing a secret with identity's — so reaching either failure below means configuration produced
// something it validates against, which is a defect in this package rather than in a deployment.
func driverTokenIssuer(d Deps) *delivery.DriverTokenIssuer {
	keys, err := delivery.NewKeyset(d.Config.Delivery.DriverTokenKeys, d.Config.Delivery.DriverTokenActiveKID)
	if err != nil {
		panic("cmd/api: driver token keyset: " + err.Error())
	}

	issuer, err := delivery.NewDriverTokenIssuer(keys, d.Config.Delivery.DriverTokenTTL, d.Clock)
	if err != nil {
		panic("cmd/api: driver token issuer: " + err.Error())
	}
	return issuer
}

// newJobService builds a job service for a domain that needs to move a job but is not `jobs`.
//
// No geocoder, and that is the same call cmd/worker's expiry task makes: an assignment moves a job
// that already exists and never touches an address, and jobs.NewService is explicit that a nil
// Geocoder is a supported state rather than a broken one. Handing this path a maps vendor would be
// an outbound dependency nothing on it has a reason for.
func newJobService(d Deps) *jobs.Service {
	return jobs.NewService(events.NewOutbox(), d.Clock, nil)
}

// jobLifecycle implements delivery.Jobs over the guarded transition (SHIP-57).
//
// This is the seam Docs/06 §4.1 describes working as intended: `delivery` names what it needs in its
// own ports.go, `jobs` knows nothing about `delivery`, and the two are joined here by a type that
// translates one vocabulary into the other. Go satisfies the interface structurally, so neither
// package imports the other.
//
// # The translation is of errors into outcomes, and that is the whole job
//
// delivery cannot call errors.Is against jobs' sentinels — that is an import — so the refusals come
// back as a delivery.JobMove instead. Everything jobs treats as a refusal becomes an outcome with a
// nil error; everything else stays an error, because a failing database is not an answer.
type jobLifecycle struct {
	jobs *jobs.Service
}

// MoveToDriverAssigned runs Docs/02 §2's `Awarded → Driver assigned` through the one guarded
// function, inside the caller's transaction.
//
// The actor is the provider, which is what Docs/02 §1 names as the primary actor for this status and
// what job_status_history records. The reason is empty on purpose: Docs/01 §3 requires one only of an
// administrator, and a provider nominating their own driver owes nobody an explanation.
//
// RecordedAt is left zero, so jobs.Transition uses the platform's clock for both. That is correct
// here and would not be for a milestone: this request is made online, by a provider looking at the
// screen, so there is only one clock. The three methods below carry the actor's separately.
func (l jobLifecycle) MoveToDriverAssigned(
	ctx context.Context,
	r db.Runner,
	jobID, providerID uuid.UUID,
) (delivery.JobMove, error) {
	return l.move(ctx, r, jobs.Move{
		JobID: jobID,
		To:    jobs.StatusDriverAssigned,
		Actor: jobs.User(jobs.ActorProvider, providerID),
	})
}

// The three moves a recorded milestone can make (SHIP-111).
//
// One method per move rather than one taking a status, which delivery/ports.go argues at length and
// this file is where the argument is cashed: **the four `jobs.Status` values below appear in cmd/api
// and nowhere else.** `delivery` names milestones, this names statuses, and the mapping between the
// two vocabularies lives in the composition root where both are visible.
//
// **`Delivered` was deliberately absent until SHIP-118, and this is where that ends.** While
// neither photo proof nor a reasoned exception could be captured, having no method here meant the
// refusal could not be removed by editing one file. Both can be captured now (SHIP-115, SHIP-116),
// so the refusal has a condition rather than being unconditional, and what replaces this method's
// absence as the second layer is a database constraint: 000605 refuses a `Delivered` milestone with
// no `proofs` row at commit, whoever wrote it and through whatever code path.
//
// RecordedAt is the actor's clock, passed through to job_status_history's actor_recorded_at. The
// transition and the milestone that caused it are one act, and the pair of rows they leave must
// agree about when the actor says it happened (Docs/02 §3.1). Each table stamps its own arrival
// time from the database's clock, so the two are never collapsed.

func (l jobLifecycle) MoveToEnRouteToPickup(
	ctx context.Context,
	r db.Runner,
	jobID uuid.UUID,
	by delivery.Recorder,
	recordedAt time.Time,
) (delivery.JobMove, error) {
	return l.moveBy(ctx, r, jobID, jobs.StatusEnRouteToPickup, by, recordedAt)
}

func (l jobLifecycle) MoveToPickedUp(
	ctx context.Context,
	r db.Runner,
	jobID uuid.UUID,
	by delivery.Recorder,
	recordedAt time.Time,
) (delivery.JobMove, error) {
	return l.moveBy(ctx, r, jobID, jobs.StatusPickedUp, by, recordedAt)
}

func (l jobLifecycle) MoveToInTransit(
	ctx context.Context,
	r db.Runner,
	jobID uuid.UUID,
	by delivery.Recorder,
	recordedAt time.Time,
) (delivery.JobMove, error) {
	return l.moveBy(ctx, r, jobID, jobs.StatusInTransit, by, recordedAt)
}

// MoveToDelivered is `In transit → Delivered` (SHIP-118).
//
// Identical in shape to the three above, which is the point: whether the delivery has anything to
// show for itself was decided in `delivery` before this was called, and the transition table has no
// opinion about it. Docs/02 §2's condition on this row — "proof-of-delivery data recorded, or a
// reasoned exception recorded" — is a fact about `proofs`, and `jobs` has never heard of that table.
func (l jobLifecycle) MoveToDelivered(
	ctx context.Context,
	r db.Runner,
	jobID uuid.UUID,
	by delivery.Recorder,
	recordedAt time.Time,
) (delivery.JobMove, error) {
	return l.moveBy(ctx, r, jobID, jobs.StatusDelivered, by, recordedAt)
}

// moveBy is the four methods above with their one difference — the target status — as an argument,
// and the actor translation in one place (SHIP-120a).
//
// # This is the second vocabulary this file translates, and it is translated exactly once
//
// The first is the milestone-to-status mapping the four methods are: `delivery` names milestones,
// this names statuses, and Docs/02 §2's table is visible from neither side alone. The second is
// who acted. A [delivery.Recorder] carries `(actor_type, actor_id)` in the delivery domain's own
// vocabulary; `jobs.Actor` carries the same pair in its own, and 000401 and 000601 already agree
// about what each value means — a `users` row for a provider, a `driver_assignments` row for a
// driver, because a driver has no account.
//
// **An actor this function has no mapping for is an error rather than a default**, which is the
// same call [jobLifecycle.move] makes about an unrecognised outcome. Defaulting to the provider
// would compile, pass every test written today, and write `job_status_history` rows saying a
// provider moved a job their driver moved — a false attribution that nothing downstream could
// detect, because the row would be perfectly well formed.
//
// [delivery.ActorSystem] and [delivery.ActorAdmin] fall into that refusal today and deliberately:
// the platform's automatic `Picked up → In transit` (Docs/02 §2) will be a worker rather than a
// request, and an administrator's move owes an audit reason that `jobs.Move.validate` requires and
// this signature has nowhere to carry. Both are a method here when somebody writes the ticket.
func (l jobLifecycle) moveBy(
	ctx context.Context,
	r db.Runner,
	jobID uuid.UUID,
	to jobs.Status,
	by delivery.Recorder,
	recordedAt time.Time,
) (delivery.JobMove, error) {
	var kind jobs.ActorType
	switch by.Type {
	case delivery.ActorProvider:
		kind = jobs.ActorProvider
	case delivery.ActorDriver:
		kind = jobs.ActorDriver
	default:
		return delivery.JobMoveUnrecognised, fmt.Errorf(
			"cmd/api: moving %s to %s: %q is not an actor this adapter can attribute a transition to",
			jobID, to, by.Type)
	}

	return l.move(ctx, r, jobs.Move{
		JobID:      jobID,
		To:         to,
		Actor:      jobs.User(kind, by.ID),
		RecordedAt: recordedAt,
	})
}

// move is the translation itself, in one place: what `jobs` treats as a refusal becomes a
// delivery.JobMove with a nil error, and everything else stays an error because a failing database
// is not an answer.
//
// Written once rather than four times because a fifth move must not be able to translate
// jobs.ErrAlreadyInStatus differently from the four before it. That is exactly the drift that would
// be invisible: every one of these methods would still compile, still pass its own test, and answer
// a repeated milestone with a refusal on one path and an absorption on another.
//
// # One refusal becomes two answers, and SHIP-112 is why
//
// jobs.ErrTransitionNotPermitted says only that Docs/02 §2 has no row from where the job stands. It
// does not say which way the refused move was pointing, and it should not: the transition table is a
// set of permitted edges and has no notion of forwards. Docs/02 §3.1 does, and it turns on exactly
// one thing — whether "a later transition has already been recorded" — so that is what is asked
// here, of the same domain, in the same transaction.
func (l jobLifecycle) move(ctx context.Context, r db.Runner, m jobs.Move) (delivery.JobMove, error) {
	_, err := l.jobs.Transition(ctx, r, m)

	switch {
	case err == nil:
		return delivery.JobMoved, nil
	case errors.Is(err, jobs.ErrJobNotFound):
		return delivery.JobNotFound, nil
	case errors.Is(err, jobs.ErrAlreadyInStatus):
		return delivery.JobAlreadyInStatus, nil
	case errors.Is(err, jobs.ErrTransitionNotPermitted):
		return l.refusal(ctx, r, m)
	default:
		return delivery.JobMoveUnrecognised, err
	}
}

// refusal tells a job that has already been past this status from one that has not reached it
// (SHIP-112).
//
// # It reads history rather than reasoning about the table
//
// The question could have been asked of Docs/02 §2's graph — "can the job still reach this status by
// any permitted sequence?" — and that answer would be wrong in a way nobody would notice for a while.
// A job cancelled or disputed at Awarded can no longer reach 'Picked up' either, and it has not moved
// *past* the pickup; it has lost the delivery to something else. Docs/02 §3.1 keeps those two apart
// and gives them different handling — the second is the administrative-conflict bullet, which is
// SHIP-113 — so the test is the recorded fact, not the reachable set.
//
// # Why the composition root and not the domain
//
// `delivery` may not name a jobs.Status (Docs/06 §4.1), and this question is about one. Everything
// below is already visible here: the target status is in the Move this file built, and
// jobs.Service.History is that domain's own reader. The domain gets an answer in its own vocabulary
// and still holds no second copy of the transition table.
//
// The read runs in the caller's transaction, after Transition has taken the job row FOR UPDATE, so
// nothing can move the job between the refusal and this question.
//
// An error here is reported rather than resolved to either answer. Guessing "not assignable" would
// discard a driver's late milestone on a failing database, and guessing "already past" would commit
// one that should have been refused.
func (l jobLifecycle) refusal(ctx context.Context, r db.Runner, m jobs.Move) (delivery.JobMove, error) {
	history, err := l.jobs.History(ctx, r, m.JobID)
	if err != nil {
		return delivery.JobMoveUnrecognised, fmt.Errorf(
			"cmd/api: reading whether %s has already been %s: %w", m.JobID, m.To, err)
	}

	for _, change := range history {
		if change.To == m.To {
			return delivery.JobAlreadyPast, nil
		}
	}
	return delivery.JobNotAssignable, nil
}

// acceptedBids implements delivery.Awards by reading the job's accepted bid.
//
// # Why this query is here and not in a domain
//
// It belongs to `bidding`, which is `doc.go` and `model.go` today: SHIP-80 built the table and the
// one-accepted-bid index, and SHIP-92 — the award transaction, a single-owner branch that never
// parallelises (Docs/11 §8) — is what gives that domain a store. Until it does, this is the honest
// place for the statement: the composition root, where a dependency between two domains is visible
// to anyone reading how the service is wired, rather than buried in `delivery/postgres.go` where it
// would read as a table delivery owns.
//
// **When `bidding` grows its store, this type is deleted and a method on that store is passed
// instead.** Nothing in `delivery` changes, which is the point of the port.
//
// # 'Accepted' is the awarded provider, and the index is why that is safe to assume
//
// uq_bids_one_accepted_per_job is partial on `status = 'Accepted'`, so this query cannot return two
// rows however many bids a job carries (SHIP-80, SHIP-91). Docs/02 §3 — "awarding a job atomically
// marks one bid accepted and all others closed" — is the statement it enforces.
type acceptedBids struct{}

// AwardedProvider is the provider whose bid the customer accepted, if any.
//
// No lock. The award is checked again by the transition that follows it in the same transaction —
// jobs.Transition takes the job row FOR UPDATE and re-reads its status — so a job awarded to
// somebody else in the instant between the two statements is refused there rather than here.
//
// A job that does not exist and a job nobody has been awarded are the same answer, and delivery
// turns both into one 404. See delivery.Awards.
func (acceptedBids) AwardedProvider(
	ctx context.Context,
	r db.Runner,
	jobID uuid.UUID,
) (uuid.UUID, bool, error) {
	const q = `SELECT provider_id FROM bids WHERE job_id = $1 AND status = 'Accepted'`

	var providerID uuid.UUID
	err := r.QueryRow(ctx, q, jobID).Scan(&providerID)
	switch {
	case errors.Is(err, db.ErrNoRows):
		return uuid.Nil, false, nil
	case err != nil:
		return uuid.Nil, false, fmt.Errorf("cmd/api: reading the accepted bid on %s: %w", jobID, err)
	}
	return providerID, true, nil
}

// jobCustomers implements delivery.JobOwners by asking `jobs` whether this account owns the job
// (SHIP-115).
//
// # Why this one goes through the domain and acceptedBids does not
//
// acceptedBids is raw SQL here because `bidding` has no store yet and will not until SHIP-92. `jobs`
// does, and jobs.Service.Job is the reader that already knows what ownership means — it takes the
// job without a lock and compares customer_id, keeping "somebody else's" and "nobody's" apart in Go
// while answering the same thing on the wire. Writing `SELECT customer_id FROM jobs` here instead
// would be a second definition of a rule that domain owns, in a file that cannot be tested against a
// database.
//
// # Both refusals collapse to false, which is the port's own contract
//
// jobs.ErrNotJobOwner and jobs.ErrJobNotFound are two facts and one answer: "no". delivery turns
// that into the 404 a stranger gets, and a reader who could tell them apart would learn that
// somebody else's job exists (delivery.JobOwners says so).
type jobCustomers struct {
	jobs *jobs.Service
}

// IsCustomer reports whether userID is the customer who owns jobID.
//
// No lock and no transaction: one read, and nothing downstream of it changes the answer — a job's
// customer_id is fixed at creation and no endpoint updates it.
func (c jobCustomers) IsCustomer(
	ctx context.Context,
	r db.Runner,
	jobID, userID uuid.UUID,
) (bool, error) {
	_, err := c.jobs.Job(ctx, r, userID, jobID)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, jobs.ErrNotJobOwner), errors.Is(err, jobs.ErrJobNotFound):
		return false, nil
	default:
		return false, fmt.Errorf("cmd/api: reading whether %s owns %s: %w", userID, jobID, err)
	}
}

// Compile-time proof that the adapters satisfy the ports delivery declared, which is the only place
// in the build where that can be established — delivery names none of these types and none of them
// names delivery, so nothing else links them.
//
// *storage.S3 appears twice on purpose. delivery declares upload signing and object reading as two
// ports, and this is where one value is shown to satisfy both.
var (
	_ delivery.Jobs         = jobLifecycle{}
	_ delivery.Awards       = acceptedBids{}
	_ delivery.JobOwners    = jobCustomers{}
	_ delivery.ProofUploads = (*storage.S3)(nil)
	_ delivery.ProofObjects = (*storage.S3)(nil)
)
