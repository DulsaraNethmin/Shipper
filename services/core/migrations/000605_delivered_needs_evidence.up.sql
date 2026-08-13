-- SHIP-118: a delivered milestone has evidence behind it, or it is not written.
--
-- **This is CLAUDE.md's invariant in the database**: "Delivered requires photo proof or a recorded
-- exception reason — never neither." Docs/01 §4.4 decides it, Docs/02 §3 restates it as a condition
-- on the `In transit → Delivered` row, and until this migration nothing but a Go `if` was holding
-- it.
--
-- # Why it is here as well as in the domain, when the domain is not wrong
--
-- delivery.Service.RecordMilestone refuses it first and answers `delivery_proof_required` with a
-- message naming the camera and the exception path beside it. That layer is the one a client can
-- act on and this one is not: a constraint gives a name and a 500 (Docs/10 §4.6). What this buys is
-- the other half — **the rule stops depending on which function did the writing.** Nothing else in
-- this repository can write a `milestones` row today; SHIP-121 gives the driver's portal a
-- milestone endpoint, SHIP-113 rewrites the switch that decides what an administrative conflict
-- does with one, and cmd/worker already applies transitions on a timer. Each of those is a second
-- writer, and the rule they must not be able to break is the one CLAUDE.md lists as a defect rather
-- than a style choice.
--
-- It is the same division 000600's uq_driver_assignments_active makes, one milestone along: the
-- service checks so that a caller is told something useful, and the database enforces so that being
-- told is not the mechanism.
--
-- # A deferred constraint trigger, because the evidence is written after the milestone
--
-- `proofs.milestone_id` points at `milestones.id`, so the milestone has to exist before its
-- evidence can. An ordinary `AFTER INSERT` trigger would fire between the two statements and refuse
-- every honest delivery there is.
--
-- `DEFERRABLE INITIALLY DEFERRED` moves the check to COMMIT, which is exactly the moment the
-- question is answerable: by then the transaction holds a milestone and either does or does not
-- hold the row that stands behind it. Nothing in this platform writes one without the other in the
-- same transaction — delivery.Service.RecordMilestone is explicit that they are one act — so the
-- deferral costs one indexed lookup per delivered milestone and refuses nothing legitimate.
--
-- `WHEN (NEW.milestone = 'Delivered')` keeps every other milestone free of it. Four of the five
-- carry evidence optionally and always will: Docs/01 §4.4 requires a photograph of one moment, not
-- of five.
--
-- # What it deliberately does not check
--
-- **Which kind of evidence.** 000604's ck_proofs_photograph_or_exception has already made a
-- `proofs` row exactly one of a photograph and a reasoned exception, so "a row exists" is the whole
-- of what is left to ask. Two constraints asking overlapping questions is two constraints to keep
-- in step.
--
-- **The rest of Docs/01 §4.4's field set.** A delivered job also requires a recipient name and a
-- delivery note, and no column holds either yet — SHIP-123 is the ticket whose *Done when* names
-- them, and it depends on SHIP-118. This migration enforces the half the invariant is about and
-- Docs/11 §3 records the half it does not.

CREATE OR REPLACE FUNCTION milestones_delivered_needs_evidence() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM proofs WHERE milestone_id = NEW.id) THEN
        RAISE EXCEPTION 'a delivered milestone needs photo proof or a recorded exception: % has neither', NEW.id
            USING HINT = 'Docs/01 §4.4 makes proof the only evidence that the job happened as '
                         'claimed, and gives three reasons a photograph can be impossible. Record '
                         'one of them in its place rather than recording nothing.';
    END IF;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER milestones_delivered_has_evidence
    AFTER INSERT ON milestones
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW
    WHEN (NEW.milestone = 'Delivered')
    EXECUTE FUNCTION milestones_delivered_needs_evidence();

COMMENT ON FUNCTION milestones_delivered_needs_evidence() IS
    'CLAUDE.md''s invariant, checked at COMMIT: a Delivered milestone has a proofs row — a photograph or a reasoned exception — behind it (SHIP-118). Deferred because the evidence points at the milestone and is therefore written second.';
