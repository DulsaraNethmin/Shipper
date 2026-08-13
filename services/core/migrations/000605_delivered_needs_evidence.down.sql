-- Reverses SHIP-118.
--
-- The trigger goes and the function goes with it, which is the opposite of what 000601 and 000603
-- do — and for a reason rather than an inconsistency. Those two drop a *table*, and a function is
-- not a dependent of a table, so dropping it separately is how it avoids surviving as an orphan.
-- Here the table stays, so `DROP TRIGGER` is the whole reversal and the function is dropped
-- immediately after because nothing else references it.
--
-- `DROP TRIGGER … IF EXISTS` still fails when the *table* is absent, which is the down-before-up
-- case SHIP-15g's guard produces and 000603's down migration describes at length. `to_regclass`
-- guards it.
DO $$
BEGIN
    IF to_regclass('public.milestones') IS NOT NULL THEN
        EXECUTE 'DROP TRIGGER IF EXISTS milestones_delivered_has_evidence ON milestones';
    END IF;
END;
$$;

DROP FUNCTION IF EXISTS milestones_delivered_needs_evidence();
