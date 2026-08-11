-- SHIP-57a: what happened to a job, who made it happen, and when they say they did.
--
-- Docs/02 §3.1 is the specification, and its central claim is that a transition carries **two**
-- timestamps rather than one: when the actor recorded it and when the platform accepted it. A
-- driver records "Picked up" in a loading bay with no signal and the request arrives forty
-- minutes later on the motorway. The first timestamp is what the customer is shown; the second
-- is what audit and support reason about. Collapsing them loses the only evidence that the two
-- ever differed, and Docs/10 §3.3 therefore makes them two explicit columns.
--
-- This table lands with SHIP-57 rather than after it, even though the backlog lists it as
-- depending on the guard. The dependency runs the other way in practice: 000402 refuses a status
-- change unless a row here already describes it, so the guard is defined in terms of this table
-- and cannot be written before it exists.

CREATE TABLE job_status_history (
    id      uuid NOT NULL PRIMARY KEY,

    -- ON DELETE RESTRICT per Docs/10 §3.3, and with the append-only triggers below it means a
    -- job with any history cannot be deleted at all. That is the intended reading of Docs/05
    -- §3.1: SHIP-171 pseudonymises rather than deletes, precisely so the transaction record
    -- survives.
    job_id  uuid NOT NULL,

    -- Both ends of the move, so a row is readable without its predecessor. Reconstructing the
    -- "from" by looking at the previous row would be wrong the moment two rows share a
    -- server_recorded_at, which a single transaction can produce.
    --
    -- The status lists are written out again rather than shared with ck_jobs_status. A domain
    -- type would avoid the repetition and Docs/10 §3.4 asks for text-with-a-CHECK; the
    -- duplication is made safe instead by TestEveryJobStatusConstraintMatchesTheGoConstants,
    -- which holds all three constraints to the same Go list, so a status added to one and not
    -- the others fails before it can be merged.
    from_status text NOT NULL,
    to_status   text NOT NULL,

    -- Who moved it. Finer-grained than audit_log's three actor types on purpose: Docs/02 §1
    -- names a primary actor per status, and the difference between a provider and the driver
    -- they nominated is exactly the difference support needs when a delivery goes wrong.
    --
    -- A driver has no account — the driver portal is link-authenticated and holds a job-scoped
    -- token (Docs/07 §3) — so actor_id for a driver names the assignment (SHIP-105) rather than
    -- a user. There is deliberately no foreign key, for the reason audit_log gives: this record
    -- must outlive its subject.
    actor_type text NOT NULL,
    actor_id   uuid,

    -- Why, where a why exists. Nullable because most transitions are self-explanatory — a
    -- customer publishing their own job is not owed an explanation — and required of an
    -- administrator, because Docs/01 §3 forbids an administrator changing a commercial record
    -- without an auditable reason, and this table is where that reason is auditable.
    reason text,

    -- The actor's clock: when the person or process says the transition happened. Supplied by
    -- the caller, and therefore not trustworthy on its own — a device clock can be wrong, and
    -- the guard does not correct it, because a corrected timestamp is no longer evidence.
    actor_recorded_at timestamptz NOT NULL,

    -- The platform's clock: when the transition was accepted. now() is transaction start time,
    -- so it is the same instant as the job row's updated_at, which is what makes the two
    -- records join up.
    --
    -- Defaulted rather than supplied, and the guard never names it in its INSERT. A caller that
    -- could set it could backdate a transition, and the whole point of the second column is that
    -- one of the two timestamps is ours.
    server_recorded_at timestamptz NOT NULL DEFAULT now(),

    -- No updated_at, and no trigger. A row that can be updated is not a history.

    CONSTRAINT ck_job_status_history_from_status CHECK (from_status IN (
        'Draft',
        'Open',
        'Negotiating',
        'Awarded',
        'Driver assigned',
        'En route to pickup',
        'Picked up',
        'In transit',
        'Delivered',
        'Completed',
        'Cancelled',
        'Disputed'
    )),

    CONSTRAINT ck_job_status_history_to_status CHECK (to_status IN (
        'Draft',
        'Open',
        'Negotiating',
        'Awarded',
        'Driver assigned',
        'En route to pickup',
        'Picked up',
        'In transit',
        'Delivered',
        'Completed',
        'Cancelled',
        'Disputed'
    )),

    -- A transition moves. A row recording a job moving from Open to Open is either a bug in the
    -- guard or an attempt to manufacture a history entry, and neither should be storable.
    CONSTRAINT ck_job_status_history_moves CHECK (from_status <> to_status),

    CONSTRAINT ck_job_status_history_actor_type CHECK (
        actor_type IN ('customer', 'provider', 'driver', 'admin', 'system')
    ),

    -- An account-backed actor names its account; 'system' must not claim one. The expiry sweep
    -- (SHIP-68) and the 72-hour auto-complete (SHIP-119) both act with no user behind them, and
    -- both must still be attributable — to the platform, which is what 'system' says.
    CONSTRAINT ck_job_status_history_actor_id CHECK (
        (actor_type =  'system' AND actor_id IS NULL) OR
        (actor_type <> 'system' AND actor_id IS NOT NULL)
    ),

    CONSTRAINT ck_job_status_history_admin_reason CHECK (
        actor_type <> 'admin' OR reason IS NOT NULL
    ),

    CONSTRAINT fk_job_status_history_job
        FOREIGN KEY (job_id) REFERENCES jobs (id) ON DELETE RESTRICT
);

-- The only way this table is read: everything that happened to one job, newest first. That is
-- the status timeline in SHIP-77, the support view, and the dispute pack.
--
-- It also indexes the foreign key, which Docs/10 §3.3 requires — and here it is load-bearing
-- rather than only prudent, because 000402 runs an EXISTS against this table on every status
-- change.
CREATE INDEX idx_job_status_history_job
    ON job_status_history (job_id, server_recorded_at DESC);

-- Append-only, enforced by the database rather than by convention — the same control 000003
-- puts on audit_log, and for the same reason.
--
-- A history that can be edited is not evidence. Docs/02 §3.1 requires that a queued update which
-- contradicts an administrative action is *retained* rather than discarded, so this table holds
-- attempts as well as outcomes, and the value of both rests on nobody being able to tidy them
-- afterwards. An application-level rule is what somebody with a psql prompt bypasses.
CREATE OR REPLACE FUNCTION job_status_history_is_append_only() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'job_status_history is append-only: % is not permitted', TG_OP
        USING HINT = 'A job that moved back is another transition, and another row.';
END;
$$;

CREATE TRIGGER job_status_history_no_update
    BEFORE UPDATE ON job_status_history
    FOR EACH ROW EXECUTE FUNCTION job_status_history_is_append_only();

CREATE TRIGGER job_status_history_no_delete
    BEFORE DELETE ON job_status_history
    FOR EACH ROW EXECUTE FUNCTION job_status_history_is_append_only();

COMMENT ON TABLE job_status_history IS
    'Every job status transition, with both clocks (Docs/02 §3.1). Append-only. 000402 refuses a status change that is not described by a row here.';
COMMENT ON COLUMN job_status_history.actor_recorded_at IS
    'When the actor says the transition happened. Client-supplied and never corrected.';
COMMENT ON COLUMN job_status_history.server_recorded_at IS
    'When the platform accepted it. Defaulted from now(); no caller sets it.';
