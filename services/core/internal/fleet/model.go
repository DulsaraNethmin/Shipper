package fleet

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// VehicleType is what kind of vehicle a fleet entry is.
//
// # This is not the capability vocabulary SHIP-79 owns, and the distinction is worth keeping
//
// `jobs.vehicle_requirement` is free text — "ute with a tailgate lifter" — and 000404 says the
// vocabulary it will one day be validated against belongs to this domain. That vocabulary is
// SHIP-79's: service area, specialties, the things a *provider* declares about the work they
// take. This list is narrower and answers a different question — what is this vehicle — which
// cannot be deferred, because a fleet record that does not say what the vehicle is describes
// nothing. Nothing in `jobs` is validated against these values and nothing here reads that column.
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
