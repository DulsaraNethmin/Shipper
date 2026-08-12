-- Reverses 000302. The index is derived entirely from rows that remain, so dropping it loses
-- nothing but the plan.

DROP INDEX IF EXISTS idx_vehicles_capability;
