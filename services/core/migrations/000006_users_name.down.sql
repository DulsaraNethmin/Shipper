-- Reverse of 000006: `users` goes back to holding no name.
--
-- The column comment goes with the column, and the constraint goes with it too — dropping a
-- column drops its CHECK, so naming the constraint separately would be a second statement that
-- can only fail. `IF EXISTS` on the column because CI runs up -> down all -> up against an
-- empty database and a down migration must be re-runnable (SHIP-15g).
--
-- **This loses data, and that is the honest reading rather than a caveat.** Every name collected
-- since the up migration is destroyed by this statement, and no other column holds a copy. That
-- is true of any additive column's reversal and is why the down direction is a development
-- convenience rather than a rollback plan: the recovery from a bad deploy of this migration is
-- to leave the column in place and stop writing it.

ALTER TABLE users
    DROP COLUMN IF EXISTS name;
