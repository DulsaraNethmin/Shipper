-- SHIP-116: the reasoned exception that stands in place of a photograph.
--
-- 000603 said this migration would exist and what it would look like — "exception_reason, and
-- object_key becoming nullable with a CHECK that exactly one of the two is present" — and this is
-- that, unchanged. It is one table rather than two, and the reason is the invariant CLAUDE.md
-- states: **Delivered requires photo proof or a recorded exception reason — never neither.** Two
-- tables would make "never both" unexpressible in SQL at all, because no constraint can span them;
-- one row with a CHECK makes it PostgreSQL's.
--
-- # An exception is evidence, not the absence of it
--
-- Docs/01 §4.4 decides that a photograph is mandatory and that "the exception path is part of the
-- same feature and must be built with it, not after". A driver whose recipient objects to being
-- photographed has still delivered the goods, and what the platform holds about that delivery is a
-- **recorded reason**, chosen from a closed list, written in the same transaction as the claim it
-- stands behind. That is why it lives in `proofs` beside the photographs rather than in a table of
-- its own: both answer one question — what is the evidence for this recorded milestone — and a
-- reader assembling a delivery's evidence reads one table in one order.
--
-- # The reason is a closed vocabulary and the list is Docs/01 §4.4's own three
--
-- "The recipient objects to being photographed", "the camera permission is denied or the hardware
-- is unavailable", and "the delivery point is unlit or unsafe to photograph". There is no `other`,
-- deliberately: a free-text reason nobody can group is a moderation queue nobody can triage
-- (Docs/04 §5), and the actor's own words already have a home in `milestones.reason`, which is
-- optional and 500 characters and sits one row away.
--
-- Paired with delivery.ProofExceptionReasons by TestProofExceptionConstraintMatchesTheGoConstants,
-- which is Docs/10 §3.4's pairing in both directions: a reason the database accepts and Go has no
-- constant for is a state no code handles, and one Go has and the database refuses is a reason a
-- driver can never select.
--
-- # What is deliberately not here
--
--   SHIP-117  the moderation flag an exception raises. 000603 already put it outside this table and
--             that is unchanged: whether a job is queued for review is a fact about the *job*, and
--             it belongs with the queue rather than with the evidence. What this migration owes
--             that ticket is a cheap answer to "which jobs completed through the exception path",
--             and idx_proofs_exception below is it.
--   X-6       whether a job completed through this path may auto-complete under Docs/02 §6.1.
--             **Nothing here presumes either answer.** The exception is a durable fact joined to
--             the milestone that carries it, so SHIP-119 can branch on it in whichever direction
--             operations decides, and no column would have to change either way.
--   SHIP-118  'Delivered' becoming recordable at all. This migration makes an exception storable;
--             it does not make a delivery acceptable without one, which is a rule about milestones
--             rather than about proof rows.

ALTER TABLE proofs
    -- The four facts a photograph has and an exception does not. They stop being individually
    -- mandatory and become mandatory *together*, which is what ck_proofs_photograph_or_exception
    -- below says: a row with a key and no entity tag is a photograph the platform half-recorded,
    -- and 000603's NOT NULLs were the only thing refusing it.
    ALTER COLUMN object_key DROP NOT NULL,
    ALTER COLUMN content_type DROP NOT NULL,
    ALTER COLUMN content_length DROP NOT NULL,
    ALTER COLUMN etag DROP NOT NULL,

    -- Why there is no photograph, from Docs/01 §4.4's list and nowhere else.
    --
    -- No actor and no second timestamp, for the reason 000603 gives about the photograph columns:
    -- who recorded this and when they say they acted are already on the milestone, in one row,
    -- written in the same transaction.
    ADD COLUMN exception_reason text;

ALTER TABLE proofs
    ADD CONSTRAINT ck_proofs_exception_reason CHECK (
        exception_reason IS NULL
        OR exception_reason IN ('recipient_objected', 'camera_unavailable', 'location_unsafe')
    ),

    -- **The invariant, in one line of SQL.**
    --
    -- A row is a photograph the store confirmed — all four facts — or a reasoned exception, and it
    -- is never both and never neither. `num_nonnulls` rather than four `IS NOT NULL` conjunctions
    -- because the failure this guards is a *partial* photograph, and counting says that directly.
    --
    -- It is not the whole of CLAUDE.md's invariant and does not pretend to be: this makes an
    -- evidence row coherent, and 000605 is what makes a delivered milestone require one.
    ADD CONSTRAINT ck_proofs_photograph_or_exception CHECK (
        (num_nonnulls(object_key, content_type, content_length, etag) = 4
             AND exception_reason IS NULL)
        OR
        (num_nonnulls(object_key, content_type, content_length, etag) = 0
             AND exception_reason IS NOT NULL)
    );

-- The jobs whose evidence includes a reasoned exception, which is SHIP-117's queue query.
--
-- Partial, because the rows it selects are the rare ones: almost every delivery is photographed,
-- and an index over the whole table would be mostly photographs nobody is asking about. The
-- moderation queue and the auto-complete decision (X-6) both start from "which jobs have one", and
-- this is what keeps that a lookup rather than a scan.
CREATE INDEX idx_proofs_exception ON proofs (job_id) WHERE exception_reason IS NOT NULL;

COMMENT ON COLUMN proofs.exception_reason IS
    'Why there is no photograph, from Docs/01 §4.4''s three (SHIP-116). Exactly one of this and object_key is present, which ck_proofs_photograph_or_exception enforces.';
COMMENT ON COLUMN proofs.object_key IS
    'The key in the private bucket, or NULL when this row is a reasoned exception. Unique among the rows that have one: an object is proof of at most one milestone, so a photograph cannot become evidence for a delivery it was not taken at.';
