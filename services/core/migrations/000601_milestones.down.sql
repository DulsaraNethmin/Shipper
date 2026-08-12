-- Reverses SHIP-110.
--
-- The triggers, the index and the foreign key go with the table; the two functions do not,
-- because a function is not a dependent of the table it is attached to and would survive a
-- rollback as an orphan (000401's down migration makes the same point, and the CI round trip
-- counts tables rather than functions, so the leftover stays invisible).
DROP TABLE IF EXISTS milestones;
DROP FUNCTION IF EXISTS milestones_server_clock();
DROP FUNCTION IF EXISTS milestones_is_append_only();
