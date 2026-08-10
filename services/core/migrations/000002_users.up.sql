-- SHIP-28: the account every other table hangs off.
--
-- users lives in the shared block rather than in identity's, even though identity owns the
-- behaviour. Jobs reference a customer, bids reference a provider, sessions and audit entries
-- reference an actor — so the table is read by most of the service, and Docs/10 §9.2 puts what
-- several domains depend on under shared ownership rather than under whichever one needed it
-- first.

CREATE TABLE users (
    id                uuid        PRIMARY KEY,

    -- citext, so the uniqueness constraint treats Alice@example.com and alice@example.com as
    -- one address. Every mail system does; a `text` column would not, and the two accounts
    -- would be indistinguishable to the person who owns them.
    email             citext      NOT NULL,

    -- E.164, normalised before it arrives. Stored NOT NULL because Docs/04 §2 requires both
    -- email and phone verified before a customer may publish, so an account without one is not
    -- a state the product has a use for.
    phone             text        NOT NULL,

    password_hash     text        NOT NULL,

    -- Fixed at registration and immutable thereafter (SHIP-45). There is no transition between
    -- customer and provider: the two see different shells, hold different obligations, and a
    -- change would silently rewrite the meaning of every job and bid already attached.
    role              text        NOT NULL,

    -- Account standing, which is not the same thing as provider verification. Verification is
    -- an eligibility decision owned by the profiles domain with its own five states
    -- (Docs/04 §4); this is whether the account may be used at all.
    status            text        NOT NULL DEFAULT 'active',

    -- Verification recorded as when, not whether. A boolean answers "is it verified"; a
    -- timestamp answers that and also "since when", which is what a support conversation and
    -- an audit trail both actually ask.
    email_verified_at  timestamptz,
    phone_verified_at  timestamptz,

    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ck_users_role   CHECK (role   IN ('customer', 'provider')),
    CONSTRAINT ck_users_status CHECK (status IN ('active', 'restricted', 'suspended'))
);

-- Uniqueness on both contact channels. Two accounts sharing a phone number would make the OTP
-- in SHIP-34 ambiguous about which account it verifies.
CREATE UNIQUE INDEX uq_users_email ON users (email);
CREATE UNIQUE INDEX uq_users_phone ON users (phone);

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON TABLE users IS
    'One account. Role is fixed at registration (SHIP-45). Deletion pseudonymises rather than removes (Docs/05 §3.1).';
COMMENT ON COLUMN users.status IS
    'Account standing. Provider verification state is separate and lives with profiles (Docs/04 §4).';
