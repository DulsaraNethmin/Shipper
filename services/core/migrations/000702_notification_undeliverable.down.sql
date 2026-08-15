-- Reverse of 000702. IF EXISTS throughout; see 000701's down for why.
--
-- Rows that reached `undeliverable` are moved to `failed` rather than dropped. The alternative is a
-- migration that cannot apply — the CHECK would refuse the rows that exist — and deleting a record
-- that somebody was not told something is the one thing this table must never do.

UPDATE notifications SET status = 'failed', last_error = coalesce(last_error, 'undeliverable')
 WHERE status = 'undeliverable';

ALTER TABLE notifications
    DROP CONSTRAINT IF EXISTS ck_notifications_status;

ALTER TABLE notifications
    ADD CONSTRAINT ck_notifications_status CHECK (status IN ('pending', 'sent', 'failed'));

DROP INDEX IF EXISTS idx_notifications_undelivered;

CREATE INDEX IF NOT EXISTS idx_notifications_undelivered
    ON notifications (created_at) WHERE status <> 'sent';
