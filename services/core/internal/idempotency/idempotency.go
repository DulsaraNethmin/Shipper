// Package idempotency stores the outcome of a state-changing request so that repeating
// it returns the original answer instead of doing the work twice (SHIP-15).
//
// # Why this exists
//
// A driver records a pickup in a car park with one bar of signal. The request reaches the
// server, the response does not reach the phone, and the app retries — correctly, because
// from the device's side an unanswered request and a lost request look identical
// (Docs/01 §5.2, Docs/02 §3.1). Without something in the middle, that retry creates a second
// milestone. The same shape of failure duplicates a bid, or a proof record.
//
// The client sends a key it generated; the platform remembers what it answered the first
// time and replays it. Docs/01 §5.2 and Docs/02 §3.1 both state the rule as "every
// state-changing request carries a client-generated idempotency key", which is why
// the middleware requires one rather than treating it as an optimisation.
//
// # Why Redis
//
// Docs/06 §2.1 gives Redis exactly this job. The rule from §4 still holds — Redis must
// never be the only copy of a job, bid, or status — and nothing here breaks it: what is
// stored is a cached HTTP response, and losing the lot means retries execute again rather
// than replaying. That is safe because the domain constraints are the real guarantee: the
// partial unique index in SHIP-91 is what makes a second award impossible, not this cache.
//
// This package holds the record types and the Redis implementation. The interface that
// consumes them is declared by the HTTP middleware that needs it, not here.
package idempotency

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Entry is what is known about one idempotency key.
type Entry struct {
	// Fingerprint identifies the request that claimed this key. A second request
	// presenting the same key with a different fingerprint is a client defect — usually a
	// key generated once and reused across a loop — and is refused rather than answered
	// with somebody else's response.
	Fingerprint string `json:"fingerprint"`

	// Response is nil while the original request is still being handled.
	Response *Response `json:"response,omitempty"`
}

// Response is a completed response, held for replay.
//
// Only the headers a client needs in order to make sense of the body are kept.
// Everything else is either regenerated per request (the request ID, so a replay is still
// traceable to the call that asked for it) or specific to the connection.
type Response struct {
	Status      int    `json:"status"`
	ContentType string `json:"content_type,omitempty"`
	Location    string `json:"location,omitempty"`
	Body        []byte `json:"body,omitempty"`
}

// ErrRaced is returned when a key was neither claimed nor readable, which can only
// happen if it expired between the two halves of the same operation. The caller should
// treat it as a failure to establish idempotency, not as permission to proceed.
var ErrRaced = errors.New("idempotency: the key changed hands during the claim")

// RedisStore keeps entries in Redis.
type RedisStore struct {
	client redis.UniversalClient

	// ttl is how long a completed response stays replayable.
	ttl time.Duration

	// inFlightTTL bounds how long a claim survives without a response written against
	// it. It is the answer to "the process handling the original request died": without
	// it the key would be held until ttl expired, and the client's retries would be
	// refused for a day.
	inFlightTTL time.Duration
}

// NewRedisStore returns a store over client.
func NewRedisStore(client redis.UniversalClient, ttl, inFlightTTL time.Duration) *RedisStore {
	return &RedisStore{client: client, ttl: ttl, inFlightTTL: inFlightTTL}
}

// claim atomically reads the entry at KEYS[1] or, if there is none, writes ARGV[1] with a
// TTL of ARGV[2] milliseconds.
//
// A script rather than SETNX followed by GET: those are two round trips with a window
// between them, and the window is exactly the case this is for — two retries of the same
// request arriving together.
var claim = redis.NewScript(`
	local existing = redis.call('GET', KEYS[1])
	if existing then
		return {0, existing}
	end
	redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
	return {1, ''}
`)

// Begin claims key for a request with the given fingerprint.
//
// It returns claimed=true when the caller now owns the key and must go on to call
// [RedisStore.Complete] or [RedisStore.Release]. Otherwise the returned entry describes
// what is already there: a response to replay, or a claim still in flight.
func (s *RedisStore) Begin(ctx context.Context, key, fingerprint string) (*Entry, bool, error) {
	pending, err := json.Marshal(Entry{Fingerprint: fingerprint})
	if err != nil {
		return nil, false, fmt.Errorf("idempotency: encoding the claim: %w", err)
	}

	result, err := claim.Run(ctx, s.client, []string{key},
		pending, s.inFlightTTL.Milliseconds()).Slice()
	if err != nil {
		return nil, false, fmt.Errorf("idempotency: claiming %s: %w", key, err)
	}
	if len(result) != 2 {
		return nil, false, fmt.Errorf("idempotency: claiming %s: unexpected reply %v", key, result)
	}

	if claimed, _ := result[0].(int64); claimed == 1 {
		return nil, true, nil
	}

	raw, ok := result[1].(string)
	if !ok || raw == "" {
		return nil, false, ErrRaced
	}

	var entry Entry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		// A stored entry that cannot be read is worse than no entry: it would refuse the
		// client's retries until it expired. Treat it as a failure to establish
		// idempotency so the caller fails closed and someone sees it in the log.
		return nil, false, fmt.Errorf("idempotency: decoding the entry at %s: %w", key, err)
	}
	return &entry, false, nil
}

// Complete records the response against a key this caller claimed, making it replayable
// for the store's TTL.
func (s *RedisStore) Complete(ctx context.Context, key, fingerprint string, resp Response) error {
	value, err := json.Marshal(Entry{Fingerprint: fingerprint, Response: &resp})
	if err != nil {
		return fmt.Errorf("idempotency: encoding the response for %s: %w", key, err)
	}
	if err := s.client.Set(ctx, key, value, s.ttl).Err(); err != nil {
		return fmt.Errorf("idempotency: storing the response for %s: %w", key, err)
	}
	return nil
}

// Release gives up a claim without recording a response, so the client's next attempt
// executes rather than being refused.
//
// This is what happens after a 5xx or a panic: those say "this may work if you try
// again", and holding the key would turn a transient failure into one the client cannot
// retry past.
func (s *RedisStore) Release(ctx context.Context, key string) error {
	if err := s.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("idempotency: releasing %s: %w", key, err)
	}
	return nil
}
