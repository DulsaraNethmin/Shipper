package httpx

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/ratelimit"
)

// Rate limiting at the transport edge (SHIP-183a).
//
// # Why the mechanism is here and the policy is not
//
// This file knows how to charge a bucket and how to refuse; it does not know which routes have
// which limit. That split is deliberate and matches the one [Idempotent] already lives on: the
// middleware is infrastructure, and *which class each of the 86 routes belongs to* is a decision
// recorded in Docs/12 and transcribed onto the route manifest in cmd/api. Putting the classes
// here would put an API-surface decision in the package every domain imports.
//
// # Why one bucket per class rather than one per route
//
// Docs/12 §3's figures are budgets for a kind of work rather than for an endpoint, and the
// document says so where it justifies them: `Upload`'s thirty "covers a provider's four
// verification documents *and* a job's proof set in one burst", which are two different routes
// sharing one allowance. Per-route buckets would multiply every figure by the number of routes in
// its class — 36 × 60 writes in a burst rather than 60 — and §7's conclusion that an override
// belongs at the class rather than the route is the same judgement from the other end.
//
// So the key is the class and the caller, and every route in a class spends from one bucket.
//
// # Why this must run inside the auth guard, and what happens if it does not
//
// The 74 enforced routes key on the authenticated caller, which only exists once the guard has
// run. Wrapped the other way round — limiter outside guard — an unauthenticated request has no
// caller to key on, so every one of them keys on the same value, and **the first attacker to
// empty that bucket refuses every anonymous request to every route in the class**. A limiter that
// converts unauthenticated traffic into a denial of service for everybody is worse than no
// limiter, so attachRoutes wraps guard(limit(handler)) and TestTheLimiterRunsInsideTheGuard holds
// it there.
//
// The consequence, stated rather than hidden: traffic that never gets past the guard is not
// counted by this middleware at all. Bounding *that* is what Docs/12 §8's address-keyed classes
// are for, and they wait for SHIP-183b.

// RateLimiter is what this middleware needs of a limiter.
//
// Declared here, by the consumer, and satisfied by *[ratelimit.Limiter] and by
// *[ratelimit.MemoryLimiter] — the same rule the domain packages follow for their adapters
// (Docs/06 §4.1) and the same shape [IdempotencyStore] takes.
//
// It is [ratelimit.Limiter.Spend] rather than Allow because Docs/12 §3 charges every request on
// every class this middleware serves. Only `Credential` charges failures alone, and that class is
// enforced inside identity and admin where the outcome is known — not here, where it is not.
type RateLimiter interface {
	Spend(ctx context.Context, key string, b ratelimit.Bucket) (ratelimit.Decision, error)
}

// Limit returns middleware charging one unit per request against the bucket key names.
//
// Both arguments are required. A nil limiter or a nil key function is a wiring mistake rather
// than a request problem, so it panics at startup: the alternative is a route that looks limited
// on the manifest and is not, which is the one failure a rate limit cannot report on its own.
func Limit(limiter RateLimiter, bucket ratelimit.Bucket, key func(*http.Request) string) func(http.Handler) http.Handler {
	switch {
	case limiter == nil:
		panic("httpx: Limit needs a limiter; without one the route would be served unlimited")
	case key == nil:
		panic("httpx: Limit needs a key function; without one every caller shares one bucket")
	case !bucket.Valid():
		panic("httpx: Limit needs a bucket that describes a limit")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			decision, err := limiter.Spend(r.Context(), key(r), bucket)
			if err != nil {
				writeLimiterFailure(w, r, err)
				return
			}
			if !decision.Allowed {
				wait := retryAfterSeconds(decision.RetryAfter)
				w.Header().Set("Retry-After", wait)
				WriteError(w, r, NewError(http.StatusTooManyRequests, CodeRateLimited,
					"Too many requests. Wait %s seconds and try again.", wait))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// writeLimiterFailure refuses a request whose limit could not be established.
//
// **Fail closed, and answer 503 rather than 429**, which is Docs/12 §4's rule for every class: a
// limiter that fails open is one an attacker disables by making Redis unreachable, and "this is
// temporarily unavailable" is true where "you have done too much" is not.
//
// The cost is worth naming rather than discovering. [Idempotent] already refuses every
// state-changing request when Redis is unreachable, so for those routes this changes nothing —
// but read-only routes pass through idempotency untouched, and this is the first Redis dependency
// on the read surface. **A Redis outage therefore takes the reads down too.** That is the
// direction Docs/12 chose knowingly; the alternative is a read surface that becomes unlimited at
// exactly the moment the platform is least able to absorb it.
func writeLimiterFailure(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ratelimit.ErrUnavailable) {
		LoggerFrom(r.Context()).LogAttrs(r.Context(), slog.LevelWarn,
			"a rate limit could not be established, so the request was refused",
			slog.String("error", err.Error()))

		w.Header().Set("Retry-After", "1")
		WriteError(w, r, NewError(http.StatusServiceUnavailable, CodeUnavailable,
			"This service is temporarily unavailable. Try again shortly.").WithCause(err))
		return
	}

	// Anything else is this package or its caller being wrong — an empty key, a bucket that
	// describes no limit. It is still refused rather than allowed, because a limit that could
	// not be applied is not a limit that found room.
	WriteError(w, r, NewError(http.StatusInternalServerError, CodeInternal,
		"Something went wrong on our end. Try again shortly.").WithCause(err))
}

// retryAfterSeconds renders a wait for the header, rounded up and never below one.
//
// Up rather than to-nearest because a client that waits exactly as long as it was told must find
// a token when it returns; rounding down sends it back a fraction of a second early, into a
// second refusal that it was explicitly promised would not happen. Never zero for the same
// reason — "Retry-After: 0" invites an immediate retry that cannot succeed.
func retryAfterSeconds(d time.Duration) string {
	seconds := int(math.Ceil(d.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}
