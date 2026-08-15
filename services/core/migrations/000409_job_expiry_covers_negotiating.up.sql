-- SHIP-70a: the expiry sweeps look at a live job, and since SHIP-90 a live job is not only an
-- Open one.
--
-- Docs/02 §2's expiry row now reads `Open / Negotiating → Cancelled`. 000406 and 000407 each built
-- a partial index on exactly the predicate its sweep claimed with, and both said so in as many
-- words — so widening the claim without widening the index would leave the statement correct and
-- the plan wrong: a sequential scan over every job the platform has ever had, every few minutes,
-- with nothing failing to say so.
--
-- # Why the deadline itself needs no migration
--
-- 000406's trigger fires on `NEW.status = 'Open' AND OLD.status IS DISTINCT FROM 'Open'`, and that
-- is still exactly right. A job reaches Negotiating *from* Open (Docs/02 §2 has no other way in),
-- so it is already carrying the deadline it was given at publication, and 000406's own comment
-- already anticipated the cycle: the trigger fills a NULL and never overwrites, which "stops the
-- clock restarting every time a job cycles Negotiating → Open as bids expire". Nothing about when
-- a deadline is set changes here. What changes is only which statuses the sweeps may read it in.
--
-- # Why both indexes are dropped and recreated rather than added to
--
-- A partial index's predicate is not alterable, and leaving the narrow one beside a wide one would
-- keep a second index on the same column that only ever serves a query nobody writes any more.
-- The names are kept — `idx_jobs_open_expiry` and `idx_jobs_open_unwarned` — because they are named
-- in 000406's and 000407's comments, in internal/jobs/expiry.go, and in the migration tests, and a
-- rename would be a change with no reader and four writers. "Open" in the name now means the live
-- marketplace rather than the literal status, which is the same reading Docs/02 §1 already takes of
-- Negotiating: "the job remains available for eligible bids unless the customer closes it or awards
-- a bid".

DROP INDEX IF EXISTS idx_jobs_open_expiry;
DROP INDEX IF EXISTS idx_jobs_open_unwarned;

-- The claim's index. Partial for the reason 000406 gives — the live marketplace is a small and
-- shrinking fraction of a table that keeps every job it has ever had — and now covering both of
-- the statuses a job can be offered in.
CREATE INDEX idx_jobs_open_expiry ON jobs (expires_at)
    WHERE status IN ('Open', 'Negotiating');

-- The warning claim's index, likewise, on exactly internal/jobs.ExpiryWarningClaim's predicate.
CREATE INDEX idx_jobs_open_unwarned ON jobs (expires_at)
    WHERE status IN ('Open', 'Negotiating') AND expiry_warned_at IS NULL;

COMMENT ON COLUMN jobs.expires_at IS
    'When a job stops being offered: the earlier of fourteen days after publication and its pickup window ending (Docs/02 §6.3, SHIP-68). Set by jobs_open_gets_a_deadline when it is NULL; extended by SHIP-70. Swept in Open and in Negotiating alike (Docs/02 §2, SHIP-70a).';
