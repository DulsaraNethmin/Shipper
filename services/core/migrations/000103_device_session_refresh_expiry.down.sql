-- Reverses SHIP-39's expiry column.
--
-- The CHECK constraint goes with the column, so naming it here would only be a second list to
-- keep in step with the first.
ALTER TABLE device_sessions
    DROP COLUMN IF EXISTS refresh_token_expires_at;
