-- SHIP-137: one row per person per channel per event, and the row is what makes the consumer
-- idempotent.
--
-- This is the notifications block's first migration. 700-799 was reserved from the start
-- (migrations/blocks.go) and has been empty for nine waves, because everything M5 had built so far
-- — the outbox (SHIP-134), the topic set and the catalogue (SHIP-135), the emission from the
-- domains (SHIP-136) — lives in the shared block or in no table at all.
--
-- # Why this table exists at all, rather than sending as the event is read
--
-- Docs/06 §4.0 makes delivery at-least-once and internal/notifications/doc.go turns that into a
-- rule: "every consumer is idempotent, because the alternative to a duplicate notification is a
-- missing one". At-least-once is not a caveat, it is a guarantee — cmd/worker/outbox.go names the
-- exact window in which a publish succeeds and the commit does not, and the next pass republishes.
-- So a consumer *will* see the same event twice, and one that sent on receipt would send twice.
--
-- uq_notifications_event_recipient_channel below is what makes that impossible, and it is a
-- database constraint rather than a check in Go for the reason Docs/06 §4.1 gives about the
-- one-accepted-bid index: a second consumer instance racing the first is exactly the case
-- application logic gets wrong and a unique index cannot.
--
-- The second thing it buys is Docs/01 §4.5: a notification failure must not lose the event. The
-- row is written and committed *before* anything is sent, so a channel that is down leaves a
-- pending row rather than a dropped message, and the next dispatch pass finds it. The event also
-- survives in the outbox and on the topic regardless — but neither of those knows that a
-- particular person has not been told, and this does.
--
-- # What this table is not
--
-- It is not a second outbox. The outbox is the record that something happened; this is the record
-- that somebody was told. One event produces several rows here and none of them is the event.
--
-- It carries no device token. SHIP-140 owns those, behind SHIP-139's Firebase adapter, and neither
-- exists — see the `push` value on ck_notifications_channel below.

CREATE TABLE notifications (
    id uuid NOT NULL PRIMARY KEY,

    -- The event this came from, as internal/events.Event.ID: a UUIDv7 assigned when the row was
    -- written into the outbox, and therefore the same identifier however many times the publisher
    -- puts the message on the topic. That is what makes it the deduplication key.
    --
    -- Deliberately no foreign key to outbox. The outbox is drained and, in time, pruned; a
    -- notification must outlive the event that caused it, for the same reason audit_log has no
    -- foreign key on its actor (000003) — this record must survive its subject.
    event_id uuid NOT NULL,

    -- The event type, carried so a row is legible without joining anything: `bid.placed`,
    -- `job.status_changed`. Not constrained, and that is a decision rather than an omission — the
    -- catalogue in internal/events is the authority on which types exist, it is populated by init
    -- functions in three domains, and a CHECK here would be a fourth copy of that list which only
    -- a migration could correct. Docs/10 §3.4 asks for text-with-a-CHECK on an enumeration this
    -- schema owns; this one it does not own.
    event_type text NOT NULL,

    -- The job the notification is about, for support and for the deep link Docs/01 §4.5 requires
    -- of a push. Every event in the catalogue today is about a job, directly or through its bid or
    -- its delivery, so this is NOT NULL; an event about something else would be a schema change
    -- rather than a NULL in this column.
    --
    -- No foreign key, again on 000003's reasoning: pruning or pseudonymising a job (Docs/05 §3.1,
    -- SHIP-171) must not be blocked by, or cascade into, the record of who was told what.
    job_id uuid NOT NULL,

    -- Who is being told. A real account, so the foreign key is here: a notification addressed to
    -- somebody who does not exist is a bug rather than a record worth keeping, and Docs/10 §3.3
    -- makes it ON DELETE RESTRICT because SHIP-171 pseudonymises rather than deletes.
    recipient_id uuid NOT NULL,

    -- How they are being told. See the CHECK below for why `push` is a value with no sender.
    channel text NOT NULL,

    -- What kind of notification this is, which is the unit SHIP-142 lets a user mute.
    category text NOT NULL,

    -- Whether this one may be muted. Docs/01 §4.5 calls its list "essential events", and
    -- SHIP-142's *Done when* is that essential events cannot be muted — so the rule has to travel
    -- with the row rather than be re-derived at send time from a table somebody has since edited.
    --
    -- Stored rather than looked up for the same reason internal/events writes the schema version
    -- into the payload: this is a property of the notification as it was decided, and a row
    -- written before a rule change must not be reinterpreted by the rule that came after it.
    essential boolean NOT NULL,

    -- Where it goes: an email address, or an E.164 number. Resolved once, when the row is written,
    -- from users — so a dispatch retry days later reaches the address the platform had when the
    -- event happened rather than re-reading a row that may since have changed. That is the same
    -- argument, again, and it is the one this table is built on throughout.
    address text NOT NULL,

    -- What it says. Rendered when the row is written, from the event and from nothing else.
    --
    -- **This is where Docs/01 §4.4's privacy rule and SHIP-141 are enforceable rather than
    -- intended**: no address, goods description or full customer name appears in a notification
    -- body, and the structural reason is that the renderer never reads the job. Its only inputs
    -- are the event payload — which the catalogue shows carries none of those three — the job
    -- identifier and the status names. A body that named a street would have to have come from
    -- somewhere, and there is nowhere for it to come from.
    --
    -- SHIP-138 owns the real copy and the templates. What is here is deliberately austere.
    subject text NOT NULL,
    body text NOT NULL,

    -- Where it has got to. See ck_notifications_status.
    status text NOT NULL DEFAULT 'pending',

    -- How many times a send has been attempted, so a row that keeps failing is visible in one
    -- query rather than by reading a log. SHIP-176 alerts on a rise in undelivered notifications
    -- and this is the column it will count.
    attempts integer NOT NULL DEFAULT 0,

    -- The last failure, kept so that "it did not send" is answerable without the process that
    -- tried still being alive.
    last_error text,

    -- When the platform accepted the send. The platform clock throughout: unlike a milestone
    -- (Docs/02 §3.1) there is no actor with a clock of their own here — a consumer is not a
    -- person — so there is one clock and no reason for two columns.
    sent_at timestamptz,

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    -- `push` is in the vocabulary and nothing can send it today, which is deliberate.
    --
    -- Docs/01 §4.5 makes push the primary channel for customers and providers. The Firebase
    -- adapter is SHIP-139 and the device token registry is SHIP-140, and neither exists — so
    -- there is no address a push row could carry, and the routing rules in
    -- internal/notifications/rules.go therefore produce none. The value is here rather than added
    -- later because a value absent from a vocabulary decodes as unknown on the day something
    -- starts writing it, which is the argument Docs/02 §4 already makes for keeping `Countered`.
    CONSTRAINT ck_notifications_channel CHECK (channel IN ('email', 'sms', 'push')),

    -- pending is written by the consumer, sent and failed by the dispatcher. `failed` is not
    -- terminal: a failed row is claimed again on the next pass, because a channel that was down
    -- for a minute is the ordinary failure and giving up after one attempt would lose exactly the
    -- notification Docs/01 §4.5 says must not be lost. What stops an infinite retry is somebody
    -- looking at `attempts`, which is why that column is not a boolean.
    CONSTRAINT ck_notifications_status CHECK (status IN ('pending', 'sent', 'failed')),

    -- The four categories of internal/notifications.Categories, paired with the Go list in both
    -- directions by TestNotificationCategoryConstraintMatchesTheGoConstants (Docs/10 §3.4).
    CONSTRAINT ck_notifications_category CHECK (category IN (
        'bidding',
        'award',
        'delivery',
        'job_expiry'
    )),

    -- A sent notification has an instant; an unsent one has none. Without this, `sent` and
    -- sent_at IS NULL is a state the dispatcher could reach by forgetting one of two assignments,
    -- and it would look exactly like a successful send in every report that counts by status.
    CONSTRAINT ck_notifications_sent_at CHECK (
        (status =  'sent' AND sent_at IS NOT NULL) OR
        (status <> 'sent' AND sent_at IS NULL)
    ),

    -- Nothing is dispatched to nowhere. An empty address would be a row the dispatcher claims
    -- every pass and can never complete.
    CONSTRAINT ck_notifications_address CHECK (length(address) > 0),

    CONSTRAINT fk_notifications_recipient
        FOREIGN KEY (recipient_id) REFERENCES users (id) ON DELETE RESTRICT
);

-- The whole reason this table exists: one notification per person per channel per event, enforced
-- by the database.
--
-- A consumer that saw the same event twice inserts and is refused the second time; two consumer
-- instances racing on the same message resolve to one row without coordinating. Neither outcome
-- needs the consumer to have remembered anything, which is what makes it hold across a restart, a
-- rebalance and a redeployment.
CREATE UNIQUE INDEX uq_notifications_event_recipient_channel
    ON notifications (event_id, recipient_id, channel);

-- The dispatcher's claim, partial on exactly its predicate, for the reason idx_jobs_open_expiry is
-- (000406): a marketplace keeps every notification it has ever sent and the queue of unsent ones is
-- a handful of rows. Ordered by created_at so the oldest goes first, which is the order somebody
-- waiting for a message would choose.
CREATE INDEX idx_notifications_undelivered
    ON notifications (created_at) WHERE status <> 'sent';

-- Docs/10 §3.3 requires every foreign key to be indexed, and this is also the support query:
-- everything one account was told, newest first.
CREATE INDEX idx_notifications_recipient
    ON notifications (recipient_id, created_at DESC);

-- Docs/10 §3.3: a mutable table with the column and no trigger is a defect, and
-- TestEveryMutableTableHasItsUpdatedAtTrigger catches it.
CREATE TRIGGER notifications_set_updated_at
    BEFORE UPDATE ON notifications
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON TABLE notifications IS
    'One row per person per channel per domain event (SHIP-137). The unique index on (event_id, recipient_id, channel) is what makes an at-least-once consumer idempotent.';
COMMENT ON COLUMN notifications.event_id IS
    'internal/events.Event.ID — stable across republication, which is why it is the deduplication key.';
COMMENT ON COLUMN notifications.essential IS
    'Whether SHIP-142 will let the recipient mute this. Stored rather than re-derived, so a rule change does not reinterpret a row written before it.';
COMMENT ON COLUMN notifications.body IS
    'Rendered from the event alone. The renderer never reads the job, which is what makes SHIP-141 structural rather than remembered.';
