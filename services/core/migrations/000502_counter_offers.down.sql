-- Reverses 000502.
--
-- **`IF EXISTS` throughout, which is this repository's convention for a down migration and is not
-- decoration here.** Numbers are drawn from reserved per-domain blocks rather than in time order
-- (migrations/blocks.go), so a new bidding migration is *below* the version a working database
-- already records — and SHIP-15g's guard prints `make migrate-down n=all && make migrate-up` as the
-- fix. That `down` walks the file list downward from the current version, so **this file runs before
-- its own `up` ever has.** Every statement below therefore has to be safe against a schema where none
-- of them was ever applied. 000302, 000407 and 000602 all take the same precaution.
--
-- `uq_bids_idempotency` is restored to the three-column form 000501 created, because a `down` that
-- left the four-column one behind would leave the schema in a state neither migration describes. That
-- is safe in this direction for the reason the widening was safe in the other: with this migration
-- reversed, `offered_by` is gone and every surviving row is a provider's offer.
--
-- The counter rows themselves are **not** deleted. A down migration that destroyed commercial record
-- would be a worse failure than an unreversible one — Docs/01 §4.3 requires every offer and
-- counter-offer to be retained. What is lost is the link between them, which is the column.

DROP INDEX IF EXISTS uq_bids_idempotency;

CREATE UNIQUE INDEX IF NOT EXISTS uq_bids_idempotency
    ON bids (job_id, provider_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

DROP INDEX IF EXISTS idx_bids_negotiation;
DROP INDEX IF EXISTS uq_bids_one_successor;

ALTER TABLE bids DROP CONSTRAINT IF EXISTS ck_bids_only_a_providers_offer_is_accepted;
ALTER TABLE bids DROP CONSTRAINT IF EXISTS ck_bids_superseded_is_not_live;
ALTER TABLE bids DROP CONSTRAINT IF EXISTS ck_bids_supersession_is_not_reflexive;
ALTER TABLE bids DROP CONSTRAINT IF EXISTS fk_bids_superseded_by;
ALTER TABLE bids DROP COLUMN IF EXISTS superseded_by;

ALTER TABLE bids DROP CONSTRAINT IF EXISTS ck_bids_offered_by;
ALTER TABLE bids DROP COLUMN IF EXISTS offered_by;

COMMENT ON COLUMN bids.provider_id IS
    'The provider who made the offer.';
