-- Reverses SHIP-123.
--
-- The constraint goes with the columns, so dropping them is enough; it is named here anyway because
-- a reader of a down migration should be able to see what disappears without reading the up
-- migration beside it.
--
-- **`IF EXISTS` throughout**, which is not decoration: `make migrate-down n=all` runs every down
-- migration in turn, and one that raises leaves `schema_migrations.dirty` set — a state that has to
-- be repaired by hand before anything else can run.
--
-- Dropping the columns discards what was in them, which for a delivered milestone is the recipient's
-- name and the note. That is the honest consequence of reversing this ticket rather than something
-- to work around: the fields exist because Docs/01 §4.4 requires them, and a database without the
-- columns is a database where the requirement is not met.
ALTER TABLE milestones DROP CONSTRAINT IF EXISTS ck_milestones_delivery_details;
ALTER TABLE milestones DROP COLUMN IF EXISTS delivery_note;
ALTER TABLE milestones DROP COLUMN IF EXISTS recipient_name;
