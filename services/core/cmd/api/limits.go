package main

import (
	"math"
	"net/http"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/ratelimit"
)

// The rate-limit classes (SHIP-183, SHIP-183a, SHIP-183b).
//
// Docs/12 is the authority for every figure in this file and for which class each route belongs
// to. This is the transcription; changing a number here without changing the reason there is how
// the reasons rot, and TestEveryRouteMatchesItsDocumentedClass fails a tree where the two have
// drifted in either direction.
//
// # Why a class rather than a number per route
//
// Eighty-six pairs of numbers is not reviewable and drifts the first time somebody adds a route by
// copying its neighbour (Docs/12 §2). What a reviewer checks on a new route is one word.
//
// It is also the only shape in which the *Done when* is achievable. "A route registered without a
// limit fails a gate rather than defaulting to unlimited" needs a field with no usable zero value,
// so [LimitUnset] is the zero and [register] refuses it — which makes `Unlimited` something
// somebody typed rather than something nobody did.

// LimitClass is which rate limit a route is served under.
//
// Declared beside [Auth] and for the same reason: it is a property of the endpoint that a reviewer
// should be able to read off the route table rather than reconstruct from middleware.
type LimitClass int

const (
	// LimitUnset is the zero value and is not a limit. A route carrying it does not register.
	LimitUnset LimitClass = iota

	// LimitUnlimited is never charged. One route has it — GET /health — because the caller is
	// the infrastructure and throttling a health check takes a healthy instance out of
	// rotation, converting a limiter into an outage (Docs/12 §3).
	LimitUnlimited

	// LimitCredential is a route that tests a secret. Keyed on the identifier *and* the
	// address, and charged on failures only, which is what makes it a control on guessing
	// rather than a cap on how often somebody may sign in.
	//
	// **Its figures are not in this file**, and that is deliberate rather than an omission:
	// the class needs two buckets and an outcome, so it is enforced inside internal/identity
	// and internal/admin where the outcome is known. Duplicating the numbers here would be a
	// second source of truth for them. See limitBucket.
	LimitCredential

	// LimitMessage is a route that causes an email or an SMS. Keyed on the address, because
	// what it bounds is one caller walking a list of destinations — which burns the sending
	// reputation and enumerates accounts at the same time (Docs/12 §3).
	LimitMessage

	// LimitUpload is a route that mints a pre-signed URL. The cost is bytes in the object
	// store, and a bucket refill does not give them back.
	LimitUpload

	// LimitWrite is an authenticated state change.
	//
	// **It is not a control on consequence.** Awarding a bid, deleting an account and
	// approving a suspension are all LimitWrite, and a bucket would make none of them safer —
	// doing one of those once is the whole harm. Authorisation and the two-person rule stand
	// in front of those; a rate limit bounds volume and has never bounded consequence.
	LimitWrite

	// LimitRead is an authenticated read.
	LimitRead

	// LimitPublicRead is an unauthenticated read. Deliberately the loosest class: the key is a
	// network address, so one corporate NAT is one caller, and these are hit by every
	// application launch.
	LimitPublicRead
)

// String renders the class as the golden file and the documents spell it.
func (c LimitClass) String() string {
	switch c {
	case LimitUnlimited:
		return "unlimited"
	case LimitCredential:
		return "credential"
	case LimitMessage:
		return "message"
	case LimitUpload:
		return "upload"
	case LimitWrite:
		return "write"
	case LimitRead:
		return "read"
	case LimitPublicRead:
		return "public-read"
	default:
		return "unset"
	}
}

// keyedOnSubject reports whether the class counts against the authenticated caller.
//
// These are the classes SHIP-183a enforced, and they were first because they survive a proxy
// untouched: a user id, an administrator's session and a driver's job token are all things the
// caller presented rather than things the network reported (Docs/12 §8). The rest waited for
// SHIP-183b — see keyedOnAddress.
func (c LimitClass) keyedOnSubject() bool {
	switch c {
	case LimitUpload, LimitWrite, LimitRead:
		return true
	default:
		return false
	}
}

// keyedOnAddress reports whether the class counts against the client's network address.
//
// **These are the eleven routes of Docs/12 §8, and SHIP-183b is what made them enforceable.**
// SHIP-183a declared the class on each of them and enforced none, because internal/httpx read
// RemoteAddr alone: behind a load balancer that is the balancer, so all eleven would have
// collapsed into one bucket per class and throttled the world at the first caller.
//
// [httpx.ResolveClientAddr] closed it. The address now comes from a forwarded header only as far
// as config.TrustedProxy says it may, and from RemoteAddr otherwise, so neither being behind a
// proxy nor being in front of one lets a caller choose their own bucket.
//
// Six of the eleven are enforced by the middleware in this package — the three LimitMessage and
// the three LimitPublicRead. The five LimitCredential routes are enforced inside internal/identity
// and internal/admin, because that class charges failures only and this middleware runs before the
// handler. See limitBucket.
func (c LimitClass) keyedOnAddress() bool {
	switch c {
	case LimitCredential, LimitMessage, LimitPublicRead:
		return true
	default:
		return false
	}
}

// limitBuckets holds the figures for the classes this file is the source of for.
//
// Docs/12 §3 is where each pair is argued. Sustained rate is one unit per interval, burst is the
// capacity, and refill-from-empty is capacity × interval — the three are not independent, which is
// why the document's table lists all three and this holds only the two that determine them.
//
// LimitCredential is absent on purpose (see its comment) and LimitUnlimited is absent because it
// is the class of having no bucket at all.
var limitBuckets = map[LimitClass]ratelimit.Bucket{
	// Ten in a burst and one back every five minutes: far past anybody registering, far under
	// what a list-walker needs.
	LimitMessage: {Capacity: 10, Interval: 5 * time.Minute},

	// Thirty covers a provider's four verification documents and a job's proof set in one
	// burst, with thirty an hour sustained after that.
	LimitUpload: {Capacity: 30, Interval: 2 * time.Minute},

	// Sixty in a burst is unreachable by a person and trivially reachable by a retry loop,
	// which is the line this class is drawn on: it turns a runaway client into a 429 instead
	// of a load event.
	LimitWrite: {Capacity: 60, Interval: 5 * time.Second},

	// Three hundred absorbs a cold start where a client opens several screens at once; 3600 an
	// hour is well past any legitimate client and well under what makes scraping worthwhile.
	LimitRead: {Capacity: 300, Interval: time.Second},

	// The loosest, because the key is a network address and one corporate NAT is one caller.
	LimitPublicRead: {Capacity: 600, Interval: time.Second},
}

// limitScale is the global lever Docs/12 §7 decided on.
//
// # Why global rather than per route
//
// Eighty-six routes is 172 environment variables and nobody maintains 172 of them correctly. Worse,
// a per-route override is a number that can drift from the reason written beside it in Docs/12 with
// nothing to notice — the exact failure that document exists to prevent, reintroduced through the
// back door.
//
// What an incident actually wants is "everything tighter, now" while something is being attacked,
// or "looser, now" because a real customer's NAT is being throttled. Both are global.
//
// # Why two scalars rather than one
//
// Capacity governs the burst and interval governs the sustained rate, and they are independent:
// scaling capacity alone moves what a cold start can absorb and leaves the long-run rate untouched.
// One scalar cannot express "absorb a bigger burst but hold the same hourly rate", which is what a
// client-side retry storm wants.
//
// The revisit trigger is named in Docs/12 §10: the first incident that genuinely wants one route
// different from the rest, at which point the answer is a per-**class** override map — seven values
// a person can hold in their head, against eighty-six they cannot.
type limitScale struct {
	// burst multiplies every class's capacity.
	burst float64

	// rate multiplies every class's sustained rate, which means it *divides* the interval: at
	// 2.0 a token comes back twice as fast.
	rate float64
}

// apply returns b scaled, or b unchanged when the scale says nothing.
//
// A non-positive scale is treated as 1.0 rather than as a limit of zero. config.Load defaults both
// to 1.0 and refuses anything outside its bounds, so the only way to arrive here with a zero is a
// zero-valued config.Config — which is a test's, and a test that had every route refuse everything
// would be a confusing way to learn that a struct literal was incomplete.
func (s limitScale) apply(b ratelimit.Bucket) ratelimit.Bucket {
	burst, rate := s.burst, s.rate
	if burst <= 0 {
		burst = 1
	}
	if rate <= 0 {
		rate = 1
	}

	// At least one token, or the class would refuse every request including the first — a
	// scale is a dial, not an off switch.
	capacity := int(math.Round(float64(b.Capacity) * burst))
	if capacity < 1 {
		capacity = 1
	}

	// At least a millisecond, which is the resolution the bucket script's arithmetic works in.
	interval := time.Duration(math.Round(float64(b.Interval) / rate))
	if interval < time.Millisecond {
		interval = time.Millisecond
	}

	return ratelimit.Bucket{Capacity: capacity, Interval: interval}
}

// limitScaleFrom reads the global lever off the configuration.
//
// A nil Config is a test's rather than a deployment's — main.go cannot reach here without one — and
// it means the unscaled figures, which is what limitScale.apply does with a zero.
func limitScaleFrom(cfg *config.Config) limitScale {
	if cfg == nil {
		return limitScale{}
	}
	return limitScale{burst: cfg.RateLimits.BurstScale, rate: cfg.RateLimits.RateScale}
}

// limitBucket returns the bucket a class is enforced with by this middleware, and whether it is
// enforced here at all.
//
// A class is enforced here exactly when limitBuckets holds figures for it. Two do not, and each is
// a different reason rather than an oversight:
//
//   - LimitUnlimited has no bucket by definition. GET /health is the one route with it.
//   - LimitCredential is enforced inside internal/identity and internal/admin, where the outcome
//     of the credential check is known. The class charges failures only (Docs/12 §3), and this
//     middleware runs before the handler and cannot see an outcome. Holding its figures here as
//     well would be a second source of truth for numbers only those two packages can apply.
//
// TestEveryClassIsEitherEnforcedOrAccountedFor is what keeps that list at two, so a class added
// with no figures is a test failure rather than a route quietly served unlimited.
func limitBucket(c LimitClass, scale limitScale) (ratelimit.Bucket, bool) {
	b, ok := limitBuckets[c]
	if !ok {
		return ratelimit.Bucket{}, false
	}
	return scale.apply(b), true
}

// limitKey names the bucket one request counts against.
//
// # Two kinds of caller, because two kinds of class
//
// A subject-keyed class counts against who the caller proved they are; an address-keyed class
// counts against where the request came from, because on those routes nobody has proved anything
// yet. [httpx.ClientAddr] is what answers the second, under the deployment's trusted-proxy rules
// and never from the raw header — so a caller can no more choose their bucket by writing an
// X-Forwarded-For than an account holder can by claiming a different user id (SHIP-183b).
//
// # The caller, from what they presented rather than from a lookup
//
// [httpx.SubjectScope] already answers "who is this, without a database read that can fail" for the
// idempotency middleware, and the answer is the right one here for the same reasons it was there:
//
//	user:<id>          an account holder, from the verified subject
//	credential:<hash>  an administrator's console session or a driver's job-scoped token
//	anonymous          nothing was presented
//
// **`anonymous` is unreachable on these routes**, because the middleware runs inside the auth guard
// and the guard has already refused a caller who presented nothing. That ordering is not an
// optimisation — see the note in internal/httpx/ratelimit.go for what happens without it.
//
// # Where this is narrower than Docs/12 §5's wording, and why that is the safe direction
//
// The document says the driver classes key on "the job" and the rest on "the subject". An account
// holder is keyed on the account, exactly as written. The other two credential systems are keyed on
// the **credential** instead, because internal/admin's and internal/delivery's grant accessors are
// unexported — deliberately, so that nothing outside those domains can read a grant at all — and
// cmd/api must not export them for a rate-limit key.
//
// Keying on the credential is strictly tighter than keying on the job or the administrator: two
// driver links for one job are two buckets rather than one, so a driver still cannot spend another
// job's allowance, and a leaked link is still bounded to the job it was issued for. The one thing
// it loosens is worth naming rather than burying — **an administrator who signs in again gets a
// fresh allowance**, because a new session is a new credential. That bypass is self-limiting: a
// sign-in costs an argon2id derivation and is itself LimitCredential-classed, so it is a slower way
// to spend an allowance than simply waiting for the refill.
//
// # Why the class is in the key
//
// So that a caller's reads and their writes are separate budgets, and so that every route in a
// class shares one — which is the shape Docs/12 §3's figures were argued for. See the note in
// internal/httpx/ratelimit.go.
func limitKey(c LimitClass) func(*http.Request) string {
	if c.keyedOnAddress() {
		return func(r *http.Request) string {
			return "route:" + c.String() + ":" + httpx.ClientAddr(r)
		}
	}
	return func(r *http.Request) string {
		return "route:" + c.String() + ":" + httpx.SubjectScope(r)
	}
}
