-- SHIP-97: job-scoped messaging between a customer and a provider.
--
-- Docs/09's *Done when* is "messages attach to a job and are visible only to its two parties and
-- admins", and the whole design follows from reading "its two parties" carefully.
--
-- # A job has one customer and, before an award, several providers — so a conversation is a pair
--
-- An Open job can carry offers from a dozen providers at once. A table keyed on the job alone would
-- put every one of those providers in one room with the customer and with each other, which breaks
-- Docs/01 §4.3's *second* privacy rule — "treat provider bid price as private from competing
-- providers" — through prose rather than through a column. A provider writing "I can do it for less
-- than whoever quoted you 600" would be telling the room.
--
-- So a conversation is scoped to `(job_id, provider_id)`, exactly as a negotiation is. **This is the
-- same reading `bids.provider_id` took at 000502**: that column means *the provider a negotiation is
-- with* rather than the author of any one row, and the author is `offered_by`. This table copies the
-- pair verbatim — `provider_id` names the conversation and `sent_by` names the writer — so the two
-- tables answer "who is this between" and "who wrote this" the same way, and a reader who has
-- understood one has understood the other.
--
-- # There is deliberately no `sender_id`, and no `customer_id` either
--
-- Both are derivable and neither belongs here. The provider side of any row is `provider_id`; the
-- customer side is `jobs.customer_id`, which is another domain's column — and `bids` has carried no
-- copy of it for six tickets, for the reason internal/bidding may not read `jobs` at all. A
-- `sender_id` would be a third copy of a fact two columns already fix, and the first thing to go
-- stale if anything ever moved.
--
-- # It attaches to the negotiation rather than to a bid, and that is the decision worth stating
--
-- `POST /v1/jobs/{id}/bids/{bid_id}/messages` names an offer, but the row records no `bid_id`. A
-- negotiation outlives any one offer — 000502's chain replaces the live row on every counter, and a
-- withdrawn offer can be replaced by a fresh one, so one pair of parties can hold several chains on
-- one job. A conversation that restarted with each of them would lose the question the answer was
-- to. The offer in the URL is how a caller *reaches* the conversation, not what the conversation is
-- about.
--
-- **And messaging is not gated on the offer being live**, which is the other half of the same
-- decision and is enforced by the absence of any status column here. The most useful moment for a
-- customer to ask a question is after the award, when the offer that got them there is `Accepted`
-- and every other one is `Rejected`. Docs/02 §4 keeps the chain readable after a negotiation ends
-- for the same reason.
--
-- # What is deliberately not here
--
--   ???       read receipts, unread counts and "typing". None is in any document, and each needs a
--             per-reader row rather than a column — a different table, decided by whichever ticket
--             first has a screen that needs one.
--   SHIP-103  attachments. Docs/09's row is "both parties exchange messages and counter-offers",
--             and a photograph in a negotiation is an object-storage design (Docs/06 §4.1) rather
--             than a column type.
--   ???       an administrator's own message. Docs/02 §4 makes an administrator a *reader* of a
--             negotiation and nothing here writes as one; `ck_job_messages_sent_by` admits the two
--             parties and would need a third value the day that changes.

CREATE TABLE job_messages (
    id          uuid        PRIMARY KEY,

    -- The job the conversation is attached to. ON DELETE RESTRICT per Docs/10 §3.3 and for
    -- 000500's reason: a job is cancelled rather than removed, and a cascade here would destroy a
    -- commercial record Docs/05 §3.1 requires retaining.
    job_id      uuid        NOT NULL,

    -- The provider the conversation is with — **the negotiation, not the author**. See the header:
    -- this is `bids.provider_id`'s meaning since 000502, copied deliberately.
    --
    -- There is no CHECK that this account's role is 'provider', for the reason 000500 gives about
    -- `bids.provider_id`: a foreign key cannot see another table's column, and the domain enforces
    -- it where a message is written. Nor is there one saying this provider has bid on this job —
    -- that is a comparison across two tables, and internal/bidding makes it in Go by reading the
    -- offer the caller reached the conversation through.
    provider_id uuid        NOT NULL,

    -- Which party wrote this message. `bids.offered_by`'s two values, stored the same way and
    -- admitted by a CHECK for Docs/10 §3.4's reason: ALTER TYPE … ADD VALUE cannot run in a
    -- transaction block alongside its use.
    --
    -- **No DEFAULT**, unlike `bids.offered_by`. That column defaults to 'provider' because a bid
    -- existed before counters did and a default was how 000502 avoided rewriting history. Nothing
    -- precedes this table, and a message whose author was left to a default is a message attributed
    -- by accident.
    sent_by     text        NOT NULL,

    -- What was written, in the sender's own words. Free text, which is the point: Docs/09's
    -- deferral row for this ticket says in as many words that "negotiation happens through
    -- counter-offers alone … parties will want to ask questions".
    body        text        NOT NULL,

    -- The key the message was sent under, so a retry cannot post it twice. See the index below for
    -- why a column is needed when SHIP-15's middleware already exists.
    idempotency_key text,

    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ck_job_messages_sent_by CHECK (sent_by IN ('provider', 'customer')),

    -- Coherence rather than policy, the split 000500, 000501 and 000300 all take. An empty message
    -- is not a value an operator would ever want to permit; the upper bound is 000501's bound on a
    -- bid's `message`, which is the same kind of text written by the same two people in the same
    -- negotiation, so a different number here would be a second opinion nobody decided.
    CONSTRAINT ck_job_messages_body CHECK (length(body) BETWEEN 1 AND 2000),

    -- The same bound httpx puts on the header (maxIdempotencyKeyLen), written out for 000501's
    -- reason: the middleware protects the endpoint, and this protects the table from anything that
    -- ever writes to it without passing through one.
    CONSTRAINT ck_job_messages_idempotency_key CHECK (
        idempotency_key IS NULL OR (length(idempotency_key) BETWEEN 1 AND 255)
    ),

    CONSTRAINT fk_job_messages_job
        FOREIGN KEY (job_id) REFERENCES jobs (id) ON DELETE RESTRICT,

    CONSTRAINT fk_job_messages_provider
        FOREIGN KEY (provider_id) REFERENCES users (id) ON DELETE RESTRICT
);

-- The conversation, oldest first, with the tiebreak the cursor needs.
--
-- **Oldest first is the opposite of every other list in this domain** and it is the right way round
-- for this one: `GET /v1/fleet/bids` is a work queue where the newest matters most, and a
-- conversation is read forward or it is unreadable. It is also `chain`'s order, for the same reason.
--
-- `id` is in the index because it is in the `ORDER BY`. Two parties answering each other inside one
-- millisecond is the ordinary case rather than an edge one — a phone and a laptop replying at once —
-- and a cursor that could not break the tie would repeat or drop a message at exactly the page
-- boundary. That is 000501's argument for `uq_bids_idempotency` applied to an ordering key, and
-- SHIP-101a's `BidCursor` already carries the pair.
--
-- Leading with `job_id` also satisfies Docs/10 §3.3's rule that every foreign key is indexed, for
-- that column.
CREATE INDEX idx_job_messages_conversation
    ON job_messages (job_id, provider_id, created_at, id);

-- The other foreign key. Not covered by the index above, which leads with `job_id`, and needed for
-- the constraint check that runs when a user row is touched.
CREATE INDEX idx_job_messages_provider ON job_messages (provider_id);

-- **The retry half, and it is not the same guarantee the middleware gives.**
--
-- SHIP-15 stores the first response against the key and replays it, which is cheap and is bounded by
-- a TTL and by Redis being a cache. A phone that sent a message, never saw the response and retries
-- an hour later outlives any TTL worth setting — and without this index the retry would post the
-- message a second time. The other party would see it twice and have no way to know which sending
-- was the mistake. Redis makes the retry fast; this makes it correct. Exactly the division 000501
-- draws for a bid and 000602 for a milestone.
--
-- **Scoped by the sending party as well as by the pair**, which is 000502's correction to 000501
-- applied here from the start rather than discovered later. A conversation has two writers inside one
-- `(job_id, provider_id)` pair, so a key without `sent_by` would let the customer's client and the
-- provider's collide on a value both happened to generate — and the second would be answered from the
-- first's row, which is a message that was never sent reported as sent.
CREATE UNIQUE INDEX uq_job_messages_idempotency
    ON job_messages (job_id, provider_id, sent_by, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

COMMENT ON TABLE job_messages IS
    'One message in the conversation between a job''s customer and one provider. Scoped to the (job, provider) pair, not to the job, so competing providers never share a room (SHIP-97, Docs/01 §4.3).';
COMMENT ON COLUMN job_messages.provider_id IS
    'The provider the conversation is with, not the author. bids.provider_id''s meaning since 000502; the author is sent_by.';
COMMENT ON COLUMN job_messages.sent_by IS
    'Which party wrote this message — provider or customer. The two values of bids.offered_by, and deliberately without its DEFAULT.';
COMMENT ON COLUMN job_messages.body IS
    'What was written, in the sender''s own words. The customer''s budget is never exposed to a provider by the platform (Docs/01 §4.3); what a party chooses to type is their own disclosure.';
