-- SHIP-170: a deletion request made while the person is carrying a delivery waits for it.
--
-- Docs/05 §3.1: "Deletion during an active job is deferred, not refused. A request made between
-- Awarded and Delivered is queued until the job closes, and the user is told why. Erasing a party
-- mid-delivery would strand the counterparty." This migration is the storage half of that — the
-- state the queue is expressed in, and the index that keeps a queued request from being joined by
-- a second one.
--
-- **000105 pre-authorised exactly this and named the mechanism.** Its note on `state` reads: "one
-- value today, and that is deliberate rather than a placeholder — every other value belongs to a
-- ticket that has not been built: SHIP-170 'deferred' … Adding either is `ALTER TABLE … DROP
-- CONSTRAINT … ADD CONSTRAINT …`, which is the whole reason §3.4 rejects an enum type." So this is
-- the anticipated widening rather than a new decision, and the two guards that fail the moment it
-- lands — the pairing test in migrations/account_deletion_requests_test.go and the scope assertion
-- in scripts/verify/40-identity.sh — are both moved in the same change rather than deleted.
--
-- # The index has to widen with the constraint, and that is the half a reader would miss
--
-- `uq_account_deletion_requests_open` was partial on `state = 'requested'`, and 000105 said why:
-- "Partial rather than total because the states above will grow. A completed request must not stop
-- a later one; only an *open* one does." A deferred request is **open** — it is the same promise,
-- waiting on a delivery rather than on the thirty days — so leaving the predicate alone would have
-- made the index stop covering the row the moment the deferral was recorded, and an account could
-- then hold one deferred request and one requested one. Two rows are two promises about one
-- account, which is the exact defect 000105 built the index to prevent.
--
-- SHIP-171's 'completed' is the state that must **not** join this predicate, for 000105's reason: a
-- request that has been executed must not stop a later one.
--
-- **No column is added, and that is a decision rather than an omission.** The tempting shape is a
-- nullable `waiting_on_job_id` naming the delivery being waited on. It would be a foreign key from
-- identity's block into `jobs`, and 000105's own reasoning argues against it: this row is evidence
-- and must outlive everything, which is why its one foreign key is ON DELETE RESTRICT — tying it to
-- a job would make the job unremovable too, and would leave a stale identifier behind the moment
-- the deferral lifted. Whether a person is carrying a delivery is re-read through the port
-- `internal/identity` declares, every time the request is touched, so it cannot go stale at all.

ALTER TABLE account_deletion_requests
    DROP CONSTRAINT ck_account_deletion_requests_state;

ALTER TABLE account_deletion_requests
    ADD CONSTRAINT ck_account_deletion_requests_state
        CHECK (state IN (
            'requested',  -- SHIP-169: asked for, not yet executed
            'deferred'    -- SHIP-170: asked for while a delivery is in flight; waiting on it
        ));

DROP INDEX uq_account_deletion_requests_open;

-- Both open states, for the reason above. Still partial, so SHIP-171's 'completed' will fall out
-- of it without a further migration.
CREATE UNIQUE INDEX uq_account_deletion_requests_open
    ON account_deletion_requests (user_id)
    WHERE state IN ('requested', 'deferred');

COMMENT ON COLUMN account_deletion_requests.state IS
    'Where the request has got to. requested is live; deferred is waiting on a delivery (SHIP-170). SHIP-171 adds completed.';
