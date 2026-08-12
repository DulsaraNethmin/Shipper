-- SHIP-110: what the driver recorded, and when — on both clocks.
--
-- Docs/02 §3.1 requires two timestamps and Docs/10 §3.3 makes them two explicit columns, "never
-- one". 000001_init named this table as the example when it explained why the set_updated_at()
-- trigger is not enough. This is that table.
--
-- # Why this is not job_status_history
--
-- The two look alike and answer different questions, and collapsing them is the mistake this
-- comment exists to prevent.
--
--   job_status_history  what the job's status did. Every row moves it, enforced by
--                       ck_job_status_history_moves and by 000402's guard, which will not accept
--                       a status change that no row here describes.
--   milestones          what an actor recorded. A row is a claim, and it may legitimately move
--                       nothing at all.
--
-- Docs/02 §3.1 forces them apart. A queued "Picked up" arriving after "In transit" is already
-- recorded "must be absorbed, not rejected as an error" — the platform accepts the historical
-- fact without moving the job backwards (SHIP-112). A "Delivered" recorded offline while an
-- administrator cancels the job loses, "the attempt is retained in history", and the driver is
-- shown what happened (SHIP-113). Neither of those is a status transition, so neither could be a
-- job_status_history row: the guard has nothing to guard and ck_job_status_history_moves would
-- refuse the row outright. Recording them anywhere less durable would mean the platform holding
-- an opinion about work a driver did with no evidence of it.
--
-- So a milestone that does move the job produces two rows, in one transaction, saying two
-- different true things. The pair is what makes an offline delivery reconstructable.
--
-- # The five, and why they are the five
--
-- Docs/01 §4.4 numbers what a provider or assigned driver can record. Docs/02 §2 is explicit that
-- this is "describing what a driver records, not constraining what the guard accepts" — the two
-- lists differ, and deliberately: nobody records 'Completed' (Docs/02 §6.1 has it expire), and
-- 'Cancelled' and 'Disputed' are not milestones on a delivery.
--
-- The strings are Docs/02 §1's own, spaces and sentence case included (Docs/10 §3.4), and held
-- to delivery.Milestones by TestMilestoneConstraintMatchesTheGoConstants. delivery declares its
-- own constants rather than importing jobs.Statuses: domains do not import each other
-- (Docs/06 §4.1), and these five are a delivery vocabulary that happens to overlap a jobs one.

CREATE TABLE milestones (
    id uuid PRIMARY KEY,

    -- ON DELETE RESTRICT per Docs/10 §3.3, and with the append-only triggers below it means a
    -- job with any milestone cannot be deleted at all — the same reading of Docs/05 §3.1 that
    -- job_status_history takes.
    job_id uuid NOT NULL,

    milestone text NOT NULL,

    -- Who recorded it. Docs/02 §3 is narrower than job_status_history's list on purpose:
    -- "delivery-status updates must be made only by the awarded provider, their assigned driver,
    -- or an administrator acting with an audit reason". A customer is not among them, and
    -- confirming a delivery is not recording one.
    --
    -- 'system' is here because Docs/02 §2 permits Picked up → In transit as "an automatic
    -- presentation change", which is the platform recording a milestone with nobody behind it.
    actor_type text NOT NULL,

    -- Which one. Deliberately without a foreign key, because the column points at three
    -- different tables and no key can express that:
    --
    --   provider  a users row
    --   driver    a driver_assignments row (000600) — a driver has no account at all
    --   admin     neither. ck_users_role refuses 'admin'; admin sign-in is a separate system
    --             (SHIP-147) with its own table
    --
    -- This is the convention 000401 already declared for job_status_history.actor_id, and the
    -- two tables are read together, so they name the actor the same way.
    actor_id uuid,

    -- Why, where a why exists. Required of an administrator, because Docs/01 §3 forbids an
    -- administrator changing a commercial record without an auditable reason and Docs/02 §3
    -- repeats it for delivery status specifically. Nullable otherwise: a driver arriving at a
    -- pickup owes nobody an explanation.
    reason text,

    -- The actor's clock: when the driver says they did it. This is what the customer is shown
    -- (Docs/02 §3.1) and it is supplied by a device that has been out of signal, may be in
    -- another zone, and may simply be wrong. The platform does not correct it — a corrected
    -- timestamp is no longer evidence — and it is not bounded against now() either, because
    -- refusing an implausible time would discard the record, which is the opposite of what
    -- Docs/02 §3.1 asks for.
    actor_recorded_at timestamptz NOT NULL,

    -- The platform's clock: when the record actually arrived. This is what audit and support
    -- reason about, and what makes an unsynced-milestone threshold (Docs/02 §6.5) measurable.
    --
    -- NOT NULL and **no DEFAULT**, which is not an omission. A default is a value a caller may
    -- override, and a caller that can set this column can backdate the one timestamp that is
    -- ours. milestones_server_clock() below refuses an INSERT that names it and fills it from
    -- now() otherwise, so the two clocks cannot be collapsed by a caller passing the same value
    -- to both. Together with the append-only triggers that is the whole of SHIP-110's guarantee:
    -- neither column can overwrite the other, at insert or afterwards.
    server_recorded_at timestamptz NOT NULL,

    -- No created_at, no updated_at, no trigger. server_recorded_at *is* the creation time, and a
    -- second column saying so is a second column that can disagree. A row that can be updated is
    -- not evidence.

    CONSTRAINT ck_milestones_milestone CHECK (milestone IN (
        'Driver assigned',
        'En route to pickup',
        'Picked up',
        'In transit',
        'Delivered'
    )),

    CONSTRAINT ck_milestones_actor_type CHECK (
        actor_type IN ('provider', 'driver', 'admin', 'system')
    ),

    -- An actor with an identity names it; 'system' must not claim one.
    CONSTRAINT ck_milestones_actor_id CHECK (
        (actor_type =  'system' AND actor_id IS NULL) OR
        (actor_type <> 'system' AND actor_id IS NOT NULL)
    ),

    CONSTRAINT ck_milestones_admin_reason CHECK (
        actor_type <> 'admin' OR reason IS NOT NULL
    ),

    CONSTRAINT fk_milestones_job
        FOREIGN KEY (job_id) REFERENCES jobs (id) ON DELETE RESTRICT
);

-- There is deliberately no uniqueness on (job_id, milestone).
--
-- A repeated request is stopped by the idempotency key (Docs/02 §3.1, SHIP-15, SHIP-111), which
-- is a different thing from a repeated milestone. A driver who reaches a pickup, finds nobody
-- there, and returns later records "En route to pickup" twice, and Docs/02 §5 lists that failed
-- attempt as an ordinary outcome. A unique index would refuse the second recording and would
-- also refuse the late arrival SHIP-112 exists to absorb.

-- The delivery timeline for one job, in the order the driver acted.
--
-- Ordered by the actor's clock rather than the platform's because that is the sequence the
-- customer is shown (Docs/02 §3.1), and an offline batch that syncs together shares one arrival
-- time while carrying four different recorded times. It also indexes the foreign key, which
-- Docs/10 §3.3 requires.
CREATE INDEX idx_milestones_job ON milestones (job_id, actor_recorded_at DESC);

-- The server's clock is not the caller's to set.
--
-- 000401 keeps the same guarantee by convention — a DEFAULT now() and a guard that never names
-- the column. That works while one function does the writing. Milestones are written from the
-- driver portal, from the mobile app's sync worker, and by an administrator, and the value of
-- the second timestamp rests entirely on none of them choosing it. So it is refused rather than
-- documented: an INSERT naming server_recorded_at fails, and one that does not gets now().
--
-- BEFORE INSERT runs ahead of constraint evaluation, which is why the column can be NOT NULL
-- with no default and still accept an INSERT that omits it.
CREATE OR REPLACE FUNCTION milestones_server_clock() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.server_recorded_at IS NOT NULL THEN
        RAISE EXCEPTION 'milestones.server_recorded_at is the platform''s clock and cannot be supplied'
            USING HINT = 'Omit the column. It records when the milestone arrived; '
                         'actor_recorded_at is where the time the actor claims belongs '
                         '(Docs/02 §3.1).';
    END IF;

    NEW.server_recorded_at = now();
    RETURN NEW;
END;
$$;

CREATE TRIGGER milestones_server_clock
    BEFORE INSERT ON milestones
    FOR EACH ROW EXECUTE FUNCTION milestones_server_clock();

-- Append-only, the same control 000003 puts on audit_log and 000401 on job_status_history.
--
-- It is what stops the platform quietly correcting a driver's clock after the fact, and it is
-- what "the attempt is retained" means in Docs/02 §3.1: a milestone that lost to an
-- administrative action is still a true record of what somebody recorded, and tidying it away
-- would destroy the evidence SHIP-113 has to show the driver.
CREATE OR REPLACE FUNCTION milestones_is_append_only() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'milestones is append-only: % is not permitted', TG_OP
        USING HINT = 'A milestone that turned out to be wrong is another milestone, and '
                     'another row. Correcting one in place rewrites what the driver recorded.';
END;
$$;

CREATE TRIGGER milestones_no_update
    BEFORE UPDATE ON milestones
    FOR EACH ROW EXECUTE FUNCTION milestones_is_append_only();

CREATE TRIGGER milestones_no_delete
    BEFORE DELETE ON milestones
    FOR EACH ROW EXECUTE FUNCTION milestones_is_append_only();

COMMENT ON TABLE milestones IS
    'What an actor recorded on a delivery, with both clocks (Docs/02 §3.1, SHIP-110). Append-only. A milestone is a claim and need not move the job: job_status_history is what the status did.';
COMMENT ON COLUMN milestones.actor_id IS
    'A users row for a provider, a driver_assignments row for a driver, neither for an administrator. Polymorphic, so no foreign key — the convention 000401 declared.';
COMMENT ON COLUMN milestones.actor_recorded_at IS
    'When the actor says it happened. Client-supplied, never corrected, and never bounded — an implausible time is evidence, not an error.';
COMMENT ON COLUMN milestones.server_recorded_at IS
    'When the platform received it. Set by the milestones_server_clock trigger; an INSERT that names this column is refused.';
