-- SHIP-58: the goods category a job carries.
--
-- 000404 brought the draft's own descriptive fields and left this one out, because the category
-- is not a free-text field the customer writes — it is a choice from a list the platform
-- publishes, and until X-9 there was no list to choose from. There is one now, in reduced form:
-- Docs/11 §4 records that the list shipping today is provisional, approved by the repository
-- owner rather than by a legal adviser, and that X-4 still owes the reviewed answer.
--
-- # There is deliberately no CHECK constraint naming the categories
--
-- This is the same argument 000405 makes about the budget's upper bound, and it matters more
-- here. The category list is reference data that operations changes — CLAUDE.md names "category
-- lists" first in its list of things that live server-side — and a CHECK constraint is a
-- migration. A list in a constraint would mean a lawyer's answer to X-4 arrives as a schema
-- change and a deployment, which is exactly the coupling SHIP-58's *Done when* forbids when it
-- says the categories load from configuration rather than being compiled in.
--
-- So the column holds whatever the configured catalogue says is a category, and internal/jobs
-- refuses one it does not recognise. That puts the list in one place instead of two that drift,
-- which is the same reasoning model.go gives for keeping the transition table out of 000402.
--
-- # Nullable, because a draft need not have chosen yet
--
-- Docs/01 §4.1 lets a customer save a draft and come back to it, and every field 000404 added is
-- nullable for that reason. NULL is "not chosen"; the empty string is not a value the column can
-- hold, so the two readings cannot be confused. Whether a category is *required* is a question
-- about publication rather than about a draft, and SHIP-63 asks it.

ALTER TABLE jobs
    ADD COLUMN goods_category text;

ALTER TABLE jobs
    ADD CONSTRAINT ck_jobs_goods_category CHECK (goods_category IS NULL OR goods_category <> '');

COMMENT ON COLUMN jobs.goods_category IS
    'The category code the customer chose, from the catalogue the platform serves at GET /v1/goods-categories (SHIP-58). Deliberately not constrained to a list of values: the catalogue is configuration that operations changes, and a CHECK naming the categories would make X-4''s answer a migration.';
