package idempotency

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// newTestStore returns a store against the local Redis, skipping the test when there is
// none.
//
// Skipping rather than failing keeps `make test` green on a clone with no stack running,
// while `make up && make test` exercises the real thing. The Lua script, the TTLs, and the
// atomicity of the claim are all properties of Redis rather than of this code, so testing
// them against a fake would prove nothing about the behaviour that matters.
func newTestStore(t *testing.T, ttl, inFlightTTL time.Duration) (*RedisStore, string) {
	t.Helper()

	url := os.Getenv("REDIS_URL")
	if url == "" {
		url = "redis://localhost:6379/0"
	}

	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("REDIS_URL is not a Redis URL: %v", err)
	}
	// No retries and a short dial timeout: a test either has Redis or does not, and
	// waiting out the client's backoff before skipping makes `make test` on a clone with
	// no stack running take longer than the tests it is skipping.
	opts.MaxRetries = -1
	opts.DialTimeout = time.Second

	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Skipf("no Redis at %s (%v) — run `make up`", url, err)
	}
	t.Cleanup(func() { _ = client.Close() })

	// A prefix of its own per test, so a run never disturbs another test's keys or a
	// developer's local data.
	var b [8]byte
	rand.Read(b[:])
	prefix := "test:idem:" + hex.EncodeToString(b[:]) + ":"

	t.Cleanup(func() {
		keys, err := client.Keys(context.Background(), prefix+"*").Result()
		if err == nil && len(keys) > 0 {
			_ = client.Del(context.Background(), keys...).Err()
		}
	})

	return NewRedisStore(client, ttl, inFlightTTL), prefix
}

func TestBeginClaimsOnlyOnce(t *testing.T) {
	store, prefix := newTestStore(t, time.Minute, time.Minute)
	ctx := t.Context()
	key := prefix + "claim"

	entry, claimed, err := store.Begin(ctx, key, "fingerprint-a")
	if err != nil {
		t.Fatalf("first Begin: %v", err)
	}
	if !claimed {
		t.Fatal("the first Begin did not claim the key")
	}
	if entry != nil {
		t.Errorf("entry = %+v, want nil when the key was claimed", entry)
	}

	entry, claimed, err = store.Begin(ctx, key, "fingerprint-a")
	if err != nil {
		t.Fatalf("second Begin: %v", err)
	}
	if claimed {
		t.Fatal("the key was claimed twice")
	}
	if entry == nil {
		t.Fatal("no entry returned for a held key")
	}
	if entry.Fingerprint != "fingerprint-a" {
		t.Errorf("fingerprint = %q, want the claiming request's", entry.Fingerprint)
	}
	if entry.Response != nil {
		t.Errorf("response = %+v, want nil while the original is in flight", entry.Response)
	}
}

// The claim has to be atomic. Two retries of the same request arriving together is the
// case this whole mechanism exists for, not an edge case.
func TestConcurrentBeginClaimsOnce(t *testing.T) {
	store, prefix := newTestStore(t, time.Minute, time.Minute)
	key := prefix + "concurrent"

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		claims  int
		failure error
	)

	for range 25 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, claimed, err := store.Begin(context.Background(), key, "fingerprint-a")

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failure = err
				return
			}
			if claimed {
				claims++
			}
		}()
	}
	wg.Wait()

	if failure != nil {
		t.Fatalf("Begin: %v", failure)
	}
	if claims != 1 {
		t.Errorf("%d callers claimed the key, want exactly 1", claims)
	}
}

func TestCompleteMakesTheResponseReplayable(t *testing.T) {
	store, prefix := newTestStore(t, time.Minute, time.Minute)
	ctx := t.Context()
	key := prefix + "complete"

	if _, _, err := store.Begin(ctx, key, "fingerprint-a"); err != nil {
		t.Fatal(err)
	}

	want := Response{
		Status:      http.StatusCreated,
		ContentType: "application/json; charset=utf-8",
		Location:    "/v1/jobs/job_123/bids/bid_9",
		Body:        []byte(`{"bid_id":"bid_9"}`),
	}
	if err := store.Complete(ctx, key, "fingerprint-a", want); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	entry, claimed, err := store.Begin(ctx, key, "fingerprint-a")
	if err != nil {
		t.Fatal(err)
	}
	if claimed {
		t.Fatal("a completed key was claimed again")
	}
	if entry.Response == nil {
		t.Fatal("no response stored")
	}

	got := *entry.Response
	if got.Status != want.Status {
		t.Errorf("status = %d, want %d", got.Status, want.Status)
	}
	if got.ContentType != want.ContentType {
		t.Errorf("content type = %q, want %q", got.ContentType, want.ContentType)
	}
	if got.Location != want.Location {
		t.Errorf("location = %q, want %q", got.Location, want.Location)
	}
	if string(got.Body) != string(want.Body) {
		t.Errorf("body = %q, want %q", got.Body, want.Body)
	}
}

func TestReleaseFreesTheKey(t *testing.T) {
	store, prefix := newTestStore(t, time.Minute, time.Minute)
	ctx := t.Context()
	key := prefix + "release"

	if _, _, err := store.Begin(ctx, key, "fingerprint-a"); err != nil {
		t.Fatal(err)
	}
	if err := store.Release(ctx, key); err != nil {
		t.Fatalf("Release: %v", err)
	}

	if _, claimed, err := store.Begin(ctx, key, "fingerprint-a"); err != nil || !claimed {
		t.Errorf("after Release, Begin claimed=%v err=%v; want claimed with no error", claimed, err)
	}
}

// If the process handling the original request dies, the claim has to lapse. Otherwise
// the client's retries are refused until the full replay TTL expires — a day, for a
// failure that lasted a second.
func TestAnAbandonedClaimLapses(t *testing.T) {
	store, prefix := newTestStore(t, time.Minute, 150*time.Millisecond)
	ctx := t.Context()
	key := prefix + "abandoned"

	if _, claimed, err := store.Begin(ctx, key, "fingerprint-a"); err != nil || !claimed {
		t.Fatalf("Begin claimed=%v err=%v", claimed, err)
	}

	if _, claimed, _ := store.Begin(ctx, key, "fingerprint-a"); claimed {
		t.Fatal("the claim was not held at all")
	}

	time.Sleep(250 * time.Millisecond)

	if _, claimed, err := store.Begin(ctx, key, "fingerprint-a"); err != nil || !claimed {
		t.Errorf("after the in-flight TTL, Begin claimed=%v err=%v; want the claim to have lapsed",
			claimed, err)
	}
}

// A stored response is a cache, not a record. It has to expire on its own.
func TestACompletedResponseExpires(t *testing.T) {
	store, prefix := newTestStore(t, 200*time.Millisecond, time.Minute)
	ctx := t.Context()
	key := prefix + "expiring"

	if _, _, err := store.Begin(ctx, key, "fingerprint-a"); err != nil {
		t.Fatal(err)
	}
	if err := store.Complete(ctx, key, "fingerprint-a", Response{Status: http.StatusOK}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(300 * time.Millisecond)

	if _, claimed, err := store.Begin(ctx, key, "fingerprint-a"); err != nil || !claimed {
		t.Errorf("after the TTL, Begin claimed=%v err=%v; want the entry to have expired",
			claimed, err)
	}
}

// The fingerprint is what tells a retry apart from a different request reusing the key,
// so it has to survive the round trip intact.
func TestTheFingerprintSurvivesTheRoundTrip(t *testing.T) {
	store, prefix := newTestStore(t, time.Minute, time.Minute)
	ctx := t.Context()
	key := prefix + "fingerprint"

	if _, _, err := store.Begin(ctx, key, "fingerprint-a"); err != nil {
		t.Fatal(err)
	}

	entry, _, err := store.Begin(ctx, key, "fingerprint-b")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Fingerprint != "fingerprint-a" {
		t.Errorf("fingerprint = %q, want the claiming request's, not the enquiring one's",
			entry.Fingerprint)
	}
}
