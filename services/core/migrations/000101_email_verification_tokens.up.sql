-- SHIP-31: the single-use, expiring token that proves somebody reads the address they gave us.
--
-- # Why the token is stored hashed
--
-- The row is a credential. Anyone who could read this table could otherwise verify every
-- unverified address on the platform, and a database backup, a support query, or a log of a
-- slow query would each be enough. SHA-256 is the right function here and argon2id is not: the
-- token is 256 bits from crypto/rand, so a preimage search is 2^256 rather than the 10^6 a
-- six-digit OTP costs — see 000102_phone_otps, which makes the opposite choice for the opposite
-- reason.
--
-- Hashing also makes the lookup exact rather than a scan: uq_email_verification_tokens_hash is
-- what the confirm endpoint reads, and a deterministic hash is what allows an index on it.
--
-- # Why the address is stored beside the token
--
-- The token proves control of the address it was *sent to*, not of whatever address the account
-- holds when the link is clicked. Recording the address here means the confirm endpoint can
-- refuse a token issued for an address the account no longer has — which is the mechanism that
-- stops a change-of-address (SHIP-43) from being confirmable with a link sent to the old one.
--
-- # Why "consumed" carries a reason
--
-- A token leaves the live state for two different reasons, and conflating them loses the
-- distinction that matters in a support conversation: 'verified' means it did its job,
-- 'superseded' means a newer one replaced it. Rows are never deleted (Docs/10 §3.3), so the
-- history of how many were sent is also what the resend rate limit counts.

CREATE TABLE email_verification_tokens (
    id           uuid        PRIMARY KEY,

    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,

    -- citext to match users.email, so a comparison between the two cannot depend on case.
    email        citext      NOT NULL,

    -- The SHA-256 of the token, hex encoded. The token itself exists only in the email.
    token_hash   text        NOT NULL,

    expires_at   timestamptz NOT NULL,

    consumed_at     timestamptz,
    consumed_reason text,

    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    -- The two consumption columns move together or not at all. Without this, a row could say
    -- it was consumed for no reason, or carry a reason while still being live.
    CONSTRAINT ck_email_verification_tokens_consumed
        CHECK ((consumed_at IS NULL) = (consumed_reason IS NULL)),

    CONSTRAINT ck_email_verification_tokens_reason
        CHECK (consumed_reason IS NULL OR consumed_reason IN ('verified', 'superseded')),

    -- An expiry in the past at the moment of issue would be a token nobody can ever use, which
    -- is a clock or configuration mistake worth catching where it happens.
    CONSTRAINT ck_email_verification_tokens_expiry
        CHECK (expires_at > created_at)
);

-- One token, one row. Two rows sharing a hash would make "consume the token" ambiguous.
CREATE UNIQUE INDEX uq_email_verification_tokens_hash
    ON email_verification_tokens (token_hash);

-- **At most one live token per account**, which is what makes "single-use" survive a resend.
-- Without it an older link keeps working after a newer one is issued, so a link forwarded or
-- left in an old mailbox stays valid for its full lifetime. Reissuing marks the outstanding one
-- 'superseded' first, and this index is what fails loudly if that step is ever skipped.
CREATE UNIQUE INDEX uq_email_verification_tokens_live
    ON email_verification_tokens (user_id)
    WHERE consumed_at IS NULL;

-- Docs/10 §3.3: every foreign key is indexed. The partial index above does not serve a plain
-- lookup by account, which is what the resend rate limit counts over.
CREATE INDEX idx_email_verification_tokens_user
    ON email_verification_tokens (user_id, created_at DESC);

CREATE TRIGGER email_verification_tokens_set_updated_at
    BEFORE UPDATE ON email_verification_tokens
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON TABLE email_verification_tokens IS
    'Single-use, expiring email verification tokens (SHIP-31). Stored hashed; the token itself exists only in the message that carried it.';
COMMENT ON COLUMN email_verification_tokens.email IS
    'The address the token was sent to. A token does not verify an address the account has since changed to.';
