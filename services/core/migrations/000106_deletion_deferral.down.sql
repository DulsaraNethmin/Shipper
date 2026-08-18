-- Reverses SHIP-170.
--
-- IF EXISTS throughout, per Docs/10 §3.5: `migrate down n=all` runs against whatever state the
-- database is actually in, which is not always the one the up migration left.
--
-- **The UPDATE has to come first or this migration cannot run at all.** Narrowing the CHECK back to
-- one value is refused by PostgreSQL while any row holds 'deferred', and a down migration that fails
-- on a database somebody has actually used is a down migration that does not exist. A deferred
-- request is an open request waiting on a delivery, so the honest reversal is to make it live again
-- — the state SHIP-169 alone would have recorded for it. Nothing is lost that this schema can still
-- express.
--
-- It cannot collide with the narrowed index either: the widened index already permits at most one
-- open row per account across both states, so converting every 'deferred' to 'requested' cannot
-- produce a second 'requested' for one user.

UPDATE account_deletion_requests SET state = 'requested' WHERE state = 'deferred';

DROP INDEX IF EXISTS uq_account_deletion_requests_open;

CREATE UNIQUE INDEX uq_account_deletion_requests_open
    ON account_deletion_requests (user_id)
    WHERE state = 'requested';

ALTER TABLE IF EXISTS account_deletion_requests
    DROP CONSTRAINT IF EXISTS ck_account_deletion_requests_state;

ALTER TABLE IF EXISTS account_deletion_requests
    ADD CONSTRAINT ck_account_deletion_requests_state
        CHECK (state IN (
            'requested'  -- SHIP-169: asked for, not yet executed
        ));

COMMENT ON COLUMN account_deletion_requests.state IS
    'Where the request has got to. SHIP-170 adds deferred; SHIP-171 adds completed.';
