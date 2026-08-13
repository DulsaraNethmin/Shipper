-- SHIP-163: what a customer or provider says went wrong, captured once, at intake.
--
-- The first table in the admin block, and the first schema M6 owns. Docs/04 §7 is the whole
-- specification of what a row holds:
--
--     "Capture job, complainant, category, description, desired outcome, time of event,
--      and evidence."
--
-- Seven things, and every one of them is a column below. Two of the seven needed a decision
-- the document does not make, and both are recorded here rather than left for a reader to
-- reconstruct from the DDL.
--
-- # Raising a dispute freezes the job, and that is Docs/02 rather than an invention here
--
-- Docs/02 §2 has one row for it — "Awarded through Delivered → Disputed, eligible user/admin
-- opens a supported dispute" — and Docs/02 §3 says what it is for: "a dispute freezes automatic
-- completion until an administrator resolves it". SHIP-164's *Done when* speaks of an outcome
-- that "unfreezes the job", which is the same statement read from the other end.
--
-- So intake is a status transition as well as an insert, and the two commit together. The
-- transition goes through the one guarded function in `jobs` (SHIP-57) like every other, which
-- is why this table has no status column of its own about the *job*: the job's status lives on
-- the job, and a copy here would be a second answer to a question that already has one.
--
-- # A dispute is not append-only, unlike audit_log and milestones beside it
--
-- SHIP-164 moves it through investigation to a documented outcome, so the row is written again.
-- What is append-only is the *audit trail* of what administrators did to it (000003), which is a
-- different table for exactly this reason.
--
-- # No internal notes column, deliberately
--
-- SHIP-162's support notes attach to a user or a job and are never user-visible. Every column
-- below is read back to the complainant by the intake response, so a note written into one of
-- them would be a note the complainant reads. Notes get rows of their own.

CREATE TABLE disputes (
    id uuid PRIMARY KEY,

    -- "job". ON DELETE RESTRICT per Docs/10 §3.3: a job with a dispute on it cannot be deleted,
    -- which is the same reading of Docs/05 §3.1 that job_status_history and milestones take.
    job_id uuid NOT NULL,

    -- "complainant", as two columns rather than one.
    --
    -- complainant_id is the account. complainant_party is which side of *this job* they were on,
    -- and it is not the same fact as users.role: being the customer who owns the job, or the
    -- provider who won it, is a stronger statement than carrying a role claim, and it is the one
    -- Docs/04 §7 is asking for. It is resolved by the platform from the job and its accepted bid,
    -- never taken from the request.
    complainant_id uuid NOT NULL,
    complainant_party text NOT NULL,

    -- "category".
    --
    -- **Docs/04 §7 names the field and enumerates no values.** The six below are derived from
    -- Docs/02 §5's exception table, which is the only list in the documents of what actually goes
    -- wrong on a delivery, plus 'Other' — because a closed list with no escape hatch turns every
    -- unanticipated complaint into a mis-filed one. Two of §5's seven rows are excluded: a driver
    -- who lost their portal link and a driver recording milestones with no signal are operational
    -- events with their own handling, not things a party complains about.
    --
    -- One string is shortened from the document's: §5 writes "Customer unavailable at
    -- pickup/delivery", and the slash has no legal form under Docs/10 §4.7's derived lower snake
    -- case wire mapping. The rest are §5's own wording.
    --
    -- Stored in the document's sentence case, per Docs/10 §3.4, and held to admin.Categories by
    -- TestDisputeCategoryConstraintMatchesTheGoConstants.
    category text NOT NULL,

    -- "description" and "desired outcome". Both required: a dispute with neither is a report
    -- nobody can act on, and Docs/04 §7 asks for both by name. The bounds are generous — this is
    -- somebody describing an incident, not a milestone note — and finite, because an unbounded
    -- text column is a denial-of-service surface as much as a data-quality one.
    description text NOT NULL,
    desired_outcome text NOT NULL,

    -- "time of event".
    --
    -- The complainant's clock, and a **different instant from created_at** — that is the whole
    -- reason it is a column. A delivery that went wrong on Tuesday and is reported on Thursday has
    -- two timestamps, and collapsing them would put the report's time on the incident, which is
    -- the collapse Docs/02 §3.1 refuses for milestones and refuses here for the same reason.
    --
    -- It is required rather than defaulted for that reason. A default would silently write a
    -- fiction on every intake that omitted it, and a fiction in this column is one support cannot
    -- tell from a fact.
    --
    -- Not bounded against now(), on the same reasoning as milestones.actor_recorded_at: a device
    -- with a wrong clock still described something that happened, and refusing the record costs
    -- more than the wrong timestamp does. A future date is caught in Go, where it can be reported
    -- as a field error rather than as a constraint name.
    occurred_at timestamptz NOT NULL,

    -- "evidence".
    --
    -- **This is the field Docs/04 §7 names that has nowhere to live yet, and the reading taken is
    -- deliberate.** Verification evidence uploads to private object storage through short-lived
    -- pre-signed URLs (Docs/04 §3.1), and delivery proof does the same from SHIP-114 — neither
    -- exists, so there is no object a complainant could reference and no endpoint that would give
    -- them one.
    --
    -- What is captured instead is what the complainant can supply today: references in their own
    -- words — "photographed the crate at the depot", "the driver's message of 14/08". They are
    -- text and the platform does not resolve them. When uploads exist, an attachment is a row in a
    -- table of its own that points at this one, and nothing here changes.
    evidence text[] NOT NULL DEFAULT '{}',

    -- The key the intake was recorded under, so a retry cannot raise a second dispute.
    --
    -- Nullable because a dispute opened by an administrator (SHIP-164, SHIP-165) arrives through
    -- no client request and has no key behind it — the same reason milestones.idempotency_key is
    -- nullable, and the reason uq_disputes_idempotency below is partial.
    idempotency_key text,

    -- When the dispute stopped being open. **SHIP-164's column, present from the start because
    -- uq_disputes_open_per_job needs a predicate and this is the honest one.**
    --
    -- No status enumeration here, deliberately. Docs/04 §7 names three stages — intake,
    -- investigation, outcome — and SHIP-164 is the ticket that moves a dispute through them; a
    -- vocabulary invented at intake for a workflow that does not exist yet is a vocabulary the
    -- ticket that owns it would have to work around. Open or not is the one distinction intake
    -- genuinely makes.
    resolved_at timestamptz,

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    -- Docs/02 §1's two party names. 'admin' is deliberately absent: an administrator opening a
    -- dispute on somebody's behalf is SHIP-164's, it reaches this table through a different
    -- endpoint, and it would be recorded as an audit entry naming the administrator rather than as
    -- a complainant who was party to the delivery.
    CONSTRAINT ck_disputes_complainant_party CHECK (
        complainant_party IN ('customer', 'provider')
    ),

    CONSTRAINT ck_disputes_category CHECK (category IN (
        'Provider fails to arrive',
        'Goods differ from listing',
        'Customer unavailable',
        'Delivery is late',
        'Goods damaged or missing',
        'Other'
    )),

    CONSTRAINT ck_disputes_description CHECK (length(description) BETWEEN 1 AND 4000),
    CONSTRAINT ck_disputes_desired_outcome CHECK (length(desired_outcome) BETWEEN 1 AND 1000),

    -- A bound on the list, on what it may contain, and on the whole of it. The same kind of
    -- limit the text columns carry: a guard against a runaway field rather than a judgement
    -- about how much evidence a person may describe.
    --
    -- Written with array operators rather than `unnest`, because a CHECK constraint may not
    -- contain a subquery — which is what the per-item bound wants to be. Go bounds each item at
    -- 500 characters where it can report which one was wrong; this bounds the total, which is the
    -- half that has to be true of the table however it is written to.
    CONSTRAINT ck_disputes_evidence CHECK (
        cardinality(evidence) <= 20
        AND array_position(evidence, NULL) IS NULL
        AND '' <> ALL (evidence)
        AND length(array_to_string(evidence, '')) <= 2000
    ),

    -- The same bound httpx puts on the header (maxIdempotencyKeyLen), written out rather than
    -- trusted: the middleware protects the endpoint, and this protects the table from anything
    -- that ever writes to it without passing through one.
    CONSTRAINT ck_disputes_idempotency_key CHECK (
        idempotency_key IS NULL OR (length(idempotency_key) BETWEEN 1 AND 255)
    ),

    CONSTRAINT fk_disputes_job
        FOREIGN KEY (job_id) REFERENCES jobs (id) ON DELETE RESTRICT,

    CONSTRAINT fk_disputes_complainant
        FOREIGN KEY (complainant_id) REFERENCES users (id) ON DELETE RESTRICT
);

-- **One open dispute per job, and this index is what makes that true.**
--
-- Docs/02 §2 gives a job one route into 'Disputed' and Docs/02 §3 has that status freeze automatic
-- completion until an administrator resolves it. A job cannot be frozen twice, so a second open
-- dispute would be a row describing a state the job cannot be in — and the two would then
-- disagree about which one SHIP-164 is unfreezing.
--
-- A unique *index* rather than a constraint because the rule is partial, and a constraint cannot
-- be: a job accumulates disputes over its life while never having two open at once, exactly as
-- uq_driver_assignments_active lets a job accumulate drivers (000600).
--
-- It is also what makes two concurrent raises resolve to one. Both callers read no open dispute
-- and both insert; the second is refused by the btree rather than by a check in Go, which is the
-- only place that refusal can be correct.
CREATE UNIQUE INDEX uq_disputes_open_per_job
    ON disputes (job_id)
    WHERE resolved_at IS NULL;

-- **The other half of "a retry does not raise a second dispute", and it is a different guarantee
-- from the one above.**
--
-- SHIP-15's middleware replays the first response for a repeated key out of Redis, for as long as
-- the entry lives. That makes a retry cheap and protects nothing that outlives a TTL, an eviction
-- or a failover. This refuses the second row permanently, and it is what is still true when a
-- phone reconnects a day later — the same pairing SHIP-111 records at 000602.
--
-- Scoped per job for the reason 000602 gives: a client that reuses one key across two jobs has
-- made two requests that both deserve to succeed.
--
-- It is arbitrated ahead of uq_disputes_open_per_job by the writer's ON CONFLICT clause, which is
-- deliberate and is what makes the two answers distinguishable: a genuine retry is absorbed and
-- answered with the dispute it already raised, while a *fresh* key against a job that already has
-- an open dispute reaches this index's sibling above and is refused. Same table, two different
-- things for a client to do about it.
CREATE UNIQUE INDEX uq_disputes_idempotency
    ON disputes (job_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- Both foreign keys indexed, per Docs/10 §3.3. Newest first, because both are support questions:
-- everything that has been disputed about this job, and everything this account has raised.
--
-- uq_disputes_open_per_job covers job_id only while a dispute is open, so it cannot serve the
-- foreign key.
CREATE INDEX idx_disputes_job ON disputes (job_id, created_at DESC);
CREATE INDEX idx_disputes_complainant ON disputes (complainant_id, created_at DESC);

-- Docs/04 §5's sixth moderation queue: open disputes, oldest first, because §8 sets an
-- acknowledgement target of two business days and the oldest is the one closest to breaching it.
CREATE INDEX idx_disputes_open ON disputes (created_at) WHERE resolved_at IS NULL;

CREATE TRIGGER disputes_set_updated_at
    BEFORE UPDATE ON disputes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON TABLE disputes IS
    'Docs/04 §7 intake: job, complainant, category, description, desired outcome, time of event, evidence. One open dispute per job (uq_disputes_open_per_job); raising one moves the job to Disputed through the guard (Docs/02 §2).';
COMMENT ON COLUMN disputes.complainant_party IS
    'Which side of this job the complainant was on, resolved by the platform from the job and its accepted bid. Not users.role.';
COMMENT ON COLUMN disputes.occurred_at IS
    'When the complainant says it happened. A different instant from created_at, which is when they reported it. Never corrected.';
COMMENT ON COLUMN disputes.evidence IS
    'References in the complainant''s own words. Uploads land in a table of their own once pre-signed upload exists (SHIP-114).';
COMMENT ON COLUMN disputes.resolved_at IS
    'NULL while the dispute is open. Set by SHIP-164, which is also what unfreezes the job.';
