-- Reverse of 000411.

ALTER TABLE jobs
    DROP COLUMN IF EXISTS terms_accepted_at;
