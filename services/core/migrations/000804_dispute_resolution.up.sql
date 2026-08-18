-- SHIP-164: the documented outcome a dispute is resolved with.
--
-- `000800` built intake and left `resolved_at` on the table from the start — "SHIP-164's column,
-- present from the start because uq_disputes_open_per_job needs a predicate and this is the honest
-- one". This migration adds the other two columns that turn "not open any more" into a *documented*
-- outcome: what was decided, and who decided it.
--
-- # The outcome is its own column, because Docs/04 §7 and Docs/02 §2 are two different vocabularies
--
-- This is the decision the ticket turns on and it is recorded here rather than in a commit message.
--
-- `Docs/04` §7 lists five possible outcomes of a dispute:
--
--     "Delivery completed as agreed. / Delivery issue acknowledged; parties directed to resolve
--      externally. / Job cancelled / failed delivery recorded. / User warning, restriction, or
--      suspension. / Referral to legal, insurer, or authorities where required."
--
-- `Docs/02` §2 offers a *disputed job* exactly two ways out: `Disputed → Completed` ("admin resolves
-- dispute with delivery accepted") and `Disputed → Cancelled` ("admin resolves as cancelled/failed
-- delivery"). **Three of §7's five map to neither.** Directing the parties to settle between
-- themselves, warning or restricting somebody, and referring a matter to an insurer are all things
-- that can be true of a delivery that nevertheless completed *or* failed.
--
-- Two wrong answers were available and both were rejected.
--
--   - **Invent a thirteenth job status.** `Docs/02` §1 has twelve and the transition table is
--     authoritative for the guard; a status added here to carry a moderation vocabulary would be a
--     status every feed, every eligibility query and every expiry sweep had to learn, for a fact
--     that is not about where the job is.
--   - **Drop the three outcomes that do not map.** That is `Docs/04` §7 quietly reduced to the two
--     rows `Docs/02` §2 happens to have, in the one field that records *what an administrator
--     decided*.
--
-- So the two vocabularies are orthogonal and are stored as such. `outcome` below is §7's, and where
-- the job goes is §2's — chosen separately by the administrator, in the same request, and applied
-- through the one guarded transition. Every resolution does both: it records an outcome **and**
-- unfreezes the job into Completed or Cancelled.
--
-- # There is no column for where the job went, deliberately
--
-- `000800` refused a job-status column on this table — "the job's status lives on the job, and a
-- copy here would be a second answer to a question that already has one" — and that refusal survives
-- the split above. The destination is on `jobs.status`, the move is in `job_status_history` with its
-- actor and its reason, and the administrator's own choice is in `audit_log.metadata`. Three records
-- and no fourth.
--
-- # There is no reason column either, and `000803` is not the precedent — SHIP-161 is
--
-- The reason an administrator gives is required, and it is already written twice: into
-- `job_status_history.reason`, which `ck_job_status_history_admin_reason` compels of any
-- administrator's transition and which a customer's support conversation reads, and into
-- `audit_log.reason`, which is append-only and is what `Docs/04` §9's controls read. A third copy
-- here would be the mutable one, and a mutable copy of an immutable fact is the one somebody later
-- corrects. SHIP-161 took that position for `users` and it is the same argument.
--
-- It also keeps `000800`'s other rule intact: **every column on this table is a column the
-- complainant could be shown**, because the intake response reads the row straight back. An
-- administrator's internal reasoning is not, and notes get rows of their own (`000802`).
--
-- # A dispute is resolved once, and the three columns move together
--
-- `ck_disputes_resolution` is the whole of that. Before: all three NULL. After: all three set. There
-- is no half-resolved state and no way to reach one — a row with `resolved_at` and no outcome would
-- be a dispute off `idx_disputes_open` with nothing documented about it, which is precisely the
-- state the ticket's *Done when* exists to prevent.
--
-- It is a constraint rather than a rule in Go for `000803`'s reason: application logic refusing to
-- write is a convention, and a convention does not apply to a repair script, a support query typed
-- at a psql prompt, or the next endpoint somebody adds without reading `internal/admin`.
--
-- # There is deliberately no `resolved_at >= created_at`, and the reason is a finding rather than an
-- # oversight
--
-- One was written, applied, and removed. It refuses the one ordering that cannot be true of any
-- clock — a dispute settled before it was reported — and it costs nothing in production, where every
-- clock is `clock.System` and a resolution follows its intake by minutes or days.
--
-- **It failed against the test suite, and what it found is a two-clock inconsistency this migration
-- does not own.** `disputes.created_at` is `DEFAULT now()` — the *database's* clock — while
-- `resolved_at` is written from the domain's injected one. `Docs/11` §9's rule is *one row, one
-- clock*: where a domain injects one, the column takes its value from the domain, and `admin.Service`
-- has injected a clock since SHIP-163. So a test holding `clock.Fixed` in the past resolves a dispute
-- at an instant *earlier* than the `now()` the same row was inserted with, and the constraint
-- correctly refuses a row that is correctly written by code that is inconsistent about which clock it
-- is on. `audit.go`'s header records the same defect one table along in `admin_sessions`, where "the
-- suite agreed with the database for exactly one idle window and then failed for ever".
--
-- Repairing it means changing what `raised_at` means on SHIP-163's intake response, which is that
-- ticket's column and outside this one. **The constraint is left out rather than the inconsistency
-- left hidden**: it is recorded here, in `Docs/11` §3, and in the handover, so whoever takes it has
-- the measurement rather than the symptom. Adding the constraint afterwards is one line.

ALTER TABLE disputes
    -- What the administrator decided, in Docs/04 §7's vocabulary. NULL while the dispute is open.
    --
    -- Stored in sentence case per Docs/10 §3.4, and held to admin.Outcomes in both directions by
    -- TestDisputeOutcomeConstraintMatchesTheGoConstants. The strings are shortened from §7's
    -- sentences, on exactly the grounds `ck_disputes_category` records for its own list: the wire
    -- form is derived by lower-snake-casing the stored form (Docs/10 §4.7), and a semicolon, a
    -- comma or a slash has no legal form under that mapping in any of the three client languages.
    -- What each one is short for is named on the Go constant.
    ADD COLUMN outcome text,

    -- Which administrator resolved it. `admin_users`, not `users`: the two are separate credential
    -- systems that cannot be exchanged for each other (SHIP-147, `000801`), and a resolution is an
    -- act on the administrator credential.
    --
    -- ON DELETE RESTRICT, per Docs/10 §3.3 and for the reason `fk_disputes_complainant` carries: a
    -- resolution that could lose the name of who took it is a resolution nobody is accountable for.
    ADD COLUMN resolved_by uuid;

ALTER TABLE disputes
    ADD CONSTRAINT ck_disputes_outcome CHECK (
        outcome IS NULL OR outcome IN (
            'Delivery completed as agreed',
            'Delivery issue acknowledged',
            'Failed delivery recorded',
            'Warning restriction or suspension',
            'Referred to legal insurer or authorities'
        )
    ),

    -- Open or resolved, and nothing in between. See the header.
    ADD CONSTRAINT ck_disputes_resolution CHECK (
        (resolved_at IS NULL AND outcome IS NULL AND resolved_by IS NULL)
        OR (resolved_at IS NOT NULL AND outcome IS NOT NULL AND resolved_by IS NOT NULL)
    ),

    ADD CONSTRAINT fk_disputes_resolved_by
        FOREIGN KEY (resolved_by) REFERENCES admin_users (id) ON DELETE RESTRICT;

-- The settled half of Docs/04 §5's sixth queue, newest first.
--
-- `idx_disputes_open` (000800) serves the open half oldest-first, because §8 sets an acknowledgement
-- target and the oldest entry is closest to breaching it. This one is the opposite ordering for the
-- opposite reason: a resolved dispute is looked up to see what was decided, and the decision
-- somebody is asking about is overwhelmingly a recent one — the same split the account and job
-- searches make against the review queues.
--
-- Two columns rather than one, so the cursor the console pages with is **total**. Several disputes
-- resolved in one sitting share a `resolved_at` to the microsecond when one administrator works
-- through a morning's queue, and a single-column cursor would either repeat a row or skip one at
-- exactly the page boundary. `idx_disputes_open` needs no such tie-break added to it: it is
-- `000800`'s index, this ticket only reads it, and the tie-break is served from the primary key.
CREATE INDEX idx_disputes_resolved
    ON disputes (resolved_at DESC, id DESC)
    WHERE resolved_at IS NOT NULL;

COMMENT ON COLUMN disputes.outcome IS
    'Docs/04 §7''s outcome, in this platform''s shortened spelling. NULL while the dispute is open. Orthogonal to where the job went, which is Docs/02 §2''s two rows and lives on jobs.status.';
COMMENT ON COLUMN disputes.resolved_by IS
    'The admin_users row that resolved it. NULL while the dispute is open.';
