package identity

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
)

// SHIP-34 against a real PostgreSQL. Every property here is a constraint or a count over rows,
// and both are what Docs/06 §4.1 says a mock would accept while proving nothing.

var codePattern = regexp.MustCompile(`\b\d{6}\b`)

// codeFromMessage pulls the one-time code out of the message that was sent, which is the only
// place it exists — the table holds an argon2id hash.
func codeFromMessage(t *testing.T, body string) string {
	t.Helper()

	found := codePattern.FindString(body)
	if found == "" {
		t.Fatalf("no six-digit code in the message: %q", body)
	}
	return found
}

// registerFor creates an account with a number of its own, so tests do not collide on
// uq_users_phone.
func registerFor(t *testing.T, svc *Service, suffix string) User {
	t.Helper()

	cmd := validRegistration()
	cmd.Email = "otp-" + suffix + "@example.com"
	cmd.Phone = "+6141200" + suffix

	user, err := svc.Register(t.Context(), cmd)
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	return user
}

func liveOTPCount(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) int {
	t.Helper()

	var n int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM phone_otps WHERE user_id = $1 AND consumed_at IS NULL`,
		userID).Scan(&n); err != nil {
		t.Fatalf("counting live codes: %v", err)
	}
	return n
}

func otpCount(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) int {
	t.Helper()

	var n int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM phone_otps WHERE user_id = $1`, userID).Scan(&n); err != nil {
		t.Fatalf("counting codes: %v", err)
	}
	return n
}

// TestRequestOTPIssuesATimeLimitedCode is SHIP-34's acceptance criterion, less the rate limit.
func TestRequestOTPIssuesATimeLimitedCode(t *testing.T) {
	now := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	svc, pool, texter := newTestServiceWithSMS(t, clock.NewFixed(now))

	user := registerFor(t, svc, "1001")

	retryAfter, err := svc.RequestOTP(t.Context(), "0412 00 1001")
	if err != nil {
		t.Fatalf("requesting a code: %v", err)
	}
	if retryAfter != otpResendCooldown {
		t.Errorf("retry after %s, want the fixed cooldown %s — a true remaining interval would "+
			"disclose that this number has an account", retryAfter, otpResendCooldown)
	}

	sent := texter.last(t)
	if sent.to != user.Phone {
		t.Errorf("the message went to %q, want %q", sent.to, user.Phone)
	}
	code := codeFromMessage(t, sent.body)
	if len(code) != otpDigits {
		t.Errorf("the code is %d digits, want %d", len(code), otpDigits)
	}

	var (
		hash      string
		phone     string
		expiresAt time.Time
		attempts  int
	)
	if err := pool.QueryRow(t.Context(), `
		SELECT code_hash, phone, expires_at, attempts
		  FROM phone_otps WHERE user_id = $1 AND consumed_at IS NULL`,
		user.ID).Scan(&hash, &phone, &expiresAt, &attempts); err != nil {
		t.Fatalf("no live code was stored: %v", err)
	}

	if phone != user.Phone {
		t.Errorf("the code records %q, want the number it was sent to", phone)
	}
	if attempts != 0 {
		t.Errorf("attempts = %d on a fresh code", attempts)
	}
	if want := now.Add(otpTTL); !expiresAt.Equal(want) {
		t.Errorf("expires_at = %s, want %s — the TTL is measured against the injected clock",
			expiresAt, want)
	}

	// Hashed, and hashed with the function that resists a 10^6 search rather than one that
	// does not.
	if hash == code {
		t.Fatal("the code itself is in the database")
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("code_hash = %q, want an argon2id PHC string — SHA-256 over six digits is a "+
			"table a laptop builds in under a second", hash)
	}

	hasher, _ := NewPasswordHasher(testProfile)
	ok, err := hasher.Verify(hash, code)
	if err != nil || !ok {
		t.Errorf("the stored hash does not verify the code that was sent (ok=%v, err=%v)", ok, err)
	}
}

// TestRequestOTPIsRateLimitedByCooldown. A person who does not receive a message taps again
// within seconds; without this, every tap costs a message.
func TestRequestOTPIsRateLimitedByCooldown(t *testing.T) {
	clk := clock.NewFixed(time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC))
	svc, pool, texter := newTestServiceWithSMS(t, clk)

	user := registerFor(t, svc, "1002")

	if _, err := svc.RequestOTP(t.Context(), user.Phone); err != nil {
		t.Fatalf("first request: %v", err)
	}
	first := texter.last(t)

	clk.Advance(otpResendCooldown - time.Second)
	if _, err := svc.RequestOTP(t.Context(), user.Phone); err != nil {
		t.Fatalf("second request: %v", err)
	}

	if n := otpCount(t, pool, user.ID); n != 1 {
		t.Errorf("%d codes after a request inside the cooldown, want 1", n)
	}
	if len(texter.sent) != 1 {
		t.Errorf("%d messages sent, want 1", len(texter.sent))
	}

	// And the throttled caller is told nothing different, which is the whole point.
	clk.Advance(2 * time.Second)
	if _, err := svc.RequestOTP(t.Context(), user.Phone); err != nil {
		t.Fatalf("third request: %v", err)
	}
	if n := otpCount(t, pool, user.ID); n != 2 {
		t.Errorf("%d codes once the cooldown has passed, want 2", n)
	}
	if texter.last(t).body == first.body {
		t.Error("the second code is identical to the first")
	}
}

// TestRequestOTPIsRateLimitedByWindow. SMS costs money per message and wakes a real handset, so
// an unbounded resend is a bill directed at whoever owns the number.
func TestRequestOTPIsRateLimitedByWindow(t *testing.T) {
	clk := clock.NewFixed(time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC))
	svc, pool, texter := newTestServiceWithSMS(t, clk)

	user := registerFor(t, svc, "1003")

	// Enough requests to exhaust the window, each past the cooldown.
	for range otpMaxPerWindow + 3 {
		if _, err := svc.RequestOTP(t.Context(), user.Phone); err != nil {
			t.Fatalf("requesting: %v", err)
		}
		clk.Advance(otpResendCooldown + time.Second)
	}

	if n := otpCount(t, pool, user.ID); n != otpMaxPerWindow {
		t.Errorf("%d codes issued, want the window cap of %d", n, otpMaxPerWindow)
	}
	if len(texter.sent) != otpMaxPerWindow {
		t.Errorf("%d messages sent, want %d", len(texter.sent), otpMaxPerWindow)
	}

	// Past the window, the account can ask again. A limit that never lifted would lock
	// somebody out of verifying their own number.
	clk.Advance(otpWindow)
	if _, err := svc.RequestOTP(t.Context(), user.Phone); err != nil {
		t.Fatalf("requesting after the window: %v", err)
	}
	if n := otpCount(t, pool, user.ID); n != otpMaxPerWindow+1 {
		t.Errorf("%d codes, want %d — the window did not roll", n, otpMaxPerWindow+1)
	}
}

// TestRequestOTPSupersedesTheOutstandingCode. Pressing "resend" has to invalidate what was sent
// before it, or two codes are live at once for ten minutes.
func TestRequestOTPSupersedesTheOutstandingCode(t *testing.T) {
	clk := clock.NewFixed(time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC))
	svc, pool, _ := newTestServiceWithSMS(t, clk)

	user := registerFor(t, svc, "1004")

	if _, err := svc.RequestOTP(t.Context(), user.Phone); err != nil {
		t.Fatalf("first request: %v", err)
	}
	clk.Advance(otpResendCooldown + time.Second)
	if _, err := svc.RequestOTP(t.Context(), user.Phone); err != nil {
		t.Fatalf("second request: %v", err)
	}

	if n := liveOTPCount(t, pool, user.ID); n != 1 {
		t.Errorf("%d live codes, want exactly 1", n)
	}

	var reason string
	if err := pool.QueryRow(t.Context(), `
		SELECT consumed_reason FROM phone_otps
		 WHERE user_id = $1 AND consumed_at IS NOT NULL`, user.ID).Scan(&reason); err != nil {
		t.Fatalf("reading the retired code: %v", err)
	}
	if reason != consumedSuperseded {
		t.Errorf("the first code is %q, want %q", reason, consumedSuperseded)
	}
}

// TestTwoLiveCodesAreRefusedByTheDatabase. The service supersedes first; this is what happens if
// it ever stops.
func TestTwoLiveCodesAreRefusedByTheDatabase(t *testing.T) {
	svc, pool, _ := newTestServiceWithSMS(t, clock.System{})

	user := registerFor(t, svc, "1005")
	if _, err := svc.RequestOTP(t.Context(), user.Phone); err != nil {
		t.Fatalf("requesting: %v", err)
	}

	_, err := pool.Exec(t.Context(), `
		INSERT INTO phone_otps (id, user_id, phone, code_hash, expires_at)
		VALUES (gen_random_uuid(), $1, $2, 'a-second-live-code', now() + interval '10 minutes')`,
		user.ID, user.Phone)
	if err == nil {
		t.Fatal("a second live code was accepted, so an old code outlives its replacement")
	}
	if !strings.Contains(err.Error(), "uq_phone_otps_live") {
		t.Errorf("expected the partial unique index to refuse it, got: %v", err)
	}
}

// TestRequestOTPForAnUnknownNumberDisclosesNothing.
//
// This is the privacy property, and it is the reason the endpoint answers the way it does. A
// different status for a known number would make "does this person have a Shipper account"
// answerable one number at a time by anybody.
func TestRequestOTPForAnUnknownNumberDisclosesNothing(t *testing.T) {
	svc, pool, texter := newTestServiceWithSMS(t, clock.System{})

	known := registerFor(t, svc, "1006")

	unknownRetry, unknownErr := svc.RequestOTP(t.Context(), "+61412009999")
	knownRetry, knownErr := svc.RequestOTP(t.Context(), known.Phone)

	if unknownErr != nil || knownErr != nil {
		t.Fatalf("errors differ: unknown=%v known=%v", unknownErr, knownErr)
	}
	if unknownRetry != knownRetry {
		t.Errorf("retry differs: unknown=%s known=%s", unknownRetry, knownRetry)
	}

	// Nothing was written for the number nobody owns, and nothing was sent to it.
	var rows int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM phone_otps WHERE phone = '+61412009999'`).Scan(&rows); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if rows != 0 {
		t.Errorf("%d codes stored for a number with no account", rows)
	}
	for _, m := range texter.sent {
		if m.to == "+61412009999" {
			t.Error("a message was sent to a number with no account")
		}
	}
}

// TestRequestOTPForAVerifiedNumberSendsNothing. Sending another code would cost a message for no
// outcome, and refusing would disclose the account's verification state.
func TestRequestOTPForAVerifiedNumberSendsNothing(t *testing.T) {
	svc, pool, texter := newTestServiceWithSMS(t, clock.System{})

	user := registerFor(t, svc, "1007")
	if _, err := pool.Exec(t.Context(),
		`UPDATE users SET phone_verified_at = now() WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("marking verified: %v", err)
	}

	retryAfter, err := svc.RequestOTP(t.Context(), user.Phone)
	if err != nil {
		t.Fatalf("requesting: %v", err)
	}
	if retryAfter != otpResendCooldown {
		t.Errorf("retry after %s, want the same fixed value every other outcome gives", retryAfter)
	}
	if len(texter.sent) != 0 {
		t.Errorf("%d messages sent to an already verified number", len(texter.sent))
	}
	if n := otpCount(t, pool, user.ID); n != 0 {
		t.Errorf("%d codes stored for an already verified number", n)
	}
}

// TestRequestOTPRejectsAnUnusableNumber. A malformed number is a client mistake worth reporting,
// and reporting it discloses nothing: the answer is the same for every unusable value.
func TestRequestOTPRejectsAnUnusableNumber(t *testing.T) {
	svc, _, _ := newTestServiceWithSMS(t, clock.System{})

	for _, phone := range []string{"", "   ", "123", "not-a-number", "+0412345678"} {
		t.Run(phone, func(t *testing.T) {
			_, err := svc.RequestOTP(t.Context(), phone)
			if err == nil {
				t.Fatalf("accepted %q", phone)
			}
			assertFieldRejected(t, err, "phone")
		})
	}
}

// TestOTPCodesAreUnpredictable. Six digits is a small space, so the draw has to be uniform over
// all of it — including the codes with leading zeros, which a naïve implementation loses.
func TestOTPCodesAreUnpredictable(t *testing.T) {
	seen := map[string]int{}
	for range 200 {
		code, err := newOTPCode()
		if err != nil {
			t.Fatalf("drawing: %v", err)
		}
		if !numericCode(code) {
			t.Fatalf("drew %q, which is not %d digits", code, otpDigits)
		}
		seen[code]++
	}

	// Two hundred draws from a million values colliding more than once would be
	// extraordinary, and a repeat of the same value is the signature of a broken source.
	for code, n := range seen {
		if n > 2 {
			t.Errorf("%q was drawn %d times in 200", code, n)
		}
	}
}

func TestNumericCode(t *testing.T) {
	valid := []string{"000000", "123456", "999999"}
	invalid := []string{"", "12345", "1234567", "12345a", "12 456", "-12345"}

	for _, code := range valid {
		if !numericCode(code) {
			t.Errorf("numericCode(%q) = false, want true", code)
		}
	}
	for _, code := range invalid {
		if numericCode(code) {
			t.Errorf("numericCode(%q) = true, want false", code)
		}
	}
}
