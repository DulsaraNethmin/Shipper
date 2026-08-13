-- Reverses SHIP-57.
--
-- The triggers are named explicitly rather than left to the table: 000400 owns `jobs` and would
-- take them with it, but a rollback of this migration alone must not depend on that. Functions
-- are never dependents of a table and always have to be dropped by name.
DROP TRIGGER IF EXISTS jobs_status_change_is_guarded ON jobs;
DROP TRIGGER IF EXISTS jobs_starts_as_a_draft ON jobs;

DROP FUNCTION IF EXISTS jobs_status_change_is_guarded();
DROP FUNCTION IF EXISTS jobs_starts_as_a_draft();
