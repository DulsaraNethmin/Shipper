package main

import (
	"net/http"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/passwords"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/platform/email"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/platform/sms"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/ratelimit"
)

// The identity domain's routes (SHIP-30 onwards).
//
// This file exists so that adding a domain adds a file and edits none. cmd/api/routes.go,
// manifest.go and main.go are shared surfaces (Docs/10 §9.2); a route registration that had to
// go into one of them is a line every concurrent branch also touches, and a badly resolved
// conflict there drops an endpoint with no compile error and no failing test.
//
// # Why the public routes here are public
//
// Every route that is, is how a caller obtains their own credentials — the only justification
// cmd/api's publicMutatingRoutes allow-list accepts. Registration takes a password and returns an
// account; sign-in takes a password and returns a session; the verification endpoints take a token
// or a code that was sent to the contact details being proved. None of them can require the
// credential they exist to produce.
//
// They are still behind httpx.Idempotent like every other state-changing request, and they are
// still rate limited — by their own issue rules today (SHIP-34) and by SHIP-47's token bucket
// across the whole authentication surface.
//
// # And why sign-out is not
//
// POST /v1/auth/logout is the first RequireUser route in the service (SHIP-43). The session it
// ends is the one named by the token being presented, so the credential is not merely a
// permission check — it is the whole input. See the note on Handler.Logout.
func init() {
	register(
		Route{
			Method:  http.MethodPost,
			Pattern: "/auth/register",
			Group:   GroupV1,
			Auth:    Public,
			Handler: func(d Deps) http.Handler { return identityHandler(d).Register() },
		},
		Route{
			// Public, and the shortest justification on the allow-list: this is the
			// endpoint that produces the credential every protected route requires
			// (SHIP-41).
			Method:  http.MethodPost,
			Pattern: "/auth/login",
			Group:   GroupV1,
			Auth:    Public,
			Handler: func(d Deps) http.Handler { return identityHandler(d).Login() },
		},
		Route{
			// The first route in the service that requires a credential (SHIP-43). SHIP-44
			// built the middleware; nothing had used it until now.
			Method:  http.MethodPost,
			Pattern: "/auth/logout",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return identityHandler(d).Logout() },
		},
		Route{
			// The device list and its revoke (SHIP-46). Read-only and state-changing on
			// one resource, so the pair is a GET and a DELETE rather than two POSTs.
			Method:  http.MethodGet,
			Pattern: "/auth/sessions",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return identityHandler(d).Devices() },
		},
		Route{
			Method:  http.MethodDelete,
			Pattern: "/auth/sessions/{id}",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return identityHandler(d).RevokeDevice() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/auth/verify-email",
			Group:   GroupV1,
			Auth:    Public,
			Handler: func(d Deps) http.Handler { return identityHandler(d).VerifyEmail() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/auth/resend-verify",
			Group:   GroupV1,
			Auth:    Public,
			Handler: func(d Deps) http.Handler { return identityHandler(d).ResendVerification() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/auth/request-otp",
			Group:   GroupV1,
			Auth:    Public,
			Handler: func(d Deps) http.Handler { return identityHandler(d).RequestOTP() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/auth/verify-phone",
			Group:   GroupV1,
			Auth:    Public,
			Handler: func(d Deps) http.Handler { return identityHandler(d).VerifyPhone() },
		},
		Route{
			// Public because it is called by exactly the client whose access token has
			// just expired (SHIP-42). Requiring one would lock that client out of the
			// endpoint that replaces it — which is the reason SHIP-44 split subject
			// resolution from subject enforcement in the first place.
			Method:  http.MethodPost,
			Pattern: "/auth/refresh",
			Group:   GroupV1,
			Auth:    Public,
			Handler: func(d Deps) http.Handler { return identityHandler(d).Refresh() },
		},
	)
}

// identityHandler builds the domain's handler from what Deps already carries.
//
// # Why this is built here rather than added to Deps
//
// Deps and its literal in main.go are the shared surfaces wave 2's three tracks would otherwise
// each grow a field on, and TestDepsCarriesExactlyWhatIsDeclared exists to make that a decision
// with a name attached. Nothing identity needs is missing: a hasher, a service and a handler are
// all pure functions of the pool, the clock and the configuration.
//
// # Why it panics
//
// It runs during attach, at startup, from a route's Handler function. Every failure it can
// report is a configuration or wiring mistake that will still be there after a restart — an
// argon2 profile outside the range this package will run, a nil clock. A service that came up
// serving registration with no password hasher would accept a password and store something
// nobody can verify against, which is worse than not starting.
//
// The pool is deliberately *not* checked. It may be nil because the database was unreachable at
// startup, which is a transient condition the service is built to survive (see the note on
// Deps); the handlers answer 503 for as long as it lasts.
func identityHandler(d Deps) *identity.Handler {
	hasher, err := passwords.NewHasher(passwords.Argon2Profile{
		MemoryKiB:   d.Config.Identity.Argon2.MemoryKiB,
		Iterations:  d.Config.Identity.Argon2.Iterations,
		Parallelism: d.Config.Identity.Argon2.Parallelism,
	})
	if err != nil {
		panic("cmd/api: identity password hasher: " + err.Error())
	}

	// The keyset and the issuer are built here rather than carried on Deps, which is the
	// pattern the note on Deps describes: both are pure functions of the configuration, so
	// neither is a reason to grow the shared struct. newRouter builds a *verifier* over the
	// same keys and passes it to the middleware — two objects over one keyset, because issuing
	// needs the active key and a TTL while verifying needs the whole set and no TTL.
	keys, err := identity.NewKeyset(d.Config.Identity.AccessTokenKeys, d.Config.Identity.AccessTokenActiveKID)
	if err != nil {
		panic("cmd/api: identity keyset: " + err.Error())
	}

	issuer, err := identity.NewAccessTokenIssuer(keys, d.Config.Identity.AccessTokenTTL, d.Clock)
	if err != nil {
		panic("cmd/api: identity access token issuer: " + err.Error())
	}

	// SHIP-47's token bucket, over the same Redis the idempotency store uses. d.Redis may be
	// nil — the process starts with an unreachable cache deliberately — and the limiter then
	// refuses every attempt, which is the fail-closed direction internal/ratelimit argues for.
	// The prefix keeps these keys distinguishable from the idempotency store's in one database.
	limiter, err := ratelimit.New(d.Redis, "rl:v1:", d.Clock)
	if err != nil {
		panic("cmd/api: identity rate limiter: " + err.Error())
	}

	svc, err := identity.NewService(d.Pool, hasher, issuer, limiter,
		newEmailSender(d.Config), newSMSSender(d.Config), d.Clock)
	if err != nil {
		panic("cmd/api: identity service: " + err.Error())
	}

	handler, err := identity.NewHandler(svc, d.Logger)
	if err != nil {
		panic("cmd/api: identity handler: " + err.Error())
	}
	return handler
}

// newEmailSender picks the email implementation for this environment (SHIP-32, SHIP-31).
//
// This is the composition root doing the one thing only it can: identity declares what it needs
// of a sender in its own ports.go and imports nothing from internal/platform/email, and the
// adapter knows nothing about identity. Go satisfies the interface structurally, and the two
// meet here (Docs/06 §4.1).
//
// email.UseConsole owns the rule rather than a switch written here, and it leans towards the
// console for anything it does not recognise. Choosing wrongly towards the console costs a
// developer a puzzled minute; choosing wrongly towards the provider sends real email from a
// machine that should never have had the credential.
//
// A staging or production deployment with no provider configured stops the process, and that is
// the correct direction. The alternative is a service that registers accounts, reports success,
// and silently sends no verification message — so every account it creates is one nobody can
// finish setting up, discovered a day later by the people who signed up.
func newEmailSender(cfg *config.Config) identity.EmailSender {
	if email.UseConsole(cfg.Env) {
		return email.NewConsole()
	}

	sender, err := email.NewProvider(email.Options{
		BaseURL: cfg.Email.ProviderBaseURL,
		APIKey:  cfg.Email.ProviderAPIKey,
		Sender:  cfg.Email.Sender,
	})
	if err != nil {
		panic("cmd/api: email provider: " + err.Error() +
			" — set EMAIL_PROVIDER_BASE_URL, EMAIL_PROVIDER_API_KEY and EMAIL_SENDER, or run " +
			"with SHIPPER_ENV=development to log messages to the console instead")
	}
	return sender
}

// newSMSSender picks the SMS implementation for this environment (SHIP-35, SHIP-34).
//
// The same shape as newEmailSender and the same reasoning, with one difference in emphasis:
// sms.UseConsole leaning towards the console matters more here, because a message costs money
// per send and wakes a real handset belonging to whoever last used that number for testing.
func newSMSSender(cfg *config.Config) identity.SMSSender {
	if sms.UseConsole(cfg.Env) {
		return sms.NewConsole()
	}

	sender, err := sms.NewProvider(sms.Options{
		BaseURL: cfg.SMS.ProviderBaseURL,
		APIKey:  cfg.SMS.ProviderAPIKey,
		Sender:  cfg.SMS.Sender,
	})
	if err != nil {
		panic("cmd/api: sms provider: " + err.Error() +
			" — set SMS_PROVIDER_BASE_URL, SMS_PROVIDER_API_KEY and SMS_SENDER, or run with " +
			"SHIPPER_ENV=development to log messages to the console instead")
	}
	return sender
}
