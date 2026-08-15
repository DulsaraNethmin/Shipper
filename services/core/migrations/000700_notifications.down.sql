-- IF EXISTS throughout, so a migration that failed part way comes back down cleanly rather than
-- leaving schema_migrations dirty. Dropping the table takes its indexes and its trigger with it;
-- the trigger function itself is 000001's and stays.

DROP TABLE IF EXISTS notifications;
