-- SHIP-69: a job is warned once per deadline, and moving the deadline is what re-arms the warning.
--
-- Docs/02 §6.3: "The customer is warned 48 hours before expiry and can extend in one action."
-- 000406 built the deadline; this builds the mark that makes "once" true, and the trigger that
-- makes "per deadline" true.
--
-- # Why a column rather than reading the outbox back
--
-- The sweep runs every few minutes, so without a mark it would emit the same warning every pass
-- for two days — the customer's phone buzzing five hundred times about one job. "Has this job
-- already been warned" therefore has to be answerable, and the tempting answer is to ask the
-- outbox whether a job.expiry_warned event exists for the aggregate.
--
-- It is the wrong table to ask. The outbox is a hand-off, not a record: rows are marked published
-- and are prunable the moment they have been (000004 says so), so a question answered from it is
-- answered correctly today and wrongly after the first clean-up. It is also a cross-domain read —
-- the events seam is infrastructure that jobs writes through, not a table jobs queries. A column
-- on the row the rule is about is both cheaper and true for as long as the job exists.
--
-- # Why the re-arming is a trigger and not a line in the extend endpoint
--
-- The same argument 000406 makes for setting the deadline in the first place, in the other
-- direction. What has to be true is that *whenever* a job's deadline moves, the warning against
-- the old one stops counting — otherwise a customer who extends is never warned again, and the
-- second half of Docs/02 §6.3 quietly stops working for exactly the jobs that used the first half.
--
-- Moving a deadline is not one code path either. SHIP-70's extend endpoint is the first, an
-- administrator adjusting a listing is a plausible second, and a job republished with a new pickup
-- window is a third. A line in the extend handler is a line the other two can forget, and the
-- symptom — a warning that silently never fires again — is invisible from every side.
--
-- So the rule is attached to the change rather than to the caller, exactly as `updated_at` and
-- `expires_at` already are.

ALTER TABLE jobs
    -- When the platform last told this job's owner it was about to expire. NULL means "not warned
    -- against the deadline it has now", which is the state a job starts in, the state the trigger
    -- below returns it to, and the state the claim in internal/jobs selects on.
    ADD COLUMN expiry_warned_at timestamptz;

CREATE OR REPLACE FUNCTION jobs_moving_the_deadline_rearms_the_warning() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    -- IS DISTINCT FROM rather than <>, so that a deadline appearing (NULL to a value) or being
    -- cleared counts as a move. Both are changes to the thing the warning was about.
    IF NEW.expires_at IS DISTINCT FROM OLD.expires_at THEN
        NEW.expiry_warned_at := NULL;
    END IF;

    RETURN NEW;
END;
$$;

-- Named so it sorts before jobs_open_gets_a_deadline, because PostgreSQL fires BEFORE UPDATE row
-- triggers in name order and the two touch the same column. This one therefore sees the deadline a
-- *caller* wrote rather than the one 000406 is about to compute, which is the honest reading: a job
-- becoming Open has no warning to re-arm, and clearing a NULL again would be a change nobody made.
CREATE TRIGGER jobs_moving_the_deadline_rearms_the_warning
    BEFORE UPDATE ON jobs
    FOR EACH ROW EXECUTE FUNCTION jobs_moving_the_deadline_rearms_the_warning();

-- The warning claim's index, partial on exactly its predicate for the reason idx_jobs_open_expiry
-- is: the sweep asks "which Open jobs are close to their deadline and have not been told", and that
-- set shrinks as each job is warned. A job that has been warned leaves the index rather than
-- staying in it and being skipped, so the index stays the size of the work outstanding rather than
-- the size of the live marketplace.
CREATE INDEX idx_jobs_open_unwarned ON jobs (expires_at)
    WHERE status = 'Open' AND expiry_warned_at IS NULL;

COMMENT ON COLUMN jobs.expiry_warned_at IS
    'When the owner was warned that this job was about to expire (Docs/02 §6.3, SHIP-69). NULL until the warning is emitted, and returned to NULL by jobs_moving_the_deadline_rearms_the_warning whenever expires_at moves.';
COMMENT ON FUNCTION jobs_moving_the_deadline_rearms_the_warning() IS
    'Clears jobs.expiry_warned_at whenever jobs.expires_at changes, so a job whose deadline moved is warned again against the new one (Docs/02 §6.3, SHIP-69).';
