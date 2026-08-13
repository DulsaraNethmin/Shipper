-- SHIP-40: reuse detection — a spent refresh token, and a session that can end.
--
-- Docs/07 §3: "A refresh token presented twice invalidates the whole device session." SHIP-39
-- rotates, so the token presented stops matching the session row the moment the exchange
-- commits — but a token that matches nothing is indistinguishable from one this platform never
-- issued, and the two must be answered differently. One is somebody guessing. The other is a
-- token that was genuinely issued, has been used, and is being presented again: either the
-- device replayed it, or somebody else has a copy. Neither is safe to keep the session for.
--
-- # Why a spent-token ledger, and not one more column on device_sessions
--
-- The obvious cheap design is to keep the *previous* hash beside the current one and call a
-- match on it reuse. It satisfies the acceptance criterion for exactly one generation and
-- silently stops satisfying it after two: a token stolen and then left while the legitimate
-- device refreshes twice matches neither column, is answered "unknown", and the session
-- survives. The criterion is "presenting a consumed token invalidates the entire device
-- session", not "presenting the most recently consumed one", so every consumed token has to
-- stay recognisable for as long as the session it belongs to is alive.
--
-- # The invariant this table is shaped around
--
-- **A refresh token hash lives in device_sessions.refresh_token_hash or in this table, never
-- both.** Rotation moves it across in one transaction: the spent hash is written here and the
-- session takes the new one. That is why there is no `live` flag and no `consumed_at IS NULL`
-- partial index — unlike email_verification_tokens, which holds both states in one table,
-- because there the live row is the record and here the live row already exists next door.
--
-- Revoking a session deliberately does **not** move its live hash here. Refresh checks
-- revoked_at before it checks anything else, so the token buys nothing either way, and moving it
-- would put one hash in both places and cost the invariant its whole value.
--
-- # Why rows are only ever inserted
--
-- The table is a ledger. Nothing updates a row, so there is no updated_at and no trigger, and
-- there is one timestamp rather than a created_at beside it: the row *is* the record of a
-- consumption, so its creation and the consumption are one event, and two columns for one
-- instant is two things to keep in step.
--
-- Rows are not pruned today, and a session in daily use writes roughly one every fifteen
-- minutes of activity. Deleting the spent tokens of a session that has ended is safe — the
-- session refuses every token it ever issued once revoked_at is set — and belongs to cmd/worker
-- (SHIP-67a) as its own ticket rather than being smuggled in here.

CREATE TABLE consumed_refresh_tokens (
    id          uuid        PRIMARY KEY,

    session_id  uuid        NOT NULL,

    -- The SHA-256 of a refresh token that has been rotated away, hex encoded, exactly as
    -- device_sessions.refresh_token_hash holds a live one. The token itself was never stored
    -- and is not stored here either.
    token_hash  text        NOT NULL,

    -- When the rotation that spent it committed. This stands in for created_at: see above.
    consumed_at timestamptz NOT NULL,

    -- ON DELETE RESTRICT per Docs/10 §3.3. A cascade would let the evidence that a token was
    -- spent disappear with the session, which is the one direction reuse detection cannot
    -- tolerate — a deleted session would turn every token it ever issued back into "unknown".
    CONSTRAINT fk_consumed_refresh_tokens_session
        FOREIGN KEY (session_id) REFERENCES device_sessions (id) ON DELETE RESTRICT
);

-- One token, one row, and it is the lookup the refresh path takes when no live session matches.
-- Unique because a hash belongs to exactly one consumption: two rows sharing one would make
-- "which session does this reused token belong to" ambiguous, and the answer is what gets
-- revoked.
CREATE UNIQUE INDEX uq_consumed_refresh_tokens_hash
    ON consumed_refresh_tokens (token_hash);

-- Docs/10 §3.3: every foreign key is indexed. This one is also the read path for pruning a
-- session's spent tokens once it has ended.
CREATE INDEX idx_consumed_refresh_tokens_session
    ON consumed_refresh_tokens (session_id, consumed_at DESC);

COMMENT ON TABLE consumed_refresh_tokens IS
    'Refresh tokens that have been rotated away (SHIP-40). Presenting one revokes the whole device session.';
COMMENT ON COLUMN consumed_refresh_tokens.token_hash IS
    'A hash of a spent refresh token. A hash is here or in device_sessions.refresh_token_hash, never both.';

-- ---------------------------------------------------------------------------------------
-- Revocation, on the session itself.
--
-- A session ends three ways and every one of them needs the same representation: reuse detected
-- (SHIP-40), the person signed this device out (SHIP-43), the person revoked it from their
-- device list (SHIP-46). The row is marked rather than deleted, because Docs/10 §3.3 forbids
-- soft-delete-by-removal and because "signed out three weeks ago" is what makes a device list
-- readable — a session that vanishes leaves the owner unable to tell a revoked device from one
-- that was never there.

ALTER TABLE device_sessions
    ADD COLUMN revoked_at     timestamptz,
    ADD COLUMN revoked_reason text;

-- The two columns move together or not at all. Without this a row could say it was revoked for
-- no reason, or carry a reason while still being live — the same pairing
-- ck_email_verification_tokens_consumed makes.
ALTER TABLE device_sessions
    ADD CONSTRAINT ck_device_sessions_revoked
        CHECK ((revoked_at IS NULL) = (revoked_reason IS NULL));

-- Docs/10 §3.4: text with a CHECK, not an enum type, and paired with a test that holds the Go
-- constants to this list. Three values, and each has a ticket behind it rather than being a
-- value somebody might want later.
ALTER TABLE device_sessions
    ADD CONSTRAINT ck_device_sessions_revoked_reason
        CHECK (revoked_reason IS NULL OR revoked_reason IN (
            'refresh_token_reused',  -- SHIP-40: a spent token was presented again
            'signed_out',            -- SHIP-43: the person signed this device out
            'revoked_by_owner'       -- SHIP-46: the person revoked it from their device list
        ));

COMMENT ON COLUMN device_sessions.revoked_at IS
    'When this session stopped being usable. Null while it is live; the row is never deleted.';
COMMENT ON COLUMN device_sessions.revoked_reason IS
    'Why it ended — reuse detected, signed out, or revoked by its owner.';
