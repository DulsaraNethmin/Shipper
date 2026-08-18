package ratelimit

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
)

// MemoryLimiter is the token bucket in a map, for tests.
//
// It is **not** a deployment option, and the reason is the same one [idempotency.MemoryStore]
// gives: it is per-process, so two instances behind a load balancer would each grant a caller the
// full allowance — which on a rate limit means the limit is whatever it is multiplied by the
// number of instances. [Limiter] is the only implementation the service runs with.
//
// What it is for is testing the behaviour *around* a limiter: that a route refuses at capacity,
// that the refusal carries an honest wait, that an unreachable cache fails closed. Those are
// properties of the middleware and of the wiring, and asserting them against a real Redis would
// make every test that touches a limited endpoint depend on a running container — which is most
// of cmd/api once SHIP-183a keys 74 routes on their caller.
//
// The arithmetic is deliberately the same as bucketScript's, including the fractional token: a
// memory limiter that rounded partial refills away would refill differently from the real one and
// the tests written against it would be testing the fake.
type MemoryLimiter struct {
	mu      sync.Mutex
	clock   clock.Clock
	buckets map[string]memoryBucket

	// FailWith, when set, makes every operation fail with it — the unreachable-Redis case,
	// which every caller has to fail closed on.
	FailWith error
}

// memoryBucket is one key's state: how many tokens are left and when that was true.
type memoryBucket struct {
	tokens float64
	at     time.Time
}

// NewMemory returns an empty limiter reading time from clk.
func NewMemory(clk clock.Clock) *MemoryLimiter {
	if clk == nil {
		panic("ratelimit: a memory limiter needs a clock")
	}
	return &MemoryLimiter{clock: clk, buckets: map[string]memoryBucket{}}
}

// Allow reports whether key has an allowance left, spending nothing.
func (m *MemoryLimiter) Allow(_ context.Context, key string, b Bucket) (Decision, error) {
	return m.take(key, b, 0)
}

// Spend charges one unit against key.
func (m *MemoryLimiter) Spend(_ context.Context, key string, b Bucket) (Decision, error) {
	return m.take(key, b, 1)
}

// Clear discards key's bucket, so the next caller finds a full allowance.
func (m *MemoryLimiter) Clear(_ context.Context, key string) error {
	if key == "" {
		return errNoKey
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.FailWith != nil {
		return m.FailWith
	}
	delete(m.buckets, key)
	return nil
}

// Keys returns every key currently holding state, for assertions about what a limit counted
// against. A key at full capacity is deleted, exactly as the Redis script deletes it, so this
// reports the buckets that have been spent from rather than the ones that have been consulted.
func (m *MemoryLimiter) Keys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return slices.Sorted(maps.Keys(m.buckets))
}

func (m *MemoryLimiter) take(key string, b Bucket, cost float64) (Decision, error) {
	if !b.Valid() {
		return Decision{}, errNoLimit(b)
	}
	if key == "" {
		return Decision{}, errNoKey
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.FailWith != nil {
		return Decision{}, m.FailWith
	}

	now := m.clock.Now()
	state, seen := m.buckets[key]
	if !seen {
		state = memoryBucket{tokens: float64(b.Capacity), at: now}
	}

	// Refill. A clock that went backwards adds nothing rather than draining the bucket.
	if now.After(state.at) {
		state.tokens = min(float64(b.Capacity),
			state.tokens+float64(now.Sub(state.at))/float64(b.Interval))
	}
	state.at = now

	decision := Decision{}
	if state.tokens >= 1 {
		decision.Allowed = true
		state.tokens = max(0, state.tokens-cost)
	} else {
		// Rounded up to the millisecond the script's integer arithmetic works in, so the two
		// implementations report the same wait rather than one that differs by a rounding.
		wait := time.Duration((1 - state.tokens) * float64(b.Interval))
		decision.RetryAfter = wait.Round(time.Millisecond)
		if decision.RetryAfter < wait {
			decision.RetryAfter += time.Millisecond
		}
	}
	decision.Remaining = int(state.tokens)

	// The key lives exactly as long as it says something, so that Keys reports what was spent
	// rather than what was looked at.
	if state.tokens >= float64(b.Capacity) {
		delete(m.buckets, key)
	} else {
		m.buckets[key] = state
	}

	return decision, nil
}
