package notifications

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-140's device registry and SHIP-139's rejection path, against a real PostgreSQL.
//
// Real for the reason the rest of this package is: the two partial unique indexes in 000701 are the
// whole of what makes registration idempotent and what stops one token being live in two places, so
// a test against a fake would be testing the fake.

// stubSessions answers which device sessions are live without an identity table.
//
// The real implementation is cmd/notifier's deviceSessionLookup and reads `device_sessions`, which
// is another domain's table; the port exists precisely so this package does not have to know that.
// What the stub cannot show is that the query is right, which is why the verify section signs a
// device out for real and watches push delivery stop.
type stubSessions struct {
	revoked map[uuid.UUID]bool
	calls   int
}

func (s *stubSessions) LiveSessions(
	_ context.Context, _ db.Runner, ids []uuid.UUID,
) (map[uuid.UUID]bool, error) {
	s.calls++
	live := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		if !s.revoked[id] {
			live[id] = true
		}
	}
	return live, nil
}

// newSession inserts a signed-in device for an account.
//
// device_sessions is identity's table and this package may not import identity, so the row is
// written with SQL — which is also what the harness does. The columns are 000100's plus 000103's
// expiry, which is NOT NULL with no default on purpose.
func newSession(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, label string) uuid.UUID {
	t.Helper()

	id := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO device_sessions
		    (id, user_id, refresh_token_hash, device_label, refresh_token_expires_at)
		VALUES ($1, $2, $3, $4, now() + interval '30 days')`,
		id, userID, uuid.NewString(), label); err != nil {
		t.Fatalf("inserting the device session %q: %v", label, err)
	}
	return id
}

// register runs one registration in a transaction, which is what RegisterDevice requires.
func register(
	t *testing.T, pool *pgxpool.Pool, s *Service, userID, sessionID uuid.UUID, platform Platform, token string,
) (DeviceToken, error) {
	t.Helper()

	var out DeviceToken
	err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var err error
		out, err = s.RegisterDevice(ctx, r, userID, sessionID, platform, token)
		return err
	})
	return out, err
}

// liveTokens is every token still addressable for an account, oldest first.
func liveTokens(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) []string {
	t.Helper()

	rows, err := pool.Query(t.Context(), `
		SELECT token FROM device_tokens
		WHERE user_id = $1 AND revoked_at IS NULL
		ORDER BY registered_at, id`, userID)
	if err != nil {
		t.Fatalf("reading the live tokens of %s: %v", userID, err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			t.Fatalf("scanning a token: %v", err)
		}
		out = append(out, token)
	}
	return out
}

// revocationOf is a token's revoked_reason, or "" while it is live.
func revocationOf(t *testing.T, pool *pgxpool.Pool, token string) string {
	t.Helper()

	var reason *string
	if err := pool.QueryRow(t.Context(),
		`SELECT revoked_reason FROM device_tokens WHERE token = $1`, token).Scan(&reason); err != nil {
		t.Fatalf("reading the revocation of %q: %v", token, err)
	}
	if reason == nil {
		return ""
	}
	return *reason
}

func deviceService(sessions Sessions) *Service {
	opts := []Option{}
	if sessions != nil {
		opts = append(opts, WithSessions(sessions))
	}
	return NewService(&stubParties{}, clock.NewFixed(testInstant), Senders{}, opts...)
}

// TestRegisteringTheSameDeviceTwiceLeavesOneLiveToken is the first half of SHIP-140's *Done when*:
// a token binds to a device session.
//
// SHIP-143 registers at every launch, because FCM hands the app a token each time and only
// sometimes the same one. So the ordinary case is the second registration, not the first.
func TestRegisteringTheSameDeviceTwiceLeavesOneLiveToken(t *testing.T) {
	pool := pgtest.DB(t)

	user := newUser(t, pool, "device-once@example.com", "+61400007201", "customer")
	session := newSession(t, pool, user, "Nethmin's iPhone")
	service := deviceService(nil)

	if _, err := register(t, pool, service, user, session, PlatformIOS, "token-first"); err != nil {
		t.Fatalf("the first registration: %v", err)
	}
	if _, err := register(t, pool, service, user, session, PlatformIOS, "token-second"); err != nil {
		t.Fatalf("the second registration: %v", err)
	}

	live := liveTokens(t, pool, user)
	if len(live) != 1 || live[0] != "token-second" {
		t.Fatalf("the account has %v live, want only token-second — a handset addressed at two "+
			"tokens is a notification delivered twice", live)
	}
	if got := revocationOf(t, pool, "token-first"); got != RevokedReplaced {
		t.Errorf("the displaced token is revoked %q, want %q", got, RevokedReplaced)
	}
}

// TestATokenIsLiveInOnePlaceOnly. FCM reissues a token to a handset restored from another device's
// backup, so the same string can legitimately arrive from a second session — and the platform must
// then address one device, not two. Without uq_device_tokens_live_token a push meant for the
// previous owner reaches the new one.
func TestATokenIsLiveInOnePlaceOnly(t *testing.T) {
	pool := pgtest.DB(t)

	previousOwner := newUser(t, pool, "device-old@example.com", "+61400007202", "customer")
	newOwner := newUser(t, pool, "device-new@example.com", "+61400007203", "customer")
	oldSession := newSession(t, pool, previousOwner, "the old phone")
	newSession := newSession(t, pool, newOwner, "the restored phone")
	service := deviceService(nil)

	if _, err := register(t, pool, service, previousOwner, oldSession, PlatformIOS, "restored-token"); err != nil {
		t.Fatalf("registering the previous owner: %v", err)
	}
	if _, err := register(t, pool, service, newOwner, newSession, PlatformIOS, "restored-token"); err != nil {
		t.Fatalf("registering the new owner: %v", err)
	}

	if live := liveTokens(t, pool, previousOwner); len(live) != 0 {
		t.Errorf("the previous owner still holds %v; a push for them would reach the new "+
			"owner's handset", live)
	}
	if live := liveTokens(t, pool, newOwner); len(live) != 1 {
		t.Errorf("the new owner holds %v, want one", live)
	}
}

// TestDeregisteringEndsDeliveryAndSaysWhetherThereWasAny.
func TestDeregisteringEndsDeliveryAndSaysWhetherThereWasAny(t *testing.T) {
	pool := pgtest.DB(t)

	user := newUser(t, pool, "device-out@example.com", "+61400007204", "customer")
	session := newSession(t, pool, user, "a phone")
	service := deviceService(nil)

	if _, err := register(t, pool, service, user, session, PlatformAndroid, "signing-out"); err != nil {
		t.Fatalf("registering: %v", err)
	}

	var was, again bool
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var err error
		if was, err = service.DeregisterDevice(ctx, r, session); err != nil {
			return err
		}
		again, err = service.DeregisterDevice(ctx, r, session)
		return err
	}); err != nil {
		t.Fatalf("deregistering: %v", err)
	}

	if !was {
		t.Error("deregistering a live token reported that there was none")
	}
	if again {
		t.Error("deregistering twice reported a second live token")
	}
	if live := liveTokens(t, pool, user); len(live) != 0 {
		t.Errorf("the account still holds %v after signing out", live)
	}
	if got := revocationOf(t, pool, "signing-out"); got != RevokedDeregistered {
		t.Errorf("the token is revoked %q, want %q", got, RevokedDeregistered)
	}
}

// TestAPushRowIsWrittenPerLiveHandset is the second half of the chain: SHIP-139's adapter and
// SHIP-140's tokens make [ChannelPush] a channel with addresses, and one person signed in on two
// devices is two rows.
func TestAPushRowIsWrittenPerLiveHandset(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newUser(t, pool, "two-handsets@example.com", "+61400007205", "customer")
	phone := newSession(t, pool, customer, "phone")
	tablet := newSession(t, pool, customer, "tablet")

	sessions := &stubSessions{}
	service := deviceService(sessions)

	for session, token := range map[uuid.UUID]string{phone: "phone-token", tablet: "tablet-token"} {
		if _, err := register(t, pool, service, customer, session, PlatformIOS, token); err != nil {
			t.Fatalf("registering %s: %v", token, err)
		}
	}

	job := uuid.Must(uuid.NewV7())
	env := envelopeOf(t, "bid.placed", map[string]any{
		"job_id":      job.String(),
		"provider_id": uuid.Must(uuid.NewV7()).String(),
	})
	service.parties = &stubParties{customer: customer}

	written, err := consume(t, pool, service, env)
	if err != nil {
		t.Fatalf("consuming: %v", err)
	}

	// One email and one push per handset.
	if written != 3 {
		t.Fatalf("the event wrote %d rows, want 3: one email and one push per signed-in "+
			"handset", written)
	}

	addresses := map[Channel][]string{}
	for _, row := range rowsFor(t, pool, env.ID) {
		addresses[row.Channel] = append(addresses[row.Channel], row.Address)
	}
	if len(addresses[ChannelPush]) != 2 {
		t.Errorf("push went to %v, want both handsets", addresses[ChannelPush])
	}
	if len(addresses[ChannelEmail]) != 1 {
		t.Errorf("email went to %v, want one address", addresses[ChannelEmail])
	}
	if sessions.calls == 0 {
		t.Error("no device session was checked for liveness; a signed-out handset would have " +
			"been addressed")
	}
}

// TestSigningOutStopsPushWithNothingWritingToDeviceTokens is the *Done when*'s second clause, and
// the design decision behind SHIP-140.
//
// 000104 revokes a device session rather than deleting it, so a cascade would never fire. What
// clears the token is that nothing resolves a push address for a session that is not live — which
// means the moment the revocation commits, delivery has stopped, with no cross-domain write and
// nothing in `device_tokens` having changed.
func TestSigningOutStopsPushWithNothingWritingToDeviceTokens(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newUser(t, pool, "signed-out@example.com", "+61400007206", "customer")
	session := newSession(t, pool, customer, "the handset they signed out of")

	sessions := &stubSessions{revoked: map[uuid.UUID]bool{}}
	service := deviceService(sessions)
	service.parties = &stubParties{customer: customer}

	if _, err := register(t, pool, service, customer, session, PlatformAndroid, "still-registered"); err != nil {
		t.Fatalf("registering: %v", err)
	}

	// Signed out. Identity marks the session revoked and touches nothing here.
	sessions.revoked[session] = true

	env := envelopeOf(t, "bid.placed", map[string]any{
		"job_id":      uuid.Must(uuid.NewV7()).String(),
		"provider_id": uuid.Must(uuid.NewV7()).String(),
	})
	if _, err := consume(t, pool, service, env); err != nil {
		t.Fatalf("consuming: %v", err)
	}

	for _, row := range rowsFor(t, pool, env.ID) {
		if row.Channel == ChannelPush {
			t.Fatalf("a push was addressed to %q after the device signed out", row.Address)
		}
	}

	// And the row is untouched, which is the half that says no cross-domain write happened.
	if live := liveTokens(t, pool, customer); len(live) != 1 {
		t.Errorf("the device token row is %v; signing out should have changed nothing here", live)
	}
}

// TestWithoutASessionsPortNoPushIsAddressed holds [WithSessions]'s default in the safe direction.
//
// A process that cannot tell a live device session from a signed-out one addresses none. The
// failure this prevents is the opposite default: pushing to every registered handset, including
// those whose owner signed out weeks ago.
func TestWithoutASessionsPortNoPushIsAddressed(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newUser(t, pool, "no-sessions-port@example.com", "+61400007207", "customer")
	session := newSession(t, pool, customer, "a phone")

	service := deviceService(nil)
	service.parties = &stubParties{customer: customer}

	if _, err := register(t, pool, service, customer, session, PlatformIOS, "unresolvable"); err != nil {
		t.Fatalf("registering: %v", err)
	}

	env := envelopeOf(t, "bid.placed", map[string]any{
		"job_id":      uuid.Must(uuid.NewV7()).String(),
		"provider_id": uuid.Must(uuid.NewV7()).String(),
	})
	if _, err := consume(t, pool, service, env); err != nil {
		t.Fatalf("consuming: %v", err)
	}

	for _, row := range rowsFor(t, pool, env.ID) {
		if row.Channel == ChannelPush {
			t.Fatal("a push was addressed by a service with no Sessions port")
		}
	}
}

// TestARegistrationMustBeInATransaction. Revoking what the handset had and inserting what it now
// has are one change: committed apart, a crash between them leaves a device with no live token and
// nothing to say so.
func TestARegistrationMustBeInATransaction(t *testing.T) {
	pool := pgtest.DB(t)

	user := newUser(t, pool, "device-no-tx@example.com", "+61400007208", "customer")
	session := newSession(t, pool, user, "a phone")

	_, err := deviceService(nil).RegisterDevice(t.Context(), pool, user, session, PlatformIOS, "loose")
	if err == nil {
		t.Fatal("a registration outside a transaction was accepted")
	}
}

// TestARejectedTokenIsDeregisteredAndTheRowIsTerminal is the ticket's central behaviour, and the
// one the Docs/11 §3 mutation breaks from the adapter's side.
//
// internal/platform/push/doc.go: a rejected token means "deregister this device", and treating it
// as a dispatch failure produces an alert that fires forever. Here that is two facts committed
// together — the device stops being addressed, and the row becomes `undeliverable` rather than
// staying claimable for the rest of the platform's life.
func TestARejectedTokenIsDeregisteredAndTheRowIsTerminal(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newUser(t, pool, "uninstalled@example.com", "+61400007209", "customer")
	session := newSession(t, pool, customer, "an uninstalled app")

	pusher := &rejectingPusher{}
	service := NewService(&stubParties{customer: customer}, clock.NewFixed(testInstant),
		Senders{Push: pusher}, WithSessions(&stubSessions{}))

	if _, err := register(t, pool, service, customer, session, PlatformIOS, "dead-token"); err != nil {
		t.Fatalf("registering: %v", err)
	}

	id := pendingPush(t, pool, customer, "dead-token")

	var claimed int
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var err error
		claimed, err = service.Dispatch(ctx, r)
		return err
	}); err != nil {
		t.Fatalf("the pass failed on a rejection, which is exactly what doc.go says it must "+
			"not do: %v", err)
	}
	if claimed != 1 {
		t.Fatalf("the pass claimed %d rows, want 1", claimed)
	}

	var status string
	var attempts int
	var sentAt *time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT status, attempts, sent_at FROM notifications WHERE id = $1`, id).
		Scan(&status, &attempts, &sentAt); err != nil {
		t.Fatalf("reading the row back: %v", err)
	}

	if status != "undeliverable" {
		t.Errorf("a rejected push left the row %q; `failed` is claimed again on every pass "+
			"forever and `sent` is untrue", status)
	}
	if sentAt != nil {
		t.Error("the row carries a sent instant for a message that was never delivered")
	}
	if attempts != 1 {
		t.Errorf("attempts is %d, want 1", attempts)
	}
	if got := revocationOf(t, pool, "dead-token"); got != RevokedRejected {
		t.Errorf("the rejected token is revoked %q, want %q — nothing would stop addressing "+
			"the handset", got, RevokedRejected)
	}

	// And the row is not claimed again, which is the whole point of a terminal status.
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var err error
		claimed, err = service.Dispatch(ctx, r)
		return err
	}); err != nil {
		t.Fatalf("the second pass: %v", err)
	}
	if claimed != 0 {
		t.Errorf("the second pass claimed %d rows; an undeliverable notification is retried "+
			"against a handset that no longer exists", claimed)
	}
}

// rejectingPusher is the adapter answering the way FCM answers for an uninstalled app.
type rejectingPusher struct{ calls int }

func (p *rejectingPusher) Push(context.Context, string, string, string, uuid.UUID) (bool, error) {
	p.calls++
	return true, nil
}

// pendingPush writes one push notification row directly, so that the dispatcher has something to
// claim without a whole consume pass.
func pendingPush(t *testing.T, pool *pgxpool.Pool, recipient uuid.UUID, address string) uuid.UUID {
	t.Helper()

	id := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO notifications
		    (id, event_id, event_type, job_id, recipient_id, channel, category, essential,
		     address, subject, body)
		VALUES ($1, $2, 'bid.placed', $3, $4, 'push', 'bidding', true, $5, 'subject', 'body')`,
		id, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), recipient, address); err != nil {
		t.Fatalf("inserting a pending push: %v", err)
	}
	return id
}
