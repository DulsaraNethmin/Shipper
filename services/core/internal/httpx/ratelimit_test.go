package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/ratelimit"
)

// The rate-limit middleware (SHIP-183a).
//
// What is asserted here is the mechanism: the status, the code, the header, and what happens when
// the limiter cannot answer. *Which* routes are limited is cmd/api's, and is checked against
// Docs/12 there.

// stubLimiter answers with whatever the test wants, including an error.
//
// A stub rather than ratelimit.MemoryLimiter, deliberately: the properties below are about
// responses to a decision, and reaching a specific RetryAfter through a real bucket would mean
// arranging the arithmetic that produces it. Parity between the memory limiter and the Redis one
// is asserted where it belongs, in internal/ratelimit.
type stubLimiter struct {
	decision ratelimit.Decision
	err      error

	calls int
	key   string
}

func (s *stubLimiter) Spend(_ context.Context, key string, _ ratelimit.Bucket) (ratelimit.Decision, error) {
	s.calls++
	s.key = key
	return s.decision, s.err
}

var testBucket = ratelimit.Bucket{Capacity: 10, Interval: time.Second}

func servedThrough(limiter RateLimiter, key func(*http.Request) string) *httptest.ResponseRecorder {
	handler := Limit(limiter, testBucket, key)(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

	rec := httptest.NewRecorder()
	StandardErrors(handler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", nil))
	return rec
}

func fixedKey(*http.Request) string { return "somebody" }

// TestAnAllowedRequestReachesTheHandler.
func TestAnAllowedRequestReachesTheHandler(t *testing.T) {
	limiter := &stubLimiter{decision: ratelimit.Decision{Allowed: true, Remaining: 9}}

	rec := servedThrough(limiter, fixedKey)

	if rec.Code != http.StatusOK {
		t.Errorf("answered %d, want 200", rec.Code)
	}
	if limiter.calls != 1 {
		t.Errorf("the limiter was called %d times, want once", limiter.calls)
	}
	if limiter.key != "somebody" {
		t.Errorf("charged %q, want the key function's answer", limiter.key)
	}
	if rec.Header().Get("Retry-After") != "" {
		t.Error("an allowed request carries a Retry-After")
	}
}

// TestARefusalIsATypedErrorWithAWait is the shape a client branches on (Docs/10 §4.4).
func TestARefusalIsATypedErrorWithAWait(t *testing.T) {
	limiter := &stubLimiter{decision: ratelimit.Decision{RetryAfter: 3 * time.Second}}

	rec := servedThrough(limiter, fixedKey)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("answered %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "3" {
		t.Errorf("Retry-After is %q, want %q", got, "3")
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding %s: %v", rec.Body.String(), err)
	}
	if body.Error.Code != string(CodeRateLimited) {
		t.Errorf("code is %q, want %q", body.Error.Code, CodeRateLimited)
	}
}

// TestTheWaitIsRoundedUp is what makes Retry-After honest rather than approximately honest.
//
// A client that waits exactly as long as it was told must find a token when it returns. Rounding
// to nearest sends it back a fraction of a second early, into a second refusal it was explicitly
// promised would not happen — and a client that trusts the header will loop on that.
func TestTheWaitIsRoundedUp(t *testing.T) {
	for _, tc := range []struct {
		wait time.Duration
		want string
	}{
		{1200 * time.Millisecond, "2"},
		{1001 * time.Millisecond, "2"},
		{time.Second, "1"},
		{999 * time.Millisecond, "1"},

		// Never zero. A bucket that reports no wait at all still gets a header telling the
		// client to pause, because "Retry-After: 0" invites an immediate retry that is
		// certain to be refused.
		{0, "1"},
		{-time.Second, "1"},
	} {
		limiter := &stubLimiter{decision: ratelimit.Decision{RetryAfter: tc.wait}}
		if got := servedThrough(limiter, fixedKey).Header().Get("Retry-After"); got != tc.want {
			t.Errorf("a wait of %s renders as %q, want %q", tc.wait, got, tc.want)
		}
	}
}

// TestAnUnavailableLimiterRefusesWith503 is Docs/12 §4's fail-closed rule.
//
// A limiter that fails open is one an attacker disables by making Redis unreachable. 503 rather
// than 429 because "this is temporarily unavailable" is true and "you have done too much" is not.
func TestAnUnavailableLimiterRefusesWith503(t *testing.T) {
	limiter := &stubLimiter{err: ratelimit.ErrUnavailable}

	rec := servedThrough(limiter, fixedKey)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("answered %d with an unreachable limiter, want 503. A 2xx here means the "+
			"route is unlimited whenever the cache is down, which is when an attacker would "+
			"arrange for it to be", rec.Code)
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding %s: %v", rec.Body.String(), err)
	}
	if body.Error.Code != string(CodeUnavailable) {
		t.Errorf("code is %q, want %q", body.Error.Code, CodeUnavailable)
	}
}

// TestAnUnexpectedLimiterErrorStillRefuses covers the errors that are this package or its caller
// being wrong — an empty key, a bucket describing no limit.
//
// It is a 500 rather than a 503 because nothing recovers on its own, and it still refuses: a limit
// that could not be applied is not a limit that found room.
func TestAnUnexpectedLimiterErrorStillRefuses(t *testing.T) {
	limiter := &stubLimiter{err: errors.New("something nobody considered")}

	rec := servedThrough(limiter, fixedKey)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("answered %d, want 500", rec.Code)
	}
	if rec.Code < 400 {
		t.Error("an error the limiter could not classify let the request through")
	}
}

// TestAnEmptyKeyIsRefusedRatherThanShared: a key function that returns nothing would put every
// caller in one bucket, so the limiter reports it and the request is refused.
func TestAnEmptyKeyIsRefusedRatherThanShared(t *testing.T) {
	limiter, err := ratelimit.New(nil, "rl:test:", nil)
	if err == nil {
		t.Fatal("a limiter with no clock was built")
	}
	_ = limiter

	memory := ratelimit.NewMemory(fixedClock{})
	rec := servedThrough(memory, func(*http.Request) string { return "" })

	if rec.Code < 400 {
		t.Errorf("an empty key answered %d; every caller would share one bucket", rec.Code)
	}
}

// TestLimitRefusesToBeWiredWrongly turns three wiring mistakes into startup panics.
//
// Each of them would otherwise produce a route that looks limited on the manifest and is not,
// which is the one failure a rate limit cannot report on its own.
func TestLimitRefusesToBeWiredWrongly(t *testing.T) {
	for _, tc := range []struct {
		name   string
		build  func()
		expect string
	}{
		{"no limiter", func() { Limit(nil, testBucket, fixedKey) }, "unlimited"},
		{"no key function", func() { Limit(&stubLimiter{}, testBucket, nil) }, "one bucket"},
		{"a bucket that is not a limit", func() {
			Limit(&stubLimiter{}, ratelimit.Bucket{}, fixedKey)
		}, "limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("wiring Limit with %s was accepted", tc.name)
				}
			}()
			tc.build()
		})
	}
}

// fixedClock is enough clock for a limiter that is never expected to refill.
type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Unix(0, 0).UTC() }
