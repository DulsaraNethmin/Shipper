-- SHIP-79: what a provider declares about the work they take — where they will go, and what they
-- carry. Docs/01 §4.2: "nominate service area and specialties".
--
-- Two tables rather than one, and both of them sets. A provider serves several regions and holds
-- several specialties; neither is a column on a profile row. There is deliberately no parent row
-- either: "this provider has declared nothing" is the absence of rows, which is one representation
-- of the empty declaration rather than two that can disagree.
--
-- # The decision this migration takes: a service area is a set of named regions, not a radius
--
-- SHIP-79 had to settle it, because SHIP-81 filters on it and Docs/11 §9 names "the first ticket
-- needing the distance between two coordinates in a domain other than jobs" as the trigger for
-- creating a neutral internal/geo package. **Settled: named regions.** So the trigger does not
-- fire, SHIP-81 is a set-membership query, and no geo package is needed. The reasoning, in the
-- order it weighed:
--
--   1. **A radius would make eligibility depend on a field the platform may not have.** A job's
--      coordinate is best-effort: SHIP-60 stores the address as typed when a lookup fails, and
--      Docs/11 §3 records that staging and production get no geocoder at all until one is
--      configured. A radius filter would silently show a provider *no* jobs in exactly that case,
--      which is a marketplace with nothing in it and no error anywhere. A state and a postcode are
--      typed by the customer and validated on the way in, so they are always there.
--   2. **It is how Australian road freight is actually quoted.** Providers price by postcode zone
--      and by state, not by kilometres from a depot; and a straight-line radius is wrong about
--      roads — 300 km from Melbourne reaches Tasmania across Bass Strait.
--   3. **Docs/11 §3 already assumed it.** SHIP-60 gave the job address four parts rather than one
--      freeform line precisely "because the suburb, the state and the postcode are values SHIP-79
--      and SHIP-81 will compare", and the geo decision recorded that "SHIP-79 has a provider
--      declare a service area and SHIP-81 filters on it; neither names a radius in kilometres".
--      This is that reading held to rather than reversed.
--
-- # Why an area names one grain and never two
--
-- An entry is *either* a whole state or one postcode, never a postcode qualified by a state. That
-- is not tidiness: Docs/11 §3 records SHIP-60's deliberate refusal to validate a postcode against
-- its state, because the allocations have exceptions (2600 is ACT inside the NSW range) and move
-- when Australia Post says so. A row carrying both would be a row that could disagree with itself,
-- and checking it would be exactly the validation that decision refuses. One grain per row leaves
-- nothing to be inconsistent about.

CREATE TABLE provider_service_areas (
    id          uuid        PRIMARY KEY,

    -- The provider who declared it. No CHECK that the account's role is 'provider' — a foreign key
    -- cannot see another table's column — so the domain enforces it where a declaration is made,
    -- the arrangement 000300 and 000400 both use.
    --
    -- ON DELETE RESTRICT per Docs/10 §3.3. SHIP-171 pseudonymises an account rather than removing
    -- it, so nothing should be deleting a user row at all.
    provider_id uuid        NOT NULL,

    -- How wide the entry is: 'state' for a whole state or territory, 'postcode' for one postcode.
    -- text with a CHECK rather than an ENUM type, per Docs/10 §3.4.
    scope       text        NOT NULL,

    -- What it names, at that scope. 'NSW' at the state scope, '2000' at the postcode scope. Held
    -- as text in both cases because a postcode is not a number: 0800 is Darwin and an integer
    -- column would store it as 800.
    area        text        NOT NULL,

    created_at  timestamptz NOT NULL DEFAULT now(),

    -- One constraint covering both scopes, so an unknown scope is impossible without being listed
    -- here. The eight states are the same eight jobs.State holds and ck_jobs_pickup_state lists;
    -- fleet cannot import the jobs domain to say so, and a test pairs this constraint with
    -- fleet.ServiceAreaStates in both directions the way Docs/10 §3.4 requires.
    CONSTRAINT ck_provider_service_areas_scope_and_area CHECK (
        (scope = 'state'    AND area IN ('ACT', 'NSW', 'NT', 'QLD', 'SA', 'TAS', 'VIC', 'WA')) OR
        (scope = 'postcode' AND area ~ '^[0-9]{4}$')
    ),

    CONSTRAINT fk_provider_service_areas_provider
        FOREIGN KEY (provider_id) REFERENCES users (id) ON DELETE RESTRICT
);

-- A service area is a *set*: declaring NSW twice declares it once.
--
-- The index is what makes that true rather than the writer remembering to deduplicate, which
-- matters because the declaration is replaced wholesale and two requests arriving together would
-- otherwise interleave into a set holding the same region twice.
--
-- provider_id leads, which also satisfies Docs/10 §3.3's "every foreign key is indexed" and serves
-- the read SHIP-81 will make: for one provider, does any entry match this job's pickup? The other
-- direction — which providers serve this postcode — has no caller yet and gets its index from
-- whoever writes the notification fan-out.
CREATE UNIQUE INDEX uq_provider_service_areas
    ON provider_service_areas (provider_id, scope, area);

-- SHIP-79's other half: what kind of work the provider takes.
--
-- **A declaration, and never a permission.** Naming dangerous_goods here claims a capability; it
-- grants nothing. What a provider is actually licensed and insured to carry is verification's
-- business (Docs/04 §3), and the prohibited-goods list is X-9's. A row here is what a customer
-- reads when comparing bids (Docs/01 §4.3, "declared capability") and what a later filter may
-- narrow a feed by — nothing more.
CREATE TABLE provider_specialties (
    id          uuid        PRIMARY KEY,
    provider_id uuid        NOT NULL,

    -- One of the twelve in fleet.Specialties, stored exactly as the wire spells it — the same
    -- decision vehicles.vehicle_type took, and for the same reason: this list has no document
    -- behind it the way Docs/02 §1 stands behind the job statuses, so a second spelling would be a
    -- mapping with nothing on the other end.
    specialty   text        NOT NULL,

    created_at  timestamptz NOT NULL DEFAULT now(),

    -- Paired with fleet.Specialties by a test that reads this constraint out of pg_constraint, in
    -- both directions (Docs/10 §3.4). A specialty the database accepts and Go does not know is a
    -- row nothing can render; one Go has and the database refuses is a form that fails on
    -- submission.
    CONSTRAINT ck_provider_specialties_specialty CHECK (specialty IN (
        'general_freight',
        'courier',
        'furniture_removals',
        'refrigerated',
        'fragile_and_high_value',
        'oversized',
        'heavy_haulage',
        'construction_materials',
        'bulk_materials',
        'vehicle_transport',
        'livestock',
        'dangerous_goods'
    )),

    CONSTRAINT fk_provider_specialties_provider
        FOREIGN KEY (provider_id) REFERENCES users (id) ON DELETE RESTRICT
);

CREATE UNIQUE INDEX uq_provider_specialties
    ON provider_specialties (provider_id, specialty);

-- Neither table carries updated_at, and neither attaches set_updated_at().
--
-- Nothing updates a row here. A declaration is a set, so changing it inserts and deletes rows; an
-- entry that survives a change is the same entry, and one that does not is gone. created_at
-- therefore says when the provider first declared that region or specialty, which survives
-- re-declaring the same set — the store adds and removes the difference rather than replacing
-- every row.

COMMENT ON TABLE provider_service_areas IS
    'Where a provider will carry freight, as a set of named regions. One entry is a whole state or one postcode, never both: Docs/11 §3 records that a postcode is deliberately not validated against a state (SHIP-79).';
COMMENT ON COLUMN provider_service_areas.scope IS
    'state or postcode. A service area is a set of named regions rather than a radius, so SHIP-81 is a set-membership query and needs no distance arithmetic.';
COMMENT ON TABLE provider_specialties IS
    'What kind of work a provider takes, from the closed list in fleet.Specialties. A declaration, never a permission: licences and insurance are verification''s business (Docs/04 §3) (SHIP-79).';
