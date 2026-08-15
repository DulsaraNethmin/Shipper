-- Reverse of 000802.
--
-- The index goes with the table. IF EXISTS because `make migrate-down n=all && make migrate-up` is
-- the documented fix for a database sitting above this block's number, and that sequence runs this
-- file against a database where the table may already be gone (CLAUDE.md, "Traps").

DROP TABLE IF EXISTS admin_notes;
