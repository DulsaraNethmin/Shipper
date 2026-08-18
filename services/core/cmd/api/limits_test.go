package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/idempotency"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/ratelimit"
)

// SHIP-183a's acceptance criteria, and the document they are transcribed from.
//
// The *Done when* has three clauses and each has a test below:
//
//   - every subject-keyed route enforces its class and answers a typed error with an honest
//     Retry-After — TestASubjectKeyedRouteRefusesAtCapacity, TestARefusalCarriesAnHonestRetryAfter;
//   - a route registered without a limit fails a gate rather than defaulting to unlimited —
//     TestARouteWithNoClassDoesNotRegister, TestARouteWithNoClassIsNotAttached;
//   - the eleven address-keyed routes carry their class and are not yet enforced —
//     TestAddressKeyedRoutesCarryTheirClassAndAreNotEnforced.

// rateLimitDocPath is Docs/12, the authority for every class, relative to this package.
const rateLimitDocPath = "../../../../Docs/12-rate-limits.md"

// documentedClasses reads Docs/12 §5's assignment table.
//
// # Why the test reads the document rather than a second copy of it
//
// Docs/12 §5 is the source SHIP-183a transcribes, and a transcription is exactly the kind of work
// that is correct on the day and wrong six months later — a route added with the neighbouring
// class, a class changed in the document and not in the code, or the reverse. Comparing the
// manifest with a Go table would only prove the transcription matched itself.
//
// The parse is deliberately strict. A row it cannot read is a failure rather than a skip: a
// silently-unparsed row is a route nobody is checking, which is the failure this test exists to
// prevent, dressed as a pass.
func documentedClasses(t *testing.T) map[string]LimitClass {
	t.Helper()

	source, err := os.ReadFile(rateLimitDocPath)
	if err != nil {
		t.Fatalf("reading %s: %v", rateLimitDocPath, err)
	}

	text := string(source)
	start := strings.Index(text, "## 5. The assignment")
	if start < 0 {
		t.Fatalf("%s has no section 5; it is where the assignment lives", rateLimitDocPath)
	}
	end := strings.Index(text[start:], "\n## 6.")
	if end < 0 {
		t.Fatalf("%s section 5 does not end; the parse would run into the rest of the document",
			rateLimitDocPath)
	}
	section := text[start : start+end]

	// | METHOD | `path` | auth | `Class` … |
	//
	// The class cell carries prose after the name on four rows — "`Credential` — address
	// bucket only", "`Message` + the existing silent destination limit" — so only the first
	// backticked word is read, and the prose is left to a human.
	row := regexp.MustCompile(`(?m)^\|\s*([A-Z]+)\s*\|\s*` + "`" + `([^` + "`" + `]+)` + "`" +
		`\s*\|\s*([^|]+?)\s*\|\s*` + "`" + `([A-Za-z]+)` + "`" + `[^|]*\|\s*$`)

	byName := map[string]LimitClass{
		"Unlimited": LimitUnlimited, "Credential": LimitCredential, "Message": LimitMessage,
		"Upload": LimitUpload, "Write": LimitWrite, "Read": LimitRead,
		"PublicRead": LimitPublicRead,
	}

	documented := map[string]LimitClass{}
	for _, m := range row.FindAllStringSubmatch(section, -1) {
		class, known := byName[m[4]]
		if !known {
			t.Fatalf("%s assigns %s %s the class %q, which is not one of the seven",
				rateLimitDocPath, m[1], m[2], m[4])
		}
		documented[m[1]+" "+m[2]] = class
	}

	if len(documented) == 0 {
		t.Fatalf("%s section 5 parsed to no rows at all; the table's shape has changed and this "+
			"test is no longer reading it", rateLimitDocPath)
	}
	return documented
}

// TestEveryRouteMatchesItsDocumentedClass compares the manifest with Docs/12 §5 in both
// directions.
//
// Both directions matter and they catch different mistakes. A route in the manifest and not in the
// document is an endpoint whose limit nobody argued; a route in the document and not in the
// manifest is a decision about something that does not exist, which is how a document starts
// describing a service that has moved on.
func TestEveryRouteMatchesItsDocumentedClass(t *testing.T) {
	documented := documentedClasses(t)

	served := map[string]LimitClass{}
	for _, r := range routes() {
		served[r.Method+" "+r.fullPath()] = r.Limit
	}

	for name, want := range documented {
		got, exists := served[name]
		switch {
		case !exists:
			t.Errorf("%s assigns %s a class, but no route serves it", rateLimitDocPath, name)
		case got != want:
			t.Errorf("%s is %s in the manifest and %s in %s", name, got, want, rateLimitDocPath)
		}
	}

	for name := range served {
		if _, exists := documented[name]; !exists {
			t.Errorf("%s is served and %s does not assign it a class. Every route belongs to "+
				"one — add a row to §5 with the reason before adding the route",
				name, rateLimitDocPath)
		}
	}
}

// TestTheClassCountsAreWhatTheDocumentClaims checks §5's headline figures.
//
// §5 states the seven counts in prose above the table, and prose is where a figure rots first: the
// table can be edited correctly and the sentence above it left alone. The counts are recomputed
// from the served manifest rather than from the table, so this fails whichever of the two moved.
func TestTheClassCountsAreWhatTheDocumentClaims(t *testing.T) {
	// Docs/12 §5: "Counts: Unlimited 1, Credential 5, Message 3, Upload 3, Write 36, Read 35,
	// PublicRead 3."
	want := map[LimitClass]int{
		LimitUnlimited: 1, LimitCredential: 5, LimitMessage: 3, LimitUpload: 3,
		LimitWrite: 36, LimitRead: 35, LimitPublicRead: 3,
	}

	got := map[LimitClass]int{}
	for _, r := range routes() {
		got[r.Limit]++
	}

	for class, n := range want {
		if got[class] != n {
			t.Errorf("%d routes are %s, %s says %d", got[class], class, rateLimitDocPath, n)
		}
	}

	total := 0
	for _, n := range got {
		total += n
	}
	if total != len(routes()) {
		t.Errorf("the classes account for %d routes and the manifest serves %d",
			total, len(routes()))
	}
}

// TestNoRouteIsServedWithoutAClass is the invariant the two gates below enforce, asserted over the
// real manifest rather than over a probe.
func TestNoRouteIsServedWithoutAClass(t *testing.T) {
	for _, r := range routes() {
		if r.Limit == LimitUnset {
			t.Errorf("%s %s has no rate-limit class", r.Method, r.fullPath())
		}
	}
}

// TestARouteWithNoClassDoesNotRegister is the first half of SHIP-183a's second clause: a route
// registered without a limit fails a gate rather than defaulting to unlimited.
func TestARouteWithNoClassDoesNotRegister(t *testing.T) {
	defer func() {
		p := recover()
		if p == nil {
			t.Fatal("registering a route with no limit class was accepted, so a new endpoint " +
				"can reach a mux with no bucket and nothing says so")
		}
		if msg, _ := p.(string); !strings.Contains(msg, "rate-limit class") {
			t.Errorf("the panic does not say what is missing: %v", p)
		}
	}()

	register(Route{
		Method:  http.MethodPost,
		Pattern: "/unclassified-probe",
		Group:   GroupV1,
		Auth:    RequireUser,
		Handler: func(Deps) http.Handler { return http.NotFoundHandler() },
	})
}

// TestARouteWithNoClassIsNotAttached is the second half, and it is not redundant.
//
// register guards the registry; this guards the mux. A Route literal handed straight to
// attachRoutes never passes through the registry at all — which is what several tests in this
// package do — so without this the gate has a bypass that the ordinary route table would never
// reveal.
func TestARouteWithNoClassIsNotAttached(t *testing.T) {
	defer func() {
		p := recover()
		if p == nil {
			t.Fatal("attaching a route with no limit class was accepted, so the registry is the " +
				"only gate and a literal handed to a mux escapes it")
		}
		if msg, _ := p.(string); !strings.Contains(msg, "unlimited") {
			t.Errorf("the panic does not say what serving it would mean: %v", p)
		}
	}()

	attachRoutes(http.NewServeMux(), []Route{{
		Method:  http.MethodGet,
		Pattern: "/unclassified-probe",
		Group:   GroupV1,
		Auth:    Public,
		Handler: func(Deps) http.Handler { return http.NotFoundHandler() },
	}}, GroupV1, testDeps(), nil, testLimiter())
}

// TestAddressKeyedRoutesCarryTheirClassAndAreNotEnforced is SHIP-183a's third clause.
//
// The eleven routes Docs/12 §8 names key on the network address, and internal/httpx does not read
// X-Forwarded-For. Enforcing them behind a load balancer would put every caller in one bucket per
// class and refuse the world at the first request. They carry the class so that SHIP-183b is a
// transcription rather than a second review, and they are not enforced until it lands.
func TestAddressKeyedRoutesCarryTheirClassAndAreNotEnforced(t *testing.T) {
	var addressKeyed []Route
	for _, r := range routes() {
		if r.Limit.keyedOnAddress() {
			addressKeyed = append(addressKeyed, r)
		}
	}

	// Docs/12 §8: "The five Credential, three Message and three PublicRead routes are the
	// eleven."
	if len(addressKeyed) != 11 {
		t.Errorf("%d routes are keyed on the address, %s §8 says eleven",
			len(addressKeyed), rateLimitDocPath)
	}

	for _, r := range addressKeyed {
		if _, enforced := limitBucket(r.Limit, limitScale{}); enforced {
			t.Errorf("%s %s is %s and is enforced. It keys on the client address, which is the "+
				"load balancer's until SHIP-183b reads a forwarded header from a configured "+
				"trusted proxy — enforcing it now throttles every caller as one",
				r.Method, r.fullPath(), r.Limit)
		}
	}
}

// TestOnlyHealthIsUnlimited holds the one deliberate exemption at one route.
//
// Docs/12 §3: the caller is the infrastructure, and throttling a health check takes a healthy
// instance out of rotation. That argument applies to nothing else on the manifest, and this fails
// if a second route ever borrows it.
func TestOnlyHealthIsUnlimited(t *testing.T) {
	var unlimited []string
	for _, r := range routes() {
		if r.Limit == LimitUnlimited {
			unlimited = append(unlimited, r.Method+" "+r.fullPath())
		}
	}
	sort.Strings(unlimited)

	if want := []string{"GET /health"}; len(unlimited) != 1 || unlimited[0] != want[0] {
		t.Errorf("unlimited routes are %v, want %v. %s §3 argues the exemption from the caller "+
			"being the infrastructure, which is true of /health and of nothing else",
			unlimited, want, rateLimitDocPath)
	}
}

// TestEverySubjectKeyedClassHasTheDocumentedFigures checks the numbers rather than the assignment.
//
// Docs/12 §3's table is the other half of the transcription, and it is the half a reviewer is
// least likely to re-derive: a capacity is a plausible number whatever it is.
func TestEverySubjectKeyedClassHasTheDocumentedFigures(t *testing.T) {
	// Docs/12 §3, the three classes this middleware enforces.
	want := map[LimitClass]ratelimit.Bucket{
		LimitUpload: {Capacity: 30, Interval: 2 * time.Minute},
		LimitWrite:  {Capacity: 60, Interval: 5 * time.Second},
		LimitRead:   {Capacity: 300, Interval: time.Second},
	}

	for class, bucket := range want {
		got, enforced := limitBucket(class, limitScale{})
		if !enforced {
			t.Errorf("%s is not enforced and it keys on the subject", class)
			continue
		}
		if got != bucket {
			t.Errorf("%s is %+v, %s §3 says %+v", class, got, rateLimitDocPath, bucket)
		}
	}
}

// TestTheGlobalScalesMoveBurstAndRateIndependently is Docs/12 §7's decision, exercised.
//
// One scalar cannot express "absorb a bigger burst but hold the same hourly rate", which is what a
// client-side retry storm wants — so the test that matters is that each dial moves one thing and
// leaves the other alone.
func TestTheGlobalScalesMoveBurstAndRateIndependently(t *testing.T) {
	base, _ := limitBucket(LimitWrite, limitScale{})

	burst, _ := limitBucket(LimitWrite, limitScale{burst: 2, rate: 1})
	if burst.Capacity != base.Capacity*2 {
		t.Errorf("doubling the burst scale gave capacity %d, want %d",
			burst.Capacity, base.Capacity*2)
	}
	if burst.Interval != base.Interval {
		t.Errorf("the burst scale moved the interval to %s; it governs capacity alone",
			burst.Interval)
	}

	rate, _ := limitBucket(LimitWrite, limitScale{burst: 1, rate: 2})
	if rate.Interval != base.Interval/2 {
		t.Errorf("doubling the rate scale gave interval %s, want %s",
			rate.Interval, base.Interval/2)
	}
	if rate.Capacity != base.Capacity {
		t.Errorf("the rate scale moved the capacity to %d; it governs the interval alone",
			rate.Capacity)
	}
}

// TestAScaleNeverBecomesAnOffSwitch holds the floor under both dials.
//
// A scale is a dial an operator turns during an incident, and one that can reach zero is a way to
// refuse every request on the API including the first. The floor is one token and one millisecond.
func TestAScaleNeverBecomesAnOffSwitch(t *testing.T) {
	tiny, _ := limitBucket(LimitWrite, limitScale{burst: 0.0001, rate: 100000})
	if tiny.Capacity < 1 {
		t.Errorf("capacity scaled to %d; a scale must not reach zero", tiny.Capacity)
	}
	if tiny.Interval < time.Millisecond {
		t.Errorf("interval scaled to %s; below a millisecond is not a rate limit", tiny.Interval)
	}
}

// TestAnUnsetScaleIsTheDocumentedFigures is what a zero-valued config.Config means.
//
// A deployment that sets neither variable runs exactly the numbers Docs/12 §3 argues for, which is
// what makes those reasons worth reading rather than merely present.
func TestAnUnsetScaleIsTheDocumentedFigures(t *testing.T) {
	for _, class := range []LimitClass{LimitUpload, LimitWrite, LimitRead} {
		unscaled, _ := limitBucket(class, limitScale{})
		if got, _ := limitBucket(class, limitScale{burst: 1, rate: 1}); got != unscaled {
			t.Errorf("%s unscaled is %+v and at 1.0 is %+v", class, unscaled, got)
		}
	}
}

// --- enforcement, through the real router --------------------------------------------------

// limitedRequest builds an authenticated GET the router will route to a Read-class endpoint.
//
// GET /v1/auth/sessions is used because it is RequireUser, it is LimitRead, and it needs no
// database to be refused by the limiter — the limiter runs before the handler, so a route whose
// handler would fail on a nil pool still proves what this is asserting.
func limitedRequest(token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/v1/auth/sessions", nil)
	r.Header.Set(httpxAuthorization, "Bearer "+token)
	return r
}

// httpxAuthorization is spelled out rather than imported so this file does not depend on the
// header constant's package for one string.
const httpxAuthorization = "Authorization" // spelling:ok — HTTP header name, RFC 9110

func limitedRouter(limiter *ratelimit.MemoryLimiter) http.Handler {
	return newRouter(testDeps(), idempotency.NewMemoryStore(), limiter,
		testAuthenticator(), testDriverGuard(), testAdminGuard())
}

// TestASubjectKeyedRouteRefusesAtCapacity is SHIP-183a's first clause.
func TestASubjectKeyedRouteRefusesAtCapacity(t *testing.T) {
	limiter := testLimiter()
	router := limitedRouter(limiter)
	token, _, _ := testAccessToken(t, identity.RoleCustomer)

	read, _ := limitBucket(LimitRead, limitScale{})

	// The bucket starts full, so exactly Capacity requests are admitted and the next is not.
	for i := range read.Capacity {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, limitedRequest(token))
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d of %d was refused; the bucket holds %d",
				i+1, read.Capacity, read.Capacity)
		}
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, limitedRequest(token))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("request %d answered %d, want 429 — the %s bucket holds %d",
			read.Capacity+1, rec.Code, LimitRead, read.Capacity)
	}
}

// TestARefusalCarriesAnHonestRetryAfter is the rest of that clause: a *typed* error with a wait a
// client can act on.
func TestARefusalCarriesAnHonestRetryAfter(t *testing.T) {
	limiter := testLimiter()
	router := limitedRouter(limiter)
	token, _, _ := testAccessToken(t, identity.RoleCustomer)

	read, _ := limitBucket(LimitRead, limitScale{})
	var rec *httptest.ResponseRecorder
	for range read.Capacity + 1 {
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, limitedRequest(token))
	}

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("answered %d, want 429", rec.Code)
	}

	// The typed error, which is what a client branches on. Docs/10 §4.4: never the message.
	var body struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	decodeJSON(t, rec, &body)

	if body.Error.Code != "rate_limited" {
		t.Errorf("code is %q, want %q", body.Error.Code, "rate_limited")
	}
	if body.Error.RequestID == "" {
		t.Error("the refusal carries no request ID, so a report of it cannot be traced")
	}

	// Honest: the wait is the time until the next token, and a client that waits exactly that
	// long must find one. Never zero, which would invite an immediate retry that cannot work.
	header := rec.Header().Get("Retry-After")
	if header == "" {
		t.Fatal("a 429 with no Retry-After; the header is the whole difference between a " +
			"refusal a client can act on and one it can only guess at")
	}
	if header == "0" {
		t.Error(`Retry-After is "0", which invites a retry that is certain to be refused`)
	}

	// The Read bucket returns a token every second, so the wait is one second and not the
	// capacity × interval a fixed window would report.
	if header != "1" {
		t.Errorf("Retry-After is %q; the %s bucket returns a token every %s, so an honest wait "+
			"is 1 second — a larger figure means a window rolled over rather than a token "+
			"returning", header, LimitRead, read.Interval)
	}
}

// TestTwoCallersDoNotShareABucket is the property that makes the limit a control on one caller
// rather than a shared allowance the first arrival exhausts.
func TestTwoCallersDoNotShareABucket(t *testing.T) {
	limiter := testLimiter()
	router := limitedRouter(limiter)

	first, _, _ := testAccessToken(t, identity.RoleCustomer)
	second, _, _ := testAccessToken(t, identity.RoleCustomer)

	read, _ := limitBucket(LimitRead, limitScale{})
	for range read.Capacity + 1 {
		router.ServeHTTP(httptest.NewRecorder(), limitedRequest(first))
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, limitedRequest(second))
	if rec.Code == http.StatusTooManyRequests {
		t.Error("one caller emptying their bucket refused another's request; the key is not " +
			"separating them and the first arrival throttles everybody")
	}
}

// TestReadsAndWritesAreSeparateBudgets is why the class is part of the key.
func TestReadsAndWritesAreSeparateBudgets(t *testing.T) {
	limiter := testLimiter()
	token, _, _ := testAccessToken(t, identity.RoleCustomer)

	read, _ := limitBucket(LimitRead, limitScale{})
	for range read.Capacity + 1 {
		limiter.Spend(t.Context(), limitKey(LimitRead)(limitedRequest(token)), read)
	}

	write, _ := limitBucket(LimitWrite, limitScale{})
	decision, err := limiter.Spend(t.Context(), limitKey(LimitWrite)(limitedRequest(token)), write)
	if err != nil {
		t.Fatalf("spending from the write bucket: %v", err)
	}
	if !decision.Allowed {
		t.Error("emptying the read bucket refused a write; a caller's reads and writes are " +
			"separate budgets and the class is in the key to keep them so")
	}
}

// TestTheLimiterRunsInsideTheGuard is the ordering the whole design rests on.
//
// # What this is actually preventing
//
// These classes key on the authenticated caller. Wrapped the other way round, an unauthenticated
// request has no caller to key on, so every one of them keys on the same value — and the first
// attacker to empty *that* bucket refuses every anonymous request to every route in the class.
// A limiter converted into a denial of service is worse than no limiter.
//
// The observable consequence is the assertion: a request the guard refuses must not have spent
// anything, because it never reached the limiter at all.
func TestTheLimiterRunsInsideTheGuard(t *testing.T) {
	limiter := testLimiter()
	router := limitedRouter(limiter)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/auth/sessions", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("an unauthenticated request answered %d, want 401", rec.Code)
	}
	if keys := limiter.Keys(); len(keys) != 0 {
		t.Errorf("a request the guard refused spent from %v. The limiter is running outside the "+
			"guard, which puts every anonymous caller in one bucket — the first to empty it "+
			"refuses the rest", keys)
	}
}

// TestAnUnlimitedRouteSpendsNothing holds the exemption at the mux rather than only in the table.
func TestAnUnlimitedRouteSpendsNothing(t *testing.T) {
	limiter := testLimiter()
	router := limitedRouter(limiter)

	for range 50 {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("/health answered %d, want 200", rec.Code)
		}
	}

	if keys := limiter.Keys(); len(keys) != 0 {
		t.Errorf("/health spent from %v. It is the one unlimited route because the caller is a "+
			"load balancer, and throttling it takes a healthy instance out of rotation", keys)
	}
}

// TestAnUnreachableCacheRefusesRatherThanServingUnlimited is Docs/12 §4's fail-closed rule.
//
// A limiter that fails open is one an attacker disables by making Redis unreachable. The status is
// 503 rather than 429 because "this is temporarily unavailable" is true and "you have done too
// much" is not.
func TestAnUnreachableCacheRefusesRatherThanServingUnlimited(t *testing.T) {
	limiter := testLimiter()
	limiter.FailWith = ratelimit.ErrUnavailable

	router := limitedRouter(limiter)
	token, _, _ := testAccessToken(t, identity.RoleCustomer)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, limitedRequest(token))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("answered %d with an unreachable cache, want 503. Anything in the 2xx range "+
			"means the route is served unlimited whenever Redis is down, which is when an "+
			"attacker would arrange for it to be", rec.Code)
	}
}

// TestARouteLimit429StillReleasesTheIdempotencyKey is the clause Docs/12 §4 marks "must not
// disturb".
//
// httpx.Idempotent treats a 429 like a 5xx and does not record it, so a throttled client that
// retries with the same key is not answered forever out of a cached refusal. That behaviour
// predates this ticket and was written when exactly two routes could produce a 429; SHIP-183a
// takes that to 74, which is what turns a latent interaction into one worth a test.
//
// Without it the failure is quiet and permanent: the client's retry — the correct response to
// Retry-After — replays the 429 from the store rather than being re-evaluated, so the endpoint
// stays refused for that key even after the bucket has refilled.
func TestARouteLimit429StillReleasesTheIdempotencyKey(t *testing.T) {
	limiter := testLimiter()
	store := idempotency.NewMemoryStore()
	router := newRouter(testDeps(), store, limiter,
		testAuthenticator(), testDriverGuard(), testAdminGuard())

	token, _, _ := testAccessToken(t, identity.RoleCustomer)
	write, _ := limitBucket(LimitWrite, limitScale{})

	post := func(key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/jobs", strings.NewReader("{}"))
		r.Header.Set(httpxAuthorization, "Bearer "+token)
		r.Header.Set("Idempotency-Key", key)
		r.Header.Set("Content-Type", "application/json")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		return rec
	}

	// Empty the write bucket. These spend keys of their own, which is what makes the last one
	// below the first request to be refused by the limiter rather than by the store.
	for i := range write.Capacity {
		post(fmt.Sprintf("filling-%d", i))
	}

	throttled := post("the-retried-action")
	if throttled.Code != http.StatusTooManyRequests {
		t.Fatalf("answered %d, want 429 — the bucket holds %d and %d were spent",
			throttled.Code, write.Capacity, write.Capacity)
	}

	if store.Held("the-retried-action") {
		t.Error("the throttled request's idempotency key is still held, so the client's retry " +
			"replays the 429 from the store instead of being re-evaluated — and the endpoint " +
			"stays refused for that key after the bucket has refilled")
	}
}

// TestTheLimitKeyNamesTheCallerAndTheClass pins the key's shape.
//
// The shape is not cosmetic. `route:<class>:<caller>` is what makes every route in a class share
// one bucket per caller and no two callers share one, and it is what an operator greps for during
// an incident.
//
// # It is asserted through the router rather than by calling limitKey directly
//
// limitKey reads the subject off the request context, and the subject is put there by
// httpx.ResolveSubject, which wraps the whole /v1 group. A hand-built request carrying the same
// bearer header has no subject on it, so limitKey falls back to the credential digest and the test
// passes on a key the service never uses. Driving the real router is what makes this assertion
// about the deployed shape rather than about a helper called in isolation.
func TestTheLimitKeyNamesTheCallerAndTheClass(t *testing.T) {
	limiter := testLimiter()
	router := limitedRouter(limiter)
	token, userID, _ := testAccessToken(t, identity.RoleCustomer)

	router.ServeHTTP(httptest.NewRecorder(), limitedRequest(token))

	want := fmt.Sprintf("route:read:user:%s", userID)
	if keys := limiter.Keys(); len(keys) != 1 || keys[0] != want {
		t.Errorf("the request spent from %v, want exactly [%s]", keys, want)
	}
}

// TestAnUnauthenticatedRequestWouldShareOneBucket names the value every anonymous caller keys on.
//
// It is unreachable on these routes, because the guard refuses first — see
// TestTheLimiterRunsInsideTheGuard. It is written out because the *reason* the ordering matters is
// that this key is shared by everybody, and a test that asserts the ordering without naming what
// it prevents is one somebody will later reorder in good faith.
func TestAnUnauthenticatedRequestWouldShareOneBucket(t *testing.T) {
	anonymous := limitKey(LimitRead)(httptest.NewRequest(http.MethodGet, "/v1/auth/sessions", nil))
	if !strings.HasSuffix(anonymous, ":anonymous") {
		t.Errorf("an unauthenticated request keys on %q; the shared-bucket case the guard "+
			"ordering prevents is no longer the one it describes", anonymous)
	}
}

// TestTheDriverAndAdminClassesKeyOnTheirCredential records where SHIP-183a is narrower than
// Docs/12 §5's wording, so that the divergence is a decision on the record rather than a surprise.
//
// The document says the driver classes key on "the job". internal/delivery's grant accessor is
// unexported — deliberately, so nothing outside that domain can read a driver grant — so cmd/api
// keys on the credential instead. That is strictly tighter: one token is one job, so a driver
// still cannot spend another job's allowance, and two links for one job are two buckets rather
// than one.
func TestTheDriverAndAdminClassesKeyOnTheirCredential(t *testing.T) {
	// A hand-built request is the honest shape here, unlike in the test above: ResolveSubject
	// refuses a driver token — internal/identity rejects the driver audience, which is the
	// separation CLAUDE.md's third invariant names — so no subject reaches the context through
	// the router either, and the credential digest is what the service really keys on.
	withToken := func(raw string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/v1/driver/jobs/x", nil)
		r.Header.Set(httpxAuthorization, "Bearer "+raw)
		return r
	}

	first := limitKey(LimitRead)(withToken("one-job-token"))
	second := limitKey(LimitRead)(withToken("another-job-token"))

	if first == second {
		t.Fatal("two different job tokens key on the same bucket, so one job's driver spends " +
			"another's allowance")
	}
	for _, key := range []string{first, second} {
		if !strings.Contains(key, ":credential:") {
			t.Errorf("a bearer credential that is not a user keys on %q, want a credential "+
				"digest — see the note on limitKey", key)
		}
		if strings.Contains(key, "one-job-token") || strings.Contains(key, "another-job-token") {
			t.Errorf("the key %q carries the raw credential, which puts a bearer token into "+
				"Redis and into anything that can run SCAN", key)
		}
	}
}

// decodeJSON reads a recorded response body, failing the test rather than the caller.
func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder, into any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
		t.Fatalf("decoding %s: %v", rec.Body.String(), err)
	}
}
