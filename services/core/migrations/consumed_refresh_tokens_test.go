package migrations_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
	"github.com/DulsaraNethmin/Shipper/services/core/migrations"
)

// SHIP-40's ledger, checked where its guarantees live.
//
// Every assertion here is about a constraint or an index, so all of them run against a real
// PostgreSQL — Docs/06 §4.1: "a mock happily accepts a write that the actual constraint would
// reject."

func spendToken(t *testing.T, pool *pgxpool.Pool, session uuid.UUID, hash string) {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO consumed_refresh_tokens (id, session_id, token_hash, consumed_at)
		 VALUES ($1, $2, $3, $4)`, id, session, hash, time.Now().UTC()); err != nil {
		t.Fatalf("recording a consumed token: %v", err)
	}
}

// TestAConsumedRefreshTokenHashIsUnique. The hash is what reuse detection looks a session up by,
// so two rows sharing one would make "which session does this reused token belong to" ambiguous
// — and the answer to that question is what gets revoked.
func TestAConsumedRefreshTokenHashIsUnique(t *testing.T) {
	pool := pgtest.DB(t)

	user := newUser(t, pool, "spent@example.com", "+61400000110", "customer")
	first := newSession(t, pool, user, "live-hash-one", "iPhone")
	second := newSession(t, pool, user, "live-hash-two", "Pixel 8")

	spendToken(t, pool, first, "a-spent-hash")

	id, _ := uuid.NewV7()
	_, err := pool.Exec(t.Context(),
		`INSERT INTO consumed_refresh_tokens (id, session_id, token_hash, consumed_at)
		 VALUES ($1, $2, 'a-spent-hash', now())`, id, second)
	if err == nil {
		t.Fatal("one spent token hash was recorded against two sessions")
	}
	if !strings.Contains(err.Error(), "uq_consumed_refresh_tokens_hash") {
		t.Errorf("expected the uniqueness index to refuse it, got: %v", err)
	}
}

// TestAConsumedRefreshTokenNeedsARealSession, and the foreign key is RESTRICT.
//
// The cascade is the dangerous direction here rather than merely the disallowed one: a deleted
// session would take its spent tokens with it, and every token it ever issued would turn back
// into "unknown" — reuse detection silently switched off for that session.
func TestAConsumedRefreshTokenNeedsARealSession(t *testing.T) {
	pool := pgtest.DB(t)

	t.Run("an unknown session is refused", func(t *testing.T) {
		id, _ := uuid.NewV7()
		stranger, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(),
			`INSERT INTO consumed_refresh_tokens (id, session_id, token_hash, consumed_at)
			 VALUES ($1, $2, 'orphan-hash', now())`, id, stranger)
		if err == nil {
			t.Fatal("a spent token was recorded against a session that does not exist")
		}
		if !strings.Contains(err.Error(), "fk_consumed_refresh_tokens_session") {
			t.Errorf("expected the foreign key to refuse it, got: %v", err)
		}
	})

	t.Run("deleting the session is refused while a spent token names it", func(t *testing.T) {
		user := newUser(t, pool, "restrict-spent@example.com", "+61400000111", "provider")
		session := newSession(t, pool, user, "live-hash-restrict", "Pixel 8")
		spendToken(t, pool, session, "spent-hash-restrict")

		_, err := pool.Exec(t.Context(), `DELETE FROM device_sessions WHERE id = $1`, session)
		if err == nil {
			t.Fatal("the session was deleted, taking the evidence that its tokens were spent " +
				"with it — every one of them is now indistinguishable from a token nobody issued")
		}
		if !strings.Contains(err.Error(), "fk_consumed_refresh_tokens_session") {
			t.Errorf("expected ON DELETE RESTRICT to refuse it, got: %v", err)
		}
	})
}

// TestConsumedRefreshTokensAreIndexedBySession is Docs/10 §3.3's foreign key rule, and it is
// also the read path a pruning sweep will take.
func TestConsumedRefreshTokensAreIndexedBySession(t *testing.T) {
	pool := pgtest.DB(t)

	var indexes int
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*)
		FROM pg_index i
		JOIN pg_class t     ON t.oid = i.indrelid
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = i.indkey[0]
		WHERE t.relname = 'consumed_refresh_tokens' AND a.attname = 'session_id'`,
	).Scan(&indexes); err != nil {
		t.Fatalf("looking for the index: %v", err)
	}
	if indexes == 0 {
		t.Error("consumed_refresh_tokens.session_id carries a foreign key and no index leading with it")
	}
}

// TestConsumedRefreshTokenTimestampsCarryTheirZone is Docs/10 §3.3's "timestamptz always".
//
// There is exactly one, and that is deliberate: the row is the record of a consumption, so its
// creation and the consumption are one event. A created_at beside consumed_at would be two
// columns for one instant and two things to keep in step.
func TestConsumedRefreshTokenTimestampsCarryTheirZone(t *testing.T) {
	pool := pgtest.DB(t)

	rows, err := pool.Query(t.Context(), `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'consumed_refresh_tokens'
		  AND data_type LIKE 'timestamp%'
		ORDER BY column_name`)
	if err != nil {
		t.Fatalf("reading the column types: %v", err)
	}
	defer rows.Close()

	found := 0
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		found++
		if dataType != "timestamp with time zone" {
			t.Errorf("consumed_refresh_tokens.%s is %s, want timestamp with time zone", name, dataType)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}
	if found != 1 {
		t.Errorf("found %d timestamp columns, want 1 (consumed_at)", found)
	}
}

// TestDeviceSessionRevocationColumnsMoveTogether. Without the paired CHECK a row could say it
// was revoked for no reason, or carry a reason while still being live — and the refresh path
// reads revoked_at, so the second of those is a session that is over according to the device
// list and usable according to the platform.
func TestDeviceSessionRevocationColumnsMoveTogether(t *testing.T) {
	pool := pgtest.DB(t)

	user := newUser(t, pool, "revoke-pair@example.com", "+61400000112", "customer")
	session := newSession(t, pool, user, "live-hash-pair", "iPhone")

	for name, set := range map[string]string{
		"an instant with no reason": `revoked_at = now()`,
		"a reason with no instant":  `revoked_reason = 'signed_out'`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := pool.Exec(t.Context(),
				`UPDATE device_sessions SET `+set+` WHERE id = $1`, session)
			if err == nil {
				t.Fatal("a half-revoked session was accepted")
			}
			if !strings.Contains(err.Error(), "ck_device_sessions_revoked") {
				t.Errorf("expected ck_device_sessions_revoked to refuse it, got: %v", err)
			}
		})
	}

	t.Run("both together are accepted", func(t *testing.T) {
		if _, err := pool.Exec(t.Context(),
			`UPDATE device_sessions SET revoked_at = now(), revoked_reason = 'signed_out'
			  WHERE id = $1`, session); err != nil {
			t.Errorf("a properly revoked session was refused: %v", err)
		}
	})
}

// TestDeviceSessionRevokedReasonIsConstrained. Docs/10 §3.4 chooses text-plus-CHECK over an enum
// type, and the trade is that the constraint has to be tested rather than assumed from the type.
func TestDeviceSessionRevokedReasonIsConstrained(t *testing.T) {
	pool := pgtest.DB(t)

	user := newUser(t, pool, "revoke-reason@example.com", "+61400000113", "customer")
	session := newSession(t, pool, user, "live-hash-reason", "iPhone")

	_, err := pool.Exec(t.Context(),
		`UPDATE device_sessions SET revoked_at = now(), revoked_reason = 'because'
		  WHERE id = $1`, session)
	if err == nil {
		t.Fatal("an unrecognised revocation reason was accepted")
	}
	if !strings.Contains(err.Error(), "ck_device_sessions_revoked_reason") {
		t.Errorf("expected ck_device_sessions_revoked_reason to refuse it, got: %v", err)
	}
}

// TestConsumedRefreshTokensMigrationIsInTheIdentityBlock names the number, for the reason
// device_sessions' equivalent gives: the block scheme is only worth having if something checks
// that a migration landed inside its range.
func TestConsumedRefreshTokensMigrationIsInTheIdentityBlock(t *testing.T) {
	block, ok := migrations.BlockContaining(104)
	if !ok {
		t.Fatal("version 104 falls in no reserved block")
	}
	if block.Domain != "identity" {
		t.Errorf("version 104 is in the %q block, want identity", block.Domain)
	}
}
