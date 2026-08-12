-- Reverse of 000406. The trigger goes before the function it calls, and the index goes with the
-- column it is built on.

DROP TRIGGER IF EXISTS jobs_open_gets_a_deadline ON jobs;
DROP FUNCTION IF EXISTS jobs_open_gets_a_deadline();

DROP INDEX IF EXISTS idx_jobs_open_expiry;

ALTER TABLE jobs
    DROP COLUMN IF EXISTS expires_at;
