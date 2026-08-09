-- Reverses 000001_init.up.sql.
--
-- Dropped in the opposite order to creation. The function is removed before the
-- extension because a later migration may have built something on top of both, and
-- unwinding in reverse is the only order that stays correct as the schema grows.

DROP FUNCTION IF EXISTS set_updated_at();

-- Guarded rather than unconditional: if a later migration has already created a citext
-- column, dropping the extension would fail and abort the rollback. RESTRICT is the
-- default and is the behaviour wanted here — a rollback that silently drops user columns
-- would be far worse than one that stops and says why.
DROP EXTENSION IF EXISTS citext;
