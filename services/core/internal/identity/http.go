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

// signInRequest is the body of POST /v1/auth/login (SHIP-41).
//
// The device label is required rather than optional, and that is a decision with a consequence
// for SHIP-46: a device list of four rows all reading "Unknown device" cannot be acted on, and
// the only moment a label can be collected is the one where the person is looking at a sign-in
// screen on the device being named. A client that has nothing better to send should send the
// handset's own model name, which is what Docs/07 §3's session design assumes.
type signInRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DeviceLabel string `json:"device_label"`
}

// Login handles POST /v1/auth/login (SHIP-41).
//
// # Why it is public
//
// It is the endpoint that produces the credential every protected route requires, so it cannot
// require one — the same justification the rest of cmd/api's publicMutatingRoutes allow-list
// rests on, and the shortest of them.
//
// # Why it carries an Idempotency-Key like every other mutation
//
// A sign-in creates a row. A phone that retries after a dropped connection would otherwise leave
// a device session nobody is holding a token for — invisible to its owner except as a duplicate
// line in their device list, and live for thirty days. The middleware replaying the first
// response is what makes the retry return the pair the first attempt issued.
func (h *Handler) Login() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		var req signInRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		pair, err := h.svc.SignIn(r.Context(), SignInCommand{
			Email:       req.Email,
			Password:    req.Password,
			DeviceLabel: req.DeviceLabel,
		})
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, h.tokenPairFrom(pair))
		return nil
	})
}

// refreshRequest is the body of POST /v1/auth/refresh (SHIP-42).
//
// The token travels in the request body rather than in the header a bearer token uses, and that
// is not an oversight. It is not a bearer token for *this* endpoint — it is the thing being
// exchanged, and that header is where the access token which has just expired would otherwise
// sit. Putting a second credential there would make "which token did this request present"
// ambiguous to a client interceptor and to a log.
type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// tokenPairResponse is what an endpoint that issues a session hands back — a refresh (SHIP-42)
// today, and sign-in (SHIP-41) next.
//
// # Why lifetimes rather than instants
//
// `expires_in` is a count of seconds, not a timestamp, because a client that compared a
// timestamp against its own clock would refresh at the wrong moment on any handset whose clock
// is wrong — which, on a phone that has been in a truck yard with no signal, is not unusual. A
// duration needs no agreement about what time it is.
//
// # What is deliberately absent
//
// No `token_type`. This platform issues one kind of bearer token and the contract's security
// scheme says so; a field whose value never varies is one the contract has to describe forever
// and no client can act on.
//
// No account object either. A refresh says what credentials the caller now holds and nothing
// about who they are — the access token carries that, and SHIP-63 reads verification state fresh
// rather than from anything cached at sign-in.
type tokenPairResponse struct {
	AccessToken string `json:"access_token"`

	// ExpiresIn is the life of the access token in seconds.
	ExpiresIn int `json:"expires_in"`

	RefreshToken string `json:"refresh_token"`

	// RefreshTokenExpiresIn is the life of the refresh token in seconds. A client that has not
	// refreshed within it is signed out and cannot recover without the password, so it is worth
	// telling the client rather than leaving it to discover.
	RefreshTokenExpiresIn int `json:"refresh_token_expires_in"`
}

func (h *Handler) tokenPairFrom(pair TokenPair) tokenPairResponse {
	return tokenPairResponse{
		AccessToken:           pair.Access.Value,
		ExpiresIn:             int(h.svc.AccessTokenTTL().Seconds()),
		RefreshToken:          pair.Refresh.Value,
		RefreshTokenExpiresIn: int(h.svc.RefreshTokenTTL().Seconds()),
	}
}

// Refresh handles POST /v1/auth/refresh (SHIP-39, SHIP-40, SHIP-42).
//
// # Why it is public
//
// It is called by exactly the client whose access token has just expired, so requiring one would
// lock that client out of the endpoint that replaces it. Docs/11 §3 records the same reasoning
// from SHIP-44's side: `ResolveSubject` runs group-wide and never rejects, and `RequireSubject`
// is per route, so a public route still resolves whatever credential is attached without
// refusing a bad one on sight.
//
// # Why a reused token is logged here as well as in the domain
//
// It is not. The domain logs it, through httpx.LoggerFrom, which is already bound to the request
// ID (SHIP-14) — so the security event and the request that caused it are joinable without this
// layer repeating it. What this layer decides is only which status and code the caller sees, and
// reuse and refusal share both.
func (h *Handler) Refresh() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		var req refreshRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		pair, err := h.svc.Refresh(r.Context(), req.RefreshToken)
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, h.tokenPairFrom(pair))
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

	// 400 rather than 401, and the rule the whole domain follows: a credential in the request
	// body is refused with 400, a credential in the bearer header with 401. See
	// CodeCredentialsInvalid for both halves of the argument.
	case errors.Is(err, ErrCredentialsInvalid):
		return httpx.NewError(http.StatusBadRequest, CodeCredentialsInvalid,
			"That email address and password do not match an account.").WithCause(err)

	// 403 rather than 400: the caller is known — they have just proved it — and is not
	// permitted. This is the only place account standing is disclosed.
	case errors.Is(err, ErrAccountSuspended):
		return httpx.NewError(http.StatusForbidden, CodeAccountSuspended,
			"This account has been suspended. Contact support.").WithCause(err)

	// 404 for a session that belongs to somebody else as well as for one that does not exist,
	// which is what stops the revoke endpoint being used to probe identifiers (SHIP-46).
	case errors.Is(err, ErrSessionNotFound):
		return httpx.NewError(http.StatusNotFound, CodeSessionNotFound,
			"No such device on this account.").WithCause(err)

	// Reuse and refusal are one answer, deliberately. The two sentinels exist so the domain
	// can log a security event and a test can tell them apart; the caller can do nothing
	// different about either, and telling somebody holding a stolen token that the platform
	// noticed is free help. See CodeRefreshTokenInvalid for why it is a 400 and not a 401.
	case errors.Is(err, ErrRefreshTokenInvalid), errors.Is(err, ErrRefreshTokenReused):
		return httpx.NewError(http.StatusBadRequest, CodeRefreshTokenInvalid,
			"This session has ended. Sign in again.").WithCause(err)

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
