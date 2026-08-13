-- SHIP-62: the draft's own fields — what is being moved, how big it is, when, and anything the
-- driver needs to know.
--
-- 000400 reserved these for this ticket by name ("dimensions, weight, vehicle requirement,
-- notes"), and the date windows join them because Docs/01 §4.1 lists them in the same sentence
-- and because a job without one cannot be published: Docs/02 §6.3 expires an Open job at the
-- earlier of fourteen days or the pickup date passing, so SHIP-68 has nothing to read without
-- pickup_window_end.
--
-- Two fields named in Docs/01 §4.1 are deliberately absent, because each has a ticket of its own
-- that owns the rules as well as the column:
--
--   SHIP-58  the goods *category*, which is reference data loaded at runtime rather than a
--            column with a CHECK — a category list moves under operational pressure and Flutter
--            has no over-the-air update path (Docs/06 §5.3)
--   SHIP-67  the customer's maximum budget, which arrives with the serialisation test that
--            proves it cannot reach a provider (Docs/01 §4.3)
--
-- # Everything is nullable, for the same reason 000403's columns are
--
-- A Draft is allowed to be incomplete. Publication is where completeness is decided (SHIP-63,
-- and Docs/02 §2's "required job details valid"), and requiring a field at creation would stop
-- the app saving what the customer has typed so far.
--
-- The constraints below are therefore about *coherence*, not completeness: a supplied dimension
-- is positive, a supplied window ends no earlier than it starts. Length limits are not here —
-- Docs/06 §5.3 wants a validation limit changeable without a deploy, and a CHECK constraint is a
-- migration. They live in the domain's validator (Docs/10 §4.6).

ALTER TABLE jobs
    -- What is being moved, in the customer's words. The category (SHIP-58) will say what kind of
    -- thing it is; this says which particular one, which is what a provider judges the job by.
    ADD COLUMN goods_description   text,

    -- Centimetres and kilograms, per CLAUDE.md's units. Integers for the dimensions because
    -- nobody measures a pallet to the millimetre and a customer typing 120.5 means 121; numeric
    -- for the weight because half a kilogram is a real quantity and floating point is the wrong
    -- type for anything a price might later be derived from.
    ADD COLUMN length_cm           integer,
    ADD COLUMN width_cm            integer,
    ADD COLUMN height_cm           integer,
    ADD COLUMN weight_kg           numeric(10, 2),

    -- What the customer believes the job needs — "van", "ute with a tailgate lifter", "tray
    -- truck". Freeform text today, and that is a decision rather than an omission: the
    -- vocabulary of vehicle capabilities belongs to the fleet domain (SHIP-79, SHIP-81), which
    -- has not defined it yet, and inventing a second list here would be the drift Docs/10 §3.4
    -- exists to prevent. The column stays text when that list arrives; only the validation
    -- changes.
    ADD COLUMN vehicle_requirement text,

    -- Access constraints, stairs, gate codes, "ring ahead" — the things Docs/01 §4.3 says are
    -- more likely than a budget signal to make a provider's bid accurate.
    ADD COLUMN handling_notes      text,

    -- When the goods can be collected and when they must arrive. Windows rather than instants,
    -- because road transport is not scheduled to the minute and a customer who says "Tuesday or
    -- Wednesday" gets more bids than one who says "10:15".
    ADD COLUMN pickup_window_start  timestamptz,
    ADD COLUMN pickup_window_end    timestamptz,
    ADD COLUMN dropoff_window_start timestamptz,
    ADD COLUMN dropoff_window_end   timestamptz;

ALTER TABLE jobs
    -- Positive, and that is an invariant rather than a policy limit: a zero-length parcel and a
    -- negative weight are not values an operator would ever want to permit. The upper bounds are
    -- the validator's business, because "the largest load this marketplace carries" is exactly
    -- the kind of number operations changes.
    ADD CONSTRAINT ck_jobs_length_cm CHECK (length_cm IS NULL OR length_cm > 0),
    ADD CONSTRAINT ck_jobs_width_cm  CHECK (width_cm  IS NULL OR width_cm  > 0),
    ADD CONSTRAINT ck_jobs_height_cm CHECK (height_cm IS NULL OR height_cm > 0),
    ADD CONSTRAINT ck_jobs_weight_kg CHECK (weight_kg IS NULL OR weight_kg > 0),

    -- A window that ends before it starts is not a window. Either end may still be absent on its
    -- own: a customer who knows only the earliest date they can release the goods has said
    -- something useful, and SHIP-63 decides whether it is enough to publish.
    ADD CONSTRAINT ck_jobs_pickup_window
        CHECK (pickup_window_start IS NULL OR pickup_window_end IS NULL
               OR pickup_window_end >= pickup_window_start),
    ADD CONSTRAINT ck_jobs_dropoff_window
        CHECK (dropoff_window_start IS NULL OR dropoff_window_end IS NULL
               OR dropoff_window_end >= dropoff_window_start);

COMMENT ON COLUMN jobs.vehicle_requirement IS
    'What the customer believes the job needs, freeform. The capability vocabulary belongs to the fleet domain (SHIP-79); this column stays text when it arrives.';
COMMENT ON COLUMN jobs.pickup_window_end IS
    'The latest the goods can be collected. SHIP-68 expires an Open job at the earlier of fourteen days or this passing (Docs/02 §6.3).';
