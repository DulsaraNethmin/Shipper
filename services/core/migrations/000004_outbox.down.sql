-- Reverses SHIP-134's table.
--
-- Rolling this back discards any events that have not yet been published, which is data loss
-- rather than a clean reversal. It is acceptable only against a database where the publisher
-- has drained the backlog, or one with no real data at all.
DROP TABLE IF EXISTS outbox;
