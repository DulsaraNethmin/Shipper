-- SHIP-119: the seventy-two hour auto-complete needs to find Delivered jobs cheaply, and that is
-- the whole of what this migration adds.
--
-- Docs/02 §6.1: "a job recorded as Delivered auto-completes 72 hours later if no dispute is
-- raised." X-6 settled the one question that was open about it on 14 August 2026 — a job delivered
-- through the proof-exception path auto-completes on the same rule, because SHIP-117 queues every
-- exception-completed job for moderation and human review therefore happens either way.
--
-- # There is no delivered_at column, deliberately
--
-- The obvious shape is a `jobs.delivered_at`, filled by a trigger as the job becomes Delivered,
-- exactly as 000406 fills expires_at as it becomes Open. It is not here for two reasons.
--
-- The first is that the fact is already recorded. 000401 writes a job_status_history row for every
-- transition, with `server_recorded_at` defaulted from the platform's clock, and Docs/02 §2 has one
-- way into Delivered — `In transit -> Delivered` — and no way back to it. So "when did this job
-- become Delivered" has exactly one answer already stored, and a column would be a second copy of
-- it that a repair script or a backfill could put out of step. That is the same argument SHIP-117
-- made for not adding a flag beside the exception evidence.
--
-- The second is that Docs/09's own row for this ticket says it "needs no column that does not
-- already exist", written when X-6 was decided. Adding one anyway would quietly contradict the
-- decision this ticket exists to implement.
--
-- # What is missing is not the fact but the way in to it
--
-- The sweep asks "which jobs are Delivered", and `jobs` has no index that answers it. Without one
-- the claim reads every job the platform has ever had, on a table Docs/10 §3.3 never deletes from.
--
-- Partial, on exactly the claim's predicate, for the same reason idx_jobs_open_expiry is
-- (000406): Delivered is a status a job passes through in seventy-two hours, so this index stays
-- the size of the deliveries in flight rather than the size of the history. The indexed column is
-- updated_at because 000001's trigger sets it on the transition that made the job Delivered, so an
-- ordered scan of this index is very nearly delivery order — but the sweep's ordering comes from
-- job_status_history and not from here, and nothing depends on the two agreeing.
--
-- The other half of the claim is already indexed. idx_job_status_history_job is
-- (job_id, server_recorded_at DESC), which is what the correlated lookup of "when did this job
-- become Delivered" probes.

CREATE INDEX idx_jobs_delivered ON jobs (updated_at) WHERE status = 'Delivered';

COMMENT ON INDEX idx_jobs_delivered IS
    'The seventy-two hour auto-complete sweep''s way in: the jobs currently Delivered (Docs/02 §6.1, SHIP-119). Partial, so it stays the size of the deliveries in flight.';
