// The HTTP surface of the profiles domain.
//
// Handlers live in the domain rather than in `cmd/api` (Docs/10 §2.1). The reason is a merge hazard
// rather than taste: it keeps `cmd/api/routes.go` free of per-domain edits, which is what lets two
// domains be built at once without touching a shared file. A domain importing `internal/httpx` is
// sitting on infrastructure, not crossing a boundary, and the import lint permits it.
//
// # Every route here is the caller's own record, and there is no parameter for whose
//
// The provider is whoever the token says is calling. A provider identifier in the path would be an
// authorisation decision made from client input, which Docs/07 §3 puts on the platform — and it would
// turn "a provider reads their own state" into a filter somebody could forget. There is nothing to
// forget: another provider's record is not refused, it is never selected.
//
// **The administrator's side of this is not here and is not this domain's to serve.** SHIP-153's
// queue and SHIP-154's decision are `internal/admin` endpoints under `/v1/admin`, on the separate
// administrator credential (SHIP-147). What they will call is [Service.Decide], through a port
// `internal/admin` declares for itself — this package exports the transition and does not export a
// route to it.
//
// The blank line below keeps this a file note rather than a second package comment.

package profiles

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// Handler serves this domain's routes.
type Handler struct {
	svc  *Service
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewHandler wires the handlers to the service.
//
// The pool may be nil and that is not an error. The service starts with an unreachable database on
// purpose — a rolling deployment during a failover would otherwise take every instance down at once —
// so a nil pool is a condition the handlers answer 503 to for as long as it lasts.
func NewHandler(svc *Service, pool *pgxpool.Pool, log *slog.Logger) (*Handler, error) {
	if svc == nil {
		return nil, errors.New("profiles: a handler needs a service")
	}
	if log == nil {
		return nil, errors.New("profiles: a handler needs a logger")
	}
	return &Handler{svc: svc, pool: pool, log: log}, nil
}

// verificationResponse is a provider's own verification standing.
//
// # What is here, and the two things that are deliberately not
//
// **No administrator.** Docs/04 §4 requires a *reason* be communicated to a rejected provider and
// says nothing about a name; the identity of the person who rejected somebody is what turns a
// moderation decision into a personal one.
//
// **No "may I bid" flag.** That question is answered by `internal/fleet`'s eligibility predicate, in
// one place, and a boolean here would be a second answer — computed from one of the four filters, so
// wrong for every provider who is Verified with no vehicle or no declared service area. A client that
// wants to know whether there is work available asks for the feed, which is what SHIP-82 serves and
// what an empty page already says.
type verificationResponse struct {
	State string `json:"state"`

	// Reason is why, and is omitted rather than sent empty for a provider nobody has decided
	// anything about yet — which is every provider on the day they register.
	Reason string `json:"reason,omitempty"`

	// DecidedAt is when the current state was decided, omitted for the same providers.
	DecidedAt *time.Time `json:"decided_at,omitempty"`

	// SubmittedAt is when the record was created, which is the answer to "how long have I been
	// waiting" — the one question a Pending screen exists to answer.
	SubmittedAt time.Time `json:"submitted_at"`
}

func verificationFrom(v Verification) verificationResponse {
	out := verificationResponse{
		State:       string(v.State),
		Reason:      v.Reason,
		SubmittedAt: v.SubmittedAt,
	}
	if v.Decided() {
		decided := v.DecidedAt
		out.DecidedAt = &decided
	}
	return out
}

// Verification handles GET /v1/provider/verification (SHIP-81a).
//
// The provider's own record and nobody else's. A caller who is not a provider is refused with
// `profiles_provider_only` rather than answered an empty record — SHIP-78a's reading, applied to a
// surface built after it.
func (h *Handler) Verification() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		found, err := h.svc.VerificationFor(r.Context(), pool, providerID)
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, verificationFrom(found))
		return nil
	})
}

// callerID is the authenticated subject's identifier.
//
// `RequireUser` has already refused the request without one, so a subject that is not a UUID is a
// defect in whatever issued the token rather than anything the caller did — hence 500 rather than
// 401, with the cause logged and nothing about it in the body.
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
// failure to the person holding the phone.
func (h *Handler) database(r *http.Request) (*pgxpool.Pool, error) {
	if h.pool != nil {
		return h.pool, nil
	}

	httpx.LoggerFrom(r.Context()).Warn("a profiles request arrived with no database connection")
	return nil, httpx.NewError(http.StatusServiceUnavailable, httpx.CodeUnavailable,
		"This cannot be completed right now. Try again shortly.")
}

// apiError turns this domain's errors into the API's error contract.
//
// The mapping lives at the transport edge on purpose: the service answers in domain terms, and which
// HTTP status a caller deserves is not a question the domain has an opinion about. A *httpx.Error
// passes straight through, because validation already produced one in the right shape.
func apiError(err error) error {
	var already *httpx.Error
	if errors.As(err, &already) {
		return err
	}

	switch {
	case errors.Is(err, ErrNotProvider):
		return httpx.NewError(http.StatusForbidden, CodeProviderOnly,
			"Only a provider account has a verification record.").WithCause(err)

	case errors.Is(err, ErrNoSuchProvider):
		return httpx.NewError(http.StatusNotFound, httpx.CodeNotFound,
			"No such provider.").WithCause(err)

	case errors.Is(err, ErrAlreadyInState):
		return httpx.NewError(http.StatusConflict, httpx.CodeConflict,
			"That provider is already in that state.").WithCause(err)

	default:
		// Including [ErrActorNotRecorded] and [ErrNotInTransaction], both of which are wiring
		// faults rather than anything a client did: an opaque 500 with the cause logged is the
		// honest answer, because there is nothing the caller could change.
		return err
	}
}
