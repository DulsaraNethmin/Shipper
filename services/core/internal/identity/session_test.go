package identity

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-39 against a real PostgreSQL, per Docs/06 §4.1.
//
// Rotation is a claim about what a *row* says after a transaction, and about what a second
// transaction sees while the first holds a lock. A mocked store would report every one of these
// as passing while proving nothing about either — uq_device_sessions_refresh_token_hash and the
// FOR UPDATE in deviceSessionByRefreshHash are the mechanism, not the code around them.

// testServiceIssuer builds the access token issuer a Service needs, over the same keyset the
// token tests use.
//
// The clock is the service's own, so that a test which moves time forward moves the token
// expiry with it rather than leaving the two disagreeing.
func testServiceIssuer(t *testing.T, clk clock.Clock) *AccessTokenIssuer {
	t.Helper()

	issuer, err := NewAccessTokenIssuer(testKeyset(t, kidOne), 15*time.Minute, clk)
	if err != nil {
		t.Fatalf("building the access token issuer: %v", err)
	}
	return issuer
}

// newSessionService builds a service, an account, and nothing else. The account is registered
// through the real path so that the row the session points at is one the platform created.
func newSessionService(t *testing.T, clk clock.Clock) (*Service, *pgxpool.Pool, User) {
	t.Helper()

	pool := pgtest.DB(t)

	hasher, err := NewPasswordHasher(testProfile)
	if err != nil {
		t.Fatalf("building the hasher: %v", err)
	}

	svc, err := NewService(pool, hasher, testServiceIssuer(t, clk),
		&recordingSender{}, &recordingTexter{}, clk)
	if err != nil {
		t.Fatalf("building the service: %v", err)
	}

	user, err := svc.Register(t.Context(), validRegistration())
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	return svc, pool, user
}

// storedRefreshHash reads what the session row actually holds.
func storedRefreshHash(t *testing.T, pool *pgxpool.Pool, sessionID string) string {
	t.Helper()

	var hash string
	if err := pool.QueryRow(t.Context(),
		`SELECT refresh_token_hash FROM device_sessions WHERE id = $1`, sessionID).Scan(&hash); err != nil {
		t.Fatalf("reading the stored refresh hash: %v", err)
	}
	return hash
}

// TestRefreshRotation is SHIP-39's acceptance criterion, both halves of it: each refresh returns
// a new token, and the one presented stops working.
//
// The second half is the one a plausible implementation gets wrong. Issuing a new token is easy
// to write and easy to see; leaving the old one usable produces no symptom at all until somebody
// with a copy of it uses it, which is exactly the situation rotation exists for.
func TestRefreshRotation(t *testing.T) {
	svc, pool, user := newSessionService(t, clock.System{})

	first, err := svc.startSession(t.Context(), pool, user, "Nethmin's iPhone")
	if err != nil {
		t.Fatalf("starting the session: %v", err)
	}

	second, err := svc.Refresh(t.Context(), first.Refresh.Value)
	if err != nil {
		t.Fatalf("refreshing: %v", err)
	}

	t.Run("a new refresh token is returned", func(t *testing.T) {
		if second.Refresh.Value == "" {
			t.Fatal("the refresh returned no refresh token, so the device cannot refresh again")
		}
		if second.Refresh.Value == first.Refresh.Value {
			t.Error("the same refresh token came back, so nothing rotated")
		}
	})

	t.Run("a new access token is returned, on the same session", func(t *testing.T) {
		if second.Access.Value == "" {
			t.Fatal("the refresh returned no access token, which is what it exists to produce")
		}
		if second.SessionID != first.SessionID {
			t.Errorf("the refresh moved the caller to session %s, was %s — rotating a token "+
				"must not create a device", second.SessionID, first.SessionID)
		}

		claims, err := NewAccessTokenVerifier(testKeyset(t, kidOne), clock.System{})
		if err != nil {
			t.Fatalf("building a verifier: %v", err)
		}
		verified, err := claims.Verify(second.Access.Value)
		if err != nil {
			t.Fatalf("the issued access token does not verify: %v", err)
		}
		if verified.Subject != user.ID.String() {
			t.Errorf("sub = %q, want the account the session belongs to", verified.Subject)
		}
		if verified.SessionID != first.SessionID.String() {
			t.Errorf("sid = %q, want %s — the token must name the device it was issued to",
				verified.SessionID, first.SessionID)
		}
	})

	t.Run("the row holds the new token and not the old one", func(t *testing.T) {
		stored := storedRefreshHash(t, pool, first.SessionID.String())
		if stored != hashRefreshToken(second.Refresh.Value) {
			t.Error("the session does not hold the hash of the token that was just issued")
		}
		if stored == hashRefreshToken(first.Refresh.Value) {
			t.Error("the session still holds the predecessor's hash")
		}
	})

	t.Run("the predecessor no longer works", func(t *testing.T) {
		if _, err := svc.Refresh(t.Context(), first.Refresh.Value); !errors.Is(err, ErrRefreshTokenInvalid) {
			t.Errorf("presenting the rotated token returned %v, want ErrRefreshTokenInvalid — "+
				"a predecessor that still refreshes is a credential rotation did not retire", err)
		}
	})

	t.Run("the successor does", func(t *testing.T) {
		third, err := svc.Refresh(t.Context(), second.Refresh.Value)
		if err != nil {
			t.Fatalf("refreshing with the new token: %v", err)
		}
		if third.Refresh.Value == second.Refresh.Value {
			t.Error("the second rotation returned the token it was given")
		}
	})
}

// TestRefreshTokenIsNeverStoredInPlaintext. The row is a credential, and this table is read by
// every support query, every backup and every replica (Docs/10 §5).
func TestRefreshTokenIsNeverStoredInPlaintext(t *testing.T) {
	svc, pool, user := newSessionService(t, clock.System{})

	pair, err := svc.startSession(t.Context(), pool, user, "Pixel 8")
	if err != nil {
		t.Fatalf("starting the session: %v", err)
	}

	var found int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM device_sessions WHERE refresh_token_hash = $1`,
		pair.Refresh.Value).Scan(&found); err != nil {
		t.Fatalf("searching for the token: %v", err)
	}
	if found != 0 {
		t.Error("the refresh token itself appears in device_sessions")
	}

	stored := storedRefreshHash(t, pool, pair.SessionID.String())
	if stored != hashRefreshToken(pair.Refresh.Value) {
		t.Error("the stored value is not the SHA-256 of the token that was issued")
	}
	if len(stored) != 64 {
		t.Errorf("the stored value is %d characters, want 64 (hex SHA-256)", len(stored))
	}
}

// TestRefreshTokensAreUnpredictable. Two sessions must not be handed the same token, and the
// value must carry real entropy rather than being derived from anything about the account.
func TestRefreshTokensAreUnpredictable(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		raw, hash, err := newRefreshToken()
		if err != nil {
			t.Fatalf("generating: %v", err)
		}
		if len(raw) != 43 {
			t.Fatalf("token is %d characters, want 43 (32 bytes, base64url)", len(raw))
		}
		if hash != hashRefreshToken(raw) {
			t.Fatal("the returned hash is not the hash of the returned token")
		}
		if seen[raw] {
			t.Fatalf("a token repeated within 50 draws: %q", raw)
		}
		seen[raw] = true
	}
}

// TestRefreshWindowSlides is the expiry decision made visible (000103).
//
// Thirty days of inactivity ends a session; a device in daily use never reaches it. So a
// rotation has to move the expiry forward — an expiry fixed at sign-in would be an absolute cap,
// which is a different control and deliberately not the one this column implements.
func TestRefreshWindowSlides(t *testing.T) {
	start := time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)
	clk := clock.NewFixed(start)
	svc, pool, user := newSessionService(t, clk)

	first, err := svc.startSession(t.Context(), pool, user, "iPhone")
	if err != nil {
		t.Fatalf("starting the session: %v", err)
	}
	if want := start.Add(refreshTokenTTL); !first.Refresh.ExpiresAt.Equal(want) {
		t.Errorf("the first token expires %s, want %s", first.Refresh.ExpiresAt, want)
	}

	clk.Advance(20 * 24 * time.Hour)

	second, err := svc.Refresh(t.Context(), first.Refresh.Value)
	if err != nil {
		t.Fatalf("refreshing after twenty days: %v", err)
	}
	if want := clk.Now().Add(refreshTokenTTL); !second.Refresh.ExpiresAt.Equal(want) {
		t.Errorf("the rotated token expires %s, want %s — the window did not slide",
			second.Refresh.ExpiresAt, want)
	}

	// And the row agrees with what the caller was told. A response that promised thirty days
	// over a column that said ten would sign the device out without warning.
	var stored time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT refresh_token_expires_at FROM device_sessions WHERE id = $1`,
		first.SessionID).Scan(&stored); err != nil {
		t.Fatalf("reading the stored expiry: %v", err)
	}
	if !stored.Equal(second.Refresh.ExpiresAt) {
		t.Errorf("the row expires %s and the caller was told %s", stored, second.Refresh.ExpiresAt)
	}
}

// TestAnExpiredRefreshTokenIsRefused. The token is genuine and its thirty days are up, which is
// the whole reason 000103 adds a column rather than deriving a lifetime from a display field.
func TestAnExpiredRefreshTokenIsRefused(t *testing.T) {
	clk := clock.NewFixed(time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC))
	svc, pool, user := newSessionService(t, clk)

	pair, err := svc.startSession(t.Context(), pool, user, "iPhone")
	if err != nil {
		t.Fatalf("starting the session: %v", err)
	}

	clk.Advance(refreshTokenTTL + time.Second)

	if _, err := svc.Refresh(t.Context(), pair.Refresh.Value); !errors.Is(err, ErrRefreshTokenInvalid) {
		t.Errorf("an expired refresh token returned %v, want ErrRefreshTokenInvalid", err)
	}

	// And it did not rotate on the way out. A refusal that still replaced the hash would sign
	// the device out and hand nothing back.
	if got := storedRefreshHash(t, pool, pair.SessionID.String()); got != hashRefreshToken(pair.Refresh.Value) {
		t.Error("a refused refresh rotated the session anyway")
	}
}

// TestRefreshRefusesTokensNobodyIssued, including the empty string — which would otherwise hash
// to a perfectly good digest and go looking for a row.
func TestRefreshRefusesTokensNobodyIssued(t *testing.T) {
	svc, _, _ := newSessionService(t, clock.System{})

	for name, presented := range map[string]string{
		"empty":       "",
		"whitespace":  "   ",
		"made up":     "9qE2vT7bYw1sJk4pNc0aRlX8oZgHdM3uQiV6yB5tCfE",
		"a hash":      hashRefreshToken("anything at all"),
		"a long junk": strings.Repeat("x", 500),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.Refresh(t.Context(), presented); !errors.Is(err, ErrRefreshTokenInvalid) {
				t.Errorf("got %v, want ErrRefreshTokenInvalid", err)
			}
		})
	}
}

// TestRefreshRefusesASuspendedAccount. Docs/10 §5 keeps account standing out of the access token
// precisely so that it is read fresh; this is the path where "fresh" means "at every refresh".
func TestRefreshRefusesASuspendedAccount(t *testing.T) {
	svc, pool, user := newSessionService(t, clock.System{})

	pair, err := svc.startSession(t.Context(), pool, user, "iPhone")
	if err != nil {
		t.Fatalf("starting the session: %v", err)
	}

	if _, err := pool.Exec(t.Context(),
		`UPDATE users SET status = 'suspended' WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("suspending the account: %v", err)
	}

	if _, err := svc.Refresh(t.Context(), pair.Refresh.Value); !errors.Is(err, ErrRefreshTokenInvalid) {
		t.Errorf("a suspended account refreshed its session (%v); the access token lives fifteen "+
			"minutes, so refresh is where a suspension actually takes effect", err)
	}

	// Restricted is not suspended. It narrows what an account may do, and an account that
	// cannot sign in cannot read the message explaining why it is restricted.
	if _, err := pool.Exec(t.Context(),
		`UPDATE users SET status = 'restricted' WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("restricting the account: %v", err)
	}
	if _, err := svc.Refresh(t.Context(), pair.Refresh.Value); err != nil {
		t.Errorf("a restricted account could not refresh: %v", err)
	}
}

// TestARefreshHoldsTheSessionRowUntilItCommits is the FOR UPDATE in
// deviceSessionByRefreshHash, checked deterministically rather than by racing two goroutines
// and hoping they overlap.
//
// A phone on a poor connection fires the same refresh twice. The second reader must wait for
// the first to commit and must then see the rotated row, not the row as it was when it started
// — otherwise two transactions both believe they hold the live token.
//
// Mutation-checked: removing FOR UPDATE makes the second read return the *stale* row
// immediately, and this test fails on the first assertion rather than the second.
func TestARefreshHoldsTheSessionRowUntilItCommits(t *testing.T) {
	svc, pool, user := newSessionService(t, clock.System{})

	pair, err := svc.startSession(t.Context(), pool, user, "iPhone")
	if err != nil {
		t.Fatalf("starting the session: %v", err)
	}
	hash := hashRefreshToken(pair.Refresh.Value)

	first, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("beginning the first transaction: %v", err)
	}
	defer func() { _ = first.Rollback(t.Context()) }()

	locked, err := svc.store.deviceSessionByRefreshHash(t.Context(), first, hash)
	if err != nil {
		t.Fatalf("the first reader could not claim the session: %v", err)
	}

	type read struct {
		session deviceSession
		err     error
	}
	issued := make(chan struct{})
	answered := make(chan read, 1)

	go func() {
		second, err := pool.Begin(t.Context())
		if err != nil {
			answered <- read{err: err}
			return
		}
		defer func() { _ = second.Rollback(t.Context()) }()

		close(issued)
		got, err := svc.store.deviceSessionByRefreshHash(t.Context(), second, hash)
		answered <- read{session: got, err: err}
	}()

	<-issued
	select {
	case got := <-answered:
		t.Fatalf("a second reader was handed the session while the first still held it "+
			"(err=%v). Both transactions now believe they hold the live refresh token, and "+
			"the second rotation overwrites the first — the device is left holding a token "+
			"the row has already replaced.", got.err)
	case <-time.After(250 * time.Millisecond):
		// Blocked, which is the point. The window is generous because the only cost of it
		// being too long is a slower test, and the only cost of it being too short is a
		// failure that has nothing to do with the lock.
	}

	rotated := locked
	rotated.RefreshTokenHash = hashRefreshToken("the token the winner was handed")
	rotated.RefreshTokenExpiresAt = time.Now().UTC().Add(refreshTokenTTL)
	rotated.LastSeenAt = time.Now().UTC()
	if err := svc.store.rotateDeviceSession(t.Context(), first, rotated, locked.RefreshTokenHash); err != nil {
		t.Fatalf("rotating inside the first transaction: %v", err)
	}
	if err := first.Commit(t.Context()); err != nil {
		t.Fatalf("committing the first transaction: %v", err)
	}

	got := <-answered
	if !isNoRows(got.err) {
		t.Errorf("the second reader saw %+v (err=%v) after the rotation committed; it must "+
			"re-evaluate the predicate against the committed row and find nothing",
			got.session, got.err)
	}
}

// TestOnlyOneOfTwoConcurrentRefreshesWins states the outcome the lock exists to produce, end to
// end through Refresh rather than through the store.
//
// It is deliberately not the mutation check — two goroutines may or may not overlap on any
// given run, and a test whose coverage depends on scheduling is one that reports a mistake
// intermittently. TestARefreshHoldsTheSessionRowUntilItCommits is the deterministic one; this
// asserts the invariant that must hold whichever way the two interleave.
func TestOnlyOneOfTwoConcurrentRefreshesWins(t *testing.T) {
	svc, pool, user := newSessionService(t, clock.System{})

	pair, err := svc.startSession(t.Context(), pool, user, "iPhone")
	if err != nil {
		t.Fatalf("starting the session: %v", err)
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		start   = make(chan struct{})
		issued  []string
		refused []error
	)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, err := svc.Refresh(t.Context(), pair.Refresh.Value)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				refused = append(refused, err)
				return
			}
			issued = append(issued, got.Refresh.Value)
		}()
	}
	close(start)
	wg.Wait()

	if len(issued) != 1 {
		t.Fatalf("%d of 2 concurrent refreshes succeeded, want exactly 1 (refusals: %v)",
			len(issued), refused)
	}
	if got := storedRefreshHash(t, pool, pair.SessionID.String()); got != hashRefreshToken(issued[0]) {
		t.Error("the session does not hold the token the winner was handed")
	}
}

// TestStartSessionValidatesTheDeviceLabel. The label is what a person recognises their own phone
// by in the device list (SHIP-46), and ck_device_sessions_device_label is the authority — this
// is only about the caller being told which field was wrong rather than being handed a
// constraint violation.
func TestStartSessionValidatesTheDeviceLabel(t *testing.T) {
	svc, pool, user := newSessionService(t, clock.System{})

	for name, label := range map[string]string{
		"blank":          "",
		"whitespace":     "   ",
		"past the limit": strings.Repeat("x", maxDeviceLabelLength+1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.startSession(t.Context(), pool, user, label)

			var apiErr *httpx.Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("got %v, want a validation error naming the field", err)
			}
			if len(apiErr.Details) != 1 || apiErr.Details[0].Field != "device_label" {
				t.Errorf("details = %+v, want one entry naming device_label", apiErr.Details)
			}
		})
	}

	t.Run("surrounding whitespace is trimmed rather than refused", func(t *testing.T) {
		pair, err := svc.startSession(t.Context(), pool, user, "  Nethmin's iPhone  ")
		if err != nil {
			t.Fatalf("starting the session: %v", err)
		}

		var label string
		if err := pool.QueryRow(t.Context(),
			`SELECT device_label FROM device_sessions WHERE id = $1`, pair.SessionID).Scan(&label); err != nil {
			t.Fatalf("reading the label: %v", err)
		}
		if label != "Nethmin's iPhone" {
			t.Errorf("device_label = %q, want it trimmed", label)
		}
	})
}

// TestStartSessionRecordsLastSeen. "Last used two months ago" beside a device nobody recognises
// is the whole reason anybody revokes one (000100), so the column has to move when the session
// is used rather than only when it is created.
func TestStartSessionRecordsLastSeen(t *testing.T) {
	clk := clock.NewFixed(time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC))
	svc, pool, user := newSessionService(t, clk)

	pair, err := svc.startSession(t.Context(), pool, user, "iPhone")
	if err != nil {
		t.Fatalf("starting the session: %v", err)
	}

	clk.Advance(72 * time.Hour)
	if _, err := svc.Refresh(t.Context(), pair.Refresh.Value); err != nil {
		t.Fatalf("refreshing: %v", err)
	}

	var lastSeen time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT last_seen_at FROM device_sessions WHERE id = $1`, pair.SessionID).Scan(&lastSeen); err != nil {
		t.Fatalf("reading last_seen_at: %v", err)
	}
	if !lastSeen.Equal(clk.Now()) {
		t.Errorf("last_seen_at = %s, want %s — the device list would show a phone in daily use "+
			"as last seen on the day it signed in", lastSeen, clk.Now())
	}
}

// TestRefreshWithoutADatabaseIsUnavailable, for the reason Register's equivalent gives: 503
// tells a mobile client to retry and 500 tells it to give up.
func TestRefreshWithoutADatabaseIsUnavailable(t *testing.T) {
	hasher, err := NewPasswordHasher(testProfile)
	if err != nil {
		t.Fatalf("building the hasher: %v", err)
	}
	svc, err := NewService(nil, hasher, testServiceIssuer(t, clock.System{}),
		&recordingSender{}, &recordingTexter{}, clock.System{})
	if err != nil {
		t.Fatalf("building the service: %v", err)
	}

	if _, err := svc.Refresh(t.Context(), "a-token-shaped-string"); !errors.Is(err, errUnavailable) {
		t.Errorf("got %v, want errUnavailable", err)
	}
}

// TestAServiceWithNoIssuerIsRefused. A service that could create a session without an issuer
// would hand out a refresh token the caller cannot exchange for anything.
func TestAServiceWithNoIssuerIsRefused(t *testing.T) {
	hasher, err := NewPasswordHasher(testProfile)
	if err != nil {
		t.Fatalf("building the hasher: %v", err)
	}
	if _, err := NewService(nil, hasher, nil,
		&recordingSender{}, &recordingTexter{}, clock.System{}); err == nil {
		t.Error("a service was built with no access token issuer")
	}
}
