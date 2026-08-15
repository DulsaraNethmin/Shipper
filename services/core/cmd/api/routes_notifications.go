package main

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/notifications"
)

// The notifications domain's routes (SHIP-140).
//
// This file exists so that adding a domain adds a file and edits none. cmd/api/routes.go,
// manifest.go and main.go are shared surfaces (Docs/10 §9.2); a route registration that had to go
// into one of them is a line every concurrent branch also touches, and a badly resolved conflict
// there drops an endpoint with no compile error and no failing test.
//
// # The seventh domain reaches cmd/api three tickets after it reached the platform
//
// SHIP-137 built `internal/notifications` with no HTTP at all: a consumer and a dispatcher, both
// running in cmd/notifier. These two routes are the first thing in this domain a client can call,
// and they exist because a push notification needs an address and only the handset knows it.
//
// # Both routes are the caller's own device, and neither takes an identifier
//
// A device token binds to the device session the access token was issued against
// (authctx.Subject.SessionID), so there is no `{id}` and no field naming whose device this is.
// internal/notifications/http.go carries the reasoning, including why deregistration is
// `…/current` rather than `…/{token}`.
//
// RequireUser on both, which also puts them behind SHIP-44's scoped idempotency: keys land in
// `idem:v1:<subject>:<key>` rather than the shared anonymous namespace, which is the gate CLAUDE.md
// holds authenticated state-changing endpoints behind.
//
// # Why the pair is not under /auth
//
// Registering a push token is adjacent to signing in — SHIP-143 does it immediately afterwards —
// and it would have been defensible to hang it off the session. It is here instead because the
// domain that owns the rule owns the endpoint: what a device token is *for* is addressing a
// notification, and every rule about one (what displaces it, what revokes it, what a rejection
// means) is in this package. `identity` neither knows nor should know that push exists.
func init() {
	register(
		Route{
			Method:  http.MethodPost,
			Pattern: "/notifications/device-tokens",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return notificationsHandler(d).Register() },
		},
		Route{
			Method:  http.MethodDelete,
			Pattern: "/notifications/device-tokens/current",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return notificationsHandler(d).Deregister() },
		},

		// SHIP-142. One path, two methods, and no identifier — for the same reason the pair
		// above has none: the account is the caller's, taken from the credential.
		//
		// **`PUT` is the first one in this service**, and it belongs here rather than being a
		// deviation. The body is the complete muted set, so the client is placing a resource
		// at a location it already knows, which is what PUT is for — and it makes the request
		// idempotent by construction rather than only by its key.
		Route{
			Method:  http.MethodGet,
			Pattern: "/notifications/preferences",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return notificationsHandler(d).Preferences() },
		},
		Route{
			Method:  http.MethodPut,
			Pattern: "/notifications/preferences",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return notificationsHandler(d).UpdatePreferences() },
		},
	)
}

// notificationsHandler builds the domain's HTTP surface from the service's dependencies.
//
// # Three of the four collaborators are deliberately absent
//
// notifications.NewService takes a Parties lookup, a clock and a Senders struct, and accepts a
// Sessions option. This composition root supplies the clock and nothing else:
//
//   - **no senders**, because cmd/api dispatches nothing. The dispatcher runs in cmd/notifier, and
//     a sender wired here would be an adapter constructed in a process that never calls it.
//   - **no Sessions**, because that port is only read while resolving push addresses, which is
//     also cmd/notifier's work. Its absence means this service resolves no push address, which is
//     correct: it resolves nothing at all.
//   - **a Parties that panics**, because the two endpoints registered above never consume an event
//     and never resolve a recipient. NewService requires one — deliberately, since a consumer that
//     cannot resolve a job's parties resolves nobody — so this supplies one that cannot be used by
//     accident. See [noPartiesHere].
//
// The alternative was to split the service in two so that a registration handler could be built
// without a consumer's collaborators. That is a larger change to another ticket's package for one
// caller's convenience, and it would put a seam where the domain does not have one.
func notificationsHandler(d Deps) *notifications.Handler {
	service := notifications.NewService(noPartiesHere{}, d.Clock, notifications.Senders{})

	handler, err := notifications.NewHandler(service, d.Pool, d.Logger)
	if err != nil {
		panic("cmd/api: notifications handler: " + err.Error())
	}
	return handler
}

// noPartiesHere satisfies notifications.Parties in the one process that never uses it.
//
// It panics rather than answering, and the panic is the point. The two alternatives are worse in
// the same way as each other: returning "no such job" would make a consumer wired into this binary
// tell nobody anything, silently and for every event, and duplicating cmd/notifier's SQL here would
// put a cross-domain join in a binary with no consumer to justify it — which is exactly the
// accumulation ports.go's argument is against.
//
// A panic is loud, immediate, and reached only by code nobody has written. httpx.Recover would turn
// it into a 500 if it somehow escaped through a handler, which is the right answer to "this process
// was asked to do something it was never wired for".
type noPartiesHere struct{}

func (noPartiesHere) PartiesOn(context.Context, db.Runner, uuid.UUID) (uuid.UUID, uuid.UUID, bool, error) {
	panic("cmd/api: notifications.Parties was called in cmd/api, which consumes no events — " +
		"the consumer runs in cmd/notifier, and this binary wires the device token endpoints only")
}
