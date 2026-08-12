-- Reverses SHIP-111.
--
-- The index and the check go with the column, so dropping it is enough; they are named here anyway
-- because a reader of a down migration should be able to see what disappears without reading the up
-- migration beside it.
DROP INDEX IF EXISTS uq_milestones_idempotency;
ALTER TABLE milestones DROP CONSTRAINT IF EXISTS ck_milestones_idempotency_key;
ALTER TABLE milestones DROP COLUMN IF EXISTS idempotency_key;
