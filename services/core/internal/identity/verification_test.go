package identity

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
)

// SHIP-31 against a real PostgreSQL. The properties being asserted are all constraints —
// one live token per account, a hash that is not the token, an expiry the row refuses to be
// created without — and a mocked store would accept every write they exist to reject.

// tokenPattern is what the message carries: 32 random bytes, base64url, unpadded.
var tokenPattern = regexp.MustCompile(`[A-Za-z0-9_-]{43}`)

// tokenFromMessage pulls the verification value out of the email that was sent, which is the
// only place it exists. Reading it from the database is impossible by design.
func tokenFromMessage(t *testing.T, body string) string {
	t.Helper()

	found := tokenPattern.FindString(body)
	if found == "" {
		t.Fatalf("no verification token in the message:\n%s", body)
	}
	return found
}

// TestRegistrationIssuesAVerificationToken is SHIP-31's acceptance criterion.
func TestRegistrationIssuesAVerificationToken(t *testing.T) {
	svc, pool, mail := newTestService(t)

	before := time.Now().UTC()
	user, err := svc.Register(t.Context(), validRegistration())
	if err != nil {
		t.Fatalf("registering: %v", err)
	}

	var (
		hash      string
		email     string
		expiresAt time.Time
		consumed  *time.Time
	)
	if err := pool.QueryRow(t.Context(), `
		SELECT token_hash, email, expires_at, consumed_at
		  FROM email_verification_tokens WHERE user_id = $1`,
		user.ID).Scan(&hash, &email, &expiresAt, &consumed); err != nil {
		t.Fatalf("no token was stored on registration: %v", err)
	}

	if consumed != nil {
		t.Error("a freshly issued token is already consumed")
	}
	if email != user.Email {
		t.Errorf("the token records %q, want the address it was sent to (%q)", email, user.Email)
	}

	// Expiring: a day, and in the future. Checked as a window rather than an equality because
	// the row was written against the service's clock and this reads a wall clock.
	wantExpiry := before.Add(emailVerificationTokenTTL)
	if expiresAt.Before(wantExpiry.Add(-time.Minute)) || expiresAt.After(wantExpiry.Add(time.Minute)) {
		t.Errorf("expires_at = %s, want about %s", expiresAt, wantExpiry)
	}

	// The token was sent, and the stored value is its hash rather than the token.
	sent := mail.last(t)
	if sent.to != user.Email {
		t.Errorf("the message went to %q, want %q", sent.to, user.Email)
	}
	raw := tokenFromMessage(t, sent.body)

	if hash == raw {
		t.Fatal("the token itself is in the database, so anyone who can read the table can " +
			"verify every unverified address on the platform")
	}
	if hash != hashVerificationToken(raw) {
		t.Errorf("the stored hash is not the hash of the token that was sent")
	}
	if len(hash) != 64 {
		t.Errorf("token_hash is %d characters, want SHA-256 hex", len(hash))
	}
}

// TestTheTokenIsNeverStoredInTheClear sweeps the whole row rather than the one column, because
// the failure this guards against is a *second* column added later that happens to keep it.
func TestTheTokenIsNeverStoredInTheClear(t *testing.T) {
	svc, pool, mail := newTestService(t)

	user, err := svc.Register(t.Context(), validRegistration())
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	raw := tokenFromMessage(t, mail.last(t).body)

	var row string
	if err := pool.QueryRow(t.Context(),
		`SELECT to_jsonb(t)::text FROM email_verification_tokens t WHERE user_id = $1`,
		user.ID).Scan(&row); err != nil {
		t.Fatalf("reading the row: %v", err)
	}
	if strings.Contains(row, raw) {
		t.Fatalf("the token appears in the stored row: %s", row)
	}
}

// TestReissuingSupersedesTheOutstandingToken.
//
// Without this an older link keeps working after a newer one is issued, so a message forwarded
// or left in an old mailbox stays valid for its full day. The partial unique index is what makes
// it a guarantee rather than a step somebody remembered.
func TestReissuingSupersedesTheOutstandingToken(t *testing.T) {
	svc, pool, mail := newTestService(t)

	user, err := svc.Register(t.Context(), validRegistration())
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	first := tokenFromMessage(t, mail.last(t).body)

	if _, err := svc.issueEmailVerification(t.Context(), pool, user); err != nil {
		t.Fatalf("reissuing: %v", err)
	}

	var live int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM email_verification_tokens WHERE user_id = $1 AND consumed_at IS NULL`,
		user.ID).Scan(&live); err != nil {
		t.Fatalf("counting live tokens: %v", err)
	}
	if live != 1 {
		t.Errorf("%d live tokens, want exactly 1", live)
	}

	var reason string
	if err := pool.QueryRow(t.Context(),
		`SELECT consumed_reason FROM email_verification_tokens WHERE token_hash = $1`,
		hashVerificationToken(first)).Scan(&reason); err != nil {
		t.Fatalf("reading the first token: %v", err)
	}
	if reason != consumedSuperseded {
		t.Errorf("the first token is %q, want %q — 'verified' would claim it did its job",
			reason, consumedSuperseded)
	}

	// The history survives, which is what the resend limit counts over (SHIP-33).
	var total int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM email_verification_tokens WHERE user_id = $1`, user.ID).Scan(&total); err != nil {
		t.Fatalf("counting all tokens: %v", err)
	}
	if total != 2 {
		t.Errorf("%d rows, want 2 — superseded tokens are retired, not deleted", total)
	}
}

// TestTwoLiveTokensAreRefusedByTheDatabase. The application supersedes first; this is what
// happens if it ever stops, and it is why the rule is an index rather than a convention.
func TestTwoLiveTokensAreRefusedByTheDatabase(t *testing.T) {
	svc, pool, _ := newTestService(t)

	user, err := svc.Register(t.Context(), validRegistration())
	if err != nil {
		t.Fatalf("registering: %v", err)
	}

	_, err = pool.Exec(t.Context(), `
		INSERT INTO email_verification_tokens (id, user_id, email, token_hash, expires_at)
		VALUES (gen_random_uuid(), $1, $2, 'a-second-live-token', now() + interval '1 day')`,
		user.ID, user.Email)
	if err == nil {
		t.Fatal("a second live token was accepted, so an old link outlives its replacement")
	}
	if !strings.Contains(err.Error(), "uq_email_verification_tokens_live") {
		t.Errorf("expected the partial unique index to refuse it, got: %v", err)
	}
}

// TestAFailedSendDoesNotFailTheRegistration.
//
// The account is already committed by the time the message goes out. Reporting failure would
// leave the caller with an account they were told was not created, and their retry would answer
// identity_email_taken — a dead end reachable only by support.
func TestAFailedSendDoesNotFailTheRegistration(t *testing.T) {
	svc, pool, mail := newTestService(t)
	mail.err = errors.New("the provider is down")

	user, err := svc.Register(t.Context(), validRegistration())
	if err != nil {
		t.Fatalf("registration failed because the email did: %v", err)
	}

	var tokens int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM email_verification_tokens WHERE user_id = $1`, user.ID).Scan(&tokens); err != nil {
		t.Fatalf("counting tokens: %v", err)
	}
	if tokens != 1 {
		t.Errorf("%d tokens, want 1 — the token is stored whether or not the message got out, "+
			"so a resend has something to supersede", tokens)
	}
}

// TestARolledBackRegistrationLeavesNoToken.
//
// The account and its token are one transaction (SHIP-31: "stored on registration"). A duplicate
// address is the readily available way to make the transaction fail after the token has been
// prepared, and the row must not survive it.
func TestARolledBackRegistrationLeavesNoToken(t *testing.T) {
	svc, pool, _ := newTestService(t)

	if _, err := svc.Register(t.Context(), validRegistration()); err != nil {
		t.Fatalf("the first registration failed: %v", err)
	}

	second := validRegistration()
	second.Phone = "0412 345 679"
	if _, err := svc.Register(t.Context(), second); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("the second registration returned %v, want ErrEmailTaken", err)
	}

	var tokens int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM email_verification_tokens`).Scan(&tokens); err != nil {
		t.Fatalf("counting tokens: %v", err)
	}
	if tokens != 1 {
		t.Errorf("%d tokens after one successful registration and one refused, want 1", tokens)
	}
}

// TestVerificationTokensAreUnpredictable. Two tokens issued back to back share nothing, which is
// the property that makes guessing 2^256 rather than a matter of watching the clock.
func TestVerificationTokensAreUnpredictable(t *testing.T) {
	seen := map[string]bool{}
	for range 64 {
		raw, hash, err := newVerificationToken()
		if err != nil {
			t.Fatalf("generating: %v", err)
		}
		if len(raw) != 43 {
			t.Fatalf("token is %d characters, want 43 (32 bytes, base64url, unpadded)", len(raw))
		}
		if seen[raw] {
			t.Fatal("two tokens were identical")
		}
		seen[raw] = true

		if hash != hashVerificationToken(raw) {
			t.Fatal("the hash returned beside a token is not the hash of that token")
		}
	}
}
