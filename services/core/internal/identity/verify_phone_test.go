package identity

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
)

// SHIP-36 against a real PostgreSQL. The attempt counter and the retirement it triggers are both
// rows, and the wrong-guess path deliberately commits — a mocked store would report all of this
// as working while the column stayed at zero.

// verifiablePhone registers an account, requests a code, and hands back the code that was sent.
func verifiablePhone(t *testing.T, svc *Service, texter *recordingTexter, suffix string) (User, string) {
	t.Helper()

	user := registerFor(t, svc, suffix)
	if _, err := svc.RequestOTP(t.Context(), user.Phone); err != nil {
		t.Fatalf("requesting a code: %v", err)
	}
	return user, codeFromMessage(t, texter.last(t).body)
}

func attemptsOn(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) int {
	t.Helper()

	var attempts int
	if err := pool.QueryRow(t.Context(), `
		SELECT attempts FROM phone_otps
		 WHERE user_id = $1 ORDER BY created_at DESC LIMIT 1`, userID).Scan(&attempts); err != nil {
		t.Fatalf("reading attempts: %v", err)
	}
	return attempts
}

// TestVerifyPhoneMarksTheNumberVerified is SHIP-36's acceptance criterion.
func TestVerifyPhoneMarksTheNumberVerified(t *testing.T) {
	now := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	svc, pool, texter := newTestServiceWithSMS(t, clock.NewFixed(now))

	user, code := verifiablePhone(t, svc, texter, "2001")

	verified, err := svc.VerifyPhone(t.Context(), user.Phone, code)
	if err != nil {
		t.Fatalf("verifying: %v", err)
	}
	if !verified.PhoneVerified() {
		t.Error("the returned account does not report its phone as verified")
	}
	if verified.EmailVerified() {
		t.Error("verifying the phone verified the email as well")
	}

	var verifiedAt *time.Time
	var reason *string
	if err := pool.QueryRow(t.Context(), `
		SELECT u.phone_verified_at, o.consumed_reason
		  FROM users u JOIN phone_otps o ON o.user_id = u.id
		 WHERE u.id = $1`, user.ID).Scan(&verifiedAt, &reason); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if verifiedAt == nil {
		t.Fatal("phone_verified_at is still null")
	}
	if !verifiedAt.UTC().Equal(now) {
		t.Errorf("phone_verified_at = %s, want %s from the injected clock", verifiedAt.UTC(), now)
	}
	if reason == nil || *reason != consumedVerified {
		t.Errorf("consumed_reason = %v, want %q", reason, consumedVerified)
	}
}

// TestVerifyPhoneAcceptsTheNumberInAnyWrittenForm. The code was sent to a number, not to a
// string, and a person retyping it will not reproduce the E.164 form.
func TestVerifyPhoneAcceptsTheNumberInAnyWrittenForm(t *testing.T) {
	svc, _, texter := newTestServiceWithSMS(t, clock.System{})

	user, code := verifiablePhone(t, svc, texter, "2002")

	// +61412002002 written the way somebody types it locally.
	local := "0412 00 2002"
	if normalisePhone(local) != user.Phone {
		t.Fatalf("the test's own number does not normalise to %s", user.Phone)
	}

	if _, err := svc.VerifyPhone(t.Context(), local, code); err != nil {
		t.Fatalf("verifying with a locally written number: %v", err)
	}
}

// TestVerifyPhoneRejectsTheWrongCodeAndCountsIt.
//
// The count is the whole point: an increment that rolled back with the error would leave the
// column at zero and the limit would limit nothing.
func TestVerifyPhoneRejectsTheWrongCodeAndCountsIt(t *testing.T) {
	svc, pool, texter := newTestServiceWithSMS(t, clock.System{})

	user, code := verifiablePhone(t, svc, texter, "2003")

	wrong := "000000"
	if wrong == code {
		wrong = "111111"
	}

	if _, err := svc.VerifyPhone(t.Context(), user.Phone, wrong); !errors.Is(err, ErrOTPInvalid) {
		t.Fatalf("returned %v, want ErrOTPInvalid", err)
	}
	if n := attemptsOn(t, pool, user.ID); n != 1 {
		t.Fatalf("attempts = %d after one wrong guess, want 1 — the increment rolled back with "+
			"the error, so the limit counts nothing", n)
	}

	// The number is not verified, and the right code still works afterwards.
	var verifiedAt *time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT phone_verified_at FROM users WHERE id = $1`, user.ID).Scan(&verifiedAt); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if verifiedAt != nil {
		t.Error("a wrong code verified the number")
	}
	if _, err := svc.VerifyPhone(t.Context(), user.Phone, code); err != nil {
		t.Fatalf("the correct code failed after a wrong one: %v", err)
	}
}

// TestVerifyPhoneRetiresTheCodeAfterTooManyGuesses. The online defence: 10^6 is only expensive
// if guessing costs something.
func TestVerifyPhoneRetiresTheCodeAfterTooManyGuesses(t *testing.T) {
	svc, pool, texter := newTestServiceWithSMS(t, clock.System{})

	user, code := verifiablePhone(t, svc, texter, "2004")

	wrong := "000000"
	if wrong == code {
		wrong = "111111"
	}

	for i := range otpMaxAttempts {
		if _, err := svc.VerifyPhone(t.Context(), user.Phone, wrong); !errors.Is(err, ErrOTPInvalid) {
			t.Fatalf("guess %d returned %v, want ErrOTPInvalid", i+1, err)
		}
	}

	var reason *string
	if err := pool.QueryRow(t.Context(),
		`SELECT consumed_reason FROM phone_otps WHERE user_id = $1`, user.ID).Scan(&reason); err != nil {
		t.Fatalf("reading the code: %v", err)
	}
	if reason == nil || *reason != consumedExhausted {
		t.Fatalf("consumed_reason = %v, want %q — the code is still live after %d wrong guesses",
			reason, consumedExhausted, otpMaxAttempts)
	}

	// Even the correct code no longer works, and the answer is the same one every other
	// failure gives.
	if _, err := svc.VerifyPhone(t.Context(), user.Phone, code); !errors.Is(err, ErrOTPInvalid) {
		t.Errorf("the correct code still worked after the limit: %v", err)
	}
}

// TestVerifyPhoneRejectsAnExpiredCode, and leaves it live so the next request has something to
// supersede rather than leaving the account with none.
func TestVerifyPhoneRejectsAnExpiredCode(t *testing.T) {
	clk := clock.NewFixed(time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC))
	svc, pool, texter := newTestServiceWithSMS(t, clk)

	user, code := verifiablePhone(t, svc, texter, "2005")

	clk.Advance(otpTTL)
	if _, err := svc.VerifyPhone(t.Context(), user.Phone, code); !errors.Is(err, ErrOTPInvalid) {
		t.Fatalf("returned %v, want ErrOTPInvalid", err)
	}
	if n := liveOTPCount(t, pool, user.ID); n != 1 {
		t.Errorf("%d live codes after an expired attempt, want 1", n)
	}

	// Asking again replaces it, and the replacement works.
	if _, err := svc.RequestOTP(t.Context(), user.Phone); err != nil {
		t.Fatalf("requesting another: %v", err)
	}
	if _, err := svc.VerifyPhone(t.Context(), user.Phone, codeFromMessage(t, texter.last(t).body)); err != nil {
		t.Fatalf("the replacement code failed: %v", err)
	}
}

// TestVerifyPhoneAnswersTheSameForEveryFailure.
//
// This is the privacy property. Six digits is a guessable space, so a distinguishable answer for
// "no account" or "no outstanding code" is information handed to whoever is guessing — and the
// first of those is the one that says whether the number belongs to anybody.
func TestVerifyPhoneAnswersTheSameForEveryFailure(t *testing.T) {
	svc, _, texter := newTestServiceWithSMS(t, clock.System{})

	withCode, code := verifiablePhone(t, svc, texter, "2006")
	withoutCode := registerFor(t, svc, "2007")

	wrong := "000000"
	if wrong == code {
		wrong = "111111"
	}

	cases := map[string]struct{ phone, code string }{
		"no account at all":     {"+61412009998", wrong},
		"no outstanding code":   {withoutCode.Phone, wrong},
		"wrong code":            {withCode.Phone, wrong},
		"a code of wrong shape": {withCode.Phone, "12345"},
		"letters":               {withCode.Phone, "abcdef"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.VerifyPhone(t.Context(), tc.phone, tc.code)
			if !errors.Is(err, ErrOTPInvalid) {
				t.Errorf("returned %v, want ErrOTPInvalid — every failure has to look alike", err)
			}
		})
	}
}

// TestVerifyPhoneRejectsAMissingField. Empty values are a client mistake worth reporting per
// field; a wrong value is not, for the reason above.
func TestVerifyPhoneRejectsAMissingField(t *testing.T) {
	svc, _, _ := newTestServiceWithSMS(t, clock.System{})

	t.Run("no phone", func(t *testing.T) {
		_, err := svc.VerifyPhone(t.Context(), "", "123456")
		assertFieldRejected(t, err, "phone")
	})
	t.Run("no code", func(t *testing.T) {
		_, err := svc.VerifyPhone(t.Context(), "+61412000000", "")
		assertFieldRejected(t, err, "code")
	})
}

// TestVerifyPhoneDoesNotHashAnUnusableCode.
//
// argon2id at the production profile is 64 MiB per call, and this endpoint needs no account. A
// caller posting arbitrary strings must not be able to choose how much memory the service
// allocates, so the shape is checked before anything is hashed.
func TestVerifyPhoneDoesNotHashAnUnusableCode(t *testing.T) {
	svc, pool, texter := newTestServiceWithSMS(t, clock.System{})

	user, _ := verifiablePhone(t, svc, texter, "2008")

	if _, err := svc.VerifyPhone(t.Context(), user.Phone, "not-six-digits"); !errors.Is(err, ErrOTPInvalid) {
		t.Fatalf("returned %v, want ErrOTPInvalid", err)
	}

	// Nothing reached the code at all, which is what "before anything is hashed" means in a
	// form a test can see: no attempt was recorded.
	if n := attemptsOn(t, pool, user.ID); n != 0 {
		t.Errorf("attempts = %d, want 0 — a malformed code reached the comparison", n)
	}
}
