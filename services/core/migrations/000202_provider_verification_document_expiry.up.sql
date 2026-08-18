-- SHIP-159: when a verification document lapses — Docs/04 §5's seventh queue.
--
-- `000201` left this column out deliberately and said in its own header what would put it back:
-- "**SHIP-159 is the ticket that builds the expiry queue**, and it will need somewhere to record
-- when a document lapses. That column is a one-line `ALTER TABLE … ADD COLUMN expires_at
-- timestamptz` against an append-only table, which is cheap; a renewal cadence invented here would
-- be a number the platform enforces that nobody decided, which is not." This is that line, and the
-- rest of this header is about the part that is not cheap.
--
-- Docs/04 §1 requires the verification record hold "every review, evidence item, decision, and
-- **expiry date**". The first three arrived with `000200` and `000201`; this is the fourth.
--
-- # Nullable, and it must stay nullable
--
-- Docs/04 §3's open question — "which documents are *legally* required rather than merely prudent,
-- and **how often each must be renewed**", owner: legal and insurance advisers, Track-X row X-4 — is
-- unanswered. NOT NULL would make every submission state an expiry, which is the platform requiring
-- an answer to a question nobody has settled and refusing an ABN extract that does not lapse at all.
-- NULL means "the platform was never told", which is the honest reading and is what the queue below
-- treats as "never surfaces".
--
-- **There is deliberately no default and no computed expiry.** A `submitted_at + interval '12
-- months'` would be exactly the invented cadence `000201` refused to write, moved from Go into SQL
-- where it is harder to see and harder to change.
--
-- # No CHECK that the expiry is in the future, and that is the interesting one
--
-- The obvious constraint is `expires_at > submitted_at`, and it would refuse the submission this
-- queue most wants to see: a provider photographing a certificate that has *already* lapsed. Docs/04
-- §3 has an administrator refusing an image that is "visibly expired", which means the platform has
-- to be able to hold one long enough for somebody to look at it. A document submitted after its own
-- expiry is a real event with a real answer, and the answer is a review rather than a 422.
--
-- # It can only ever be written at INSERT, and that is a consequence rather than a design
--
-- `provider_verification_documents_no_update` refuses every UPDATE from any connection. So an expiry
-- is stated when the document is submitted or it is never stated at all — a provider whose licence
-- renewal date was mistyped re-photographs the licence, which is a second row, which is what the
-- table's append-only rule already says a correction is. The alternative — a second mutable table
-- keyed by document — would put a correctable fact beside an uncorrectable one and make "what did
-- the reviewer see" ambiguous, which is the failure `000201` is built to prevent.
--
-- # The index is partial, because most rows will never have one
--
-- The queue reads documents that carry an expiry, soonest first. Most rows do not carry one — every
-- ABN extract, and every document submitted before X-4 is answered — so a full index would be mostly
-- NULLs that no query ever probes. `(expires_at, id)` rather than `(expires_at)` because the queue is
-- cursor paged and its ordering has to be total: two documents lapsing in the same millisecond would
-- otherwise make a page either skip a row or repeat one, and a skipped row here is a provider whose
-- insurance nobody chased.

ALTER TABLE provider_verification_documents
    ADD COLUMN expires_at timestamptz;

CREATE INDEX idx_provider_verification_documents_expiry
    ON provider_verification_documents (expires_at, id)
    WHERE expires_at IS NOT NULL;

COMMENT ON COLUMN provider_verification_documents.expires_at IS
    'When this document lapses, as stated at submission (SHIP-159). NULL means the platform was never told, which is not the same as "does not expire" and never surfaces on Docs/04 §5''s expiry queue. Writable only at INSERT — the table is append-only — and deliberately unconstrained: a document submitted after its own expiry is what an administrator is asked to refuse as "visibly expired", so the schema has to be able to hold one.';
