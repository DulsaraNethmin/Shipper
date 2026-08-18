-- Reverse of 000804: a dispute can be closed but not documented.
--
-- `IF EXISTS` throughout, because CI runs up -> down all -> up against an empty database and a down
-- migration must be re-runnable (SHIP-15g).
--
-- **This destroys what every resolved dispute was resolved as**, and nothing else holds the §7
-- outcome — `jobs.status` and `job_status_history` record where the job went, which is the other
-- vocabulary and deliberately not this one. `audit_log.metadata` carries the outcome on every
-- resolution entry and is append-only, so the trail survives; the *table* does not.
--
-- `resolved_at` is left in place. It is `000800`'s column and `uq_disputes_open_per_job`'s
-- predicate, so dropping it here would take out an index this migration never created.

ALTER TABLE disputes
    DROP CONSTRAINT IF EXISTS fk_disputes_resolved_by,
    DROP CONSTRAINT IF EXISTS ck_disputes_resolution,
    DROP CONSTRAINT IF EXISTS ck_disputes_outcome;

DROP INDEX IF EXISTS idx_disputes_resolved;

ALTER TABLE disputes
    DROP COLUMN IF EXISTS resolved_by,
    DROP COLUMN IF EXISTS outcome;
