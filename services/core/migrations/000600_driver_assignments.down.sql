-- Reverses SHIP-105.
--
-- The triggers, the indexes and the foreign key are dependents of the table and go with it. The
-- function is not — a function is not a dependent of the table it happens to be attached to and
-- would survive as an orphan, which is invisible until a later migration tries to create it
-- again (000401's down migration makes the same point).
DROP TABLE IF EXISTS driver_assignments;
DROP FUNCTION IF EXISTS driver_assignments_identity_is_immutable();
