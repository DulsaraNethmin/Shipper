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
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

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
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		var req registerRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
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

// phoneRequest is the body of the endpoints whose whole input is a mobile number.
type phoneRequest struct {
	Phone string `json:"phone"`
}

// acceptedResponse is what an endpoint answers when it will not say whether it did anything.
//
// One shape, one status, one value, whether or not the contact details belong to an account —
// see the note at the top of this file and the one on [Service.RequestOTP]. The retry interval
// is a constant rather than the true remaining cooldown, because the true one would say "this
// number has been sent a code recently", which says "this number has an account".
//
// It is still useful to the client: SHIP-54's resend button runs its timer from this.
type acceptedResponse struct {
	RetryAfterSeconds int `json:"retry_after_seconds"`
}

// RequestOTP handles POST /v1/auth/request-otp (SHIP-34).
//
// 202 Accepted rather than 200, and the status is doing real work: the platform has accepted the
// request and will not tell the caller what came of it. A 200 with a body claiming the message
// was sent would be a lie for every number that has no account.
func (h *Handler) RequestOTP() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		var req phoneRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		retryAfter, err := h.svc.RequestOTP(r.Context(), req.Phone)
		if err != nil {
			return apiError(err)
		}

		// Retry-After as well as the body. The header is the standard place for it and the
		// body is the place a Flutter client will actually read it from, and there is no
		// cost to both agreeing.
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
		httpx.WriteJSON(w, http.StatusAccepted, acceptedResponse{
			RetryAfterSeconds: int(retryAfter.Seconds()),
		})
		return nil
	})
}

// verifyEmailRequest is the body of POST /v1/auth/verify-email (SHIP-33).
type verifyEmailRequest struct {
	Token string `json:"token"`
}

// emailRequest is the body of POST /v1/auth/resend-verify (SHIP-33).
type emailRequest struct {
	Email string `json:"email"`
}

// VerifyEmail handles POST /v1/auth/verify-email (SHIP-33).
//
// Public, and it has to be: the caller is proving an address, which is a step before they have
// any session at all. The token in the body is the credential, and it was sent to the address
// being proved.
func (h *Handler) VerifyEmail() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		var req verifyEmailRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		user, err := h.svc.VerifyEmail(r.Context(), req.Token)
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, accountFrom(user))
		return nil
	})
}

// ResendVerification handles POST /v1/auth/resend-verify (SHIP-33).
//
// 202 and the same body for every outcome, exactly as RequestOTP does — see the note on
// [Service.ResendVerification]. This is the endpoint a person reaches when the first message
// never arrived, which is also why a send failure at registration is logged rather than
// returned.
func (h *Handler) ResendVerification() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		var req emailRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		retryAfter, err := h.svc.ResendVerification(r.Context(), req.Email)
		if err != nil {
			return apiError(err)
		}

		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
		httpx.WriteJSON(w, http.StatusAccepted, acceptedResponse{
			RetryAfterSeconds: int(retryAfter.Seconds()),
		})
		return nil
	})
}

// verifyPhoneRequest is the body of POST /v1/auth/verify-phone (SHIP-36).
type verifyPhoneRequest struct {
	Phone string `json:"phone"`
	Code  string `json:"code"`
}

// VerifyPhone handles POST /v1/auth/verify-phone (SHIP-36).
//
// Public, and it carries both the number and the code because the caller has no session yet —
// the code is what establishes that they hold the handset the number reaches.
func (h *Handler) VerifyPhone() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		var req verifyPhoneRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		user, err := h.svc.VerifyPhone(r.Context(), req.Phone, req.Code)
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, accountFrom(user))
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

	case errors.Is(err, ErrVerificationTokenInvalid):
		return httpx.NewError(http.StatusBadRequest, CodeVerificationTokenInvalid,
			"This verification link is no longer valid. Ask for a new one.").WithCause(err)

	case errors.Is(err, ErrVerificationTokenExpired):
		return httpx.NewError(http.StatusBadRequest, CodeVerificationTokenExpired,
			"This verification link has expired. Ask for a new one.").WithCause(err)

	case errors.Is(err, ErrOTPInvalid):
		return httpx.NewError(http.StatusBadRequest, CodeOTPInvalid,
			"That code is not valid. Ask for a new one and try again.").WithCause(err)

	case errors.Is(err, errUnavailable):
		// 503 rather than 500, because the two say different things to a mobile client:
		// retry, or give up and surface a failure to the person holding the phone.
		return httpx.NewError(http.StatusServiceUnavailable, httpx.CodeUnavailable,
			"This cannot be completed right now. Try again shortly.").WithCause(err)

	default:
		return err
	}
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
