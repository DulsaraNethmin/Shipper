package jobs

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// Drafts — creating one and editing it (SHIP-60, SHIP-61, SHIP-62).
//
// A separate file from service.go, which stays about the transition guard. The precedent is
// identity, where verify.go, otp.go and token.go each hold one coherent piece of the domain's
// rules rather than everything landing in one file; Docs/10 §2.1's table names the files a domain
// must have, not the only files it may have.
//
// # Nothing here moves a job
//
// Creation lands at Draft because 000400 defaults the column and 000402 refuses any other value
// on insert; editing does not touch status at all. The first caller of [Service.Transition] from
// an endpoint is SHIP-63, publishing. That is worth stating because it is the question a reader
// of these three tickets will ask: the guard is not bypassed here, it is simply not needed —
// there is no transition to make.

// The validation limits for the draft's own fields.
//
// Bounds against abuse and against a client with a runaway text field, not a judgement about what
// a good job description looks like. Generous on purpose, and constants for the same reason the
// address limits in location.go are: these do not move under operational pressure. The limits
// that will — the goods categories, and any commercial cap on the size of load this marketplace
// accepts — are the ones SHIP-58 puts in reference data.
const (
	// 30 metres. Longer than a B-double and shorter than any number a slipped decimal point
	// produces.
	maxDimensionCm = 3000

	// 100 tonnes, comfortably above a B-double's gross mass.
	maxWeightKg = 100_000
)

// CreateDraft creates a job owned by the calling customer (SHIP-61).
//
// The job lands at Draft and cannot land anywhere else: 000400 defaults the column, 000402's
// insert trigger refuses any other value, and insertDraft does not name it. Status is not a
// settable field at creation any more than it is afterwards.
//
// Every field is optional. Docs/01 §4.1 lets a customer save a draft and come back to it, so an
// empty POST is a legitimate "start a job for me" and is answered with an empty draft. What is
// checked is that whatever *was* supplied is well formed, and that the account creating it is a
// customer account — which 000400 says has to be enforced here, because a foreign key cannot see
// another table's column.
//
// No domain event is emitted. A Draft is visible to nobody but its owner, so there is nothing
// downstream to tell; the first event in a job's life is the transition that publishes it.
func (s *Service) CreateDraft(ctx context.Context, r db.Runner, customerID uuid.UUID, f DraftFields) (Job, error) {
	if customerID == uuid.Nil {
		return Job{}, fmt.Errorf("jobs: a draft names no customer: %w", ErrNotCustomer)
	}

	fields := f.normalise()
	if err := fields.validate(); err != nil {
		return Job{}, err
	}

	customer, err := s.store.isCustomer(ctx, r, customerID)
	if err != nil {
		return Job{}, err
	}
	if !customer {
		return Job{}, fmt.Errorf("jobs: %s: %w", customerID, ErrNotCustomer)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Job{}, fmt.Errorf("jobs: generating a job id: %w", err)
	}

	job := fields.applyTo(Job{ID: id, CustomerID: customerID})
	job.Pickup = s.resolve(ctx, "pickup", job.Pickup)
	job.Dropoff = s.resolve(ctx, "dropoff", job.Dropoff)

	return s.store.insertDraft(ctx, r, job)
}

// UpdateDraft applies a partial edit to a draft the caller owns (SHIP-62).
//
// Three refusals, in this order, and the order is the point:
//
//  1. a job that does not exist is [ErrJobNotFound];
//  2. a job belonging to somebody else is [ErrNotJobOwner];
//  3. a job that has left Draft is [ErrJobNotDraft].
//
// Ownership is checked before status, so a caller who does not own the job learns nothing about
// what state it is in — the two errors are the same 404 on the wire, but a difference in *which*
// requests are refused is itself a disclosure. Checking status first would let a stranger
// distinguish somebody else's draft from somebody else's published job by the shape of the
// refusal.
//
// r must be a transaction. The read, the ownership check and the write are one decision made
// against one version of the row, and lockJob's FOR UPDATE only holds for the length of a
// transaction — outside one the lock is released the instant the SELECT returns, and two
// concurrent edits can then interleave into a job carrying half of each.
func (s *Service) UpdateDraft(ctx context.Context, r db.Runner, customerID, jobID uuid.UUID, f DraftFields) (Job, error) {
	if _, inTx := r.(pgx.Tx); !inTx {
		return Job{}, fmt.Errorf("jobs: editing %s: %w", jobID, ErrNotInTransaction)
	}
	if f.IsEmpty() {
		return Job{}, fmt.Errorf("jobs: editing %s: %w", jobID, ErrNothingToUpdate)
	}

	fields := f.normalise()
	if err := fields.validate(); err != nil {
		return Job{}, err
	}

	job, err := s.store.lockJob(ctx, r, jobID)
	if err != nil {
		return Job{}, err
	}

	if job.CustomerID != customerID {
		return Job{}, fmt.Errorf("jobs: %s does not belong to %s: %w", jobID, customerID, ErrNotJobOwner)
	}
	if job.Status != StatusDraft {
		return Job{}, fmt.Errorf("jobs: %s is %s: %w", jobID, job.Status, ErrJobNotDraft)
	}

	// Applying a supplied address produces a Location with no coordinate, so an address that
	// has changed is re-resolved and one that was not mentioned keeps the answer it already
	// had. That is the whole reason the coordinate lives on the same value as the address.
	edited := fields.applyTo(job)
	edited.Pickup = s.resolve(ctx, "pickup", edited.Pickup)
	edited.Dropoff = s.resolve(ctx, "dropoff", edited.Dropoff)

	return s.store.updateDraft(ctx, r, edited)
}

// normalise tidies every supplied field, returning a copy.
//
// Done once, before validation, so that what is validated is exactly what will be stored. The
// alternative — validating the raw input and normalising on the way to the database — is how a
// value passes a length check and then fails a constraint, or passes as "  NSW " and is stored as
// something the state check has never seen.
func (f DraftFields) normalise() DraftFields {
	out := f

	if f.Pickup != nil {
		tidy := f.Pickup.Normalise()
		out.Pickup = &tidy
	}
	if f.Dropoff != nil {
		tidy := f.Dropoff.Normalise()
		out.Dropoff = &tidy
	}

	// Trimmed rather than collapsed: a handling note is a paragraph a driver reads, and
	// collapsing its line breaks into spaces would run three separate instructions together.
	out.GoodsDescription = trimmed(f.GoodsDescription)
	out.HandlingNotes = trimmed(f.HandlingNotes)

	// Collapsed: this one is a phrase on a single line — "ute with a tailgate lifter".
	if f.VehicleRequirement != nil {
		tidy := collapse(*f.VehicleRequirement)
		out.VehicleRequirement = &tidy
	}

	return out
}

func trimmed(s *string) *string {
	if s == nil {
		return nil
	}
	tidy := strings.TrimSpace(*s)
	return &tidy
}

// validate reports everything wrong with the supplied fields at once.
//
// All of it, not the first failure: a customer filling in a form should be shown every field that
// needs attention in one answer rather than discovering them one request at a time (Docs/10 §4.6).
//
// A field the caller did not supply is not validated, which is what makes this usable by both
// verbs. Zero is not a failure either — a non-nil pointer to zero is how a customer clears a
// field they filled in earlier, and the columns refuse zero on their own, so it never reaches the
// database as a value.
func (f DraftFields) validate() error {
	problems := f.problems()
	return problems.Err()
}

// problems is validate with the answer left open, so a caller that has complaints of its own can
// add them to the same list.
//
// http.go is that caller. A timestamp it could not parse and a postcode this file will not accept
// are both things wrong with one request, and reporting them in two round trips is the behaviour
// the "all of it, not the first failure" rule exists to prevent — a customer who fixes the date
// and is then told about the postcode has been made to fill the form in twice.
func (f DraftFields) problems() validate.Errors {
	var e validate.Errors

	if f.Pickup != nil {
		f.Pickup.Validate("pickup", &e)
	}
	if f.Dropoff != nil {
		f.Dropoff.Validate("dropoff", &e)
	}

	if f.GoodsDescription != nil {
		e.Length("goods_description", *f.GoodsDescription, 0, maxGoodsText)
	}
	if f.VehicleRequirement != nil {
		e.Length("vehicle_requirement", *f.VehicleRequirement, 0, maxVehicleText)
	}
	if f.HandlingNotes != nil {
		e.Length("handling_notes", *f.HandlingNotes, 0, maxHandlingNotes)
	}

	dimension(&e, "length_cm", f.LengthCm)
	dimension(&e, "width_cm", f.WidthCm)
	dimension(&e, "height_cm", f.HeightCm)

	if f.WeightKg != nil && (*f.WeightKg < 0 || *f.WeightKg > maxWeightKg) {
		e.Add("weight_kg", validate.CodeOutOfRange,
			"Enter a weight between 0 and %d kilograms.", maxWeightKg)
	}

	timeWindow(&e, "pickup_window", f.PickupWindow)
	timeWindow(&e, "dropoff_window", f.DropoffWindow)

	return e
}

func dimension(e *validate.Errors, field string, value *int) {
	if value == nil {
		return
	}
	if *value < 0 || *value > maxDimensionCm {
		e.Add(field, validate.CodeOutOfRange,
			"Enter a measurement between 0 and %d centimetres.", maxDimensionCm)
	}
}

// timeWindow refuses a window that ends before it starts.
//
// Nothing else is checked. A window in the past is not a mistake at this point — a draft can be
// weeks old, and whether the dates still make sense is a question for publication (SHIP-63),
// which is the moment the job becomes visible to anybody who might act on them.
func timeWindow(e *validate.Errors, field string, w *TimeWindow) {
	if w == nil || w.Start.IsZero() || w.End.IsZero() {
		return
	}
	if w.End.Before(w.Start) {
		e.Add(field+".end", validate.CodeOutOfRange, "This must be on or after the start of the window.")
	}
}

// applyTo returns j with every supplied field replaced.
//
// A supplied address replaces the whole [Location], coordinate included — which is to say it
// discards one. That is deliberate and is the invariant Location exists to carry: a coordinate
// belonging to an address that has since been edited would send a driver to the previous place,
// which is worse than sending them nowhere.
func (f DraftFields) applyTo(j Job) Job {
	if f.Pickup != nil {
		j.Pickup = Location{Address: *f.Pickup}
	}
	if f.Dropoff != nil {
		j.Dropoff = Location{Address: *f.Dropoff}
	}

	if f.GoodsDescription != nil {
		j.GoodsDescription = *f.GoodsDescription
	}
	if f.LengthCm != nil {
		j.Dimensions.LengthCm = *f.LengthCm
	}
	if f.WidthCm != nil {
		j.Dimensions.WidthCm = *f.WidthCm
	}
	if f.HeightCm != nil {
		j.Dimensions.HeightCm = *f.HeightCm
	}
	if f.WeightKg != nil {
		j.WeightKg = *f.WeightKg
	}
	if f.VehicleRequirement != nil {
		j.VehicleRequirement = *f.VehicleRequirement
	}
	if f.HandlingNotes != nil {
		j.HandlingNotes = *f.HandlingNotes
	}
	if f.PickupWindow != nil {
		j.PickupWindow = *f.PickupWindow
	}
	if f.DropoffWindow != nil {
		j.DropoffWindow = *f.DropoffWindow
	}

	return j
}
