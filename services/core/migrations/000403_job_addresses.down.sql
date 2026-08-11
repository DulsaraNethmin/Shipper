-- Reverse of 000403. Dropping the columns takes their constraints with them.

ALTER TABLE jobs
    DROP COLUMN IF EXISTS pickup_line,
    DROP COLUMN IF EXISTS pickup_suburb,
    DROP COLUMN IF EXISTS pickup_state,
    DROP COLUMN IF EXISTS pickup_postcode,
    DROP COLUMN IF EXISTS pickup_latitude,
    DROP COLUMN IF EXISTS pickup_longitude,
    DROP COLUMN IF EXISTS pickup_formatted,
    DROP COLUMN IF EXISTS dropoff_line,
    DROP COLUMN IF EXISTS dropoff_suburb,
    DROP COLUMN IF EXISTS dropoff_state,
    DROP COLUMN IF EXISTS dropoff_postcode,
    DROP COLUMN IF EXISTS dropoff_latitude,
    DROP COLUMN IF EXISTS dropoff_longitude,
    DROP COLUMN IF EXISTS dropoff_formatted;
