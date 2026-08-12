-- SHIP-80: bids — the eight statuses of Docs/02 §4, and the index the award transaction is
-- built against. First table in block 500–599.
--
-- This table starts narrow, the way 000400 did, and for the same reason: every ticket from
-- SHIP-84 onward brings behaviour and the columns that behaviour needs, and guessing at those
-- now would be guessing at designs nobody has written. What SHIP-80 does have to get right is
-- the status list and the one-accepted-bid index, because those are the two things that are
-- expensive to add afterwards.
--
-- # The principle this table is built on: constraints now, columns later
--
-- Docs/09 states it in the note beside SHIP-91: "the database constraint comes *before* the
-- award endpoint deliberately — it is far easier to build correct behaviour against a
-- constraint that already exists than to add one afterwards and discover your data violates
-- it." Adding a nullable column later is a two-line migration. Adding a *constraint* later is a
-- migration plus whatever has to be done about the rows that already break it, and the rows
-- that break the one below are two providers who both believe they have the job.
--
-- So the constraints and the indexes are all here, and the columns are only the ones a bid
-- cannot be a bid without.
--
-- # What is deliberately not here, and who owns it
--
--   SHIP-84  the timing an offer commits to — when the provider can collect and when they will
--            deliver — together with any message that accompanies it. "Price and timing" is
--            that ticket's *Done when*, and the timing half is a shape (instants? windows
--            against the job's own?) rather than a column type.
--   SHIP-84  the vehicle or vehicles a bid is offered on. Docs/01 §4.2 gives the provider
--            "select one or more", which is a join table rather than a column if it is taken
--            literally — and that is SHIP-84's decision to take. 000300's header says selection
--            is a bid's business; it does not say it is one column.
--   SHIP-87  the supersede chain. A counter-offer supersedes the prior offer (Docs/02 §4) and
--            the whole chain stays readable, which needs a link between rows. Whether that is a
--            self-referencing column or a separate offer table is SHIP-87 and SHIP-88's design.
--   SHIP-89  bid expiry "on their own terms", which needs a column saying what those terms are.
--   SHIP-92  when the award happened, and by whom. The award transaction is a single-owner
--            branch (CLAUDE.md, Docs/11 §8) and it writes its own record.
--
-- **There is deliberately no "one active bid per provider per job" index here either**, and
-- that is the one omission most likely to look like a mistake. SHIP-84's *Done when* is "can
-- bid once per job", so the rule is real — but "active" is defined by the supersede design that
-- SHIP-87 and SHIP-88 own, and a partial index written against a guess at that definition is a
-- constraint the ticket would have to drop. It is additive whenever its definition is settled.

CREATE TABLE bids (
    id          uuid        PRIMARY KEY,

    -- The job being bid on. ON DELETE RESTRICT per Docs/10 §3.3: a job is cancelled rather than
    -- removed, and a cascade here would destroy the commercial record Docs/05 §3.1 requires
    -- retaining — including the losing bids, which are the evidence that the award was a choice.
    job_id      uuid        NOT NULL,

    -- The provider who made the offer. There is no CHECK that this account's role is
    -- 'provider', for the reason 000400 gives about jobs.customer_id: a foreign key cannot see
    -- another table's column. The domain enforces it where a bid is placed (SHIP-84).
    --
    -- Nor is there a CHECK that the provider is not the job's own customer, which is the same
    -- limitation seen from the other side — it is a comparison across two tables and belongs
    -- with SHIP-81's eligibility filter.
    provider_id uuid        NOT NULL,

    -- One of the eight in Docs/02 §4, stored exactly as that document writes it (Docs/10 §3.4).
    -- These eight happen to be single words in sentence case, unlike the twelve job statuses,
    -- but the rule is the same one and for the same reason: storing the document's own strings
    -- is what lets the Go, Dart and TypeScript copies be diffed against the document rather than
    -- against each other.
    --
    -- text with a CHECK rather than a PostgreSQL ENUM type, per Docs/10 §3.4: ALTER TYPE … ADD
    -- VALUE cannot run in a transaction block alongside its use, and removing or reordering a
    -- value means recreating the type and every column referencing it.
    --
    -- DEFAULT 'Draft' because that is where Docs/02 §4 lists the lifecycle starting, and because
    -- a provider composing an offer has somewhere to put it.
    --
    -- **Unlike jobs, there is no trigger refusing an insert at another status.** 000402 has one
    -- because Docs/02 §2 gives the job lifecycle exactly one entry point and a guarded function
    -- that every transition passes. Docs/02 §4 says no such thing about bids: a provider who
    -- fills the form in and sends it has legitimately created a row at 'Submitted', and a
    -- customer's counter-offer arrives as a new row that was never a draft. Inventing a guard
    -- here would be inventing a mechanism the documents do not describe.
    status      text        NOT NULL DEFAULT 'Draft',

    -- What the provider is asking, in AUD. numeric(12,2) per Docs/10 §3.3 — never a float, and
    -- no currency column in the MVP.
    --
    -- This is the provider's own number and it has nothing to do with the customer's budget.
    -- Docs/01 §4.3 forbids the budget reaching a provider in any form; nothing here carries,
    -- derives from, or hints at it, and SHIP-67 adds the budget to `jobs` with the serialisation
    -- test that proves it stays there.
    --
    -- Nullable, which mirrors the asymmetry 000404 takes on the job draft: a Draft is allowed to
    -- be incomplete, because refusing an incomplete row is refusing to save what somebody has
    -- typed so far. The constraint below is what makes it coherent — an offer anybody else can
    -- see names a price.
    amount      numeric(12, 2),

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ck_bids_status CHECK (status IN (
        'Draft',
        'Submitted',
        'Countered',
        'Accepted',
        'Rejected',
        'Withdrawn',
        'Expired',
        'Superseded'
    )),

    -- Coherence rather than policy, the same split 000300 and 000404 take. A free delivery and a
    -- negative price are not values an operator would ever want to permit; "the largest bid this
    -- marketplace carries" is a number operations changes (Docs/06 §5.3), so it lives in the
    -- validator rather than in a migration.
    CONSTRAINT ck_bids_amount CHECK (amount IS NULL OR amount > 0),

    -- A Draft may be incomplete. Anything past it is an offer somebody is expected to act on,
    -- and an offer with no price is not one — most sharply at 'Accepted', where the award would
    -- otherwise commit both parties to an unstated amount.
    CONSTRAINT ck_bids_offer_has_an_amount
        CHECK (status = 'Draft' OR amount IS NOT NULL),

    CONSTRAINT fk_bids_job
        FOREIGN KEY (job_id) REFERENCES jobs (id) ON DELETE RESTRICT,

    CONSTRAINT fk_bids_provider
        FOREIGN KEY (provider_id) REFERENCES users (id) ON DELETE RESTRICT
);

-- Exactly one accepted bid per job, and this index is the whole of that guarantee.
--
-- CLAUDE.md states the invariant as "enforced by a database constraint, not application logic
-- alone", and the "not alone" is the load-bearing half. A SELECT that finds no accepted bid
-- followed by an UPDATE that creates one is correct in a single-threaded reading and wrong under
-- two customers' requests, two retries of one request, or one request racing its own idempotency
-- replay. The window between the read and the write is small and it is not zero, and what
-- arrives in it is a job awarded twice — two providers who each believe they have the work, and
-- the first either of them learns otherwise is at a pickup address.
--
-- A **partial** unique index rather than a plain one, because the uniqueness is only wanted in
-- one state: a job has many bids and most of them are Rejected once it is awarded, so a unique
-- index on job_id alone would refuse the second bid rather than the second *acceptance*. This is
-- the same shape as 000300's uq_vehicles_provider_registration, where the predicate is
-- `deactivated_at IS NULL`.
--
-- It also does the locking. Two transactions inserting the same key into a btree do not race:
-- the second blocks on the first's uncommitted entry and then either succeeds (the first rolled
-- back) or raises unique_violation (the first committed). SHIP-92 gets that serialisation for
-- free and does not have to invent it, which is why Docs/09 puts SHIP-91 before the endpoint.
--
-- Withdrawing the award is not blocked by it: moving the accepted bid to any other status leaves
-- the predicate, and the job can then be awarded again — which is exactly what Docs/02 §6.2's
-- provider cancellation needs.
CREATE UNIQUE INDEX uq_bids_one_accepted_per_job
    ON bids (job_id)
    WHERE status = 'Accepted';

-- Every foreign key is indexed (Docs/10 §3.3). The partial index above does not count for
-- job_id: it covers a single status, so it cannot answer the constraint check that runs when a
-- job row is deleted, and it cannot serve the customer's list of bids on a job either.
--
-- Leading with job_id and ordered newest first, because that is the read SHIP-93 makes when it
-- closes every competing bid and the read SHIP-102 makes when the customer compares them.
CREATE INDEX idx_bids_job ON bids (job_id, created_at DESC);

-- The other foreign key, and the provider's own list of bids (SHIP-101), which is likewise
-- newest first. One index serves both.
CREATE INDEX idx_bids_provider ON bids (provider_id, created_at DESC);

CREATE TRIGGER bids_set_updated_at
    BEFORE UPDATE ON bids
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON TABLE bids IS
    'One offer against one job. At most one row per job is Accepted, enforced by uq_bids_one_accepted_per_job rather than by application logic (SHIP-80, SHIP-91).';
COMMENT ON COLUMN bids.status IS
    'One of the eight statuses in Docs/02 §4, stored exactly as that document writes them.';
COMMENT ON COLUMN bids.amount IS
    'What the provider is asking, in AUD. The provider''s own number: the customer''s budget is never exposed to a provider in any form (Docs/01 §4.3).';
