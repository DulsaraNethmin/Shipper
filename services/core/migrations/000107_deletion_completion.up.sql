-- SHIP-171: the clock runs out, the person is replaced by a pseudonym, and the request is history.
--
-- Docs/05 §3.1 is the adopted model and this migration is the last piece of schema it needs:
-- "delete the person, retain the transaction". The account row stays — sixteen migrations carry
-- `REFERENCES users` and every one of them is ON DELETE RESTRICT, deliberately — and what changes
-- is that the columns identifying a human being stop identifying one.
--
-- **000105 pre-authorised this by name and 000106 pre-authorised the shape of it.** 000105's note
-- on `state`: "SHIP-171 'completed' the person has been replaced by a stable pseudonym … Adding
-- either is `ALTER TABLE … DROP CONSTRAINT … ADD CONSTRAINT …`, which is the whole reason §3.4
-- rejects an enum type." So this is the second anticipated widening rather than a new decision.
--
-- # The index does *not* widen with the constraint, and that is the half a reader would get wrong
--
-- 000106 widened `uq_account_deletion_requests_open` because a deferred request is an *open*
-- request. A completed one is not, and 000105 said why before either existed: "Partial rather than
-- total because the states above will grow. A completed request must not stop a later one; only an
-- *open* one does." 000106 then named this migration in the same breath — "SHIP-171's 'completed'
-- is the state that must **not** join this predicate".
--
-- So there is no `DROP INDEX` here. Adding 'completed' to that predicate would be a silent,
-- permanent refusal: the account would hold one row forever and every later request would be
-- swallowed by `ON CONFLICT … DO NOTHING` with the executed request handed back as though it were
-- live. Nothing in the build would report it, which is why
-- `TestTheOpenStatesTheIndexCoversAreTheOnesTheDomainCallsOpen` exists and reads the predicate back
-- out of `pg_indexes`.
--
-- # Why the person is not deleted
--
-- 000201's `provider_id` foreign key states the consequence directly: "a provider whose documents
-- have been reviewed cannot be deleted at all. SHIP-171 pseudonymises rather than removes, which is
-- the reading Docs/04 §1 requires of an evidence trail." The same is true of `jobs.customer_id`,
-- `bids.provider_id`, `audit_log.actor_id`, `notifications.recipient_id` and every other reference:
-- a `DELETE FROM users` would be refused by the first of them, and if it were not, it would take the
-- counterparty's own history with it.

ALTER TABLE account_deletion_requests
    DROP CONSTRAINT ck_account_deletion_requests_state;

ALTER TABLE account_deletion_requests
    ADD CONSTRAINT ck_account_deletion_requests_state
        CHECK (state IN (
            'requested',  -- SHIP-169: asked for, not yet executed
            'deferred',   -- SHIP-170: asked for while a delivery is in flight; waiting on it
            'completed'   -- SHIP-171: executed — the person has been replaced by a pseudonym
        ));

-- What the sweep reads: the open requests whose promised date has arrived.
--
-- **`(state, complete_by)` with no predicate, and the absence of one is deliberate.** A partial
-- index `WHERE state IN ('requested', 'deferred')` would be a *third* written-out copy of the open
-- set — beside `uq_account_deletion_requests_open`'s predicate and `identity.openDeletionStatesSQL`
-- — and the two that exist are held together by a test precisely because a third would not be. A
-- total index on the leading column answers the same query and cannot drift from anything.
--
-- The claim itself is `state IN (…) AND complete_by <= $1 ORDER BY complete_by FOR UPDATE SKIP
-- LOCKED`, which is the shape every task in cmd/worker uses and the reason `complete_by` is the
-- second column rather than the first: the equality is on `state`.
CREATE INDEX idx_account_deletion_requests_due
    ON account_deletion_requests (state, complete_by);

-- A fourth reason a session stops being usable.
--
-- 000104's own note — "Three values, and each has a ticket behind it rather than being a value
-- somebody might want later" — is the standard this meets rather than an argument against it: an
-- account that has been pseudonymised must not still be reachable by a credential issued before it
-- was. Revoking is a mark rather than a delete, so the device list a support conversation reads
-- still says what happened and when.
--
-- The Go constants and this list are held together by TestRevokedReasonsMatchTheConstraint, which
-- is what makes widening it on one side alone a failure rather than a drift.
ALTER TABLE device_sessions
    DROP CONSTRAINT ck_device_sessions_revoked_reason;

ALTER TABLE device_sessions
    ADD CONSTRAINT ck_device_sessions_revoked_reason
        CHECK (revoked_reason IS NULL OR revoked_reason IN (
            'refresh_token_reused',  -- SHIP-40: a spent token was presented again
            'signed_out',            -- SHIP-43: the person signed this device out
            'revoked_by_owner',      -- SHIP-46: the person revoked it from their device list
            'account_deleted'        -- SHIP-171: the account was pseudonymised
        ));

COMMENT ON COLUMN account_deletion_requests.state IS
    'Where the request has got to. requested is live; deferred is waiting on a delivery (SHIP-170); completed means the person has been replaced by a pseudonym (SHIP-171).';
COMMENT ON COLUMN account_deletion_requests.complete_by IS
    'The date the platform promised, recorded when it was promised. Never recomputed at read time. On a completed request it is the promise that was kept, and updated_at is when it was kept.';
COMMENT ON COLUMN device_sessions.revoked_reason IS
    'Why it ended — reuse detected, signed out, revoked by its owner, or the account was pseudonymised.';
