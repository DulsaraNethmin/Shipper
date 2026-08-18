package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
)

// The memory limiter, and the property that makes it worth having (SHIP-183a).
//
// # Why parity is the test that matters
//
// [MemoryLimiter] exists so that cmd/api can exercise 74 limited routes without a Redis container.
// The risk that creates is precise: if its arithmetic drifts from bucketScript's, every test
// written against it is testing the fake, and it will drift silently because nothing else compares
// them. TestTheTwoLimitersAgree runs one sequence through both and requires the same answers.

func newMemory(t *testing.T) (*MemoryLimiter, *clock.Fixed) {
	t.Helper()
	clk := clock.NewFixed(time.Now().UTC())
	return NewMemory(clk), clk
}

// limiterUnderTest is the surface the two implementations share, so a property that must hold on
// both can be written once rather than twice with one of them quietly drifting.
type limiterUnderTest interface {
	Spend(ctx context.Context, key string, b Bucket) (Decision, error)
	Clear(ctx context.Context, key string) error
}

// TestTheTwoLimitersAgree is the parity check the memory implementation is allowed to exist under.
//
// The sequence deliberately covers what the two could disagree about: the first call on an empty
// key, spending to exactly empty, being refused, a partial refill that a whole-token
// implementation would round away, and the refill back to full that deletes the key.
func TestTheTwoLimitersAgree(t *testing.T) {
	redis, redisClock := newLimiter(t)
	memory, memoryClock := newMemory(t)
	memoryClock.Instant = redisClock.Now()

	ctx := t.Context()
	bucket := Bucket{Capacity: 3, Interval: time.Minute}

	step := func(label string, advance time.Duration) {
		t.Helper()

		if advance > 0 {
			redisClock.Advance(advance)
			memoryClock.Advance(advance)
		}

		fromRedis, err := redis.Spend(ctx, "parity", bucket)
		if err != nil {
			t.Fatalf("%s: redis: %v", label, err)
		}
		fromMemory, err := memory.Spend(ctx, "parity", bucket)
		if err != nil {
			t.Fatalf("%s: memory: %v", label, err)
		}

		if fromRedis != fromMemory {
			t.Errorf("%s: redis said %+v and memory said %+v. The memory limiter is what "+
				"cmd/api's route tests run against, so a divergence here means those tests "+
				"are asserting against a fake rather than against the limiter that ships",
				label, fromRedis, fromMemory)
		}
	}

	step("first call, full bucket", 0)
	step("second", 0)
	step("third, spends the last token", 0)
	step("fourth, refused", 0)
	step("still refused, half an interval later", 30*time.Second)
	step("one token back", 30*time.Second)
	step("refused again", 0)
	step("refilled to full", 3*time.Minute)
}

// TestClearReturnsTheAllowance is Docs/12 §6's mechanism, on both implementations.
func TestClearReturnsTheAllowance(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(t *testing.T) limiterUnderTest
	}{
		{"redis", func(t *testing.T) limiterUnderTest { l, _ := newLimiter(t); return l }},
		{"memory", func(t *testing.T) limiterUnderTest { l, _ := newMemory(t); return l }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limiter := tc.build(t)
			ctx := t.Context()
			bucket := Bucket{Capacity: 2, Interval: time.Minute}

			for range bucket.Capacity {
				limiter.Spend(ctx, "clear-me", bucket)
			}
			if decision, _ := limiter.Spend(ctx, "clear-me", bucket); decision.Allowed {
				t.Fatal("the bucket is not empty; the rest of this test asserts nothing")
			}

			if err := limiter.Clear(ctx, "clear-me"); err != nil {
				t.Fatalf("clearing: %v", err)
			}

			decision, err := limiter.Spend(ctx, "clear-me", bucket)
			if err != nil {
				t.Fatalf("spending after a clear: %v", err)
			}
			if !decision.Allowed {
				t.Error("a cleared bucket still refused. Docs/12 §6 gives a successful sign-in " +
					"its allowance back, and without this the lockout somebody else triggered " +
					"lasts capacity × interval rather than interval")
			}
			if decision.Remaining != bucket.Capacity-1 {
				t.Errorf("%d tokens left after clearing and spending one, want %d — a clear "+
					"returns the whole allowance, not one token",
					decision.Remaining, bucket.Capacity-1)
			}
		})
	}
}

// TestClearingAnUntouchedKeyIsNotAnError: a bucket at capacity is deleted by both implementations,
// so "absent" and "full" are the same state and clearing either must be safe.
func TestClearingAnUntouchedKeyIsNotAnError(t *testing.T) {
	memory, _ := newMemory(t)
	if err := memory.Clear(t.Context(), "never-spent"); err != nil {
		t.Errorf("clearing an untouched key: %v", err)
	}

	redis, _ := newLimiter(t)
	if err := redis.Clear(t.Context(), "never-spent"); err != nil {
		t.Errorf("clearing an untouched key: %v", err)
	}
}

// TestClearNeedsAKey refuses the call that would otherwise clear the prefix itself.
func TestClearNeedsAKey(t *testing.T) {
	memory, _ := newMemory(t)
	if err := memory.Clear(t.Context(), ""); err == nil {
		t.Error("clearing an empty key was accepted")
	}

	redis, _ := newLimiter(t)
	if err := redis.Clear(t.Context(), ""); err == nil {
		t.Error("clearing an empty key was accepted")
	}
}

// TestClearWithoutRedisFailsClosed keeps Clear on the same footing as every other method.
//
// It fails rather than reporting success, so that a caller logging the failure says something true.
// Nothing depends on a clear succeeding — the allowance refills on its own — which is why identity
// logs it rather than failing the sign-in.
func TestClearWithoutRedisFailsClosed(t *testing.T) {
	limiter, err := New(nil, "rl:test:", clock.NewFixed(time.Now()))
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	if err := limiter.Clear(t.Context(), "somewhere"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Clear reported %v, want ErrUnavailable", err)
	}
}

// TestTheMemoryLimiterFailsWhenToldTo is what cmd/api's fail-closed test drives.
func TestTheMemoryLimiterFailsWhenToldTo(t *testing.T) {
	memory, _ := newMemory(t)
	memory.FailWith = ErrUnavailable

	if _, err := memory.Spend(t.Context(), "k", Bucket{Capacity: 1, Interval: time.Second}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Spend reported %v, want ErrUnavailable", err)
	}
	if _, err := memory.Allow(t.Context(), "k", Bucket{Capacity: 1, Interval: time.Second}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Allow reported %v, want ErrUnavailable", err)
	}
	if err := memory.Clear(t.Context(), "k"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Clear reported %v, want ErrUnavailable", err)
	}
}

// TestTheMemoryLimiterKeepsOnlySpentKeys mirrors the Redis script's TTL behaviour, which is what
// cmd/api asserts against when it checks that a route spent nothing.
func TestTheMemoryLimiterKeepsOnlySpentKeys(t *testing.T) {
	memory, clk := newMemory(t)
	ctx := t.Context()
	bucket := Bucket{Capacity: 2, Interval: time.Minute}

	if keys := memory.Keys(); len(keys) != 0 {
		t.Fatalf("a fresh limiter holds %v", keys)
	}

	memory.Spend(ctx, "spent", bucket)
	if keys := memory.Keys(); len(keys) != 1 || keys[0] != "spent" {
		t.Errorf("after one spend the keys are %v, want [spent]", keys)
	}

	// Allow charges nothing, so a key only looked at leaves no state — which is what makes
	// Keys() a report of what was spent rather than of what was consulted.
	memory.Allow(ctx, "consulted", bucket)
	if keys := memory.Keys(); len(keys) != 1 {
		t.Errorf("Allow left state behind: %v", keys)
	}

	clk.Advance(2 * time.Minute)
	memory.Allow(ctx, "spent", bucket)
	if keys := memory.Keys(); len(keys) != 0 {
		t.Errorf("a bucket back at capacity is still held as %v; full and absent are the same "+
			"state in both implementations", keys)
	}
}
