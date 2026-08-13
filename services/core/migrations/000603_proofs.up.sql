-- SHIP-115: the record that turns an object in a bucket into proof of a delivery.
--
-- SHIP-114 issues a pre-signed URL and the client PUTs a photograph straight to the object store,
-- with this service in neither direction (Docs/06 §5.2). Nothing was written: an object in a
-- bucket is bytes with a key, and the platform holds no opinion about which delivery it belongs
-- to or who may look at it. **This table is that opinion**, and internal/platform/storage/doc.go
-- says explicitly that it lives here — "nothing there is authoritative about which job a file
-- belongs to or who may see it".
--
-- # A row is attached to a milestone, and through it to a job
--
-- Docs/01 §4.4 numbers five things an actor records and requires a photograph of one of them. So
-- proof is evidence *for a recorded claim* rather than a property of the job: a photograph is of
-- a moment, and the moment is the milestone. The consequence that matters is what happens to a
-- late milestone — SHIP-112 keeps the row and moves nothing, so proof attached to it is kept too,
-- and it appears on the customer's timeline at the time the driver acted rather than at the time
-- the phone found signal. A record hung off the *job* would have had to answer "which proof is
-- current" with the arrival order, which is the one order Docs/02 §3.1 says not to show.
--
-- job_id is carried as well, and it is not a denormalisation somebody can let drift: the
-- composite foreign key below makes "this proof's job is its milestone's job" a fact PostgreSQL
-- checks, not one an INSERT statement remembers.
--
-- # What is stored is what the *store* reported, not what the client said
--
-- content_type, content_length and etag are read back from the object with a metadata request
-- after the upload (delivery.ProofObjects). That is the whole reason a row is not written when
-- the URL is issued: the platform never sees the bytes, so the only moment it can know an object
-- exists is when it asks. Writing the client's claim instead would let a proof row point at
-- nothing, and Docs/01 §4.4 makes proof "the *only* evidence that the job happened as claimed".
--
-- # What is deliberately not here
--
--   SHIP-116  exception_reason, and object_key becoming nullable with a CHECK that exactly one of
--             the two is present. "A reasoned exception can be recorded in place of a photo" is
--             that ticket's Done when; a nullable column added here would be a state nothing
--             writes and nothing reads.
--   SHIP-117  the moderation flag an exception raises, which is a fact about the job and belongs
--             with the queue rather than with the evidence.
--   SHIP-155  the administrator's access log. Docs/04 §6 requires the evidence reference to be
--             recorded with the decision, and there is no administrator to record: ck_users_role
--             refuses 'admin' and admin sign-in is SHIP-147.
--
-- No actor columns, and that is the same narrowness. The milestone this proof belongs to already
-- records actor_type and actor_id, in one row, written in the same transaction — a second copy
-- here could disagree with it, and there is no question it could answer that a join cannot.

-- milestones (id, job_id) has to be declarable as a foreign key target before anything can point
-- at the pair. `id` is already the primary key so the pair is unique either way; PostgreSQL still
-- requires a named unique constraint to reference, which is what this adds and all it adds.
ALTER TABLE milestones ADD CONSTRAINT uq_milestones_id_job UNIQUE (id, job_id);

CREATE TABLE proofs (
    id uuid PRIMARY KEY,

    -- The delivery. Carried rather than joined for, because every read of this table is "the
    -- proof on this job" and the composite key below makes the copy safe.
    --
    -- ON DELETE RESTRICT per Docs/10 §3.3, which with the append-only triggers means a job that
    -- has been photographed cannot be deleted at all — the reading 000601 and 000401 both take of
    -- Docs/05 §3.1.
    job_id uuid NOT NULL,

    -- The recorded claim this photograph is evidence for.
    milestone_id uuid NOT NULL,

    -- Where the object is. The platform chose it (delivery.proofObjectKey), so it is always
    -- `proof/<job>/<uuidv7>` — but no CHECK asserts that shape, deliberately: the prefix is a
    -- convenience for reading a log, the database is what is authoritative about which job an
    -- object belongs to, and a constraint parsing a key would make the two say it twice.
    --
    -- UNIQUE, which is an access-control rule rather than tidiness. Without it the same object
    -- could be recorded as proof of two different milestones — on two different jobs, belonging
    -- to two different customers — and one photograph would then be evidence for a delivery it
    -- was never taken at.
    object_key text NOT NULL,

    -- What the object store reported when the platform asked, after the upload.
    --
    -- Not what the client said it would upload. The two are the same whenever the signed URL did
    -- its job, and this is the copy that is true when it did not.
    content_type text NOT NULL,
    content_length bigint NOT NULL,

    -- The store's entity tag for the bytes at the moment they became proof.
    --
    -- A pre-signed PUT stays usable until it expires, so the holder of a URL can overwrite the
    -- object it names within that window. Nothing can revoke the URL (SHIP-114), so the answer is
    -- not prevention but detection: a reader that finds a different tag is looking at bytes that
    -- are not the ones recorded. Nothing consumes it yet — SHIP-155's viewer is where an
    -- administrator would be shown a mismatch — and it is stored now because it is unrecoverable
    -- later.
    etag text NOT NULL,

    -- When the platform recorded it. There is no actor clock here and there should not be: the
    -- actor's claim about when the delivery happened is on the milestone, in
    -- milestones.actor_recorded_at, and a second copy would be a second answer to Docs/02 §3.1's
    -- one question.
    created_at timestamptz NOT NULL DEFAULT now(),

    -- No updated_at and no set_updated_at() trigger, because there is no update. Docs/10 §3.3
    -- requires the trigger of "every mutable table", and the two triggers below are what make
    -- this one immutable.

    CONSTRAINT ck_proofs_content_type CHECK (
        length(content_type) BETWEEN 1 AND 128
    ),

    -- An object of no length is not a photograph, and the platform refuses to sign for one
    -- (storage.PresignUpload). Bounded below only: the upper bound is STORAGE_MAX_UPLOAD_BYTES,
    -- which is configuration precisely because it moves, and a CHECK holding a copy of it would
    -- refuse rows the running platform had just accepted.
    CONSTRAINT ck_proofs_content_length CHECK (content_length > 0),

    CONSTRAINT ck_proofs_object_key CHECK (
        length(object_key) BETWEEN 1 AND 1024
    ),

    CONSTRAINT ck_proofs_etag CHECK (length(etag) BETWEEN 1 AND 255),

    -- **The link, and it is one key rather than two.**
    --
    -- Two separate foreign keys — one to milestones (id), one to jobs (id) — would each hold, and
    -- together they would still permit a proof whose job_id names one job and whose milestone
    -- belongs to another. That row reads perfectly: it would put a photograph of somebody else's
    -- delivery on this job's timeline, and it is exactly the shape a mistyped identifier in a
    -- future writer produces. A composite key makes the agreement the database's.
    CONSTRAINT fk_proofs_milestone
        FOREIGN KEY (milestone_id, job_id) REFERENCES milestones (id, job_id) ON DELETE RESTRICT,

    -- job_id is a foreign key in its own right as well. The composite above already reaches jobs
    -- transitively through milestones, and this is what indexes and enforces the column a reader
    -- actually filters on.
    CONSTRAINT fk_proofs_job
        FOREIGN KEY (job_id) REFERENCES jobs (id) ON DELETE RESTRICT
);

-- One photograph per recorded milestone.
--
-- A driver who photographs a second time has recorded a second milestone — 000601 has no
-- uniqueness on (job_id, milestone) precisely so they can, and Docs/02 §5 calls the failed pickup
-- attempt an ordinary outcome. What this refuses is two proofs of *one* claim, which is a claim
-- with two answers.
--
-- It is also what makes proof idempotent without a key column of its own: the milestone is
-- written under uq_milestones_idempotency and the proof is written in the same transaction, so a
-- retry that reaches the table finds the milestone already recorded and never inserts here.
CREATE UNIQUE INDEX uq_proofs_milestone ON proofs (milestone_id);

-- One object is proof of at most one thing. See object_key above for why this is access control.
CREATE UNIQUE INDEX uq_proofs_object_key ON proofs (object_key);

-- The proof on one job, in the order a timeline reads.
--
-- Ordered by the milestone's actor clock is what a reader wants, and that lives in the other
-- table; this indexes the foreign key Docs/10 §3.3 requires indexed and gives the join its job.
CREATE INDEX idx_proofs_job ON proofs (job_id, created_at DESC);

-- Append-only, the control 000003 puts on audit_log, 000401 on job_status_history and 000601 on
-- milestones.
--
-- Proof is evidence and Docs/04 §7 has an administrator reviewing it in a dispute. A row that can
-- be updated is a photograph that can be swapped for a different one after somebody complained,
-- with nothing in the record to say it was.
CREATE OR REPLACE FUNCTION proofs_is_append_only() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'proofs is append-only: % is not permitted', TG_OP
        USING HINT = 'Proof that turned out to be wrong is another milestone and another '
                     'photograph. Replacing one in place rewrites the evidence.';
END;
$$;

CREATE TRIGGER proofs_no_update
    BEFORE UPDATE ON proofs
    FOR EACH ROW EXECUTE FUNCTION proofs_is_append_only();

CREATE TRIGGER proofs_no_delete
    BEFORE DELETE ON proofs
    FOR EACH ROW EXECUTE FUNCTION proofs_is_append_only();

COMMENT ON TABLE proofs IS
    'Which uploaded object is proof of which recorded milestone, and therefore of which job (SHIP-115). Append-only. The object store holds the bytes and knows nothing about either.';
COMMENT ON COLUMN proofs.object_key IS
    'The key in the private bucket. Unique: one object is proof of at most one milestone, so a photograph cannot become evidence for a delivery it was not taken at.';
COMMENT ON COLUMN proofs.content_type IS
    'What the object store reported after the upload, not what the client said it would send.';
COMMENT ON COLUMN proofs.etag IS
    'The store''s entity tag for the bytes at the moment they became proof. A pre-signed PUT stays usable until it expires, so this is how a later overwrite is detectable.';
