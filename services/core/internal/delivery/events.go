package delivery

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
)

// The delivery domain's entry in the event catalogue, and every emission this package makes
// (SHIP-136).
//
// One file, in the shape internal/jobs/events.go established at SHIP-135: the event types, the
// payload struct behind each, one `init` registering them, and the emit helpers the service calls.
// **internal/events was not edited to add any of this** — the catalogue is a table it holds, not a
// list it writes.
//
// # Docs/01 §4.5 says "delivery status changes", and the job's status is only half of that
//
// Every milestone that moves the job already emits `job.status_changed`, from `jobs`, inside this
// domain's transaction and through the port in ports.go. That half was true before this ticket and
// nothing here duplicates it: there is no `delivery.status_changed`, because a second event saying
// the same thing is a second thing to keep in step.
//
// What was **not** emitted is everything this domain records that the job's status does not carry,
// and each of the three is a state change Docs/01 §4.4 names:
//
//   - **A driver on the job.** Docs/01 §4.4's first recordable item. The job moving to
//     'Driver assigned' says a driver exists; it does not say the assignment was replaced, and it
//     says nothing at all when the job was already there (see [Service.AssignDriver]'s
//     JobAlreadyInStatus branch, which writes a row and moves nothing).
//   - **A milestone that moved nothing.** Two ways to get one: the repeated attempt Docs/02 §5
//     describes, and SHIP-112's *absorbed* late milestone, where Docs/02 §3.1 requires the platform
//     to accept the historical fact without moving the job backwards. Both write a row and emit no
//     `job.status_changed`, because there was no transition — so before this ticket a driver's
//     queued 'Picked up' syncing after 'In transit' was invisible to everything downstream.
//   - **The evidence.** Docs/01 §4.4 makes a photograph or a reasoned exception the condition of
//     completing a delivery, and Docs/04 §5 sends the exception path into the moderation queue.
//     Neither is a status.
//
// # The aggregate id is the job, and that is what makes the ordering worth having
//
// Ordering is promised per aggregate (000004_outbox.up.sql) and the key is the aggregate id, so
// keying on the job means **one delivery's events are ordered against each other** — assignment,
// then each milestone in the order the platform recorded them, then the evidence beside the
// milestone it stands behind. That is the sequence a consumer rendering a delivery actually reads,
// and it is the one ordering guarantee Kafka can give here.
//
// The alternative — keying on the milestone or the assignment row — would make every event its own
// aggregate of one, which is ordering nobody can use. `bidding` keys on the bid for the opposite
// reason: there the row *is* the thing with a life, and two providers' offers need no order between
// them.
//
// # What is deliberately not in any payload here
//
// **The driver's name and mobile number.** They are captured at assignment (Docs/01 §4.5's closing
// paragraph) and they are exactly what Docs/01 §5.1 means by "minimise exposure of phone numbers,
// addresses, and delivery details". An event is the worst place for them: it travels through the
// outbox, onto a topic with seven days of retention, and into every consumer there will ever be.
// The event names the assignment; a consumer that has a reason to know who is driving reads the
// row.
//
// **The object key of a photograph.** A proof event says that evidence exists and what kind it is.
// Where the bytes are is a storage locator, and the decision about who may look at them is
// [Service.ProofFor]'s — issued as a short-lived signed URL *after* an authorisation check, which
// is a decision no consumer of a topic is in a position to make.
//
// **Anything of the job beyond its identifier**, which keeps Docs/01 §4.3's budget rule structurally
// out of reach here in the same way [Bid] keeps it out of `bidding`.
const (
	// EventDriverAssigned is a driver put on an awarded job (SHIP-106, SHIP-107).
	EventDriverAssigned = "delivery.driver_assigned"

	// EventMilestoneRecorded is one recorded claim about a delivery (SHIP-111, SHIP-112).
	//
	// Emitted for every milestone row this domain writes, **including one that moved nothing**. See
	// [milestoneRecorded.JobMoved] for why that flag is on the payload rather than expressed as two
	// event types.
	EventMilestoneRecorded = "delivery.milestone_recorded"

	// EventProofRecorded is the evidence behind a recorded claim (SHIP-115, SHIP-116).
	//
	// # One event for a photograph and for a reasoned exception, which is [Proof]'s own shape
	//
	// SHIP-116 widened proof from "one photograph" to "a photograph or the reason there is none" and
	// deliberately did not add a second type beside it: they are two answers to one question — what
	// stands behind this claim — rather than two questions. The event follows the row, and
	// `is_exception` is what a consumer branches on.
	//
	// The alternative considered was a separate `delivery.exception_recorded`, on the argument that
	// Docs/04 §5 sends an exception into the moderation queue and a photograph nowhere. It was not
	// taken: a consumer filtering one boolean is no harder than a consumer subscribing to a second
	// type, and two event types over one table is two things to keep in step every time the table
	// grows. Docs/11 §3 records it as a decision rather than leaving it to be rediscovered.
	EventProofRecorded = "delivery.proof_recorded"
)

func init() {
	events.Register(events.Schema{
		Type:      EventDriverAssigned,
		Aggregate: events.AggregateDelivery,
		Version:   1,
		Payload:   driverAssigned{},
	})

	events.Register(events.Schema{
		Type:      EventMilestoneRecorded,
		Aggregate: events.AggregateDelivery,
		Version:   1,
		Payload:   milestoneRecorded{},
	})

	events.Register(events.Schema{
		Type:      EventProofRecorded,
		Aggregate: events.AggregateDelivery,
		Version:   1,
		Payload:   proofRecorded{},
	})
}

// driverAssigned is a driver taking a job on.
//
// No name and no telephone number — see this file's header. What a consumer gets is enough to
// address the assignment and enough to know whose it is.
type driverAssigned struct {
	AssignmentID string `json:"assignment_id"`
	JobID        string `json:"job_id"`

	// ProviderID is the awarded provider who made the nomination, which is the account
	// [Service.AssignDriver] has already checked owns the work.
	ProviderID string `json:"provider_id"`
}

// milestoneRecorded is one thing an actor says happened on a delivery.
//
// The milestone is the wire form (Docs/10 §4.7) rather than the stored form, which is the one place
// this file departs from `jobs`' precedent — jobs.statusChanged carries Docs/02 §1's own strings
// because the mapping between the two forms belongs to SHIP-56a and it owns the *status* list.
// There is no such ticket for the five milestones: [Milestone.Wire] is this domain's own mapping
// and there is nothing for it to drift against.
type milestoneRecorded struct {
	MilestoneID string `json:"milestone_id"`
	JobID       string `json:"job_id"`

	// Milestone is one of the five, in its wire form.
	Milestone string `json:"milestone"`

	ActorType string `json:"actor_type"`
	ActorID   string `json:"actor_id"`

	// Reason is why, when there was one to give. Empty on the ordinary recording.
	Reason string `json:"reason,omitempty"`

	// JobMoved is false when the row was written and the job stayed where it was.
	//
	// # A flag rather than a second event type, and the reason is what it is a flag *about*
	//
	// The two cases that produce it are the failed pickup attempt (Docs/02 §5 — the job is already
	// at this milestone) and SHIP-112's absorbed late milestone (Docs/02 §3.1 — the job is past it).
	// Both are ordinary recordings that happen not to move anything, and neither is a different
	// *kind* of thing from the recording that does; splitting them into event types would make a
	// consumer subscribe to three names to learn what one delivery did.
	//
	// It is here at all because it is the one fact a consumer cannot derive: when this is false
	// there is no `job.status_changed` to correlate with, and a consumer waiting for one would wait
	// for ever.
	JobMoved bool `json:"job_moved"`

	// ActorRecordedAt is when the actor says they acted and ServerRecordedAt is when the platform
	// wrote it down. Docs/02 §3.1's whole point is that the two differ for an offline driver, and
	// 000601's trigger refuses an insert that tries to name the second — so a consumer that shows a
	// timeline has both and can tell them apart, which the envelope's single `occurred_at` cannot.
	ActorRecordedAt  time.Time `json:"actor_recorded_at"`
	ServerRecordedAt time.Time `json:"server_recorded_at"`
}

// proofRecorded is the evidence behind one recorded claim.
//
// No object key — see this file's header.
type proofRecorded struct {
	ProofID     string `json:"proof_id"`
	JobID       string `json:"job_id"`
	MilestoneID string `json:"milestone_id"`

	// IsException is true when this row is one of Docs/01 §4.4's reasons there is no photograph
	// rather than a photograph. 000604 permits no row that is neither and none that is both, so
	// this is total.
	IsException bool `json:"is_exception"`

	// ExceptionReason is which of the three, and is empty when there is a photograph.
	ExceptionReason string `json:"exception_reason,omitempty"`

	// ContentType is the photograph's, as the *store* reported it rather than as the client claimed
	// it (SHIP-115), and is empty on an exception. It is here because it is the one thing a
	// moderation or viewer consumer can act on without fetching the object.
	ContentType string `json:"content_type,omitempty"`
}

// emitDriverAssigned writes [EventDriverAssigned], inside the caller's transaction.
//
// OccurredAt is the row's own `created_at` — `now()`, the transaction's timestamp — so the event
// says the same instant the row does, from one source. Reading the service's clock here would
// produce a second answer that agrees only by luck.
func (s *Service) emitDriverAssigned(ctx context.Context, r db.Runner, a Assignment, providerID uuid.UUID) error {
	return s.emit(ctx, r, EventDriverAssigned, a.JobID, a.CreatedAt, driverAssigned{
		AssignmentID: a.ID.String(),
		JobID:        a.JobID.String(),
		ProviderID:   providerID.String(),
	})
}

// emitMilestoneRecorded writes [EventMilestoneRecorded], inside the caller's transaction.
//
// OccurredAt is `server_recorded_at` and not the actor's clock, which is the same call
// jobs.Service.emit makes and for the same reason: an event says what the platform decided and when
// it decided it. The actor's claim travels in the payload, where a consumer that cares can see both
// and tell them apart.
func (s *Service) emitMilestoneRecorded(ctx context.Context, r db.Runner, rec Record, jobMoved bool) error {
	payload := milestoneRecorded{
		MilestoneID:      rec.ID.String(),
		JobID:            rec.JobID.String(),
		Milestone:        rec.Milestone.Wire(),
		ActorType:        string(rec.Actor),
		ActorID:          rec.ActorID.String(),
		Reason:           rec.Reason,
		JobMoved:         jobMoved,
		ActorRecordedAt:  rec.ActorRecordedAt,
		ServerRecordedAt: rec.ServerRecordedAt,
	}
	return s.emit(ctx, r, EventMilestoneRecorded, rec.JobID, rec.ServerRecordedAt, payload)
}

// emitProofRecorded writes [EventProofRecorded], inside the caller's transaction.
//
// OccurredAt is `accepted_at`, which is when this row was written, rather than `recorded_at`, which
// is the milestone's actor clock copied onto it. The distinction is the one above: the event is
// timed by the platform and the actor's claim is in the payload of the milestone event beside it.
func (s *Service) emitProofRecorded(ctx context.Context, r db.Runner, p Proof) error {
	return s.emit(ctx, r, EventProofRecorded, p.JobID, p.AcceptedAt, proofRecorded{
		ProofID:         p.ID.String(),
		JobID:           p.JobID.String(),
		MilestoneID:     p.MilestoneID.String(),
		IsException:     p.IsException(),
		ExceptionReason: string(p.ExceptionReason),
		ContentType:     p.ContentType,
	})
}

// emit is the one place this domain builds an event and hands it to the sink.
//
// One function rather than an `events.New` at each call site, for the reason [Service.refused]
// exists: the argument that is easy to get wrong is the aggregate id, and a helper per event type
// makes it impossible to pass the milestone's where the job's belongs.
//
// r must be the transaction making the state change. Both emitting paths are reached only from a
// method that has already checked `r.(pgx.Tx)` at the top.
func (s *Service) emit(
	ctx context.Context,
	r db.Runner,
	eventType string,
	aggregateID uuid.UUID,
	at time.Time,
	payload any,
) error {
	event, err := events.New(eventType, aggregateID, at, payload)
	if err != nil {
		return err
	}
	return s.events.Emit(ctx, r, event)
}
