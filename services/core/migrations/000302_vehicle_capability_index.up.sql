-- SHIP-81: the index the eligibility filter's capability half reads.
--
-- Docs/01 §4.3 requires the platform to "filter jobs by provider service area, vehicle
-- capability, verification state, and job status". Three of those four already have the index
-- they need: `users` is reached by primary key, and `provider_service_areas` by
-- uq_provider_service_areas (provider_id, scope, area), which 000301 created leading with the
-- provider precisely so this read could use it.
--
-- The capability half is the exception, and it is the clause of the predicate that runs **once
-- per candidate job**: "does this provider have a vehicle still in service that could carry it?"
-- idx_vehicles_provider covers (provider_id, created_at DESC) and would answer it, but only by
-- visiting every one of the provider's rows in the heap — the retired ones included — to read
-- four columns.
--
-- So: partial on the vehicles still in service, carrying the four capacity columns as INCLUDE
-- payload so the check is an index-only scan. A provider with forty vehicles, thirty of them
-- retired, is ten index entries and no heap access.
--
-- INCLUDE rather than four more key columns, deliberately. These columns are never searched on a
-- range that leads — the comparison is `v.load_length_cm >= j.length_cm` against a *job's*
-- dimension, which no ordering of this index can seek on — so putting them in the key would
-- enlarge every entry and buy nothing. INCLUDE puts them in the leaf, where the scan can read
-- them and nothing else has to.
--
-- # This does not duplicate uq_vehicles_provider_registration
--
-- That index is partial on the same predicate, but its key is (provider_id, registration) and it
-- carries no capacity. It answers "is this plate already in service"; this one answers "can any
-- of these vehicles carry this". PostgreSQL could use the unique index for the second question
-- and would then read the heap tuple of every vehicle the provider owns, which is the cost this
-- exists to remove.

CREATE INDEX idx_vehicles_capability
    ON vehicles (provider_id)
    INCLUDE (max_weight_kg, load_length_cm, load_width_cm, load_height_cm)
    WHERE deactivated_at IS NULL;

COMMENT ON INDEX idx_vehicles_capability IS
    'The capability half of SHIP-81''s eligibility filter: for one provider, has any vehicle still in service the capacity for this job? Partial and covering, so the check is an index-only scan.';

-- # Two indexes SHIP-81 deliberately does NOT create, and who they belong to
--
-- **The job-first direction on provider_service_areas — "which providers serve this postcode" —
-- still has no caller.** 000301 left it for "whoever writes the notification fan-out" and expected
-- SHIP-81 to be the one that needed it. It is not. The eligibility filter is *provider*-first in
-- both of its forms: the feed asks "which open jobs may this provider see" and SHIP-84 asks "may
-- this provider see this job", and both bind provider_id first — which is exactly what
-- uq_provider_service_areas leads on. An index on (scope, area) would serve the fan-out — publish
-- a job, notify every provider covering its pickup — and that is M5's ticket. Creating it here
-- would be write amplification on every declaration change in exchange for a query nobody makes.
--
-- **The `jobs` index this feed will eventually want is not this migration's to create**, and that
-- is a finding rather than an omission. The feed reads
--
--     WHERE status IN ('Open', 'Negotiating') AND (expires_at IS NULL OR expires_at > $2)
--     ORDER BY created_at DESC, id DESC
--
-- and nothing in `jobs` indexes that: idx_jobs_open_expiry is partial on 'Open' alone and ordered
-- by expires_at. The index wanted is roughly
--
--     CREATE INDEX idx_jobs_biddable ON jobs (created_at DESC, id DESC)
--         WHERE status IN ('Open', 'Negotiating');
--
-- and `jobs` is block 400–499. A fleet migration may not draw a number from it, and creating an
-- index on another domain's table from this block would put two domains' schema history in one
-- file for the next person to unpick. **This is the one real cost of reading another domain's
-- table in SQL: the SELECT crosses the boundary and the index cannot.** Until the jobs track adds
-- it, the feed plans as a sequential scan on `jobs` under a LIMIT — correct, and cheap while open
-- jobs number in the hundreds. internal/fleet/eligibility.go carries the whole argument and
-- Docs/11 §3 records what would change it.
