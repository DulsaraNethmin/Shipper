-- Every statement uses IF EXISTS: a down migration that fails halfway leaves a schema nobody can
-- describe (SHIP-15g).
--
-- The trigger on `users` goes first. It is the one object this migration put on another domain's
-- table, and leaving it behind would make every later provider registration fail with a foreign-key
-- violation against a table that no longer exists.
DROP TRIGGER IF EXISTS provider_verification_on_registration ON users;
DROP FUNCTION IF EXISTS provider_verification_on_registration();

DROP FUNCTION IF EXISTS provider_verification_decide(uuid, text, text, uuid, text);

DROP TABLE IF EXISTS provider_verification_decisions;
DROP TABLE IF EXISTS provider_verifications;

DROP FUNCTION IF EXISTS provider_verification_decisions_is_append_only();
DROP FUNCTION IF EXISTS provider_verification_change_is_guarded();
DROP FUNCTION IF EXISTS provider_verification_starts_as_pending();
