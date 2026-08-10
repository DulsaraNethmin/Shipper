-- SHIP-149: the append-only record of who did what.
--
-- This lands in the foundation rather than in M6 with the rest of administration, for one
-- reason: Docs/09 lists audit among the things that must not be cut because it is "impossible
-- to backfill". An audit trail started late has a hole exactly where the early, least
-- careful changes are, and nothing can fill it afterwards.

CREATE TABLE audit_log (
    id           uuid        PRIMARY KEY,

    -- Who acted. actor_id is nullable because 'system' has no account: the expiry sweep and
    -- the auto-complete timer both act, and both need to be attributable to something.
    actor_type   text        NOT NULL,
    actor_id     uuid,

    -- What they did, as a stable identifier rather than a sentence: 'job.unpublished',
    -- 'user.suspended', 'verification.rejected'. Docs/04 §6 requires the decision, the actor,
    -- the timestamp and the reason to be recorded together; free text would make the log
    -- unsearchable by action, which is how support will use it.
    action       text        NOT NULL,

    -- What it was done to. Deliberately not a foreign key: an audit entry must outlive its
    -- subject. Docs/05 §3.1 keeps audit history after a user is deleted, and a cascade or a
    -- RESTRICT would either destroy the record or block the deletion.
    target_type  text        NOT NULL,
    target_id    uuid        NOT NULL,

    -- Why. Required for the privileged actions in Docs/01 §3 — an administrator may not change
    -- a commercial record without an auditable reason — and left nullable only because routine
    -- system actions have no reason beyond the action itself.
    reason       text,

    -- Anything else worth keeping: the before and after of a changed field, the evidence
    -- reference behind a moderation decision.
    metadata     jsonb       NOT NULL DEFAULT '{}'::jsonb,

    -- No updated_at, and no trigger. A row that can be updated is not an audit entry.
    created_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ck_audit_log_actor_type CHECK (actor_type IN ('admin', 'user', 'system')),

    -- An account-backed actor must say which account. 'system' must not claim one.
    CONSTRAINT ck_audit_log_actor_id CHECK (
        (actor_type = 'system' AND actor_id IS NULL) OR
        (actor_type <> 'system' AND actor_id IS NOT NULL)
    )
);

-- The two ways this table is read: everything about one subject, and everything one actor did.
-- Both are support questions, and both are asked newest-first.
CREATE INDEX idx_audit_log_target ON audit_log (target_type, target_id, created_at DESC);
CREATE INDEX idx_audit_log_actor  ON audit_log (actor_type, actor_id, created_at DESC);
CREATE INDEX idx_audit_log_action ON audit_log (action, created_at DESC);

-- Append-only, enforced by the database rather than by convention.
--
-- CLAUDE.md and Docs/04 §9 both state that ordinary administrators cannot delete audit
-- history. Stating it is not enough: the log's value rests entirely on nobody being able to
-- edit it after the fact, and an application-level rule is exactly what someone with a psql
-- prompt bypasses. A trigger refuses the write wherever it comes from.
--
-- A retention job that genuinely needs to remove old entries will have to disable this
-- deliberately, which is the correct amount of friction.
CREATE OR REPLACE FUNCTION audit_log_is_append_only() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'audit_log is append-only: % is not permitted', TG_OP
        USING HINT = 'Correct the record by appending a new entry that supersedes it.';
END;
$$;

CREATE TRIGGER audit_log_no_update
    BEFORE UPDATE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_is_append_only();

CREATE TRIGGER audit_log_no_delete
    BEFORE DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_is_append_only();

COMMENT ON TABLE audit_log IS
    'Append-only. UPDATE and DELETE are refused by trigger (Docs/04 §9). Survives deletion of its subject (Docs/05 §3.1).';
