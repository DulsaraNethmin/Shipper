-- Reverses SHIP-40.
--
-- The ledger goes first: its foreign key points at device_sessions, and dropping the columns
-- below does not touch that, but the order is the one a reader expects and costs nothing.
-- The two CHECK constraints go with the columns they constrain.
DROP TABLE IF EXISTS consumed_refresh_tokens;

ALTER TABLE device_sessions
    DROP COLUMN IF EXISTS revoked_reason,
    DROP COLUMN IF EXISTS revoked_at;
