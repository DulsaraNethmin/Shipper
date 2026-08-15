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

	// A postcode is four digits. Not a length bound but the whole format, because unlike a
	// registration plate this one is fixed: Australia Post allocates four digits and has since
	// 1967.
	postcodeDigits = 4

	// A service area of more than a thousand postcodes is describing a state.
	//
	// Australia has roughly 2,600 allocated postcodes and greater Sydney about 600, so this is
	// generous for any real declaration — it is a bound against a client with a runaway list
	// rather than a judgement about how much of the country a provider may cover. The provider who
	// genuinely serves everywhere says so in eight state entries instead.
	maxPostcodes = 1000

	// A trading name has to fit on a comparison screen beside a price (SHIP-79a).
	//
	// **The same 80 as `ck_provider_profiles_display_name`, and the pair is tested.** Docs/10 §3.1
	// puts coherence in the database and policy in the domain, and this is the rare value that is
	// both: "not a paragraph" is coherence, "fits a row on a phone" is policy, and they agree at
	// 80. The floor is 2 because a one-character trading name is a typo far more often than it is
	// a business.
	minDisplayName = 2
	maxDisplayName = 80
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

	if err := s.mustBeProvider(ctx, r, providerID); err != nil {
		return Vehicle{}, err
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

	if err := s.mustBeProvider(ctx, r, providerID); err != nil {
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
	if err := s.mustBeProvider(ctx, r, providerID); err != nil {
		return Vehicle{}, err
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
	if err := s.mustBeProvider(ctx, r, providerID); err != nil {
		return Vehicle{}, err
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
//
// **A caller who is not a provider is refused before the vehicle is read** (SHIP-78a). It changes
// nothing a customer could previously see — a vehicle is never theirs, so they got the same 404 —
// and it changes what they are told, from "no such vehicle" to "only a provider account can keep a
// fleet". See [Service.mustBeProvider].
func (s *Service) Vehicle(ctx context.Context, r db.Runner, providerID, vehicleID uuid.UUID) (Vehicle, error) {
	if err := s.mustBeProvider(ctx, r, providerID); err != nil {
		return Vehicle{}, err
	}
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
	if err := s.mustBeProvider(ctx, r, providerID); err != nil {
		return VehiclePage{}, err
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

// Profile is what the provider has declared about the work they take (SHIP-79).
//
// **An undeclared profile is an empty one rather than a missing one**, so this never reports "no
// such thing": there is no parent row in the schema, and "I have not nominated a service area yet"
// is a truthful answer with the same shape as any other.
//
// **A caller who is not a provider is refused, and SHIP-78a reversed this method's position on
// that.** Until then it argued the other way, and the argument is worth recording rather than
// deleting: a customer's declaration is empty because they have never made one, so a 403 disclosed
// nothing a 200 did not, and it made the client special-case a screen it never shows.
//
// What that reasoning left out is the one Docs/07 §3 makes. "The app may hide or disable; the
// platform decides" is only true if the platform *does* decide, and a surface that answers a
// customer plausibly has decided nothing — it has relied on the client to keep them away, which is
// SHIP-98 and is a presentation choice. The empty profile is also not the harmless answer it looks
// like next to [Service.Declare], which refuses the same caller: a read that succeeds where the
// write refuses is a screen that renders and then fails at the save button.
//
// No transaction: two SELECTs against one provider's rows, neither of which the other can
// invalidate in a way that matters — a declaration is replaced under an advisory lock, so the worst
// a concurrent write can do is put this read on one side of it or the other.
func (s *Service) Profile(ctx context.Context, r db.Runner, providerID uuid.UUID) (Profile, error) {
	if err := s.mustBeProvider(ctx, r, providerID); err != nil {
		return Profile{}, err
	}
	return s.store.profile(ctx, r, providerID)
}

// Declare replaces the parts of the provider's declaration the caller named (SHIP-79).
//
// **The owner is whoever the token says is calling**, as everywhere else in this domain: there is
// no field for naming a provider, because that would be an authorisation decision made from client
// input (Docs/07 §3).
//
// Each of the three lists moves independently. One that was not named is left exactly as it is; one
// that was is replaced whole, including by the empty list, which is how a provider withdraws from a
// region. See [ProfileFields] for why replacement rather than add-one and remove-one.
//
// **What is replaced is the set, not the rows.** The store removes only what is no longer in the
// declaration and inserts only what is new, so an entry the provider has held since January keeps
// its created_at through a request that merely resends it.
//
// r must be a transaction, and for a sharper reason than [Service.Update]'s. Replacing a set is a
// delete and an insert that must be one decision: two requests arriving together in READ COMMITTED
// would each fail to see the other's uncommitted rows, and the declaration that resulted would be
// the union of two sets rather than either of them. [postgresStore.lockProfile] is what serialises
// them, and an advisory lock only lasts as long as the transaction that took it.
func (s *Service) Declare(ctx context.Context, r db.Runner, providerID uuid.UUID, f ProfileFields) (Profile, error) {
	if _, inTx := r.(pgx.Tx); !inTx {
		return Profile{}, fmt.Errorf("fleet: declaring for %s: %w", providerID, ErrNotInTransaction)
	}
	if providerID == uuid.Nil {
		return Profile{}, fmt.Errorf("fleet: a declaration names no provider: %w", ErrNotProvider)
	}
	if f.IsEmpty() {
		return Profile{}, fmt.Errorf("fleet: declaring for %s: %w", providerID, ErrNothingToUpdate)
	}

	fields := f.normalise()
	problems := fields.problems()
	if err := problems.Err(); err != nil {
		return Profile{}, err
	}

	if err := s.mustBeProvider(ctx, r, providerID); err != nil {
		return Profile{}, err
	}

	if err := s.store.lockProfile(ctx, r, providerID); err != nil {
		return Profile{}, err
	}

	if fields.States != nil {
		if err := s.store.replaceAreas(ctx, r, providerID, ScopeState, *fields.States); err != nil {
			return Profile{}, err
		}
	}
	if fields.Postcodes != nil {
		if err := s.store.replaceAreas(ctx, r, providerID, ScopePostcode, *fields.Postcodes); err != nil {
			return Profile{}, err
		}
	}
	if fields.Specialties != nil {
		if err := s.store.replaceSpecialties(ctx, r, providerID, *fields.Specialties); err != nil {
			return Profile{}, err
		}
	}

	// The public half (SHIP-79a), under the same lock and in the same transaction as the sets
	// above. A provider editing only their service area writes nothing here — see
	// [ProfileFields.declaresPublic] for why that matters more than it looks.
	if fields.declaresPublic() {
		// **One read decides two things, and it is free because the lock is already held.**
		//
		// 000303 makes both columns NOT NULL, so a provider who has named one field and never
		// named the other has no legal row. Left to the store that is a constraint violation
		// reaching the client as a 500 — an error about the database standing in for a rule about
		// the request — so it is refused here as `required` on the field they left out.
		//
		// The same read chooses the statement. An `INSERT … ON CONFLICT DO UPDATE` would have
		// needed no branch and does not work: PostgreSQL checks the proposed row *before* it
		// arbitrates the conflict, so the placeholder standing in for "not named" would have to
		// satisfy `ck_provider_profiles_display_name`. See [postgresStore.insertPublicProfile].
		existing, err := s.store.publicProfile(ctx, r, providerID)
		if err != nil {
			return Profile{}, err
		}

		switch {
		case existing.Declared():
			if err := s.store.updatePublicProfile(ctx, r, providerID, fields.DisplayName, fields.OperatesAs); err != nil {
				return Profile{}, err
			}
		case fields.DisplayName == nil || fields.OperatesAs == nil:
			var e validate.Errors
			if fields.DisplayName == nil {
				e.Add("display_name", validate.CodeRequired,
					"Tell customers who they are dealing with.")
			}
			if fields.OperatesAs == nil {
				e.Add("operates_as", validate.CodeRequired,
					"Say whether you operate as an individual or as a business.")
			}
			return Profile{}, e.Err()
		default:
			if err := s.store.insertPublicProfile(ctx, r, providerID, *fields.DisplayName, *fields.OperatesAs); err != nil {
				return Profile{}, err
			}
		}
	}

	return s.store.profile(ctx, r, providerID)
}

// PublicProfiles is who each of these providers trades as (SHIP-79a).
//
// **The batch read `cmd/api/routes_bidding.go` recorded as missing**, written here now that a
// customer-facing disclosure needs it: that file's note says a page of offers cannot use
// `fleet.Service.Vehicle` because "a page of a hundred offers would be a hundred round trips", and
// the same is true of anything read one provider at a time. One statement serves a whole page.
//
// # This method is the disclosure boundary, and that is the point of it
//
// It returns [PublicProfile] and nothing else — no service area, no specialties, no vehicle. A
// caller assembling a customer-facing summary of a provider cannot reach what SHIP-102a's *Done
// when* forbids, because the only thing it was handed is the set that may be shown. Reading the
// whole [Profile] and copying two fields out of it would be correct only for as long as somebody
// kept copying two.
//
// A provider with nothing declared is absent from the map rather than present with empty strings.
// The caller renders what it has, because an offer must not disappear from a comparison screen
// because its provider has not finished their profile.
//
// The nil provider is skipped rather than refused: this is a decoration on a page that has already
// been authorised, and a caller assembling one from rows it has already read should not have a
// screen fail over an identifier that cannot match anything anyway.
func (s *Service) PublicProfiles(ctx context.Context, r db.Runner, providerIDs []uuid.UUID) (map[uuid.UUID]PublicProfile, error) {
	wanted := make([]uuid.UUID, 0, len(providerIDs))
	seen := make(map[uuid.UUID]bool, len(providerIDs))

	for _, id := range providerIDs {
		if id == uuid.Nil || seen[id] {
			continue
		}
		seen[id] = true
		wanted = append(wanted, id)
	}
	return s.store.publicProfiles(ctx, r, wanted)
}

// normalise tidies and deduplicates every supplied list, returning a copy.
//
// Done before validation for the reason [VehicleFields.normalise] is: what is validated is then
// exactly what will be stored, rather than a raw form that passes a check and a tidy form that
// fails a constraint.
//
// **Deduplication is part of normalising rather than a refusal.** A declaration is a set, so
// `["NSW", "nsw"]` names New South Wales once and saying so twice is not a mistake worth reporting
// — the client rendered a chip twice, and refusing the request would leave the provider unable to
// see which. The unique index makes the same guarantee against two requests racing; this makes it
// against one request contradicting itself.
//
// An entry that cannot be recognised is left as it arrived rather than dropped, so that
// [ProfileFields.problems] can report which position was wrong instead of silently declaring a
// smaller set than the provider asked for.
func (f ProfileFields) normalise() ProfileFields {
	out := f

	// The display name is collapsed rather than merely trimmed, the treatment a make and a model
	// already get: "Smith   Removals" and "Smith Removals" are one business, and a name whose
	// internal spacing survived would sort and compare as a different one.
	if f.DisplayName != nil {
		name := collapse(*f.DisplayName)
		out.DisplayName = &name
	}
	if f.OperatesAs != nil {
		form := string(OperatingForm(*f.OperatesAs).normalise())
		out.OperatesAs = &form
	}

	if f.States != nil {
		out.States = tidied(*f.States, normaliseState)
	}
	if f.Postcodes != nil {
		out.Postcodes = tidied(*f.Postcodes, func(raw string) string {
			// Every space removed rather than collapsed, the same treatment a registration
			// gets and for the same reason: "3 000" and "3000" are one postcode.
			return strings.Join(strings.Fields(raw), "")
		})
	}
	if f.Specialties != nil {
		out.Specialties = tidied(*f.Specialties, Specialty.normalise)
	}

	return out
}

// tidied applies a normaliser to every entry and removes the repeats, preserving the order the
// caller sent — which is what makes the positions in a field error still point at the right chip.
func tidied[T comparable](raw []T, normalise func(T) T) *[]T {
	out := make([]T, 0, len(raw))
	seen := make(map[T]bool, len(raw))

	for _, entry := range raw {
		tidy := normalise(entry)
		if seen[tidy] {
			continue
		}
		seen[tidy] = true
		out = append(out, tidy)
	}
	return &out
}

// normaliseState resolves any form of an Australian state to its abbreviation.
//
// Unrecognised input comes back collapsed rather than empty, so the validator reports a state it
// does not know rather than a state that is missing — two different things to say to a provider.
func normaliseState(raw string) string {
	tidy := collapse(raw)
	if tidy == "" {
		return ""
	}

	if upper := strings.ToUpper(tidy); validState(upper) {
		return upper
	}
	if abbreviation, known := longStateNames[strings.ToLower(tidy)]; known {
		return abbreviation
	}
	return tidy
}

func validState(abbreviation string) bool {
	for _, known := range ServiceAreaStates {
		if abbreviation == known {
			return true
		}
	}
	return false
}

// problems reports everything wrong with a declaration at once (Docs/10 §4.6).
//
// **The field paths carry the position**, `service_area.states.2` rather than `service_area.states`,
// because the client renders each entry as its own control and an error naming only the list leaves
// the provider to work out which of forty postcodes is the wrong one. It is an extension of the
// dotted path httpx.FieldError describes rather than a new convention: the same walk into the JSON
// the client sent, through an array index.
//
// The offending value is deliberately not echoed into the message. It came from the client, which
// still has it, and a message that repeated it would put unbounded caller-supplied text into a
// response body for no benefit.
func (f ProfileFields) problems() validate.Errors {
	var e validate.Errors

	if f.States != nil {
		states := *f.States
		// Checked before the entries are, so a runaway list produces one error rather than
		// thousands. There are eight; after deduplication a longer list cannot be anything else.
		if len(states) > len(ServiceAreaStates) {
			e.Add("service_area.states", validate.CodeTooLong,
				"There are only %d states and territories.", len(ServiceAreaStates))
		} else {
			for i, state := range states {
				if !validState(state) {
					e.Add(fmt.Sprintf("service_area.states.%d", i), validate.CodeNotAllowed,
						"Choose an Australian state or territory.")
				}
			}
		}
	}

	if f.Postcodes != nil {
		postcodes := *f.Postcodes
		if len(postcodes) > maxPostcodes {
			e.Add("service_area.postcodes", validate.CodeTooLong,
				"Name at most %d postcodes. A wider area than that is a state.", maxPostcodes)
		} else {
			for i, postcode := range postcodes {
				if !isPostcode(postcode) {
					e.Add(fmt.Sprintf("service_area.postcodes.%d", i), validate.CodeInvalid,
						"Enter a %d-digit Australian postcode.", postcodeDigits)
				}
			}
		}
	}

	f.publicProblems(&e)

	if f.Specialties != nil {
		specialties := *f.Specialties
		if len(specialties) > len(Specialties) {
			e.Add("specialties", validate.CodeTooLong,
				"There are only %d specialties.", len(Specialties))
		} else {
			for i, specialty := range specialties {
				if !specialty.Valid() {
					// The twelve are not listed in the message, for the reason the vehicle
					// type's refusal does not list the eleven: the contract publishes them, and
					// a message that enumerated them is another copy to keep in step.
					e.Add(fmt.Sprintf("specialties.%d", i), validate.CodeNotAllowed,
						"This is not a kind of work this platform carries.")
				}
			}
		}
	}

	return e
}

// publicProblems validates the half of a declaration a customer will read (SHIP-79a).
//
// # A first declaration has to name both fields, and this is where that is said
//
// 000303 makes both columns NOT NULL, so a row naming only one of them cannot exist. The store's
// upsert would produce a constraint violation, which reaches a client as a 500 — an error about the
// database standing in for a rule about the request. Refusing it here produces `required` against
// the field the provider left out, which is what a form can render.
//
// **It is a rule about the *first* declaration only.** A provider who has already said who they are
// may send either field alone, because the other one is already in the row for the upsert to keep.
// This function cannot see the row, so it checks only what is wrong regardless — an empty name, an
// over-long one, an unknown form. The "both on a first declaration" rule needs the row and is
// enforced in [Service.Declare], under the lock, where the read is free.
//
// # There is no pattern check on the name, and that is a decision
//
// A trading name that is a phone number would be a way to take a deal off the platform, and a
// regular expression refusing digits would refuse "3 Kings Removals" as readily as "0400 123 456".
// Docs/04 §7's moderation is where content nobody can validate belongs, and Docs/05 §3 puts the
// off-platform question with policy rather than with a validator. **Recorded rather than solved**:
// this field is a moderation surface with no queue behind it yet, and Docs/11 §3 says so.
func (f ProfileFields) publicProblems(e *validate.Errors) {
	if f.DisplayName != nil {
		switch name := *f.DisplayName; {
		case name == "":
			// Distinct from "not named at all", which is the pointer being nil. An empty string
			// is a provider asking to be an identifier again, and there is no such state — see
			// [ProfileFields].
			e.Add("display_name", validate.CodeRequired, "Tell customers who they are dealing with.")
		case len([]rune(name)) < minDisplayName:
			e.Add("display_name", validate.CodeTooShort,
				"Use at least %d characters.", minDisplayName)
		case len([]rune(name)) > maxDisplayName:
			e.Add("display_name", validate.CodeTooLong,
				"Use at most %d characters.", maxDisplayName)
		}
	}

	if f.OperatesAs != nil && !OperatingForm(*f.OperatesAs).Valid() {
		e.Add("operates_as", validate.CodeNotAllowed, "Choose individual or business.")
	}
}

// isPostcode reports whether value is four digits and nothing else.
//
// **Not checked against the state it might belong to**, and that is the same refusal Docs/11 §3
// records for SHIP-60: the allocations have exceptions — 2600 is ACT inside the NSW range — and
// they move when Australia Post says so. Here there is not even a state to check it against, since
// an entry names one grain and never two.
func isPostcode(value string) bool {
	if len(value) != postcodeDigits {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// mustBeProvider refuses a caller who is not a provider account (SHIP-78a).
//
// # Every method on this service calls it, and until SHIP-78a two of them did
//
// `Add` and `Declare` checked; `Update`, `Deactivate`, `Reactivate`, `Vehicle`, `Vehicles` and
// `Profile` did not. Docs/11 §9 carried the gap from wave 5 under two different measurements, and
// its own note is the reason to be precise about what it was and was not: **it was never a
// disclosure.** Each of those six scopes to the caller's own identifier, so a customer reaching one
// got an empty list or a 404 and never another provider's vehicle. What it was is an endpoint
// declining to *refuse* somebody with no business on the surface — and Docs/07 §3's "the app may
// hide or disable; the platform decides" is exactly the rule that makes the client's own gating
// (SHIP-98) a presentation choice rather than the enforcement.
//
// # One refusal, at the first call rather than the last
//
// The value of checking in all eight is that a customer's *first* request to this surface is
// refused with [ErrNotProvider] — one 403 carrying [CodeProviderOnly], which a client can act on —
// instead of six requests answering plausibly and the seventh finally refusing. A screen that lists
// an empty fleet and then refuses the button is a worse explanation than a screen that was never
// reachable.
//
// # It reads users.role rather than the token's claim
//
// [postgresStore.isProvider] selects the column, which is the reading [ErrNotProvider] already
// records: the claim is evidence about the token and the column is the fact. A missing account is
// not a provider either — the row has gone underneath a live session, and "you may not keep a fleet"
// is truthful about that.
//
// The nil identifier is refused here rather than by a separate check in each method. It cannot match
// a row, so `isProvider` answers false, and one sentinel covers "no subject on the request" and "the
// subject is a customer" — which are one thing to a client and were two spellings of the same error
// in six methods before this.
func (s *Service) mustBeProvider(ctx context.Context, r db.Runner, providerID uuid.UUID) error {
	provider, err := s.store.isProvider(ctx, r, providerID)
	if err != nil {
		return err
	}
	if !provider {
		return fmt.Errorf("fleet: %s: %w", providerID, ErrNotProvider)
	}
	return nil
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
