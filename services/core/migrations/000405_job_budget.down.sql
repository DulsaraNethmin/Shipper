-- Reverse of 000405. Dropping the column takes ck_jobs_budget with it.

ALTER TABLE jobs
    DROP COLUMN IF EXISTS budget;
