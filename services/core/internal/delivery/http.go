// The HTTP surface of the delivery domain.
//
// Handlers live in the domain rather than in cmd/api (Docs/10 §2.1). The reason is a merge hazard
// rather than taste: it keeps cmd/api/routes.go free of per-domain edits, which is what lets two
// domains be built at once without touching a shared file. A domain importing internal/httpx is
// sitting on infrastructure, not crossing a boundary, and the import lint permits it.
//
// # These endpoints are called by the provider, not by the driver
//
// Worth stating first, because the domain's name invites the opposite assumption. The driver has no
// account and no session — the portal is link-authenticated (Docs/07 §3) — and the job-scoped token
// they hold is a separate system that cannot be exchanged for a user token in either direction
// (Docs/10 §5). The caller of every route below is the awarded provider, authenticated the ordinary
// way, and each declares RequireUser like every other product endpoint in the service.
//
// **The assignment response now carries a driver token, and that does not change the sentence
// above** (SHIP-107). The provider is handed the link to forward (Docs/01 §4.5); nothing here reads
// one, no route accepts one, and the middleware that would is SHIP-108's.
//
// # There is no field for whose job, and no field for who is assigning
//
// The provider is whoever the token says is calling. A provider id in the request would be an
// authorisation decision made from client input, which Docs/07 §3 puts on the platform. The job is
// named in the path and the platform checks it against the accepted bid.
//
// The blank line below keeps this a file note rather than a second package comment.

package delivery

import (
	"context"
	"errors"
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
// nothing at all.
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
			record   Record
			recorded bool
		)
		err = db.InTx(r.Context(), pool, func(ctx context.Context, runner db.Runner) error {
			var err error
			record, recorded, err = h.svc.RecordMilestone(ctx, runner, providerID, jobID, recording)
			return err
		})
		if err != nil {
			return apiError(err)
		}

		if !recorded {
			// Logged because it is the signal that the middleware's entry had gone and the
			// database caught the retry instead. That is the mechanism working, and it is
			// otherwise invisible: the response is indistinguishable from an ordinary one.
			httpx.LoggerFrom(r.Context()).Info("a milestone retry was answered from the record rather than recorded again",
				slog.String("job_id", jobID.String()),
				slog.String("milestone", record.Milestone.Wire()),
				slog.String("milestone_id", record.ID.String()))

			httpx.WriteJSON(w, http.StatusOK, milestoneFrom(record))
			return nil
		}

		httpx.WriteJSON(w, http.StatusCreated, milestoneFrom(record))
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
		return httpx.NewError(http.StatusConflict, CodeMilestoneNotPermitted,
			"This milestone cannot be recorded from the job's current status.").WithCause(err)

	default:
		return err
	}
}
