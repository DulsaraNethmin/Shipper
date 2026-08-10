-- Reverses SHIP-149.
--
-- The triggers go with the table, but the function does not — it is a schema-level object and
-- would survive as an orphan.
--
-- Dropping an audit log is not something that should ever happen outside a rollback of this
-- migration on an empty database. That it is possible here is a property of migrations being
-- reversible, not a supported operation.
DROP TABLE IF EXISTS audit_log;
DROP FUNCTION IF EXISTS audit_log_is_append_only();
