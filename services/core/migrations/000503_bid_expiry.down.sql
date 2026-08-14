-- Reverses 000503.
--
-- `IF EXISTS` throughout, which is this repository's convention for a down migration and is not
-- decoration here. Numbers are drawn from reserved per-domain blocks rather than in time order
-- (migrations/blocks.go), so a new bidding migration is *below* the version a working database
-- already records — and SHIP-15g's guard prints `make migrate-down n=all && make migrate-up` as
-- the fix. That `down` walks the file list downward from the current version, so **this file runs
-- before its own `up` ever has.** Every statement below therefore has to be safe against a schema
-- where none of them was ever applied. 000302, 000407, 000502 and 000602 all take the same
-- precaution.
--
-- The column comment is restored to 000501's wording rather than dropped, because a `down` that
-- left this migration's sentence behind would leave the schema describing a sweep that no longer
-- runs.

-- `COMMENT ON COLUMN` has no `IF EXISTS`, so the guard is a lookup rather than a keyword. It is
-- the same precaution the `DROP` above gets for free, and it is what makes this file safe to run
-- against a schema in which 000501 has not added the column yet.
DROP INDEX IF EXISTS idx_bids_live_expiry;

DO $do$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'bids' AND column_name = 'pickup_at'
    ) THEN
        COMMENT ON COLUMN bids.pickup_at IS
            'When the provider commits to collecting. An instant rather than a window: the job states the customer''s flexibility, the bid states a commitment (SHIP-84).';
    END IF;
END
$do$;
