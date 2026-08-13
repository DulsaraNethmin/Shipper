package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/redistest"
)

// Against the real Redis, per Docs/10 §7.1 and §7.2.
//
// Every property here is a property of the Lua script and of Redis running it to completion — the
// atomic refill-and-charge, the key's TTL, the arithmetic on fractional tokens. A fake would
// exercise the test's idea of Redis rather than Redis, and would report the one defect that
// matters — two callers interleaving between a read and a write — as passing.

// newLimiter returns a limiter with a key prefix of its own, and a clock the test drives.
//
// A fixed clock rather than the system one: refill is arithmetic on elapsed time, and a test that
// sleeps to observe it is a test that is slow when it passes and flaky when the machine is busy.
func newLimiter(t *testing.T) (*Limiter, *clock.Fixed) {
	t.Helper()

	client, prefix := redistest.Client(t)
	clk := clock.NewFixed(time.Now().UTC())

	limiter, err := New(client, prefix, clk)
	if err != nil {
		t.Fatalf("building the limiter: %v", err)
	}
	return limiter, clk
}

var testBucket = Bucket{Capacity: 3, Interval: time.Minute}

// TestABucketAllowsItsCapacityAndThenRefuses.
func TestABucketAllowsItsCapacityAndThenRefuses(t *testing.T) {
	limiter, _ := newLimiter(t)
	ctx := t.Context()

	for i := range testBucket.Capacity {
		decision, err := limiter.Spend(ctx, "account", testBucket)
		if err != nil {
			t.Fatalf("spend %d: %v", i, err)
		}
		if !decision.Allowed {
			t.Fatalf("spend %d was refused with %d units still to spend", i, testBucket.Capacity-i)
		}
	}

	decision, err := limiter.Allow(ctx, "account", testBucket)
	if err != nil {
		t.Fatalf("allow: %v", err)
	}
	if decision.Allowed {
		t.Error("the bucket allowed a fourth attempt with a capacity of three")
	}
	if decision.RetryAfter <= 0 || decision.RetryAfter > testBucket.Interval {
		t.Errorf("RetryAfter = %s, want (0, %s] — a client is told when to come back",
			decision.RetryAfter, testBucket.Interval)
	}
}

// TestAllowSpendsNothing is what lets the caller charge only failures.
//
// Checking the bucket on the way in and charging on the way out is the whole of "repeated
// *failures* are throttled": if the check spent a unit, an honest client would be throttled for
// signing in successfully.
func TestAllowSpendsNothing(t *testing.T) {
	limiter, _ := newLimiter(t)
	ctx := t.Context()

	for range testBucket.Capacity * 5 {
		decision, err := limiter.Allow(ctx, "account", testBucket)
		if err != nil {
			t.Fatalf("allow: %v", err)
		}
		if !decision.Allowed {
			t.Fatal("checking the bucket emptied it, so a client is throttled for succeeding")
		}
	}
}

// TestTheBucketRefillsContinuously. A fixed window would allow nothing until it rolled over and
// then allow everything; this returns one unit per interval, which is what makes the sustained
// rate a rate.
func TestTheBucketRefillsContinuously(t *testing.T) {
	limiter, clk := newLimiter(t)
	ctx := t.Context()

	for range testBucket.Capacity {
		if _, err := limiter.Spend(ctx, "account", testBucket); err != nil {
			t.Fatalf("spend: %v", err)
		}
	}

	t.Run("half an interval is not enough", func(t *testing.T) {
		clk.Advance(testBucket.Interval / 2)

		decision, err := limiter.Allow(ctx, "account", testBucket)
		if err != nil {
			t.Fatalf("allow: %v", err)
		}
		if decision.Allowed {
			t.Error("half an interval returned a whole unit")
		}
	})

	t.Run("a whole interval returns exactly one", func(t *testing.T) {
		clk.Advance(testBucket.Interval / 2)

		if _, err := limiter.Spend(ctx, "account", testBucket); err != nil {
			t.Fatalf("spend: %v", err)
		}
		decision, err := limiter.Allow(ctx, "account", testBucket)
		if err != nil {
			t.Fatalf("allow: %v", err)
		}
		if decision.Allowed {
			t.Error("one interval returned more than one unit")
		}
	})

	t.Run("and it never fills past capacity", func(t *testing.T) {
		clk.Advance(testBucket.Interval * 100)

		for i := range testBucket.Capacity {
			if _, err := limiter.Spend(ctx, "account", testBucket); err != nil {
				t.Fatalf("spend %d: %v", i, err)
			}
		}
		decision, err := limiter.Allow(ctx, "account", testBucket)
		if err != nil {
			t.Fatalf("allow: %v", err)
		}
		if decision.Allowed {
			t.Errorf("an idle bucket refilled past its capacity of %d", testBucket.Capacity)
		}
	})
}

// TestKeysAreIndependent. Per account *and* per address means two buckets, and one filling up must
// not refuse the other — otherwise one determined caller locks out everybody who shares the
// limiter.
func TestKeysAreIndependent(t *testing.T) {
	limiter, _ := newLimiter(t)
	ctx := t.Context()

	for range testBucket.Capacity {
		if _, err := limiter.Spend(ctx, "account:one", testBucket); err != nil {
			t.Fatalf("spend: %v", err)
		}
	}

	decision, err := limiter.Allow(ctx, "account:two", testBucket)
	if err != nil {
		t.Fatalf("allow: %v", err)
	}
	if !decision.Allowed {
		t.Error("emptying one key's bucket refused another key")
	}
}

// TestSpendingWhileRefusedDoesNotDeepenTheHole. The limit is a rate: somebody who kept trying
// while being refused must not have to wait longer than somebody who stopped, or a client with a
// retry loop locks its user out for as long as the loop runs.
func TestSpendingWhileRefusedDoesNotDeepenTheHole(t *testing.T) {
	limiter, clk := newLimiter(t)
	ctx := t.Context()

	for range testBucket.Capacity + 20 {
		if _, err := limiter.Spend(ctx, "account", testBucket); err != nil {
			t.Fatalf("spend: %v", err)
		}
	}

	clk.Advance(testBucket.Interval)

	decision, err := limiter.Allow(ctx, "account", testBucket)
	if err != nil {
		t.Fatalf("allow: %v", err)
	}
	if !decision.Allowed {
		t.Error("one interval after being refused, twenty extra attempts were still being paid for")
	}
}

// TestAnEmptyBucketExpiresBackToFull. A bucket at capacity says nothing a fresh one does not, so
// it is deleted rather than kept — otherwise every address that ever failed a sign-in is a key
// held forever.
func TestAnEmptyBucketExpiresBackToFull(t *testing.T) {
	client, prefix := redistest.Client(t)
	clk := clock.NewFixed(time.Now().UTC())

	limiter, err := New(client, prefix, clk)
	if err != nil {
		t.Fatalf("building the limiter: %v", err)
	}
	ctx := t.Context()

	if _, err := limiter.Spend(ctx, "account", testBucket); err != nil {
		t.Fatalf("spend: %v", err)
	}

	ttl, err := client.PTTL(ctx, prefix+"account").Result()
	if err != nil {
		t.Fatalf("reading the ttl: %v", err)
	}
	if ttl <= 0 || ttl > testBucket.Interval {
		t.Errorf("ttl = %s, want (0, %s] — the key must not outlive the state it holds",
			ttl, testBucket.Interval)
	}

	// And a bucket back at capacity leaves nothing behind at all.
	clk.Advance(testBucket.Interval * 10)
	if _, err := limiter.Allow(ctx, "account", testBucket); err != nil {
		t.Fatalf("allow: %v", err)
	}
	exists, err := client.Exists(ctx, prefix+"account").Result()
	if err != nil {
		t.Fatalf("checking the key: %v", err)
	}
	if exists != 0 {
		t.Error("a bucket back at capacity is still a key, which is a key per caller kept forever")
	}
}

// TestConcurrentSpendsChargeOnce is the property the Lua script exists for.
//
// A read followed by a write has a window between them, and two attempts arriving together is
// precisely the case a rate limit is for. With the two steps separate, the capacity below would be
// spent many times over.
func TestConcurrentSpendsChargeOnce(t *testing.T) {
	limiter, _ := newLimiter(t)

	const attempts = 25
	bucket := Bucket{Capacity: 5, Interval: time.Hour}

	allowed := make(chan bool, attempts)
	failures := make(chan error, attempts)

	for range attempts {
		go func() {
			decision, err := limiter.Spend(context.Background(), "account", bucket)
			if err != nil {
				failures <- err
				allowed <- false
				return
			}
			allowed <- decision.Allowed
		}()
	}

	permitted := 0
	for range attempts {
		if <-allowed {
			permitted++
		}
	}
	select {
	case err := <-failures:
		t.Fatalf("spend: %v", err)
	default:
	}

	if permitted != bucket.Capacity {
		t.Errorf("%d of %d concurrent attempts were allowed, want exactly the capacity of %d",
			permitted, attempts, bucket.Capacity)
	}
}

// TestWithoutRedisEveryCallIsRefused.
//
// The fail-closed direction, which is the whole security position of this package: a limiter that
// failed open would be one an attacker disables by making Redis unreachable, on the endpoint the
// limiter exists to protect.
func TestWithoutRedisEveryCallIsRefused(t *testing.T) {
	limiter, err := New(nil, "test:", clock.System{})
	if err != nil {
		t.Fatalf("building the limiter: %v", err)
	}

	if _, err := limiter.Allow(t.Context(), "account", testBucket); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Allow: err = %v, want ErrUnavailable — a limiter that fails open is one an "+
			"attacker turns off by taking Redis down", err)
	}
	if _, err := limiter.Spend(t.Context(), "account", testBucket); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Spend: err = %v, want ErrUnavailable", err)
	}
}

// TestABucketThatDescribesNoLimitIsRefused, rather than silently allowing or refusing everything
// depending on which zero it was.
func TestABucketThatDescribesNoLimitIsRefused(t *testing.T) {
	limiter, _ := newLimiter(t)

	for name, bucket := range map[string]Bucket{
		"no capacity": {Capacity: 0, Interval: time.Minute},
		"no interval": {Capacity: 3, Interval: 0},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := limiter.Allow(t.Context(), "account", bucket); err == nil {
				t.Error("a bucket describing no limit was accepted")
			}
		})
	}
}

// TestATypedNilClientIsStillNoClient.
//
// A *redis.Client that is nil, assigned to a redis.UniversalClient, produces an interface that is
// not nil. cmd/api reaches exactly that — Deps.Redis is a typed pointer and is nil whenever the
// cache was unreachable at startup — and without the normalisation in New the first call
// dereferences it and panics mid-request. A panic is a 500, which tells nobody that a limit was
// skipped, so this is the fail-open case dressed as a crash.
func TestATypedNilClientIsStillNoClient(t *testing.T) {
	var client *redis.Client // nil, and about to become a non-nil interface

	limiter, err := New(client, "test:", clock.System{})
	if err != nil {
		t.Fatalf("building the limiter: %v", err)
	}

	if _, err := limiter.Allow(t.Context(), "account", testBucket); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable — a typed nil reached the client and the "+
			"limiter panicked instead of failing closed", err)
	}
}
