package identity

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/passwords"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-33 against a real PostgreSQL. Single use is a guarded UPDATE and "already verified" is a
// column, so both are properties of the database rather than of the code that reads it.

// newVerifiableService builds a service whose clock the test can move, and registers one account
// with a live token.
func newVerifiableService(t *testing.T, clk clock.Clock) (*Service, *pgxpool.Pool, *recordingSender, User) {
	t.Helper()

	pool := pgtest.DB(t)

	hasher, err := passwords.NewHasher(testProfile)
	if err != nil {
		t.Fatalf("building the hasher: %v", err)
	}

	mail := &recordingSender{}
	svc, err := NewService(pool, hasher, testServiceIssuer(t, clk), testLimiter(t),
		mail, &recordingTexter{}, noDelivery, clk)
	if err != nil {
		t.Fatalf("building the service: %v", err)
	}

	user, err := svc.Register(t.Context(), validRegistration())
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	return svc, pool, mail, user
}

// TestVerifyEmailMarksTheAddressVerifiedAndConsumesTheToken is SHIP-33's acceptance criterion.
func TestVerifyEmailMarksTheAddressVerifiedAndConsumesTheToken(t *testing.T) {
	now := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	svc, pool, mail, user := newVerifiableService(t, clock.NewFixed(now))

	token := tokenFromMessage(t, mail.last(t).body)

	verified, err := svc.VerifyEmail(t.Context(), token)
	if err != nil {
		t.Fatalf("verifying: %v", err)
	}
	if !verified.EmailVerified() {
		t.Error("the returned account does not report its email as verified")
	}
	if verified.PhoneVerified() {
		t.Error("verifying the email verified the phone as well")
	}

	var verifiedAt *time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT email_verified_at FROM users WHERE id = $1`, user.ID).Scan(&verifiedAt); err != nil {
		t.Fatalf("reading the account: %v", err)
	}
	if verifiedAt == nil {
		t.Fatal("email_verified_at is still null")
	}
	if !verifiedAt.UTC().Equal(now) {
		t.Errorf("email_verified_at = %s, want %s from the injected clock", verifiedAt.UTC(), now)
	}

	var consumedAt *time.Time
	var reason *string
	if err := pool.QueryRow(t.Context(),
		`SELECT consumed_at, consumed_reason FROM email_verification_tokens WHERE user_id = $1`,
		user.ID).Scan(&consumedAt, &reason); err != nil {
		t.Fatalf("reading the token: %v", err)
	}
	if consumedAt == nil {
		t.Fatal("the token was not consumed, so it can be used again")
	}
	if reason == nil || *reason != consumedVerified {
		t.Errorf("consumed_reason = %v, want %q", reason, consumedVerified)
	}
}

// TestVerifyEmailIsSingleUse. The second presentation of a token whose account is verified is a
// double click and answers 200; a token consumed any other way does not.
func TestVerifyEmailIsSingleUse(t *testing.T) {
	svc, pool, mail, user := newVerifiableService(t, clock.System{})

	token := tokenFromMessage(t, mail.last(t).body)
	if _, err := svc.VerifyEmail(t.Context(), token); err != nil {
		t.Fatalf("first verification: %v", err)
	}

	t.Run("clicking the link twice is not an error", func(t *testing.T) {
		again, err := svc.VerifyEmail(t.Context(), token)
		if err != nil {
			t.Fatalf("second verification: %v", err)
		}
		if !again.EmailVerified() {
			t.Error("the account is no longer verified")
		}
	})

	t.Run("the verification instant does not move", func(t *testing.T) {
		var count int
		if err := pool.QueryRow(t.Context(), `
			SELECT count(*) FROM email_verification_tokens
			 WHERE user_id = $1 AND consumed_reason = $2`,
			user.ID, consumedVerified).Scan(&count); err != nil {
			t.Fatalf("counting: %v", err)
		}
		if count != 1 {
			t.Errorf("%d tokens marked verified, want 1", count)
		}
	})
}

// TestVerifyEmailRefusesASupersededToken. The link left in an old mailbox after a resend.
func TestVerifyEmailRefusesASupersededToken(t *testing.T) {
	clk := clock.NewFixed(time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC))
	svc, _, mail, user := newVerifiableService(t, clk)

	first := tokenFromMessage(t, mail.last(t).body)

	clk.Advance(emailResendCooldown + time.Second)
	if _, err := svc.ResendVerification(t.Context(), user.Email); err != nil {
		t.Fatalf("resending: %v", err)
	}
	second := tokenFromMessage(t, mail.last(t).body)
	if first == second {
		t.Fatal("the resend sent the same token")
	}

	if _, err := svc.VerifyEmail(t.Context(), first); !errors.Is(err, ErrVerificationTokenInvalid) {
		t.Fatalf("the superseded token returned %v, want ErrVerificationTokenInvalid", err)
	}

	// And the replacement still works, or superseding would be a way of breaking verification
	// rather than of narrowing it.
	if _, err := svc.VerifyEmail(t.Context(), second); err != nil {
		t.Fatalf("the replacement token failed: %v", err)
	}
}

// TestVerifyEmailRefusesAnExpiredToken, and reports it distinctly, because the remedy is
// specific: ask for another.
func TestVerifyEmailRefusesAnExpiredToken(t *testing.T) {
	clk := clock.NewFixed(time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC))
	svc, pool, mail, user := newVerifiableService(t, clk)

	token := tokenFromMessage(t, mail.last(t).body)

	clk.Advance(emailVerificationTokenTTL)
	_, err := svc.VerifyEmail(t.Context(), token)
	if !errors.Is(err, ErrVerificationTokenExpired) {
		t.Fatalf("returned %v, want ErrVerificationTokenExpired", err)
	}

	// Nothing was consumed and nothing was verified: an expired token must not half-succeed.
	var verifiedAt, consumedAt *time.Time
	if err := pool.QueryRow(t.Context(), `
		SELECT u.email_verified_at, t.consumed_at
		  FROM users u JOIN email_verification_tokens t ON t.user_id = u.id
		 WHERE u.id = $1`, user.ID).Scan(&verifiedAt, &consumedAt); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if verifiedAt != nil {
		t.Error("an expired token verified the address")
	}
	if consumedAt != nil {
		t.Error("an expired token was consumed, so a resend has nothing to supersede")
	}
}

// TestVerifyEmailRefusesAnUnknownToken. The overwhelmingly likely caller here is somebody
// guessing, and they learn nothing.
func TestVerifyEmailRefusesAnUnknownToken(t *testing.T) {
	svc, _, _, _ := newVerifiableService(t, clock.System{})

	for name, token := range map[string]string{
		"empty":           "",
		"whitespace":      "   ",
		"not a token":     "nonsense",
		"right shape":     "9qE2vT7bYw1sJk4pNc0aRlX8oZgHdM3uQiV6yB5tCfE",
		"the hash itself": hashVerificationToken("anything"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.VerifyEmail(t.Context(), token); !errors.Is(err, ErrVerificationTokenInvalid) {
				t.Errorf("returned %v, want ErrVerificationTokenInvalid", err)
			}
		})
	}
}

// TestVerifyEmailRefusesATokenForAnAddressTheAccountNoLongerHas.
//
// The token proves control of the address it was *sent to*. SHIP-43 is what makes an address
// change reachable through the API; the check exists now so that it cannot be forgotten then.
func TestVerifyEmailRefusesATokenForAnAddressTheAccountNoLongerHas(t *testing.T) {
	svc, pool, mail, user := newVerifiableService(t, clock.System{})

	token := tokenFromMessage(t, mail.last(t).body)

	if _, err := pool.Exec(t.Context(),
		`UPDATE users SET email = 'moved@example.com' WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("changing the address: %v", err)
	}

	if _, err := svc.VerifyEmail(t.Context(), token); !errors.Is(err, ErrVerificationTokenInvalid) {
		t.Fatalf("returned %v, want ErrVerificationTokenInvalid — a link sent to the old address "+
			"must not verify the new one", err)
	}
}

// TestResendVerificationIssuesANewToken (SHIP-33).
func TestResendVerificationIssuesANewToken(t *testing.T) {
	clk := clock.NewFixed(time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC))
	svc, pool, mail, user := newVerifiableService(t, clk)

	clk.Advance(emailResendCooldown + time.Second)
	retryAfter, err := svc.ResendVerification(t.Context(), user.Email)
	if err != nil {
		t.Fatalf("resending: %v", err)
	}
	if retryAfter != emailResendCooldown {
		t.Errorf("retry after %s, want the fixed cooldown %s", retryAfter, emailResendCooldown)
	}
	if len(mail.sent) != 2 {
		t.Fatalf("%d messages sent, want 2", len(mail.sent))
	}

	var live int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM email_verification_tokens WHERE user_id = $1 AND consumed_at IS NULL`,
		user.ID).Scan(&live); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if live != 1 {
		t.Errorf("%d live tokens after a resend, want 1", live)
	}
}

// TestResendVerificationIsRateLimited, both rules, and neither of them observable in the answer.
func TestResendVerificationIsRateLimited(t *testing.T) {
	clk := clock.NewFixed(time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC))
	svc, pool, mail, user := newVerifiableService(t, clk)

	// Registration already sent one, and the cooldown has not passed.
	if _, err := svc.ResendVerification(t.Context(), user.Email); err != nil {
		t.Fatalf("resending: %v", err)
	}
	if len(mail.sent) != 1 {
		t.Errorf("%d messages sent inside the cooldown, want 1", len(mail.sent))
	}

	for range emailMaxPerWindow + 3 {
		clk.Advance(emailResendCooldown + time.Second)
		if _, err := svc.ResendVerification(t.Context(), user.Email); err != nil {
			t.Fatalf("resending: %v", err)
		}
	}

	var issued int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM email_verification_tokens WHERE user_id = $1`, user.ID).Scan(&issued); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if issued != emailMaxPerWindow {
		t.Errorf("%d tokens issued, want the window cap of %d", issued, emailMaxPerWindow)
	}
}

// TestResendVerificationDisclosesNothing. The privacy property, stated as an equality between
// what a known address is told and what an unknown one is told.
func TestResendVerificationDisclosesNothing(t *testing.T) {
	svc, pool, mail, user := newVerifiableService(t, clock.System{})

	unknownRetry, unknownErr := svc.ResendVerification(t.Context(), "nobody@example.com")
	knownRetry, knownErr := svc.ResendVerification(t.Context(), user.Email)

	if unknownErr != nil || knownErr != nil {
		t.Fatalf("errors differ: unknown=%v known=%v", unknownErr, knownErr)
	}
	if unknownRetry != knownRetry {
		t.Errorf("retry differs: unknown=%s known=%s", unknownRetry, knownRetry)
	}

	for _, m := range mail.sent {
		if m.to == "nobody@example.com" {
			t.Error("a message was sent to an address with no account")
		}
	}

	var rows int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM email_verification_tokens WHERE email = 'nobody@example.com'`).
		Scan(&rows); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if rows != 0 {
		t.Errorf("%d tokens stored for an address with no account", rows)
	}
}

// TestResendVerificationForAVerifiedAddressSendsNothing.
func TestResendVerificationForAVerifiedAddressSendsNothing(t *testing.T) {
	clk := clock.NewFixed(time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC))
	svc, _, mail, user := newVerifiableService(t, clk)

	token := tokenFromMessage(t, mail.last(t).body)
	if _, err := svc.VerifyEmail(t.Context(), token); err != nil {
		t.Fatalf("verifying: %v", err)
	}

	clk.Advance(emailResendCooldown + time.Second)
	if _, err := svc.ResendVerification(t.Context(), user.Email); err != nil {
		t.Fatalf("resending: %v", err)
	}
	if len(mail.sent) != 1 {
		t.Errorf("%d messages sent, want 1 — an already verified address needs nothing", len(mail.sent))
	}
}

// TestResendVerificationRejectsAnUnusableAddress.
func TestResendVerificationRejectsAnUnusableAddress(t *testing.T) {
	svc, _, _, _ := newVerifiableService(t, clock.System{})

	for _, address := range []string{"", "   ", "not-an-address", "a@b"} {
		t.Run(address, func(t *testing.T) {
			_, err := svc.ResendVerification(t.Context(), address)
			if err == nil {
				t.Fatalf("accepted %q", address)
			}
			assertFieldRejected(t, err, "email")
		})
	}
}
