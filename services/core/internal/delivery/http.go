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
type recordMilestoneRequest struct {
	Milestone  string `json:"milestone"`
	RecordedAt string `json:"recorded_at"`
	Reason     string `json:"reason"`
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
// nothing at all. Since SHIP-112 there are two ways for that to happen and this shape distinguishes
// neither of them; [Outcome] is where the decision not to is argued.
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

		recording, err := recordingFrom(req, r.Header.Get(httpx.HeaderIdempotencyKey))
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
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
	})
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
}

func driverJobFrom(a Assignment, grant DriverGrant) driverJobResponse {
	return driverJobResponse{
		JobID:        a.JobID.String(),
		AssignmentID: a.ID.String(),

		DriverName: a.DriverName,

		AssignedAt:    timestamp(a.CreatedAt),
		LinkExpiresAt: timestamp(grant.ExpiresAt),
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

		httpx.WriteJSON(w, http.StatusOK, driverJobFrom(assignment, grant))
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
func recordingFrom(req recordMilestoneRequest, key string) (Recording, error) {
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

	if err := problems.Err(); err != nil {
		return Recording{}, err
	}

	return Recording{
		Milestone:  milestone,
		RecordedAt: recordedAt,
		Reason:     req.Reason,
		Key:        key,
	}, nil
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
		return httpx.NewError(http.StatusConflict, CodeProofRequired,
			"A delivery cannot be recorded without proof, and capturing proof is not built yet.").
			WithCause(err)

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
