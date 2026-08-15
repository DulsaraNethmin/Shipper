// The HTTP surface of the notifications domain (SHIP-140).
//
// Handlers live in the domain rather than in cmd/api (Docs/10 §2.1). This is the first HTTP in this
// package: SHIP-137 built a consumer and a dispatcher, both of which run in cmd/notifier and answer
// no request at all.
//
// # Two endpoints, both about the caller's own handset, and neither takes an identifier
//
// A device token belongs to a device session, and the device session is the one the caller's access
// token was issued against (authctx.Subject.SessionID). So there is no `{id}` on either route and
// no field in either body naming whose device this is — a client naming a session would be an
// authorisation decision made from client input, which Docs/07 §3 puts on the platform.
//
// That is also what makes the pair small. There is no "somebody else's token" case, so no 404 that
// has to be indistinguishable from a 403, and no ownership check: the row this writes and the row
// this revokes are both selected by a value the client cannot influence.
//
// # Why `DELETE …/current` rather than `DELETE …/{token}`
//
// The thing being deregistered is *this device*, and the client always knows which device it is
// without knowing which token is live. Putting the token in the path would also put a value that
// identifies somebody's handset into every access log and every proxy on the way, for no gain.
//
// `current` is the same word `DELETE /v1/admin/sessions/current` uses for the same idea.
//
// # Why registering is a POST to a collection and not a PUT
//
// A handset does not choose its token; FCM hands it one, and hands it a different one whenever it
// feels like it. So the client is reporting a fact rather than placing a resource at a location it
// picked, and re-registering is the ordinary case rather than an update. What makes it idempotent
// is [Service.RegisterDevice] revoking whatever the same device had — not the verb.
//
// The blank line below keeps this a file note rather than a second package comment.

package notifications

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// Handler serves this domain's routes.
//
// Built in cmd/api/routes_notifications.go from Deps. The pool is held here rather than inside the
// service because Docs/10 §3.2 puts the transaction boundary with whoever owns the invariant, and
// registering a device is one: revoking what this handset had and inserting what it now has commit
// together or not at all.
type Handler struct {
	svc  *Service
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewHandler wires the handlers to the service.
//
// The pool may be nil and that is not an error — the service starts with an unreachable database on
// purpose, so a nil pool is a 503 for as long as it lasts rather than a refusal to start.
func NewHandler(svc *Service, pool *pgxpool.Pool, log *slog.Logger) (*Handler, error) {
	if svc == nil {
		return nil, errors.New("notifications: a handler needs a service")
	}
	if log == nil {
		return nil, errors.New("notifications: a handler needs a logger")
	}
	return &Handler{svc: svc, pool: pool, log: log}, nil
}

// registerDeviceRequest is the body of POST /v1/notifications/device-tokens.
//
// Two fields, and no third. In particular there is no user id and no session id: both come from
// the credential. And no `enabled`, no category list — muting is SHIP-142 and is a property of the
// account rather than of a handset.
type registerDeviceRequest struct {
	// Token is the FCM registration token this handset was given.
	Token string `json:"token"`

	// Platform is `ios` or `android`. Not inferred from a user agent, which a client controls
	// just as directly and which nothing else in this service reads.
	Platform string `json:"platform"`
}

// deviceTokenResponse is what a registration answers with.
//
// It deliberately does **not** echo the token. The client sent it and already has it, and a value
// that identifies somebody's handset should appear in as few places as possible — including
// response bodies, which are logged by more middleware than anybody remembers.
type deviceTokenResponse struct {
	ID           string `json:"id"`
	Platform     string `json:"platform"`
	RegisteredAt string `json:"registered_at"`
}

// Register records the device token this handset is addressable at.
//
// 201 every time, including when the same token is presented again — the resource created is the
// registration, and a client that retries after a dropped connection has created one either way.
// The alternative, 200 on a repeat, would have the client branching on a distinction it cannot act
// on.
func (h *Handler) Register() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		userID, sessionID, err := caller(r)
		if err != nil {
			return err
		}

		var req registerDeviceRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		// ck_device_tokens_token and ck_device_tokens_platform would both refuse these, and
		// a constraint violation reaches the client as a 500 with a constraint name in it.
		var problems validate.Errors
		if problems.Required("token", req.Token) {
			problems.Length("token", req.Token, 1, maxDeviceTokenLength)
		}
		problems.OneOf("platform", req.Platform, platformNames())
		if err := problems.Err(); err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		var registered DeviceToken
		err = db.InTx(r.Context(), pool, func(ctx context.Context, tx db.Runner) error {
			var txErr error
			registered, txErr = h.svc.RegisterDevice(ctx, tx, userID, sessionID,
				Platform(req.Platform), req.Token)
			return txErr
		})
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusCreated, deviceTokenResponse{
			ID:           registered.ID.String(),
			Platform:     registered.Platform.String(),
			RegisteredAt: registered.RegisteredAt.Format(http.TimeFormat),
		})
		return nil
	})
}

// Deregister stops addressing this handset.
//
// 204 whether or not anything was live. Deregistering twice is not an error — a client retrying
// after a dropped connection must not be told it did something wrong — and "there was nothing
// registered" is not a fact the client can act on differently.
//
// **It is a courtesy rather than the control**, and the distinction matters to whoever reads this
// next: sign-out ends push delivery whether or not this is ever called, because nothing addresses a
// device whose session has been revoked. See [Sessions] and 000701.
func (h *Handler) Deregister() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		_, sessionID, err := caller(r)
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		var wasRegistered bool
		err = db.InTx(r.Context(), pool, func(ctx context.Context, tx db.Runner) error {
			var txErr error
			wasRegistered, txErr = h.svc.DeregisterDevice(ctx, tx, sessionID)
			return txErr
		})
		if err != nil {
			return apiError(err)
		}

		// Logged rather than returned, because it is an operational fact and not the client's
		// business: a client that consistently deregisters nothing has a registration path
		// that is not working.
		httpx.LoggerFrom(r.Context()).LogAttrs(r.Context(), slog.LevelInfo,
			"device token deregistered",
			slog.String("session_id", sessionID.String()),
			slog.Bool("was_registered", wasRegistered))

		w.WriteHeader(http.StatusNoContent)
		return nil
	})
}

// --- notification preferences (SHIP-142) ------------------------------------------------------

// preferenceView is one category as the preference screen shows it.
//
// `essential` is served rather than left for the client to know. CLAUDE.md puts anything that
// changes under operational pressure server-side, and Flutter has no over-the-air path for Dart
// code — so a build that hard-coded which categories may be muted would be wrong the day a second
// one became mutable and could not be corrected. The app renders a switch per row and disables the
// ones marked essential; the platform is what refuses a muted essential, not the disabled switch.
type preferenceView struct {
	Category  string `json:"category"`
	Essential bool   `json:"essential"`
	Muted     bool   `json:"muted"`
}

// preferencesResponse is the whole screen: every category, in a stable order.
//
// An object rather than a bare array, because a bare array at the top level of a response has
// nowhere to grow — and this one will, the first time a preference is per channel rather than per
// category.
type preferencesResponse struct {
	Categories []preferenceView `json:"categories"`
}

// updatePreferencesRequest is the body of PUT /v1/notifications/preferences.
//
// One field, and it is the **whole** muted set rather than a change to it. A preference screen sends
// the state it is in, so the same request twice leaves the same rows and a retry after a dropped
// connection is free. `[]` is meaningful and accepted: it is "unmute everything".
//
// There is no user id. The account is the caller's, from the credential.
type updatePreferencesRequest struct {
	Muted []string `json:"muted"`
}

// Preferences serves which categories this account has switched off.
//
// Every category, not only the muted ones. The client is rendering a screen and the list of
// categories is the platform's to hold — see [preferenceView].
func (h *Handler) Preferences() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		userID, _, err := caller(r)
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		// A read, so it runs on the pool rather than in a transaction: there is no invariant
		// spanning two statements here, and Docs/10 §3.2 puts the transaction boundary with
		// whoever owns one.
		preferences, err := h.svc.Preferences(r.Context(), pool, userID)
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, preferencesView(preferences))
		return nil
	})
}

// UpdatePreferences replaces this account's muted set.
//
// PUT rather than PATCH because the body is the complete state: the client is placing the resource
// at a location it already knows, which is what PUT is for, and it makes the request idempotent by
// construction rather than by the key alone.
//
// It answers with the full screen rather than 204, so the client renders what the platform now holds
// instead of what it just sent — which are the same thing today and would not be if a category
// stopped being mutable between the app being built and the request being made.
func (h *Handler) UpdatePreferences() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		userID, _, err := caller(r)
		if err != nil {
			return err
		}

		var req updatePreferencesRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		// Checked here as well as in the service, because the client is owed the *index* of
		// the value that was wrong. The service refuses the same things and the database
		// refuses them again; this is the only layer that can say which element it was.
		var problems validate.Errors
		for i, name := range req.Muted {
			category := Category(name)
			switch {
			case !category.Valid():
				problems.OneOf(fmt.Sprintf("muted.%d", i), name, categoryNames())
			case category.Essential():
				problems.Add(fmt.Sprintf("muted.%d", i), CodeCategoryEssential,
					"This kind of notification cannot be switched off.")
			}
		}
		if err := problems.Err(); err != nil {
			return err
		}

		muted := make([]Category, 0, len(req.Muted))
		for _, name := range req.Muted {
			muted = append(muted, Category(name))
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		var preferences []Preference
		err = db.InTx(r.Context(), pool, func(ctx context.Context, tx db.Runner) error {
			var txErr error
			preferences, txErr = h.svc.SetMuted(ctx, tx, userID, muted)
			return txErr
		})
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, preferencesView(preferences))
		return nil
	})
}

// preferencesView is the domain's answer as the contract's shape.
func preferencesView(preferences []Preference) preferencesResponse {
	out := preferencesResponse{Categories: make([]preferenceView, 0, len(preferences))}
	for _, preference := range preferences {
		out.Categories = append(out.Categories, preferenceView{
			Category:  preference.Category.String(),
			Essential: preference.Essential,
			Muted:     preference.Muted,
		})
	}
	return out
}

// categoryNames is [Categories] as the validator wants it.
//
// Derived rather than typed out, so the list a client is validated against and the list
// ck_notifications_category enforces cannot come apart.
func categoryNames() []string {
	names := make([]string, 0, len(Categories))
	for _, c := range Categories {
		names = append(names, c.String())
	}
	return names
}

// maxDeviceTokenLength matches ck_device_tokens_token. FCM's tokens are around 160 characters
// today and the format has changed twice, so this is a sanity limit rather than a specification.
const maxDeviceTokenLength = 4096

// caller is the account and the device session the request was authenticated as.
//
// The session is what a device token binds to, and a token that names none cannot register one —
// which is [CodeNoDeviceSession] rather than a 500, because a bare 401 sends the Flutter client's
// interceptor into a refresh-and-replay loop against a session that will still not be there.
func caller(r *http.Request) (userID, sessionID uuid.UUID, err error) {
	subject := authctx.MustSubject(r.Context())

	userID, err = uuid.Parse(subject.UserID)
	if err != nil {
		return uuid.Nil, uuid.Nil, httpx.NewError(http.StatusInternalServerError, httpx.CodeInternal,
			"Something went wrong at our end.").WithCause(err)
	}

	sessionID, err = uuid.Parse(subject.SessionID)
	if err != nil {
		return uuid.Nil, uuid.Nil, httpx.NewError(http.StatusUnauthorized, CodeNoDeviceSession,
			"That credential does not name a device session. Sign in again.").WithCause(err)
	}
	return userID, sessionID, nil
}

func (h *Handler) database(r *http.Request) (*pgxpool.Pool, error) {
	if h.pool != nil {
		return h.pool, nil
	}

	httpx.LoggerFrom(r.Context()).Warn("a notifications request arrived with no database connection")
	return nil, httpx.NewError(http.StatusServiceUnavailable, httpx.CodeUnavailable,
		"This cannot be completed right now. Try again shortly.")
}

// apiError maps this domain's sentinels onto the error contract.
//
// Short, because both endpoints act on the caller's own device session: there is no not-found, no
// forbidden, and no conflict a client could resolve. The two validation cases are caught before the
// service is reached and are here as well, because a service called from anywhere else must not
// answer a 500 for a value a client got wrong.
func apiError(err error) error {
	var apiErr *httpx.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}

	var problems validate.Errors
	switch {
	case errors.Is(err, ErrUnknownPlatform):
		problems.OneOf("platform", "", platformNames())
		return problems.Err()

	case errors.Is(err, ErrNoDeviceToken):
		problems.Required("token", "")
		return problems.Err()

	// The two the service refuses on the preference path. Both are caught in the handler above,
	// where the *index* of the offending element is still known; these are here because a
	// service called from anywhere else must not answer a 500 for a value a client got wrong.
	case errors.Is(err, ErrUnknownCategory):
		problems.OneOf("muted", "", categoryNames())
		return problems.Err()

	case errors.Is(err, ErrEssentialCategory):
		problems.Add("muted", CodeCategoryEssential,
			"This kind of notification cannot be switched off.")
		return problems.Err()

	default:
		return err
	}
}

// platformNames is [Platforms] as the validator wants it.
//
// Derived rather than typed out, so the list a client is validated against and the list
// ck_device_tokens_platform enforces cannot come apart.
func platformNames() []string {
	names := make([]string, 0, len(Platforms))
	for _, p := range Platforms {
		names = append(names, p.String())
	}
	return names
}
