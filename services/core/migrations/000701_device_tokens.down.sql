-- Reverse of 000701.
--
-- IF EXISTS throughout. A branch whose migrations sit below the applied schema version is
-- reversed with `make migrate-down n=all` before it can be applied at all (SHIP-15g), so this
-- file runs against trees in more than one state.

DROP INDEX IF EXISTS uq_notifications_event_recipient_device;
DROP INDEX IF EXISTS uq_notifications_event_recipient_channel;

-- Back to 000700's rule exactly: one notification per person per channel per event, with no
-- partial predicate. Safe to recreate, because reversing this migration also removes the only
-- thing that writes more than one address per person per channel.
CREATE UNIQUE INDEX IF NOT EXISTS uq_notifications_event_recipient_channel
    ON notifications (event_id, recipient_id, channel);

DROP TRIGGER IF EXISTS device_tokens_set_updated_at ON device_tokens;
DROP TABLE IF EXISTS device_tokens;
