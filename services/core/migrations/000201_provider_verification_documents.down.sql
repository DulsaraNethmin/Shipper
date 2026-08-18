-- Every statement uses IF EXISTS: a down migration that fails halfway leaves a schema nobody can
-- describe (SHIP-15g).
--
-- The table goes before the function it uses, because dropping a table drops its triggers with it
-- and a function still referenced by a trigger cannot be dropped.
DROP TABLE IF EXISTS provider_verification_documents;

DROP FUNCTION IF EXISTS provider_verification_documents_is_append_only();
