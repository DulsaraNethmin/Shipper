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
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
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
// It is built in cmd/api/routes_admin.go from Deps. The pool is held here rather than inside the
// service because Docs/10 §3.2 puts the transaction boundary with whoever owns the invariant, and
// for intake that is this layer: the party check, the dispute row and the status change are one
// decision against one version of the job.
type Handler struct {
	svc        *Service
	creds      *Credentials
	moderation *Moderation
	pool       *pgxpool.Pool
	log        *slog.Logger
}

// NewHandler wires the handlers to the service.
//
// The pool may be nil and that is not an error. The service starts with an unreachable database on
// purpose — a rolling deployment during a failover would otherwise take every instance down at once
// — so a nil pool is a condition the handlers answer 503 to for as long as it lasts, not a reason to
// refuse to start.
func NewHandler(
	svc *Service,
	creds *Credentials,
	moderation *Moderation,
	pool *pgxpool.Pool,
	log *slog.Logger,
) (*Handler, error) {
	if svc == nil {
		return nil, errors.New("admin: a handler needs a service")
	}
	if creds == nil {
		// SHIP-147. A handler with no credentials service would serve the administrator
		// endpoints with a nil-pointer panic rather than refuse to start, and the sign-in
		// endpoint is the one place a missing collaborator is least visible in testing: it is
		// the first request anybody makes and the last one anybody retries.
		return nil, errors.New("admin: a handler needs the administrator credentials service")
	}
	if moderation == nil {
		// SHIP-117. A nil here would make the queue endpoint panic rather than answer, and the
		// panic would be at the first request rather than at startup — which is the wrong way
		// round for a collaborator that is decided once, in the composition root.
		return nil, errors.New("admin: a handler needs the moderation queue service")
	}
	if log == nil {
		return nil, errors.New("admin: a handler needs a logger")
	}
	return &Handler{svc: svc, creds: creds, moderation: moderation, pool: pool, log: log}, nil
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

		if err := h.creds.SignOut(r.Context(), grant.SessionID); err != nil {
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
		if _, err := h.permitted(r, PermissionAdminsManage); err != nil {
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

// --- SHIP-117: the delivery-exception moderation queue ------------------------------------------

// exceptionEntryResponse is one delivery that was evidenced by a reason rather than a photograph.
//
// **No object key and no signed URL**, because by construction there is no photograph — a proof row
// is one or the other and never both (`ck_proofs_photograph_or_exception`). **No customer, no
// provider and no budget**: a queue entry is what somebody triaging needs in order to decide whether
// to open the job, and a shape that never carried a budget cannot leak one.
type exceptionEntryResponse struct {
	ProofID string `json:"proof_id"`
	JobID   string `json:"job_id"`

	Milestone string `json:"milestone"`
	Reason    string `json:"reason"`

	// Note is the actor's own words, where they left any. Always present, empty when they did
	// not — `milestones.reason` is optional, and a `null` makes a console that renders without
	// checking crash on the ordinary case.
	Note string `json:"note"`

	// RecordedAt is the platform's clock, not the device's. See [ExceptionEntry.RecordedAt].
	RecordedAt string `json:"recorded_at"`

	// JobStatus is where the job is now. It implies nothing about whether a job completed
	// through this path may auto-complete, which is undecided (X-6).
	JobStatus string `json:"job_status"`
}

func exceptionEntryFrom(e ExceptionEntry) exceptionEntryResponse {
	return exceptionEntryResponse{
		ProofID:    e.ProofID.String(),
		JobID:      e.JobID.String(),
		Milestone:  e.Milestone,
		Reason:     e.Reason,
		Note:       e.Note,
		RecordedAt: timestamp(e.RecordedAt),
		JobStatus:  e.JobStatus,
	}
}

// ExceptionQueue handles GET /v1/admin/moderation/exceptions (SHIP-117).
//
// The *Done when* is that an exception-completed job **enters the moderation queue**, and this is
// the queue. It needs [PermissionModerationRead], which every role holds — reading a queue is what
// the least-privileged role exists to be able to do, and acting on what is in it is a different
// permission on a different endpoint.
//
// Oldest first, cursor paged, and it takes no filter: SHIP-157 is where this becomes a screen with
// four kinds of exception on it, and a query parameter added now would be one that ticket has to
// work around.
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
				last.ProofID.String(),
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
	return QueueQuery{Limit: limit, After: after}, nil
}

// decodeExceptionCursor reads the two fields the queue's ordering is total on.
func decodeExceptionCursor(raw string) (QueueCursor, error) {
	if raw == "" {
		return QueueCursor{}, nil
	}

	fields, err := pagination.Decode(raw, 2)
	if err != nil {
		return QueueCursor{}, err
	}

	recordedAt, err := time.Parse(time.RFC3339, fields[0])
	if err != nil {
		return QueueCursor{}, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"That cursor is not one this endpoint issued.").WithCause(err)
	}

	proofID, err := uuid.Parse(fields[1])
	if err != nil {
		return QueueCursor{}, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"That cursor is not one this endpoint issued.").WithCause(err)
	}

	return QueueCursor{RecordedAt: recordedAt, ProofID: proofID}, nil
}
