-- Reverses SHIP-57a.
--
-- The triggers go with the table; the function does not, because a function is not a dependent
-- of the table it happens to be attached to and would survive a rollback as an orphan. The CI
-- round trip counts tables rather than functions, so this is the kind of leftover that stays
-- invisible until a later migration tries to create it again.
DROP TABLE IF EXISTS job_status_history;
DROP FUNCTION IF EXISTS job_status_history_is_append_only();
