-- Reverse of 000801.
--
-- Sessions first: fk_admin_sessions_admin is ON DELETE RESTRICT, and dropping the parent table
-- with a child still pointing at it fails. The indexes and the triggers go with their tables;
-- set_updated_at() itself belongs to 000001 and stays.
--
-- IF EXISTS throughout, because `make migrate-down n=all && make migrate-up` is the documented fix
-- for a database sitting above this block's number, and that sequence runs this file against a
-- database where the tables may already be gone (CLAUDE.md, "Traps").

DROP TABLE IF EXISTS admin_sessions;
DROP TABLE IF EXISTS admin_users;
