-- SHIP-68: an Open job carries a deadline, and the deadline is set where the job becomes Open.
--
-- Docs/02 §6.3: a job leaves Open at whichever comes first — fourteen days after publication, or
-- the moment its own pickup date passes. The second is the operative rule and the first is a
-- backstop for jobs with distant dates; both are here, as one column and one expression.
--
-- 000404 put pickup_window_end in for this and said so in its COMMENT.
--
-- # Why the deadline is set by the database rather than by the domain
--
-- Because "every published job has a deadline" should be structural rather than remembered, which
-- is the same argument 000402 makes for the status guard itself. Publication is SHIP-63 and does
-- not exist yet; a provider cancellation returns an Awarded job to Open (Docs/02 §6.2, SHIP-93);
-- an administrator may reopen one. Each of those is a separate ticket on a separate branch, and a
-- deadline computed in one Go function is a deadline three of them can forget — producing an Open
-- job that never expires, which nothing reports and nobody notices until the marketplace is full
-- of listings from last month.
--
-- Here there is one place, it fires on the transition itself, and it cannot be skipped by a route
-- into Open that nobody has written yet. `updated_at` is the precedent: a derived timestamp
-- belonging to a state change is set by the trigger attached to that change (000001).
--
-- # It is a default, not a lock
--
-- The trigger fills expires_at only when it is NULL. A caller that writes its own value in the
-- same statement keeps it, and a job already carrying a deadline keeps that — which is what makes
-- SHIP-70's extend endpoint an ordinary UPDATE, and what stops the clock restarting every time a
-- job cycles Negotiating → Open as bids expire. Docs/02 §6.3 counts fourteen days from
-- publication, not from the most recent time the job happened to be Open.
--
-- The fourteen days is therefore a default that a later ticket may compute differently and write
-- explicitly. It lives in SQL rather than in configuration because it is a lifecycle rule from
-- Docs/02 §6.3 — a product decision with a document behind it — rather than an operational limit
-- of the kind Docs/06 §5.3 requires to be changeable without a deploy, and because the customer's
-- own escape hatch is SHIP-70 rather than an operator's.
--
-- # Nothing is cleared on the way out
--
-- A job that leaves Open keeps the deadline it had. The claim below filters on status, so a stale
-- value is never read; and if the job comes back to Open — Docs/02 §6.2 — the original deadline is
-- the correct one, including when it has already passed. A job returning to Open with its pickup
-- date behind it is expired, and the next pass saying so is right rather than harsh.

ALTER TABLE jobs
    -- When an Open job stops being offered. NULL until the job is published, and never NOT NULL:
    -- a Draft has no deadline, and a job in the middle of a delivery has one it stopped caring
    -- about.
    ADD COLUMN expires_at timestamptz;

CREATE OR REPLACE FUNCTION jobs_open_gets_a_deadline() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.status = 'Open'
       AND OLD.status IS DISTINCT FROM 'Open'
       AND NEW.expires_at IS NULL
    THEN
        -- LEAST ignores NULL arguments in PostgreSQL, so a job with no pickup window gets the
        -- fourteen-day backstop on its own, with no CASE and no COALESCE to a sentinel date.
        NEW.expires_at := LEAST(now() + interval '14 days', NEW.pickup_window_end);
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER jobs_open_gets_a_deadline
    BEFORE UPDATE ON jobs
    FOR EACH ROW EXECUTE FUNCTION jobs_open_gets_a_deadline();

-- The claim's index, and it is partial for the same reason the claim is narrow: the sweep asks
-- "which Open jobs are due", and Open is a small and shrinking fraction of a table that keeps
-- every job it has ever had (Docs/10 §3.3 has no deletion). A partial index stays the size of the
-- live marketplace rather than the size of its history.
CREATE INDEX idx_jobs_open_expiry ON jobs (expires_at) WHERE status = 'Open';

COMMENT ON COLUMN jobs.expires_at IS
    'When an Open job stops being offered: the earlier of fourteen days after publication and its pickup window ending (Docs/02 §6.3, SHIP-68). Set by jobs_open_gets_a_deadline when it is NULL; extended by SHIP-70.';
COMMENT ON FUNCTION jobs_open_gets_a_deadline() IS
    'Gives a job a deadline as it becomes Open, unless one was supplied. The earlier of fourteen days and the pickup window ending (Docs/02 §6.3).';
