-- SHIP-169: a person asks for their account to be deleted, and is told when it will happen.
--
-- Docs/05 §3.1 is the adopted model and this table is shaped by it rather than by the endpoint:
-- "Shipper confirms the request in-app, executes within 30 days, and tells the user when it will
-- complete." The date the user was told is a promise the platform made, so it is *recorded* here
-- rather than derived whenever somebody asks — see the note on complete_by, which is the whole
-- reason this is a table and not a boolean.
--
-- # Why this is a table in identity's block and not a column on users
--
-- Three reasons, in the order they decide it.
--
-- [1] `users` is shared-surface (Docs/10 §3.5, §9.2): the shared block 000001–000099 belongs to
-- whoever is doing shared-platform work, and a domain branch does not alter it. That is a process
-- rule, but it is the one that settles where the migration goes.
--
-- [2] Docs/10 §3.3 forbids soft deletes, and a `deleted_at` on `users` is exactly the column that
-- invites one. SHIP-171 pseudonymises rather than removes, precisely so the transaction record
-- Docs/05 §3.1 requires retaining survives; a nullable timestamp on the account is the shape that
-- would quietly turn into `WHERE deleted_at IS NULL` in eight domains' queries.
--
-- [3] A deletion request is evidence, not a flag. Who asked, when they asked, what they were
-- promised, and what became of it are four facts, and one of them (the promise) has to stay
-- readable after the state has moved on. `users.status` already carries the account's standing and
-- has no room for any of that — 000002's ck_users_status has no 'deleted' value on purpose, and
-- internal/identity's Status type says why.
--
-- # What this table deliberately does not have
--
-- **No actor column.** The only caller who can create a row is the account holder: the endpoint is
-- RequireUser and takes the subject from the token, and Docs/05 §3.1 puts execution out of an
-- ordinary administrator's reach entirely. An `actor_id` would therefore always equal `user_id`,
-- and a column that is always a copy of another is a column that eventually disagrees with it.
-- When SHIP-170 or SHIP-171 gives something *other* than the account holder a way to move a row,
-- that mover is a decision with a reason and belongs in audit_log, which already has the vocabulary.
--
-- **No created_at beside requested_at.** The row *is* the record of a request, so its creation and
-- the request are one event — the same argument 000104's consumed_refresh_tokens makes for holding
-- one instant rather than two that have to be kept in step.
--
-- **No cancellation.** Withdrawing a deletion request has no ticket, and inventing a state for it
-- here would be inventing the product decision that goes with it (Apple requires deletion to be
-- offered, not that it be revocable). ck_account_deletion_requests_state is what makes adding one
-- an ordinary migration rather than a silent widening.

CREATE TABLE account_deletion_requests (
    id           uuid        PRIMARY KEY,

    user_id      uuid        NOT NULL,

    -- Docs/10 §3.4: text with a CHECK, never a PostgreSQL enum type. One value today, and that
    -- is deliberate rather than a placeholder — every other value belongs to a ticket that has
    -- not been built:
    --
    --   SHIP-170  'deferred'   a request made between Awarded and Delivered waits for the job
    --   SHIP-171  'completed'  the person has been replaced by a stable pseudonym
    --
    -- Adding either is `ALTER TABLE … DROP CONSTRAINT … ADD CONSTRAINT …`, which is the whole
    -- reason §3.4 rejects an enum type. The paired test that holds this list to the Go constants
    -- is in migrations/account_deletion_requests_test.go.
    state        text        NOT NULL,

    -- When the person asked. This stands in for created_at: see above.
    requested_at timestamptz NOT NULL,

    -- **The date the person was told, recorded when they were told it.**
    --
    -- This is the column the ticket exists for and it is the one a plausible implementation
    -- leaves out. Deriving the completion date at read time — now() plus thirty days, computed
    -- afresh whenever anybody asks — produces an answer that always looks right and is a
    -- different date every day. The platform would then have no record of what it promised, and
    -- the promise would silently slide forward for exactly as long as nobody executed it.
    --
    -- NOT NULL rather than nullable-with-a-default, so an INSERT that stops supplying it fails
    -- here rather than storing a plausible substitute.
    complete_by  timestamptz NOT NULL,

    updated_at   timestamptz NOT NULL DEFAULT now(),

    -- ON DELETE RESTRICT per Docs/10 §3.3. A cascade would be self-defeating: this row is the
    -- evidence that a deletion was asked for and when it was due, and the one event that must not
    -- destroy it is the deletion itself. SHIP-171 replaces the person and keeps the account row,
    -- so nothing legitimate ever removes the parent.
    CONSTRAINT fk_account_deletion_requests_user
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,

    CONSTRAINT ck_account_deletion_requests_state
        CHECK (state IN (
            'requested'  -- SHIP-169: asked for, not yet executed
        )),

    -- The promise is in the future when it is made. This is a weak check on purpose — it says
    -- nothing about thirty days, because the window is a product decision that may move and a
    -- CHECK that encoded it would make changing it a migration. What it does catch is the class
    -- of defect where complete_by is written from a zero value or from the wrong variable, which
    -- is what an implementation that forgot to record it looks like from the database's side.
    CONSTRAINT ck_account_deletion_requests_complete_by
        CHECK (complete_by > requested_at)
);

-- **One open request per account.**
--
-- A partial unique index rather than application logic, on the same reasoning as SHIP-91's
-- one-accepted-bid index (Docs/06 §4.1): the endpoint is idempotent by middleware, but two taps
-- carrying two different keys are two honest requests arriving together, and a check-then-insert
-- loses that race. With this, the second INSERT is refused by the database and the caller is
-- handed the request that already exists — with the completion date it already carried, which is
-- the property that keeps the promise from moving.
--
-- Partial rather than total because the states above will grow. A completed request must not stop
-- a later one; only an *open* one does.
CREATE UNIQUE INDEX uq_account_deletion_requests_open
    ON account_deletion_requests (user_id)
    WHERE state = 'requested';

-- Docs/10 §3.3: every foreign key is indexed. uq_…_open covers only the open row, so this is what
-- serves "everything this account has ever asked for", newest first — the support question, and
-- SHIP-171's read path.
CREATE INDEX idx_account_deletion_requests_user
    ON account_deletion_requests (user_id, requested_at DESC);

-- Docs/10 §3.3: a mutable table attaches the trigger from 000001_init, and a table with the column
-- and no trigger is a defect a test catches. Nothing in SHIP-169 updates a row — SHIP-170 and
-- SHIP-171 both do, by moving state — and the column is here from the start so that the ticket
-- which first needs it is not also the ticket that has to remember the trigger.
CREATE TRIGGER set_account_deletion_requests_updated_at
    BEFORE UPDATE ON account_deletion_requests
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON TABLE account_deletion_requests IS
    'Account deletion requests and the completion date the person was told (SHIP-169, Docs/05 §3.1).';
COMMENT ON COLUMN account_deletion_requests.complete_by IS
    'The date the platform promised, recorded when it was promised. Never recomputed at read time.';
COMMENT ON COLUMN account_deletion_requests.state IS
    'Where the request has got to. SHIP-170 adds deferred; SHIP-171 adds completed.';
