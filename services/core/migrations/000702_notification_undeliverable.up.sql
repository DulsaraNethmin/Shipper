-- SHIP-139: a fourth notification status, for the address that no longer exists.
--
-- # The status this table was missing, and the ticket that found it
--
-- 000700 gave the dispatcher three states — pending, sent, failed — and made `failed` deliberately
-- non-terminal: "a failed row is claimed again on the next pass, because a channel that was down
-- for a minute is the ordinary failure and giving up after one attempt would lose exactly the
-- notification Docs/01 §4.5 says must not be lost".
--
-- That is right for every failure email can produce and wrong for the one push produces constantly.
-- FCM rejects a device token whenever an app is uninstalled or its data is cleared, which happens
-- across a real install base every hour of every day. Under three statuses that row is either:
--
--   * `failed`, and therefore claimed on every pass forever, retrying a handset that no longer
--     exists and counting itself into whatever SHIP-176 alerts on — which is
--     internal/platform/push/doc.go's "an alert that fires forever and is eventually ignored,
--     including on the day it means something"; or
--   * `sent`, which is a lie, and a lie that a support query cannot see through.
--
-- So there is a fourth: **undeliverable — the address is gone, no retry can help, and nobody needs
-- telling.** It is terminal like `sent` and truthful like `failed`, and it is what the dispatcher
-- writes when the push provider says a token is dead. The device is deregistered in the same
-- transaction (000701).
--
-- # It is deliberately not reachable from email
--
-- A bounce is not the same fact. An address that hard-bounces today may be a mailbox that was full,
-- and the platform learns about it asynchronously through a webhook this MVP does not have
-- (Docs/01 §8). Nothing marks an email row undeliverable, and whoever builds bounce handling should
-- decide separately whether it belongs here or in a column of its own.

ALTER TABLE notifications
    DROP CONSTRAINT ck_notifications_status;

ALTER TABLE notifications
    ADD CONSTRAINT ck_notifications_status CHECK (status IN (
        'pending',        -- written by the consumer
        'sent',           -- the channel accepted it
        'failed',         -- the send did not land; not terminal, the next pass claims it again
        'undeliverable'   -- the address is gone (SHIP-139); terminal, and never retried
    ));

-- The claim's partial index has to match the claim's predicate or it stops serving it, and a
-- sequential scan over every notification the platform has ever sent is the failure that only shows
-- up once there are a million of them.
--
-- `NOT IN` rather than `<> 'sent'`, listing the two terminal states. The dispatcher's own query is
-- changed to match in the same commit; internal/notifications/dispatch.go's DispatchClaim is the
-- string, and a test holds the two together.
DROP INDEX idx_notifications_undelivered;

CREATE INDEX idx_notifications_undelivered
    ON notifications (created_at) WHERE status NOT IN ('sent', 'undeliverable');

COMMENT ON COLUMN notifications.status IS
    'pending, sent, failed (retried) or undeliverable (terminal — the address is gone, SHIP-139).';
