-- IF EXISTS throughout, so a partially applied migration comes back down without leaving the
-- schema_migrations row dirty.

DROP INDEX IF EXISTS idx_jobs_delivered;
