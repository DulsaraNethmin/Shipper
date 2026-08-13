-- Reverse of 000501.
--
-- The indexes go before the columns they read, and the constraints before the columns they check.
-- PostgreSQL would drop both with the column, but naming them keeps this file readable against its
-- twin — a reader comparing the two should be able to see every object accounted for.

DROP INDEX IF EXISTS uq_bids_idempotency;
DROP INDEX IF EXISTS uq_bids_one_submitted_per_provider_per_job;

ALTER TABLE bids DROP CONSTRAINT IF EXISTS ck_bids_message;
ALTER TABLE bids DROP CONSTRAINT IF EXISTS ck_bids_idempotency_key;
ALTER TABLE bids DROP CONSTRAINT IF EXISTS ck_bids_timing_is_ordered;

ALTER TABLE bids DROP COLUMN IF EXISTS idempotency_key;
ALTER TABLE bids DROP COLUMN IF EXISTS message;
ALTER TABLE bids DROP COLUMN IF EXISTS deliver_by;
ALTER TABLE bids DROP COLUMN IF EXISTS pickup_at;
