// The HTTP surface of the identity domain.
//
// Handlers live in the domain rather than in cmd/api (Docs/10 §2.1), and the reason is a merge
// hazard rather than taste: it keeps cmd/api/routes.go free of per-domain edits, which is what
// lets two domains be built at once without touching a shared file. A domain importing
// internal/httpx is sitting on infrastructure, not crossing a boundary, and the import lint
// permits it.
//
// # What this file does not disclose
//
// Registration reports a duplicate address, and nothing else here reports whether an account
// exists. The resend and OTP endpoints answer identically whether or not the contact details
// are known, because on those routes the disclosure buys the caller nothing and costs the
// account holder their privacy — see the note on CodeEmailTaken for why registration is the
// exception rather than an inconsistency.
//
// The blank line below keeps this a file note rather than a second package comment.

package identity

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// maxRequestBody is the largest body a handler in this package will decode.
//
// It must stay equal to httpx's maxIdempotentRequestBody (Docs/10 §4.3). The idempotency
// middleware reads and fingerprints the body before the handler sees it, so a handler with the
// larger limit would fingerprint a body it never read; the constant there is unexported, so
// this is a literal with the rule written beside it rather than a reference.
const maxRequestBody = 1 << 20 // 1 MiB

// Handler serves this domain's routes.
//
// It is built in cmd/api/routes_identity.go from Deps, which is what keeps the composition root
// free of identity's collaborators — a hasher, a service and the adapters are all pure
// functions of the pool, the Redis client and the configuration (see the note on Deps).
type Handler struct {
	svc *Service
	log *slog.Logger
}

// NewHandler wires the handlers to the service.
func NewHandler(svc *Service, log *slog.Logger) (*Handler, error) {
	if svc == nil {
		return nil, errors.New("identity: a handler needs a service")
	}
	if log == nil {
		return nil, errors.New("identity: a handler needs a logger")
	}
	return &Handler{svc: svc, log: log}, nil
}

// registerRequest is the body of POST /v1/auth/register (SHIP-30).
//
// snake_case on the wire throughout (Docs/10 §4.7), and unknown fields are refused, so a client
// that sends `phone_number` is told about the typo rather than having it silently ignored.
type registerRequest struct {
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// accountResponse is what the caller is told about their own account.
//
// Verification is reported as two booleans rather than as the timestamps the column holds. The
// client's question is "which screen next"; "since when" is a support and audit question, and
// exposing an instant that is only ever compared with nil invites a client to start doing
// arithmetic on it.
//
// There is no token here, and that is deliberate: registering is not signing in. SHIP-41 issues
// the first access token, after the password has been presented at the endpoint that exists to
// take it.
type accountResponse struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Phone  string `json:"phone"`
	Role   string `json:"role"`
	Status string `json:"status"`

	EmailVerified bool `json:"email_verified"`
	PhoneVerified bool `json:"phone_verified"`

	CreatedAt string `json:"created_at"`
}

func accountFrom(u User) accountResponse {
	return accountResponse{
		ID:            u.ID.String(),
		Email:         u.Email,
		Phone:         u.Phone,
		Role:          u.Role.String(),
		Status:        u.Status.String(),
		EmailVerified: u.EmailVerified(),
		PhoneVerified: u.PhoneVerified(),
		CreatedAt:     u.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
	}
}

// Register handles POST /v1/auth/register (SHIP-30, SHIP-45).
//
// Public, because it is how a caller obtains credentials in the first place — one of the seven
// entries on cmd/api's publicMutatingRoutes allow-list. It still carries an Idempotency-Key
// like every other state-changing request: a phone that retries after a dropped connection must
// not end up with two accounts, or with a duplicate-address error for the account it just
// successfully created.
func (h *Handler) Register() http.Handler {
	return apiHandler(func(w http.ResponseWriter, r *http.Request) error {
		var req registerRequest
		if err := decodeJSON(r, &req); err != nil {
			return err
		}

		user, err := h.svc.Register(r.Context(), RegisterCommand{
			Email:    req.Email,
			Phone:    req.Phone,
			Password: req.Password,
			Role:     Role(req.Role),
		})
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusCreated, accountFrom(user))
		return nil
	})
}

// apiError turns this domain's errors into the API's error contract.
//
// The mapping lives at the transport edge on purpose: the service answers in domain terms, and
// which HTTP status a duplicate address deserves is not a question the domain has an opinion
// about. A *httpx.Error is passed straight through, because validation already produced one in
// exactly the right shape.
//
// Anything unrecognised is returned as-is and becomes an opaque 500 in httpx.WriteError. That
// is the correct default: an error nobody has given a status and a code has not been
// considered, and guessing on its behalf is how an internal message reaches a client.
func apiError(err error) error {
	var apiErr *httpx.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}

	switch {
	case errors.Is(err, ErrEmailTaken):
		return httpx.NewError(http.StatusConflict, CodeEmailTaken,
			"An account already exists for this email address.").WithCause(err)

	case errors.Is(err, ErrPhoneTaken):
		return httpx.NewError(http.StatusConflict, CodePhoneTaken,
			"An account already exists for this mobile number.").WithCause(err)

	case errors.Is(err, errUnavailable):
		// 503 rather than 500, because the two say different things to a mobile client:
		// retry, or give up and surface a failure to the person holding the phone.
		return httpx.NewError(http.StatusServiceUnavailable, httpx.CodeUnavailable,
			"This cannot be completed right now. Try again shortly.").WithCause(err)

	default:
		return err
	}
}

// apiHandler adapts a handler that returns an error to one that writes a response.
//
// # This belongs in internal/httpx and is not there
//
// Docs/10 §4.3 specifies exactly this — "handlers have the signature
// func(http.ResponseWriter, *http.Request) error, adapted by httpx.H" — and httpx.H does not
// exist. Neither does httpx.DecodeJSON, which the same section names. That is the shape
// httpx.RegisterCode was in until SHIP-15c: documented, believed, and absent, because no domain
// had yet needed it.
//
// internal/httpx is a shared surface and not this branch's to edit (Docs/10 §9.2), so the
// mechanism is written here, unexported, and flagged in Docs/11 §9 for whoever owns cmd/api
// next. The second domain to want it should promote these two functions rather than copy them.
//
// The signature earns its place regardless of where it lives: returning an error rather than
// writing one removes the "forgot to return after writing" defect, which otherwise emits two
// response bodies and a status the client cannot make sense of.
func apiHandler(fn func(http.ResponseWriter, *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			httpx.WriteError(w, r, err)
		}
	})
}

// decodeJSON reads a request body into v, refusing anything it does not fully understand.
//
// See apiHandler for why this is here rather than in internal/httpx.
//
// Unknown fields are rejected (Docs/10 §4.3). Responses stay additive so that an old build
// tolerates a new field, but a request is the other direction: a client that sends `pasword`
// has made a mistake that will otherwise look like a validation failure on a field it believes
// it supplied.
func decodeJSON(r *http.Request, v any) error {
	if ct := r.Header.Get("Content-Type"); ct != "" && !isJSONContentType(ct) {
		return httpx.NewError(http.StatusUnsupportedMediaType, httpx.CodeUnsupportedMediaType,
			"This endpoint accepts application/json.")
	}

	if r.Body == nil {
		return httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"The request needs a JSON body.")
	}

	dec := json.NewDecoder(io.LimitReader(r.Body, maxRequestBody+1))
	dec.DisallowUnknownFields()

	if err := dec.Decode(v); err != nil {
		var unknown *json.UnmarshalTypeError
		switch {
		case errors.Is(err, io.EOF):
			return httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
				"The request needs a JSON body.").WithCause(err)
		case errors.As(err, &unknown):
			return httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
				"The %s field is not the expected type.", unknown.Field).WithCause(err)
		case strings.Contains(err.Error(), "unknown field"):
			// encoding/json reports this as a plain error with no type of its own, so
			// there is nothing else to match on. The message is quoted rather than
			// reworded because it names the offending field.
			return httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
				"The request contains a field this endpoint does not accept: %s",
				strings.TrimPrefix(err.Error(), "json: ")).WithCause(err)
		default:
			return httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
				"The request body is not valid JSON.").WithCause(err)
		}
	}

	// A second value in the stream means the client sent two documents, which is never what
	// was intended and would otherwise be silently ignored.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"The request body must be a single JSON object.")
	}
	return nil
}

func isJSONContentType(value string) bool {
	media, _, _ := strings.Cut(value, ";")
	return strings.EqualFold(strings.TrimSpace(media), "application/json")
}

// logFor returns the request-scoped logger, which is already bound to the request ID (SHIP-14).
//
// Falling back to the handler's own logger rather than slog's default keeps a background caller
// — a test, or the worker at SHIP-67a — attributable to the service that built the handler.
func (h *Handler) logFor(r *http.Request) *slog.Logger {
	if log := httpx.LoggerFrom(r.Context()); log != nil {
		return log
	}
	return h.log
}
