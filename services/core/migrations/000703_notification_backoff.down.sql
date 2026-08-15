-- Reverse of 000703. IF EXISTS throughout; see 000701's down for why.

DROP INDEX IF EXISTS idx_notifications_undelivered;

ALTER TABLE notifications
    DROP COLUMN IF EXISTS next_attempt_at;

CREATE INDEX IF NOT EXISTS idx_notifications_undelivered
    ON notifications (created_at) WHERE status NOT IN ('sent', 'undeliverable');
