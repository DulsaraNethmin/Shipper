-- SHIP-78: vehicles — the fleet a provider maintains, and the first table in block 300–399.
--
-- Docs/01 §4.2 gives the provider four verbs over a fleet: add, edit, deactivate, and select one
-- or more. This table carries the first three. Selection is a bid's business (SHIP-89) and needs
-- no column here.
--
-- # A vehicle is deactivated, never deleted, and that is structural rather than a habit
--
-- internal/fleet/doc.go states the rule: "a deactivated vehicle stops making its provider eligible
-- for new work without disturbing deliveries already under way". A vehicle is named by the bid
-- that won a job and by the delivery that followed it (SHIP-89, SHIP-105), so a row that could
-- vanish would take a commercial record with it — which Docs/05 §3.1 requires retaining and
-- Docs/10 §3.3 has no soft delete for.
--
-- deactivated_at rather than an `active` boolean, for two reasons. It says *when*, which support
-- needs when a provider asks why a job stopped reaching them; and it makes the partial unique
-- index below expressible, which a boolean would too but less honestly.
--
-- **This is not a soft delete.** A deactivated vehicle is still read, still shown to the customer
-- comparing the bid it was offered on, and can be brought back. What it stops doing is making its
-- provider eligible for new work.
--
-- # What is deliberately not here
--
--   SHIP-79  service area and specialties. Those belong to the *provider*, not to one vehicle,
--            and the capability vocabulary jobs.vehicle_requirement will one day be validated
--            against is that ticket's to define. vehicle_type below is a property of the vehicle
--            and is not that list.
--   SHIP-81  the eligibility filter. It reads these columns; it adds none.
--   SHIP-84+ verification state, which belongs to profiles and is checked against the provider
--            rather than against the vehicle.

CREATE TABLE vehicles (
    id          uuid        PRIMARY KEY,

    -- The provider who owns the vehicle. There is no CHECK that this account's role is
    -- 'provider', because a foreign key cannot see another table's column — the domain enforces
    -- it where a vehicle is added, the same arrangement 000400 uses for jobs.customer_id.
    --
    -- ON DELETE RESTRICT per Docs/10 §3.3. SHIP-171 pseudonymises an account rather than removing
    -- it, so nothing should be deleting a user row at all.
    provider_id uuid        NOT NULL,

    -- The registration plate, stored upper case with its whitespace collapsed. Normalisation is
    -- the domain's, not the column's: a CHECK enforcing a plate format would be wrong within a
    -- year, since the six states and two territories issue different shapes and custom plates
    -- exist in all of them.
    registration text       NOT NULL,

    -- What kind of vehicle it is, from a closed list. text with a CHECK rather than a PostgreSQL
    -- ENUM type, per Docs/10 §3.4 — ALTER TYPE … ADD VALUE cannot run in a transaction block
    -- alongside its use, and a CHECK constraint is an ordinary migration.
    --
    -- Stored exactly as the wire form spells it, which is the one place this differs from
    -- jobs.status. Job statuses are stored as Docs/02 §1 writes them because that document is
    -- their source; this list has no document behind it, so a second spelling would be a mapping
    -- with nothing on the other end. fleet.VehicleTypes is the Go copy and a test holds the two
    -- together.
    vehicle_type text       NOT NULL,

    -- Who made it and what they call it. Both optional: a provider adding a vehicle in a truck
    -- yard has the plate to hand and may not have anything else, and Docs/01 §4.2's acceptance
    -- measure is that they can maintain a fleet rather than that they can fill in a form.
    make        text,
    model       text,

    -- What it can carry. Optional for the same reason, and the units are CLAUDE.md's: kilometres,
    -- kilograms, centimetres. numeric for the mass because half a tonne is a real quantity and
    -- floating point is the wrong type for anything a price might later be derived from.
    --
    -- These are the load space, not the vehicle's own dimensions — a customer's 190 cm sofa has
    -- to fit inside, not alongside.
    max_weight_kg    numeric(10, 2),
    load_length_cm   integer,
    load_width_cm    integer,
    load_height_cm   integer,

    -- NULL while the vehicle is in service. Set when the provider deactivates it, and cleared
    -- when they bring it back.
    deactivated_at timestamptz,

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ck_vehicles_type CHECK (vehicle_type IN (
        'van',
        'ute',
        'tray_truck',
        'box_truck',
        'refrigerated_truck',
        'flatbed',
        'tipper',
        'prime_mover',
        'trailer',
        'motorcycle',
        'car'
    )),

    -- A plate is not optional and not blank. Everything else about a vehicle may be filled in
    -- later; without this there is nothing identifying the row at all.
    CONSTRAINT ck_vehicles_registration CHECK (length(btrim(registration)) > 0),

    -- Coherence, not policy. A zero-capacity vehicle and a negative load space are not values an
    -- operator would ever want to permit; "the largest load this marketplace carries" is the
    -- validator's business, because that is a number operations changes (Docs/06 §5.3).
    CONSTRAINT ck_vehicles_max_weight_kg
        CHECK (max_weight_kg  IS NULL OR max_weight_kg  > 0),
    CONSTRAINT ck_vehicles_load_length_cm
        CHECK (load_length_cm IS NULL OR load_length_cm > 0),
    CONSTRAINT ck_vehicles_load_width_cm
        CHECK (load_width_cm  IS NULL OR load_width_cm  > 0),
    CONSTRAINT ck_vehicles_load_height_cm
        CHECK (load_height_cm IS NULL OR load_height_cm > 0),

    CONSTRAINT fk_vehicles_provider
        FOREIGN KEY (provider_id) REFERENCES users (id) ON DELETE RESTRICT
);

-- Every foreign key is indexed (Docs/10 §3.3), and this one is also the read path for the
-- provider's own fleet list, which is ordered newest first. One index serves both.
CREATE INDEX idx_vehicles_provider ON vehicles (provider_id, created_at DESC);

-- One live vehicle per plate per provider.
--
-- A **partial** unique index, covering only vehicles still in service. That is what lets a
-- provider retire a plate and later add it again — a truck sold and a replacement given the same
-- personalised plate is ordinary — while stopping the duplicate that actually causes trouble: two
-- active rows for one truck, which would let the same vehicle be offered on two jobs at once and
-- make the fleet list read as a larger business than it is.
--
-- **Scoped to the provider, deliberately.** Two providers claiming one plate is a real-world
-- dispute — a sold vehicle whose previous owner never deactivated it — and adjudicating it is
-- verification's job (Docs/04), not a constraint's. A global unique index would answer it by
-- refusing the honest provider, whichever of the two typed second.
CREATE UNIQUE INDEX uq_vehicles_provider_registration
    ON vehicles (provider_id, registration)
    WHERE deactivated_at IS NULL;

CREATE TRIGGER vehicles_set_updated_at
    BEFORE UPDATE ON vehicles
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON TABLE vehicles IS
    'One vehicle in a provider''s fleet. Deactivated, never deleted: a vehicle is named by the bid that won a job and by the delivery that followed it (SHIP-78).';
COMMENT ON COLUMN vehicles.deactivated_at IS
    'NULL while in service. A deactivated vehicle stops making its provider eligible for new work without disturbing deliveries already under way.';
COMMENT ON COLUMN vehicles.vehicle_type IS
    'One of the eleven types in fleet.VehicleTypes, stored exactly as the wire spells it. Not the capability vocabulary SHIP-79 owns.';
