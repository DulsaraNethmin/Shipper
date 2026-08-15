-- Reverses SHIP-70a's index widening, returning both partial indexes to the predicates 000406 and
-- 000407 created them with.
--
-- IF EXISTS throughout, because a down migration runs against whatever state a failed up left
-- behind rather than against the state the up intended.

DROP INDEX IF EXISTS idx_jobs_open_expiry;
DROP INDEX IF EXISTS idx_jobs_open_unwarned;

CREATE INDEX IF NOT EXISTS idx_jobs_open_expiry ON jobs (expires_at) WHERE status = 'Open';

CREATE INDEX IF NOT EXISTS idx_jobs_open_unwarned ON jobs (expires_at)
    WHERE status = 'Open' AND expiry_warned_at IS NULL;

COMMENT ON COLUMN jobs.expires_at IS
    'When an Open job stops being offered: the earlier of fourteen days after publication and its pickup window ending (Docs/02 §6.3, SHIP-68). Set by jobs_open_gets_a_deadline when it is NULL; extended by SHIP-70.';
