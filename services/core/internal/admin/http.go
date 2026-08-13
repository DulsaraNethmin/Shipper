// The HTTP surface of the admin domain.
//
// Handlers live in the domain rather than in cmd/api (Docs/10 §2.1). The reason is a merge hazard
// rather than taste: it keeps cmd/api/routes.go free of per-domain edits, which is what lets two
// domains be built at once without touching a shared file. A domain importing internal/httpx is
// sitting on infrastructure, not crossing a boundary, and the import lint permits it.
//
// # This endpoint is called by a customer or a provider, not by an administrator
//
// Worth stating first, because the domain's name invites the opposite assumption. Docs/04 §7's
// intake stage is a party to a delivery reporting that something went wrong, and the route declares
// RequireUser like every other product endpoint in the service. Administrator sign-in is a separate
// system that a user token can never reach (SHIP-147); the middleware behind RequireAdmin does not
// exist, and a route declaring that class stops the process at startup rather than being served
// open. The investigation and outcome stages are SHIP-164 and arrive with it.
//
// # There is no field for whose job, and no field for who is complaining
//
// The complainant is whoever the token says is calling, and which side of the job they were on is
// resolved by the platform from the job and its accepted bid. A `complainant` in the request would
// be an authorisation decision made from client input, which Docs/07 §3 puts on the platform. The
// job is named in the path.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

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
// It is built in cmd/api/routes_admin.go from Deps. The pool is held here rather than inside the
// service because Docs/10 §3.2 puts the transaction boundary with whoever owns the invariant, and
// for intake that is this layer: the party check, the dispute row and the status change are one
// decision against one version of the job.
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
		return nil, errors.New("admin: a handler needs a service")
	}
	if log == nil {
		return nil, errors.New("admin: a handler needs a logger")
	}
	return &Handler{svc: svc, pool: pool, log: log}, nil
}

// raiseDisputeRequest is the body of POST /v1/jobs/{id}/disputes.
//
// Docs/04 §7's intake fields, minus the two the platform supplies:
//
//	{"category": "goods_damaged_or_missing",
//	 "description": "Two of the four crates arrived with the sides staved in.",
//	 "desired_outcome": "A record of the damage, and the provider contacted about it.",
//	 "occurred_at": "2026-08-11T15:40:00Z",
//	 "evidence": ["Photographed the crates at the depot", "The driver's message of 11/08"]}
//
// There is no `job_id` and no `complainant`. The job is in the path and the complainant is the
// token. httpx.DecodeJSON refuses unknown fields, so a client that sends either is told the field
// does not exist rather than having it quietly ignored.
type raiseDisputeRequest struct {
	Category       string   `json:"category"`
	Description    string   `json:"description"`
	DesiredOutcome string   `json:"desired_outcome"`
	OccurredAt     string   `json:"occurred_at"`
	Evidence       []string `json:"evidence"`
}

// disputeResponse is a dispute as its complainant sees it.
//
// **The job's status is deliberately not echoed here.** The status vocabulary is `jobs`' own and its
// wire form is `jobs`' to map (Docs/10 §4.7); a copy of the string "disputed" in this package would
// be a second list to keep in step for the sake of a field the client already knows the value of —
// a successful response to this endpoint means the job is frozen and nothing else.
//
// **Nothing an administrator writes appears here, and nothing ever will.** SHIP-162's internal notes
// are never user-visible, and this shape is the reason 000800 has no column for them: every field
// below is read straight back to the complainant, so a note written into one would be a note the
// complainant reads. The administrator's view of a dispute (SHIP-164) is a shape of its own rather
// than this one reached by another route, for the same reason `jobs` keeps the customer's and the
// provider's views apart.
//
// The idempotency key is not echoed either. It is the client's own value coming back at it, and
// putting it in a response invites a client to treat it as an identifier the platform issued.
type disputeResponse struct {
	ID    string `json:"id"`
	JobID string `json:"job_id"`

	// RaisedBy is which side of the job the complainant was on, not who they are. The client
	// knows who it is; what it cannot know without being told is whether the platform agreed
	// they were the customer or the provider here.
	RaisedBy string `json:"raised_by"`

	Category       string `json:"category"`
	Description    string `json:"description"`
	DesiredOutcome string `json:"desired_outcome"`

	Evidence []string `json:"evidence"`

	// OccurredAt is when the complainant says it happened; RaisedAt is when the platform
	// received the report. Two fields, never one — a client that renders only the second is
	// showing a filing time as though it were the incident.
	OccurredAt string `json:"occurred_at"`
	RaisedAt   string `json:"raised_at"`
}

func disputeFrom(d Dispute) disputeResponse {
	// Never nil on the wire. An absent list and an empty one are the same fact here — no
	// evidence was described — and `null` makes a client that iterates without checking crash on
	// the one dispute that had none.
	evidence := d.Evidence
	if evidence == nil {
		evidence = []string{}
	}

	return disputeResponse{
		ID:    d.ID.String(),
		JobID: d.JobID.String(),

		RaisedBy: string(d.ComplainantParty),

		Category:       d.Category.Wire(),
		Description:    d.Description,
		DesiredOutcome: d.DesiredOutcome,

		Evidence: evidence,

		OccurredAt: timestamp(d.OccurredAt),
		RaisedAt:   timestamp(d.CreatedAt),
	}
}

// timestamp renders an instant the way every other endpoint does, in UTC with milliseconds.
func timestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// RaiseDispute handles POST /v1/jobs/{id}/disputes (SHIP-163).
//
// A collection under the job, and plural: Docs/02 §2 lets a job be disputed more than once over its
// life — resolved, returned to the lifecycle, and disputed again — while never having two open at
// once. `POST /jobs/{id}/dispute` would have read as a verb on the job, and the thing being created
// is a record with an identifier, a workflow and an outcome of its own.
//
// 201 when a dispute was raised, 200 when this idempotency key had already raised it and nothing was
// written. Both carry the same shape, so a client that does not care which happened parses one type.
//
// # The 200 is the ticket, not a nicety
//
// SHIP-15's middleware replays the stored response for a repeated key and this handler is never
// reached — while the entry lives. The path that matters is the one after it expires or is evicted:
// the handler runs again, the unique index refuses the second row, and this answers with the dispute
// the first attempt raised. Without it, a phone that retried after a long outage would be told its
// own dispute was somebody else's.
//
// The transaction is opened here. The dispute and the job's move into 'Disputed' commit together or
// neither does — a complaint against a job the platform still believes is running normally is a
// state no support queue can read, and a frozen job with nothing to resolve is one nothing can
// unfreeze.
func (h *Handler) RaiseDispute() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		complainantID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, err := jobIDFrom(r)
		if err != nil {
			return err
		}

		var req raiseDisputeRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		intake, err := intakeFrom(req, r.Header.Get(httpx.HeaderIdempotencyKey))
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		var (
			dispute Dispute
			raised  bool
		)
		err = db.InTx(r.Context(), pool, func(ctx context.Context, runner db.Runner) error {
			var err error
			dispute, raised, err = h.svc.RaiseDispute(ctx, runner, complainantID, jobID, intake)
			return err
		})
		if err != nil {
			return apiError(err)
		}

		if !raised {
			// Logged because it is the signal that the middleware's entry had gone and the
			// database caught the retry instead. That is the mechanism working, and it is
			// otherwise invisible: the response is indistinguishable from an ordinary one.
			httpx.LoggerFrom(r.Context()).Info("a dispute retry was answered from the record rather than raised again",
				slog.String("job_id", jobID.String()),
				slog.String("dispute_id", dispute.ID.String()))

			httpx.WriteJSON(w, http.StatusOK, disputeFrom(dispute))
			return nil
		}

		httpx.WriteJSON(w, http.StatusCreated, disputeFrom(dispute))
		return nil
	})
}

// intakeFrom turns the request body and the idempotency header into what the domain takes.
//
// Both failures it reports are failures to *read* the request rather than to validate it, which is
// why they are here and not in [Intake.problems]: neither an unrecognised category nor a timestamp
// that is not one leaves a value the domain could be given to judge.
//
// The wire form is translated in this direction only. `Goods damaged or missing` sent verbatim is
// refused like any other unknown string — a client that sent the stored form has misread the
// contract, and accepting both spellings would make two forms interchangeable in one direction and
// not the other.
//
// The key is read from the header rather than from the body. It identifies the *request*, and a
// client able to put a different value in each would have two answers to which one a dispute was
// raised under.
func intakeFrom(req raiseDisputeRequest, key string) (Intake, error) {
	var problems validate.Errors

	var category Category
	parsed, known := CategoryFromWire(req.Category)
	switch {
	case req.Category == "":
		// Left as the zero value so the domain's Required check reports it. A client that
		// sent nothing should be told the field is required, not that "" is not a category.

	case known:
		category = parsed

	default:
		problems.Add("category", validate.CodeInvalid,
			"That is not a dispute category. Use one of %s.", strings.Join(CategoriesWire(), ", "))
	}

	// Left zero when absent, so the domain reports it as required rather than defaulting it.
	// Docs/04 §7 names the time of the event as an intake field of its own, and a default would
	// write the report's time into the incident's column — see [Intake].
	var occurredAt time.Time
	if req.OccurredAt != "" {
		parsed, err := time.Parse(time.RFC3339, req.OccurredAt)
		if err != nil {
			problems.Add("occurred_at", validate.CodeInvalid,
				"Send when this happened as RFC 3339, like 2026-08-11T15:40:00Z.")
		} else {
			occurredAt = parsed
		}
	}

	if err := problems.Err(); err != nil {
		return Intake{}, err
	}

	return Intake{
		Category:       category,
		Description:    req.Description,
		DesiredOutcome: req.DesiredOutcome,
		OccurredAt:     occurredAt,
		Evidence:       req.Evidence,
		Key:            key,
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

// callerID is the authenticated complainant's account id.
//
// The route declares RequireUser, so a subject is guaranteed by the time a handler runs — which is
// why the absence of one is reported as an internal failure rather than as a 401. Reaching here
// without a subject means a route was declared public and written as though it were protected, and
// that is a wiring defect the caller can do nothing about.
//
// The *role* is deliberately not read from the subject. Being the customer who owns the job, or the
// provider its bid was awarded to, is a stronger statement than carrying `role: customer`, and it is
// the one Docs/02 §2 actually makes — so the platform resolves it from the job rather than from a
// claim in a token.
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

	httpx.LoggerFrom(r.Context()).Warn("a dispute request arrived with no database connection")
	return nil, httpx.NewError(http.StatusServiceUnavailable, httpx.CodeUnavailable,
		"This cannot be completed right now. Try again shortly.")
}

// apiError turns this domain's errors into the API's error contract.
//
// The mapping lives at the transport edge on purpose: the service answers in domain terms, and which
// HTTP status somebody who is not party to a job deserves is not a question the domain has an
// opinion about. A *httpx.Error passes straight through, because validation already produced one in
// the right shape.
//
// # Why somebody else's job is 404 and not 403
//
// 403 would confirm that the job exists. Which jobs exist, who won them and what is going wrong on
// them is commercial information nobody published, and httpx's own description of `not_found` says
// the two cases are deliberately indistinguishable wherever telling them apart would confirm the
// existence of something private.
//
// The domain still distinguishes them ([ErrNotAParty] against [ErrJobNotFound]) so that a test can
// tell "the stranger was refused" from "the job silently stopped existing".
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
	case errors.Is(err, ErrJobNotFound), errors.Is(err, ErrNotAParty):
		return httpx.NewError(http.StatusNotFound, httpx.CodeNotFound,
			"No such job.").WithCause(err)

	case errors.Is(err, ErrJobNotDisputable):
		return httpx.NewError(http.StatusConflict, CodeJobNotDisputable,
			"A dispute can only be raised on a job that has been awarded and has not yet been "+
				"completed or cancelled.").WithCause(err)

	case errors.Is(err, ErrDisputeAlreadyOpen):
		return httpx.NewError(http.StatusConflict, CodeDisputeAlreadyOpen,
			"This job already has a dispute waiting on an outcome.").WithCause(err)

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
			"This %s has already raised a different dispute on this job. Generate a new key "+
				"for each action.", httpx.HeaderIdempotencyKey).WithCause(err)

	default:
		return err
	}
}
