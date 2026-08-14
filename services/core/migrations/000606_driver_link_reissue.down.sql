-- Reverse of 000606. IF EXISTS throughout, so `migrate down all` on a database this migration
-- failed part-way through still leaves a clean tree rather than a dirty one.

ALTER TABLE driver_assignments DROP CONSTRAINT IF EXISTS ck_driver_assignments_link_issue_count;
ALTER TABLE driver_assignments DROP CONSTRAINT IF EXISTS ck_driver_assignments_link_token_id;

ALTER TABLE driver_assignments DROP COLUMN IF EXISTS link_issue_count;
ALTER TABLE driver_assignments DROP COLUMN IF EXISTS link_issued_at;
ALTER TABLE driver_assignments DROP COLUMN IF EXISTS link_token_id;
