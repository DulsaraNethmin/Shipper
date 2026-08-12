-- SHIP-67: the customer's maximum budget — stored, and never serialised to a provider.
--
-- 000400 reserved this column by name and by type ("the customer's maximum budget, numeric(12,2)
-- and never serialised to a provider"), and 000404 left it out deliberately, because the field
-- was to arrive with the ticket that also brings the proof it cannot leak (Docs/01 §4.3).
--
-- # The privacy rule is not enforceable here, and saying so is the point
--
-- A column cannot refuse to be selected. What keeps Docs/01 §4.3 true is that the provider-facing
-- shapes are separate types in internal/jobs rather than the customer's shape with fields hidden,
-- and that a test reads this package's own source and refuses a `budget` json tag anywhere but on
-- the owner's response. This migration's contribution is to make the value *exist in one place* —
-- one column, on the job, read by the customer's endpoints and by nothing else.
--
-- # numeric(12,2), like every other amount in this platform
--
-- Docs/10 §3.3: money is numeric(12,2) in PostgreSQL and minor units as int64 in Go. Never a
-- float. The Go side holds cents and converts at the boundary in postgres.go, so nothing in the
-- domain, the handlers or the wire format ever rounds.
--
-- AUD is implied. There is no currency column in the MVP (Docs/10 §3.3), and adding one later is
-- an ordinary migration; guessing at one now would be a column every query has to carry for a
-- product that operates in one country (CLAUDE.md).
--
-- # Nullable, and positive when present
--
-- Optional, per Docs/01 §4.1 — "optional maximum budget". NULL is "not supplied", which is
-- distinct from any amount a customer could enter, because the constraint refuses zero and
-- everything below it. That is what lets the Go zero value mean "absent" without ambiguity, the
-- same arrangement 000404's dimensions use.
--
-- The upper bound is the validator's, not the constraint's, for the reason 000404 gives: "the
-- largest budget this marketplace accepts" is exactly the kind of number operations changes, and
-- a CHECK constraint is a migration (Docs/06 §5.3).

ALTER TABLE jobs
    ADD COLUMN budget numeric(12, 2);

ALTER TABLE jobs
    ADD CONSTRAINT ck_jobs_budget CHECK (budget IS NULL OR budget > 0);

COMMENT ON COLUMN jobs.budget IS
    'The customer''s maximum, in AUD. Private from providers in every form — not an amount, not a band, not a "budget supplied" flag (Docs/01 §4.3, SHIP-67). No provider-facing shape may carry it.';
