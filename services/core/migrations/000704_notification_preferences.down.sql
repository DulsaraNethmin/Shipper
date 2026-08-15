-- Reverse of 000704. IF EXISTS throughout; see 000701's down for why.
--
-- Dropping this table loses every mute, and there is no way to make that untrue: the rows *are* the
-- preference. That is the ordinary consequence of reversing a migration that created a table, and it
-- is worth saying only because a preference is a statement a person made rather than data the
-- platform derived.

DROP TABLE IF EXISTS notification_preferences;
