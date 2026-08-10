-- SHIP-134's table, built with the foundation because three earlier tickets write to it.
--
-- Docs/06 §4.0 sets out why the outbox exists rather than a direct publish to Kafka: the two
-- cannot be made atomic, and both failure modes are real — publish then fail to commit and a
-- consumer acts on an award that never happened; commit then fail to publish and the winning
-- provider is never told. Writing the event in the same transaction as the state change makes
-- the transaction the only thing that has to be atomic, which it already is.
--
-- The table arrives now, well before the publisher, because SHIP-57, SHIP-69 and SHIP-89 all
-- emit events and would otherwise each invent a mechanism for SHIP-136 to rewrite.

CREATE TABLE outbox (
    -- UUIDv7, so rows sort by time without a sequence, and so a consumer has a stable key to
    -- deduplicate on. Delivery is at-least-once: the publisher can crash between the broker
    -- acknowledging and this row being marked, so some events are published twice and every
    -- consumer must recognise them (Docs/06 §4.0).
    id              uuid        PRIMARY KEY,

    -- What the event is about. Ordering is promised per aggregate, not globally — events for
    -- one job publish in the order written, and nothing in the MVP needs more than that. A
    -- global order would mean a single Kafka partition and a throughput ceiling for no gain.
    aggregate_type  text        NOT NULL,
    aggregate_id    uuid        NOT NULL,

    -- '<aggregate>.<past tense>': 'job.published', 'bid.accepted'. Schema versioning belongs to
    -- SHIP-135, not to this string.
    event_type      text        NOT NULL,

    payload         jsonb       NOT NULL,

    -- When the change happened, from the domain's injected clock — distinct from when the row
    -- was written and from when it was published. Docs/02 §3.1 makes the same distinction for
    -- milestones, and for the same reason: the actor's clock and the server's are not the same
    -- clock.
    occurred_at     timestamptz NOT NULL,

    created_at      timestamptz NOT NULL DEFAULT now(),

    -- NULL until the broker has acknowledged it. This is the publisher's only state.
    published_at    timestamptz
);

-- The publisher's query: the oldest unpublished events, in order.
--
-- Partial, so the index holds only the backlog rather than the entire history of the system.
-- Once the publisher is keeping up this index is nearly empty, which is what keeps the claim
-- cheap no matter how many million rows have been through the table.
CREATE INDEX idx_outbox_unpublished
    ON outbox (occurred_at, id)
    WHERE published_at IS NULL;

-- Replaying or auditing what happened to one job.
CREATE INDEX idx_outbox_aggregate
    ON outbox (aggregate_type, aggregate_id, occurred_at);

COMMENT ON TABLE outbox IS
    'Transactional outbox (Docs/06 §4.0). Written by the domain inside the transaction that made the change; drained by the publisher in SHIP-134. At-least-once, so consumers must be idempotent.';
