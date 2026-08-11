-- Reverse of 000404. Dropping the columns takes their constraints with them.

ALTER TABLE jobs
    DROP COLUMN IF EXISTS goods_description,
    DROP COLUMN IF EXISTS length_cm,
    DROP COLUMN IF EXISTS width_cm,
    DROP COLUMN IF EXISTS height_cm,
    DROP COLUMN IF EXISTS weight_kg,
    DROP COLUMN IF EXISTS vehicle_requirement,
    DROP COLUMN IF EXISTS handling_notes,
    DROP COLUMN IF EXISTS pickup_window_start,
    DROP COLUMN IF EXISTS pickup_window_end,
    DROP COLUMN IF EXISTS dropoff_window_start,
    DROP COLUMN IF EXISTS dropoff_window_end;
