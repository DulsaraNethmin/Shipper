-- SHIP-84: what an offer commits to, and the two indexes that make "bid once per job" true.
--
-- 000500 built the table narrow on purpose and named this ticket four times: "the timing an offer
-- commits to — when the provider can collect and when they will deliver — together with any message
-- that accompanies it. 'Price and timing' is that ticket's *Done when*, and the timing half is a
-- shape … rather than a column type." This is that shape, and the reasoning for it.
--
-- # The timing is two instants, not two windows
--
-- A job carries two *windows*, because a customer says "any time on Thursday". A bid answers with
-- two *instants*, because a provider says "I will be there at nine and it will be there by five".
-- Three reasons, in the order they weighed.
--
--   1. **Docs/03's provider row is "sets price, timing, and conditions", and the customer row is
--      "present comparable price, timing, vehicle, profile".** Comparable is the operative word.
--      Two instants sort; four numbers do not, and a customer comparing five bids would be reading
--      twenty timestamps.
--   2. **A window is a weaker promise than the thing being promised.** The customer already stated
--      the flexibility they have; a provider restating it back is not an offer, it is a repetition.
--      A provider who wants slack says a later deliver_by, which is a commitment they can be held
--      to rather than a range they cannot.
--   3. **It is the reversible direction.** Widening an instant into a window later is two nullable
--      columns and an additive response field. Narrowing a stored window into an instant is a data
--      migration over offers people have already made, and there is no honest answer for which end
--      of the window the commitment was.
--
-- **The bid's timing is deliberately not constrained to the job's windows.** A provider offering a
-- pickup outside the window the customer asked for is making an offer the customer is free to
-- decline, and Docs/01 §4.3's answer to a mismatched bid is better job detail rather than a refusal.
-- Roughly half the jobs in the fixture set state no window at all. This is the same coherence/policy
-- split 000500 and 000300 take: what cannot be true is a constraint, what somebody might change is
-- not.
--
-- # What is still deliberately not here, and who it belongs to
--
--   SHIP-85…88  the supersede chain, and any widening of the two predicates below. See the note on
--               uq_bids_one_submitted_per_provider_per_job.
--   SHIP-89     bid expiry "on their own terms", which needs a column saying what those terms are.
--   SHIP-92     when the award happened, and by whom.
--   ???         **the vehicle or vehicles a bid is offered on.** 000500 handed this decision to
--               SHIP-84 and SHIP-84 is declining it rather than guessing: it is not in this ticket's
--               *Done when* ("price and timing"), and Docs/01 §4.3 wants it for the *customer's*
--               comparison, which is SHIP-102. Validating it needs a second `fleet` fact — that the
--               vehicle is the caller's and in service — and therefore a second port, which is more
--               design than a three-point ticket should be taking on somebody else's behalf.
--               Docs/11 §3 records it as an open seam with the trigger named.

-- The timing an offer commits to. Nullable for the reason `amount` is nullable: 000500 lets a Draft
-- be incomplete, because refusing an incomplete row is refusing to save what somebody has typed so
-- far. ck_bids_offer_has_timing below is what makes that coherent.
ALTER TABLE bids ADD COLUMN pickup_at  timestamptz;
ALTER TABLE bids ADD COLUMN deliver_by timestamptz;

-- The conditions that accompany the offer, in the provider's own words — Docs/03's third item after
-- price and timing. Free text on purpose: "I can do Thursday if the lift is working" is not a field.
ALTER TABLE bids ADD COLUMN message text;

-- The key the offer was placed under, so a retry cannot place a second one. See the index below for
-- why a column is needed at all when SHIP-15's middleware already exists.
ALTER TABLE bids ADD COLUMN idempotency_key text;

-- Coherence, not policy. Delivering before collecting is not a value an operator would ever want to
-- permit; "how far ahead an offer may be scheduled" is a number operations changes, so it lives in
-- the validator.
ALTER TABLE bids ADD CONSTRAINT ck_bids_timing_is_ordered CHECK (
    pickup_at IS NULL OR deliver_by IS NULL OR deliver_by > pickup_at
);

-- **There is deliberately no `ck_bids_offer_has_timing`, and this note is the whole reason.**
--
-- The obvious twin of ck_bids_offer_has_an_amount — `status = 'Draft' OR (pickup_at IS NOT NULL AND
-- deliver_by IS NOT NULL)` — was written, applied, and then removed. It is worth recording why,
-- because it looks like an omission and reads as an inconsistency with the constraint directly above
-- it in 000500.
--
-- **It binds a design SHIP-87 has not made yet, which is exactly what 000500 declined to do.** A
-- counter-offer is a new row in this table (Docs/02 §4 — each counter supersedes the prior offer),
-- and whether a customer countering on *price alone* restates the timing or inherits it from the
-- offer it supersedes is that ticket's decision. A constraint here would settle it, in the direction
-- that costs SHIP-87 a migration to undo. 000500's own header is explicit about this trade for the
-- index it declined to write: "a partial index written against a guess at that definition is a
-- constraint the ticket would have to drop."
--
-- **The evidence that it was over-reaching was immediate rather than theoretical**: it failed six of
-- SHIP-80's own migration tests, which insert Submitted and Accepted bids to exercise ck_bids_status
-- and uq_bids_one_accepted_per_job and have no interest in timing. Making them pass would have meant
-- editing another ticket's test file to accommodate a constraint that ticket had considered and left
-- out.
--
-- **What enforces "price and timing" instead is the validator**, in internal/bidding, where SHIP-84's
-- *Done when* actually lives. That is also the better answer for a caller: a missing date comes back
-- as a field error naming `pickup_at` rather than as a constraint violation nobody can act on.
--
-- The one timing rule that *is* a constraint is the one above, and the difference is the test: nothing
-- any future ticket designs could want a delivery that precedes its own collection, so
-- ck_bids_timing_is_ordered binds nobody's design. "An offer must state its timing" is a product rule,
-- and product rules that a later ticket might legitimately vary do not belong in the schema.

-- The same bound httpx puts on the header (maxIdempotencyKeyLen), so a value reaching the column has
-- already been refused at the edge if it is malformed. Written out rather than trusted, exactly as
-- 000602 does for milestones: the middleware protects the endpoint, and this protects the table from
-- anything that ever writes to it without passing through one.
ALTER TABLE bids ADD CONSTRAINT ck_bids_idempotency_key CHECK (
    idempotency_key IS NULL OR (length(idempotency_key) BETWEEN 1 AND 255)
);

-- A bound on the message, for the reason 000404's text columns have one: protection against a client
-- with a runaway field, not a judgement about how much a provider should write.
ALTER TABLE bids ADD CONSTRAINT ck_bids_message CHECK (
    message IS NULL OR (length(message) BETWEEN 1 AND 2000)
);

-- **"A verified, eligible provider can bid once per job." This index is the "once".**
--
-- 000500 declined to write it and said exactly why: "'active' is defined by the supersede design
-- that SHIP-87 and SHIP-88 own, and a partial index written against a guess at that definition is a
-- constraint the ticket would have to drop." That warning is honoured rather than overridden — the
-- predicate below **is not a guess at "active"**. It names one status, 'Submitted', which is the
-- only status this endpoint writes and the only one that exists today meaning "a live offer from
-- this provider that the customer has not answered".
--
-- Every other status leaves the predicate by itself, so nothing later has to drop this:
--
--   Countered / Superseded  SHIP-87 and SHIP-88 move the prior offer out before writing the next
--   Accepted                the award (SHIP-92), which uq_bids_one_accepted_per_job already governs
--   Rejected                the customer declining, and every competing bid closed by an award
--   Withdrawn               the provider taking it back (SHIP-86)
--   Expired                 the offer running out on its own terms (SHIP-89)
--
-- If SHIP-87 finds it needs a wider predicate — 'Submitted' or 'Countered', say — that is one extra
-- value in an additive migration, not a constraint to drop. That is the whole difference between
-- naming a status and guessing at a word.
--
-- **A withdrawn bid may be replaced, and that is a decision rather than a consequence.** Once
-- SHIP-86 exists, a provider who withdraws leaves the predicate and may bid again. Docs/01 §4.2 says
-- "place, update, and withdraw a bid until it is accepted or expires" and forbids nothing about
-- what follows a withdrawal; refusing the second bid would mean a fat-fingered price is a job the
-- provider can never bid on again. The permissive direction is also the reversible one.
--
-- A unique *index* rather than a constraint because the rule is partial and a constraint cannot be —
-- the same reason uq_bids_one_accepted_per_job is one, and with the same consequence: there is no
-- constraint name for a writer to name.
CREATE UNIQUE INDEX uq_bids_one_submitted_per_provider_per_job
    ON bids (job_id, provider_id)
    WHERE status = 'Submitted';

-- **And this index is the "retry" half, which is not the same guarantee.**
--
-- SHIP-15's middleware stores the first response against the key and replays it. That makes a retry
-- cheap and it is bounded by a TTL and by Redis being a cache. It is also *not* what the index above
-- covers: a provider whose bid was recorded and whose response never arrived retries, the index
-- above refuses the second row, and without this one the honest answer would be "you have already
-- bid" — a 409 for a request that actually succeeded. The client would show a failure for a bid that
-- is live.
--
-- So the key is stored on the row, and a retry that outlives the cache is answered from the record
-- rather than from the constraint. Redis makes the retry fast; this makes it correct. Exactly the
-- division 000602 draws for milestones.
--
-- **Scoped by provider as well as by job, which is where this differs from 000602.** Only the
-- awarded provider writes a milestone, so job_id alone already scopes that key to one caller. A job
-- has many bidders, so job_id alone does not scope this one: two providers whose clients happened to
-- generate the same key would collide, and the second would be handed the first's bid. provider_id
-- is what makes a key unable to reach another caller's offer. job_id is in the key for the opposite
-- reason — a client reusing one value across two jobs has made two requests that both deserve to
-- succeed.
CREATE UNIQUE INDEX uq_bids_idempotency
    ON bids (job_id, provider_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

COMMENT ON COLUMN bids.pickup_at IS
    'When the provider commits to collecting. An instant rather than a window: the job states the customer''s flexibility, the bid states a commitment (SHIP-84).';
COMMENT ON COLUMN bids.deliver_by IS
    'When the provider commits to having delivered (SHIP-84).';
COMMENT ON COLUMN bids.message IS
    'The conditions accompanying the offer, in the provider''s own words (Docs/03, SHIP-84).';
COMMENT ON COLUMN bids.idempotency_key IS
    'The key this offer was placed under. A retry is answered from the row rather than refused by uq_bids_one_submitted_per_provider_per_job (SHIP-84).';
