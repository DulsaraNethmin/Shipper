-- SHIP-81b: the evidence behind a verification record — Docs/04 §3's four documents.
--
-- `000200` gave every provider a verification standing and a trail of the decisions taken about it.
-- Docs/04 §1 requires the trail hold "every review, **evidence item**, decision, and expiry date",
-- and until this migration there was no evidence item anywhere in the schema: an administrator
-- reviewing a provider (SHIP-153, SHIP-154) had a queue entry, a state and a reason, and nothing to
-- look at. This is the table the reviewing is *of*.
--
-- # The platform never sees the bytes, so a row here is written after the fact
--
-- Docs/06 §5.2 and Docs/04 §3.1 both put the image outside this service entirely: the provider
-- photographs a document, the client is handed a short-lived pre-signed URL, and the bytes travel
-- from the handset to the object store with the API in neither direction. `000603` is the precedent
-- and its reasoning transfers without change — the platform cannot tell an upload that succeeded
-- from one that failed halfway from one that was never attempted, so the row is written only after
-- the store has been asked what it holds, and `content_type`, `content_length` and `etag` are the
-- store's answer rather than the client's claim.
--
-- The consequence worth stating is the one `000603` states: **a row always has an object behind it,
-- and an object may have no row.** The first is refused because a record asserting that a licence
-- was submitted, held by a platform that never looked, is not evidence. The second is expected — a
-- URL issued and never spent leaves bytes nothing references, unfindable (the bucket has no public
-- read path and the keys are unguessable) and ageing out under a lifecycle rule.
--
-- # A retake is a new row, not an edit
--
-- Each of the four kinds is independently capturable and re-uploadable: a provider whose insurance
-- certificate was rejected as illegible photographs it again. That could have been an UPDATE against
-- a unique `(provider_id, kind)`, and it is the wrong shape for the same reason
-- `provider_verification_decisions` is append-only — the replaced image is the thing an
-- administrator already looked at and recorded a decision against, and overwriting it removes the
-- evidence that the first submission was ever made. So this table is append-only, the **newest row
-- per kind is the current document**, and the older ones stay as what was reviewed at the time.
--
-- That is also why there is no `(provider_id, kind)` unique index. The uniqueness that *is* here is
-- on `object_key`, and it is access control rather than tidiness — see the column.
--
-- # There is deliberately no expiry column, and adding one is not this ticket's to guess
--
-- Docs/04 §3 states the open question in as many words: "which documents are *legally* required
-- rather than merely prudent, and how often each must be renewed. Owner: legal and insurance
-- advisers… This determines expiry tracking (§5) and retention obligations, and remains genuinely
-- outside engineering's competence to settle." It is Track-X row X-4 and it is not answered.
--
-- **SHIP-159 is the ticket that builds the expiry queue**, and it will need somewhere to record when
-- a document lapses. That column is a one-line `ALTER TABLE … ADD COLUMN expires_at timestamptz`
-- against an append-only table, which is cheap; a renewal cadence invented here would be a number
-- the platform enforces that nobody decided, which is not. The note is the whole of what this
-- migration has to say about it.

CREATE TABLE provider_verification_documents (
    id uuid PRIMARY KEY,

    -- The verification record this document is evidence for.
    --
    -- The foreign key points at `provider_verifications` rather than at `users`, and that is the
    -- *Done when* read literally: "each stored document names its kind and **the verification
    -- record it belongs to**". `provider_verification_decisions` points the same way for the same
    -- reason — the two tables are the two halves of one provider's file, and a document that could
    -- exist for an account with no verification record would be evidence attached to nothing.
    --
    -- ON DELETE RESTRICT per Docs/10 §3.3, and with the append-only triggers below it means a
    -- provider whose documents have been reviewed cannot be deleted at all. SHIP-171 pseudonymises
    -- rather than removes, which is the reading Docs/04 §1 requires of an evidence trail.
    provider_id uuid NOT NULL,

    -- Which of Docs/04 §3's four documents this is.
    --
    -- A closed vocabulary rather than free text, and held to `profiles.Kinds` in both directions by
    -- TestTheFourKindsAreTheConstraintsFourKinds (Docs/10 §3.4) — the same arrangement
    -- `ck_provider_verifications_state` and `profiles.States` are under. A fifth kind is a decision
    -- somebody records in a migration, not a string a client sends.
    --
    -- 'abn_evidence' rather than 'abn', because Docs/04 §3's row is "ABN where applicable —
    -- required for businesses" and what is photographed is evidence *of* an ABN — a registration
    -- extract, a tax invoice showing it — rather than the number itself. A number is a field
    -- somebody types; this table is for things somebody looks at.
    kind text NOT NULL,

    -- Where the object is. The platform chose it (profiles.documentObjectKey), so it is always
    -- `verification/<provider>/<uuidv7>`.
    --
    -- No CHECK asserts that shape, deliberately, which is `000603`'s decision and its reasoning:
    -- the prefix is a convenience for reading a log, `provider_id` beside it is what is
    -- authoritative about whose document this is, and a constraint parsing a key would make the two
    -- say it twice and eventually disagree. **The check that the key was minted for the caller is
    -- profiles.keyBelongsToProvider's**, in the domain, before anything is written.
    --
    -- UNIQUE, and that is access control rather than tidiness. Without it one object could be
    -- recorded as evidence on two providers' records, and a single photograph of a licence would
    -- verify two people.
    object_key text NOT NULL,

    -- What the object store reported when the platform asked, after the upload — not what the
    -- client said it would send. The two agree whenever the signed URL did its job, and these are
    -- the values that are true when it did not.
    content_type text NOT NULL,
    content_length bigint NOT NULL,

    -- The store's entity tag for the bytes at the moment they became evidence.
    --
    -- A pre-signed PUT stays usable until it expires and nothing can revoke it, so the holder of a
    -- URL can overwrite the object it names inside that window. The answer is detection rather than
    -- prevention: a reader that finds a different tag is looking at bytes that are not the ones
    -- recorded. SHIP-155's viewer is where an administrator would be shown a mismatch; it is stored
    -- now because it is unrecoverable later.
    etag text NOT NULL,

    -- When the platform recorded the document. There is no actor clock and no actor columns: the
    -- provider is `provider_id`, and a document is submitted by the person whose record it is or it
    -- is not submitted at all — `provider_verification_decisions` has an actor because a decision
    -- can be taken by an administrator or by the system, and a submission cannot.
    submitted_at timestamptz NOT NULL DEFAULT now(),

    -- No updated_at and no set_updated_at() trigger. Docs/10 §3.3 requires the trigger of every
    -- *mutable* table, and the two triggers below are what make this one immutable.

    CONSTRAINT ck_provider_verification_documents_kind CHECK (
        kind IN ('licence', 'registration', 'insurance', 'abn_evidence')
    ),

    CONSTRAINT ck_provider_verification_documents_object_key CHECK (
        length(object_key) BETWEEN 1 AND 1024
    ),

    CONSTRAINT ck_provider_verification_documents_content_type CHECK (
        length(content_type) BETWEEN 1 AND 128
    ),

    -- An object of no length is not a photograph of anything, and the platform refuses to sign for
    -- one (storage.PresignUpload). Bounded below only: the upper bound is
    -- STORAGE_MAX_UPLOAD_BYTES, which is configuration precisely because it moves, and a CHECK
    -- holding a copy of it would refuse rows the running platform had just accepted.
    CONSTRAINT ck_provider_verification_documents_content_length CHECK (content_length > 0),

    CONSTRAINT ck_provider_verification_documents_etag CHECK (
        length(etag) BETWEEN 1 AND 255
    ),

    CONSTRAINT fk_provider_verification_documents_verification
        FOREIGN KEY (provider_id) REFERENCES provider_verifications (provider_id) ON DELETE RESTRICT
);

-- One object is evidence for at most one provider. See object_key above for why this is access
-- control.
CREATE UNIQUE INDEX uq_provider_verification_documents_object_key
    ON provider_verification_documents (object_key);

-- One provider's file, newest first within each kind.
--
-- This is the read both callers make: the provider's own list, and SHIP-155's administrator opening
-- a submission. "The current licence" is the first row of its kind, which is what the leading two
-- columns and the descending clock give in one index scan.
CREATE INDEX idx_provider_verification_documents_provider
    ON provider_verification_documents (provider_id, kind, submitted_at DESC);

-- Append-only, the control `000003` puts on audit_log, `000200` on provider_verification_decisions
-- and `000603` on proofs.
--
-- Docs/04 §6.6 requires "the decision, actor, timestamp, reason, and evidence reference" of every
-- moderation outcome. A row that can be updated is an evidence reference that can be pointed at a
-- different image after a decision was taken against the first one, with nothing in the record to
-- say so.
CREATE OR REPLACE FUNCTION provider_verification_documents_is_append_only() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'provider_verification_documents is append-only: % is not permitted', TG_OP
        USING HINT = 'A document that was wrong, illegible or out of date is another document. '
                     'Replacing one in place removes the evidence that the first was submitted '
                     '(Docs/04 §1, §9).';
END;
$$;

CREATE TRIGGER provider_verification_documents_no_update
    BEFORE UPDATE ON provider_verification_documents
    FOR EACH ROW EXECUTE FUNCTION provider_verification_documents_is_append_only();

CREATE TRIGGER provider_verification_documents_no_delete
    BEFORE DELETE ON provider_verification_documents
    FOR EACH ROW EXECUTE FUNCTION provider_verification_documents_is_append_only();

COMMENT ON TABLE provider_verification_documents IS
    'Docs/04 §3''s four verification documents as submitted by a provider, one row per upload (SHIP-81b). Append-only; the newest row of a kind is the current document. The object store holds the bytes and knows nothing about whose they are.';
COMMENT ON COLUMN provider_verification_documents.kind IS
    'licence, registration, insurance or abn_evidence (Docs/04 §3). Held to profiles.Kinds in both directions by test.';
COMMENT ON COLUMN provider_verification_documents.object_key IS
    'The key in the private bucket, always verification/<provider>/<uuidv7>. Unique: one object is evidence for at most one provider, so a single photograph cannot verify two people.';
COMMENT ON COLUMN provider_verification_documents.etag IS
    'The store''s entity tag for the bytes at the moment they became evidence. A pre-signed PUT stays usable until it expires, so this is how a later overwrite is detectable.';
