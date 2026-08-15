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
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

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
// It is built in cmd/api/routes_admin.go from Deps. The pool is held here rather than inside the
// service because Docs/10 §3.2 puts the transaction boundary with whoever owns the invariant, and
// for intake that is this layer: the party check, the dispute row and the status change are one
// decision against one version of the job.
type Handler struct {
	svc        *Service
	creds      *Credentials
	moderation *Moderation
	users      *Users
	jobs       *JobConsole
	trail      *AuditTrail
	enforce    *Enforcement
	notes      *Notes
	pool       *pgxpool.Pool
	log        *slog.Logger
}

// HandlerServices is what a handler is built from, beyond the pool and the logger.
//
// **A struct rather than a widening parameter list, adopted at SHIP-152.** [NewHandler] took four
// collaborators positionally and M6 has five tickets left that each add one. Six same-typed pointers
// in a row is a call that still compiles after two of them are swapped, and the failure would be a
// console serving one screen from another screen's service — which no test in this package would
// notice, because each of them supplies the collaborator it is about.
//
// Every field is required. A nil is a wiring mistake decided once in the composition root, so the
// constructor refuses it there rather than letting the first request to that endpoint panic.
type HandlerServices struct {
	// Disputes is the intake workflow (SHIP-163).
	Disputes *Service

	// Credentials is administrator sign-in and account creation (SHIP-147, SHIP-148).
	Credentials *Credentials

	// Moderation is the queues of Docs/04 §5 (SHIP-117).
	Moderation *Moderation

	// Users is the account search (SHIP-151).
	Users *Users

	// Jobs is the job and bid search (SHIP-152).
	Jobs *JobConsole

	// Trail is the audit log viewer (SHIP-165).
	Trail *AuditTrail

	// Enforcement is the administrative outcomes of Docs/04 §6 — unpublishing a job
	// (SHIP-160) and changing an account's standing (SHIP-161).
	Enforcement *Enforcement

	// Notes is the internal support history (SHIP-162).
	Notes *Notes
}

// NewHandler wires the handlers to the services.
//
// The pool may be nil and that is not an error. The service starts with an unreachable database on
// purpose — a rolling deployment during a failover would otherwise take every instance down at once
// — so a nil pool is a condition the handlers answer 503 to for as long as it lasts, not a reason to
// refuse to start.
func NewHandler(s HandlerServices, pool *pgxpool.Pool, log *slog.Logger) (*Handler, error) {
	if s.Disputes == nil {
		return nil, errors.New("admin: a handler needs a service")
	}
	if s.Credentials == nil {
		// SHIP-147. A handler with no credentials service would serve the administrator
		// endpoints with a nil-pointer panic rather than refuse to start, and the sign-in
		// endpoint is the one place a missing collaborator is least visible in testing: it is
		// the first request anybody makes and the last one anybody retries.
		return nil, errors.New("admin: a handler needs the administrator credentials service")
	}
	if s.Moderation == nil {
		// SHIP-117. A nil here would make the queue endpoint panic rather than answer, and the
		// panic would be at the first request rather than at startup — which is the wrong way
		// round for a collaborator that is decided once, in the composition root.
		return nil, errors.New("admin: a handler needs the moderation queue service")
	}
	if s.Users == nil {
		// SHIP-151. A nil here would make the search endpoint panic at the first request
		// rather than at startup, which is the wrong way round for a collaborator decided once
		// in the composition root — the same argument the queue service above records.
		return nil, errors.New("admin: a handler needs the account search service")
	}
	if s.Jobs == nil {
		// SHIP-152. Same argument again, and it now applies to a service the *validation* of a
		// query parameter depends on: [Handler.jobQueryFrom] asks it which job statuses exist.
		return nil, errors.New("admin: a handler needs the job and bid search service")
	}
	if s.Trail == nil {
		// SHIP-165. The same argument again, and here it guards the read side of the one
		// table this platform cannot reconstruct: a trail that answers 500 to every request
		// is a control nobody can exercise, and the failure would arrive at the first
		// support question rather than at startup.
		return nil, errors.New("admin: a handler needs the audit trail reader")
	}
	if s.Enforcement == nil {
		// SHIP-160, SHIP-161. The same argument again, and it is sharpest here: these are the
		// two endpoints that take something away from somebody, and a nil discovered at the
		// first one is a nil discovered while a moderator is trying to remove a policy breach.
		return nil, errors.New("admin: a handler needs the enforcement service")
	}
	if s.Notes == nil {
		// SHIP-162. The same argument once more. A nil here would panic at the first support
		// note somebody tried to write down, which is a moment when the console failing is
		// the second-worst thing that happens that day.
		return nil, errors.New("admin: a handler needs the notes service")
	}
	if log == nil {
		return nil, errors.New("admin: a handler needs a logger")
	}
	return &Handler{
		svc:        s.Disputes,
		creds:      s.Credentials,
		moderation: s.Moderation,
		users:      s.Users,
		jobs:       s.Jobs,
		trail:      s.Trail,
		enforce:    s.Enforcement,
		notes:      s.Notes,
		pool:       pool,
		log:        log,
	}, nil
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

	// --- administrator authentication (SHIP-147, SHIP-148) -----------------------------------

	case errors.Is(err, ErrAdminCredentialsInvalid):
		// 400 rather than 401, matching identity's sign-in: a 401 invites the client to
		// present a better credential for the request it just made, and the credential here
		// *was* the body.
		return httpx.NewError(http.StatusBadRequest, CodeAdminCredentialsInvalid,
			"That email address and password do not match an administrator account.").WithCause(err)

	case errors.Is(err, ErrAdminAccountDisabled):
		// 403 rather than 401. The caller has proved they hold the account, so this is not a
		// credential problem and a challenge header would be misleading — presenting a better
		// password will not help.
		return httpx.NewError(http.StatusForbidden, CodeAdminAccountDisabled,
			"This administrator account has been disabled.").WithCause(err)

	case errors.Is(err, ErrAdminEmailTaken):
		return httpx.NewError(http.StatusConflict, CodeAdminEmailTaken,
			"An administrator account already exists with that email address.").WithCause(err)

	case errors.Is(err, ErrAdminPermissionDenied):
		return httpx.NewError(http.StatusForbidden, CodeAdminPermissionDenied,
			"This administrator account does not have permission to do that.").WithCause(err)

	// --- the administrative outcomes of Docs/04 §6 (SHIP-160, SHIP-161) ----------------------

	case errors.Is(err, ErrReasonRequired), errors.Is(err, ErrReasonTooShort),
		errors.Is(err, ErrReasonTooLong):
		// A field-level failure in validate.Errors' shape, so it answers 422 naming `reason`
		// rather than a bare 400. Docs/10 §4.6: a client should be told which field, and this
		// is a field somebody typed.
		return fieldProblem("reason", err)

	case errors.Is(err, ErrNoteEmpty), errors.Is(err, ErrNoteTooLong):
		return fieldProblem("body", err)

	case errors.Is(err, ErrNoteSubjectUnrecognised):
		return fieldProblem("subject_type", err)

	case errors.Is(err, ErrNoteSubjectMissing):
		return fieldProblem("subject_id", err)

	case errors.Is(err, ErrUserNotFound):
		// Disclosed plainly. The caller is an administrator holding a permission over accounts,
		// and unlike the 404 on dispute intake there is nothing here being kept from them.
		return httpx.NewError(http.StatusNotFound, httpx.CodeNotFound,
			"No such account.").WithCause(err)

	case errors.Is(err, ErrStandingUnrecognised):
		return fieldProblem("standing", fmt.Errorf(
			"that is not an account standing; use one of %s", strings.Join(standingNames(), ", ")))

	case errors.Is(err, ErrExceptionGroundUnrecognised):
		return fieldProblem("ground", fmt.Errorf(
			"that is not a delivery-exception ground; use one of %s",
			strings.Join(groundNames(), ", ")))

	case errors.Is(err, ErrStandingUnchanged):
		return httpx.NewError(http.StatusConflict, CodeUserStandingUnchanged,
			"This account already has that standing.").WithCause(err)

	case errors.Is(err, ErrJobNotUnpublishable):
		return httpx.NewError(http.StatusConflict, CodeJobNotUnpublishable,
			"A job can only be unpublished before it is awarded.").WithCause(err)

	case errors.Is(err, ErrJobAlreadyUnpublished):
		return httpx.NewError(http.StatusConflict, CodeJobAlreadyUnpublished,
			"This job has already been unpublished.").WithCause(err)

	case errors.Is(err, ErrAdminUnavailable):
		// 503 rather than 500: a dependency is not answering, and retrying is the right
		// advice. The rate limiter reaches here when Redis is unreachable, which is the
		// fail-closed direction (see Credentials.admit).
		return httpx.NewError(http.StatusServiceUnavailable, httpx.CodeUnavailable,
			"This cannot be completed right now. Try again shortly.").WithCause(err)

	default:
		var throttled *ThrottledError
		if errors.As(err, &throttled) {
			// The wait itself goes in a Retry-After header, set by the handler — see
			// Handler.SignIn. The body says nothing about how many attempts are left,
			// because that is a number only somebody working through passwords needs.
			return httpx.NewError(http.StatusTooManyRequests, httpx.CodeRateLimited,
				"Too many sign-in attempts. Wait a moment and try again.").WithCause(err)
		}
		return err
	}
}

// fieldProblem renders a domain error as a one-field validation failure.
//
// The domain answers in its own terms because [Enforcement] is also reachable from a future task
// with no request behind it, and the field name belongs at the transport edge — which is where
// Docs/10 §4.6's "one details entry per field" is a rule about a response shape rather than about a
// service's vocabulary.
//
// The `admin: ` prefix is stripped: it identifies the package to a Go reader and means nothing to
// somebody looking at a console.
func fieldProblem(field string, err error) error {
	var problems validate.Errors
	problems.Add(field, validate.CodeInvalid, "%s",
		capitaliseFirst(strings.TrimPrefix(err.Error(), "admin: "))+".")
	return problems.Err()
}

// capitaliseFirst upper-cases the first rune, so a sentinel's message reads as a sentence.
//
// Docs/10 §4.4 wants a client to branch on the code and show the message; a message starting
// lower-case reads as a fragment somebody forgot to finish.
func capitaliseFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// --- administrator authentication (SHIP-147) -----------------------------------------------------
//
// Three endpoints and one shape between them. They are in this file rather than a second one for
// Docs/10 §2.1's reason — a domain has one `http.go`, and "which file is this handler in" is not a
// question anybody should have to answer twice.
//
// # These are the first routes in the service that declare RequireAdmin
//
// Everything above is called by a customer or a provider with a mobile access token. Everything
// below is called by an administrator with a credential that system cannot produce, verified by
// adminauth.go, and reaching a handler here with a *user* token is impossible rather than merely
// refused: the guard resolves a digest against `admin_sessions`, and a JWT is not one.

// signInRequest is the body of POST /v1/admin/sessions.
//
//	{"email": "moderator@shipper.example", "password": "…"}
//
// There is no device label. `identity` collects one because Docs/07 §3 makes every phone
// individually revocable by its owner and a person needs to recognise the phone in a list; an
// administrator console has no equivalent screen, and a label supplied by the client and never
// checked would be an unauthenticated string in the audit trail.
type signInRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// administratorResponse is an administrator as the console sees them.
//
// **No status field.** Every response carrying this shape is being sent to the administrator it
// describes, and an account that is not active cannot reach any of them — sign-in refuses it and
// the guard refuses it. A field whose value is always `active` is a field a client will one day
// branch on and be wrong.
//
// **No password material of any kind**, which [Administrator] makes structural rather than
// careful: there is no hash on the struct to leak.
type administratorResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`

	// Permissions is what the role holds, expanded (SHIP-148).
	//
	// **Published so the console can hide what it may not do, and never so it can decide.**
	// Docs/07 §3 is the rule and it is a rule about every client: the app may hide or disable,
	// the platform rules. An administrator who edited this list in a response would find every
	// endpoint refusing them exactly as before, because the check reads the role out of the
	// session row on each request.
	//
	// Sent expanded rather than left for a client to derive from `role`, because a derivation
	// is a second copy of [rolePermissions] in TypeScript — and the copy would be the one that
	// drifted, since nothing fails when a console shows a button that answers 403.
	Permissions []string `json:"permissions"`

	CreatedAt string `json:"created_at"`
}

func administratorFrom(a Administrator) administratorResponse {
	held := a.Role.Permissions()

	// Never nil on the wire: a role with no permissions is a real state — that is what an
	// unrecognised one grants — and `null` makes a console that iterates without checking crash
	// on exactly the account it most needs to render.
	permissions := make([]string, 0, len(held))
	for _, p := range held {
		permissions = append(permissions, p.String())
	}

	return administratorResponse{
		ID:          a.ID.String(),
		Email:       a.Email,
		Name:        a.Name,
		Role:        a.Role.String(),
		Permissions: permissions,
		CreatedAt:   timestamp(a.CreatedAt),
	}
}

// sessionResponse is what a client needs to know about the session it is holding.
//
// One expiry, not two. The console has no use for the difference between "you have been idle" and
// "this session has been open twelve hours" — it needs to know when to stop trusting the credential
// it holds, which is [Session.ExpiresAt], the earlier of the two. Publishing both would invite a
// client to compute the answer itself and get it wrong on the day one of the constants changes.
type sessionResponse struct {
	ID        string `json:"id"`
	ExpiresAt string `json:"expires_at"`
}

func sessionFrom(s Session) sessionResponse {
	return sessionResponse{ID: s.ID.String(), ExpiresAt: timestamp(s.ExpiresAt())}
}

// signInResponse is the body of a successful sign-in.
//
// **The token appears here and nowhere else, ever.** There is no endpoint that reads it back, no
// field on any other shape that carries it, and it is not stored in a form anything can reverse —
// `admin_sessions.token_hash` holds a digest. A console that loses it signs in again.
type signInResponse struct {
	Token         string                `json:"token"`
	Session       sessionResponse       `json:"session"`
	Administrator administratorResponse `json:"administrator"`
}

// SignIn handles POST /v1/admin/sessions (SHIP-147).
//
// A collection and a POST, deliberately, rather than `POST /v1/admin/login`: what this creates is a
// session with an identifier and a lifetime, and `DELETE /v1/admin/sessions/current` ends it. That
// is the same reading SHIP-163 took of `POST /jobs/{id}/disputes` — the thing being created is a
// record rather than a verb.
//
// **It is `Public`, and it is in cmd/api's `publicMutatingRoutes` allow-list because of that.** An
// endpoint that hands out a credential cannot require one. The list is short, it is checked by
// TestNoMutatingRouteIsPublic, and every entry on it is rate limited — this one twice over, per
// account and per address (see credentials.go).
//
// 200 rather than 201: `identity`'s sign-in answers 200 and a console has no use for a `Location`
// header pointing at a session it cannot fetch.
func (h *Handler) SignIn() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		var req signInRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		issued, administrator, err := h.creds.SignIn(r.Context(), SignInCommand{
			Email:    req.Email,
			Password: req.Password,
			ClientIP: clientIP(r),
		})
		if err != nil {
			// Retry-After is set here rather than inside the error, because httpx.Error
			// carries a status, a code and a message and no headers — and a throttled caller
			// told to come back later without being told when either gives up or polls.
			var throttled *ThrottledError
			if errors.As(err, &throttled) {
				w.Header().Set("Retry-After", retryAfterSeconds(throttled.RetryAfter))
			}
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, signInResponse{
			Token:         issued.Token,
			Session:       sessionFrom(issued.Session),
			Administrator: administratorFrom(administrator),
		})
		return nil
	})
}

// meResponse is the body of GET /v1/admin/me.
type meResponse struct {
	Administrator administratorResponse `json:"administrator"`
	Session       sessionResponse       `json:"session"`
}

// Me handles GET /v1/admin/me (SHIP-147).
//
// The console's first call after sign-in and after every reload: who am I, and how long is this
// credential good for. It reads the grant the guard put on the context and touches no database of
// its own, which is what makes it the cheapest possible demonstration that the class is enforced —
// a 200 here means a credential was resolved against `admin_sessions`, and a 401 means it was not.
//
// **It deliberately does not echo the session's absolute expiry or its idle expiry separately.**
// See [sessionResponse].
func (h *Handler) Me() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		grant, err := mustGrant(r.Context())
		if err != nil {
			return err
		}

		httpx.WriteJSON(w, http.StatusOK, meResponse{
			Administrator: administratorFrom(grant.Administrator),
			Session: sessionResponse{
				ID:        grant.SessionID.String(),
				ExpiresAt: timestamp(grant.ExpiresAt),
			},
		})
		return nil
	})
}

// SignOut handles DELETE /v1/admin/sessions/current (SHIP-147).
//
// It ends the session the caller is presenting and no other. There is no way to name somebody
// else's session, because there is no path parameter to name it with — "sign out that machine" is a
// feature with an authorisation question behind it, and a ticket that has not been written.
//
// **Idempotent by state rather than by key**, which is stronger: a second call from a retrying
// browser is a 204 rather than a 404, because the caller wanted the session ended and it is ended.
// The `Idempotency-Key` the middleware requires is still required — every state-changing endpoint
// takes one (SHIP-15) — and it is not what makes this safe to repeat.
//
// 204 rather than 200 with a body: there is nothing left to describe.
func (h *Handler) SignOut() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		grant, err := mustGrant(r.Context())
		if err != nil {
			return err
		}

		// The administrator and the session, because SHIP-150 records who signed out as well as
		// which session ended. Both come from the grant the guard resolved rather than from the
		// request, so there is no way to attribute a sign-out to somebody else.
		if err := h.creds.SignOut(r.Context(), grant.Administrator.ID, grant.SessionID); err != nil {
			return apiError(err)
		}

		w.WriteHeader(http.StatusNoContent)
		return nil
	})
}

// clientIP is where the request came from, as the per-address sign-in limit counts it.
//
// **`RemoteAddr` only. `X-Forwarded-For` is deliberately not read**, which is the position
// `identity` took at SHIP-47 and the reasoning is unchanged: a forwarded header is whatever the
// client wrote unless a trusted proxy overwrote it, so honouring one would let any caller pick their
// own bucket and evade the limit — worse than no limit, because it would look like one. Behind a
// load balancer that does not yet exist, every request arrives from one address and shares one
// bucket, which is the other unacceptable end. The answer is a trusted-proxy configuration, and it
// belongs with the deployment work.
//
// A second copy of identity's function rather than a shared one, because the two domains cannot
// import each other and the alternative is promoting six lines into infrastructure that would then
// own a deployment decision neither domain has made yet.
//
// The port is stripped, so a caller does not get a fresh bucket per connection.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	// httptest and any transport reporting a bare address land here. Returned as-is rather than
	// as an empty string, because the domain refuses an empty address.
	return strings.TrimSpace(r.RemoteAddr)
}

// retryAfterSeconds renders a wait for the header, rounded up.
//
// Up rather than to nearest: rounding 1.4 seconds down to one tells a client to retry before the
// allowance exists, which produces a second refusal and a client that believes the header lies.
func retryAfterSeconds(d time.Duration) string {
	seconds := int(math.Ceil(d.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}

// --- administrator management, behind a granular permission (SHIP-148) ---------------------------

// createAdministratorRequest is the body of POST /v1/admin/administrators.
//
//	{"email": "sam@shipper.example", "name": "Sam Okafor",
//	 "password": "…", "role": "moderator"}
//
// **`role` is optional and omitting it is not an error**, which is the ticket's *Done when* on the
// wire: a request that does not say gets the least-privileged role rather than the creator's, and
// rather than a refusal. A role the platform does not have *is* an error, because that is a
// different mistake — see [CreateCommand.Validate].
type createAdministratorRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
	Role     string `json:"role,omitempty"`
}

// CreateAdministrator handles POST /v1/admin/administrators (SHIP-147, SHIP-148).
//
// # This is the endpoint the permission model exists for
//
// Somebody who can create an administrator can create one with any role, which makes every other
// boundary in permissions.go advisory. So it is the one endpoint gated on [PermissionAdminsManage],
// which only [RoleOwner] holds — a `support` or `moderator` session reaching it gets a 403 naming no
// permission, and the permission it wanted goes to the log.
//
// # There is no bootstrap path here, deliberately
//
// The first administrator in a deployment cannot come from an endpoint that requires an
// administrator. It comes from one `INSERT` by an operator — see `000801`'s header — and this
// endpoint is how every account after it is made. An "if there are no administrators yet, allow
// anybody" branch would be an unauthenticated account-creation endpoint for as long as the table
// was empty, which is every deployment's first minute and any deployment somebody has cleaned.
//
// 201 with the account, and the password is not echoed.
func (h *Handler) CreateAdministrator() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		// The grant is used rather than discarded, which is what [Handler.permitted]'s note
		// says it is returned for: SHIP-150 attributes the entry to whoever acted, and taking
		// the actor from the grant means a handler cannot record one without having stated the
		// permission it was acting under.
		grant, err := h.permitted(r, PermissionAdminsManage)
		if err != nil {
			return err
		}

		var req createAdministratorRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		created, err := h.creds.Create(r.Context(), CreateCommand{
			Email:    req.Email,
			Name:     req.Name,
			Password: req.Password,
			Role:     Role(req.Role),
			ActorID:  grant.Administrator.ID,
		})
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusCreated, administratorFrom(created))
		return nil
	})
}

// permitted is the authorisation check every administrative handler makes (SHIP-148).
//
// # Why it takes a permission and not a role
//
// "Granular" is the *Done when*'s first word. A handler asking `if role == owner` would make every
// future permission a change to that condition, and a condition drifts towards whichever role
// already worked — which is how a moderation console ends up with three roles and one of them
// meaning anything.
//
// # Why the refusal names nothing
//
// The message sends somebody to ask for access rather than to enumerate what the platform can do,
// and the permission that was missing goes to the log against the request id instead. An
// administrator who needs to know which permission they lack asks the person who can grant it,
// which is the same person who can read the log.
//
// # Why the grant is returned as well
//
// Every caller needs it immediately afterwards — to attribute an audit entry (SHIP-150), or to scope
// a query. Returning it here means a handler cannot read the grant *without* having stated which
// permission it was acting under, which is the property that makes an unguarded handler visible in
// review rather than merely absent.
func (h *Handler) permitted(r *http.Request, p Permission) (Grant, error) {
	grant, err := mustGrant(r.Context())
	if err != nil {
		return Grant{}, err
	}
	if !grant.Permits(p) {
		httpx.LoggerFrom(r.Context()).LogAttrs(r.Context(), slog.LevelInfo,
			"an administrator was refused an action their role does not permit",
			slog.String("administrator_id", grant.Administrator.ID.String()),
			slog.String("role", grant.Administrator.Role.String()),
			slog.String("permission", p.String()))

		return Grant{}, apiError(fmt.Errorf("%w: %s needs %s",
			ErrAdminPermissionDenied, grant.Administrator.Role, p))
	}
	return grant, nil
}

// --- SHIP-117, SHIP-157: the delivery-exception moderation queue ---------------------------------

// exceptionEntryResponse is one delivery that is going wrong, on one of Docs/04 §5's four grounds.
//
// **No object key and no signed URL**, because on the one ground that has a photograph there is by
// construction none — a proof row is a photograph or a reason and never both
// (`ck_proofs_photograph_or_exception`) — and a queue listing is not where a pre-signed URL is
// issued in any case (Docs/04 §3.1). **No customer, no provider and no budget**: a queue entry is
// what somebody triaging needs in order to decide whether to open the job, and a shape that never
// carried a budget cannot leak one.
//
// # It is a union shape and `ground` is what makes that legible
//
// Three fields are empty on some grounds — `milestone` and `reason` on the two window grounds, and
// `reason` on the unsynced ground. **They are always present and never null**, so a console that
// renders without checking does not crash on the ordinary case; and `ground` is what a console
// branches on rather than guessing from which fields are blank.
type exceptionEntryResponse struct {
	// Ground is one of `overdue_pickup`, `delayed_delivery`, `failed_proof` or
	// `unsynced_milestone` (SHIP-157). Clients branch on this and never on `reason`.
	Ground string `json:"ground"`

	// EntryID identifies the row the entry came from, and what that row *is* depends on the
	// ground — a proof, a milestone, or the job itself where nothing was recorded. **It is not a
	// handle to resolve**; it exists so the ordering is total and two entries are tellable apart.
	// `job_id` is the identifier a console follows.
	EntryID string `json:"entry_id"`

	JobID string `json:"job_id"`

	// Milestone is which recorded claim the entry stands behind. Empty on the two window
	// grounds, where the entry exists because nothing was recorded.
	Milestone string `json:"milestone"`

	// Reason is Docs/01 §4.4's recorded reason. Empty on every ground but `failed_proof`.
	Reason string `json:"reason"`

	// Note is the actor's own words, where they left any. Always present, empty when they did
	// not — `milestones.reason` is optional, and a `null` makes a console that renders without
	// checking crash on the ordinary case.
	Note string `json:"note"`

	// RecordedAt is the platform's clock, not the device's, and it is the instant this entry has
	// been waiting since — which means the evidence, the arrival or the window's close depending
	// on the ground. See [ExceptionEntry.RecordedAt].
	RecordedAt string `json:"recorded_at"`

	// JobStatus is where the job is now. It implies nothing about whether a job completed
	// through the exception path may auto-complete, which is undecided (X-6).
	JobStatus string `json:"job_status"`
}

func exceptionEntryFrom(e ExceptionEntry) exceptionEntryResponse {
	return exceptionEntryResponse{
		Ground:     e.Ground.String(),
		EntryID:    e.EntryID.String(),
		JobID:      e.JobID.String(),
		Milestone:  e.Milestone,
		Reason:     e.Reason,
		Note:       e.Note,
		RecordedAt: timestamp(e.RecordedAt),
		JobStatus:  e.JobStatus,
	}
}

// ExceptionQueue handles GET /v1/admin/moderation/exceptions (SHIP-117, SHIP-157).
//
// Docs/04 §5's fourth queue, on all four grounds: overdue pickup, delayed delivery, failed proof and
// unsynced milestones. It needs [PermissionModerationRead], which every role holds — reading a queue
// is what the least-privileged role exists to be able to do, and acting on what is in it is a
// different permission on a different endpoint.
//
// Oldest first and cursor paged, unchanged by the widening. SHIP-117 left this filterless on the
// grounds that "a query parameter added now would be one that ticket has to work around"; SHIP-157
// is that ticket and the parameter it wanted is `ground`, which narrows the one queue rather than
// selecting between four.
func (h *Handler) ExceptionQueue() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		if _, err := h.permitted(r, PermissionModerationRead); err != nil {
			return err
		}

		query, err := exceptionQueryFrom(r)
		if err != nil {
			return err
		}

		// One more than asked for, so that "is there another page" is answered by the rows
		// rather than by a second COUNT — the same arrangement the jobs feed uses.
		query.Limit++

		entries, err := h.moderation.Exceptions(r.Context(), query)
		if err != nil {
			return apiError(err)
		}

		var next string
		if len(entries) == query.Limit {
			last := entries[len(entries)-2]
			next = pagination.Cursor{
				timestamp(last.RecordedAt),
				last.Ground.String(),
				last.EntryID.String(),
			}.Encode()
			entries = entries[:len(entries)-1]
		}

		out := make([]exceptionEntryResponse, 0, len(entries))
		for _, e := range entries {
			out = append(out, exceptionEntryFrom(e))
		}

		httpx.WriteJSON(w, http.StatusOK, pagination.NewPage(out, next))
		return nil
	})
}

// --- SHIP-151: the administrator's account search -----------------------------------------------

// userResponse is one account as the console sees it.
//
// **A closed set of keys, and nothing commercial in it.** A customer's budget is never exposed in
// any form (Docs/01 §4.3), and SHIP-83 established that searching a response for the word "budget"
// is not the check that catches it — a field called `max_price` passes that search and leaks the
// same fact. So this shape is held to its key set by
// TestTheUserSearchResponseCarriesNothingCommercial, which is an assertion about what is *present*
// rather than about what is absent.
//
// **No password material**, which [UserRecord] makes structural: there is no hash on the struct
// behind this to leak.
type userResponse struct {
	ID string `json:"id"`

	// Name is what the account holder is called (SHIP-30a), and one of the four terms `q`
	// matches. Always present, and empty for an account created before `000006` — a name cannot
	// be backfilled, so the console shows the absence rather than a placeholder that would read
	// as a name somebody chose.
	Name string `json:"name"`

	Email string `json:"email"`
	Phone string `json:"phone"`

	// Role is `customer` or `provider`, fixed at registration (SHIP-45).
	Role string `json:"role"`

	// Status is the account's standing, which is not the same thing as provider verification —
	// `000002`s own comment makes the distinction and SHIP-153 is the queue for the other.
	Status string `json:"status"`

	// The verification timestamps, empty where the channel is unconfirmed. Always present, so a
	// console that renders without checking does not crash on the ordinary case; `000002` records
	// why they are instants rather than booleans.
	EmailVerifiedAt string `json:"email_verified_at"`
	PhoneVerifiedAt string `json:"phone_verified_at"`

	CreatedAt string `json:"created_at"`
}

func userFrom(u UserRecord) userResponse {
	return userResponse{
		ID:              u.ID.String(),
		Name:            u.Name,
		Email:           u.Email,
		Phone:           u.Phone,
		Role:            u.Role,
		Status:          u.Standing.String(),
		EmailVerifiedAt: timestamp(u.EmailVerifiedAt),
		PhoneVerifiedAt: timestamp(u.PhoneVerifiedAt),
		CreatedAt:       timestamp(u.CreatedAt),
	}
}

// SearchUsers handles GET /v1/admin/users (SHIP-151).
//
// Docs/01 §4.6's first capability, behind [PermissionUsersRead] — which every role holds, because
// looking is what the least-privileged role exists to be able to do. Acting on what is found is
// [PermissionUsersRestrict] on a different endpoint (SHIP-161).
//
// # All four of the *Done when*'s terms are served, and the fourth arrived a wave late
//
// "Search users by email, phone, name, and status." `q` matches an email address, a **name** or a
// phone number, and `status` narrows by standing. The name was the term with no column: nothing in
// the schema held one and registration never asked, so this shipped serving three and the gap went
// to Docs/11 §4 rather than being papered over with a field that would have matched nothing.
// SHIP-30a closed it — `000006` adds `users.name`, registration requires it, and the search matches
// it. An account registered *before* that migration has no name and is found by its address.
//
// # A collection, cursor paged, newest first
//
// Docs/10 §4.5s envelope. Newest first because a search is not a queue: the moderation queue is
// oldest-first so the entry closest to breaching Docs/04 §8s target surfaces, and no such target
// applies to looking somebody up.
func (h *Handler) SearchUsers() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		if _, err := h.permitted(r, PermissionUsersRead); err != nil {
			return err
		}

		query, err := userQueryFrom(r)
		if err != nil {
			return err
		}

		// One more than asked for, so "is there another page" is answered by the rows rather
		// than by a second COUNT — the same arrangement the exception queue and the jobs feed
		// use.
		query.Limit++

		records, err := h.users.Search(r.Context(), query)
		if err != nil {
			return apiError(err)
		}

		var next string
		if len(records) == query.Limit {
			last := records[len(records)-2]
			next = pagination.Cursor{
				timestamp(last.CreatedAt),
				last.ID.String(),
			}.Encode()
			records = records[:len(records)-1]
		}

		out := make([]userResponse, 0, len(records))
		for _, u := range records {
			out = append(out, userFrom(u))
		}

		httpx.WriteJSON(w, http.StatusOK, pagination.NewPage(out, next))
		return nil
	})
}

// userQueryFrom reads the search parameters.
//
// Every problem is reported rather than the first (Docs/10 §4.6) for the two fields this endpoint
// owns. The limit and the cursor are internal/pagination's and refuse on their own, which is
// deliberate: a malformed cursor is a client bug rather than a person's typing, and mixing the two
// into one error list would put a field the console generated beside one somebody typed.
func userQueryFrom(r *http.Request) (UserQuery, error) {
	values := r.URL.Query()

	var problems validate.Errors

	term := strings.TrimSpace(values.Get("q"))
	if len(term) > maxSearchTermLength {
		problems.Add("q", validate.CodeTooLong,
			"That search term is longer than any address or number this could match. "+
				"Use at most %d characters.", maxSearchTermLength)
	}

	// An unrecognised standing is refused rather than ignored. Ignoring it would answer with
	// every account, and a support engineer who mistyped `suspeneded` would read the whole table
	// as the suspended ones — which is the opposite of the mistake being caught.
	standing := UserStanding(strings.TrimSpace(values.Get("status")))
	if standing != "" && !standing.Valid() {
		problems.Add("status", validate.CodeInvalid,
			"That is not an account standing. Use one of %s.", strings.Join(standingNames(), ", "))
	}

	if err := problems.Err(); err != nil {
		return UserQuery{}, err
	}

	limit, err := pagination.Limit(values.Get("limit"))
	if err != nil {
		return UserQuery{}, err
	}

	after, err := decodeUserCursor(values.Get("cursor"))
	if err != nil {
		return UserQuery{}, err
	}

	return UserQuery{Term: term, Standing: standing, Limit: limit, After: after}, nil
}

// decodeUserCursor reads the two fields the search ordering is total on.
func decodeUserCursor(raw string) (UserCursor, error) {
	if raw == "" {
		return UserCursor{}, nil
	}

	fields, err := pagination.Decode(raw, 2)
	if err != nil {
		return UserCursor{}, err
	}

	createdAt, err := time.Parse(time.RFC3339, fields[0])
	if err != nil {
		return UserCursor{}, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"That cursor is not one this endpoint issued.").WithCause(err)
	}

	userID, err := uuid.Parse(fields[1])
	if err != nil {
		return UserCursor{}, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"That cursor is not one this endpoint issued.").WithCause(err)
	}

	return UserCursor{CreatedAt: createdAt, UserID: userID}, nil
}

// exceptionQueryFrom reads the page parameters.
//
// A malformed cursor is a bad request rather than an empty page, for the reason internal/pagination
// gives: a client that sent one has a bug, and answering "nothing here" would let it page for ever
// through a queue it never saw.
func exceptionQueryFrom(r *http.Request) (QueueQuery, error) {
	values := r.URL.Query()

	limit, err := pagination.Limit(values.Get("limit"))
	if err != nil {
		return QueueQuery{}, err
	}

	after, err := decodeExceptionCursor(values.Get("cursor"))
	if err != nil {
		return QueueQuery{}, err
	}

	// Validated in the service rather than here, so that a task with no request behind it gets
	// the same refusal. The handler's job is to carry the value across.
	return QueueQuery{
		Limit:  limit,
		After:  after,
		Ground: ExceptionGround(strings.TrimSpace(values.Get("ground"))),
	}, nil
}

// groundNames is [ExceptionGrounds] as strings, for a validation message.
func groundNames() []string {
	out := make([]string, 0, len(ExceptionGrounds))
	for _, g := range ExceptionGrounds {
		out = append(out, g.String())
	}
	return out
}

// decodeExceptionCursor reads the three fields the queue's ordering is total on.
//
// **Three since SHIP-157**, and a cursor issued before the widening decodes to a length error rather
// than to a wrong page. That is the correct failure: the two-field form named a proof, and a proof
// identifier means nothing to a union whose entries are keyed by three different kinds of row.
func decodeExceptionCursor(raw string) (QueueCursor, error) {
	if raw == "" {
		return QueueCursor{}, nil
	}

	fields, err := pagination.Decode(raw, 3)
	if err != nil {
		return QueueCursor{}, err
	}

	recordedAt, err := time.Parse(time.RFC3339, fields[0])
	if err != nil {
		return QueueCursor{}, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"That cursor is not one this endpoint issued.").WithCause(err)
	}

	ground := ExceptionGround(fields[1])
	if !ground.Valid() {
		return QueueCursor{}, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"That cursor is not one this endpoint issued.")
	}

	entryID, err := uuid.Parse(fields[2])
	if err != nil {
		return QueueCursor{}, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"That cursor is not one this endpoint issued.").WithCause(err)
	}

	return QueueCursor{RecordedAt: recordedAt, Ground: ground, EntryID: entryID}, nil
}

// --- SHIP-152: the administrator's job and bid search ---------------------------------------------

// adminJobResponse is one job as the console sees it in a list.
//
// **No budget, no addresses and no contact details.** jobsearch.go's header has the argument in
// full: nothing in the *Done when* asks for the customer's maximum, an administrator is not the
// audience Docs/01 §4.3 protects it from but a shape that never carried one cannot leak it, and
// Docs/01 §5.1 asks the platform to minimise exposure of addresses and delivery details.
//
// Held to its key set by TestTheAdminJobShapesCarryNothingPrivate, which asserts what is *present*
// rather than searching for the word "budget" — SHIP-83's finding was that a field called
// `max_price` passes that search and leaks the same fact.
type adminJobResponse struct {
	ID         string `json:"id"`
	CustomerID string `json:"customer_id"`

	// Status is Docs/02 §1's **stored** form — `En route to pickup`, not `en_route_to_pickup`.
	//
	// A departure from Docs/10 §4.7, taken deliberately and consistently with the one
	// administrative endpoint that already publishes a job status: `job_status` on the
	// exception queue (SHIP-117) is stored form too. Two reasons. The console is an operator
	// surface, and an operator reading a screen beside a `psql` window should see one
	// vocabulary rather than two — support is the audience that most often has both open. And
	// the wire mapping lives in `jobs`, which this domain may not import, so translating here
	// would mean a hand-written copy of a list generated from `contracts/statuses.yaml`
	// (SHIP-56a) — the trade [ExceptionEntry.JobStatus] already refused.
	//
	// It is also what the `status` **filter** takes, which is the property that matters most: a
	// console that displayed one vocabulary and filtered on another would make every filter a
	// guess.
	Status string `json:"status"`

	// GoodsDescription is what the customer said they are sending. Empty on a draft that never
	// reached the details step.
	GoodsDescription string `json:"goods_description"`

	// BidCount is how many offers the job carries, in every status. See [JobRecord.BidCount].
	BidCount int `json:"bid_count"`

	// ExpiresAt is when an unclaimed job lapses. Empty where none is set — always present, so a
	// console that renders without checking does not crash on the ordinary case.
	ExpiresAt string `json:"expires_at"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func adminJobFrom(j JobRecord) adminJobResponse {
	return adminJobResponse{
		ID:               j.ID.String(),
		CustomerID:       j.CustomerID.String(),
		Status:           j.Status,
		GoodsDescription: j.GoodsDescription,
		BidCount:         j.BidCount,
		ExpiresAt:        timestamp(j.ExpiresAt),
		CreatedAt:        timestamp(j.CreatedAt),
		UpdatedAt:        timestamp(j.UpdatedAt),
	}
}

// adminBidResponse is one offer as the console sees it.
//
// Docs/02 §4's third reader. Amounts are here and the customer's budget is not, which is the whole
// distinction: a bid is what a provider offered and is visible to administrators by that document's
// own sentence, and a budget is what the customer would pay and is visible to nobody but them.
type adminBidResponse struct {
	ID         string `json:"id"`
	ProviderID string `json:"provider_id"`

	// Status is Docs/02 §4's stored form, for the reason [adminJobResponse.Status] records.
	Status string `json:"status"`

	// OfferedBy is which side made this offer — `provider` or `customer`. A customer's
	// counter-offer is a bid row like any other, and a reader who could not tell them apart
	// would read a negotiation as one party bidding against themselves.
	OfferedBy string `json:"offered_by"`

	// AmountCents is the offer in cents. Zero where the row carries none.
	AmountCents int64 `json:"amount_cents"`

	// PickupAt and DeliverBy are the timing offered. Empty where the row carries none.
	PickupAt  string `json:"pickup_at"`
	DeliverBy string `json:"deliver_by"`

	Message string `json:"message"`

	// SupersededBy names the offer that displaced this one, so a reader can follow a negotiation
	// in the order it happened. Empty on an offer nothing displaced.
	SupersededBy string `json:"superseded_by"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func adminBidFrom(b BidRecord) adminBidResponse {
	out := adminBidResponse{
		ID:          b.ID.String(),
		ProviderID:  b.ProviderID.String(),
		Status:      b.Status,
		OfferedBy:   b.OfferedBy,
		AmountCents: b.AmountCents,
		PickupAt:    timestamp(b.PickupAt),
		DeliverBy:   timestamp(b.DeliverBy),
		Message:     b.Message,
		CreatedAt:   timestamp(b.CreatedAt),
		UpdatedAt:   timestamp(b.UpdatedAt),
	}
	if b.SupersededBy != uuid.Nil {
		out.SupersededBy = b.SupersededBy.String()
	}
	return out
}

// adminStatusEventResponse is one recorded transition.
//
// **Both clocks, never one.** Docs/02 §3.1 keeps the actor's claim apart from the platform's
// acceptance because a driver records a milestone out of signal and the device syncs later; a
// support timeline showing one of them cannot answer why an update arrived when it did.
type adminStatusEventResponse struct {
	ID string `json:"id"`

	From string `json:"from"`
	To   string `json:"to"`

	// ActorType is `customer`, `provider`, `driver`, `admin` or `system`. Finer than the audit
	// log's three kinds, which is `000401`s decision rather than this endpoint's.
	ActorType string `json:"actor_type"`

	// ActorID is the account that acted, or the driver assignment for a driver. Empty for the
	// platform, which has no account.
	ActorID string `json:"actor_id"`

	// Reason is why, where one was given. Required of an administrator by the schema.
	Reason string `json:"reason"`

	ActorRecordedAt  string `json:"actor_recorded_at"`
	ServerRecordedAt string `json:"server_recorded_at"`
}

func adminStatusEventFrom(e StatusEvent) adminStatusEventResponse {
	out := adminStatusEventResponse{
		ID:               e.ID.String(),
		From:             e.From,
		To:               e.To,
		ActorType:        e.ActorType,
		Reason:           e.Reason,
		ActorRecordedAt:  timestamp(e.ActorRecordedAt),
		ServerRecordedAt: timestamp(e.ServerRecordedAt),
	}
	if e.ActorID != uuid.Nil {
		out.ActorID = e.ActorID.String()
	}
	return out
}

// adminJobDetailResponse is one job opened, with everything recorded against it.
//
// One shape rather than three endpoints, because the *Done when* is "open any job **with** its full
// bid and status history" — and a console making three calls to draw one screen is three chances to
// show a job beside somebody else's bids. [JobConsole.Open] reads all three in one snapshot for the
// same reason.
type adminJobDetailResponse struct {
	Job adminJobResponse `json:"job"`

	// Bids is every offer on the job, oldest first, in every status. Never nil on the wire.
	Bids []adminBidResponse `json:"bids"`

	// History is every recorded transition, oldest first. Never nil on the wire.
	History []adminStatusEventResponse `json:"history"`
}

// SearchJobs handles GET /v1/admin/jobs (SHIP-152).
//
// Docs/01 §4.6's first capability, second term, behind [PermissionJobsRead] — which every role
// holds, because looking is what the least-privileged role exists to be able to do.
//
// A collection, cursor paged, newest first. `q` matches the goods description, `status` narrows to
// one of Docs/02 §1's statuses, and `customer` narrows to one account's jobs.
func (h *Handler) SearchJobs() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		if _, err := h.permitted(r, PermissionJobsRead); err != nil {
			return err
		}

		query, err := h.jobQueryFrom(r)
		if err != nil {
			return err
		}

		// One more than asked for, so "is there another page" is answered by the rows rather
		// than by a second COUNT — the arrangement every paged endpoint in this service uses.
		query.Limit++

		records, err := h.jobs.Search(r.Context(), query)
		if err != nil {
			return apiError(err)
		}

		var next string
		if len(records) == query.Limit {
			last := records[len(records)-2]
			next = pagination.Cursor{
				timestamp(last.CreatedAt),
				last.ID.String(),
			}.Encode()
			records = records[:len(records)-1]
		}

		out := make([]adminJobResponse, 0, len(records))
		for _, j := range records {
			out = append(out, adminJobFrom(j))
		}

		httpx.WriteJSON(w, http.StatusOK, pagination.NewPage(out, next))
		return nil
	})
}

// OpenJob handles GET /v1/admin/jobs/{id} (SHIP-152).
//
// The second half of the *Done when* — "open any job with its full bid and status history" — and
// **any** is the word doing the work. Every job is an administrator's to open; there is no
// ownership scope on this endpoint and no 404 standing in for a refusal, which is the opposite of
// every other job endpoint in the service and is exactly what Docs/01 §4.6 asks for.
func (h *Handler) OpenJob() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		if _, err := h.permitted(r, PermissionJobsRead); err != nil {
			return err
		}

		jobID, err := jobIDFrom(r)
		if err != nil {
			return err
		}

		detail, err := h.jobs.Open(r.Context(), jobID)
		if err != nil {
			return apiError(err)
		}

		// Never nil on the wire. An absent list and an empty one are the same fact — no bids,
		// no recorded transitions — and `null` crashes a console that iterates without
		// checking, on the one job that had none.
		bids := make([]adminBidResponse, 0, len(detail.Bids))
		for _, b := range detail.Bids {
			bids = append(bids, adminBidFrom(b))
		}
		history := make([]adminStatusEventResponse, 0, len(detail.History))
		for _, e := range detail.History {
			history = append(history, adminStatusEventFrom(e))
		}

		httpx.WriteJSON(w, http.StatusOK, adminJobDetailResponse{
			Job:     adminJobFrom(detail.Job),
			Bids:    bids,
			History: history,
		})
		return nil
	})
}

// jobQueryFrom reads the search parameters.
//
// Every problem is reported rather than the first (Docs/10 §4.6) for the three fields this endpoint
// owns; the limit and the cursor are internal/pagination's and refuse on their own, for the reason
// [userQueryFrom] records.
//
// A method rather than a function, because validating `status` needs the closed list the service was
// given by cmd/api. That is the whole reason [JobConsole] holds one — see jobsearch.go.
func (h *Handler) jobQueryFrom(r *http.Request) (JobQuery, error) {
	values := r.URL.Query()

	var problems validate.Errors

	term := strings.TrimSpace(values.Get("q"))
	if len(term) > maxJobTermLength {
		problems.Add("q", validate.CodeTooLong,
			"That search term is longer than any description this could match. "+
				"Use at most %d characters.", maxJobTermLength)
	}

	// An unrecognised status is refused rather than ignored, for the reason the account search
	// refuses an unrecognised standing: ignoring it answers with every job, and a support
	// engineer who mistyped `Delivred` would read the whole marketplace as delivered.
	status := strings.TrimSpace(values.Get("status"))
	if status != "" && !h.jobs.KnowsStatus(status) {
		problems.Add("status", validate.CodeInvalid,
			"That is not a job status. Use one of %s.", strings.Join(h.jobs.Statuses(), ", "))
	}

	var customerID uuid.UUID
	if raw := strings.TrimSpace(values.Get("customer")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			problems.Add("customer", validate.CodeInvalid,
				"That is not a valid account identifier.")
		} else {
			customerID = parsed
		}
	}

	if err := problems.Err(); err != nil {
		return JobQuery{}, err
	}

	limit, err := pagination.Limit(values.Get("limit"))
	if err != nil {
		return JobQuery{}, err
	}

	after, err := decodeJobCursor(values.Get("cursor"))
	if err != nil {
		return JobQuery{}, err
	}

	return JobQuery{
		Term: term, Status: status, CustomerID: customerID, Limit: limit, After: after,
	}, nil
}

// decodeJobCursor reads the two fields the job search's ordering is total on.
func decodeJobCursor(raw string) (JobCursor, error) {
	if raw == "" {
		return JobCursor{}, nil
	}

	fields, err := pagination.Decode(raw, 2)
	if err != nil {
		return JobCursor{}, err
	}

	createdAt, err := time.Parse(time.RFC3339, fields[0])
	if err != nil {
		return JobCursor{}, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"That cursor is not one this endpoint issued.").WithCause(err)
	}

	jobID, err := uuid.Parse(fields[1])
	if err != nil {
		return JobCursor{}, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"That cursor is not one this endpoint issued.").WithCause(err)
	}

	return JobCursor{CreatedAt: createdAt, JobID: jobID}, nil
}

// --- SHIP-165: the audit log viewer ---------------------------------------------------------------

// auditEntryResponse is one entry as the console reads it.
//
// **Every column, and nothing withheld.** The trail is what holds administrators to account
// (Docs/04 §9), and a viewer showing a filtered version of the record would be a second record — the
// one somebody checks would be the wrong one. What keeps that safe is upstream: nothing commercial is
// ever written into an entry, and a ticket tempted to put a budget in one has made the mistake a
// layer earlier than this shape.
type auditEntryResponse struct {
	ID string `json:"id"`

	// ActorType is `admin`, `user` or `system` (`000003`). Coarser than the job history's five
	// kinds, which is that table's decision rather than this endpoint's.
	ActorType string `json:"actor_type"`

	// ActorID is the account that acted. Empty for `system`, which has none — and
	// `ck_audit_log_actor_id` requires exactly that, so an empty string here is a fact rather
	// than a missing value.
	ActorID string `json:"actor_id"`

	Action string `json:"action"`

	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`

	// Reason is why, where the action recorded one. Empty where it has none: creating an
	// administrator has no reason beyond the act, and the actions that need one are refused
	// without it.
	Reason string `json:"reason"`

	// Metadata is the extra facts the action recorded, as the object that was written.
	//
	// Passed through as raw JSON rather than decoded and re-encoded, so what a reader sees is
	// byte for byte what the platform wrote. A round trip through `map[string]any` reorders keys
	// and turns every number into a float — a gratuitous difference between the record and the
	// report of it, in the one table whose value is being trusted. Never `null`: the column is
	// NOT NULL and the writer supplies `{}` for nothing.
	Metadata json.RawMessage `json:"metadata"`

	// CreatedAt is the platform's clock at the moment the action committed, in UTC.
	CreatedAt string `json:"created_at"`
}

func auditEntryFrom(e AuditRecord) auditEntryResponse {
	out := auditEntryResponse{
		ID:         e.ID.String(),
		ActorType:  e.ActorType.String(),
		Action:     e.Action.String(),
		TargetType: e.TargetType,
		TargetID:   e.TargetID.String(),
		Reason:     e.Reason,
		Metadata:   json.RawMessage(e.Metadata),
		CreatedAt:  timestamp(e.CreatedAt),
	}
	if e.ActorID != uuid.Nil {
		out.ActorID = e.ActorID.String()
	}
	if len(out.Metadata) == 0 {
		// Defensive rather than expected: the column is NOT NULL DEFAULT '{}' and
		// [marshalMetadata] writes an object for nothing. An empty json.RawMessage would
		// serialise as invalid JSON and take the whole page down, which is not the failure
		// mode an audit viewer should have.
		out.Metadata = json.RawMessage("{}")
	}
	return out
}

// AuditTrail handles GET /v1/admin/audit (SHIP-165).
//
// Docs/01 §4.6's last capability — "view an immutable history of important actions" — and the read
// side of SHIP-150. It needs [PermissionAuditRead], which every role holds including the least
// privileged: a trail only the people it records can read is not a control.
//
// **There is no write, update or delete on this route or any other.** CLAUDE.md's invariant is that
// audit entries are append-only and ordinary administrators cannot delete them; permissions.go has
// no permission that would authorise it, [AuditTrail] has no method, and `000003`s triggers refuse
// both from any connection.
func (h *Handler) AuditTrail() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		if _, err := h.permitted(r, PermissionAuditRead); err != nil {
			return err
		}

		query, err := auditQueryFrom(r)
		if err != nil {
			return err
		}

		// One more than asked for, so "is there another page" is answered by the rows rather
		// than by a second COUNT — the arrangement every paged endpoint in this service uses.
		query.Limit++

		records, err := h.trail.Search(r.Context(), query)
		if err != nil {
			return apiError(err)
		}

		var next string
		if len(records) == query.Limit {
			last := records[len(records)-2]
			next = pagination.Cursor{
				timestamp(last.CreatedAt),
				last.ID.String(),
			}.Encode()
			records = records[:len(records)-1]
		}

		out := make([]auditEntryResponse, 0, len(records))
		for _, e := range records {
			out = append(out, auditEntryFrom(e))
		}

		httpx.WriteJSON(w, http.StatusOK, pagination.NewPage(out, next))
		return nil
	})
}

// auditQueryFrom reads the search parameters.
//
// Every problem is reported rather than the first (Docs/10 §4.6) for the five fields this endpoint
// owns; the limit and the cursor are internal/pagination's and refuse on their own, for the reason
// [userQueryFrom] records.
func auditQueryFrom(r *http.Request) (AuditQuery, error) {
	values := r.URL.Query()

	var problems validate.Errors

	actorID := uuidParam(&problems, values, "actor",
		"That is not a valid account identifier.")
	targetID := uuidParam(&problems, values, "target",
		"That is not a valid identifier.")

	// An unrecognised action is refused rather than ignored, for the reason every filter in this
	// console is: an ignored filter answers with the whole trail, and "everything" and "the
	// seventeen entries for this action" are indistinguishable to somebody who mistyped one —
	// who then concludes the action never happened, which is the failure an audit search has.
	action := AuditAction(strings.TrimSpace(values.Get("action")))
	if action != "" && !action.Valid() {
		problems.Add("action", validate.CodeInvalid,
			"That is not an action this platform records. Use one of %s.",
			strings.Join(auditActionNames(), ", "))
	}

	from := instantParam(&problems, values, "from")
	to := instantParam(&problems, values, "to")
	if !from.IsZero() && !to.IsZero() && !to.After(from) {
		// Refused rather than answered with an empty page. An inverted range is a console bug
		// or a person's slip, and both are better served by being told than by reading "no
		// administrator did anything that week".
		problems.Add("to", validate.CodeInvalid,
			"The end of the range must be after its start. `from` is inclusive and `to` is "+
				"exclusive, so consecutive days tile without overlapping.")
	}

	if err := problems.Err(); err != nil {
		return AuditQuery{}, err
	}

	limit, err := pagination.Limit(values.Get("limit"))
	if err != nil {
		return AuditQuery{}, err
	}

	after, err := decodeAuditCursor(values.Get("cursor"))
	if err != nil {
		return AuditQuery{}, err
	}

	return AuditQuery{
		ActorID: actorID, TargetID: targetID, Action: action,
		From: from, To: to, Limit: limit, After: after,
	}, nil
}

// auditActionNames is [AuditActions] as strings, for a validation message.
func auditActionNames() []string {
	out := make([]string, 0, len(AuditActions))
	for _, a := range AuditActions {
		out = append(out, a.String())
	}
	return out
}

// uuidParam reads an optional identifier, recording a field-level problem rather than refusing.
//
// A helper rather than the same six lines three times: every administrative search takes at least
// one identifier filter, and Docs/10 §4.6 wants every problem reported rather than the first — which
// means each one has to add to the list rather than return.
func uuidParam(problems *validate.Errors, values url.Values, field, message string) uuid.UUID {
	raw := strings.TrimSpace(values.Get(field))
	if raw == "" {
		return uuid.Nil
	}

	parsed, err := uuid.Parse(raw)
	if err != nil {
		problems.Add(field, validate.CodeInvalid, "%s", message)
		return uuid.Nil
	}
	return parsed
}

// instantParam reads an optional date or date-time bound.
//
// **Two accepted forms, and that is a deliberate accommodation rather than laziness.** A support
// engineer types a date — `2026-08-14` — and a console sends an instant. Accepting only the second
// would make the endpoint unusable by hand, which for an audit viewer is most of its use; accepting
// only the first would round away the precision a console has. A bare date is read as UTC midnight,
// which with the inclusive-from and exclusive-to rule makes consecutive days tile exactly.
//
// UTC rather than a local zone, and that is the only defensible reading: the column is `timestamptz`
// holding the platform's clock, and interpreting a bare date in the *server's* zone would move the
// boundary whenever the deployment moved.
func instantParam(problems *validate.Errors, values url.Values, field string) time.Time {
	raw := strings.TrimSpace(values.Get(field))
	if raw == "" {
		return time.Time{}
	}

	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.DateOnly, raw); err == nil {
		return t.UTC()
	}

	problems.Add(field, validate.CodeInvalid,
		"That is not a date. Use a day such as 2026-08-14, or a full instant such as "+
			"2026-08-14T09:30:00Z.")
	return time.Time{}
}

// decodeAuditCursor reads the two fields the trail's ordering is total on.
func decodeAuditCursor(raw string) (AuditCursor, error) {
	if raw == "" {
		return AuditCursor{}, nil
	}

	fields, err := pagination.Decode(raw, 2)
	if err != nil {
		return AuditCursor{}, err
	}

	createdAt, err := time.Parse(time.RFC3339, fields[0])
	if err != nil {
		return AuditCursor{}, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"That cursor is not one this endpoint issued.").WithCause(err)
	}

	entryID, err := uuid.Parse(fields[1])
	if err != nil {
		return AuditCursor{}, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"That cursor is not one this endpoint issued.").WithCause(err)
	}

	return AuditCursor{CreatedAt: createdAt, EntryID: entryID}, nil
}

// --- SHIP-160 and SHIP-161: the administrative outcomes of Docs/04 §6 -----------------------------

// unpublishJobRequest is the body of POST /v1/admin/jobs/{id}/unpublish.
//
//	{"reason": "Listing offers to move a live animal, which Docs/05 prohibits."}
//
// One field, and no job identifier: the job is in the path. No actor either — the administrator is
// whoever the credential says is calling, and a body that named its own actor would be an audit
// trail the client writes. httpx.DecodeJSON refuses unknown fields, so a console sending either is
// told the field does not exist rather than having it quietly ignored.
type unpublishJobRequest struct {
	Reason string `json:"reason"`
}

// unpublishJobResponse is what the console is told afterwards.
//
// The job's new status **is** echoed here, unlike on dispute intake where it deliberately is not.
// The difference is the audience: that response goes to a customer whose client already knows what
// raising a dispute does, and this one goes to a console rendering a row it must now redraw. It is
// the stored form, consistently with every other administrative shape.
type unpublishJobResponse struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`

	// Reason is read back so a console can render what was recorded without a second request —
	// and, more usefully, so that what the platform *stored* is what the operator sees rather
	// than what they typed. The two differ by trimming.
	Reason string `json:"reason"`
}

// UnpublishJob handles POST /v1/admin/jobs/{id}/unpublish (SHIP-160).
//
// Docs/01 §4.6's third capability — "remove or unpublish policy-breaching jobs" — behind
// [PermissionJobsUnpublish], which `moderator` and `owner` hold and `support` does not. Reading the
// job is `jobs.read` and every role has it; taking it off the marketplace is a different permission
// on a different endpoint, which is the whole shape of Docs/04 §9's least-privilege control.
//
// # It is a POST with an Idempotency-Key, and the key matters here more than usual
//
// Every state-changing endpoint takes one (SHIP-15) and this one is refused without it. A retry
// after a dropped connection must not produce two audit entries about one removal — and unlike a
// second bid or a second milestone, a duplicate entry in an append-only table cannot be tidied up
// afterwards.
//
// # Why "unpublish" and not DELETE
//
// Nothing is deleted. The job moves to `Cancelled` through the one guarded function, its history
// records who moved it and why, and the row stays exactly where it was — Docs/05 §3.1 keeps records,
// and a `DELETE` verb on this path would describe the opposite of what happens.
func (h *Handler) UnpublishJob() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		// The grant is used rather than discarded: the entry is attributed to whoever acted,
		// and taking the actor from the grant means a handler cannot record one without
		// having stated the permission it was acting under.
		grant, err := h.permitted(r, PermissionJobsUnpublish)
		if err != nil {
			return err
		}

		jobID, err := jobIDFrom(r)
		if err != nil {
			return err
		}

		var req unpublishJobRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		if err := h.enforce.Unpublish(r.Context(), UnpublishCommand{
			JobID:   jobID,
			ActorID: grant.Administrator.ID,
			Reason:  req.Reason,
		}); err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, unpublishJobResponse{
			JobID: jobID.String(),

			// The stored form of Docs/02 §1's terminal status, written out rather than read
			// back from the job. The transition committed, and the guard is what decides
			// where it went — a second read to confirm would be this domain asking whether
			// the function it just called did what it says.
			Status: "Cancelled",
			Reason: strings.TrimSpace(req.Reason),
		})
		return nil
	})
}

// setStandingRequest is the body of POST /v1/admin/users/{id}/standing.
//
//	{"standing": "restricted", "reason": "Two unresolved no-shows in a fortnight."}
//
// No account identifier — it is in the path. No `from`: what the account currently holds is read
// under a row lock rather than taken from a console that may have loaded the page five minutes ago.
type setStandingRequest struct {
	Standing string `json:"standing"`
	Reason   string `json:"reason"`
}

// standingResponse is what changed.
//
// **Both ends, never just the new one.** A console rendering "restricted" cannot tell a reader
// whether that was a tightening or a loosening, and the same request is used for all three
// directions — so the response says where the account was as well as where it is.
type standingResponse struct {
	UserID string `json:"user_id"`

	From string `json:"from"`
	To   string `json:"to"`

	Reason string `json:"reason"`
}

// SetStanding handles POST /v1/admin/users/{id}/standing (SHIP-161).
//
// Docs/01 §4.6's fourth capability — "restrict or suspend accounts with a recorded reason" — behind
// [PermissionUsersRestrict], which `moderator` and `owner` hold and `support` does not.
//
// # One endpoint for all three standings, including putting an account back
//
// `restricted`, `suspended` and `active` are one vocabulary (`ck_users_status`), the permission to
// move between them is one permission, and the reason is required in every direction. A separate
// reinstate endpoint would be a second place for the reason to become optional, and the direction is
// already in the response and in the entry.
//
// # What enforcement this adds: none, deliberately
//
// `identity` already refuses a suspended account at sign-in and at refresh, and a restricted
// provider at bidding. This is the administrative act that sets the column those checks read — see
// enforcement.go. A second check inside `admin` would be a second authority for one question.
//
// # POST rather than PATCH, and standing rather than the account
//
// PATCH on `/v1/admin/users/{id}` would invite a body that sets several fields, and the only field
// an administrator may set is this one — a role is fixed at registration by trigger, and an address
// is the account holder's. A named sub-resource says what the endpoint does and cannot grow into a
// general account editor by somebody adding a key.
func (h *Handler) SetStanding() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		grant, err := h.permitted(r, PermissionUsersRestrict)
		if err != nil {
			return err
		}

		userID, err := userIDFrom(r)
		if err != nil {
			return err
		}

		var req setStandingRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		change, err := h.enforce.SetStanding(r.Context(), StandingCommand{
			UserID:   userID,
			ActorID:  grant.Administrator.ID,
			Standing: UserStanding(strings.TrimSpace(req.Standing)),
			Reason:   req.Reason,
		})
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, standingResponse{
			UserID: change.UserID.String(),
			From:   change.From.String(),
			To:     change.To.String(),
			Reason: strings.TrimSpace(req.Reason),
		})
		return nil
	})
}

// userIDFrom reads and parses the {id} path parameter on the account routes.
//
// A second function rather than a rename of [jobIDFrom], because the message names the thing: a
// console sending a malformed identifier should be told which one, and "the job id in the path" on
// an account route sends somebody looking in the wrong place.
func userIDFrom(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return uuid.Nil, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"The account id in the path is not a valid identifier.").WithCause(err)
	}
	return id, nil
}

// --- SHIP-162: internal support notes -------------------------------------------------------------

// addNoteRequest is the body of POST /v1/admin/notes.
//
//	{"subject_type": "user", "subject_id": "0198f2c1-…", "body": "Rang about the damaged crates."}
//
// There is no author: the administrator is whoever the credential says is calling, and a body that
// named its own author would be a support history a client writes.
type addNoteRequest struct {
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	Body        string `json:"body"`
}

// noteResponse is one support note.
//
// **This shape is returned by two endpoints, both under /v1/admin, and by nothing else.** That is
// how "never user-visible" is kept true — see notes.go. A field added here reaches administrators
// and nobody else, because there is no user-facing response that carries a note at all.
type noteResponse struct {
	ID string `json:"id"`

	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`

	// AuthorID is the administrator who wrote it, as an identifier. A name would be a second
	// read of `admin_users`, and a console listing notes already knows its own administrators.
	AuthorID string `json:"author_id"`

	Body string `json:"body"`

	CreatedAt string `json:"created_at"`
}

func noteFrom(n Note) noteResponse {
	return noteResponse{
		ID:          n.ID.String(),
		SubjectType: n.Subject.String(),
		SubjectID:   n.SubjectID.String(),
		AuthorID:    n.AuthorID.String(),
		Body:        n.Body,
		CreatedAt:   timestamp(n.CreatedAt),
	}
}

// AddNote handles POST /v1/admin/notes (SHIP-162).
//
// Docs/01 §4.6's fifth capability, behind [PermissionNotesWrite] — which `moderator` and `owner`
// hold and `support` does not. **Reading them needs less**, which is the right way round and is
// deliberate: a support administrator reads the history and a moderator adds to it. See
// [Handler.ReadNotes].
//
// 201 with the note, which carries the identifier and the instant the platform assigned.
func (h *Handler) AddNote() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		grant, err := h.permitted(r, PermissionNotesWrite)
		if err != nil {
			return err
		}

		var req addNoteRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		subject, subjectID, err := noteSubjectFrom(req.SubjectType, req.SubjectID)
		if err != nil {
			return err
		}

		note, err := h.notes.Add(r.Context(), AddNoteCommand{
			Subject:   subject,
			SubjectID: subjectID,
			AuthorID:  grant.Administrator.ID,
			Body:      req.Body,
		})
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusCreated, noteFrom(note))
		return nil
	})
}

// ReadNotes handles GET /v1/admin/notes (SHIP-162).
//
// # The permission is the subject's, not a note permission
//
// A note on a user needs `users.read`; a note on a job needs `jobs.read`. permissions.go records why
// there is no `notes.read`: "notes are never user-visible, so there is no corresponding read
// permission for anyone outside the console". Everyone in the console may read them, and what
// [PermissionNotesWrite] distinguishes is who may add one.
//
// **The permission is therefore chosen from a value in the request**, which is a shape worth being
// explicit about. It is safe here because both permissions are held by every role, so the choice
// cannot widen anybody's access — and because an unrecognised subject is refused before the choice
// is made, so there is no branch in which no permission is checked. A subject kind added later that
// *is* restricted would make this a real decision, which is why [ReadPermissionFor] is a switch with
// no default rather than a map lookup with a fallback.
//
// Not paged. See [Notes.For]: the notes on one subject are tens at most, and a cursor would need a
// tie-break the index does not carry.
func (h *Handler) ReadNotes() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		values := r.URL.Query()

		subject, subjectID, err := noteSubjectFrom(
			values.Get("subject_type"), values.Get("subject_id"))
		if err != nil {
			return err
		}

		permission, ok := ReadPermissionFor(subject)
		if !ok {
			// Unreachable: noteSubjectFrom refuses an unrecognised subject above. Written out
			// rather than assumed, because the alternative to this branch is a handler that
			// serves notes having checked no permission at all — which is the one failure this
			// endpoint must not have, and it would be invisible.
			return apiError(fmt.Errorf("%w: %q", ErrNoteSubjectUnrecognised, subject))
		}
		if _, err := h.permitted(r, permission); err != nil {
			return err
		}

		notes, err := h.notes.For(r.Context(), subject, subjectID)
		if err != nil {
			return apiError(err)
		}

		// Never nil on the wire. An absent list and an empty one are the same fact — nobody has
		// written anything about this subject — and `null` crashes a console that iterates
		// without checking, on the ordinary case rather than the rare one.
		out := make([]noteResponse, 0, len(notes))
		for _, n := range notes {
			out = append(out, noteFrom(n))
		}

		httpx.WriteJSON(w, http.StatusOK, noteListResponse{Data: out})
		return nil
	})
}

// noteListResponse is the notes on one subject.
//
// **Not internal/pagination's envelope**, and that is deliberate rather than an omission: that shape
// carries `next_cursor` and `has_more`, and publishing either on an endpoint that never pages would
// be telling a client there is a mechanism when there is not. `data` is kept so the shape can grow
// into the envelope on the day somebody adds paging, without the field being renamed.
type noteListResponse struct {
	Data []noteResponse `json:"data"`
}

// noteSubjectFrom reads and validates a subject from a request.
//
// One function for both endpoints, because a note is written and read against the same pair of
// fields and the two must agree about what a valid subject is — a body that accepted a kind the
// query refused would write notes nothing could read.
//
// Every problem is reported rather than the first (Docs/10 §4.6).
func noteSubjectFrom(subjectType, subjectID string) (NoteSubject, uuid.UUID, error) {
	var problems validate.Errors

	subject := NoteSubject(strings.TrimSpace(subjectType))
	if !subject.Valid() {
		problems.Add("subject_type", validate.CodeInvalid,
			"A note is about a user or a job. Use one of %s.",
			strings.Join(noteSubjectNames(), ", "))
	}

	id := uuidParam(&problems, url.Values{"subject_id": {subjectID}}, "subject_id",
		"That is not a valid identifier.")
	if id == uuid.Nil && strings.TrimSpace(subjectID) == "" {
		problems.Add("subject_id", validate.CodeRequired, "A note must say what it is about.")
	}

	if err := problems.Err(); err != nil {
		return "", uuid.Nil, err
	}
	return subject, id, nil
}
