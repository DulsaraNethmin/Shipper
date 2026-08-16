-- Reverses SHIP-169.
--
-- IF EXISTS throughout, per Docs/10 §3.5: `migrate down n=all` runs against whatever state the
-- database is actually in, which is not always the one the up migration left. Dropping the table
-- takes its indexes, its constraints and its trigger with it.
DROP TABLE IF EXISTS account_deletion_requests;
