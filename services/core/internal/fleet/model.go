package fleet

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// VehicleType is what kind of vehicle a fleet entry is.
//
// # This is not the capability vocabulary, and the distinction is worth keeping
//
// `jobs.vehicle_requirement` is free text — "ute with a tailgate lifter" — and 000404 says the
// vocabulary it will one day be validated against belongs to this domain. That vocabulary is
// [Specialty], built at SHIP-79: what a *provider* declares about the work they take. This list is
// narrower and answers a different question — what is this vehicle — which cannot be deferred,
// because a fleet record that does not say what the vehicle is describes nothing. Nothing in `jobs`
// is validated against these values and nothing here reads that column.
//
// Eleven values, closed. It covers what an Australian road-transport marketplace moves general
// freight with, and adding a twelfth is a migration plus a constant plus a line in the contract —
// which is the right amount of friction for a list three clients render.
//
// The stored form is the wire form, unlike [jobs.Status]. Docs/10 §3.4 stores job statuses as
// Docs/02 §1 writes them because that document is their source and three languages hold a copy;
// this list has no document behind it, so a second spelling would be a mapping with nothing on the
// other end. Docs/10 §4.7's lower snake case is therefore what the column holds.
type VehicleType string

const (
	TypeVan               VehicleType = "van"
	TypeUte               VehicleType = "ute"
	TypeTrayTruck         VehicleType = "tray_truck"
	TypeBoxTruck          VehicleType = "box_truck"
	TypeRefrigeratedTruck VehicleType = "refrigerated_truck"
	TypeFlatbed           VehicleType = "flatbed"
	TypeTipper            VehicleType = "tipper"
	TypePrimeMover        VehicleType = "prime_mover"
	TypeTrailer           VehicleType = "trailer"
	TypeMotorcycle        VehicleType = "motorcycle"
	TypeCar               VehicleType = "car"
)

// VehicleTypes is every type, smallest to largest.
//
// Ordered rather than a set because a client renders it as a picker and "van, ute, car" in
// alphabetical order reads as an accident. It is also what TestVehicleTypeConstraintMatchesTheGoConstants
// compares against ck_vehicles_type: Docs/10 §3.4 pairs every enumeration with a test that reads
// the constraint out of pg_constraint, because a type the database accepts and Go does not know is
// a row nothing can render, and a type Go has and the database refuses is a form that fails on
// submission.
var VehicleTypes = []VehicleType{
	TypeMotorcycle,
	TypeCar,
	TypeUte,
	TypeVan,
	TypeTrayTruck,
	TypeBoxTruck,
	TypeRefrigeratedTruck,
	TypeFlatbed,
	TypeTipper,
	TypePrimeMover,
	TypeTrailer,
}

// Valid reports whether t is one of the eleven.
func (t VehicleType) Valid() bool {
	for _, known := range VehicleTypes {
		if t == known {
			return true
		}
	}
	return false
}

func (t VehicleType) String() string { return string(t) }

// normalise tolerates case and surrounding space on the way in.
//
// Tolerant on input and exact on output, which is the same asymmetry the Australian state takes in
// `jobs`: a client that sends `Van` meant `van`, and refusing it teaches nobody anything. What is
// *not* tolerated is a value outside the list — [VehicleFields.problems] answers that with a field
// error rather than with a silent fallback, because a vehicle stored as the wrong type is a vehicle
// offered on jobs it cannot do.
//
// Unexported, and there is deliberately no exported FromWire beside it. A second entry point would
// be a second answer to what `  VAN ` means, and the handler already reaches the domain's copy by
// passing the raw string through.
func (t VehicleType) normalise() VehicleType {
	return VehicleType(strings.ToLower(strings.TrimSpace(string(t))))
}

// Capacity is what a vehicle can carry.
//
// The load space, not the vehicle's own dimensions: a customer's 190 cm sofa has to fit inside,
// not alongside. Every field is optional and the zero value means "not stated" — the columns refuse
// zero on their own (ck_vehicles_load_length_cm and its three siblings), so zero and NULL cannot be
// confused in either direction.
//
// A struct rather than four fields on [Vehicle] because SHIP-81 compares a job's dimensions against
// exactly this group, and a value it can pass whole is one it cannot pass three quarters of.
type Capacity struct {
	MaxWeightKg float64
	LengthCm    int
	WidthCm     int
	HeightCm    int
}

// Vehicle is one entry in a provider's fleet.
type Vehicle struct {
	ID         uuid.UUID
	ProviderID uuid.UUID

	// Registration is the plate, upper case with its whitespace removed. See
	// [VehicleFields.normalise] for why that is the stored form rather than what was typed.
	Registration string

	Type  VehicleType
	Make  string
	Model string

	Capacity Capacity

	// DeactivatedAt is when the provider took the vehicle out of service, and the zero time while
	// it is in service.
	//
	// A time rather than a boolean, because support asking "why did this provider stop seeing
	// jobs" wants the date, and because uq_vehicles_provider_registration is partial on this
	// column being NULL.
	DeactivatedAt time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Active reports whether the vehicle is in service.
//
// **Active is not the same as "not deleted".** Nothing deletes a vehicle: it is named by the bid
// that won a job and by the delivery that followed, so the row outlives the provider's interest in
// it (Docs/10 §3.3, and 000300's own comment). Deactivation stops it making its provider eligible
// for new work and changes nothing about work already under way.
func (v Vehicle) Active() bool { return v.DeactivatedAt.IsZero() }

// VehicleFields is a partial description of a vehicle, used by both writing verbs.
//
// One type for creation and for editing, because a field a provider can set when adding a vehicle
// is a field they can change afterwards, and two types would be two places for that list to drift.
// Every field is a pointer, so absent, null and an explicit empty value are three distinct things:
//
//	omitted or null   leave it alone — on create that means unset, on edit unchanged
//	an empty value    clear it
//
// The second is not decoration: without it a provider could state a vehicle's maximum weight and
// never correct it back to "unstated", because 0 would be indistinguishable from having said
// nothing.
//
// **Registration and Type are the exception**, and only on creation: [Service.Add] requires both,
// because a row with a blank plate identifies nothing and a row with no type describes nothing.
// Neither may be cleared by an edit for the same reason.
type VehicleFields struct {
	Registration *string
	Type         *VehicleType
	Make         *string
	Model        *string

	MaxWeightKg *float64
	LengthCm    *int
	WidthCm     *int
	HeightCm    *int
}

// IsEmpty reports whether the caller named no field at all.
func (f VehicleFields) IsEmpty() bool {
	return f.Registration == nil && f.Type == nil && f.Make == nil && f.Model == nil &&
		f.MaxWeightKg == nil && f.LengthCm == nil && f.WidthCm == nil && f.HeightCm == nil
}

// applyTo returns v with every supplied field replaced.
func (f VehicleFields) applyTo(v Vehicle) Vehicle {
	if f.Registration != nil {
		v.Registration = *f.Registration
	}
	if f.Type != nil {
		v.Type = *f.Type
	}
	if f.Make != nil {
		v.Make = *f.Make
	}
	if f.Model != nil {
		v.Model = *f.Model
	}
	if f.MaxWeightKg != nil {
		v.Capacity.MaxWeightKg = *f.MaxWeightKg
	}
	if f.LengthCm != nil {
		v.Capacity.LengthCm = *f.LengthCm
	}
	if f.WidthCm != nil {
		v.Capacity.WidthCm = *f.WidthCm
	}
	if f.HeightCm != nil {
		v.Capacity.HeightCm = *f.HeightCm
	}
	return v
}

// VehicleQuery is what a provider is asking of their own fleet.
//
// The zero value is a valid query: every vehicle they own, active and retired alike, newest first,
// one default page.
type VehicleQuery struct {
	// Active narrows the list to vehicles in service, or to those out of it. Nil means both.
	//
	// A pointer rather than a bool, because "in service" and "not filtered" are different
	// requests and a bool cannot hold three states. The fleet screen wants the active ones; the
	// customer looking at an old bid wants the vehicle whatever became of it.
	Active *bool

	// Limit is how many vehicles to return. Zero means the default, and anything above the
	// maximum is clamped rather than refused.
	Limit int

	// After is the position to continue from. The zero value starts at the newest.
	After VehicleCursor
}

// VehicleCursor is a position in a provider's fleet list.
//
// Two fields for the reason jobs' is: `created_at` is not unique — a provider adding their fleet
// in one sitting writes several rows in the same millisecond — so a cursor that could not break the
// tie would repeat or skip a vehicle at exactly the page boundary.
type VehicleCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// IsZero reports whether the query starts at the newest vehicle.
func (c VehicleCursor) IsZero() bool { return c.ID == uuid.Nil && c.CreatedAt.IsZero() }

// VehiclePage is one page of a provider's fleet.
type VehiclePage struct {
	Vehicles []Vehicle

	// Next is where the following page starts, and is the zero cursor on the last page.
	Next VehicleCursor

	// HasMore says whether asking again would return anything. Carried rather than inferred from
	// Next, because "no cursor" is also what the first request looks like.
	HasMore bool
}

// # SHIP-79 — what a provider declares about the work they take
//
// Everything from here down is the provider's own declaration rather than a fact about one
// vehicle: where they will carry freight, and what kind of freight they carry. Docs/01 §4.2 calls
// it "nominate service area and specialties" and Docs/04 §3 requires it of a verified provider.
//
// # A service area is a set of named regions, not a radius, and this is where that was settled
//
// SHIP-79 had to answer it rather than leave it for SHIP-81, because Docs/11 §9 names SHIP-81 —
// "the first ticket needing the distance between two coordinates in a domain other than jobs" — as
// the trigger that would create a neutral internal/geo package holding a Point and a haversine. A
// radius fires that trigger; a set of regions does not. **Settled: a set of named regions**, so
// SHIP-81 is a set-membership query, nothing in this domain does distance arithmetic, and
// internal/geo is still not needed. Three reasons, in the order they weighed:
//
//  1. **A radius would make eligibility depend on a field the platform may not have.** A job's
//     coordinate is best-effort by design: SHIP-60 stores the address exactly as typed when a
//     lookup fails, and Docs/11 §3 records that staging and production get no geocoder at all
//     until one is configured. A radius filter would then match *nothing* — a provider opening the
//     app to an empty feed, with no error anywhere to explain it. The state and the postcode are
//     typed by the customer and validated on the way in, so they are always present.
//  2. **It is how Australian road freight is quoted.** Providers price by postcode zone and by
//     state rather than by kilometres from a depot, and a straight-line radius is wrong about
//     roads: 300 km from Melbourne reaches Tasmania across Bass Strait.
//  3. **Both documents already read it this way.** Docs/11 §3 gave a job address four parts rather
//     than one freeform line "because the suburb, the state and the postcode are values SHIP-79
//     and SHIP-81 will compare", and the geo decision recorded that "SHIP-79 has a provider
//     declare a service area and SHIP-81 filters on it; neither names a radius in kilometres".
//
// **What would reopen it** is a ticket that genuinely needs a distance — "providers within 50 km
// of the pickup, ranked" is the shape — and the answer then is Docs/11 §9's: write internal/geo,
// narrow the geocoding port, and keep the regions as well, because a provider still has to be able
// to say "I do not cross the Nullarbor" in a form a straight line cannot express.
//
// # An empty declaration matches nothing, and that is the safe direction
//
// A provider who has declared no service area serves no region — not every region. Eligibility is
// opt-in: the alternative would make the provider who has not finished onboarding the
// widest-reaching provider on the platform, and Docs/04 §3 requires the area declared before
// verification passes. SHIP-81 enforces it; this file records the reading so there is nothing left
// to argue about.

// AreaScope is how wide one service-area entry is.
//
// Two grains today. A state is the coarse one — "anywhere in Victoria" — and a postcode is the
// fine one. There is deliberately no third: suburbs are ambiguous (there are seven Springfields),
// and local government areas would need reference data nobody maintains yet.
type AreaScope string

const (
	ScopeState    AreaScope = "state"
	ScopePostcode AreaScope = "postcode"
)

func (s AreaScope) String() string { return string(s) }

// ServiceArea is one region a provider will carry freight in.
//
// **An entry names one grain and never two.** There is no postcode qualified by a state, and that
// is not tidiness: Docs/11 §3 records SHIP-60's deliberate refusal to validate a postcode against
// its state, because the allocations have exceptions — 2600 is ACT inside the NSW range — and move
// when Australia Post says so. An entry carrying both could disagree with itself, and checking that
// it did not would be exactly the validation that decision refuses.
type ServiceArea struct {
	Scope AreaScope

	// Area is what the entry names at that scope: an abbreviated state at [ScopeState], four
	// digits at [ScopePostcode].
	//
	// Text in both cases, because a postcode is not a number — 0800 is Darwin, and an integer
	// would store it as 800.
	Area string
}

// ServiceAreaStates is the eight states and territories, in the abbreviated form Australia Post
// uses, sorted the way ck_provider_service_areas_scope_and_area lists them.
//
// **A second copy of a list `jobs` also holds, and the boundary rule is why.** Domains do not
// import each other (Docs/06 §4.1), so fleet cannot reach jobs.States to compare a service area
// against a job's address — and a shared package for eight strings that have not changed since
// 1975 would be a registration in internal/boundaries for no benefit. What keeps the copies honest
// is that each is paired with its own CHECK constraint by a test (Docs/10 §3.4): if either list
// moves, its own test fails.
var ServiceAreaStates = []string{"ACT", "NSW", "NT", "QLD", "SA", "TAS", "VIC", "WA"}

// longStateNames maps the spelled-out form to the abbreviation.
//
// Accepted on input for the reason `jobs` accepts it on an address: a provider typing "Victoria"
// means Victoria, and refusing it teaches nobody anything. Tolerant on input and exact on output,
// which is the asymmetry every value in this domain takes.
var longStateNames = map[string]string{
	"australian capital territory": "ACT",
	"new south wales":              "NSW",
	"northern territory":           "NT",
	"queensland":                   "QLD",
	"south australia":              "SA",
	"tasmania":                     "TAS",
	"victoria":                     "VIC",
	"western australia":            "WA",
}

// Specialty is a kind of work a provider takes.
//
// # This is the vocabulary 000300 and 000404 were holding for SHIP-79
//
// [VehicleType] answers what a *vehicle* is; this answers what a *provider* does, which is a
// different question with a different owner. A refrigerated truck is a fact about the truck; a
// provider who runs one and will not do interstate work is a fact about the business.
//
// # A declaration, and never a permission
//
// Naming [SpecialtyDangerousGoods] claims a capability; it grants nothing. What a provider is
// licensed and insured to carry is verification's business (Docs/04 §3), and what may not be
// carried at all is X-9's prohibited-goods list. A row here is what a customer reads when comparing
// bids — Docs/01 §4.3's "declared capability" — and what a later filter may narrow a feed by.
//
// Twelve values, closed, ordered from the work most providers do to the work few are equipped for.
// Adding a thirteenth is a migration plus a constant plus a line in the contract, which is the
// right amount of friction for a list three clients render — the same trade [VehicleTypes] takes.
//
// The stored form is the wire form, lower snake case per Docs/10 §4.7. Unlike a job status there is
// no document behind this list, so a second spelling would be a mapping with nothing on the other
// end.
type Specialty string

const (
	SpecialtyGeneralFreight        Specialty = "general_freight"
	SpecialtyCourier               Specialty = "courier"
	SpecialtyFurnitureRemovals     Specialty = "furniture_removals"
	SpecialtyRefrigerated          Specialty = "refrigerated"
	SpecialtyFragileAndHighValue   Specialty = "fragile_and_high_value"
	SpecialtyOversized             Specialty = "oversized"
	SpecialtyHeavyHaulage          Specialty = "heavy_haulage"
	SpecialtyConstructionMaterials Specialty = "construction_materials"
	SpecialtyBulkMaterials         Specialty = "bulk_materials"
	SpecialtyVehicleTransport      Specialty = "vehicle_transport"
	SpecialtyLivestock             Specialty = "livestock"
	SpecialtyDangerousGoods        Specialty = "dangerous_goods"
)

// Specialties is every specialty, in the order a client renders them.
//
// Ordered rather than a set, for the reason [VehicleTypes] is: a picker showing them alphabetically
// would put dangerous goods first and general freight ninth, which reads as an accident. It is also
// the order a stored declaration is returned in, so two providers with the same specialties present
// them identically.
var Specialties = []Specialty{
	SpecialtyGeneralFreight,
	SpecialtyCourier,
	SpecialtyFurnitureRemovals,
	SpecialtyRefrigerated,
	SpecialtyFragileAndHighValue,
	SpecialtyOversized,
	SpecialtyHeavyHaulage,
	SpecialtyConstructionMaterials,
	SpecialtyBulkMaterials,
	SpecialtyVehicleTransport,
	SpecialtyLivestock,
	SpecialtyDangerousGoods,
}

// Valid reports whether s is one of the twelve.
func (s Specialty) Valid() bool {
	for _, known := range Specialties {
		if s == known {
			return true
		}
	}
	return false
}

func (s Specialty) String() string { return string(s) }

// normalise tolerates case and surrounding space on the way in, the way [VehicleType.normalise]
// does and for the same reason.
func (s Specialty) normalise() Specialty {
	return Specialty(strings.ToLower(strings.TrimSpace(string(s))))
}

// Profile is what one provider has declared.
//
// **The zero value is a real answer.** A provider who has declared nothing has an empty profile,
// not a missing one, which is why reading it never reports [ErrVehicleNotFound]'s equivalent and
// why there is no parent row in the schema for it to be the absence of.
type Profile struct {
	ProviderID uuid.UUID

	// Areas is every region the provider will carry freight in, states first and each group in
	// ascending order.
	Areas []ServiceArea

	// Specialties is what they carry, in [Specialties] order rather than the order they were sent.
	Specialties []Specialty
}

// Serves reports whether the profile covers an address in this state and postcode.
//
// The membership question SHIP-81 asks, answered on a value that has already been read. It is here
// rather than in the store because it is a property of the declaration rather than of the query
// that fetched it — and because a caller holding a profile should not have to go back to the
// database to ask.
//
// Either argument may be empty, which is how a caller asks about the half it has.
func (p Profile) Serves(state, postcode string) bool {
	for _, area := range p.Areas {
		switch area.Scope {
		case ScopeState:
			if state != "" && area.Area == state {
				return true
			}
		case ScopePostcode:
			if postcode != "" && area.Area == postcode {
				return true
			}
		}
	}
	return false
}

// ProfileFields is a partial declaration, used by the one writing verb.
//
// Every field is a pointer to a slice, so absent, null and an explicit empty list are three
// distinct things — the same distinction [VehicleFields] draws, applied to a set:
//
//	omitted or null   leave that list exactly as it is
//	an empty list     clear it
//
// The second is not decoration. A provider who stated a postcode and later withdrew from it has to
// be able to say so, and without an empty list "I no longer serve any single postcode, only whole
// states" would be inexpressible.
//
// A list that *is* supplied replaces its predecessor whole rather than being merged into it. There
// is no add-one or remove-one verb, deliberately: the client renders the declaration as a set of
// chips and sends the set back, and two verbs operating on one collection is where a client and a
// server stop agreeing about what is in it.
type ProfileFields struct {
	// States and Postcodes are the two grains of the service area, and they move independently:
	// naming one leaves the other alone. Grouping them into one field would mean a provider
	// adding a postcode had to resend every state.
	States    *[]string
	Postcodes *[]string

	Specialties *[]Specialty
}

// IsEmpty reports whether the caller named no list at all.
func (f ProfileFields) IsEmpty() bool {
	return f.States == nil && f.Postcodes == nil && f.Specialties == nil
}
