-- SHIP-147: administrator accounts and their sessions — a credential system that shares nothing
-- with the one signed-in users hold.
--
-- The *Done when* is "admin sign-in is independent and cannot be reached with a user token", and
-- the second clause is a statement about these two tables as much as about any middleware.
-- `users` and `device_sessions` are one system; `admin_users` and `admin_sessions` are another.
-- **There is no foreign key between them, in either direction, and there is deliberately no
-- column an administrator account and a user account could be joined on.** A join is the first
-- half of an exchange, and CLAUDE.md's rule about the driver token is the same rule read for a
-- third credential: neither system can be turned into the other.
--
-- An address may therefore exist in both tables at once, and that is correct rather than
-- tolerated. The person who moderates the marketplace may also ship pallets on it; those are two
-- accounts with two passwords, two sign-ins and two sets of consequences, and the platform is not
-- entitled to assume the second from the first.
--
-- # The credential is a row, not a signed token, and that is the design decision worth reading
--
-- Docs/10 §5's mobile pair is an access token (a signed JWT, fifteen minutes, verified from key
-- material alone) plus an opaque refresh token stored hashed against `device_sessions`. An
-- administrator gets **only the second shape**: one opaque high-entropy credential, stored hashed
-- here, read back on every request.
--
-- Three reasons, and none of them is symmetry with `device_sessions`:
--
--   * **Revocation has to be immediate.** Docs/04 §9 asks for least-privilege administrative
--     access, and a fifteen-minute window in which a dismissed administrator still holds a valid
--     credential is a privilege nobody granted. A signed token is valid until it expires whatever
--     the database says; a row can be revoked in one statement.
--   * **A permission change has to be immediate for the same reason** (SHIP-148). Docs/10 §5
--     already refuses to put permissions in the mobile access token because verification state
--     changes during a session. An administrator's role changes during a session too, and it
--     changes in the direction that matters — downwards.
--   * **The read costs nothing here.** The argument for a stateless access token is a phone on a
--     mobile network making many requests against a database that would rather not be asked. The
--     admin panel is a browser talking to one service, and one indexed primary-key read per
--     request buys both of the properties above.
--
-- The consequence is that this credential is presented directly on every administrative request,
-- so there is no refresh endpoint and nothing to rotate: `admin_sessions` holds one hash for the
-- life of the session, and the session ends by lapsing or by being revoked.
--
-- # The expiry columns, and where they part company with SHIP-39
--
-- SHIP-39 settled `device_sessions.refresh_token_expires_at`: an explicit column rather than a
-- Redis TTL, a sliding window rewritten on every use, and **no absolute cap**. The first of those
-- is followed here without argument — a security control a cache flush can undo is not one — and
-- the third is deliberately reversed.
--
-- SHIP-39's reason for refusing a cap was a product consequence: an absolute lifetime signs
-- everybody out on a schedule, "including a driver mid-delivery". That argument does not survive
-- the change of subject. An administrator is a person at a desk with a browser; being asked to
-- sign in again at the end of a shift costs one password entry, and the credential it bounds can
-- suspend accounts and unpublish jobs. So there are two columns:
--
--   * `idle_expires_at` slides, exactly as SHIP-39's does, so a console in use does not expire
--     under its user;
--   * `absolute_expires_at` does not, so no administrator session outlives a working day however
--     busy it was.
--
-- `ck_admin_sessions_idle_within_absolute` is what makes the cap a fact about the table rather
-- than a rule the sliding code remembers: a slide that tried to reach past the cap is refused by
-- PostgreSQL, not by a `min()` somebody could delete.
--
-- # What is deliberately not here
--
--   SHIP-148  the permission catalogue. `role` is below because an account without one is not a
--             complete account and sign-in has to report it, but what a role *grants* is a Go
--             table and a middleware check rather than schema — there is no permission column, no
--             grant table and no join, because permissions are a security control read on every
--             request and not data an operator edits.
--   SHIP-150  the audit entry every privileged action writes. `audit_log` (000003) already exists
--             and is already append-only by trigger; what is missing is the writer, not the table.
--   SHIP-166  the second administrator's approval for a permanent suspension.
--
-- **There is no `admin_users` seeding here, and that is deliberate.** A migration that inserted a
-- first administrator would be a credential in the repository — the one thing CLAUDE.md's
-- never-commit list names first — or a row with a password nobody can use. The first account is
-- created by an operator with one `INSERT`, and every account after it by an administrator
-- holding `admins.manage`.

CREATE TABLE admin_users (
    id uuid PRIMARY KEY,

    -- citext for 000002's reason: an administrator typing Alice@ and alice@ at a sign-in screen
    -- means one account, and case-folding in the column type is stronger than case-folding at
    -- every call site.
    --
    -- **No foreign key to users.email and no uniqueness across the two tables.** See the header:
    -- the same address in both places is two accounts, not one person the platform has recognised.
    email citext NOT NULL,

    -- Who this is, for the audit trail and for the administrator list. Not a display name the
    -- account holder chooses at will — 000003 records `actor_id` and support reads it back as a
    -- person.
    name text NOT NULL,

    -- argon2id, as a PHC string, in one column — Docs/10 §5, and the same shape users.password_hash
    -- holds.
    --
    -- **Written by internal/passwords and by nothing else.** SHIP-15r moved argon2id out of
    -- internal/identity into infrastructure precisely so this table would not be the reason for a
    -- second implementation; a domain may not import another domain, and the answer to that is one
    -- hasher both sit on rather than two that agree by comment (Docs/10 §3.4).
    password_hash text NOT NULL,

    -- What this administrator may do, as a name rather than as a set (SHIP-148).
    --
    -- **DEFAULT 'support', and the default is the control.** Docs/04 §9's least-privilege
    -- requirement is easiest to break by omission: an INSERT that forgets this column should
    -- produce the account that can do least, never the account that can do most. 'support' is the
    -- read-only bundle.
    role text NOT NULL DEFAULT 'support',

    -- Whether the account may sign in at all. Two values, because an administrator who has left is
    -- disabled rather than deleted — 000003's audit entries name them for ever, and Docs/05 §3.1
    -- keeps that history after the subject is gone.
    status text NOT NULL DEFAULT 'active',

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    -- The bounds outside which the column stops being usable, not a product rule about what a
    -- valid address looks like. 000002 takes the same position on users.email.
    CONSTRAINT ck_admin_users_email CHECK (length(email) BETWEEN 3 AND 320),
    CONSTRAINT ck_admin_users_name CHECK (length(name) BETWEEN 1 AND 200),

    -- A stored value that is not a PHC argon2id string is a credential nothing can verify, and it
    -- would surface as an unreadable-hash 500 at somebody's sign-in rather than at the write that
    -- caused it. Cheap to refuse here; passwords.ErrMalformedHash covers the rest.
    CONSTRAINT ck_admin_users_password_hash CHECK (password_hash LIKE '$argon2id$%'),

    -- Paired with admin.Roles by TestAdminRoleConstraintMatchesTheGoConstants, which is Docs/10
    -- §3.4's pairing in both directions: a role the database accepts and Go has no bundle for is
    -- an administrator whose permissions are undefined, and a role Go knows and the database
    -- refuses is one nobody can be given.
    --
    -- Ordered least-privileged first, which is also the order internal/admin lists them in.
    CONSTRAINT ck_admin_users_role CHECK (role IN ('support', 'moderator', 'owner')),

    CONSTRAINT ck_admin_users_status CHECK (status IN ('active', 'disabled'))
);

-- One account per address. The sign-in path arrives holding an address and nothing else, so this
-- is both the uniqueness rule and the read path.
CREATE UNIQUE INDEX uq_admin_users_email ON admin_users (email);

CREATE TRIGGER admin_users_set_updated_at
    BEFORE UPDATE ON admin_users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE admin_sessions (
    id uuid PRIMARY KEY,

    admin_user_id uuid NOT NULL,

    -- The session credential, hashed. The token itself is never stored, for 000100's reason: this
    -- table is read by every support query, every backup and every replica, and a readable
    -- administrator credential in any of those is the whole console.
    --
    -- A plain SHA-256 digest rather than argon2id, and the reasoning is 000100's unchanged: the
    -- token is 32 bytes from crypto/rand rather than something a person chose, so there is nothing
    -- to guess and a per-verification cost of tens of milliseconds would be paid on **every**
    -- administrative request rather than once per sign-in.
    token_hash text NOT NULL,

    -- When this session stops being usable through inactivity. Rewritten as the console is used,
    -- so the window slides; clamped by the column below, so the slide cannot outrun the cap.
    idle_expires_at timestamptz NOT NULL,

    -- When it stops being usable regardless. Written once, at sign-in, and never moved. See the
    -- header for why SHIP-39 refused this column for a phone and it is here for a console.
    absolute_expires_at timestamptz NOT NULL,

    -- When the credential was last presented. Unlike device_sessions.last_seen_at this is not a
    -- display column that something else derives a lifetime from — the lifetime is
    -- idle_expires_at, above, which is exactly the mistake SHIP-39 refused to make. This is for
    -- support: "which administrator was working when that happened".
    last_used_at timestamptz NOT NULL,

    -- When it was ended deliberately: a sign-out, or an administrator being disabled. NULL while
    -- the session is live, which is what makes uq_admin_sessions_live_per_token partial.
    --
    -- Revocation is a column rather than a DELETE so that "this session was signed out at 16:12"
    -- survives, and because a row that can vanish cannot be reasoned about after the fact.
    revoked_at timestamptz,

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    -- An expiry at or before the moment of issue is a session nobody can ever use: a clock or
    -- configuration mistake, worth catching where it happens rather than as a sign-in loop.
    -- Compared against created_at rather than now(), for 000103's reason — the row is rewritten on
    -- every slide, and a now() comparison would refuse any correction of a historic row.
    CONSTRAINT ck_admin_sessions_idle_expiry CHECK (idle_expires_at > created_at),
    CONSTRAINT ck_admin_sessions_absolute_expiry CHECK (absolute_expires_at > created_at),

    -- **The cap, as a fact about the table.** A sliding idle window that could be written past the
    -- absolute expiry would make the cap advisory — true only for as long as the Go that clamps it
    -- is written correctly. This is what makes it true whatever writes the row.
    CONSTRAINT ck_admin_sessions_idle_within_absolute
        CHECK (idle_expires_at <= absolute_expires_at),

    -- ON DELETE RESTRICT per Docs/10 §3.3. An administrator who has left is disabled rather than
    -- deleted (see admin_users.status), so nothing should be removing the parent row; if something
    -- tries, its sessions are the reason to stop it rather than collateral.
    CONSTRAINT fk_admin_sessions_admin
        FOREIGN KEY (admin_user_id) REFERENCES admin_users (id) ON DELETE RESTRICT
);

-- The credential arrives and nothing else does, so the hash is how a session is found. Unique
-- because one token belongs to one session: without this, a bug that wrote the same hash twice
-- would leave a credential that resolves to two administrators, and which one an audit entry named
-- would depend on the plan.
CREATE UNIQUE INDEX uq_admin_sessions_token_hash ON admin_sessions (token_hash);

-- Every foreign key is indexed (Docs/10 §3.3). This is also "sign this administrator out
-- everywhere", which is what disabling an account has to do to mean anything.
CREATE INDEX idx_admin_sessions_admin ON admin_sessions (admin_user_id, created_at DESC);

-- The live sessions, for the sweep that removes lapsed ones and for an administrator-session list.
-- Partial, because a table that accumulates one row per sign-in is mostly history.
CREATE INDEX idx_admin_sessions_live
    ON admin_sessions (idle_expires_at)
    WHERE revoked_at IS NULL;

CREATE TRIGGER admin_sessions_set_updated_at
    BEFORE UPDATE ON admin_sessions
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON TABLE admin_users IS
    'Administrator accounts. A separate credential system from users: no foreign key joins the two, and neither can be exchanged for the other (SHIP-147).';
COMMENT ON COLUMN admin_users.role IS
    'What this administrator may do, as a name. Defaults to the least-privileged role, deliberately (Docs/04 §9). Paired with admin.Roles.';
COMMENT ON TABLE admin_sessions IS
    'One row per administrator sign-in. The credential is opaque and stored hashed; it is read back on every request so that revocation and a role change take effect at once (SHIP-147).';
COMMENT ON COLUMN admin_sessions.idle_expires_at IS
    'Slides as the console is used, and never past absolute_expires_at — ck_admin_sessions_idle_within_absolute is what makes the cap the database''s rather than the caller''s.';
COMMENT ON COLUMN admin_sessions.absolute_expires_at IS
    'Written once at sign-in. SHIP-39 refused this column for a phone on a product argument that does not survive the change of subject; see 000801''s header.';
