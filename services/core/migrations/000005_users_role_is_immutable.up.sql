-- SHIP-45: the role chosen at registration is the role the account keeps.
--
-- 000002_users states the rule in a column comment and enforces nothing. This makes it a
-- database guarantee, for the same reason audit_log's append-only rule is one (000003) and the
-- one-accepted-bid index will be (SHIP-91): application logic refusing to write is a convention,
-- and a convention does not apply to a support query typed at a psql prompt, to a migration
-- written in a hurry, or to the next endpoint somebody adds without reading this file.
--
-- Docs/06 §4.1 is the argument in general form — a mock happily accepts the write the real
-- constraint exists to reject — and here the "mock" is any code path that does not go through
-- the one function that knows the rule.
--
-- # Why a trigger rather than a CHECK
--
-- A CHECK constraint sees only the new row. Immutability is a statement about the *transition*,
-- which needs both OLD and NEW, and BEFORE UPDATE is the only place both exist.
--
-- # Why there is no escape hatch
--
-- There is no session variable that lets a privileged path through, which is a deliberate
-- difference from the mechanism Docs/11 §9 proposes for job status. Job status changes
-- constantly and needs exactly one guarded path; a role changes never. A provider's bid history
-- and a customer's job history are not interchangeable, so changing role would silently rewrite
-- the meaning of every job and bid already attached to the account. The answer is a new account,
-- and support asking for one is the correct outcome rather than an obstacle.

CREATE OR REPLACE FUNCTION users_role_is_immutable() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'users.role is immutable: % cannot become %', OLD.role, NEW.role
        USING HINT = 'Role is fixed at registration (SHIP-45). Changing it means a new account, '
                     'because a provider''s bid history and a customer''s job history are not '
                     'interchangeable.';
END;
$$;

-- WHEN keeps the function off every other UPDATE. Without it, marking an email verified or
-- touching last_seen_at would call a function whose only job is to raise, and PostgreSQL would
-- evaluate the row-level trigger on every write to the busiest table in the service.
CREATE TRIGGER users_role_is_immutable
    BEFORE UPDATE ON users
    FOR EACH ROW
    WHEN (NEW.role IS DISTINCT FROM OLD.role)
    EXECUTE FUNCTION users_role_is_immutable();

COMMENT ON COLUMN users.role IS
    'Fixed at registration and immutable thereafter (SHIP-45), enforced by the users_role_is_immutable trigger rather than by application logic alone.';
