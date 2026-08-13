-- SHIP-60: where the goods are collected and where they are taken.
--
-- Two locations, each an Australian address in four parts plus the coordinate a geocoder
-- resolved it to. 000400 reserved this ticket a line in its forward plan and it is claimed here.
--
-- # Why four columns rather than one text blob
--
-- The address is read by three different things and each wants a different part of it. A
-- provider navigating wants the whole of it; SHIP-79's service area and SHIP-81's eligibility
-- filter want the suburb, the state and the postcode as values they can compare; and Docs/01
-- §4.1 has the customer typing it into a form with those fields already separated. A single
-- freeform column would have every one of those parsing it back out, three times, differently.
--
-- The street part stays freeform. Unit numbers, level numbers, lot numbers, PO boxes, RMBs and
-- "the shed behind the second gate" are all legitimate first lines of an Australian address, and
-- a schema that tried to structure them would refuse deliveries the platform exists to carry.
--
-- # Why every column is nullable
--
-- A Draft is allowed to be incomplete. Docs/01 §4.1 lets a customer save a draft and come back
-- to it, SHIP-75 has a partially completed job surviving an app restart, and the wizard in
-- SHIP-71…74 captures the locations in one step of several. Requiring an address at creation
-- would mean the app could not save what the customer has typed so far, which is the whole
-- purpose of a draft.
--
-- The completeness rules live at publication instead (SHIP-63), which is where Docs/02 §2 puts
-- them: "Draft → Open: required job details valid".
--
-- What is enforced here is that a *supplied* value is well formed, and that the parts of one
-- address agree with each other: a coordinate is either fully present or fully absent, and a
-- state is one of the eight.

ALTER TABLE jobs
    -- Pickup.
    ADD COLUMN pickup_line       text,
    ADD COLUMN pickup_suburb     text,
    ADD COLUMN pickup_state      text,
    ADD COLUMN pickup_postcode   text,

    -- What the geocoder made of it. Both NULL when the address has not been resolved — either
    -- because it has not been supplied yet, or because the lookup failed or did not recognise
    -- it. SHIP-59a is explicit that an unrecognised address is an ordinary outcome and must not
    -- fail the job, so "no coordinate" is a state the product supports rather than an error to
    -- be avoided.
    ADD COLUMN pickup_latitude   double precision,
    ADD COLUMN pickup_longitude  double precision,

    -- The provider's own rendering of the address it matched, kept for display and for support:
    -- when a customer says the driver went to the wrong place, this is what the platform
    -- actually looked up. NULL whenever the coordinate is.
    ADD COLUMN pickup_formatted  text,

    -- Drop-off, identical in every respect.
    ADD COLUMN dropoff_line      text,
    ADD COLUMN dropoff_suburb    text,
    ADD COLUMN dropoff_state     text,
    ADD COLUMN dropoff_postcode  text,
    ADD COLUMN dropoff_latitude  double precision,
    ADD COLUMN dropoff_longitude double precision,
    ADD COLUMN dropoff_formatted text;

-- The eight states and territories, in the abbreviated form Australia Post uses and every
-- Australian address form offers. Held to the Go constants by
-- TestJobStateConstraintMatchesTheGoConstants, the same pairing Docs/10 §3.4 requires of every
-- enumeration and that ck_jobs_status already has.
ALTER TABLE jobs
    ADD CONSTRAINT ck_jobs_pickup_state
        CHECK (pickup_state IS NULL OR pickup_state IN
            ('ACT', 'NSW', 'NT', 'QLD', 'SA', 'TAS', 'VIC', 'WA')),
    ADD CONSTRAINT ck_jobs_dropoff_state
        CHECK (dropoff_state IS NULL OR dropoff_state IN
            ('ACT', 'NSW', 'NT', 'QLD', 'SA', 'TAS', 'VIC', 'WA')),

    -- Four digits, leading zeros kept — which is why the column is text and not an integer.
    -- 0800 is Darwin and 0872 is remote South Australia and the Northern Territory; stored as a
    -- number they become 800 and 872 and stop matching anything.
    --
    -- The range within a state is deliberately not checked. The allocations have exceptions
    -- (2600 and 2611 are ACT inside the NSW range, 3644 is NSW inside the Victorian one), they
    -- change when Australia Post says they do, and a CHECK constraint is the wrong place for a
    -- rule that moves under operational pressure (Docs/06 §5.3).
    ADD CONSTRAINT ck_jobs_pickup_postcode
        CHECK (pickup_postcode IS NULL OR pickup_postcode ~ '^[0-9]{4}$'),
    ADD CONSTRAINT ck_jobs_dropoff_postcode
        CHECK (dropoff_postcode IS NULL OR dropoff_postcode ~ '^[0-9]{4}$'),

    -- A coordinate is a pair or it is nothing. Half of one is not a location a driver can be
    -- sent to, and a latitude with no longitude is the shape a partial write leaves behind.
    ADD CONSTRAINT ck_jobs_pickup_coordinate_is_a_pair
        CHECK ((pickup_latitude IS NULL) = (pickup_longitude IS NULL)),
    ADD CONSTRAINT ck_jobs_dropoff_coordinate_is_a_pair
        CHECK ((dropoff_latitude IS NULL) = (dropoff_longitude IS NULL)),

    ADD CONSTRAINT ck_jobs_pickup_latitude
        CHECK (pickup_latitude IS NULL OR pickup_latitude BETWEEN -90 AND 90),
    ADD CONSTRAINT ck_jobs_dropoff_latitude
        CHECK (dropoff_latitude IS NULL OR dropoff_latitude BETWEEN -90 AND 90),
    ADD CONSTRAINT ck_jobs_pickup_longitude
        CHECK (pickup_longitude IS NULL OR pickup_longitude BETWEEN -180 AND 180),
    ADD CONSTRAINT ck_jobs_dropoff_longitude
        CHECK (dropoff_longitude IS NULL OR dropoff_longitude BETWEEN -180 AND 180);

COMMENT ON COLUMN jobs.pickup_line IS
    'The street part of the pickup address, freeform: unit and lot numbers, PO boxes and RMBs are all legitimate.';
COMMENT ON COLUMN jobs.pickup_latitude IS
    'Resolved by the geocoding adapter, NULL when the address has not been resolved. An unrecognised address is an ordinary outcome and never fails the job (SHIP-59a).';
COMMENT ON COLUMN jobs.dropoff_latitude IS
    'Resolved by the geocoding adapter, NULL when the address has not been resolved (SHIP-59a).';
