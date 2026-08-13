-- SHIP-87 and SHIP-88: the supersede chain, and the two CHECKs that make "only the latest valid
-- offer is acceptable" something the award transaction cannot get wrong.
--
-- 000500 named this migration twice and 000501 twice more, both times declining to guess:
--
--   000500  "the supersede chain. A counter-offer supersedes the prior offer (Docs/02 §4) and the
--            whole chain stays readable, which needs a link between rows. Whether that is a
--            self-referencing column or a separate offer table is SHIP-87 and SHIP-88's design."
--   000501  "whether a customer countering on *price alone* restates the timing or inherits it from
--            the offer it supersedes is that ticket's decision."
--
-- Both are decided here, and this header is the reasoning rather than the announcement.
--
-- # A counter is a new row in this table, and the link points backwards from the displaced one
--
-- The new row was never in question: Docs/02 §4 has each counter *superseding* the prior offer, and
-- 000500 already reads it that way — "a customer's counter-offer arrives as a new row that was never
-- a draft". What had to be chosen is where the link lives, and the two candidates are not
-- equivalent.
--
--   `supersedes_bid_id` on the **new** row  — the chain read forwards is a self-join, and "has this
--                                            offer been displaced?" is a question about some *other*
--                                            row. No single-row constraint can express it.
--   `superseded_by`     on the **displaced** row — "this offer has been displaced" becomes a fact
--                                            *in the row itself*, which a CHECK can read.
--
-- **The second wins because of what it makes possible, not because of what it stores.** With the
-- link on the displaced row, `ck_bids_superseded_is_not_live` below is an ordinary column
-- constraint, and SHIP-92 physically cannot accept an offer that has been countered — the database
-- refuses the `UPDATE`, whatever the award transaction remembers to check. The other direction would
-- have left that rule as application logic with a comment asking somebody to keep it.
--
-- This is the same trade 000500 made for `uq_bids_one_accepted_per_job` and stated in its own words:
-- "enforced by a database constraint, not application logic alone", where the "not alone" is the
-- load-bearing half.
--
-- # A separate `offers` table was considered and rejected
--
-- 000500 named it as the alternative. It would have given the chain a home of its own and left
-- `bids` as the negotiation. It is rejected because `uq_bids_one_accepted_per_job` — the invariant
-- CLAUDE.md names and Docs/11 §8 builds the whole award branch against — is an index on **this**
-- table, and moving offers out of it would mean either moving that index to a table SHIP-91 was
-- never written against, or keeping the accepted amount in two places. Neither is worth a normal
-- form. Every status in `ck_bids_status` is a status of an *offer*; the table has been the chain all
-- along and only lacked the link.
--
-- # What is still deliberately not here
--
--   SHIP-89  bid expiry "on their own terms", which needs a column saying what those terms are. It
--            fits without change: expiry moves the head from 'Submitted' to 'Expired', which leaves
--            `uq_bids_one_submitted_per_provider_per_job`'s predicate and satisfies every constraint
--            below.
--   SHIP-90  `Open → Negotiating`. Countering does not move the job, for the reason placing a bid
--            does not (Docs/11 §3, SHIP-84).
--   SHIP-92  when the award happened, and by whom.
--   ???      `ck_bids_offer_has_timing`. 000501 removed it because it bound this ticket's design,
--            and this ticket has now made that design — a counter inherits the timing it does not
--            restate, so every offer past 'Draft' that this platform writes names both instants.
--            The constraint is **still not added**, because 000501's other finding still holds: it
--            failed six of SHIP-80's own migration tests, which insert 'Submitted' and 'Accepted'
--            bids with no timing to exercise `ck_bids_status`, and making them pass means editing
--            another ticket's test file. Docs/11 §3 records it as answerable now and names the cost.

-- Who made this offer: the provider bidding, or the customer answering them.
--
-- **`provider_id` keeps its meaning as the party, and this column is the author.** That is the one
-- reinterpretation this migration makes, and it is worth stating plainly because the alternative is
-- much worse. A negotiation is one `(job_id, provider_id)` pair, and every row in the chain carries
-- that pair — including the customer's counters. If a customer's counter carried the *customer's*
-- id in `provider_id` instead, `uq_bids_one_submitted_per_provider_per_job` would stop meaning "one
-- live offer in this negotiation", `idx_bids_provider` would stop serving the provider's own bid
-- list, and `fk_bids_provider` would point at a customer. One column moves; four objects keep
-- working.
--
-- DEFAULT 'provider' so that every row 000500 and 000501 wrote is correctly attributed without a
-- backfill statement — a bid placed through `POST /v1/jobs/{id}/bids` is the provider's offer, and
-- there is no other writer.
--
-- text with a CHECK rather than an enum type, per Docs/10 §3.4, and the two values are the ones
-- `users.role` and `job_status_history.actor_type` already use — lower case, because they are role
-- names rather than Docs/02 statuses.
ALTER TABLE bids ADD COLUMN offered_by text NOT NULL DEFAULT 'provider';

ALTER TABLE bids ADD CONSTRAINT ck_bids_offered_by CHECK (
    offered_by IN ('customer', 'provider')
);

-- The offer that displaced this one, or NULL if this is the live head of its chain.
--
-- ON DELETE RESTRICT for the reason `fk_bids_job` has it: there is no delete on this table, and a
-- cascade would destroy the record Docs/01 §4.3 requires the platform to keep.
ALTER TABLE bids ADD COLUMN superseded_by uuid;

ALTER TABLE bids ADD CONSTRAINT fk_bids_superseded_by
    FOREIGN KEY (superseded_by) REFERENCES bids (id) ON DELETE RESTRICT;

-- An offer cannot displace itself. Coherence rather than policy — no design anybody could want has a
-- chain whose head is its own successor, so this binds nobody, which is the test 000501 applied to
-- `ck_bids_timing_is_ordered` and refused for the timing constraint.
ALTER TABLE bids ADD CONSTRAINT ck_bids_supersession_is_not_reflexive CHECK (
    superseded_by IS NULL OR superseded_by <> id
);

-- **"Only the latest valid offer can be accepted" (Docs/02 §4), as a constraint rather than a rule
-- somebody has to remember.** This is the point of putting the link on the displaced row.
--
-- A row that has been countered is not live and cannot be awarded. So SHIP-92's `UPDATE bids SET
-- status = 'Accepted'` against a stale offer does not quietly succeed and does not depend on the
-- award transaction having re-read the status under its lock: PostgreSQL refuses the write. That is
-- the same guarantee `uq_bids_one_accepted_per_job` gives for *two* awards, pointing at the other
-- half of the same rule.
--
-- 'Submitted' is in the list as well as 'Accepted', and it is doing separate work. It is what keeps
-- `uq_bids_one_submitted_per_provider_per_job` meaning "the one live offer in this negotiation": a
-- displaced row cannot re-enter that index's predicate, so the live offer and the head of the chain
-- are the same row by construction rather than by agreement between two writers.
--
-- **The other five statuses are deliberately permitted on a displaced row.** A superseded offer may
-- later become 'Rejected' when the job is awarded elsewhere (SHIP-93 closes *all* other bids,
-- Docs/02 §3), and forbidding that would be this migration binding that ticket's design in exactly
-- the way 000501 refused to bind this one.
ALTER TABLE bids ADD CONSTRAINT ck_bids_superseded_is_not_live CHECK (
    superseded_by IS NULL OR status NOT IN ('Submitted', 'Accepted')
);

-- **The award commits a provider, so only a provider's offer can be awarded.**
--
-- Docs/02 §1 defines Awarded as "Customer has accepted one provider bid; provider commitment
-- exists", and Docs/01 §4.1 gives the customer "Accept one bid" against bids they received. With
-- counters, the head of a chain is sometimes the *customer's* offer — and awarding that would bind a
-- provider to a price and a date they never agreed to, with `uq_bids_one_accepted_per_job` happily
-- allowing it and nothing else to notice.
--
-- The rule is therefore in the schema rather than in the award endpoint that has not been written.
-- It costs a customer nothing: a customer who wants their own number accepted waits for the provider
-- to counter at it, and *that* row is the provider's commitment and is awardable.
ALTER TABLE bids ADD CONSTRAINT ck_bids_only_a_providers_offer_is_accepted CHECK (
    status <> 'Accepted' OR offered_by = 'provider'
);

-- **A chain is a list, not a tree, and it takes two facts to say that. This index is the second.**
--
-- "An offer has at most one successor" is already true by construction: `superseded_by` is a single
-- column, so a row cannot name two. This index is the *other* direction — "an offer is the successor
-- of at most one predecessor" — which nothing else says. Without it two separate offers could both
-- name one counter as what displaced them, and a reader walking the links backwards would find a
-- negotiation that merged rather than a chain.
--
-- **It is deliberately not the guard against two counters racing each other**, and it is worth being
-- precise about that rather than claiming the stronger thing. Two counters against one live offer are
-- refused three times over, in this order:
--
--   1. `internal/bidding` takes the head `FOR UPDATE`, so the second transaction waits and then reads
--      the committed result — which is the only one of the three that produces a legible refusal.
--   2. The status write is a compare-and-set on `status = 'Submitted' AND superseded_by IS NULL`, so a
--      transaction that got past the lock matches no rows and rolls the whole counter back.
--   3. `uq_bids_one_submitted_per_provider_per_job` refuses the second live offer outright, which is
--      the backstop that holds if the first two are ever removed.
--
-- That third one is 000501's index doing exactly what its header promised: "SHIP-87 and SHIP-88 move
-- the prior offer out before writing the next", so the predicate never needed widening.
--
-- A partial unique *index* rather than a constraint, for the reason the other two are indexes: the
-- uniqueness is wanted only where the column is set, and a constraint cannot be partial. It is also
-- the index that makes reading a chain backwards a lookup rather than a scan.
CREATE UNIQUE INDEX uq_bids_one_successor
    ON bids (superseded_by)
    WHERE superseded_by IS NOT NULL;

-- The chain, read newest first, and the read SHIP-88's history endpoint makes.
--
-- `idx_bids_job` leads with job_id alone, so it cannot serve "this negotiation's offers in order"
-- without scanning every bid on the job — which on a well-bid job is every competitor's chain read
-- and discarded. Leading with the pair is what makes one negotiation's history a range scan.
CREATE INDEX idx_bids_negotiation ON bids (job_id, provider_id, created_at DESC);

-- **`uq_bids_idempotency` gains the author, and this is 000501's own argument extended rather than
-- reversed.**
--
-- That index is scoped `(job_id, provider_id, idempotency_key)`, and 000501 says why the provider is
-- in it: "A job has many bidders, so job_id alone does not scope this one: two providers whose
-- clients happened to generate the same key would collide, and the second would be handed the
-- first's bid."
--
-- A negotiation now has **two** writers, and the identical sentence applies to them: a customer and
-- a provider whose clients generated the same key would collide inside one `(job_id, provider_id)`
-- pair, and the second would be answered with the first's row — a request that wrote nothing,
-- reported as though it had. `offered_by` is what makes a key unable to reach the other party's
-- offer, exactly as `provider_id` makes one unable to reach another provider's.
--
-- Recreated rather than added alongside, because two overlapping unique indexes would be two
-- arbiters and `insertBid`'s `ON CONFLICT` names exactly one. **It is a strict widening**: every row
-- written before this migration has `offered_by = 'provider'`, so no pair of existing rows changes
-- its relationship, and nothing that was refused becomes permitted for any caller that existed.
DROP INDEX uq_bids_idempotency;

CREATE UNIQUE INDEX uq_bids_idempotency
    ON bids (job_id, provider_id, offered_by, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

COMMENT ON COLUMN bids.provider_id IS
    'The provider this negotiation is with. Not necessarily the author of the row: a customer''s counter-offer carries the same provider_id and is distinguished by offered_by (SHIP-87).';
COMMENT ON COLUMN bids.offered_by IS
    'Who made this offer — the bidding provider, or the customer answering them (Docs/02 §4, SHIP-87).';
COMMENT ON COLUMN bids.superseded_by IS
    'The counter-offer that displaced this one, or NULL if this is the live head of its chain. ck_bids_superseded_is_not_live is what makes "only the latest valid offer can be accepted" a database rule (SHIP-88).';
