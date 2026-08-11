-- SHIP-57: job status is not a settable field, and the database is what says so.
--
-- CLAUDE.md states the invariant and Docs/02 §2 gives the transition table, but neither is
-- structurally enforced by a Go function that everybody is asked to remember. Nothing stops a
-- future postgres.go writing `UPDATE jobs SET status = …`, the boundary lint cannot see it, and
-- review catches it only while somebody is looking. Docs/11 §9 carried this as an open
-- recommendation — "job status as a database guarantee, decide at SHIP-57" — and it is decided
-- here, in favour of the database, on the same argument that puts one-accepted-bid in a partial
-- unique index rather than in application logic.
--
-- Two triggers, covering the two ways a status can be set.
--
-- # Creation
--
-- Docs/02 §2 has one entry point: every job starts as a Draft and every path out of it is a
-- transition. An INSERT naming any other status has skipped the guard entirely — a job created
-- at 'Awarded' has no accepted bid, and one created at 'Delivered' has no proof.
--
-- # Change
--
-- A status change is refused unless a job_status_history row written in the same transaction
-- already describes it. That single condition carries three guarantees at once:
--
--   * the change went through the guard, because nothing else writes that row;
--   * it is recorded, with an actor, both clocks, and a reason where one is required — those
--     are NOT NULL columns in 000401, so the record cannot be a placeholder;
--   * it is in a transaction, because the session variable naming the row is transaction-local.
--     set_config(…, true) outside a transaction lasts exactly as long as the statement that set
--     it, so a caller holding a pool rather than a transaction is refused here rather than
--     committing a status change whose history entry might not follow.
--
-- What this deliberately does *not* do is duplicate the transition table from Docs/02 §2. The
-- permitted moves live in Go, in one place, tested against the document; expressing them here as
-- well would be a second copy to keep in step, and the failure of a copy that has drifted is a
-- transition that is legal in one layer and impossible in the other.

CREATE OR REPLACE FUNCTION jobs_starts_as_a_draft() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.status IS DISTINCT FROM 'Draft' THEN
        RAISE EXCEPTION 'a job is created as a Draft, not as %', NEW.status
            USING HINT = 'Insert the job, then move it with the transition guard (Docs/02 §2, SHIP-57).';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER jobs_starts_as_a_draft
    BEFORE INSERT ON jobs
    FOR EACH ROW EXECUTE FUNCTION jobs_starts_as_a_draft();

CREATE OR REPLACE FUNCTION jobs_status_change_is_guarded() RETURNS trigger
    LANGUAGE plpgsql
AS $$
DECLARE
    claimed  text;
    recorded uuid;
BEGIN
    -- Every other update to a job — its category, its addresses, its budget — passes straight
    -- through. This trigger has an opinion about one column.
    IF NEW.status IS NOT DISTINCT FROM OLD.status THEN
        RETURN NEW;
    END IF;

    -- The second argument makes an unset variable NULL rather than an error, which is the
    -- ordinary case: almost every update to a job is not a status change and sets nothing.
    claimed := current_setting('shipper.job_status_transition', true);

    IF claimed IS NULL OR claimed = '' THEN
        RAISE EXCEPTION 'job status is not a settable field: % may not move from % to % by direct UPDATE',
            NEW.id, OLD.status, NEW.status
            USING HINT = 'Every transition passes one guarded function (CLAUDE.md, Docs/02 §2, SHIP-57).';
    END IF;

    BEGIN
        recorded := claimed::uuid;
    EXCEPTION WHEN invalid_text_representation THEN
        RAISE EXCEPTION 'shipper.job_status_transition is %, which is not a job_status_history id', claimed;
    END;

    IF NOT EXISTS (
        SELECT 1
        FROM job_status_history h
        WHERE h.id          = recorded
          AND h.job_id      = NEW.id
          AND h.from_status = OLD.status
          AND h.to_status   = NEW.status
    ) THEN
        RAISE EXCEPTION 'job status change is unrecorded: no job_status_history row % says % moved from % to %',
            recorded, NEW.id, OLD.status, NEW.status
            USING HINT = 'Write the history row in the same transaction, before the update (SHIP-57a).';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER jobs_status_change_is_guarded
    BEFORE UPDATE ON jobs
    FOR EACH ROW EXECUTE FUNCTION jobs_status_change_is_guarded();

COMMENT ON FUNCTION jobs_status_change_is_guarded() IS
    'Refuses any change to jobs.status that is not described by a job_status_history row written in the same transaction (Docs/02 §2, SHIP-57).';
COMMENT ON FUNCTION jobs_starts_as_a_draft() IS
    'Refuses a job created at any status but Draft; every other status is reached by transition (Docs/02 §2).';
