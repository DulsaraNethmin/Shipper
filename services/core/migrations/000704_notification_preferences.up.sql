-- SHIP-142: which categories an account has switched off.
--
-- The *Done when* is two clauses and they pull in opposite directions: "a user can mute
-- non-essential categories; **essential events cannot be muted**". The first is a feature and the
-- second is a guarantee, and a guarantee held only in Go is one an `INSERT` from psql walks past.
--
-- # Presence is the mute, and there is no `muted boolean`
--
-- A row here means "this account has switched this category off". There is deliberately no column
-- saying so, because there is no third state: a category is on unless somebody turned it off, and
-- "explicitly on" and "never touched" are the same fact about what the platform should send.
--
-- That also means **no backfill**. Every account that existed before this migration has no rows and
-- is therefore receiving everything, which is exactly what it was doing yesterday. A `muted boolean
-- NOT NULL DEFAULT false` table with one row per account per category would have needed four rows
-- written for every user in the database, and a trigger or an application rule to keep new accounts
-- in step — a second mechanism whose only job is to reproduce a default.
--
-- `muted_at` is kept because support is asked "when did they turn this off", and because a
-- preference row with no timestamp is the one record in this schema that could not be dated.
--
-- # The CHECK is the second clause, and it is the reason this table is not one column wider
--
-- ck_notification_preferences_category admits **only the categories that may be muted**, which today
-- is `job_expiry` alone. So "essential events cannot be muted" is not a branch in a handler that a
-- later refactor can drop: the row cannot exist. The handler refuses it too, because a client is
-- owed a field error rather than a constraint name in a 500 — but the handler is the courtesy and
-- this is the control, which is the same division 000701 draws for a device token.
--
-- The list is short and it is a **copy** of internal/notifications.Category.Essential(), which
-- Docs/10 §3.4 requires to be paired in both directions by a test. migrations/notifications_test.go
-- holds it: a category Go calls mutable and this CHECK refuses is a preference nothing can save, and
-- a category this CHECK admits and Go calls essential is a mute the consumer would honour on an
-- event Docs/01 §4.5 says must always be sent.
--
-- **Making a second category mutable is therefore a migration**, deliberately. Docs/01 §4.5's list
-- of essential events is a product decision, and the shape that lets somebody widen it by editing a
-- Go constant is the shape where it gets widened without anybody noticing.
--
-- # Why the category is text with a CHECK rather than an enum type
--
-- Docs/10 §3.4, and 000700 took the same position for `notifications.category`: an enum type cannot
-- have a value removed and its `ALTER TYPE` is not transactional in the way a CHECK is.

CREATE TABLE notification_preferences (
    -- The account. ON DELETE RESTRICT for Docs/10 §3.3's reason and 000700's: SHIP-171
    -- pseudonymises rather than deletes, and a preference is a statement by a person that the
    -- platform should be able to produce afterwards.
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,

    -- What was switched off. Only the mutable categories; see the header.
    category text NOT NULL,

    -- When. From the injected clock (Docs/10 §6.3), not `now()`, so a test with a fixed clock
    -- writes a row it can assert on — the mistake 000703 made and recorded.
    muted_at timestamptz NOT NULL,

    -- One row per account per category, which is also what makes the write idempotent: setting the
    -- same preference twice is an ON CONFLICT that changes a timestamp.
    PRIMARY KEY (user_id, category),

    CONSTRAINT ck_notification_preferences_category
        CHECK (category IN ('job_expiry'))
);

COMMENT ON TABLE notification_preferences IS
    'Categories an account has muted (SHIP-142). A row is a mute; absence is the default, which is that everything is sent. ck_notification_preferences_category is what makes "essential events cannot be muted" a property of the schema rather than of a handler.';

COMMENT ON COLUMN notification_preferences.muted_at IS
    'When the account switched this category off, from the injected clock.';
