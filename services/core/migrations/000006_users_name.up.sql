-- SHIP-30a: a user's name, in a column of its own.
--
-- `000002_users` never had one, registration has never asked for one, and the only `name`
-- columns anywhere in the schema are `admin_users.name` — an administrator's — and
-- `driver_assignments.driver_name`, which is captured at assignment and belongs to a job.
-- Neither is the account holder's. SHIP-151 shipped a search whose *Done when* names four
-- terms and could only serve three, and `Docs/11` §4 has carried that gap since.
--
-- # A new migration rather than an edit to `000002`
--
-- `000005_users_role_is_immutable` is the precedent. Rewriting an applied migration breaks
-- every database that has already run it: the version table says `000002` is applied, so the
-- new text is never executed and the schema silently disagrees with the file.
--
-- # Why the column is nullable, which is the decision worth reading
--
-- `NOT NULL` is what "registration requires a name" would look like in the schema, and it is
-- not available here. Every account that already exists predates the column, and **a name
-- cannot be backfilled** — `Docs/09`'s own row for this ticket says so, and it is the reason
-- the ticket sits in M1 rather than in M6. A `DEFAULT ''` or a backfill to 'Unknown' would put
-- a value nobody supplied into the one field whose entire purpose is that a person supplied it,
-- and it would make the column unable to distinguish "did not say" from "said nothing".
--
-- So the requirement lives where the value is collected — `identity.RegisterCommand.Validate`
-- refuses a registration without one — and the column records that older accounts have none.
-- **The trigger for tightening it is nameable rather than left to judgement:** once every row
-- has a name, `ALTER TABLE users ALTER COLUMN name SET NOT NULL` is one migration in this same
-- block. Before pilot (M7) is the moment; `select count(*) from users where name is null` is
-- the figure to point at.
--
-- # What the CHECK does and deliberately does not do
--
-- It refuses a name that is present and blank, because `NULL` and `''` would otherwise be two
-- spellings of "no name" and every reader would have to know both. It does **not** bound the
-- length: `000404`'s comment settles that question for this schema — a length limit is a
-- validation limit, `Docs/06` §5.3 wants those changeable without a deploy, and a CHECK
-- constraint is a migration. `identity` bounds it.
--
-- `btrim(name, E' \t\r\n')` rather than the one-argument form. **PostgreSQL's one-argument
-- `btrim` strips spaces only** — not tabs, not newlines — which `ck_admin_notes_body` (SHIP-162)
-- got wrong and a test caught: a name of a single newline satisfied the constraint while
-- `strings.TrimSpace` in the service refused the same value, and the two disagreed about what
-- an empty name is. The character set is spelled out here for that reason.

ALTER TABLE users
    ADD COLUMN name text;

ALTER TABLE users
    ADD CONSTRAINT ck_users_name
        CHECK (name IS NULL OR btrim(name, E' \t\r\n') <> '');

COMMENT ON COLUMN users.name IS
    'The account holder''s name, collected at registration (SHIP-30a). Nullable because accounts created before this migration have none and a name cannot be backfilled; registration refuses to create one without it.';
