-- Reverses SHIP-38.
--
-- The trigger, the indexes and the foreign key go with the table; DROP TABLE takes its own
-- dependents with it, and naming them here would only be a list to keep in step.
DROP TABLE IF EXISTS device_sessions;
