-- Reverse of 000005: the rule goes back to being a comment.
--
-- The column comment goes too. 000002_users left users.role with none, so restoring that state
-- means clearing it: CI runs up -> down all -> up against an empty database, and a comment that
-- survived a down would make the second up a different schema from the first.

DROP TRIGGER IF EXISTS users_role_is_immutable ON users;
DROP FUNCTION IF EXISTS users_role_is_immutable();

COMMENT ON COLUMN users.role IS NULL;
