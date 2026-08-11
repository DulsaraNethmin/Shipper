// Package ratelimit is the Redis token bucket behind the limited routes (SHIP-47).
//
// It was registered in internal/boundaries ahead of the code, with that exact description, so that
// whoever first needed it could write it without editing a shared file. SHIP-47 is the first
// client and this is that package.
//
// # Why a token bucket rather than a fixed window
//
// A fixed window lets a caller spend the whole allowance in the last instant of one window and the
// whole of the next in the first instant of the following one — twice the intended rate, on
// demand, at a moment of their choosing. A bucket refills continuously, so there is no boundary to
// aim at, and it produces an honest `Retry-After`: the time until the next token, rather than the
// time until an arbitrary window rolls over.
//
// # Why the caller decides what a unit costs
//
// This package counts; it has no opinion about what is being counted. SHIP-47 charges *failed*
// sign-ins and lets successful ones through free, which is what makes the limit a control on
// guessing rather than a cap on how often somebody may sign in. Another caller will want to charge
// every request. Both are [Bucket] and a cost, and neither is a mode this package has to know
// about.
//
// # Redis, and what happens when it is not there
//
// The state is per key and short-lived, which is exactly what Docs/06 §2.1 gives Redis. Losing it
// is safe in the direction that matters: buckets refill to full, so a flush forgives rather than
// locks out.
//
// **Every method fails closed**, and the reason is worth stating because the opposite reading is
// tempting. A rate limiter that fails open is one an attacker can disable by making Redis
// unreachable — which, on the endpoint this exists to protect, hands them exactly the unlimited
// guessing the limiter is there to prevent. The error is reported rather than swallowed, and the
// caller decides the status; on the authentication surface it is 503 rather than 429, because
// "this is temporarily unavailable" is true and "you have done too much" is not.
//
// The cost of that choice is bounded here in a way that is worth naming: httpx.Idempotent already
// wraps the whole /v1 group and already fails closed on the same Redis, so every state-changing
// route is refused before this package is reached. Failing closed here changes nothing about what
// a client sees during a Redis outage; it only means there is one answer to the question rather
// than two.
package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
)

// ErrUnavailable means the bucket could not be read or written.
//
// It is a failure to establish a limit, never permission to proceed. See the package note.
var ErrUnavailable = errors.New("ratelimit: the cache is not available")

// Bucket is how much of something is allowed, and how fast the allowance comes back.
type Bucket struct {
	// Capacity is the burst: how many units may be spent with an empty history.
	Capacity int

	// Interval is how long one unit takes to return. The sustained rate is one unit per
	// Interval, and a bucket that has been empty is full again after Capacity × Interval.
	Interval time.Duration
}

// Valid reports whether the bucket describes a limit at all.
func (b Bucket) Valid() bool { return b.Capacity > 0 && b.Interval > 0 }

// Decision is what the limiter said.
type Decision struct {
	// Allowed is false when the bucket is empty.
	Allowed bool

	// Remaining is whole units left after this call.
	Remaining int

	// RetryAfter is how long until the bucket holds a unit again, and is zero when Allowed.
	// It is the honest figure a Retry-After header wants: time until the next token, not time
	// until a window rolls over.
	RetryAfter time.Duration
}

// Limiter is a token bucket over Redis.
//
// One Limiter serves every bucket: the shape of the limit travels with the call, so a caller with
// two limits — per account and per address, say — needs one of these and two [Bucket] values.
type Limiter struct {
	client redis.UniversalClient
	prefix string
	clock  clock.Clock
}

// New returns a limiter over client.
//
// The client may be nil, which is the state cmd/api is in when Redis was unreachable at startup
// and the state every test that has not asked for Redis is in. It is not an error at construction
// — the process starts deliberately — and every call then reports [ErrUnavailable], which is the
// fail-closed direction the package note argues for.
//
// prefix namespaces the keys. It exists so that tests can share one Redis without sharing buckets,
// and so that a deployment can tell this package's keys from the idempotency store's.
func New(client redis.UniversalClient, prefix string, clk clock.Clock) (*Limiter, error) {
	if clk == nil {
		return nil, errors.New("ratelimit: a limiter needs a clock")
	}
	if isNil(client) {
		client = nil
	}
	return &Limiter{client: client, prefix: prefix, clock: clk}, nil
}

// isNil reports whether client is nil, including the case that a plain `== nil` misses.
//
// A `*redis.Client` that is nil, assigned to a [redis.UniversalClient], produces an interface that
// is **not** nil — it carries a type and a nil pointer — so `client == nil` is false and the first
// method call dereferences it. cmd/api reaches exactly that: `Deps.Redis` is a typed pointer and
// is nil whenever the cache was unreachable at startup or the caller is a test.
//
// It is normalised here, once, rather than guarded at every call site, because the failure it
// produces is a panic in the middle of a request rather than the [ErrUnavailable] this package
// promises — a fail-*open* in the sense that matters, since a 500 tells nobody a limit was
// skipped.
func isNil(client redis.UniversalClient) bool {
	if client == nil {
		return true
	}
	value := reflect.ValueOf(client)
	switch value.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Interface, reflect.Func, reflect.Chan:
		return value.IsNil()
	default:
		return false
	}
}

// Allow reports whether key has an allowance left, spending nothing.
//
// This is what a caller asks *before* doing the work, so that the work — an argon2id derivation, a
// database round trip — is what the limit actually protects. Spending on the way in instead would
// charge honest callers for their successes.
func (l *Limiter) Allow(ctx context.Context, key string, b Bucket) (Decision, error) {
	return l.take(ctx, key, b, 0)
}

// Spend charges one unit against key.
//
// Called after the outcome is known, so the caller decides what counts. A bucket that is already
// empty is left empty rather than going negative: the limit is a rate, and a caller who kept
// trying while refused should not have to wait longer than one who stopped.
func (l *Limiter) Spend(ctx context.Context, key string, b Bucket) (Decision, error) {
	return l.take(ctx, key, b, 1)
}

// bucketScript refills a bucket and, if it is not empty, charges the cost.
//
// One script rather than a read followed by a write, for the reason the idempotency store's claim
// gives: those are two round trips with a window between them, and two attempts arriving together
// is the case a rate limit is for. Redis runs a script to completion, so the refill and the charge
// cannot interleave with another caller's.
//
// The unit is fractional on purpose. Storing whole tokens would round every partial refill away,
// so a bucket refilling at one token a minute, queried every thirty seconds, would never refill at
// all.
var bucketScript = redis.NewScript(`
	local capacity = tonumber(ARGV[1])
	local interval = tonumber(ARGV[2])
	local now      = tonumber(ARGV[3])
	local cost     = tonumber(ARGV[4])

	local state  = redis.call('HMGET', KEYS[1], 'tokens', 'at')
	local tokens = tonumber(state[1])
	local at     = tonumber(state[2])

	if tokens == nil or at == nil then
		tokens = capacity
		at = now
	end

	-- Refill. A clock that went backwards adds nothing rather than draining the bucket.
	if now > at then
		tokens = math.min(capacity, tokens + (now - at) / interval)
	end
	at = now

	local allowed = 0
	local retry = 0
	if tokens >= 1 then
		allowed = 1
		tokens = math.max(0, tokens - cost)
	else
		retry = math.ceil((1 - tokens) * interval)
	end

	-- The key lives exactly as long as it says something. A bucket back at capacity is
	-- indistinguishable from one that never existed, so keeping it would be a key per caller
	-- held for no reason.
	local ttl = math.ceil((capacity - tokens) * interval)
	if ttl > 0 then
		redis.call('HSET', KEYS[1], 'tokens', tokens, 'at', at)
		redis.call('PEXPIRE', KEYS[1], ttl)
	else
		redis.call('DEL', KEYS[1])
	end

	return {allowed, math.floor(tokens), retry}
`)

func (l *Limiter) take(ctx context.Context, key string, b Bucket, cost int) (Decision, error) {
	if !b.Valid() {
		// A bucket with no capacity or no interval refuses everything or allows everything,
		// depending on which zero it is. Both are a programming mistake dressed as a limit.
		return Decision{}, fmt.Errorf("ratelimit: bucket %+v describes no limit", b)
	}
	if key == "" {
		return Decision{}, errors.New("ratelimit: a limit needs a key to count against")
	}
	if l.client == nil {
		return Decision{}, ErrUnavailable
	}

	// The interval is per unit, in milliseconds, which is the resolution the script's clock
	// arithmetic works in. A sub-millisecond interval is not a rate limit.
	interval := math.Max(1, float64(b.Interval.Milliseconds()))

	result, err := bucketScript.Run(ctx, l.client, []string{l.prefix + key},
		b.Capacity, interval, l.clock.Now().UnixMilli(), cost).Slice()
	if err != nil {
		return Decision{}, fmt.Errorf("%w: %s: %w", ErrUnavailable, key, err)
	}
	if len(result) != 3 {
		return Decision{}, fmt.Errorf("%w: %s: unexpected reply %v", ErrUnavailable, key, result)
	}

	allowed, _ := result[0].(int64)
	remaining, _ := result[1].(int64)
	retryMillis, _ := result[2].(int64)

	return Decision{
		Allowed:    allowed == 1,
		Remaining:  int(remaining),
		RetryAfter: time.Duration(retryMillis) * time.Millisecond,
	}, nil
}
