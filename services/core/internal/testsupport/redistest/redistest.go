// Package redistest gives a test an isolated corner of Redis.
//
// Redis has no equivalent of cloning a database, so isolation is by namespace rather than by
// instance: a per-run key prefix, cleaned up when the test ends.
//
// The important behaviour here is what happens when Redis is *absent*. See Unavailable.
package redistest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Client returns a Redis client and a key prefix unique to this test.
//
// Every key the test writes must carry the prefix. Keys are removed when the test ends, so a
// failing test does not poison the next run.
func Client(t *testing.T) (*redis.Client, string) {
	t.Helper()

	opts, err := redis.ParseURL(url())
	if err != nil {
		t.Fatalf("redistest: REDIS_URL is unusable: %v", err)
	}

	// A test that cannot reach Redis should say so at once rather than retrying into a
	// timeout that reads like a hang.
	opts.MaxRetries = -1
	opts.DialTimeout = time.Second

	client := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		Unavailable(t, err)
		return nil, ""
	}

	prefix := "test:" + nonce(t) + ":"

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()

		// SCAN rather than KEYS: KEYS blocks the server for the length of the scan, and
		// a test suite that briefly stalls a shared Redis is a bad neighbour even when
		// the only neighbour is another test.
		iter := client.Scan(cleanupCtx, 0, prefix+"*", 100).Iterator()
		var keys []string
		for iter.Next(cleanupCtx) {
			keys = append(keys, iter.Val())
		}
		if len(keys) > 0 {
			if err := client.Del(cleanupCtx, keys...).Err(); err != nil {
				t.Logf("redistest: could not remove %d keys under %s: %v", len(keys), prefix, err)
			}
		}
		_ = client.Close()
	})

	return client, prefix
}

// Unavailable decides what an unreachable Redis means for a test.
//
// Skipping is reserved for `-short`. Everything else fails.
//
// This is a deliberate change from how the idempotency tests originally behaved: they skipped
// themselves whenever Redis was absent, and .github/workflows/README.md already identifies the
// consequence — "they pass by being skipped, which reads as green". Seven tests covering the
// atomic claim, the in-flight lapse and the replay window would silently not run, and CI would
// report success. A test that quietly does not run is worse than one that does not exist,
// because it is counted.
//
// Exported so that packages with their own client construction can share the decision rather
// than each making it again.
func Unavailable(t *testing.T, err error) {
	t.Helper()

	if testing.Short() {
		t.Skipf("redistest: no Redis at %s and -short was given (%v)", url(), err)
		return
	}
	t.Fatalf("redistest: no Redis at %s (%v)\n"+
		"run `make up`, or pass -short to skip tests that need infrastructure", url(), err)
}

// url is where Redis is expected. TEST_REDIS_URL first, so a worktree can point at its own
// instance or its own logical database with a single variable.
func url() string {
	if u := os.Getenv("TEST_REDIS_URL"); u != "" {
		return u
	}
	if u := os.Getenv("REDIS_URL"); u != "" {
		return u
	}
	return "redis://localhost:6379/0"
}

// nonce is the random component of a key prefix.
func nonce(t *testing.T) string {
	t.Helper()

	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("redistest: generating a key prefix: %v", err)
	}
	return hex.EncodeToString(b[:])
}
