-- SHIP-155a: what a customer or provider says is wrong with a job or with one message on it.
--
-- Docs/04 §5's **second** moderation queue — "reported jobs or messages" — from the producing end.
-- SHIP-156 is the queue itself, and its *Done when* is "reports surface with the job and
-- conversation in context"; **nothing anywhere created a report**, which is the gap `Docs/11` §6
-- carried as a strike for eleven passes before this row was written. A queue with no producer is
-- not blocked on a dependency; it is short a row.
--
-- Docs/04 §6 step 1 is the sentence this table serves: "Receive a report, automatic flag, or
-- support request." This is the first of the three.
--
-- # Raising a report moves no job status, and that is the one modelling decision worth the space
--
-- `000800` is the obvious template and this table departs from it in exactly one place. Raising a
-- **dispute** moves the job to 'Disputed' because Docs/02 §2 has a row for precisely that and §3
-- has the freeze it buys. Raising a **report** moves nothing, for two reasons that agree:
--
--   * **Docs/02 §2 has no transition for it.** There is no `→ Reported` status and no row from any
--     status to anything on receipt of a complaint about content. Inventing one here would put a
--     status in the lifecycle that Docs/02 does not have, which `CLAUDE.md` calls a defect rather
--     than a style choice.
--   * **Docs/04 §6 puts the outcome in an administrator's hands** — "select outcome: no action,
--     warning, content removal, job cancellation, restriction, suspension, or escalation" — *after*
--     step 2 has them review the job, the profile, the communications and the history. A report is
--     the input to that review, not its conclusion.
--
-- **And a report that froze a job would be a denial of service dressed as moderation.** Either
-- party could stop the other's delivery on their own say-so, with no administrator involved and
-- nothing to stop them doing it again. The dispute freeze is safe precisely because Docs/02 §3
-- bounds it — one open dispute per job, and an administrator resolves it — and none of that
-- machinery applies to a complaint about a listing's wording.
--
-- So there is no status column here about the *job*, for `000800`'s reason as well: the job's status
-- lives on the job, and a copy here would be a second answer to a question that already has one.
--
-- # The subject is a job or a message, never both
--
-- The ticket's *Done when* asks for exactly this, "so SHIP-156's queue can open the context the
-- report is actually about". It is spelled `subject_type` plus a nullable `message_id`, held
-- together by `ck_reports_subject`.
--
-- **`job_id` is NOT NULL on both kinds, and it is context rather than a second subject.** A message
-- belongs to a job (`000506`), so a report about a message is still a report about something that
-- happened on one — and the queue, the party check and the moderator's screen all need the job
-- either way. What `ck_reports_subject` refuses is a row that names a message *and* claims to be
-- about the job, or claims to be about a message and names none.
--
-- **`subject_type` is not inferable-and-therefore-redundant, and that was the alternative
-- considered.** `message_id IS NULL` would discriminate the two kinds on its own. It is written out
-- because `moderation.go` already settled the question for this domain's other union — an entry
-- "says why it is here rather than leaving a console to infer it from which fields are blank" — and
-- because Docs/04 §5's queue will grow: a report about a *profile* is a plausible next ticket, and
-- it should be a value in this CHECK rather than a third nullable column. `admin_notes` (`000802`)
-- takes the same position on the same trade for the same reason.
--
-- # Both foreign keys are real here, unlike `admin_notes`
--
-- `000802` deliberately has no FK on its subject, because a support note outlives its subject and
-- the note explaining why an account was closed is the one most worth keeping after it is. A report
-- is the opposite case: it is *about* a live thing an administrator is going to open and read, and
-- a report pointing at a job that is not there is one nobody can action. ON DELETE RESTRICT on both,
-- per Docs/10 §3.3 and for `000800`'s reading of Docs/05 §3.1 — a job with a report on it is
-- cancelled rather than removed.
--
-- **What no foreign key can say is that the message is on the job**, because that is a comparison
-- across two tables. `internal/admin` makes it in Go before it inserts, exactly as `000506`'s header
-- records `internal/bidding` doing for "this provider has bid on this job".
--
-- # Reports are appended and never edited
--
-- No `updated_at` and no UPDATE path, which is `admin_notes`' shape rather than `disputes`'.
-- `disputes` is rewritten because SHIP-164 moves one through investigation to an outcome; **no
-- ticket does that to a report**, and Docs/04 §6's outcomes are recorded where they already are —
-- `audit_log` (`000003`), the enforcement rows (`000801`+), and the job's own history.
--
-- **So there is deliberately no `resolved_at` and no status vocabulary**, and `moderation.go`
-- already wrote down why for this domain's other queues: "it is not a work queue: there is no claim,
-- no assignment and no 'done', because Docs/04 §6's process ends in a recorded decision rather than
-- in a queue entry being ticked off". `000800` added `resolved_at` at intake only because
-- `uq_disputes_open_per_job` needed a predicate; nothing here needs one, and a vocabulary invented
-- for a workflow that does not exist yet is one the ticket that owns it would have to work around.
--
-- # There is no "one open report per job", and that is not an omission
--
-- `disputes` has `uq_disputes_open_per_job` because a job is frozen once. Nothing here freezes, and
-- two parties reporting the same job for two different reasons are two things a moderator wants to
-- see. What is still refused is the *same request twice* — `uq_reports_idempotency` below.

CREATE TABLE reports (
    id uuid PRIMARY KEY,

    -- The job this is about, or the job the reported message is on. Never null on either kind:
    -- see the header — it is the context SHIP-156 opens and the scope the party check runs against.
    job_id uuid NOT NULL,

    -- What the report is about. See the header for why this is written out rather than inferred
    -- from message_id, and paired against the Go constants by
    -- TestReportSubjectConstraintMatchesTheGoConstants (Docs/10 §3.4).
    subject_type text NOT NULL,

    -- Which message, when the subject is one. NULL on a report about the job itself.
    message_id uuid,

    -- "its reporter", as two columns rather than one — `000800`'s split, for its reason.
    --
    -- reporter_id is the account. reporter_party is which side of *this job* they were on, and it
    -- is not the same fact as users.role: being the customer who owns the job, or the provider who
    -- won it, is a stronger statement than carrying a role claim. It is resolved by the platform
    -- from the job and its accepted bid, never taken from the request — which is the *Done when*
    -- clause "a party cannot report a job they are not on".
    reporter_id uuid NOT NULL,
    reporter_party text NOT NULL,

    -- "a reason from a closed list".
    --
    -- **No document enumerates these**, exactly as Docs/04 §7 named `disputes.category` and
    -- enumerated nothing. `000800` answered that by deriving its list from the one document that
    -- does enumerate what goes wrong on a delivery; this derives from the documents that enumerate
    -- what Shipper refuses to carry and what it escalates. Every value traces to a closed line:
    --
    --   Prohibited goods         Docs/01 §2's out-of-scope list — "dangerous goods, live animals,
    --                            people, or specialist regulated freight" — and Docs/05 §4's policy
    --                            position, "illegal, living, dangerous/specialist goods unless
    --                            expressly supported". SHIP-59 refuses these at publication by
    --                            category; this is the same policy reported by a person, which is
    --                            what catches a prohibited load described in prose under a
    --                            permitted category.
    --   Misleading listing       Docs/03's Create-job row — "validate fields and flag
    --                            prohibited/unclear goods", against the risk "poor job data leads
    --                            to poor bids". The automated half is SHIP-59; this is the half a
    --                            provider notices after bidding.
    --   Dealing outside Shipper  Docs/03's Negotiate row, whose stated opportunity is "reduce
    --                            off-platform activity". Payments being outside Shipper for the
    --                            MVP (Docs/05 §4) is what makes this reportable rather than
    --                            obvious: the marketplace is the part that must not move.
    --   Abusive or threatening   Docs/04 §6's escalation sentence — "credible threats".
    --   Suspected fraud          the same sentence — "suspected criminal activity".
    --   Safety concern           the same sentence — "urgent safety".
    --   Other                    the escape hatch, for `000800`'s reason: a closed list with none
    --                            turns every complaint nobody anticipated into a mis-filed one, and
    --                            the filing is what a moderator triages from.
    --
    -- **One string is not the document's**, and it is the same shortening `000800` had to make.
    -- Docs/03 writes "off-platform activity", and the hyphen in that compound has no legal form
    -- under Docs/10 §4.7's derived lower snake case wire mapping — `off-platform_dealing` is not an
    -- identifier in any of the three client languages, exactly as `000800`'s slash was not. The
    -- value is reworded rather than tabulated as an exception, because a hand-written exception is
    -- the second list the derivation exists to avoid.
    --
    -- The list is deliberately **one list for both subject kinds**. A reason that applied to only
    -- one would need a second list and a cross-check between them, for a distinction a moderator
    -- does not make: a message can describe prohibited goods and a listing can be abusive.
    --
    -- Stored in the sentence case Docs/10 §3.4 asks for, and held to admin.Reasons by
    -- TestReportReasonConstraintMatchesTheGoConstants.
    reason text NOT NULL,

    -- "and a description". Required: a reason on its own is a category, and Docs/04 §6 step 2 has
    -- somebody review the report before choosing an outcome — with nothing to read, the reason is
    -- all they have. Bounded generously and finitely, as `000800` bounds its own account.
    description text NOT NULL,

    -- The key the report was recorded under, so a retry cannot raise a second.
    --
    -- Nullable for `000800`'s reason and `000602`'s before it: a row written by anything other
    -- than a client request has no key behind it, which is why uq_reports_idempotency is partial.
    idempotency_key text,

    -- "and the moment it was made". The platform's clock, and the only instant this table holds.
    --
    -- **There is no `occurred_at` here, unlike `000800`.** Docs/04 §7 names "time of event" as a
    -- dispute intake field and Docs/04 §5 names no such thing for a report — and the subject of a
    -- report is a *thing that is still there to be looked at*, a listing or a message, rather than
    -- an incident that happened out of sight. The moderator opens the subject; they do not have to
    -- reconstruct when it occurred. A second timestamp nobody reads is one more field to get wrong.
    created_at timestamptz NOT NULL DEFAULT now(),

    -- The closed list of subject kinds. Paired against the Go constants per Docs/10 §3.4.
    CONSTRAINT ck_reports_subject_type CHECK (subject_type IN ('job', 'message')),

    -- **"a report names a job or a message and never both."**
    --
    -- The *Done when*'s clause, as a constraint rather than as a convention. Both halves matter:
    -- a report about the job may not carry a message, and a report about a message must carry one.
    -- Without the second half a client could send `subject_type = 'message'` and no id, and
    -- SHIP-156 would have a row claiming a conversation it cannot open.
    CONSTRAINT ck_reports_subject CHECK (
        (subject_type = 'job' AND message_id IS NULL)
        OR (subject_type = 'message' AND message_id IS NOT NULL)
    ),

    -- Docs/02 §1's two party names. 'admin' is deliberately absent: an administrator does not
    -- report, they act — Docs/04 §6 step 4 — and what they did is an audit entry naming them.
    CONSTRAINT ck_reports_reporter_party CHECK (reporter_party IN ('customer', 'provider')),

    CONSTRAINT ck_reports_reason CHECK (reason IN (
        'Prohibited goods',
        'Misleading listing',
        'Dealing outside Shipper',
        'Abusive or threatening',
        'Suspected fraud',
        'Safety concern',
        'Other'
    )),

    -- Trimmed before it is measured, so a description of four thousand spaces is not a description.
    --
    -- **The character set is spelled out, and `000802` is why.** `btrim(description)` with one
    -- argument strips *spaces only* — not tabs, not newlines — so a body of a single newline would
    -- satisfy it while `strings.TrimSpace` in the service refuses the same value, and the two would
    -- disagree only through a connection that never went through the service, which is exactly the
    -- connection a CHECK exists for. `ck_admin_notes_body` learned this from a test that tried a
    -- newline; `ck_disputes_description` is the earlier, weaker `BETWEEN 1 AND 4000` form and is
    -- left as it is rather than changed under a ticket that does not own it.
    CONSTRAINT ck_reports_description
        CHECK (btrim(description, E' \t\r\n') <> '' AND length(description) <= 4000),

    -- The same bound httpx puts on the header (maxIdempotencyKeyLen), written out rather than
    -- trusted: the middleware protects the endpoint, and this protects the table from anything that
    -- ever writes to it without passing through one.
    CONSTRAINT ck_reports_idempotency_key CHECK (
        idempotency_key IS NULL OR (length(idempotency_key) BETWEEN 1 AND 255)
    ),

    CONSTRAINT fk_reports_job
        FOREIGN KEY (job_id) REFERENCES jobs (id) ON DELETE RESTRICT,

    -- Real, unlike `admin_notes.subject_id` — see the header. A report points at something a
    -- moderator is about to open, and one pointing at a message that is not there is unactionable.
    CONSTRAINT fk_reports_message
        FOREIGN KEY (message_id) REFERENCES job_messages (id) ON DELETE RESTRICT,

    CONSTRAINT fk_reports_reporter
        FOREIGN KEY (reporter_id) REFERENCES users (id) ON DELETE RESTRICT
);

-- **A retry does not raise a second report**, and this is what is still true after Redis has
-- forgotten the request.
--
-- SHIP-15's middleware replays the first response for a repeated key for as long as its entry
-- lives; that makes a retry cheap and protects nothing that outlives a TTL, an eviction or a
-- failover. This refuses the second row permanently — the same pairing `000602` records for
-- milestones and `000800` for disputes.
--
-- Scoped per job for their reason: a client that reuses one key across two jobs has made two
-- requests that both deserve to succeed.
--
-- **Not scoped per message.** Two reports on two messages of one job, under one key, are one key
-- used for two actions — which is the thing an idempotency key means the opposite of. The service
-- tells that apart from a genuine retry by comparing the subject and the reason, and answers
-- `idempotency_key_reused` rather than the wrong report.
CREATE UNIQUE INDEX uq_reports_idempotency
    ON reports (job_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

-- Every foreign key indexed, per Docs/10 §3.3.
--
-- The first two are newest-first because both are support questions: everything reported about this
-- job, and everything this account has reported — the second being how a moderator sees somebody
-- who reports every provider who outbids them.
CREATE INDEX idx_reports_job ON reports (job_id, created_at DESC);
CREATE INDEX idx_reports_reporter ON reports (reporter_id, created_at DESC);

-- Partial, because the column is NULL on every report about a job itself and an index entry for
-- each of those would be dead weight. It serves the foreign key and SHIP-156's "has this message
-- been reported before".
CREATE INDEX idx_reports_message ON reports (message_id) WHERE message_id IS NOT NULL;

-- Docs/04 §5's second queue: every report, oldest first, because Docs/04 §8 sets an acknowledgement
-- target and the oldest is the one closest to breaching it. This is `idx_disputes_open`'s
-- counterpart with no predicate — there is nothing to be open *about*, per the header.
--
-- `(created_at, id)` rather than `(created_at)` alone so SHIP-156's cursor has a total order.
-- `admin_notes` records the opposite decision deliberately — the notes on one subject are tens at
-- most and are not paged — and this is a platform-wide queue, which is the case that needs the
-- tie-break. Adding it now costs nothing and saves SHIP-156 widening the index.
CREATE INDEX idx_reports_queue ON reports (created_at, id);

COMMENT ON TABLE reports IS
    'Docs/04 §5''s second moderation queue, from the producing end (SHIP-155a): a party to a job '
    'reports the job or one message on it. Moves no job status — Docs/02 §2 has no transition and '
    'Docs/04 §6 puts the outcome in an administrator''s hands.';

COMMENT ON COLUMN reports.job_id IS
    'The job reported, or the job the reported message is on. Never null: it is the context '
    'SHIP-156 opens and the scope the party check runs against.';

COMMENT ON COLUMN reports.message_id IS
    'The message reported, or NULL when the subject is the job itself. ck_reports_subject holds '
    'this to subject_type. That the message is on job_id is checked in Go — no foreign key can '
    'compare two tables.';

COMMENT ON COLUMN reports.reporter_party IS
    'Which side of this job the reporter was on, resolved by the platform from the job and its '
    'accepted bid. Not users.role.';
