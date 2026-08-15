-- SHIP-166: two-person review for permanent suspension.
--
-- Docs/04 §9's second required internal control — "two-person review for permanent account
-- suspension where practical" — and this is the table that makes it a fact rather than a
-- convention.
--
-- # Why suspension and not the other two standings
--
-- `users.status` has three values (`000002`). `restricted` narrows what an account may do and is
-- reversible in one action; `active` is the reversal. **`suspended` is the one that takes the
-- account away**: `identity.User.CanSignIn` refuses it at sign-in and the session service refuses
-- it at refresh, so a suspended person cannot reach the platform to ask why. Docs/04 §9 says
-- "permanent", and suspension is the only standing this schema has that means it.
--
-- Reinstatement stays a single administrator's action, deliberately. A control that made undoing
-- a mistake as slow as making one would leave somebody locked out while two people found each
-- other.
--
-- # The two-person rule is here as well as in Go, and that is the point
--
-- `ck_suspension_reviews_two_people` is what makes this a control. Application logic refusing to
-- write is a convention, and a convention does not apply to a support query typed at a psql
-- prompt, to a repair script, or to the next endpoint somebody adds without reading
-- `internal/admin/suspension.go`. `000005_users_role_is_immutable` argues the same position for
-- the same reason, and `Docs/06` §4.1 puts it in general form: a mock happily accepts the write
-- the actual constraint exists to reject.
--
-- The service checks it too, one statement earlier, so that a moderator gets a message naming
-- what happened rather than a constraint name. Two checks that are believed to agree are two
-- checks until something compares them (`Docs/10` §3.4), which is why both are exercised.
--
-- # No `decided_by` for a rejection, and no rejection at all
--
-- A review is `pending`, `approved` or `withdrawn`. There is deliberately no `rejected`: a second
-- administrator who disagrees says so to the first, and the request is withdrawn by whoever made
-- it. Recording a rejection would make this a workflow with two outcomes to route and would put a
-- disagreement between colleagues in the one table that cannot be corrected — `audit_log` records
-- what was *done*, and nothing was.
--
-- Withdrawal has no endpoint yet and the status exists anyway, because the alternative is a
-- pending review nobody can clear except by approving it. The column is where a later ticket
-- writes; nothing reads it as a decision.

CREATE TABLE suspension_reviews (
    id uuid PRIMARY KEY,

    -- The account proposed for suspension.
    --
    -- ON DELETE RESTRICT per Docs/10 §3.3, like every other reference to `users`: SHIP-171
    -- pseudonymises an account rather than removing it, so nothing should be deleting the row at
    -- all, and a cascade here would destroy the record of a control being exercised.
    user_id uuid NOT NULL,

    -- Who asked. A foreign key, unlike `admin_notes.subject_id` and unlike `audit_log.target_id`,
    -- and the difference is the same one 000802 draws: an author must always be nameable. A
    -- review whose requester cannot be identified is a two-person rule with one person in it.
    requested_by uuid NOT NULL,

    -- Why, and it is required at the *request* rather than at the approval. The second
    -- administrator is being asked to agree with a reason, and a request that carried none would
    -- make the control a formality — somebody clicking approve on a proposition nobody stated.
    --
    -- Bounded here only against blankness. The length limits are the validator's business
    -- (Docs/06 §5.3, and 000404's comment settles it for this schema).
    --
    -- btrim with an explicit character set: PostgreSQL's one-argument btrim strips spaces only,
    -- which `ck_admin_notes_body` got wrong and a test caught.
    reason text NOT NULL,

    -- Who agreed, and when. Null while the review is pending.
    approved_by uuid,
    approved_at timestamptz,

    status text NOT NULL DEFAULT 'pending',

    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ck_suspension_reviews_reason
        CHECK (btrim(reason, E' \t\r\n') <> ''),

    CONSTRAINT ck_suspension_reviews_status
        CHECK (status IN ('pending', 'approved', 'withdrawn')),

    -- **The control.** A second approver who is the first requester is one person performing a
    -- two-person review, which is precisely what Docs/04 §9 asks the platform to prevent.
    CONSTRAINT ck_suspension_reviews_two_people
        CHECK (approved_by IS NULL OR approved_by <> requested_by),

    -- An approved review names its approver and its instant; a pending one names neither. Without
    -- this a row could claim to be approved by nobody, which is the shape a partial write leaves
    -- and the shape somebody reading the table would trust.
    CONSTRAINT ck_suspension_reviews_approval_is_complete
        CHECK ((status = 'approved') = (approved_by IS NOT NULL)
               AND (approved_by IS NULL) = (approved_at IS NULL)),

    CONSTRAINT fk_suspension_reviews_user
        FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE RESTRICT,

    CONSTRAINT fk_suspension_reviews_requested_by
        FOREIGN KEY (requested_by) REFERENCES admin_users (id) ON DELETE RESTRICT,

    CONSTRAINT fk_suspension_reviews_approved_by
        FOREIGN KEY (approved_by) REFERENCES admin_users (id) ON DELETE RESTRICT
);

-- One pending review per account.
--
-- A partial unique index rather than application logic, on exactly the reasoning
-- `uq_bids_one_accepted_per_job` records: two moderators opening the same account at the same
-- moment is the ordinary case, and a SELECT-then-INSERT loses that race. Two pending reviews would
-- also let one administrator approve the *other's* request while their own waited, which satisfies
-- the letter of a two-person rule while one person drives both halves.
CREATE UNIQUE INDEX uq_suspension_reviews_one_pending
    ON suspension_reviews (user_id)
    WHERE status = 'pending';

-- The queue read: pending reviews, oldest first, which is the order Docs/04 §8's targets make
-- meaningful.
CREATE INDEX idx_suspension_reviews_pending
    ON suspension_reviews (created_at)
    WHERE status = 'pending';

-- Every foreign key is indexed (Docs/10 §3.3). user_id is covered by the partial index above only
-- while a review is pending, so the full index is what serves an account's history.
CREATE INDEX idx_suspension_reviews_user ON suspension_reviews (user_id, created_at DESC);
CREATE INDEX idx_suspension_reviews_requested_by ON suspension_reviews (requested_by);
CREATE INDEX idx_suspension_reviews_approved_by ON suspension_reviews (approved_by);

CREATE TRIGGER suspension_reviews_set_updated_at
    BEFORE UPDATE ON suspension_reviews
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON TABLE suspension_reviews IS
    'Docs/04 §9''s two-person review for permanent suspension (SHIP-166). The rule that the approver is not the requester is ck_suspension_reviews_two_people, not application logic alone.';
COMMENT ON COLUMN suspension_reviews.approved_by IS
    'The second administrator. Never equal to requested_by — ck_suspension_reviews_two_people refuses it, which is what makes this a control rather than a convention.';
