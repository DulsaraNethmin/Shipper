-- SHIP-105: who is actually driving the job, and how to reach them.
--
-- The first table in the delivery block. It carries the two facts Docs/01 §4.4 and Docs/03 need
-- about a driver — a name and a mobile number — and nothing about tokens, which are SHIP-107's
-- and get their own migration.
--
-- # The driver has no account, and this table is where that stops being an abstract statement
--
-- The driver portal is link-authenticated (Docs/07 §3): a driver holds a job-scoped token and
-- never a session, and that token cannot be exchanged for one. So there is no `users` row to
-- point at, and no foreign key to the account system anywhere below. A driver *is* a row here,
-- for the length of one job.
--
-- 000401 already committed to this reading: `job_status_history.actor_id` for
-- `actor_type = 'driver'` names an assignment rather than a user. This table is the thing that
-- reference resolves to, which has two consequences that shaped everything else here.
--
-- # Consequence one: an assignment's identity is immutable
--
-- If `driver_name` could be edited, every history row and every milestone already attributed to
-- this assignment would silently become attributed to somebody else. That is not an update, it
-- is a rewrite of who did what — the same defect 000005 refuses for `users.role`, and refused
-- the same way, with a trigger rather than a convention. A provider replacing a driver ends this
-- assignment and creates another.
--
-- # Consequence two: at most one assignment is live at a time
--
-- A job with two live assignments is a job with two valid driver links, which is the one thing
-- SHIP-108's "exactly one job, and nothing else" cannot be allowed to become "and one of several
-- drivers nobody stood down". A partial unique index is the mechanism — the same PostgreSQL
-- feature the one-accepted-bid invariant uses (SHIP-91), and load-bearing here for the same
-- reason: application logic that forgets to check is the normal way this goes wrong.
--
-- # What is deliberately not here
--
--   SHIP-106  who made the assignment. It is an account for a provider and *not* an account for
--             an administrator — ck_users_role refuses 'admin', because admin sign-in is a
--             separate system (SHIP-147) — so recording it means the polymorphic actor_type /
--             actor_id pair audit_log and job_status_history carry, and SHIP-106 is the ticket
--             that knows whether an administrator may assign at all. Until then the Awarded →
--             Driver assigned history row records who did it.
--   SHIP-107  the job-scoped token: its hash, its expiry, and its issue count.
--   SHIP-109  revocation and reissue of that token, which is a fact about the link and not
--             about the assignment. A driver who loses the link (Docs/02 §5) keeps the job.
--
-- The narrowness is 000400's discipline rather than an oversight: every later ticket brings its
-- own columns, and guessing at them now would be guessing at designs nobody has written.

CREATE TABLE driver_assignments (
    id     uuid PRIMARY KEY,

    -- The job being driven. There is no CHECK that the job has reached 'Awarded', for the reason
    -- 000400 gives about the customer's role: a CHECK constraint cannot see another table's
    -- column. SHIP-106 enforces it where the assignment is made, and it is the same shape of rule
    -- — the status guard will already have refused a transition into 'Driver assigned' from
    -- anywhere Docs/02 §2 does not permit.
    --
    -- ON DELETE RESTRICT per Docs/10 §3.3. A delivery record that vanished with its job would
    -- take the only evidence of who carried the goods with it.
    job_id uuid NOT NULL,

    -- What the customer is shown and what support asks for by name. Non-blank because a table
    -- called driver_assignments storing '   ' has recorded nothing.
    --
    -- No length bound here. A maximum is a validation limit, and Docs/06 §5.3 keeps those
    -- server-side where they can be changed under operational pressure; a CHECK constraint is a
    -- migration. SHIP-106 bounds it.
    driver_name text NOT NULL,

    -- How the link reaches them, and the only channel the platform has to a person with no
    -- account. E.164, normalised before it arrives, exactly as users.phone is.
    --
    -- The shape is checked and the allocation is not: whether +61499999999 is answered by anybody
    -- is not a thing a constraint can know. This mirrors identity's validE164 — a leading '+',
    -- then eight to fifteen digits, the first of which is not zero, because a country code never
    -- starts with one and a leading zero is an unrecognised trunk prefix.
    driver_mobile text NOT NULL,

    -- When this driver came off the job. NULL means they are still on it, and that is what the
    -- partial unique index below counts.
    --
    -- Not a boolean: "when did the driver change" is a question support asks, and a flag answers
    -- only "did it". Not a deletion either — Docs/10 §3.3 has no soft deletes, and this is not
    -- one: the row remains a true statement about a period of the job's life, which is precisely
    -- what a milestone recorded during that period needs it to be.
    unassigned_at timestamptz,

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    -- At least one non-whitespace character. Written as a regexp rather than
    -- `btrim(driver_name) <> ''` because btrim's default character set is the space alone, so
    -- that form accepts a tab — which is exactly the value a form field returns when somebody
    -- tabs through it.
    CONSTRAINT ck_driver_assignments_name CHECK (driver_name ~ '\S'),

    CONSTRAINT ck_driver_assignments_mobile
        CHECK (driver_mobile ~ '^\+[1-9][0-9]{7,14}$'),

    CONSTRAINT fk_driver_assignments_job
        FOREIGN KEY (job_id) REFERENCES jobs (id) ON DELETE RESTRICT
);

-- At most one live assignment per job.
--
-- Partial rather than plain, because a job legitimately accumulates assignments over its life:
-- the first driver falls ill, the provider nominates another, and both rows have to survive
-- because milestones point at them. What must never exist is two rows with unassigned_at NULL.
CREATE UNIQUE INDEX uq_driver_assignments_active
    ON driver_assignments (job_id)
    WHERE unassigned_at IS NULL;

-- The foreign key's index (Docs/10 §3.3), and also the read path: every assignment this job has
-- had, most recent first, which is what a support view and a dispute pack ask for.
--
-- The partial index above does not serve this. It covers only live rows, so it can answer "who is
-- driving this job" and not "who has driven it".
CREATE INDEX idx_driver_assignments_job
    ON driver_assignments (job_id, created_at DESC);

-- Who the assignment is cannot change; whether it is still live can, once.
--
-- A trigger rather than a CHECK for 000005's reason: immutability is a statement about the
-- transition, and BEFORE UPDATE is the only place OLD and NEW both exist.
--
-- The second half matters as much as the first. Clearing unassigned_at would revive a driver who
-- was stood down — and with them, if SHIP-109 hangs the token off this row, a link somebody was
-- deliberately cut off from. Changing it would move the boundary of the period a milestone was
-- recorded in, after the fact.
--
-- Unconditional, where 000005's equivalent carries a WHEN clause. That clause exists because
-- users is the busiest table in the service and the function's only job is to raise; here the
-- function decides between three different transitions, so a WHEN would be the same three
-- predicates written a second place to drift from — and this table is written a handful of times
-- over the life of a job rather than on every request.
CREATE OR REPLACE FUNCTION driver_assignments_identity_is_immutable() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.job_id IS DISTINCT FROM OLD.job_id THEN
        RAISE EXCEPTION 'driver_assignments.job_id is immutable'
            USING HINT = 'An assignment belongs to one job. Moving it re-attributes every '
                         'milestone recorded against it.';
    END IF;

    IF NEW.driver_name IS DISTINCT FROM OLD.driver_name
        OR NEW.driver_mobile IS DISTINCT FROM OLD.driver_mobile THEN
        RAISE EXCEPTION 'driver_assignments identity is immutable: % / % cannot become % / %',
            OLD.driver_name, OLD.driver_mobile, NEW.driver_name, NEW.driver_mobile
            USING HINT = 'A driver has no account, so this row is the driver''s identity '
                         '(000401). Replacing a driver means unassigning this row and '
                         'inserting another, not editing this one.';
    END IF;

    IF OLD.unassigned_at IS NOT NULL AND NEW.unassigned_at IS DISTINCT FROM OLD.unassigned_at THEN
        RAISE EXCEPTION 'driver_assignments.unassigned_at is set once'
            USING HINT = 'Reviving an assignment restores a driver, and their link, to a job '
                         'they were taken off.';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER driver_assignments_identity_is_immutable
    BEFORE UPDATE ON driver_assignments
    FOR EACH ROW
    EXECUTE FUNCTION driver_assignments_identity_is_immutable();

CREATE TRIGGER driver_assignments_set_updated_at
    BEFORE UPDATE ON driver_assignments
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON TABLE driver_assignments IS
    'The driver carrying one job. A driver has no account (Docs/07 §3), so this row is their identity: job_status_history.actor_id names it when actor_type is ''driver''.';
COMMENT ON COLUMN driver_assignments.driver_mobile IS
    'E.164, normalised before it arrives. The only channel the platform has to a person with no account.';
COMMENT ON COLUMN driver_assignments.unassigned_at IS
    'When this driver came off the job. NULL means live, and uq_driver_assignments_active permits one live assignment per job.';
