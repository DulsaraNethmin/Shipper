-- Reverse of 000803: permanent suspension goes back to being one administrator's action.
--
-- The indexes and the trigger go with the table. `IF EXISTS` because CI runs up -> down all -> up
-- against an empty database and a down migration must be re-runnable (SHIP-15g).
--
-- **This destroys the record of every review**, approved and pending alike, and no other table
-- holds a copy of the *pending* ones — an approved suspension leaves its `audit_log` entries, which
-- are append-only and survive this. That asymmetry is worth naming rather than discovering: the
-- recovery from a bad deploy of the up migration is to stop writing the table, not to reverse it.

DROP TABLE IF EXISTS suspension_reviews;
