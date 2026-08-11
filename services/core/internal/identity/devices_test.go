package identity

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SHIP-43 and SHIP-46 against a real PostgreSQL, per Docs/06 §4.1.
//
// Both criteria are claims about which rows changed and which did not, and in both the half a
// plausible implementation gets wrong is the second one. Revoking everything the account owns
// looks correct from the device that asked and is visible only from the phone in the other
// pocket; a device list that quietly included another account's sessions looks entirely ordinary
// in a single-account test.

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

// SHIP-46 against a real PostgreSQL.
//
// A device list is a claim about which rows are returned and which are left out, and the leaving
// out is the part that matters: a list that quietly included another account's sessions would look
// entirely ordinary in a single-account test.

// deviceIDs is the list's identifiers, in the order it returned them.
func deviceIDs(devices []Device) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(devices))
	for _, d := range devices {
		ids = append(ids, d.ID)
	}
	return ids
}

// TestDevicesListsTheLiveSessionsAndMarksTheCurrentOne is SHIP-46's first half.
func TestDevicesListsTheLiveSessionsAndMarksTheCurrentOne(t *testing.T) {
	svc, pool, user := signInService(t, testProfile)

	phone := signInAs(t, svc, "Nethmin's iPhone")
	tablet := signInAs(t, svc, "Nethmin's iPad")
	laptop := signInAs(t, svc, "Nethmin's Pixel")

	devices, truncated, err := svc.Devices(t.Context(), user.ID, tablet.SessionID)
	if err != nil {
		t.Fatalf("listing devices: %v", err)
	}
	if truncated {
		t.Error("three devices reported as truncated")
	}

	t.Run("every live session is there, and nothing else", func(t *testing.T) {
		got := map[uuid.UUID]bool{}
		for _, id := range deviceIDs(devices) {
			got[id] = true
		}
		for _, want := range []uuid.UUID{phone.SessionID, tablet.SessionID, laptop.SessionID} {
			if !got[want] {
				t.Errorf("the list does not name session %s", want)
			}
		}
		if len(devices) != 3 {
			t.Errorf("%d devices listed, want 3", len(devices))
		}
	})

	t.Run("exactly one row is the current one, and it is the caller's", func(t *testing.T) {
		var current []uuid.UUID
		for _, d := range devices {
			if d.Current {
				current = append(current, d.ID)
			}
		}
		if len(current) != 1 || current[0] != tablet.SessionID {
			t.Errorf("current = %v, want exactly [%s]", current, tablet.SessionID)
		}
	})

	t.Run("the label the device signed in with is what is shown", func(t *testing.T) {
		labels := map[uuid.UUID]string{}
		for _, d := range devices {
			labels[d.ID] = d.DeviceLabel
		}
		if labels[phone.SessionID] != "Nethmin's iPhone" {
			t.Errorf("label = %q, want the one sent at sign-in", labels[phone.SessionID])
		}
	})

	t.Run("reading the list does not move last_seen_at", func(t *testing.T) {
		var before string
		if err := pool.QueryRow(t.Context(),
			`SELECT last_seen_at::text FROM device_sessions WHERE id = $1`,
			phone.SessionID).Scan(&before); err != nil {
			t.Fatalf("reading last_seen_at: %v", err)
		}

		if _, _, err := svc.Devices(t.Context(), user.ID, tablet.SessionID); err != nil {
			t.Fatalf("listing devices: %v", err)
		}

		var after string
		if err := pool.QueryRow(t.Context(),
			`SELECT last_seen_at::text FROM device_sessions WHERE id = $1`,
			phone.SessionID).Scan(&after); err != nil {
			t.Fatalf("reading last_seen_at: %v", err)
		}
		if after != before {
			t.Errorf("last_seen_at moved from %s to %s on a read.\n"+
				"000103's header is explicit that nothing derived from this column may extend a "+
				"credential, and a display column every read writes to stops meaning anything.",
				before, after)
		}
	})
}

// TestDevicesLeavesOutWhatCannotActAsTheAccount. The list answers "which devices can act as me,
// and let me stop one"; a revoked or lapsed session can do neither.
func TestDevicesLeavesOutWhatCannotActAsTheAccount(t *testing.T) {
	svc, pool, user := signInService(t, testProfile)

	live := signInAs(t, svc, "Live")
	revoked := signInAs(t, svc, "Revoked")
	lapsed := signInAs(t, svc, "Lapsed")

	if err := svc.SignOut(t.Context(), user.ID, revoked.SessionID); err != nil {
		t.Fatalf("signing out: %v", err)
	}
	// created_at moves with it: ck_device_sessions_refresh_expiry refuses a token that expired
	// before it was issued, which is the constraint 000103 exists for.
	if _, err := pool.Exec(t.Context(), `
		UPDATE device_sessions
		   SET created_at = now() - interval '31 days',
		       refresh_token_expires_at = now() - interval '1 second'
		 WHERE id = $1`, lapsed.SessionID); err != nil {
		t.Fatalf("lapsing a session: %v", err)
	}

	devices, _, err := svc.Devices(t.Context(), user.ID, live.SessionID)
	if err != nil {
		t.Fatalf("listing devices: %v", err)
	}

	if len(devices) != 1 || devices[0].ID != live.SessionID {
		t.Errorf("the list is %v, want only the live session %s", deviceIDs(devices), live.SessionID)
	}
}

// TestDevicesListsNobodyElsesSessions.
//
// Mutation-checked: removing `user_id = $1` from liveDeviceSessionsByUser makes this fail. That is
// the failure a single-account test cannot see, and it hands one account's device list to another.
func TestDevicesListsNobodyElsesSessions(t *testing.T) {
	svc, _, user := signInService(t, testProfile)

	mine := signInAs(t, svc, "Mine")

	stranger, err := svc.Register(t.Context(), RegisterCommand{
		Email:    "stranger@example.com",
		Phone:    "0412 345 679",
		Password: "correct-horse-battery-staple",
		Role:     RoleProvider,
	})
	if err != nil {
		t.Fatalf("registering the stranger: %v", err)
	}
	if _, err := svc.startSession(t.Context(), pgtestPool(t, svc), stranger, "Not Yours"); err != nil {
		t.Fatalf("starting the stranger's session: %v", err)
	}

	devices, _, err := svc.Devices(t.Context(), user.ID, mine.SessionID)
	if err != nil {
		t.Fatalf("listing devices: %v", err)
	}

	for _, d := range devices {
		if d.DeviceLabel == "Not Yours" {
			t.Fatalf("the list carries another account's session %s", d.ID)
		}
	}
	if len(devices) != 1 {
		t.Errorf("%d devices listed, want 1", len(devices))
	}
}

// TestDevicesBoundsTheResponse. Every sign-in creates a session and nothing stops a client signing
// in repeatedly instead of refreshing, so the response has to be bounded whatever the account has
// done to itself. The rows are planted directly: what is under test is the query's bound, and a
// hundred argon2id derivations would be a slow way of not testing it.
func TestDevicesBoundsTheResponse(t *testing.T) {
	svc, pool, user := signInService(t, testProfile)

	current := signInAs(t, svc, "Current")

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO device_sessions
		    (id, user_id, refresh_token_hash, refresh_token_expires_at, device_label)
		SELECT gen_random_uuid(), $1, 'bound-' || n, now() + interval '30 days', 'Device ' || n
		  FROM generate_series(1, $2) AS n`, user.ID, maxDevicesListed); err != nil {
		t.Fatalf("planting sessions: %v", err)
	}

	devices, truncated, err := svc.Devices(t.Context(), user.ID, current.SessionID)
	if err != nil {
		t.Fatalf("listing devices: %v", err)
	}

	if len(devices) != maxDevicesListed {
		t.Errorf("%d devices returned, want the bound of %d", len(devices), maxDevicesListed)
	}
	if !truncated {
		t.Error("the list was truncated and has_more would have said otherwise, which is the " +
			"one thing a caller in that state can act on")
	}
}

// TestRevokeDeviceEndsTheNamedSessionAndNoOther is SHIP-46's second half.
func TestRevokeDeviceEndsTheNamedSessionAndNoOther(t *testing.T) {
	svc, pool, user := signInService(t, testProfile)

	doomed := signInAs(t, svc, "Lost In A Taxi")
	kept := signInAs(t, svc, "Still Mine")

	if err := svc.RevokeDevice(t.Context(), user.ID, doomed.SessionID); err != nil {
		t.Fatalf("revoking: %v", err)
	}

	t.Run("the reason distinguishes it from a sign-out", func(t *testing.T) {
		if got := revocationOf(t, pool, doomed.SessionID); got != revokedReasonByOwner {
			t.Errorf("the session is %q, want %q — a support conversation asks which of the "+
				"two happened", got, revokedReasonByOwner)
		}
	})

	t.Run("its refresh token stops working", func(t *testing.T) {
		if _, err := svc.Refresh(t.Context(), doomed.Refresh.Value); !errors.Is(err, ErrRefreshTokenInvalid) {
			t.Errorf("err = %v, want ErrRefreshTokenInvalid", err)
		}
	})

	t.Run("it leaves the list, and the others stay", func(t *testing.T) {
		devices, _, err := svc.Devices(t.Context(), user.ID, kept.SessionID)
		if err != nil {
			t.Fatalf("listing devices: %v", err)
		}
		if len(devices) != 1 || devices[0].ID != kept.SessionID {
			t.Errorf("the list is %v, want only %s", deviceIDs(devices), kept.SessionID)
		}
	})
}

// TestRevokeDeviceRefusesASessionThatIsNotTheCallers, and says the same thing about one that does
// not exist.
//
// Mutation-checked: removing `user_id = $2` from deviceSessionOwnedBy makes the first subtest fail
// — one account signing another's devices out, from a request that looks entirely ordinary.
func TestRevokeDeviceRefusesASessionThatIsNotTheCallers(t *testing.T) {
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

	t.Run("another account's session", func(t *testing.T) {
		err := svc.RevokeDevice(t.Context(), stranger.ID, victim.SessionID)
		if !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("err = %v, want ErrSessionNotFound", err)
		}
		if got := revocationOf(t, pool, victim.SessionID); got != "live" {
			t.Errorf("the session is %q, want live — it was ended by somebody who does not own it", got)
		}
	})

	t.Run("a session that does not exist answers identically", func(t *testing.T) {
		if err := svc.RevokeDevice(t.Context(), stranger.ID, uuid.New()); !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("err = %v, want ErrSessionNotFound — telling the two apart makes this a "+
				"way of finding out which identifiers name real sessions", err)
		}
	})
}

// TestRevokeDeviceIsSafeToRepeat. The caller asked for a state and the state holds; an error would
// make the honest retry the idempotency middleware exists for look like a mistake.
func TestRevokeDeviceIsSafeToRepeat(t *testing.T) {
	svc, _, user := signInService(t, testProfile)
	phone := signInAs(t, svc, "Nethmin's iPhone")

	if err := svc.RevokeDevice(t.Context(), user.ID, phone.SessionID); err != nil {
		t.Fatalf("first revoke: %v", err)
	}
	if err := svc.RevokeDevice(t.Context(), user.ID, phone.SessionID); err != nil {
		t.Errorf("second revoke: %v — a session already in the state asked for is not a failure", err)
	}
}

// TestRevokeDeviceAcceptsTheCurrentSession. It is the same action as signing out, and refusing it
// would produce a device list where exactly one row cannot be acted on. What differs is the reason
// recorded, which is the point of having two.
func TestRevokeDeviceAcceptsTheCurrentSession(t *testing.T) {
	svc, pool, user := signInService(t, testProfile)
	phone := signInAs(t, svc, "Nethmin's iPhone")

	if err := svc.RevokeDevice(t.Context(), user.ID, phone.SessionID); err != nil {
		t.Fatalf("revoking the current session: %v", err)
	}
	if got := revocationOf(t, pool, phone.SessionID); got != revokedReasonByOwner {
		t.Errorf("the session is %q, want %q", got, revokedReasonByOwner)
	}
}

// TestDeviceListingWithoutADatabaseIsUnavailable, both endpoints.
func TestDeviceListingWithoutADatabaseIsUnavailable(t *testing.T) {
	svc := serviceWithoutADatabase(t)

	if _, _, err := svc.Devices(t.Context(), uuid.New(), uuid.New()); !errors.Is(err, errUnavailable) {
		t.Errorf("Devices: err = %v, want errUnavailable", err)
	}
	if err := svc.RevokeDevice(t.Context(), uuid.New(), uuid.New()); !errors.Is(err, errUnavailable) {
		t.Errorf("RevokeDevice: err = %v, want errUnavailable", err)
	}
}

// pgtestPool reaches the pool the service was built over, for the one test that has to write on
// behalf of an account it is not signing in as.
func pgtestPool(t *testing.T, svc *Service) *pgxpool.Pool {
	t.Helper()

	if svc.pool == nil {
		t.Fatal("the service has no pool")
	}
	return svc.pool
}
