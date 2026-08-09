-- SHIP-7: the foundation every later migration builds on.
--
-- Deliberately contains no tables. The first migration's job is to establish the
-- extensions and helpers that subsequent schema depends on, so that SHIP-28 (users) and
-- SHIP-56 (jobs) can assume they exist rather than each carrying its own copy.

-- Case-insensitive text. Email addresses are compared case-insensitively by every mail
-- system in practice, so storing them as `text` means the uniqueness constraint in
-- SHIP-28 would happily accept both alice@example.com and Alice@example.com as separate
-- accounts. citext moves that guarantee into the column type rather than leaving it to
-- every query that touches an address.
CREATE EXTENSION IF NOT EXISTS citext;

-- Maintains updated_at on any table carrying that column.
--
-- Doing this in a trigger rather than in application code means the timestamp is correct
-- even for a row changed by a migration, a support query, or a future service — none of
-- which will remember to set it.
--
-- now() is transaction start time, so every row touched by one transaction shares a
-- timestamp. That is the intended behaviour: it makes "what changed in that request"
-- answerable. Where the distinction between actor time and server time genuinely matters
-- it is recorded explicitly instead, as the milestones table does (Docs/02 §3.1,
-- SHIP-110).
CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;

COMMENT ON FUNCTION set_updated_at() IS
    'Trigger function maintaining updated_at. Attach as: CREATE TRIGGER <table>_set_updated_at BEFORE UPDATE ON <table> FOR EACH ROW EXECUTE FUNCTION set_updated_at();';
