package fleet

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/pagination"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// The fleet a provider maintains: adding a vehicle, editing it, and taking it out of service
// (SHIP-78).
//
// # This domain emits no events, and that is a decision rather than an omission
//
// `jobs` emits one on every transition because a job's status is the thing the whole platform
// reacts to. A vehicle has no lifecycle of that kind: nothing downstream is waiting to hear that a
// provider bought a van. SHIP-81's eligibility filter reads this table directly rather than a
// projection built from a stream, and SHIP-89's bid names a vehicle by id at the moment it is
// placed — so an event today would have no consumer, and an event with no consumer is a shape
// somebody later has to either keep or break.
//
// That is also why there is no ports.go: this is the first domain that needs nothing of any other
// domain and nothing of any adapter. Verification is checked against the *provider* rather than the
// vehicle and belongs to `profiles` (SHIP-84 onwards); SHIP-81 is where fleet first needs to ask
// another domain a question, and it declares the interface then.

// The validation limits for a vehicle's own fields.
//
// Bounds against abuse and against a client with a runaway text field, not a judgement about what
// a good fleet looks like. Constants for the reason the draft limits in `jobs` are: these do not
// move under operational pressure. The one that will — which vehicle types this marketplace
// accepts — is the CHECK constraint plus [VehicleTypes], and moving it is a migration on purpose.
const (
	// Twelve characters. The longest Australian plate is six or seven; the slack is for
	// personalised plates and for the states that print a leading letter group.
	maxRegistration = 12

	// A make and a model are short phrases. "Mercedes-Benz" and "Sprinter 314 CDI MWB" both fit
	// comfortably.
	maxMakeModel = 60

	// 100 tonnes and 30 metres. **Deliberately the same numbers `jobs` uses for a load**
	// (maxWeightKg, maxDimensionCm): a vehicle whose stated capacity exceeds the largest job the
	// marketplace will accept is describing a slipped decimal point rather than a truck.
	maxCapacityWeightKg    = 100_000
	maxCapacityDimensionCm = 3000
)

// Service holds this domain's rules.
//
// It owns no connection. Every method takes a db.Runner, because the transaction belongs to
// whoever owns the invariant being protected (Docs/10 §3.2) — and here that is this domain: an
// edit is a read, an ownership check and a write against one version of the row.
type Service struct {
	clock clock.Clock
	store postgresStore
}

// NewService builds the domain service.
//
// The clock is required and it panics without one, in the same spirit as httpx.RegisterCode and
// jobs.NewService: this is called once from the composition root, a missing collaborator is a
// programming mistake rather than a runtime condition, and the alternative is a service that starts
// and then records every deactivation at the zero time.
func NewService(c clock.Clock) *Service {
	if c == nil {
		panic("fleet: NewService needs a clock (Docs/10 §6.3)")
	}
	return &Service{clock: c}
}

// Add puts a vehicle into the calling provider's fleet (SHIP-78).
//
// **The owner is whoever the token says is calling.** There is no field in [VehicleFields] for
// naming a provider, because that would be an authorisation decision made from client input, which
// Docs/07 §3 puts on the platform.
//
// Registration and type are required and everything else is optional. That split is Docs/01 §4.2's
// acceptance measure read literally — "a verified provider can maintain a fleet" — a provider
// standing in a truck yard has the plate to hand and may not know the load height, and a form that
// insisted would be filled in with guesses.
//
// A plate the provider already has in service is [ErrDuplicateRegistration], which comes from
// uq_vehicles_provider_registration rather than from a SELECT before the INSERT: two requests
// racing would both find nothing and both write, and the index is the only thing that can be right
// about that.
func (s *Service) Add(ctx context.Context, r db.Runner, providerID uuid.UUID, f VehicleFields) (Vehicle, error) {
	if providerID == uuid.Nil {
		return Vehicle{}, fmt.Errorf("fleet: a vehicle names no provider: %w", ErrNotProvider)
	}

	fields := f.normalise()
	problems := fields.problems(true)
	if err := problems.Err(); err != nil {
		return Vehicle{}, err
	}

	provider, err := s.store.isProvider(ctx, r, providerID)
	if err != nil {
		return Vehicle{}, err
	}
	if !provider {
		return Vehicle{}, fmt.Errorf("fleet: %s: %w", providerID, ErrNotProvider)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Vehicle{}, fmt.Errorf("fleet: generating a vehicle id: %w", err)
	}

	return s.store.insert(ctx, r, fields.applyTo(Vehicle{ID: id, ProviderID: providerID}))
}

// Update applies a partial edit to a vehicle the caller owns (SHIP-78).
//
// Two refusals, in this order, and the order is the point:
//
//  1. a vehicle that does not exist is [ErrVehicleNotFound];
//  2. a vehicle belonging to somebody else is [ErrNotVehicleOwner].
//
// Both are the same 404 on the wire. They are kept apart in Go so a test asserting "the stranger
// was refused" fails if the vehicle silently stopped existing instead.
//
// **A deactivated vehicle can still be edited.** Correcting the plate on a truck that is off the
// road for a month is an ordinary thing to want, and refusing it would push the provider towards
// creating a second row for the same vehicle — which is the duplication deactivation exists to
// avoid. What an edit cannot do is bring it back into service; [Service.Reactivate] is that, and it
// is a separate intent because it is the one that can collide with another live plate.
//
// r must be a transaction. The read, the ownership check and the write are one decision against one
// version of the row, and lockVehicle's FOR UPDATE only holds for the length of a transaction —
// outside one the lock is released the instant the SELECT returns, and two concurrent edits can
// interleave into a vehicle carrying half of each.
func (s *Service) Update(ctx context.Context, r db.Runner, providerID, vehicleID uuid.UUID, f VehicleFields) (Vehicle, error) {
	if _, inTx := r.(pgx.Tx); !inTx {
		return Vehicle{}, fmt.Errorf("fleet: editing %s: %w", vehicleID, ErrNotInTransaction)
	}
	if f.IsEmpty() {
		return Vehicle{}, fmt.Errorf("fleet: editing %s: %w", vehicleID, ErrNothingToUpdate)
	}

	fields := f.normalise()
	problems := fields.problems(false)
	if err := problems.Err(); err != nil {
		return Vehicle{}, err
	}

	vehicle, err := s.owned(ctx, r, providerID, vehicleID)
	if err != nil {
		return Vehicle{}, err
	}

	return s.store.update(ctx, r, fields.applyTo(vehicle))
}

// Deactivate takes a vehicle out of service (SHIP-78).
//
// **Not a delete, and there is no delete.** Docs/01 §4.2 gives the provider "add, edit, deactivate"
// and stops there deliberately: a vehicle is named by the bid that won a job and by the delivery
// that followed it, so removing the row would remove part of a commercial record Docs/05 §3.1
// requires retaining. What deactivation does is stop the vehicle making its provider eligible for
// new work; deliveries already under way are undisturbed, which is the rule this domain's doc.go
// states.
//
// Deactivating an already-deactivated vehicle answers with the vehicle and records nothing further.
// The caller asked for an outcome that holds — the same reading `jobs` gives a repeated
// cancellation, and the reason a retry that generated a fresh idempotency key is absorbed rather
// than refused.
func (s *Service) Deactivate(ctx context.Context, r db.Runner, providerID, vehicleID uuid.UUID) (Vehicle, error) {
	if _, inTx := r.(pgx.Tx); !inTx {
		return Vehicle{}, fmt.Errorf("fleet: deactivating %s: %w", vehicleID, ErrNotInTransaction)
	}

	vehicle, err := s.owned(ctx, r, providerID, vehicleID)
	if err != nil {
		return Vehicle{}, err
	}
	if !vehicle.Active() {
		return vehicle, nil
	}

	return s.store.setDeactivatedAt(ctx, r, vehicle.ID, s.clock.Now().UTC())
}

// Reactivate brings a vehicle back into service (SHIP-78).
//
// The counterpart of [Service.Deactivate] rather than a field on an edit, for the reason `jobs`
// makes cancellation a verb: the client names an intent and the platform decides what the state
// becomes. It is also the operation that can fail for a reason an edit cannot — the provider may
// have added a replacement on the same plate meanwhile, and
// uq_vehicles_provider_registration refuses the second live row. That answers
// [ErrDuplicateRegistration], which is a different conversation with the provider from "your edit
// was invalid".
//
// A vehicle that is already in service answers with the vehicle, unchanged.
func (s *Service) Reactivate(ctx context.Context, r db.Runner, providerID, vehicleID uuid.UUID) (Vehicle, error) {
	if _, inTx := r.(pgx.Tx); !inTx {
		return Vehicle{}, fmt.Errorf("fleet: reactivating %s: %w", vehicleID, ErrNotInTransaction)
	}

	vehicle, err := s.owned(ctx, r, providerID, vehicleID)
	if err != nil {
		return Vehicle{}, err
	}
	if vehicle.Active() {
		return vehicle, nil
	}

	return s.store.clearDeactivatedAt(ctx, r, vehicle.ID)
}

// Vehicle is one vehicle, for the provider who owns it.
//
// Ownership is checked here rather than in the query's WHERE clause, deliberately: a
// `WHERE id = $1 AND provider_id = $2` that returns nothing cannot say whether the vehicle is
// somebody else's or nobody's, and the two are different defects even though they are one answer on
// the wire.
//
// No lock and no transaction. This is a read, and [postgresStore.vehicle] takes the row without FOR
// UPDATE for the reason recorded there.
func (s *Service) Vehicle(ctx context.Context, r db.Runner, providerID, vehicleID uuid.UUID) (Vehicle, error) {
	return s.owned(ctx, r, providerID, vehicleID)
}

// Vehicles is the provider's own fleet, newest first (SHIP-78).
//
// **A provider sees only their own vehicles, and there is no argument for anything else.** The
// provider id comes from the token rather than from the request, so there is no parameter to widen
// and no filter to forget: a vehicle belonging to somebody else is not refused here, it is never
// selected.
//
// Keyset rather than offset, per Docs/10 §4.5, and through internal/pagination rather than a
// page-size constant of this domain's own — every list endpoint needs the same answer and the
// bounds come from configuration (SHIP-15g).
func (s *Service) Vehicles(ctx context.Context, r db.Runner, providerID uuid.UUID, q VehicleQuery) (VehiclePage, error) {
	if providerID == uuid.Nil {
		return VehiclePage{}, fmt.Errorf("fleet: a fleet list names no provider: %w", ErrNotProvider)
	}

	limit := q.Limit
	switch {
	case limit <= 0:
		limit = pagination.DefaultLimit
	case limit > pagination.MaxLimit:
		limit = pagination.MaxLimit
	}

	// One more row than was asked for, which answers "is there another page" without a second
	// query and without counting the whole set. The extra is dropped below and never reaches a
	// caller.
	found, err := s.store.vehiclesFor(ctx, r, providerID, q.Active, q.After, limit+1)
	if err != nil {
		return VehiclePage{}, err
	}

	page := VehiclePage{Vehicles: found}
	if len(found) > limit {
		page.Vehicles = found[:limit]

		last := page.Vehicles[limit-1]
		page.Next = VehicleCursor{CreatedAt: last.CreatedAt, ID: last.ID}
		page.HasMore = true
	}
	return page, nil
}

// owned reads a vehicle and refuses one belonging to another provider.
//
// One function rather than the same four lines in five methods, because the alternative is five
// places for the ownership check to be forgotten — and a forgotten one here is another provider's
// fleet, not a cosmetic defect. It takes the lock when it is inside a transaction and does not when
// it is not, which is the distinction [postgresStore.lockVehicle] exists to carry.
func (s *Service) owned(ctx context.Context, r db.Runner, providerID, vehicleID uuid.UUID) (Vehicle, error) {
	read := s.store.vehicle
	if _, inTx := r.(pgx.Tx); inTx {
		read = s.store.lockVehicle
	}

	vehicle, err := read(ctx, r, vehicleID)
	if err != nil {
		return Vehicle{}, err
	}
	if vehicle.ProviderID != providerID {
		return Vehicle{}, fmt.Errorf("fleet: %s does not belong to %s: %w",
			vehicleID, providerID, ErrNotVehicleOwner)
	}
	return vehicle, nil
}

// normalise tidies every supplied field, returning a copy.
//
// Done once, before validation, so that what is validated is exactly what will be stored. The
// alternative — validating the raw input and normalising on the way to the database — is how a
// value passes a length check and then fails a constraint, or passes as "  van " and is stored as
// something ck_vehicles_type has never seen.
func (f VehicleFields) normalise() VehicleFields {
	out := f

	if f.Registration != nil {
		tidy := plate(*f.Registration)
		out.Registration = &tidy
	}
	if f.Type != nil {
		tidy := f.Type.normalise()
		out.Type = &tidy
	}

	// Collapsed rather than merely trimmed: both are phrases on a single line, and "Mercedes  -
	// Benz" typed on a phone should not be a different make from "Mercedes-Benz".
	if f.Make != nil {
		tidy := collapse(*f.Make)
		out.Make = &tidy
	}
	if f.Model != nil {
		tidy := collapse(*f.Model)
		out.Model = &tidy
	}

	return out
}

// plate is the stored form of a registration: upper case, with every space removed.
//
// Removing the spaces rather than collapsing them is the decision worth stating. A plate is written
// both ways — "ABC 123" on the vehicle and "ABC123" on the paperwork — and a provider who typed one
// then the other means one truck. Keeping the space would let uq_vehicles_provider_registration see
// two live vehicles where there is one, which is precisely the duplicate that index exists to
// refuse.
func plate(raw string) string {
	return strings.ToUpper(strings.Join(strings.Fields(raw), ""))
}

// collapse trims a value and reduces every run of whitespace inside it to one space.
func collapse(raw string) string {
	return strings.Join(strings.Fields(raw), " ")
}

// problems reports everything wrong with the supplied fields at once.
//
// All of it, not the first failure: a provider filling in a form should be shown every field that
// needs attention in one answer rather than discovering them one request at a time (Docs/10 §4.6).
//
// creating says whether the two identifying fields must be present. They are required when a
// vehicle is added and optional when one is edited — but never *clearable*, which is why an empty
// value is refused on both paths: a row with a blank plate identifies nothing and a row with no type
// describes nothing, and neither is a state a provider can have meant to reach.
//
// A field the caller did not supply is not validated, which is what makes this usable by both
// verbs. Zero is not a failure either — a non-nil pointer to zero is how a provider clears a
// capacity they stated earlier, and the columns refuse zero on their own, so it never reaches the
// database as a value.
func (f VehicleFields) problems(creating bool) validate.Errors {
	var e validate.Errors

	switch {
	case f.Registration == nil:
		if creating {
			e.Add("registration", validate.CodeRequired, "Enter the vehicle's registration.")
		}
	case strings.TrimSpace(*f.Registration) == "":
		e.Add("registration", validate.CodeRequired, "Enter the vehicle's registration.")
	default:
		e.Length("registration", *f.Registration, 0, maxRegistration)
	}

	switch {
	case f.Type == nil:
		if creating {
			e.Add("vehicle_type", validate.CodeRequired, "Choose what kind of vehicle this is.")
		}
	case *f.Type == "":
		e.Add("vehicle_type", validate.CodeRequired, "Choose what kind of vehicle this is.")
	case !f.Type.Valid():
		// The valid values are not listed in the message. There are eleven, the contract
		// publishes them, and a message that enumerates them is a twelfth copy of the list to
		// keep in step (Docs/10 §3.4).
		e.Add("vehicle_type", validate.CodeNotAllowed,
			"This is not a kind of vehicle this platform carries freight with.")
	}

	if f.Make != nil {
		e.Length("make", *f.Make, 0, maxMakeModel)
	}
	if f.Model != nil {
		e.Length("model", *f.Model, 0, maxMakeModel)
	}

	if f.MaxWeightKg != nil && (*f.MaxWeightKg < 0 || *f.MaxWeightKg > maxCapacityWeightKg) {
		e.Add("max_weight_kg", validate.CodeOutOfRange,
			"Enter a capacity between 0 and %d kilograms.", maxCapacityWeightKg)
	}

	dimension(&e, "load_length_cm", f.LengthCm)
	dimension(&e, "load_width_cm", f.WidthCm)
	dimension(&e, "load_height_cm", f.HeightCm)

	return e
}

func dimension(e *validate.Errors, field string, value *int) {
	if value == nil {
		return
	}
	if *value < 0 || *value > maxCapacityDimensionCm {
		e.Add(field, validate.CodeOutOfRange,
			"Enter a measurement between 0 and %d centimetres.", maxCapacityDimensionCm)
	}
}
