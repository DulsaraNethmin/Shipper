-- SHIP-39: a refresh token that expires, and the column that says when.
--
-- # The decision this migration settles
--
-- Docs/11 §9 has carried "device_sessions has no expiry column" since SHIP-38, and it comes due
-- here: rotation is meaningless without a lifetime, because a token that never expires is a
-- credential a stolen phone keeps forever. The three places expiry could have lived, and why it
-- lives here:
--
--   * **In Redis, as a key TTL.** Refused by Docs/10 §5. Redis may hold a denylist as a fast
--     path, but the guarantee that a session ends is a security control, and a security control
--     a cache flush can undo is not one. That paragraph amends Docs/06 §2.1 on exactly this
--     point, and it is the reason device_sessions exists in PostgreSQL at all.
--
--   * **Derived from last_seen_at plus a constant, with no new column.** This is the tempting
--     one, because it needs no migration. It is refused because last_seen_at is a *display*
--     column: 000100 describes it as what the device list shows beside a phone nobody
--     recognises, and SHIP-46 will write it from a device list read. The moment a lifetime is
--     derived from it, every future write to a display column silently extends a credential —
--     a coupling nothing would fail on and nobody would see.
--
--   * **Here, as an explicit column.** Chosen. An expiry that is written is an expiry that can
--     be read, indexed, and audited, and a session whose refresh token has lapsed is
--     answerable by a query rather than by arithmetic somebody has to remember to do.
--
-- # Why it is the *token's* expiry and not the *session's*
--
-- The name is doing work. Rotation issues a new refresh token on every use (SHIP-39) and this
-- column moves with it, so the window is a sliding one: thirty days of inactivity ends the
-- session, and a device in daily use never sees it. Docs/07 §3 asks for a "longer-lived" token
-- rotated on every use, and a sliding window is what that means.
--
-- **An absolute session cap — sign every device out N days after sign-in regardless of use —
-- is deliberately not added.** It is a second control with a product consequence (every user
-- signed out on a schedule, including a driver mid-delivery), it is a policy decision rather
-- than a mechanism, and adding a column nothing writes to would be guessing at a design that
-- has not been argued. If it is wanted later it is another column and another migration, and
-- this one does not stand in its way.
--
-- # Why NOT NULL with no default
--
-- A session row with no expiry is a credential that never lapses, which is the whole defect
-- this column exists to prevent — so the schema refuses to hold one. There is no DEFAULT for
-- the same reason insertEmailToken writes created_at explicitly: expires_at is computed in Go
-- from the injected clock, and a database-side default would come from a second clock that can
-- disagree with it under ordinary skew.

-- Added nullable, backfilled, then constrained, so that the migration applies to a database
-- that already holds sessions. ADD COLUMN ... NOT NULL with no default fails outright on a
-- non-empty table, and a DEFAULT added only to get past that would linger as the safety net
-- the paragraph above rejects.
ALTER TABLE device_sessions
    ADD COLUMN refresh_token_expires_at timestamptz;

-- Existing rows are development leftovers — this table has never been written by the service,
-- because the endpoints that issue a refresh token are SHIP-41 and SHIP-42. Deriving their
-- expiry from created_at is the honest reading: the token they hold was issued then.
UPDATE device_sessions
   SET refresh_token_expires_at = created_at + interval '30 days'
 WHERE refresh_token_expires_at IS NULL;

ALTER TABLE device_sessions
    ALTER COLUMN refresh_token_expires_at SET NOT NULL;

-- An expiry at or before the moment of issue is a token nobody can ever use: a clock or
-- configuration mistake, worth catching where it happens rather than as a sign-in loop.
-- Compared against created_at rather than now(), because the row is rewritten on every
-- rotation and a now() comparison would refuse any correction of a historic row.
ALTER TABLE device_sessions
    ADD CONSTRAINT ck_device_sessions_refresh_expiry
        CHECK (refresh_token_expires_at > created_at);

COMMENT ON COLUMN device_sessions.refresh_token_expires_at IS
    'When the refresh token currently in refresh_token_hash stops being usable. Rewritten on every rotation, so the window slides (SHIP-39).';
