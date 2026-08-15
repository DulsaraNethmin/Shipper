package delivery

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/pagination"
)

// The delivery read shelf: what the two parties to a job may read about how it is being carried out
// (SHIP-115a).
//
// # Why there is a shelf at all, and why every path on it has five segments
//
// SHIP-115 found the constraint and this file is what it cost. `GET /v1/jobs/open/{id}` (SHIP-83)
// put a literal in the `{id}` position, so it and **any** four-segment `GET /v1/jobs/{id}/<literal>`
// both matched `/v1/jobs/open/<literal>` with neither more specific — and Go's `ServeMux` panics at
// registration rather than answering a 404 somebody debugs. `GET /v1/jobs/{id}/delivery` would have
// stopped the process; `GET /v1/jobs/{id}/delivery/detail` did not. `Docs/09`'s row for this ticket
// names both paths for that reason rather than describing a shelf.
//
// **SHIP-83a has since made the structural fix**: the provider feed is `GET /v1/fleet/jobs/{id}`
// and the four-segment space is free, demonstrated by cmd/api/routes_jobsegment_test.go. These
// paths do not move with it — they are published, installed clients call them, and `Docs/06` §5.3
// is why that is decisive. The shelf also composes, which is the argument that would have kept it
// anyway: SHIP-116's exception and SHIP-133's tracking view have somewhere to go.
//
// # Both parties, decided from the database rather than from a role claim
//
// The customer who owns the job and the provider whose bid they accepted. Being either is a fact;
// `role: provider` is an assertion the platform made about an account and says nothing about *this*
// delivery. Anybody else gets exactly what a job that does not exist gets, because a 403 would
// confirm that somebody else's delivery exists — the reading every endpoint in this domain takes.
//
// # The assigned driver is not a reader here, and that is the same decision [Service.ProofFor] made
//
// A driver has `GET /v1/driver/jobs/{id}`, which serves the one delivery their link grants. These
// two operations are the customer's and the provider's view of the same delivery and are reached
// with a mobile session; a driver holding a seven-day forwardable link is deliberately not given a
// second door to them.

// Delivery is what a party to a job is shown about how it is being carried out (SHIP-115a).
//
// # The driver's mobile number is in this type and is not always populated, deliberately
//
// `Docs/01` §4 asks the platform to "minimise exposure of phone numbers", and §4.4 gives the
// driver's number one stated purpose: support escalation when a delivery goes wrong. A driver has no
// account and no consent surface, so their number is not the customer's to receive — while the
// provider typed it, and returning to somebody what they themselves supplied is not exposure.
//
// **It is blanked in this domain rather than left to the handler to omit.** A handler that has the
// number can leak it by rendering a struct; a handler that never receives it cannot. See
// [Service.DeliveryFor].
type Delivery struct {
	JobID uuid.UUID

	// Assigned is false when nobody is driving the job yet. The rest of this struct is then the
	// zero value, and that is a complete answer rather than a 404: a party to an awarded job is
	// entitled to look, and "no driver yet" is true and useful.
	Assigned bool

	AssignmentID uuid.UUID

	DriverName string

	// DriverMobile is populated for the awarded provider and empty for the customer. See the type
	// note.
	DriverMobile string

	AssignedAt time.Time
}

// DeliveryFor is the driver assignment on a job, for a reader entitled to see it (SHIP-115a).
//
// The live assignment rather than every assignment the job has had. A provider who replaces a driver
// ends one row and inserts another (000600), and what both parties need to know is who is carrying
// the goods now; the history is a support and dispute question, and `driver_assignments` keeps every
// row for it.
//
// A stranger and a job that does not exist are one answer — [ErrJobNotFound], one 404 — for the
// reason [Service.ProofFor] gives at greater length.
//
// r is a reader rather than a transaction: two statements, no writes, nothing to keep consistent.
func (s *Service) DeliveryFor(
	ctx context.Context,
	r db.Runner,
	readerID, jobID uuid.UUID,
) (Delivery, error) {
	party, err := s.partyTo(ctx, r, readerID, jobID)
	if err != nil {
		return Delivery{}, err
	}
	if party == PartyNone {
		return Delivery{}, fmt.Errorf("delivery: %s is not a party to %s: %w",
			readerID, jobID, ErrJobNotFound)
	}

	live, hasDriver, err := s.store.liveAssignment(ctx, r, jobID)
	if err != nil {
		return Delivery{}, err
	}
	if !hasDriver {
		return Delivery{JobID: jobID}, nil
	}

	detail := Delivery{
		JobID:        jobID,
		Assigned:     true,
		AssignmentID: live.ID,
		DriverName:   live.DriverName,
		AssignedAt:   live.CreatedAt,
	}

	// The one field that differs by reader, and it is dropped here rather than in the handler so
	// that nothing downstream is holding a number it must remember not to render.
	if party == PartyProvider {
		detail.DriverMobile = live.DriverMobile
	}
	return detail, nil
}

// MilestonePage is where a page of milestones starts and how long it is.
//
// A domain type rather than `pagination.Cursor` in the signature, because a cursor is an encoding
// and this is a position: the handler decodes what the client sent, and what reaches the store is
// two values it can compare. A malformed cursor is refused at the edge, where the error contract
// already has a shape for it.
type MilestonePage struct {
	After milestoneCursor
	Limit int
}

// milestoneCursor is the ordering key of the last row of a page: the actor's clock and the
// identifier that breaks its ties.
//
// Unexported, so the only cursor the store can be handed is one this package built — from a value it
// issued, or from a client's token that [decodeMilestoneCursor] has already parsed into two typed
// values. `set` rather than a nil pointer, because "the first page" and "a page after the zero time"
// are different requests and a zero `time.Time` cannot express the difference.
type milestoneCursor struct {
	recordedAt time.Time
	id         uuid.UUID
	set        bool
}

// MilestonesFor is a page of one job's milestones, for a reader entitled to see them (SHIP-115a).
//
// # Every recorded milestone, including the ones that moved nothing
//
// That is the point of serving this at all rather than deriving a timeline from the job's status.
// A milestone is a claim and need not move the job (000601): a driver who reaches a pickup, finds
// nobody there and sets off again records `en_route_to_pickup` twice, and SHIP-112 absorbs a late
// milestone as history without moving the job backwards. Neither writes a `job_status_history` row,
// so neither is visible anywhere else — and the second is precisely what a customer asking "where is
// my delivery" is owed an answer about.
//
// # It pages, and `proof` deliberately does not
//
// A delivery has at most one photograph per milestone and at most five recordable milestones, so
// [Service.ProofFor] is bounded by the domain. This is not: 000601 has no uniqueness on
// `(job_id, milestone)` on purpose, and a delivery that goes badly accumulates repeats. A collection
// with no bound is a collection whose worst case is a client's memory.
//
// A stranger and a job that does not exist are one answer, as everywhere else here.
//
// r is a reader rather than a transaction: two statements, no writes.
func (s *Service) MilestonesFor(
	ctx context.Context,
	r db.Runner,
	readerID, jobID uuid.UUID,
	page MilestonePage,
) ([]Record, bool, error) {
	party, err := s.partyTo(ctx, r, readerID, jobID)
	if err != nil {
		return nil, false, err
	}
	if party == PartyNone {
		return nil, false, fmt.Errorf("delivery: %s is not a party to %s: %w",
			readerID, jobID, ErrJobNotFound)
	}

	limit := page.Limit
	if limit < 1 {
		// A caller that asked for nothing gets the configured default rather than an empty
		// page, which is what `pagination.Limit` already answers for an absent `?limit=`. This
		// covers a non-HTTP caller that built a [MilestonePage] by hand.
		limit = pagination.DefaultLimit
	}
	return s.store.milestonesOn(ctx, r, jobID, page.After, limit)
}

// Party is which of the two parties to a job a reader is, if either.
//
// It exists because one field of [Delivery] differs between them. Everything else in this domain
// needs only the yes-or-no, which is why [Service.ProofFor] asks the same question and throws the
// answer away.
type Party int

const (
	// PartyNone is the zero value and is the answer for everybody who is not on this delivery —
	// including a job that does not exist. First on purpose: a lookup that fails to establish
	// anything must not read as a party to the job.
	PartyNone Party = iota

	// PartyCustomer is the account that owns the job.
	PartyCustomer

	// PartyProvider is the account whose bid the customer accepted.
	PartyProvider
)

// partyTo answers which party this account is on a delivery, if either.
//
// The customer is asked first and the awarded provider second, which is an ordering rather than a
// preference: the two are never the same account today — `ck_users_role` fixes the role at
// registration and SHIP-45's trigger keeps it fixed — and cmd/api's jobPartiesLookup records the
// same ordering for the same reason.
//
// Both questions are asked of ports rather than of a role claim on the token, for the reason
// [Service.ProofFor] gives: being the customer on the job and the provider on its accepted bid are
// facts about this delivery, and a role is not.
func (s *Service) partyTo(
	ctx context.Context,
	r db.Runner,
	readerID, jobID uuid.UUID,
) (Party, error) {
	isCustomer, err := s.owners.IsCustomer(ctx, r, jobID, readerID)
	if err != nil {
		return PartyNone, err
	}
	if isCustomer {
		return PartyCustomer, nil
	}

	awarded, isAwarded, err := s.awards.AwardedProvider(ctx, r, jobID)
	if err != nil {
		return PartyNone, err
	}
	if isAwarded && awarded == readerID {
		return PartyProvider, nil
	}
	return PartyNone, nil
}
