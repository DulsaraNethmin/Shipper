-- SHIP-34: the time-limited numeric code that proves somebody holds the handset.
--
-- # Why this is hashed with argon2id and the email token is not
--
-- The two look like the same problem and are the opposite of each other. An email token is 256
-- bits of entropy, so SHA-256 is enough — there is nothing for a work factor to protect
-- (000101_email_verification_tokens). A six-digit code has 10^6 possibilities, and a stolen
-- database of SHA-256 digests would be inverted by a laptop in under a second.
--
-- So the code is stored the way a password is: argon2id, at the configured profile, with the
-- cost parameters travelling in the PHC string. Verification is rare — the rate limits below cap
-- it — so the cost is paid a handful of times per account and not per request.
--
-- The attempt counter is the *other* half, and it is the one that matters online: 10^6 is only
-- expensive if guessing takes more than five tries.
--
-- # Why the number is stored beside the code
--
-- The same reason the email token records its address. A code proves control of the number it
-- was sent to, not of whatever number the account holds when the code comes back.
--
-- # Why the OTP is not stored in Redis
--
-- Redis holds idempotency keys and will hold rate-limit buckets, and both are recoverable if it
-- is flushed. This is not: an account's verification state depends on it, and a flush that
-- silently invalidated every outstanding code would look to a person holding a phone like the
-- code they were just sent being wrong. Docs/10 §5 makes the same call for refresh tokens —
-- PostgreSQL is the record of truth, Redis is a fast path.

CREATE TABLE phone_otps (
    id           uuid        PRIMARY KEY,

    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,

    -- E.164, matching users.phone, which is normalised before it arrives.
    phone        text        NOT NULL,

    -- The argon2id PHC string, exactly as users.password_hash holds one.
    code_hash    text        NOT NULL,

    expires_at   timestamptz NOT NULL,

    -- Wrong guesses. The code is retired once this reaches the limit, so that a six-digit
    -- space cannot be walked at leisure.
    attempts     integer     NOT NULL DEFAULT 0,

    consumed_at     timestamptz,
    consumed_reason text,

    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ck_phone_otps_consumed
        CHECK ((consumed_at IS NULL) = (consumed_reason IS NULL)),

    -- 'exhausted' is a third outcome the email token does not have, and it is worth recording
    -- separately: a code retired by wrong guesses is a support conversation, and possibly an
    -- attack, whereas one superseded by a resend is neither.
    CONSTRAINT ck_phone_otps_reason
        CHECK (consumed_reason IS NULL OR consumed_reason IN ('verified', 'superseded', 'exhausted')),

    CONSTRAINT ck_phone_otps_attempts CHECK (attempts >= 0),

    CONSTRAINT ck_phone_otps_expiry CHECK (expires_at > created_at)
);

-- At most one live code per account. Asking for a new code invalidates the previous one, which
-- is what a person expects when they press "resend" — and without it, an older code stays valid
-- for its full ten minutes alongside the new one.
CREATE UNIQUE INDEX uq_phone_otps_live
    ON phone_otps (user_id)
    WHERE consumed_at IS NULL;

-- Docs/10 §3.3: every foreign key is indexed. This one is also what the rate limit reads —
-- how many codes an account has been sent, and when the last one went — so it carries
-- created_at.
CREATE INDEX idx_phone_otps_user
    ON phone_otps (user_id, created_at DESC);

CREATE TRIGGER phone_otps_set_updated_at
    BEFORE UPDATE ON phone_otps
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON TABLE phone_otps IS
    'Time-limited numeric phone verification codes (SHIP-34). Stored as argon2id, because six digits is a search space a work factor has something to protect.';
COMMENT ON COLUMN phone_otps.attempts IS
    'Wrong guesses. The online defence; the hash is the offline one.';
