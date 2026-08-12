-- Reverses 000407. The index goes with the column, and the trigger before the function it names.

DROP TRIGGER IF EXISTS jobs_moving_the_deadline_rearms_the_warning ON jobs;
DROP FUNCTION IF EXISTS jobs_moving_the_deadline_rearms_the_warning();
DROP INDEX IF EXISTS idx_jobs_open_unwarned;

ALTER TABLE jobs
    DROP COLUMN IF EXISTS expiry_warned_at;
