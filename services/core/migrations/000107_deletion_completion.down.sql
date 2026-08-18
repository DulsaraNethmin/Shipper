-- Reverses SHIP-171's schema.
--
-- IF EXISTS throughout, per Docs/10 §3.5: `migrate down n=all` runs against whatever state the
-- database is actually in, which is not always the one the up migration left.
--
-- # The completed rows are deleted, and that loses evidence
--
-- **This is the honest reading rather than a caveat**, in 000006's words. Narrowing the CHECK is
-- refused by PostgreSQL while any row holds 'completed', so something has to happen to those rows,
-- and there are only two candidates.
--
-- 000106 turned its 'deferred' rows back into 'requested', which was lossless: a deferred request is
-- an open request, and the state SHIP-169 alone would have recorded for it says the same thing less
-- precisely. **That reasoning does not carry here and reversing it would be a lie.** A completed
-- request has been *executed*; calling it 'requested' again would say the account is due to be
-- deleted when it already has been, and the sweep would then find it and pseudonymise a row that is
-- already a pseudonym. It could also fail outright — an account may hold a completed request *and* a
-- later open one, which is the whole reason the open index is partial, and converting the first
-- would give one account two open rows for `uq_account_deletion_requests_open` to refuse.
--
-- So the row goes. What is lost is the record that the execution happened and when; what is **not**
-- lost, and cannot be, is the pseudonymisation itself. That is an UPDATE against `users` and four
-- other tables with no copy of what it overwrote, and no migration reverses it. A database rolled
-- back through this point holds pseudonymised accounts with no rows saying why.
--
-- Nothing references account_deletion_requests, so the DELETE is unobstructed. Its own foreign key
-- points the other way and is ON DELETE RESTRICT for the parent, which this does not touch.

DELETE FROM account_deletion_requests WHERE state = 'completed';

DROP INDEX IF EXISTS idx_account_deletion_requests_due;

ALTER TABLE IF EXISTS account_deletion_requests
    DROP CONSTRAINT IF EXISTS ck_account_deletion_requests_state;

ALTER TABLE IF EXISTS account_deletion_requests
    ADD CONSTRAINT ck_account_deletion_requests_state
        CHECK (state IN (
            'requested',  -- SHIP-169: asked for, not yet executed
            'deferred'    -- SHIP-170: asked for while a delivery is in flight; waiting on it
        ));

-- The sessions revoked because an account was pseudonymised become sessions revoked by their owner.
--
-- Unlike the request rows there is a *true* neighbouring value here, so nothing is invented: the
-- session is revoked either way, the person did ask for the account to go, and the alternative —
-- clearing revoked_at with it — would hand a live session back to a credential that had been ended.
-- ck_device_sessions_revoked forbids clearing one column and not the other, which is that constraint
-- doing its job.
UPDATE device_sessions SET revoked_reason = 'revoked_by_owner' WHERE revoked_reason = 'account_deleted';

ALTER TABLE IF EXISTS device_sessions
    DROP CONSTRAINT IF EXISTS ck_device_sessions_revoked_reason;

ALTER TABLE IF EXISTS device_sessions
    ADD CONSTRAINT ck_device_sessions_revoked_reason
        CHECK (revoked_reason IS NULL OR revoked_reason IN (
            'refresh_token_reused',  -- SHIP-40: a spent token was presented again
            'signed_out',            -- SHIP-43: the person signed this device out
            'revoked_by_owner'       -- SHIP-46: the person revoked it from their device list
        ));

COMMENT ON COLUMN account_deletion_requests.state IS
    'Where the request has got to. requested is live; deferred is waiting on a delivery (SHIP-170). SHIP-171 adds completed.';
COMMENT ON COLUMN account_deletion_requests.complete_by IS
    'The date the platform promised, recorded when it was promised. Never recomputed at read time.';
COMMENT ON COLUMN device_sessions.revoked_reason IS
    'Why it ended — reuse detected, signed out, or revoked by its owner.';
