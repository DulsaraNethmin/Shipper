-- Reverse of 000410. Dropping the column takes ck_jobs_goods_category with it.

ALTER TABLE jobs
    DROP COLUMN IF EXISTS goods_category;
