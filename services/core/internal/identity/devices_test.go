package identity

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SHIP-43 against a real PostgreSQL, per Docs/06 §4.1.
//
// "Revokes the current device session only" is a claim about two rows, and the half that a
// plausible implementation gets wrong is the second one: revoking everything the account owns
// looks correct from the device that asked, and is only visible from the phone in the other
// pocket.

// signInAs starts a session for the account signInService registered, under the given label.
func signInAs(t *testing.T, svc *Service, label string) TokenPair {
	t.Helper()

	cmd := validSignIn()
	cmd.DeviceLabel = label

	pair, err := svc.SignIn(t.Context(), cmd)
	if err != nil {
		t.Fatalf("signing in as %q: %v", label, err)
	}
	return pair
}

// revocationOf reads how a session ended, or "live".
func revocationOf(t *testing.T, pool *pgxpool.Pool, sessionID uuid.UUID) string {
	t.Helper()

	var reason string
	if err := pool.QueryRow(t.Context(),
		`SELECT coalesce(revoked_reason, 'live') FROM device_sessions WHERE id = $1`,
		sessionID).Scan(&reason); err != nil {
		t.Fatalf("reading the session's revocation: %v", err)
	}
	return reason
}

// TestSignOutEndsOnlyTheCallingDevice is SHIP-43's acceptance criterion, both halves.
func TestSignOutEndsOnlyTheCallingDevice(t *testing.T) {
	svc, pool, user := signInService(t, testProfile)

	phone := signInAs(t, svc, "Nethmin's iPhone")
	tablet := signInAs(t, svc, "Nethmin's iPad")

	if err := svc.SignOut(t.Context(), user.ID, phone.SessionID); err != nil {
		t.Fatalf("signing out: %v", err)
	}

	t.Run("the calling device is revoked, with the reason that says why", func(t *testing.T) {
		if got := revocationOf(t, pool, phone.SessionID); got != revokedReasonSignedOut {
			t.Errorf("the session is %q, want %q", got, revokedReasonSignedOut)
		}
	})

	t.Run("its refresh token stops working", func(t *testing.T) {
		if _, err := svc.Refresh(t.Context(), phone.Refresh.Value); !errors.Is(err, ErrRefreshTokenInvalid) {
			t.Errorf("err = %v, want ErrRefreshTokenInvalid — the token outlived the session", err)
		}
	})

	t.Run("the other device is untouched", func(t *testing.T) {
		if got := revocationOf(t, pool, tablet.SessionID); got != "live" {
			t.Fatalf("the second device is %q, want live — signing out one device signed out "+
				"every device the account owns", got)
		}
		if _, err := svc.Refresh(t.Context(), tablet.Refresh.Value); err != nil {
			t.Errorf("the other device can no longer refresh: %v", err)
		}
	})
}

// TestSignOutCannotEndSomebodyElsesSession.
//
// The identifier comes from a signed token today, so this cannot happen through the endpoint —
// which is exactly why the check belongs in the statement rather than in the handler. Docs/07 §3
// puts the decision on the platform, and a decision expressed as a predicate on the write is one
// no later caller can leave out.
//
// Mutation-checked: removing `user_id = $2` from revokeOwnDeviceSession makes this fail.
func TestSignOutCannotEndSomebodyElsesSession(t *testing.T) {
	svc, pool, _ := signInService(t, testProfile)

	victim := signInAs(t, svc, "Somebody Else's iPhone")

	stranger, err := svc.Register(t.Context(), RegisterCommand{
		Email:    "stranger@example.com",
		Phone:    "0412 345 679",
		Password: "correct-horse-battery-staple",
		Role:     RoleProvider,
	})
	if err != nil {
		t.Fatalf("registering the stranger: %v", err)
	}

	if err := svc.SignOut(t.Context(), stranger.ID, victim.SessionID); err != nil {
		t.Fatalf("signing out: %v", err)
	}

	if got := revocationOf(t, pool, victim.SessionID); got != "live" {
		t.Errorf("the session is %q, want live — naming another account's session ended it", got)
	}
	if _, err := svc.Refresh(t.Context(), victim.Refresh.Value); err != nil {
		t.Errorf("the victim's device can no longer refresh: %v", err)
	}
}

// TestSignOutIsSafeToRepeat. The client has discarded its tokens by the time it reads the
// response, so a second attempt — an honest retry, or a stale tab — must not become an error, and
// must not move the instant the session ended.
func TestSignOutIsSafeToRepeat(t *testing.T) {
	svc, pool, user := signInService(t, testProfile)
	phone := signInAs(t, svc, "Nethmin's iPhone")

	if err := svc.SignOut(t.Context(), user.ID, phone.SessionID); err != nil {
		t.Fatalf("first sign-out: %v", err)
	}

	var first string
	if err := pool.QueryRow(t.Context(),
		`SELECT revoked_at::text FROM device_sessions WHERE id = $1`, phone.SessionID).Scan(&first); err != nil {
		t.Fatalf("reading revoked_at: %v", err)
	}

	if err := svc.SignOut(t.Context(), user.ID, phone.SessionID); err != nil {
		t.Fatalf("second sign-out: %v", err)
	}

	var second string
	if err := pool.QueryRow(t.Context(),
		`SELECT revoked_at::text FROM device_sessions WHERE id = $1`, phone.SessionID).Scan(&second); err != nil {
		t.Fatalf("reading revoked_at: %v", err)
	}
	if second != first {
		t.Errorf("revoked_at moved from %s to %s; a repeat rewrote when the session ended", first, second)
	}
}

// TestSignOutOfAnUnknownSessionIsNotAnError. A token naming a session that no longer exists is
// the state the caller asked for, and failing would leave somebody on a screen they cannot get
// past.
func TestSignOutOfAnUnknownSessionIsNotAnError(t *testing.T) {
	svc, _, user := signInService(t, testProfile)

	if err := svc.SignOut(t.Context(), user.ID, uuid.New()); err != nil {
		t.Errorf("signing out of a session that does not exist: %v", err)
	}
}

// TestSignOutLeavesTheLiveHashWhereItIs.
//
// SHIP-40's invariant is that a refresh token hash is live in device_sessions or spent in
// consumed_refresh_tokens and never both. Moving the hash across on sign-out is the tidy-looking
// change that breaks it, and it would gain nothing: rotation checks revoked_at first.
func TestSignOutLeavesTheLiveHashWhereItIs(t *testing.T) {
	svc, pool, user := signInService(t, testProfile)
	phone := signInAs(t, svc, "Nethmin's iPhone")

	if err := svc.SignOut(t.Context(), user.ID, phone.SessionID); err != nil {
		t.Fatalf("signing out: %v", err)
	}

	var overlap int
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*) FROM device_sessions d
		  JOIN consumed_refresh_tokens c ON c.token_hash = d.refresh_token_hash
		 WHERE d.id = $1`, phone.SessionID).Scan(&overlap); err != nil {
		t.Fatalf("checking the overlap: %v", err)
	}
	if overlap != 0 {
		t.Error("signing out moved the live hash into the ledger, so one hash is in both places " +
			"and SHIP-40's invariant no longer holds")
	}
}

// TestSignOutWithoutADatabaseIsUnavailable, for the reason every other path gives: 503 tells a
// mobile client to retry and 500 tells it to give up.
func TestSignOutWithoutADatabaseIsUnavailable(t *testing.T) {
	svc := serviceWithoutADatabase(t)

	if err := svc.SignOut(t.Context(), uuid.New(), uuid.New()); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v, want errUnavailable", err)
	}
}
