-- Reverses SHIP-115.
--
-- `IF EXISTS` throughout, and it is load-bearing rather than defensive. SHIP-15g's guard refuses a
-- migration numbered below the database's current version, so the way a delivery branch applies
-- this against a tree already carrying a later block's work is `make migrate-down n=all` followed
-- by `make migrate-up` — which runs this file before its own up migration has ever run. Without
-- `IF EXISTS` that leaves the database dirty and the fix is manual.
--
-- The triggers and the indexes go with the table; the function does not, because a function is not
-- a dependent of the table it is attached to and would survive as an orphan (000601 and 000401
-- both make this point, and the CI round trip counts tables rather than functions, so a leftover
-- stays invisible).
--
-- uq_milestones_id_job is dropped after the table, because the composite foreign key references it.
DROP TABLE IF EXISTS proofs;
DROP FUNCTION IF EXISTS proofs_is_append_only();
ALTER TABLE IF EXISTS milestones DROP CONSTRAINT IF EXISTS uq_milestones_id_job;
