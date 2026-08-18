-- Every statement uses IF EXISTS: a down migration that fails halfway leaves a schema nobody can
-- describe (SHIP-15g).
--
-- The index goes first even though dropping the column would take it with it, because the two
-- statements are then independent — a tree that has the index and not the column, or the column and
-- not the index, is reversed by this file either way.
DROP INDEX IF EXISTS idx_provider_verification_documents_expiry;

ALTER TABLE provider_verification_documents
    DROP COLUMN IF EXISTS expires_at;
