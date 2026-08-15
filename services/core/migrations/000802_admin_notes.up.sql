-- SHIP-162: what support writes down about a user or a job, and never shows them.
--
-- Docs/01 §4.6's fifth administrative capability — "add internal support notes" — and the *Done
-- when* is two claims: notes attach to a user or a job, and they are **never user-visible**.
--
-- # Why they get a table rather than a column
--
-- `000800`'s header already answered this from the other side: "SHIP-162's support notes attach to a
-- user or a job and are never user-visible. Every column below is read back to the complainant by
-- the intake response, so a note written into one of them would be a note the complainant reads.
-- Notes get rows of their own."
--
-- The same argument holds for `users` and for `jobs`. Both are read by their owners through
-- endpoints whose shapes grow over time, and a note column on either is one careless `SELECT *`
-- away from being published. A table nothing but the console reads cannot be added to a customer's
-- response by accident; it can only be added deliberately, by writing a join that is not there.
--
-- # The subject is polymorphic, and there is deliberately no foreign key
--
-- `subject_type` plus `subject_id`, with no FK to either table. Three reasons, in order of weight:
--
--   * **A note outlives its subject.** Docs/05 §3.1 keeps records beyond the account, and the note
--     explaining why an account was closed is the one most worth keeping after it is. An FK with
--     ON DELETE RESTRICT would make the note block the deletion; CASCADE would delete the record of
--     why. `audit_log.target_id` takes exactly this position for exactly this reason (`000003`).
--   * **Two parents cannot both be enforced.** A single column cannot reference two tables, and the
--     alternatives — two nullable FK columns with a CHECK that exactly one is set, or a table per
--     subject kind — are both more schema for a guarantee the console does not need. Whether the
--     subject still exists is a question the reader asks by opening it.
--   * **The kinds will grow.** Docs/04 §5 has six queues and §7 has disputes; a note on a dispute
--     or on a provider verification is a plausible next ticket, and it should be a value in a CHECK
--     rather than a third nullable column.
--
-- `ck_admin_notes_subject_type` is the closed list, paired against the Go constants by a test in
-- this package per Docs/10 §3.4 — a kind the database accepts and Go cannot name is a note nothing
-- can write, and one Go knows and the database refuses is a write that fails at run time.
--
-- # The author is a foreign key, and that one is real
--
-- `fk_admin_notes_author` to `admin_users` with ON DELETE RESTRICT. Unlike the subject, the author
-- is always an administrator of this platform and must always be nameable: a support note whose
-- author cannot be identified is a note nobody can weigh. RESTRICT rather than CASCADE for the same
-- reason `admin_sessions` uses it — deleting an administrator must not silently delete what they
-- recorded.
--
-- **`admin_users` rows are disabled rather than deleted** (`000801`), so RESTRICT is not a
-- constraint anybody meets in practice. It is here for the operator at a `psql` prompt.
--
-- # Notes are appended and never edited, and that is enforced here rather than by convention
--
-- There is no `updated_at` and no UPDATE path. A support note is a contemporaneous record of what
-- somebody knew at the time; one that can be revised afterwards is worth less than one that cannot,
-- in exactly the situation it exists for.
--
-- **It is deliberately weaker than `audit_log`, which has triggers refusing UPDATE and DELETE from
-- any connection.** That table holds administrators to account (Docs/04 §9) and its immutability is
-- an invariant of the platform; this one holds working notes, and an operator correcting a note
-- that names the wrong job is a legitimate act rather than a breach. The shape says append-only,
-- the schema does not forbid a correction, and the difference between the two tables is a decision
-- rather than an oversight.
--
-- What *is* audited is the addition: SHIP-150's entry names the subject, so "everything that
-- happened to this account" includes the notes taken about it.

CREATE TABLE admin_notes (
    id uuid PRIMARY KEY,

    -- What the note is about. See the header for why this is not a foreign key.
    subject_type text NOT NULL,
    subject_id   uuid NOT NULL,

    -- Who wrote it. Always an administrator of this platform, and always nameable.
    author_id uuid NOT NULL,

    -- The note itself.
    --
    -- Bounded at 4000 characters by ck_admin_notes_body. A floor as well as a ceiling: an empty
    -- note is a row that records nothing, and this is a table whose whole value is what is in this
    -- column. The ceiling is generous because a support note is prose rather than a reason field —
    -- audit_log.reason is capped at 500 in Go for the opposite reason, that it is a sentence.
    body text NOT NULL,

    created_at timestamptz NOT NULL DEFAULT now(),

    -- The closed list of subject kinds, paired against the Go constants by
    -- admin_notes_test.go (Docs/10 §3.4).
    CONSTRAINT ck_admin_notes_subject_type CHECK (subject_type IN ('user', 'job')),

    -- Trimmed before it is measured, so a note of four thousand spaces is not a note.
    --
    -- **The character set is spelled out, and that is not decoration.** `btrim(body)` with one
    -- argument strips **spaces only** — not tabs, not newlines — so a body of a single newline
    -- satisfies it while `strings.TrimSpace` in the service refuses the same value. The two would
    -- have disagreed about what an empty note is, and the disagreement would only ever have shown
    -- up through a connection that did not go through the service, which is exactly the connection
    -- this constraint exists for. Found by TestANoteMustRecordSomething, which tried a newline.
    CONSTRAINT ck_admin_notes_body
        CHECK (btrim(body, E' \t\r\n') <> '' AND length(body) <= 4000),

    CONSTRAINT fk_admin_notes_author
        FOREIGN KEY (author_id) REFERENCES admin_users (id) ON DELETE RESTRICT
);

-- The one read this table has: every note on one subject, newest first.
--
-- Composite on `(subject_type, subject_id, created_at DESC)` rather than two indexes, because the
-- query always supplies both halves of the subject — a note is opened from the thing it is about,
-- never listed across subjects. The trailing `created_at DESC` makes the ordering an index scan
-- rather than a sort, which matters on a job that has accumulated a support history.
--
-- `id` is not in the index and the ordering is not total on `created_at` alone. That is a deliberate
-- limit rather than an oversight: this read is **not paged**, because the notes on one subject are
-- tens at most and a console shows them all. A cursor over this table would need the identifier as
-- a tie-break and would need this index widened; whoever pages it does both.
CREATE INDEX idx_admin_notes_subject
    ON admin_notes (subject_type, subject_id, created_at DESC);

COMMENT ON TABLE admin_notes IS
    'Internal support notes about a user or a job (SHIP-162). Never user-visible: no endpoint '
    'outside /v1/admin reads this table, and no user-facing response joins it.';

COMMENT ON COLUMN admin_notes.subject_id IS
    'Deliberately not a foreign key — a note outlives its subject (Docs/05 §3.1), and one column '
    'cannot reference two tables. Whether the subject still exists is a question the reader asks '
    'by opening it.';
