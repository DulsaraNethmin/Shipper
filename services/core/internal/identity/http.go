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
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
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
	// Name is required (SHIP-30a), and it is the one field here that an endpoint which already
	// shipped needed and this one had never collected: `GET /v1/admin/users` searches four terms
	// and could serve three until registration started asking.
	Name     string `json:"name"`
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
	ID string `json:"id"`

	// Name is what the platform stored, trimmed as it normalised it — so a client that sent
	// " Alice " renders back what will be searched for rather than what was typed.
	//
	// Always present, and empty for an account created before `000006`. That case is only
	// reachable through the verification endpoints, which also answer with this shape: an
	// account registered *since* cannot have an empty one.
	Name string `json:"name"`

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
		Name:          u.Name,
		Email:         u.Email,
		Phone:         u.Phone,
		Role:          u.Role.String(),
		Status:        u.Status.String(),
		EmailVerified: u.EmailVerified(),
		PhoneVerified: u.PhoneVerified(),
		CreatedAt:     timestamp(u.CreatedAt),
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
			Name:     req.Name,
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
			ClientIP:    clientIP(r),
		})
		if err != nil {
			// Retry-After is set here rather than inside the error, because httpx.Error
			// carries a status, a code and a message and no headers — and a throttled
			// caller told to come back later without being told when either gives up or
			// polls. The same shape RequestOTP already uses for its cooldown.
			var throttled *ThrottledError
			if errors.As(err, &throttled) {
				w.Header().Set("Retry-After", retryAfterSeconds(throttled.RetryAfter))
			}
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

// caller is the authenticated subject in the form this domain's methods take it (SHIP-43).
//
// authctx keeps both identifiers as strings so that no domain reading a subject has to depend on
// the uuid package to compare one. This domain does need the parsed values — they are primary
// keys here — and doing the conversion once, in one place, is what stops three handlers each
// deciding what an unparseable subject means.
type caller struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
}

// callerFrom reads the authenticated subject off the request.
//
// It is only correct on a route whose auth class is RequireUser, and [authctx.MustSubject]
// enforces that by panicking rather than by returning a zero subject: a route declared Public and
// written as though it were protected is a wiring defect, and an empty user identifier that
// compared equal to a row's owner would be a serious one.
//
// The identifiers are parsed rather than trusted. [AccessTokenVerifier] has already checked that
// both are UUIDs and neither is nil, so a failure here means the verifier and this parser have
// come to disagree — reported as an opaque 500, which is what an internal contradiction deserves.
func callerFrom(r *http.Request) (caller, error) {
	subject := authctx.MustSubject(r.Context())

	userID, err := uuid.Parse(subject.UserID)
	if err != nil {
		return caller{}, fmt.Errorf("identity: the subject's user id %q is not a uuid, "+
			"which the access token verifier should have refused: %w", subject.UserID, err)
	}
	sessionID, err := uuid.Parse(subject.SessionID)
	if err != nil {
		return caller{}, fmt.Errorf("identity: the subject's session id %q is not a uuid, "+
			"which the access token verifier should have refused: %w", subject.SessionID, err)
	}
	return caller{UserID: userID, SessionID: sessionID}, nil
}

// Logout handles POST /v1/auth/logout (SHIP-43).
//
// # Why this one requires a credential and the rest of the domain does not
//
// It is the first route in the service with `Auth: RequireUser`, and it is the natural first: the
// session being ended is named by the token being presented, so there is nothing to authorise
// against and nothing to look up. A body carrying a session identifier would be an endpoint that
// could end somebody else's session, which is SHIP-46's job and needs SHIP-46's owner check.
//
// # 204, and no body
//
// There is nothing to say. The client has already discarded its tokens by the time it reads this,
// and a body would only be something for a client to start branching on.
func (h *Handler) Logout() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		who, err := callerFrom(r)
		if err != nil {
			return err
		}

		if err := h.svc.SignOut(r.Context(), who.UserID, who.SessionID); err != nil {
			return apiError(err)
		}

		w.WriteHeader(http.StatusNoContent)
		return nil
	})
}

// deviceResponse is one row of the caller's device list (SHIP-46).
//
// What is here is what somebody deciding "is that me?" needs: a name they chose, when the device
// was first used, when it was last used, and which row is the one they are looking at it from.
// The refresh token hash is not here in any form, and neither is the session's expiry — a session
// is on this list because it has not lapsed, and an instant that only ever gets compared with now
// invites a client to do arithmetic on it.
type deviceResponse struct {
	ID          string `json:"id"`
	DeviceLabel string `json:"device_label"`

	CreatedAt  string `json:"created_at"`
	LastSeenAt string `json:"last_seen_at"`

	Current bool `json:"current"`
}

// deviceListResponse is the collection envelope of Docs/10 §4.5.
//
// The envelope is used even though this endpoint does not page, because §4.7 makes it the shape of
// every collection and a bare array would be the one exception a client generator has to be told
// about. `next_cursor` is always null: keyset paging belongs to internal/pagination, which is
// registered in internal/boundaries and not yet written. See [maxDevicesListed] for why `has_more`
// is a truncation report rather than an invitation to ask for the rest.
type deviceListResponse struct {
	Data       []deviceResponse `json:"data"`
	NextCursor *string          `json:"next_cursor"`
	HasMore    bool             `json:"has_more"`
}

// Devices handles GET /v1/auth/sessions (SHIP-46).
//
// Read-only, so no Idempotency-Key: the middleware lets safe methods through untouched, and a key
// on a request that changes nothing would be a key stored for no reason.
func (h *Handler) Devices() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		who, err := callerFrom(r)
		if err != nil {
			return err
		}

		devices, truncated, err := h.svc.Devices(r.Context(), who.UserID, who.SessionID)
		if err != nil {
			return apiError(err)
		}

		// A non-nil empty slice, so the field is `[]` rather than `null`. A client that has to
		// handle both is a client that will one day handle only one.
		out := make([]deviceResponse, 0, len(devices))
		for _, d := range devices {
			out = append(out, deviceResponse{
				ID:          d.ID.String(),
				DeviceLabel: d.DeviceLabel,
				CreatedAt:   timestamp(d.CreatedAt),
				LastSeenAt:  timestamp(d.LastSeenAt),
				Current:     d.Current,
			})
		}

		httpx.WriteJSON(w, http.StatusOK, deviceListResponse{Data: out, HasMore: truncated})
		return nil
	})
}

// RevokeDevice handles DELETE /v1/auth/sessions/{id} (SHIP-46).
//
// # Why DELETE, when nothing is deleted
//
// The verb describes what happens to the resource the client is looking at — the device leaves
// their list — and that is the question a REST verb answers. The row is marked revoked and kept,
// per Docs/10 §3.3, because "signed out three weeks ago" is what makes the trail readable and
// because 000104's ON DELETE RESTRICT will not let a session go while its spent tokens name it.
// The contract says so where a client will read it.
func (h *Handler) RevokeDevice() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		who, err := callerFrom(r)
		if err != nil {
			return err
		}

		sessionID, err := uuid.Parse(r.PathValue("id"))
		if err != nil {
			// The same answer an identifier belonging to somebody else gets. A separate
			// "that is not a uuid" would tell a caller probing identifiers which of their
			// guesses were at least the right shape — and there is nothing they could do
			// differently either way.
			return apiError(ErrSessionNotFound)
		}

		if err := h.svc.RevokeDevice(r.Context(), who.UserID, sessionID); err != nil {
			return apiError(err)
		}

		w.WriteHeader(http.StatusNoContent)
		return nil
	})
}

// deletionRequestResponse is what a person is told when they ask to be deleted (SHIP-169).
//
// `completes_by` is the whole point of the response. Docs/05 §3.1 requires the platform to tell the
// person when deletion will complete, and this is that sentence in machine-readable form — read
// back from the stored row, never computed while rendering. See the note at the top of deletion.go.
//
// The account is not echoed. A client that has just asked to be deleted does not need its own email
// address handed back, and a response naming the contact details of an account under a deletion
// request is one more copy of them to end up in a log.
type deletionRequestResponse struct {
	ID string `json:"id"`

	// State is `requested` or, since SHIP-170, `deferred`. SHIP-169 put it here rather than
	// leaving it implied for exactly this reason: a client that had assumed one value would
	// render a deferral as an ordinary request, and the contract said so before the second
	// value existed.
	State string `json:"state"`

	RequestedAt string `json:"requested_at"`
	CompletesBy string `json:"completes_by"`

	// DeferralReason is why the request is waiting, present only when it is (SHIP-170).
	//
	// **Omitted rather than empty on a live request.** `omitempty` makes the field's presence
	// the same fact as the state, so a client cannot render an explanation for a request that
	// has none — and a screen branching on the field being there is branching on the same
	// thing as one branching on the state.
	//
	// It carries no job, in any form. Both halves of the marketplace reach this endpoint, and
	// Docs/01 §4.3's invariant is easiest to keep on a shape that never had a job to describe.
	DeferralReason string `json:"deferral_reason,omitempty"`
}

func deletionRequestFrom(req DeletionRequest) deletionRequestResponse {
	response := deletionRequestResponse{
		ID:          req.ID.String(),
		State:       req.State.String(),
		RequestedAt: timestamp(req.RequestedAt),
		CompletesBy: timestamp(req.CompleteBy),
	}
	if req.State == DeletionDeferred {
		response.DeferralReason = DeferralReason
	}
	return response
}

// RequestAccountDeletion handles POST /v1/account/deletion (SHIP-169).
//
// # Why there is no request body
//
// Which account is being deleted is decided by the token, exactly as sign-out is. A body naming an
// account would be an endpoint that could delete somebody else's, and no confirmation field belongs
// here either: Docs/05 §3.1 puts the confirmation in the app, and a `"confirm": true` on the wire
// is a checkbox the platform cannot see anybody tick.
//
// # 202 the first time, 200 afterwards
//
// 202 Accepted is the honest status for the first request: the platform has accepted it and will
// act within thirty days, which is precisely what 202 means and what 201 does not. A repeat gets
// 200 with the same body — the request that already exists, carrying the date it has carried since
// it was made. A client can tell "I have just asked" from "I asked before" without either being an
// error, and neither answer moves the completion date.
//
// # A deferral is neither an error nor a different status code (SHIP-170)
//
// Docs/05 §3.1 defers a request made during a delivery rather than refusing it, so the answer is
// the same 202: the platform has accepted it and will act. What differs is `state` and the
// `deferral_reason` beside it. **A 409 or a 422 would have been the wrong shape twice** — the
// request succeeded, and a client rendering an error would tell somebody their deletion had not
// been recorded when it had.
//
// The status codes therefore say only whether a request was *created*, and the state says whether
// its clock is running. A client must not read a `200` as "nothing happened": the deferral may have
// lifted on this very call, which is a change to the account with no new row to show for it.
func (h *Handler) RequestAccountDeletion() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		who, err := callerFrom(r)
		if err != nil {
			return err
		}

		request, created, err := h.svc.RequestDeletion(r.Context(), who.UserID)
		if err != nil {
			return apiError(err)
		}

		status := http.StatusOK
		if created {
			status = http.StatusAccepted
		}

		httpx.WriteJSON(w, status, deletionRequestFrom(request))
		return nil
	})
}

// timestamp renders an instant the way every response in this domain renders one.
//
// One function rather than a format string repeated per struct: two copies is how a field ends up
// with milliseconds in one response and not in another, which a client parsing both has to
// discover for itself.
func timestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// clientIP is where the request came from, as SHIP-47's per-address limit counts it.
//
// **RemoteAddr only. `X-Forwarded-For` is deliberately not read**, and that is a decision with a
// deployment consequence recorded in Docs/11 §9 rather than an oversight. A forwarded header is
// whatever the client wrote unless a trusted proxy overwrote it, so honouring one here would let
// any caller pick their own bucket and evade the limit entirely — which is worse than no limit,
// because it would look like one. The other direction has a cost too: behind a load balancer that
// does not yet exist, every request would arrive from one address and share one bucket. Neither is
// acceptable in production and the answer is a trusted-proxy configuration, which belongs with the
// deployment work rather than inside a three-point ticket.
//
// The port is stripped, so that a caller does not get a fresh bucket per connection.
func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	// httptest and any transport that reports a bare address land here. Returned as-is
	// rather than as an empty string, because the domain refuses an empty address.
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

	// 429, with the honest wait in Retry-After. The message says which limit was reached in
	// the vaguest terms that are still useful, because "this account" and "this network" are
	// different remedies and neither discloses anything the caller does not already know.
	case errors.As(err, new(*ThrottledError)):
		return httpx.NewError(http.StatusTooManyRequests, httpx.CodeRateLimited,
			"Too many sign-in attempts. Wait a moment and try again.").WithCause(err)

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
