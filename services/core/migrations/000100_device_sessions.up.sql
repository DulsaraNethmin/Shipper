-- SHIP-38: one row per signed-in device.
--
-- This is the record of truth for refresh state, and PostgreSQL is deliberately where it lives.
-- Docs/10 §5 amends Docs/06 §2.1 on exactly this point: Redis may hold a denylist as a fast
-- path, but the guarantee that presenting a consumed refresh token invalidates the whole device
-- session is a security control, and a security control that a cache flush can undo is not one.
--
-- The refresh token itself is opaque random bytes and is never stored. What is stored is a hash
-- of it, for the same reason a password is: this table is read by every support query, every
-- backup, and every replica, and a readable refresh token in any of those is a signed-in
-- session someone else can take over.
--
-- The grain is one row per device, which is what makes "sign out this phone" (SHIP-43) and
-- "show me my devices" (SHIP-46) answerable. It is not enforced by a unique constraint, because
-- the only thing identifying a device here is a label the person typed — two phones may both be
-- called "iPhone", and refusing the second would be refusing a sign-in.
--
-- Rotation, reuse detection and revocation are SHIP-39, SHIP-40 and SHIP-43. They are not here:
-- each adds behaviour and the columns that behaviour needs, from the identity block, and
-- guessing at those columns now would be guessing at a design that has not been written.

CREATE TABLE device_sessions (
    id                 uuid        PRIMARY KEY,

    user_id            uuid        NOT NULL,

    -- The refresh token, hashed. Docs/10 §5: opaque random, stored hashed.
    --
    -- The token is high-entropy random rather than a password, so this is a plain digest and
    -- not argon2id: there is nothing to guess, and a per-verification cost measured in tens of
    -- milliseconds would be paid on every token refresh by every device. The digest and its
    -- encoding are the issuer's to choose (SHIP-39); the column only requires that whatever
    -- arrives here is not the token.
    refresh_token_hash text        NOT NULL,

    -- What the person sees in the device list: "Nethmin's iPhone", "Pixel 8". Supplied by the
    -- client and never trusted for anything but display — it identifies a row to a human, not
    -- to the platform.
    device_label       text        NOT NULL,

    -- When this device was last seen using its session. Docs/07 §3 makes every session
    -- individually revocable by its owner, and "last used two months ago" beside a device
    -- nobody recognises is the whole reason anybody revokes one.
    last_seen_at       timestamptz NOT NULL DEFAULT now(),

    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now(),

    -- A blank label leaves a row nobody can identify in their own device list, and an
    -- unbounded one is a display problem on a phone. Neither is a product rule — those live
    -- server-side and changeably (Docs/06 §5.3) — only the bounds outside which the column
    -- stops being usable at all.
    CONSTRAINT ck_device_sessions_device_label
        CHECK (char_length(device_label) BETWEEN 1 AND 120),

    -- ON DELETE RESTRICT, per Docs/10 §3.3, and there are no soft deletes to soften it:
    -- SHIP-171 pseudonymises an account rather than removing it, so a cascade here would be a
    -- path by which sessions disappear without anyone having signed out.
    CONSTRAINT fk_device_sessions_user
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT
);

-- Every foreign key is indexed (Docs/10 §3.3). This one is also the read path for the device
-- list, and for the revocation that ends every session an account has.
CREATE INDEX idx_device_sessions_user ON device_sessions (user_id);

-- The refresh path arrives holding a token and nothing else, so the hash is how a session is
-- found. Unique because one token belongs to one session: without this, a collision — or a bug
-- that wrote the same hash twice — would leave a token that refreshes two sessions at once.
CREATE UNIQUE INDEX uq_device_sessions_refresh_token_hash
    ON device_sessions (refresh_token_hash);

CREATE TRIGGER device_sessions_set_updated_at
    BEFORE UPDATE ON device_sessions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON TABLE device_sessions IS
    'One row per signed-in device. The record of truth for refresh state; Redis is a fast path only (Docs/10 §5).';
COMMENT ON COLUMN device_sessions.refresh_token_hash IS
    'A hash of the opaque refresh token. The token itself is never stored.';
COMMENT ON COLUMN device_sessions.device_label IS
    'Client-supplied, for display in the device list. Never used to authenticate anything.';
