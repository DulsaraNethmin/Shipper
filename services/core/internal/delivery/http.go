// The HTTP surface of the delivery domain.
//
// Handlers live in the domain rather than in cmd/api (Docs/10 §2.1). The reason is a merge hazard
// rather than taste: it keeps cmd/api/routes.go free of per-domain edits, which is what lets two
// domains be built at once without touching a shared file. A domain importing internal/httpx is
// sitting on infrastructure, not crossing a boundary, and the import lint permits it.
//
// # Two callers, two credentials, and they are never the same credential
//
// Worth stating first, because the domain's name invites the assumption that the driver calls all of
// this. Almost all of it is the awarded provider, authenticated the ordinary way, and every route
// under `/jobs/{id}` below declares RequireUser like every other product endpoint in the service.
//
// **One route is the driver's, and it is served under a different auth class entirely** (SHIP-108).
// `GET /v1/driver/jobs/{id}` is reached with the job-scoped link the provider forwarded, verified by
// [RequireDriverToken] rather than by httpx.RequireSubject. The driver has no account and no session
// — the portal is link-authenticated (Docs/07 §3) — and the two token systems cannot be exchanged in
// either direction (Docs/10 §5): the mobile token is refused on the driver route and the driver token
// is refused on all the others, both over HTTP.
//
// The split is visible in the code rather than remembered: a provider route reads its caller with
// [callerID], which reads an authctx.Subject; the driver route reads a [DriverGrant], which has no
// subject in it and cannot be turned into one.
//
// # There is no field for whose job, and no field for who is assigning
//
// The provider is whoever the token says is calling. A provider id in the request would be an
// authorisation decision made from client input, which Docs/07 §3 puts on the platform. The job is
// named in the path and the platform checks it against the accepted bid — or, on the driver route,
// against the job inside the link.
//
// The blank line below keeps this a file note rather than a second package comment.

package delivery

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/pagination"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// Handler serves this domain's routes.
//
// It is built in cmd/api/routes_delivery.go from Deps. The pool is held here rather than inside the
// service because Docs/10 §3.2 puts the transaction boundary with whoever owns the invariant, and
// for an assignment that is this layer: the award check, the assignment row and the status change
// are one decision against one version of the job.
type Handler struct {
	svc  *Service
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewHandler wires the handlers to the service.
//
// The pool may be nil and that is not an error. The service starts with an unreachable database on
// purpose — a rolling deployment during a failover would otherwise take every instance down at once
// — so a nil pool is a condition the handlers answer 503 to for as long as it lasts, not a reason to
// refuse to start.
func NewHandler(svc *Service, pool *pgxpool.Pool, log *slog.Logger) (*Handler, error) {
	if svc == nil {
		return nil, errors.New("delivery: a handler needs a service")
	}
	if log == nil {
		return nil, errors.New("delivery: a handler needs a logger")
	}
	return &Handler{svc: svc, pool: pool, log: log}, nil
}

// assignDriverRequest is the body of POST /v1/jobs/{id}/driver.
//
// Three fields and two ways to fill them in, which is Docs/02 §1's "nominated a driver or
// self-assigned" written as one request:
//
//	{"driver_name": "Sam Patel", "driver_mobile": "0412 345 678"}   nomination
//	{"driver_name": "Ravi Chandra", "self": true}                   self-assignment
//
// `self` with a mobile is refused rather than resolved either way — see [Nomination].
//
// There is no `job_id` and no `provider_id`. The job is in the path and the provider is the token.
// httpx.DecodeJSON refuses unknown fields, so a client that sends either is told the field does not
// exist rather than having it quietly ignored.
type assignDriverRequest struct {
	DriverName   string `json:"driver_name"`
	DriverMobile string `json:"driver_mobile"`
	Self         bool   `json:"self"`
}

// assignmentResponse is the driver on a job, as the awarded provider sees it.
//
// **The job's status is deliberately not echoed here.** The status vocabulary is `jobs`' own and its
// wire form is `jobs`' to map (Docs/10 §4.7, and SHIP-56a takes it over for three languages); a copy
// of the string "driver_assigned" in this package would be a second list to keep in step for the
// sake of a field the client already knows the value of — a successful response to this endpoint
// means the job is at 'Driver assigned' and nothing else.
//
// The mobile is returned because the provider typed it and needs to see it was read correctly,
// including when the platform supplied it from their own account. It reaches nobody but the awarded
// provider: the customer's view of a delivery (SHIP-133) is a shape of its own rather than this one
// reached by another route, for the same reason `jobs` keeps the customer's and the provider's views
// apart.
//
// # The driver's token is returned here, and the awarded provider is the right person to hand it to
//
// Docs/01 §4.5 decides it: "the provider forwards the job-scoped portal link to their driver
// themselves". Shipper sends no SMS in the MVP, so the only way the link reaches the driver is
// through the response to the request that created the assignment — which is also what makes
// SHIP-107's *Done when* readable as one sentence, since the token is generated on assignment and
// obtainable nowhere else.
//
// **This response therefore carries a credential, and it is scoped like one.** The idempotency
// middleware stores it under `idem:v1:user:<provider>:<key>` (SHIP-44), so a replay reaches only the
// provider who made the request — a stronger position than the anonymous scope the sign-in endpoints
// already store their tokens under, and one worth stating because it is not obvious from the shape.
//
// The URL is deliberately **not** assembled here. The driver portal's landing route is SHIP-120's to
// define, and a base URL in this response would be this domain asserting a path in an application it
// does not own; a client that has the token can build the link once that route exists.
type assignmentResponse struct {
	ID    string `json:"id"`
	JobID string `json:"job_id"`

	DriverName   string `json:"driver_name"`
	DriverMobile string `json:"driver_mobile"`

	AssignedAt string `json:"assigned_at"`

	// DriverToken is the signed, job-scoped credential the driver presents (SHIP-107).
	//
	// It grants exactly this job and cannot be exchanged for a mobile session in either
	// direction (Docs/10 §5). Nothing verifies one until SHIP-108, so today it is a value the
	// provider can forward and a driver cannot yet spend.
	DriverToken string `json:"driver_token"`

	// DriverTokenExpiresAt is when the link stops working, in UTC.
	//
	// Returned rather than left to the client to decode out of the token, for two reasons: a
	// client that parsed the JWT to find it would be reading a credential it has no business
	// interpreting, and the provider needs to be able to tell the driver how long they have.
	DriverTokenExpiresAt string `json:"driver_token_expires_at"`
}

func assignmentFrom(a Assignment, token DriverToken) assignmentResponse {
	return assignmentResponse{
		ID:    a.ID.String(),
		JobID: a.JobID.String(),

		DriverName:   a.DriverName,
		DriverMobile: a.DriverMobile,

		AssignedAt: timestamp(a.CreatedAt),

		DriverToken:          token.Value,
		DriverTokenExpiresAt: timestamp(token.ExpiresAt),
	}
}

// timestamp renders an instant the way every other endpoint does, in UTC with milliseconds.
func timestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// AssignDriver handles POST /v1/jobs/{id}/driver (SHIP-106).
//
// A noun under the resource rather than a verb, unlike `/jobs/{id}/cancel`: cancelling is an intent
// with no record of its own, and a driver *is* a record — one row, at most one live per job. POST
// rather than PUT because the two are not the same request: PUT would say a second call replaces
// the first, and replacing a driver is deliberately not this endpoint (see [ErrDriverAlreadyAssigned]).
//
// 201 when a driver was put on the job, 200 when the job already had exactly this driver and
// nothing was written. Both carry the assignment, so a client that does not care which happened
// parses one shape.
//
// The transaction is opened here. Both the assignment and the job's move into 'Driver assigned'
// commit together or neither does — a job at 'Driver assigned' with no driver is a state no client
// and no support queue can read.
func (h *Handler) AssignDriver() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, err := jobIDFrom(r)
		if err != nil {
			return err
		}

		var req assignDriverRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		var (
			assignment Assignment
			token      DriverToken
			created    bool
		)
		err = db.InTx(r.Context(), pool, func(ctx context.Context, runner db.Runner) error {
			var err error
			assignment, token, created, err = h.svc.AssignDriver(ctx, runner, providerID, jobID, Nomination{
				DriverName:   req.DriverName,
				DriverMobile: req.DriverMobile,
				Self:         req.Self,
			})
			return err
		})
		if err != nil {
			return apiError(err)
		}

		if !created {
			// The same driver, nominated twice, with two idempotency keys. Logged because
			// it is the signal that a client is retrying without reusing its key, which is
			// worth seeing in aggregate and impossible to see from the response.
			//
			// The token is not logged and never is — it is the credential itself, and a
			// log line carrying one is a credential in whatever collects the logs. The
			// expiry is safe and is what somebody diagnosing a dead link actually wants.
			httpx.LoggerFrom(r.Context()).Info("a repeated driver nomination was absorbed",
				slog.String("job_id", jobID.String()),
				slog.String("assignment_id", assignment.ID.String()),
				slog.Time("driver_token_expires_at", token.ExpiresAt))

			httpx.WriteJSON(w, http.StatusOK, assignmentFrom(assignment, token))
			return nil
		}

		httpx.WriteJSON(w, http.StatusCreated, assignmentFrom(assignment, token))
		return nil
	})
}

// recordMilestoneRequest is the body of POST /v1/jobs/{id}/milestones.
//
//	{"milestone": "picked_up"}
//	{"milestone": "en_route_to_pickup", "recorded_at": "2026-08-12T06:40:11Z",
//	 "reason": "gate locked, returning at four"}
//
// **`recorded_at` is the actor's clock and the platform does not correct it.** A phone that has been
// out of signal since dawn sends the time the driver acted, and 000601 stores it beside — never
// instead of — the time the request arrived. It is deliberately not bounded against now(): refusing
// an implausible time would discard the record, which is the opposite of what Docs/02 §3.1 asks for.
//
// Omitting it means "now", which is what an online client sends. There is no field for the
// platform's clock and there cannot be — the trigger behind that column raises on an INSERT that
// names it.
//
// # `proof` is an object rather than a string, and SHIP-116 is why (SHIP-115)
//
//	{"milestone": "picked_up", "proof": {"object_key": "proof/<job>/<uuid>"}}
//
// A bare `proof_object_key` would have been shorter and would have had to be replaced. Docs/01 §4.4
// makes the exception path "part of the same feature… built with it, not after": SHIP-116 records a
// *reason* in place of a photograph, and it belongs in the same field because it answers the same
// question — what is the evidence for this milestone. A nested object grows a second key; a flat
// string grows a second field that has to be mutually exclusive with the first by convention.
//
// The key is the one the platform issued (`object_key` from `POST /v1/jobs/{id}/proof-uploads`) and
// clients do not invent one: it is checked against the job in the path before anything is looked up.
type recordMilestoneRequest struct {
	Milestone  string `json:"milestone"`
	RecordedAt string `json:"recorded_at"`
	Reason     string `json:"reason"`

	Proof *proofRequest `json:"proof"`

	// RecipientName and DeliveryNote are Docs/01 §4.4's other two required facts about a
	// delivered job (SHIP-123):
	//
	//	{"milestone": "delivered", "recipient_name": "R. Chen",
	//	 "delivery_note": "Left with reception, signed for",
	//	 "proof": {"object_key": "proof/<job>/<uuid>"}}
	//
	// **Flat rather than nested, unlike `proof`.** That object exists because its two keys are
	// *alternatives* to one question — a photograph or a reason there is none — and a nested
	// object is what makes "exactly one of" expressible. These two are not alternatives to
	// anything: they are required together, on one milestone, and a `delivery` object holding
	// them would be a wrapper whose only job is to be present.
	//
	// Sent on any other milestone they are refused rather than ignored, which is the same call
	// `ck_milestones_delivery_details` makes in the database: a recipient name on a `picked_up`
	// would be recording a handover that did not happen.
	RecipientName string `json:"recipient_name"`
	DeliveryNote  string `json:"delivery_note"`
}

// proofRequest is the evidence for this milestone: what the client uploaded, or why it could not.
//
// A pointer on [recordMilestoneRequest] so that "no proof" and "proof with nothing in it" are
// different requests: the first is every milestone recorded today, and the second is a client that
// meant to send something and did not, which is worth telling them about rather than silently
// recording an unphotographed delivery.
//
// There is no `content_type` and no `content_length`. The client stated both when it asked for the
// URL, and what is recorded is what the **store** reports — see delivery.VerifyProof. Accepting them
// here would be accepting the client's word for the thing this ticket exists to stop taking on
// trust.
//
// # `exception_reason` is the second field SHIP-115 said this object would grow (SHIP-116)
//
//	{"milestone": "picked_up", "proof": {"exception_reason": "recipient_objected"}}
//
// Exactly one of the two, which is the shape [recordMilestoneRequest] chose an object for: Docs/01
// §4.4's exception is "in place of" the photograph, so the two are alternatives to one question
// rather than two independent fields a client could send together or omit together by accident.
// Sending both is refused, and so is sending an object with neither — see [recordingFrom].
type proofRequest struct {
	ObjectKey       string `json:"object_key"`
	ExceptionReason string `json:"exception_reason"`
}

// milestoneResponse is one recorded milestone.
//
// **Two timestamps, deliberately, named for what they mean rather than for their columns.**
// `recorded_at` is when the actor says they acted and is what a customer is shown; `accepted_at` is
// when the platform received it and is what support reasons about. A client that renders only the
// first is right; a client that renders only the second is showing a sync time as though it were a
// delivery event.
//
// The job's status is not echoed, for the reason [assignmentResponse] gives — it is the jobs
// endpoints' vocabulary — and here there is a second reason: a milestone may deliberately move
// nothing at all. Since SHIP-113 there are **three** ways for that to happen — the repeat of
// Docs/02 §5, SHIP-112's absorbed late milestone, and SHIP-113's overruled one — and this shape
// distinguishes none of them; [Outcome] is where the decision not to is argued.
//
// **SHIP-113 is the one that tests that decision, because Docs/02 §3.1 asks for the driver to be
// shown what happened.** It is answered on the job resource rather than here, which is the same
// paragraph's own instruction: "the app displays optimistic local state, clearly marked as pending,
// and reconciles to whatever the platform returns". A field here could not be answered consistently
// anyway — `milestones` is append-only and the row is written *before* the move is attempted, so
// there is nowhere to record which of the three happened, and a retry landing on
// [Service.alreadyRecorded] would have to recompute what the first attempt decided. SHIP-132 is the
// client ticket that reconciles and shows it.
type milestoneResponse struct {
	ID    string `json:"id"`
	JobID string `json:"job_id"`

	Milestone string `json:"milestone"`

	// RecordedBy is the kind of actor rather than who they are. It is `provider` for every row
	// this endpoint writes today and stops being constant at SHIP-108, when a driver holding a
	// job-scoped link can record one — which is when a client first needs to tell "you recorded
	// this" from "your driver did".
	RecordedBy string `json:"recorded_by"`

	Reason string `json:"reason,omitempty"`

	// RecipientName and DeliveryNote are present on a delivered milestone and absent on every
	// other (SHIP-123). `omitempty` for the reason [proofResponse]'s fields are: an absent field
	// says nothing, while an empty string would say that nobody received the goods.
	//
	// They are served to the same readers the rest of the row is — both parties to the delivery,
	// through `GET /v1/jobs/{id}/delivery/milestones` — which is Docs/01 §4.4's acceptance measure
	// for the customer ("a customer can see the latest delivery milestone and proof of delivery").
	RecipientName string `json:"recipient_name,omitempty"`
	DeliveryNote  string `json:"delivery_note,omitempty"`

	RecordedAt string `json:"recorded_at"`
	AcceptedAt string `json:"accepted_at"`
}

func milestoneFrom(rec Record) milestoneResponse {
	return milestoneResponse{
		ID:    rec.ID.String(),
		JobID: rec.JobID.String(),

		Milestone:  rec.Milestone.Wire(),
		RecordedBy: string(rec.Actor),
		Reason:     rec.Reason,

		RecipientName: rec.RecipientName,
		DeliveryNote:  rec.DeliveryNote,

		RecordedAt: timestamp(rec.ActorRecordedAt),
		AcceptedAt: timestamp(rec.ServerRecordedAt),
	}
}

// RecordMilestone handles POST /v1/jobs/{id}/milestones (SHIP-111).
//
// A collection under the job, and plural: a delivery accumulates milestones and 000601 refuses none
// of them for being a repeat. `POST /jobs/{id}/status` would have been the same request under the
// wrong name — job status is not a settable field, and what a client records here is what somebody
// did, from which a status move may or may not follow.
//
// 201 when a milestone was recorded, 200 when this idempotency key had already recorded it and
// nothing was written. Both carry the same shape.
//
// # The 200 is the ticket, not a nicety
//
// SHIP-15's middleware replays the stored response for a repeated key and this handler is never
// reached — while the entry lives. The path that matters is the one after it expires or is evicted:
// the handler runs again, the unique index refuses the second row, and this answers with the
// milestone the first attempt recorded. A driver's phone reconnecting after a day in a valley takes
// that path, and it is the only reason "records a milestone once per idempotency key" is true of the
// platform rather than of its cache.
//
// # An absorbed milestone is a 201 like any other, and the response says nothing more (SHIP-112)
//
// A late milestone — one whose point in the delivery the job has already passed — is recorded and
// the job is left where it stands. The row was created, so the status is `201`, and the body is the
// same body: **there is no field saying the job did not move**, because this response has never told
// a client what status the job is in and Docs/02 §3.1 puts that reconciliation on the job resource.
// See [Outcome] for why one was not added.
//
// What changes for a client is that the `409` it used to get here has gone, which is the whole point:
// the driver's work is now safe on the platform rather than pending on a phone forever.
func (h *Handler) RecordMilestone() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, err := jobIDFrom(r)
		if err != nil {
			return err
		}

		var req recordMilestoneRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		recording, proofKey, err := recordingFrom(req, r.Header.Get(httpx.HeaderIdempotencyKey))
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		// The store is asked **before** the transaction opens (SHIP-115), and the ordering is
		// the decision rather than an accident. VerifyProof makes a network request to the
		// object store, and a database transaction held open across a call to another service
		// puts a pool connection at the mercy of that service's worst day. So the question is
		// asked on the pool, and the transaction below starts with the answer already in hand.
		//
		// The cost is that the accepted-bid lookup happens twice, which is one indexed read.
		// Skipping it here would let a stranger learn whether an object exists; skipping it in
		// RecordMilestone would make this call's result load-bearing for authorisation, which is
		// exactly the kind of rule that survives until somebody adds a second caller.
		if proofKey != "" {
			recording.Proof, err = h.svc.VerifyProof(r.Context(), pool, providerID, jobID, proofKey)
			if err != nil {
				return apiError(err)
			}
		}

		var (
			record  Record
			outcome Outcome
		)
		err = db.InTx(r.Context(), pool, func(ctx context.Context, runner db.Runner) error {
			var err error
			record, outcome, err = h.svc.RecordMilestone(ctx, runner, providerID, jobID, recording)
			return err
		})
		if err != nil {
			return apiError(err)
		}

		return writeMilestone(w, r, jobID, recording, record, outcome)
	})
}

// RecordDriverMilestone handles POST /v1/driver/jobs/{id}/milestones (SHIP-120a).
//
// # The second entry point to the same recording, and it is a second route rather than a relaxation
//
// `POST /v1/jobs/{id}/milestones` is RequireUser and always will be. This is RequireDriverToken, and
// the two credentials are separate systems that cannot be exchanged in either direction (Docs/10
// §5): a mobile access token answers 401 here, and a driver's link answers 401 there. Neither is a
// special case in a handler — both are what the auth class does, which is why the route is separate.
//
// It is the route three lanes of wave 7 independently specified and none built, each correctly
// finding it belonged to another lane's ticket (Docs/11 §9). SHIP-121 draws the buttons over it.
//
// # There is no job identifier in this function, deliberately
//
// [Handler.DriverJob] made the same call for a read and this one matters more, because this writes.
// The grant is what the service is given, so the *only* job this handler can act on is the one
// inside a signature — and the guard has already refused a link presented on any other job before
// this runs. A handler that read `{id}` itself would be a second place deciding, agreeing with the
// first today and one refactor away from not.
//
// # A driver may attach a photograph, and may say there is none (SHIP-122)
//
// Both halves of Docs/01 §4.4 are reachable from a driver's link now, and until SHIP-122 only the
// second was. The reason was never that a driver should not photograph a delivery — it was that
// **there was no route by which a driver could obtain an object key**, so any key presented here had
// come from somewhere a driver should not have been, and an `object_key` was refused as a field
// error naming the exception path beside it. SHIP-122 built the driver's upload and its record
// together, which is what that refusal said would have to happen.
//
// A key is checked against the **grant** rather than against a path — [Service.VerifyDriverProof] —
// so a driver carrying two deliveries cannot attach one job's photograph to the other, and the
// refusal is the same [ErrProofNotForThisJob] a provider gets. The store is asked before the
// transaction opens, for the reason [Handler.RecordMilestone] gives: a database transaction held
// open across a call to another service puts a pool connection at the mercy of that service's worst
// day.
//
// The consequence of the old refusal is worth recording rather than forgetting: for the whole of
// wave 8, SHIP-117's moderation queue was the *only* path by which a driver-recorded delivery could
// reach 'Delivered', because a reasoned exception was the only evidence a driver could produce.
// Docs/11 §3 carries it.
//
// # The idempotency key is scoped `anonymous`, and the guarantee is not Redis's anyway
//
// Docs/11 §9 has held this decision open since SHIP-15m for the first driver-authenticated write,
// which is this one. `httpx.SubjectScope` keys on the `authctx.Subject`; a driver token deliberately
// produces none, and the scope is computed group-wide outside the middleware while a guard runs per
// route inside it — so **nothing this route can do changes it**, and the only shape that would is a
// second resolver in `internal/httpx`, which no domain branch may edit.
//
// **The decision is the second of the two §9 offered: a job-scoped grant scopes on the job
// identifier already in the path, and it costs nothing because the platform already does it.**
// `uq_milestones_idempotency` is `(job_id, idempotency_key)` and 000602 argued that scope in
// advance, naming this exact case — "a subject column would be a second copy of that fact, and a
// wrong one the moment SHIP-108's driver records under the same key". So the guarantee lives in a
// btree that survives an eviction, a flush and two instances; Redis makes a retry cheap and the
// index makes it correct. `replayOrRefuse` fingerprints method, path and body, and the path carries
// the job, so reaching another caller's stored response means already holding their job identifier,
// their key and their exact body — the posture Docs/11 §6 accepts for every public route.
//
// What a driver and their provider **do** share is that namespace: a key one has used on a job
// answers the other with the row it recorded, or refuses it as reused. That is correct rather than
// a leak — they are the two actors on one delivery, and a milestone is not private between them.
func (h *Handler) RecordDriverMilestone() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		grant, ok := driverGrantFrom(r.Context())
		if !ok {
			// The same call [Handler.DriverJob] makes: the route declares RequireDriverToken, so
			// reaching here with no grant means it was declared with the wrong class — a wiring
			// defect the caller can do nothing about.
			return httpx.NewError(http.StatusInternalServerError, httpx.CodeInternal,
				"Something went wrong at our end.").WithCause(errors.New(
				"delivery: a driver route was reached with no verified grant on the context; " +
					"its Auth class is not RequireDriverToken"))
		}

		var req recordMilestoneRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		recording, proofKey, err := recordingFrom(req, r.Header.Get(httpx.HeaderIdempotencyKey))
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		// Outside the transaction, exactly as [Handler.RecordMilestone] does it and for the same
		// reason. The cost is that the live-assignment lookup happens twice, which is one indexed
		// read on a primary key.
		if proofKey != "" {
			recording.Proof, err = h.svc.VerifyDriverProof(r.Context(), pool, grant, proofKey)
			if err != nil {
				return apiError(err)
			}
		}

		var (
			record  Record
			outcome Outcome
		)
		err = db.InTx(r.Context(), pool, func(ctx context.Context, runner db.Runner) error {
			var err error
			record, outcome, err = h.svc.RecordDriverMilestone(ctx, runner, grant, recording)
			return err
		})
		if err != nil {
			return apiError(err)
		}

		return writeMilestone(w, r, grant.JobID, recording, record, outcome)
	})
}

// writeMilestone is what both milestone routes answer with, and the logging both owe.
//
// One function rather than a copy per entry point, which is the same call [Service.recorded] makes
// one layer down: the four outcomes and the two operational log lines are the ticket, and a second
// copy is a second place for a driver's absorbed milestone to be answered differently from a
// provider's.
func writeMilestone(
	w http.ResponseWriter,
	r *http.Request,
	jobID uuid.UUID,
	recording Recording,
	record Record,
	outcome Outcome,
) error {
	// Logged when proof was recorded, for the reason [Handler.PresignProofUpload]'s line is:
	// **the object is otherwise invisible to this service.** The bytes never came through here
	// and the fetch never will, so this line and the store's own access log are the only two
	// records that a photograph became evidence for this delivery — which is what somebody
	// reconciling a bucket against the database has to work from. The URL is not logged
	// anywhere and neither is anything derived from it; a key is a name, not a credential.
	if recording.Proof.present() && outcome != OutcomeAlreadyRecorded {
		httpx.LoggerFrom(r.Context()).Info("a photograph was recorded as proof of delivery",
			slog.String("job_id", jobID.String()),
			slog.String("milestone_id", record.ID.String()),
			slog.String("milestone", record.Milestone.Wire()),
			slog.String("object_key", recording.Proof.ObjectKey()))
	}

	// Logged at warning rather than info, and it is the one line here that is about operations
	// rather than about reconciliation (SHIP-116). Docs/04 §5 puts "failed proof of delivery" in
	// a moderation queue and **SHIP-117 is the ticket that builds one**; until it does, this line
	// is the only trace an exception leaves anywhere outside the `proofs` table, and a delivery
	// with no photograph is exactly what somebody should be able to find in a log while the queue
	// is being written.
	if recording.Exception != "" && outcome != OutcomeAlreadyRecorded {
		httpx.LoggerFrom(r.Context()).Warn("a milestone was recorded with a reasoned exception instead of a photograph",
			slog.String("job_id", jobID.String()),
			slog.String("milestone_id", record.ID.String()),
			slog.String("milestone", record.Milestone.Wire()),
			slog.String("exception_reason", recording.Exception.String()))
	}

	switch outcome {
	case OutcomeRecorded:
		httpx.WriteJSON(w, http.StatusCreated, milestoneFrom(record))
		return nil

	case OutcomeAbsorbed:
		// Logged because it is the *only* trace absorption leaves. The row is
		// indistinguishable from any other milestone, the job did not move, no history
		// row was written and no event was emitted — so without this line a late
		// milestone is invisible to operations, and Docs/02 §3.1's escalation is
		// entirely about how long updates have been out of sync.
		httpx.LoggerFrom(r.Context()).Info("a late milestone was absorbed as history and the job was not moved",
			slog.String("job_id", jobID.String()),
			slog.String("milestone", record.Milestone.Wire()),
			slog.String("milestone_id", record.ID.String()),
			slog.Time("recorded_at", record.ActorRecordedAt),
			slog.Time("accepted_at", record.ServerRecordedAt))

		httpx.WriteJSON(w, http.StatusCreated, milestoneFrom(record))
		return nil

	case OutcomeOverruled:
		// **SHIP-113, and this line is the trace the retention leaves.** At warning rather
		// than info, like the proof exception above and for the same reason: this is an
		// operational fact rather than a reconciliation one. A driver has recorded work on a
		// job that was ended or frozen while they were out of signal, and Docs/04 §5's queues
		// are where somebody looks at that.
		//
		// The status the job stands in is deliberately not on this line. `delivery` may not
		// name a jobs.Status (Docs/06 §4.1) and this would be the only place in the package
		// that did — while `job_status_history` already records which action overruled the
		// attempt, with the administrator's reason attached, which Docs/01 §3 requires.
		httpx.LoggerFrom(r.Context()).Warn("a queued milestone lost to an action taken while it was unsynced, and was retained",
			slog.String("job_id", jobID.String()),
			slog.String("milestone", record.Milestone.Wire()),
			slog.String("milestone_id", record.ID.String()),
			slog.Time("recorded_at", record.ActorRecordedAt),
			slog.Time("accepted_at", record.ServerRecordedAt))

		httpx.WriteJSON(w, http.StatusCreated, milestoneFrom(record))
		return nil

	case OutcomeAlreadyRecorded:
		// Logged because it is the signal that the middleware's entry had gone and the
		// database caught the retry instead. That is the mechanism working, and it is
		// otherwise invisible: the response is indistinguishable from an ordinary one.
		httpx.LoggerFrom(r.Context()).Info("a milestone retry was answered from the record rather than recorded again",
			slog.String("job_id", jobID.String()),
			slog.String("milestone", record.Milestone.Wire()),
			slog.String("milestone_id", record.ID.String()))

		httpx.WriteJSON(w, http.StatusOK, milestoneFrom(record))
		return nil

	default:
		// The service returned no error and no outcome, which is a defect here rather
		// than anything the caller did. Answering 201 would be the tempting default and
		// the wrong one: it would report a recording that nothing in this function can
		// say happened.
		return httpx.NewError(http.StatusInternalServerError, httpx.CodeInternal,
			"Something went wrong at our end.").WithCause(fmt.Errorf(
			"delivery: recording %s on %s answered %q with no error",
			recording.Milestone, jobID, outcome))
	}
}

// proofUploadRequest is the body of POST /v1/jobs/{id}/proof-uploads.
//
//	{"content_type": "image/jpeg", "content_length": 1874233}
//
// **Both fields are signed into the URL**, which is why they are required and why neither is a
// hint. A URL that did not bind them would authorise any body at all, and the platform's size limit
// and accepted-type list would be advice the client gave itself (see [UploadPolicy]).
//
// `content_length` is the exact size, not a maximum. A pre-signed PUT has one bound available to it
// and that is the signed `Content-Length` header, so an upload of any other size is refused by the
// store. The client has the file, so it knows.
//
// There is no `job_id`, no `provider_id` and no `object_key`. The first two are the path and the
// token; the third is the platform's to choose, and a client that could name the object it writes
// to could name one that already holds somebody's proof.
type proofUploadRequest struct {
	ContentType   string `json:"content_type"`
	ContentLength int64  `json:"content_length"`
}

// proofUploadResponse is one place to put one photograph.
//
// # It carries a credential, and it is scoped like one
//
// `upload_url` is the whole of the authorisation to write that object — anyone holding it can,
// until `expires_at`, and nothing can revoke it. The idempotency middleware stores this response
// under `idem:v1:user:<provider>:<key>` (SHIP-44), so even a replay reaches only the provider who
// asked. That is the same property [assignmentResponse] relies on, stated again because neither
// shape looks like a credential.
//
// # The two echoed fields are instructions rather than confirmations
//
// `content_type` and `content_length` are what the client **must** send on the PUT, byte for byte:
// they are inside the signature. They are echoed rather than assumed because a client that
// normalised its own media type differently — `IMAGE/JPEG`, or a `; charset=` parameter — would get
// a signature failure from the store with no explanation, and the platform has already decided on
// one spelling by the time it signs.
//
// The method is stated for the same reason. A PUT is not guessable from a `201`-shaped response
// body, and SHIP-122's caller is a browser doing this by hand.
type proofUploadResponse struct {
	ObjectKey string `json:"object_key"`

	UploadURL string `json:"upload_url"`
	Method    string `json:"method"`

	ContentType   string `json:"content_type"`
	ContentLength int64  `json:"content_length"`

	ExpiresAt string `json:"expires_at"`
}

func proofUploadFrom(u Upload) proofUploadResponse {
	return proofUploadResponse{
		ObjectKey: u.ObjectKey,

		UploadURL: u.URL,
		Method:    http.MethodPut,

		ContentType:   u.ContentType,
		ContentLength: u.ContentLength,

		ExpiresAt: timestamp(u.ExpiresAt),
	}
}

// PresignProofUpload handles POST /v1/jobs/{id}/proof-uploads (SHIP-114).
//
// # 200 rather than 201, and the difference is not pedantry
//
// Nothing was created. The platform holds no record of this URL, wrote no row and reserved no
// object — the same request a minute later produces a different key, and the bucket is untouched
// until the client PUTs. What happened is that a credential was issued, which is what
// `POST /v1/auth/login` does and what it also answers `200` to. **SHIP-115 is the ticket that
// creates something**: the record linking an uploaded object to a job and a milestone, at which
// point the object becomes proof and the response that says so is a `201`.
//
// # POST on a read-shaped request, and it is state-changing enough to need a key
//
// It reads nothing and writes nothing, so `GET` was available and is wrong twice over: the request
// carries a body the platform signs, and issuing a credential that cannot be revoked is not a safe
// method whatever the database did. It therefore requires an `Idempotency-Key` like every other
// state-changing route (SHIP-15), and what a retry gets is argued at [Service.PresignProofUpload] —
// the short version is that the middleware replays the identical URL with its expiry already
// running down, which is correct, because one intent bought one upload slot.
//
// # The bytes do not come back through here, and that is the ticket
//
// A successful response is the end of this service's involvement. The client PUTs to `upload_url`
// directly (Docs/06 §5.2) and the API sees neither the request nor the photograph — which is what
// `scripts/verify/70-delivery.sh` demonstrates by uploading to a host that is not the API's.
func (h *Handler) PresignProofUpload() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, err := jobIDFrom(r)
		if err != nil {
			return err
		}

		var req proofUploadRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		upload, err := h.svc.PresignProofUpload(r.Context(), pool, providerID, jobID, UploadRequest{
			ContentType:   req.ContentType,
			ContentLength: req.ContentLength,
		})
		if err != nil {
			return apiError(err)
		}

		// Logged because the URL is otherwise invisible to operations: the upload it authorises
		// never touches this service, so this line and the store's own access log are the only
		// two records that it was issued. **The URL itself is not logged and never is** — it is
		// the credential, and a log line carrying one is a credential in whatever collects the
		// logs. The key and the expiry are what somebody reconciling a missing photograph wants.
		httpx.LoggerFrom(r.Context()).Info("an upload URL was issued for proof of delivery",
			slog.String("job_id", jobID.String()),
			slog.String("object_key", upload.ObjectKey),
			slog.String("content_type", upload.ContentType),
			slog.Int64("content_length", upload.ContentLength),
			slog.Time("expires_at", upload.ExpiresAt))

		httpx.WriteJSON(w, http.StatusOK, proofUploadFrom(upload))
		return nil
	})
}

// PresignDriverProofUpload handles POST /v1/driver/jobs/{id}/proof-uploads (SHIP-122).
//
// # The route routes_delivery.go said was a gap rather than a decision
//
// That file's comment on `POST /v1/jobs/{id}/proof-uploads` recorded it in advance: "SHIP-122 needs
// the driver's version of this and it is not here… a driver-token route is served with the
// idempotency scope `anonymous`… and this response carries a URL that can write into the evidence
// bucket. That is the SHIP-44 shape the whole service was fixed for, and it stays shut until
// SHIP-121 settles the scope." SHIP-121 settled it and this is the route.
//
// 200 rather than 201, for the reason [Handler.PresignProofUpload] gives: nothing was created. The
// platform holds no record of this URL, wrote no row and reserved no object.
//
// # The idempotency key on this route is `anonymous`-scoped, and this is the paragraph to read
// before changing anything here
//
// `httpx.Idempotent` wraps the whole `/v1` group and the auth class is applied **per route, inside
// it** — so on a repeated key the middleware replays the stored response *before* RequireDriverToken
// runs. On [Handler.RecordDriverMilestone] that is argued as acceptable because the stored body is a
// milestone both parties to the delivery may read anyway. **Here the stored body is a credential**,
// which is a different question and is why routes_delivery.go held this route shut for a wave.
//
// What is reachable, stated precisely rather than waved at. To be handed this response somebody must
// send an *identical* request: `replayOrRefuse` fingerprints method, path and body, and a mismatch is
// a 409 rather than a replay. So they need the job identifier, the exact `content_length` of the
// driver's photograph in bytes, and the key. The key is 122 bits from a CSPRNG — the driver portal's
// `lib/keys.ts` mints it with `crypto.randomUUID`, and it is fresh per attempt because a presign must
// not reuse a stored one (a retry would otherwise get the same URL with its expiry already run down).
//
// What such a caller could then do is bounded on every side, and the bounds are the argument:
//
//   - **They cannot make it evidence.** Recording an object against a milestone needs a valid driver
//     token for this job — [Service.VerifyDriverProof] takes a grant — so the object stays
//     unreferenced, which is the state SHIP-114 already calls expected and ages out under a lifecycle
//     rule.
//   - **They cannot read it.** The bucket has no public read path, and a signed download comes only
//     from `GET /v1/jobs/{id}/delivery/proof` after [Service.ProofFor] has decided the reader is the
//     job's customer or its awarded provider.
//   - **They cannot write anything else.** The content type and the length are signed into the URL,
//     so the only object it authorises is one of exactly that size and type.
//   - **An overwrite of a recorded photograph is detectable.** 000603 stores the store's entity tag
//     at the moment the object became proof, which is what that column exists for.
//
// **This is the posture Docs/11 §6 already accepts for every anonymous-scoped route** — "reading
// another caller's stored response requires sending their exact request" — and what is new is that
// the body is a credential rather than a fact. It is recorded in Docs/11 §9 with the mechanism that
// closes it properly: §9's *first* option, a second group-wide resolver beside `ResolveSubject` that
// a driver grant can populate, with `SubjectScope` widened to read either. That is an
// `internal/httpx` change and therefore a prep ticket's, not a domain branch's.
//
// # There is no job identifier in this function, deliberately
//
// The same call [Handler.RecordDriverMilestone] and [Handler.DriverJob] make, and it matters most on
// the route that mints a write credential: the grant is what the service is given, so the only job
// this handler can sign a key for is the one inside a signature. A handler that read `{id}` itself
// would agree with the guard today and be one refactor away from being the only thing deciding.
func (h *Handler) PresignDriverProofUpload() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		grant, ok := driverGrantFrom(r.Context())
		if !ok {
			return httpx.NewError(http.StatusInternalServerError, httpx.CodeInternal,
				"Something went wrong at our end.").WithCause(errors.New(
				"delivery: a driver route was reached with no verified grant on the context; " +
					"its Auth class is not RequireDriverToken"))
		}

		var req proofUploadRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		upload, err := h.svc.PresignDriverProofUpload(r.Context(), pool, grant, UploadRequest{
			ContentType:   req.ContentType,
			ContentLength: req.ContentLength,
		})
		if err != nil {
			return apiError(err)
		}

		// Logged for the reason [Handler.PresignProofUpload]'s line is — the upload never touches
		// this service, so this and the store's own access log are the only two records that a URL
		// was issued — plus one this route has of its own: **the assignment is named**, because the
		// driver has no account and the `driver_assignments` row is their whole identity. Somebody
		// reconciling a bucket against the database, or asking who was handed a URL that wrote an
		// object nothing references, has nothing else to work from.
		//
		// **The URL is not logged and never is.** It is the credential.
		httpx.LoggerFrom(r.Context()).Info("an upload URL was issued to a driver for proof of delivery",
			slog.String("job_id", grant.JobID.String()),
			slog.String("assignment_id", grant.AssignmentID.String()),
			slog.String("object_key", upload.ObjectKey),
			slog.String("content_type", upload.ContentType),
			slog.Int64("content_length", upload.ContentLength),
			slog.Time("expires_at", upload.ExpiresAt))

		httpx.WriteJSON(w, http.StatusOK, proofUploadFrom(upload))
		return nil
	})
}

// proofResponse is one photograph and the milestone it is evidence for (SHIP-115).
//
// # `download_url` is a credential and is the reason this endpoint exists at all
//
// The bucket has no public read path (internal/platform/storage/doc.go), so a proof photograph is
// reachable only through a short-lived signed URL, and one is issued only after this service has
// decided that the caller is the job's customer or the provider who was awarded it. Whoever ends up
// holding the URL can fetch the image until `download_expires_at`, and **nothing can revoke it** —
// the same property the upload URL has, which is why the lifetime is short and why a client should
// render the image rather than store the link.
//
// # What is deliberately not here
//
// No `etag`. The store's tag for the bytes is recorded (000603) so that a later overwrite is
// detectable, and it is an integrity fact for an administrator (SHIP-155) rather than something a
// customer's app would do anything with.
//
// No actor. Who recorded the milestone is on the milestone, and `GET /v1/jobs/{id}` is where a
// client assembles a delivery's timeline; repeating it here would be a second copy that can
// disagree with the first.
//
// `recorded_at` is the **actor's** clock, carried through from the milestone — when the driver says
// they took the photograph — and `accepted_at` is when the platform recorded it. The pair is
// Docs/02 §3.1's and it is the same pair [milestoneResponse] carries, named the same way on purpose.
//
// # Two shapes in one, and `exception_reason` is how a client tells them apart (SHIP-116)
//
// A reasoned exception carries no object, so it carries no key, no media type, no size and **no
// download URL** — none of them is signed, and there is nothing to sign one for. Every one of those
// fields is `omitempty`, so what a client receives is the six fields every row has plus either the
// photograph's five or `exception_reason`. Branching on `exception_reason` is the supported test;
// branching on a missing `download_url` would also work today and would be reading the absence of a
// credential as a fact about the delivery.
//
// The alternative — always present, `"object_key": ""`, `"content_length": 0` — was rejected because
// a zero length is a statement about a photograph that does not exist. An absent field says nothing;
// a zero says something false.
type proofResponse struct {
	ID          string `json:"id"`
	JobID       string `json:"job_id"`
	MilestoneID string `json:"milestone_id"`

	Milestone string `json:"milestone"`

	ObjectKey string `json:"object_key,omitempty"`

	ContentType   string `json:"content_type,omitempty"`
	ContentLength int64  `json:"content_length,omitempty"`

	DownloadURL       string `json:"download_url,omitempty"`
	DownloadExpiresAt string `json:"download_expires_at,omitempty"`

	// ExceptionReason is why this milestone has no photograph, from Docs/01 §4.4's three.
	//
	// It is the wire form of delivery.ProofExceptionReason and the stored form is the same
	// string, so there is no translation here — see that type for why it has none.
	ExceptionReason string `json:"exception_reason,omitempty"`

	RecordedAt string `json:"recorded_at"`
	AcceptedAt string `json:"accepted_at"`
}

// proofListResponse is the collection envelope of Docs/10 §4.5.
//
// `next_cursor` is always null and `has_more` always false: a delivery has at most five recordable
// milestones (Docs/01 §4.4) and at most one photograph each, so this is bounded by the domain rather
// than by a page size. The envelope is here anyway, because a client tells a collection from a single
// resource by its shape and not by knowing which endpoint it called — the reading `bidding`'s
// negotiation chain already takes of its own bound.
type proofListResponse struct {
	Data       []proofResponse `json:"data"`
	NextCursor *string         `json:"next_cursor"`
	HasMore    bool            `json:"has_more"`
}

func proofListFrom(links []ProofLink) proofListResponse {
	// A non-nil empty slice, so a job with nothing photographed answers `[]` rather than `null`.
	// Reachable and ordinary: every job before its first proof is in this state.
	data := make([]proofResponse, 0, len(links))
	for _, link := range links {
		data = append(data, proofResponse{
			ID:          link.ID.String(),
			JobID:       link.JobID.String(),
			MilestoneID: link.MilestoneID.String(),

			Milestone: link.Milestone.Wire(),

			ObjectKey: link.ObjectKey,

			ContentType:   link.ContentType,
			ContentLength: link.ContentLength,

			DownloadURL:       link.URL,
			DownloadExpiresAt: timestamp(link.ExpiresAt),

			ExceptionReason: string(link.ExceptionReason),

			RecordedAt: timestamp(link.RecordedAt),
			AcceptedAt: timestamp(link.AcceptedAt),
		})
	}
	return proofListResponse{Data: data}
}

// ProofOnJob handles GET /v1/jobs/{id}/proof (SHIP-115).
//
// # This is the access control, and it is the half of the ticket that is not the table
//
// A proof photograph identifies an address and a recipient, which makes it the most sensitive thing
// a delivery produces. Two parties may see it — the customer who owns the job and the provider it
// was awarded to — and the decision is made from the database rather than from a role claim, because
// `role: provider` says nothing about *this* delivery. [Service.ProofFor] argues who is on the list,
// who is not, and why the assigned driver and the administrator are both deferred.
//
// Everybody else gets the 404 a job that does not exist gets, for the reason [apiError] gives.
//
// # The URLs are minted per request and cannot be cached
//
// Each response signs a fresh short-lived URL per photograph. A client that caches the *response*
// caches expiring links, which is why `download_expires_at` is beside each one rather than left to
// be parsed out of the query string.
//
// # A GET, with no idempotency key
//
// It reads and writes nothing. Contrast `POST /v1/jobs/{id}/proof-uploads`, which is a POST despite
// writing nothing because it *issues a credential* — this hands out short-lived read links too, and
// the difference is that they reach an object the caller has just been authorised for rather than
// authorising a new one. Repeating a read is free; repeating a write of evidence is not.
func (h *Handler) ProofOnJob() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		readerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, err := jobIDFrom(r)
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		links, err := h.svc.ProofFor(r.Context(), pool, readerID, jobID)
		if err != nil {
			return apiError(err)
		}

		// Logged for the reason PresignProofUpload's line is: the fetch that follows never
		// touches this service, so this and the store's own access log are the only two records
		// that a photograph was reachable. **No URL is logged** — it is the credential.
		//
		// It is deliberately not the access log Docs/04 §6 requires of an administrator viewing
		// evidence. That is a durable record with a decision attached to it, and it is SHIP-155's.
		httpx.LoggerFrom(r.Context()).Info("proof of delivery was read",
			slog.String("job_id", jobID.String()),
			slog.Int("proof_count", len(links)))

		httpx.WriteJSON(w, http.StatusOK, proofListFrom(links))
		return nil
	})
}

// driverJobResponse is what a driver's link opens (SHIP-108).
//
// # It is the assignment, and deliberately not the delivery
//
// A driver opening their link wants the addresses, the goods and who to call, and **none of that is
// here**. That is SHIP-120's — "opening the link shows only that job's delivery detail" — and it
// needs a port into `jobs` that this domain has no reason to declare for a middleware ticket. What
// this answers is the smallest true thing: which job the link grants, which assignment it belongs
// to, and how long it lasts. SHIP-120 adds fields to this shape, which is an additive change
// (Docs/07 §6) and does not move the route.
//
// # The driver's mobile is not here, and that is a decision rather than an omission
//
// [assignmentResponse] returns it, because the provider typed it and needs to see it was read
// correctly. The driver already knows their own number, so returning it would buy nothing and cost
// something: a link travels through whatever channel the provider uses (Docs/01 §4.5), so it lands
// in a message thread and can be forwarded again. Whoever ends up holding it should learn as little
// as the endpoint can manage.
//
// The driver's *name* stays, because it is what tells them the link is theirs rather than the other
// driver's on the next job.
//
// The job's status is not echoed, for the reason [assignmentResponse] gives: it is `jobs`'
// vocabulary and a copy here would be a second list to keep in step.
type driverJobResponse struct {
	JobID        string `json:"job_id"`
	AssignmentID string `json:"assignment_id"`

	DriverName string `json:"driver_name"`

	AssignedAt string `json:"assigned_at"`

	// LinkExpiresAt is when this link stops working, in UTC.
	//
	// The same instant the provider was shown as `driver_token_expires_at`, named for what the
	// person reading it holds: the provider forwards a token, and the driver opens a link
	// (Docs/01 §4.5 calls it a link throughout). It is returned rather than left to be decoded out
	// of the credential, which is what the contract already tells clients not to do.
	LinkExpiresAt string `json:"link_expires_at"`

	// DeliveredAt is when this delivery was recorded as delivered, on the actor's clock, and is
	// absent until it has been (SHIP-123).
	//
	// # A sixth field on a shape whose closed set was the point
	//
	// SHIP-108 held this response to five fields and a test and a `make verify` check both say so,
	// because "what may a driver see" is a decision the endpoint's response *is*. This is an
	// additive change (Docs/07 §6) made on purpose and with both guards updated, not around them.
	//
	// # It is not the job's status wearing a different name
	//
	// The status vocabulary stays `jobs`', and this domain still holds no copy of it. What this
	// reports is a fact about `milestones`, which is this domain's own table: whether a 'Delivered'
	// milestone has been recorded on this job. The two are not the same question — SHIP-112 records
	// milestones that move nothing — and this is the one the portal needs.
	//
	// # Why the portal cannot do without it
	//
	// SHIP-123's *Done when* ends "portal becomes read-only after", and a driver reloads. Component
	// state does not survive that, `sessionStorage` would be the page inventing a fact about the
	// delivery, and there is no driver-readable milestone list. So the platform answers it, which is
	// also where Docs/07 §3 puts every question of this kind: the app may hide or disable, and the
	// platform decides.
	//
	// **It is presentation rather than authorisation.** The link keeps opening the page and the
	// endpoints keep answering after a delivery is finished — a driver may reasonably reopen it to
	// check what they recorded, and recording a milestone on a delivered job is still refused by the
	// transition guard rather than by this field.
	DeliveredAt string `json:"delivered_at,omitempty"`
}

func driverJobFrom(a Assignment, grant DriverGrant, deliveredAt time.Time) driverJobResponse {
	return driverJobResponse{
		JobID:        a.JobID.String(),
		AssignmentID: a.ID.String(),

		DriverName: a.DriverName,

		AssignedAt:    timestamp(a.CreatedAt),
		LinkExpiresAt: timestamp(grant.ExpiresAt),
		DeliveredAt:   timestamp(deliveredAt),
	}
}

// DriverJob handles GET /v1/driver/jobs/{id} (SHIP-108).
//
// # This is the route that makes the *Done when* demonstrable, and it is deliberately the smallest
//
// SHIP-108 is a middleware ticket: it needs one route declaring RequireDriverToken so that "grants
// exactly one job" and "cannot be exchanged for a user session" can be shown over HTTP rather than
// only in Go. A read is the right size for that — it needs no idempotency scope (Docs/11 §9 records
// that a driver-token request scopes its key to `anonymous`, which bites a write and not a read) —
// and the delivery detail behind it belongs to SHIP-120.
//
// # Why the job is in the path when the token already names it
//
// It looks redundant and it is the mechanism. The path is the client's statement of what it means to
// act on; the token is the platform's statement of what the caller may act on; comparing them is what
// makes "exactly one job" **observable from outside** rather than merely true inside. Drop the id and
// a grant that had silently widened would be undetectable — there would be no other job to ask for.
//
// It is also what the driver half of M4 needs anyway: SHIP-121 records milestones per job, a driver
// carrying two deliveries holds two links, and two links that resolved to one URL would be
// indistinguishable in a browser's history.
//
// # The handler never reads the path, and that is the other half
//
// The job comes from the grant, which the guard has already checked against the path. There is no
// call to [jobIDFrom] here and there must not be: the two would agree today and a handler that read
// the path directly is one refactor away from being the only thing deciding.
func (h *Handler) DriverJob() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		grant, ok := driverGrantFrom(r.Context())
		if !ok {
			// The route declares RequireDriverToken, so the guard has run and a grant is
			// guaranteed by the time this executes. Reaching here means the route was declared
			// with the wrong auth class — a wiring defect the caller can do nothing about, and
			// the same call [callerID] makes about a missing subject.
			return httpx.NewError(http.StatusInternalServerError, httpx.CodeInternal,
				"Something went wrong at our end.").WithCause(errors.New(
				"delivery: a driver route was reached with no verified grant on the context; " +
					"its Auth class is not RequireDriverToken"))
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		assignment, err := h.svc.AssignmentFor(r.Context(), pool, grant)
		if err != nil {
			return apiError(err)
		}

		// Asked after the assignment and never before it: a caller whose link no longer opens the
		// delivery is refused by the line above, and this must not become a way to learn that a job
		// has been delivered by presenting a superseded link.
		deliveredAt, _, err := h.svc.DeliveryFinishedFor(r.Context(), pool, grant)
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, driverJobFrom(assignment, grant, deliveredAt))
		return nil
	})
}

// recordingFrom turns the request body and the idempotency header into what the domain takes.
//
// Both failures it reports are failures to *read* the request rather than to validate it, which is
// why they are here and not in [Recording.problems]: neither an unrecognised milestone nor a
// timestamp that is not one leaves a value the domain could be given to judge.
//
// The wire form is translated in this direction only. `Picked up` sent verbatim is refused like any
// other unknown string — a client that sent the stored form has misread the contract, and accepting
// both spellings would make two forms interchangeable in one direction and not the other.
//
// The key is read from the header rather than from the body. It identifies the *request*, and a
// client able to put a different value in each would have two answers to which one a milestone was
// recorded under.
func recordingFrom(req recordMilestoneRequest, key string) (Recording, string, error) {
	var problems validate.Errors

	var milestone Milestone
	parsed, known := MilestoneFromWire(req.Milestone)
	switch {
	case req.Milestone == "":
		// Left as the zero value so the domain's Required check reports it. A client that
		// sent nothing should be told the field is required, not that "" is not a milestone.

	case known:
		milestone = parsed

	default:
		problems.Add("milestone", validate.CodeInvalid,
			"That is not a milestone. Use one of %s.", strings.Join(recordableWire(), ", "))
	}

	var recordedAt time.Time
	if req.RecordedAt != "" {
		parsed, err := time.Parse(time.RFC3339, req.RecordedAt)
		if err != nil {
			problems.Add("recorded_at", validate.CodeInvalid,
				"Send the time you recorded this as RFC 3339, like 2026-08-12T06:40:11Z.")
		} else {
			recordedAt = parsed
		}
	}

	// The evidence: one object key, or one reason there is none, and never both (SHIP-116).
	//
	// The object key is read and checked for being *there*, and nothing else is decided here
	// (SHIP-115). Whether it names this job, whether the object exists and whether the platform
	// will accept what it holds are all [Service.VerifyProof]'s, because two of the three need a
	// database and a store and the third belongs beside them rather than split off.
	//
	// The reason **is** decided here, and the asymmetry is the point: there is nothing to look it
	// up against. It is a selection from three values the platform published, so the only thing
	// that can be wrong with it is that it is not one of them, and that is answerable from the
	// string alone.
	var (
		proofKey  string
		exception ProofExceptionReason
	)
	if req.Proof != nil {
		proofKey = strings.TrimSpace(req.Proof.ObjectKey)
		reason := strings.TrimSpace(req.Proof.ExceptionReason)

		switch {
		case proofKey != "" && reason != "":
			problems.Add("proof.exception_reason", validate.CodeNotAllowed,
				"Send the photograph you uploaded or a reason there is none, not both.")

		case proofKey == "" && reason == "":
			// Reported against `object_key`, because a client that sent `"proof": {}` was
			// almost certainly trying to send a photograph and lost the key on the way. The
			// message names the other door.
			problems.Add("proof.object_key", validate.CodeRequired,
				"Send the object_key you were given when you asked for an upload URL, or an "+
					"exception_reason if there is no photograph.")

		case reason != "":
			// Case-sensitive, which is [MilestoneFromWire]'s call and made here for the same
			// reason: a client sending `Recipient_Objected` has misread the contract, and
			// accepting a second spelling would make two forms interchangeable in one
			// direction and not the other. The domain checks it again — see
			// [Recording.problems] — because a rule that holds only for callers who came in
			// through this decoder is not a rule.
			exception = ProofExceptionReason(reason)
			if !exception.Valid() {
				problems.Add("proof.exception_reason", validate.CodeInvalid,
					"That is not a reason a photograph can be missing. Use one of %s.",
					strings.Join(proofExceptionWire(), ", "))
			}
		}
	}

	if err := problems.Err(); err != nil {
		return Recording{}, "", err
	}

	return Recording{
		Milestone:  milestone,
		RecordedAt: recordedAt,
		Reason:     req.Reason,
		Key:        key,
		Exception:  exception,

		// Read and passed through untrimmed: [Recording.normalise] collapses whitespace and
		// [Recording.problems] judges what is left, so a name that is only spaces is refused as
		// required rather than stored. Deciding either here would put the rule in the decoder,
		// where a second caller would not get it.
		RecipientName: req.RecipientName,
		DeliveryNote:  req.DeliveryNote,
	}, proofKey, nil
}

// jobIDFrom reads and parses the {id} path parameter.
//
// A path parameter of the wrong shape is bad_request rather than not_found, which is the httpx
// registry's own description of that code. It also discloses nothing: the answer is the same whether
// or not any job exists.
func jobIDFrom(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return uuid.Nil, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"The job id in the path is not a valid identifier.").WithCause(err)
	}
	return id, nil
}

// callerID is the authenticated provider's account id.
//
// The route declares RequireUser, so a subject is guaranteed by the time a handler runs — which is
// why the absence of one is reported as an internal failure rather than as a 401. Reaching here
// without a subject means a route was declared public and written as though it were protected, and
// that is a wiring defect the caller can do nothing about.
//
// The *role* is deliberately not read from the subject, and here it is not read at all: being the
// provider on the accepted bid is a stronger statement than carrying `role: provider`, and it is the
// one Docs/02 §3 actually makes.
func callerID(ctx context.Context) (uuid.UUID, error) {
	subject := authctx.MustSubject(ctx)

	id, err := uuid.Parse(subject.UserID)
	if err != nil {
		return uuid.Nil, httpx.NewError(http.StatusInternalServerError, httpx.CodeInternal,
			"Something went wrong at our end.").WithCause(err)
	}
	return id, nil
}

// database returns the pool, or the answer to give while there is not one.
//
// 503 rather than 500, because the two say different things to a mobile client: retry, or surface a
// failure to the person holding the phone. Docs/10 §9.2 is explicit that the pool may be nil — the
// service starts with an unreachable database on purpose — so this is an expected condition rather
// than a defect, and it is logged at warning level for exactly that reason.
func (h *Handler) database(r *http.Request) (*pgxpool.Pool, error) {
	if h.pool != nil {
		return h.pool, nil
	}

	httpx.LoggerFrom(r.Context()).Warn("a delivery request arrived with no database connection")
	return nil, httpx.NewError(http.StatusServiceUnavailable, httpx.CodeUnavailable,
		"This cannot be completed right now. Try again shortly.")
}

// apiError turns this domain's errors into the API's error contract.
//
// The mapping lives at the transport edge on purpose: the service answers in domain terms, and which
// HTTP status a provider who was not awarded the job deserves is not a question the domain has an
// opinion about. A *httpx.Error passes straight through, because validation already produced one in
// the right shape.
//
// # Why another provider's job is 404 and not 403
//
// 403 would confirm that the job exists and that somebody was awarded it. Which jobs a competitor
// won is commercial information they never published, and httpx's own description of `not_found`
// says the two cases are deliberately indistinguishable wherever telling them apart would confirm
// the existence of something private.
//
// The domain still distinguishes them ([ErrNotAwardedProvider] against [ErrJobNotFound]) so that a
// test can tell "the stranger was refused" from "the job silently stopped existing".
//
// Anything unrecognised is returned as-is and becomes an opaque 500 in httpx.WriteError, which logs
// the cause against the request id (SHIP-15i). That is the correct default: an error nobody has
// given a status and a code has not been considered.
func apiError(err error) error {
	var apiErr *httpx.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}

	switch {
	case errors.Is(err, ErrJobNotFound), errors.Is(err, ErrNotAwardedProvider):
		return httpx.NewError(http.StatusNotFound, httpx.CodeNotFound,
			"No such job.").WithCause(err)

	case errors.Is(err, ErrDriverLinkSuperseded):
		// 404 rather than 401, and the distinction is not pedantic: the credential is fine, and
		// what has gone is the assignment behind it. Telling a stood-down driver "your link is
		// invalid" would send them back to the provider for a *new link*, which is not what they
		// need. The same answer a job that never existed gets, for the reason apiError gives
		// above — and SHIP-109, which is the ticket that makes this reachable, is the right place
		// to decide whether a reissued link deserves a code of its own.
		return httpx.NewError(http.StatusNotFound, httpx.CodeNotFound,
			"No such delivery.").WithCause(err)

	case errors.Is(err, ErrJobNotAssignable):
		return httpx.NewError(http.StatusConflict, CodeJobNotAssignable,
			"A driver can only be assigned to a job that has been awarded and has not set off yet.").
			WithCause(err)

	case errors.Is(err, ErrDriverAlreadyAssigned):
		return httpx.NewError(http.StatusConflict, CodeDriverAlreadyAssigned,
			"This job already has a driver.").WithCause(err)

	case errors.Is(err, ErrNoIdempotencyKey):
		// The middleware answers this before a handler runs, so reaching it means the route
		// was served without the middleware. Answering with the middleware's own code keeps
		// one thing for a client to branch on either way.
		return httpx.NewError(http.StatusBadRequest, httpx.CodeIdempotencyKeyRequired,
			"This request must carry an %s header. Generate one value per action and reuse it "+
				"for every retry of that action.", httpx.HeaderIdempotencyKey).WithCause(err)

	case errors.Is(err, ErrIdempotencyKeyReused):
		// The same code the middleware uses on a fingerprint mismatch, and the same status.
		// A client should not have to know whether Redis still held the key or the database
		// caught it — the answer to both is "generate a new key for the new action".
		return httpx.NewError(http.StatusConflict, httpx.CodeIdempotencyKeyReused,
			"This %s has already recorded a different milestone on this job. Generate a new key "+
				"for each action.", httpx.HeaderIdempotencyKey).WithCause(err)

	case errors.Is(err, ErrProofRequired):
		// 409 rather than 422, and the difference is which thing is wrong: the body is a
		// perfectly good milestone recording. What it contradicts is a rule about deliveries,
		// and a client fixes it by capturing something rather than by correcting a field.
		return httpx.NewError(http.StatusConflict, CodeProofRequired,
			"A delivery is recorded with a photograph, or with a reason there is none.").
			WithCause(err)

	case errors.Is(err, ErrProofNotForThisJob):
		// 422 with the field named, because it is the caller's own body disagreeing with the
		// caller's own path — the same treatment an unaccepted content type gets, and it
		// discloses nothing: the key names the other job in plain text and the caller sent it.
		//
		// Built here rather than in [recordingFrom] because the job in the path is not something
		// that function is given, and passing it in so the check could live there would put an
		// authorisation rule in the request decoder.
		var problems validate.Errors
		problems.Add("proof.object_key", validate.CodeInvalid,
			"That object_key was issued for a different job. Ask for an upload URL on this job "+
				"and send the key it answers with.")
		return problems.Err()

	case errors.Is(err, ErrProofNotUploaded):
		return httpx.NewError(http.StatusConflict, CodeProofNotUploaded,
			"That photograph has not reached us. Finish uploading it, then record the milestone "+
				"again.").WithCause(err)

	case errors.Is(err, ErrProofRejected):
		return httpx.NewError(http.StatusConflict, CodeProofRejected,
			"That file is not a photograph this platform accepts.").WithCause(err)

	case errors.Is(err, ErrProofAlreadyRecorded):
		return httpx.NewError(http.StatusConflict, CodeProofAlreadyRecorded,
			"That photograph is already the proof for another milestone. Ask for a new upload "+
				"URL and send it again.").WithCause(err)

	case errors.Is(err, ErrMilestoneNotPermitted):
		// Since SHIP-112 this can only mean "too early". A milestone the delivery has already
		// passed is absorbed and never reaches here, so the message says which direction the
		// conflict is in rather than leaving a client to work out whether retrying is futile.
		return httpx.NewError(http.StatusConflict, CodeMilestoneNotPermitted,
			"This delivery has not reached the point where that milestone can be recorded.").WithCause(err)

	default:
		return err
	}
}

// --- the delivery read shelf (SHIP-115a) --------------------------------------------------------

// deliveryDetailResponse is what a party to a job is shown about who is carrying it.
//
// # `driver_assigned` is a field rather than the absence of the others
//
// A job that has been awarded and has no driver yet is an ordinary state, not a missing resource, so
// this answers `200` with `driver_assigned: false` rather than `404`. A client that branched on a
// missing `driver_name` would be reading the absence of a field as a fact about the delivery, which
// is the same mistake the proof contract warns against for `download_url`.
//
// # `driver_mobile` is present for the provider and absent for the customer
//
// `omitempty`, because the domain does not hand this handler the number when the reader is the
// customer — see [Delivery]. That is `Docs/01` §4's "minimise exposure of phone numbers" applied to
// a person who has no account and no way to consent: the provider typed the number, and giving back
// what somebody supplied is not exposure.
//
// # The job's status is not echoed
//
// The same call [assignmentResponse] and [driverJobResponse] make. The status vocabulary is `jobs`'
// and a copy here would be a second list to keep in step; a client that needs it reads the job.
type deliveryDetailResponse struct {
	JobID string `json:"job_id"`

	DriverAssigned bool `json:"driver_assigned"`

	AssignmentID string `json:"assignment_id,omitempty"`
	DriverName   string `json:"driver_name,omitempty"`
	DriverMobile string `json:"driver_mobile,omitempty"`
	AssignedAt   string `json:"assigned_at,omitempty"`
}

func deliveryDetailFrom(d Delivery) deliveryDetailResponse {
	body := deliveryDetailResponse{JobID: d.JobID.String(), DriverAssigned: d.Assigned}
	if !d.Assigned {
		return body
	}

	body.AssignmentID = d.AssignmentID.String()
	body.DriverName = d.DriverName
	body.DriverMobile = d.DriverMobile
	body.AssignedAt = timestamp(d.AssignedAt)
	return body
}

// DeliveryDetail handles GET /v1/jobs/{id}/delivery/detail (SHIP-115a).
//
// # Five segments, and four would stop the process
//
// `GET /v1/jobs/{id}/delivery` and `GET /v1/jobs/open/{id}` both match `/v1/jobs/open/delivery` with
// neither more specific, and Go's ServeMux panics at registration rather than serving a route that
// answers oddly. This is the shelf SHIP-115 opened with `/delivery/proof` for exactly that reason;
// see read.go's header.
//
// RequireUser, and the auth class is not the access control: it gets a caller as far as the handler,
// and [Service.DeliveryFor] asks the database which of two parties they are. Being neither answers
// exactly what a job that does not exist answers.
func (h *Handler) DeliveryDetail() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		readerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, err := jobIDFrom(r)
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		detail, err := h.svc.DeliveryFor(r.Context(), pool, readerID, jobID)
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, deliveryDetailFrom(detail))
		return nil
	})
}

// milestoneCursorFields is how many values a milestone cursor carries: the actor's clock, and the
// identifier that breaks its ties.
const milestoneCursorFields = 2

// encodeMilestoneCursor is the cursor that resumes after this record.
//
// RFC 3339 with nanoseconds rather than the millisecond precision responses render, which is the
// same call jobs' own cursor makes: this value is compared against a `timestamptz` rather than
// displayed, and a rounded rendering would put the page boundary inside a group of rows sharing the
// rounded value — repeating some and skipping others.
func encodeMilestoneCursor(rec Record) string {
	return pagination.Cursor{
		rec.ActorRecordedAt.UTC().Format(time.RFC3339Nano),
		rec.ID.String(),
	}.Encode()
}

// decodeMilestoneCursor reads one back.
//
// pagination.Decode establishes the shape — this version, this many fields — and this establishes
// the meaning. Only the domain knows that its ordering key is a timestamp and a UUID, and a cursor
// whose fields decode but do not parse has to be refused here: reaching the query as a zero time
// would silently answer with the first page, which is a client's list quietly starting again.
func decodeMilestoneCursor(raw string) (milestoneCursor, error) {
	fields, err := pagination.Decode(raw, milestoneCursorFields)
	if err != nil || fields == nil {
		return milestoneCursor{}, err
	}

	at, err := time.Parse(time.RFC3339Nano, fields[0])
	if err != nil {
		return milestoneCursor{}, invalidMilestoneCursor(err)
	}
	id, err := uuid.Parse(fields[1])
	if err != nil {
		return milestoneCursor{}, invalidMilestoneCursor(err)
	}
	return milestoneCursor{recordedAt: at, id: id, set: true}, nil
}

func invalidMilestoneCursor(cause error) error {
	return httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
		"The cursor is not one this endpoint issued. Ask for the first page without one.").
		WithCause(cause)
}

// MilestonesOnJob handles GET /v1/jobs/{id}/delivery/milestones (SHIP-115a).
//
// Every milestone recorded on the delivery, newest by the actor's clock first, in the collection
// envelope of Docs/10 §4.5 — which is what lets a client tell a collection from a single resource
// without knowing the endpoint.
//
// **It pages and `/delivery/proof` does not**, and read.go's [Service.MilestonesFor] argues why:
// 000601 deliberately has no uniqueness on `(job_id, milestone)`, so a delivery that goes badly
// accumulates repeats and this collection has no domain bound to stand on.
//
// The same two-party check as the operation above, with the same 404 for anybody else.
func (h *Handler) MilestonesOnJob() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		readerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, err := jobIDFrom(r)
		if err != nil {
			return err
		}

		limit, err := pagination.Limit(r.URL.Query().Get("limit"))
		if err != nil {
			return err
		}
		after, err := decodeMilestoneCursor(r.URL.Query().Get("cursor"))
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		records, hasMore, err := h.svc.MilestonesFor(r.Context(), pool, readerID, jobID,
			MilestonePage{After: after, Limit: limit})
		if err != nil {
			return apiError(err)
		}

		page := make([]milestoneResponse, 0, len(records))
		for _, rec := range records {
			page = append(page, milestoneFrom(rec))
		}

		var next string
		if hasMore && len(records) > 0 {
			next = encodeMilestoneCursor(records[len(records)-1])
		}

		httpx.WriteJSON(w, http.StatusOK, pagination.NewPage(page, next))
		return nil
	})
}

// --- reissuing a driver's link (SHIP-109) -------------------------------------------------------

// driverLinkResponse is a freshly issued link, and the record of how many this assignment has had.
//
// **It carries a credential**, exactly as [assignmentResponse] does, and it is scoped like one: the
// idempotency middleware stores it under `idem:v1:user:<provider>:<key>` (SHIP-44), so a replay
// reaches only the provider who asked. The driver's name and mobile are deliberately not repeated
// here — this operation is about the link, and the assignment is read from `/delivery/detail`.
//
// The URL is not assembled, for the reason [assignmentResponse] gives: the driver portal's landing
// route is the portal's to define, and a base URL in this response would be this domain asserting a
// path in an application it does not own.
type driverLinkResponse struct {
	JobID        string `json:"job_id"`
	AssignmentID string `json:"assignment_id"`

	DriverToken          string `json:"driver_token"`
	DriverTokenExpiresAt string `json:"driver_token_expires_at"`

	// IssuedAt and IssueCount are what tells a provider they are looking at a new link rather
	// than the one they already forwarded. The count is the only trace a revocation leaves —
	// the previous link's identifier is overwritten rather than kept (000606).
	IssuedAt   string `json:"issued_at"`
	IssueCount int    `json:"issue_count"`
}

func driverLinkFrom(a Assignment, token DriverToken) driverLinkResponse {
	return driverLinkResponse{
		JobID:        a.JobID.String(),
		AssignmentID: a.ID.String(),

		DriverToken:          token.Value,
		DriverTokenExpiresAt: timestamp(token.ExpiresAt),

		IssuedAt:   timestamp(a.LinkIssuedAt),
		IssueCount: a.LinkIssueCount,
	}
}

// ReissueDriverLink handles POST /v1/jobs/{id}/driver/link (SHIP-109).
//
// A noun under the assignment rather than a verb — `POST /jobs/{id}/driver/revoke` would name the
// half of this that destroys and not the half that a provider actually wants, which is a working
// link to forward. Posting to the collection of links this assignment has had is what happens: one
// more is issued, and the previous one stops opening anything the moment this commits.
//
// 200 rather than 201, and that is deliberate. A link is not a resource this API serves — there is
// no `GET /jobs/{id}/driver/link`, and there never will be, because the platform keeps no copy of a
// token it has issued. What comes back is a credential minted for this request.
//
// **RequireUser, on a route whose whole subject is the driver's credential.** The caller is the
// provider, authenticated the ordinary way; a driver cannot reissue their own link, which is the
// point of a link a provider controls. The pairing is the same one `POST /jobs/{id}/driver` has:
// this route mints a driver token and does not accept one.
func (h *Handler) ReissueDriverLink() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, err := jobIDFrom(r)
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		var (
			assignment Assignment
			token      DriverToken
		)
		err = db.InTx(r.Context(), pool, func(ctx context.Context, runner db.Runner) error {
			var err error
			assignment, token, err = h.svc.ReissueDriverLink(ctx, runner, providerID, jobID)
			return err
		})
		if err != nil {
			return apiError(err)
		}

		// Logged because a revocation is otherwise invisible: the previous identifier is
		// overwritten rather than kept (000606), so this line and `link_issue_count` are the
		// whole record that a link was ended. **Neither token is logged** — the outgoing one is
		// the credential itself, and the incoming one is what a log reader could use to work out
		// which link a driver was refused on.
		httpx.LoggerFrom(r.Context()).Info("a driver link was reissued and the previous one stopped working",
			slog.String("job_id", jobID.String()),
			slog.String("assignment_id", assignment.ID.String()),
			slog.Int("issue_count", assignment.LinkIssueCount))

		httpx.WriteJSON(w, http.StatusOK, driverLinkFrom(assignment, token))
		return nil
	})
}
