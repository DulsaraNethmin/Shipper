package delivery

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// Milestone is one of the five things a provider or an assigned driver records on a delivery
// (Docs/01 §4.4).
//
// The values are Docs/02 §1's own strings — spaces and sentence case included — because
// Docs/10 §3.4 requires it and because the same list is held in SQL by ck_milestones_milestone.
// TestMilestoneConstraintMatchesTheGoConstants reads the constraint out of pg_constraint and
// holds the two together in both directions, which is the pairing Docs/10 §3.4 asks of every
// enumeration.
//
// # Why this is not jobs.Status
//
// Four of the five are also job statuses and the strings are identical. They are still a
// different list, for two reasons that will not go away.
//
// Mechanically, domains do not import each other (Docs/06 §4.1), so delivery cannot reach into
// jobs for the constants even if it wanted to.
//
// Substantively, the two lists answer different questions and Docs/02 §2 says so directly:
// "Where 01 §4.4 numbers the five recordable milestones as a sequence, it is describing what a
// driver records, not constraining what the guard accepts." A milestone need not move the job at
// all — a late "Picked up" arriving after "In transit" is recorded and absorbed (SHIP-112) — and
// there are statuses nobody records, because 'Completed' expires into existence after seventy-two
// hours (Docs/02 §6.1) and 'Cancelled' and 'Disputed' are not things that happen on a delivery.
//
// # Not a sequence, despite Docs/01 §4.4 numbering one
//
// 'Driver assigned' is skippable: Docs/02 §2 permits Awarded → En route to pickup directly,
// because a provider driving the job themselves has nobody to nominate. Anything that treats the
// order below as a chain to be walked will be wrong about that job.
type Milestone string

const (
	MilestoneDriverAssigned  Milestone = "Driver assigned"
	MilestoneEnRouteToPickup Milestone = "En route to pickup"
	MilestonePickedUp        Milestone = "Picked up"
	MilestoneInTransit       Milestone = "In transit"
	MilestoneDelivered       Milestone = "Delivered"
)

// Milestones is every milestone, in the order Docs/01 §4.4 numbers them.
//
// Ordered rather than a set because that is the order a delivery normally runs in and the order
// a timeline reads in, not because anything may rely on it as a sequence — see above.
var Milestones = []Milestone{
	MilestoneDriverAssigned,
	MilestoneEnRouteToPickup,
	MilestonePickedUp,
	MilestoneInTransit,
	MilestoneDelivered,
}

// Valid reports whether m is one of the five.
func (m Milestone) Valid() bool {
	for _, known := range Milestones {
		if m == known {
			return true
		}
	}
	return false
}

func (m Milestone) String() string { return string(m) }

// Wire is the milestone as a client names it: lower snake case, per Docs/10 §4.7.
//
// Derived rather than tabulated, which is the same call jobs.Status.Wire makes and for the same
// reason: a five-entry table beside the five constants is a second list that can disagree with the
// first, and a transformation cannot disagree with its input.
//
// **This is delivery's own mapping and not a copy of `jobs`'.** Four of the five strings happen to
// match a job status, so the wire forms match too — `picked_up` here and `picked_up` there — and
// they are still two vocabularies (see the type's own comment). SHIP-56a generates the *status*
// list for three languages; it has no opinion about this one, which has five values, a different
// membership, and a table of its own behind it.
func (m Milestone) Wire() string {
	return strings.ReplaceAll(strings.ToLower(string(m)), " ", "_")
}

// MilestoneFromWire is [Milestone.Wire] read backwards: the milestone a client named, or false.
//
// Case-sensitive on purpose. `Picked up` is the stored form and `picked_up` is the wire form, and a
// client that sends the stored form has misread the contract rather than typed the wrong case — it
// is told so, which is more useful than being quietly understood and then finding the two forms
// used interchangeably by the next endpoint.
func MilestoneFromWire(wire string) (Milestone, bool) {
	for _, known := range Milestones {
		if known.Wire() == wire {
			return known, true
		}
	}
	return "", false
}

// maxMilestoneReason bounds the note an actor may attach to a milestone.
//
// Generous but finite, on the same reasoning as maxDriverName: a bound against a runaway text
// field rather than a judgement about what may be said. What goes here is Docs/02 §5's ordinary
// exception — "customer unavailable at pickup", "the gate was locked and I am returning at four" —
// and a driver typing it on a phone in a yard is not writing an incident report.
const maxMilestoneReason = 500

// Recording is what an actor says they did, as it arrives from a client.
//
// # RecordedAt is the actor's clock and is never corrected
//
// Docs/02 §3.1 requires the two clocks to be carried separately, and the migration that made them
// two columns (000601) is explicit that this one is not bounded against now(): a device that has
// been out of signal for a day, or is simply set wrong, still recorded something true about work
// that was done, and "refusing an implausible time would discard the record".
//
// The zero value means "the actor did not say", which is the ordinary case for anything recorded
// online: there is one clock, and [Service.RecordMilestone] fills this from it. That is the same
// treatment jobs.Move.RecordedAt gets, deliberately — a milestone and the transition it causes are
// one act, and they must not disagree about when the actor says it happened.
//
// # Key is the client's idempotency key, and it is a field rather than an argument
//
// It reaches the milestones row, so it is part of what is recorded rather than part of how the
// request was made. The uniqueness it buys is the database's (000602), not this struct's.
//
// # Proof is part of the recording rather than a request of its own (SHIP-115)
//
// A photograph is evidence for a claim, so it arrives with the claim: one request, one transaction,
// and a `proofs` row that cannot exist without the `milestones` row it points at. The alternative —
// record the milestone, then attach proof to it afterwards — has a window in which a delivery has
// been recorded and its photograph has not, which is precisely the state SHIP-118 exists to refuse
// ("Delivered is rejected without either"). A rule cannot be enforced against a state the platform
// deliberately passes through.
//
// **The field is a [VerifiedProof] and not a string**, which is the whole of the guarantee: the only
// value another package can construct is the zero one, so nothing outside `delivery` can put a
// photograph here that the platform has not looked at. [Service.VerifyProof] is what produces one.
//
// # Exception is the other half of the same field, and exactly one of the two is ever set (SHIP-116)
//
// Docs/01 §4.4 makes the exception path "part of the same feature", and this is where the two meet:
// a recording carries a photograph, or a reason there is none, and never both. [Recording.problems]
// refuses the pair and 000604's ck_proofs_photograph_or_exception refuses the row.
//
// **It is an ordinary exported string type and not a [VerifiedProof]**, and the asymmetry is
// deliberate rather than an oversight. A photograph is a claim about the world that the platform
// must check — the object either exists in the bucket or it does not — so the type is what makes it
// uncheckable-by-accident. A reason is a *selection* the actor made from three the platform
// published; there is nothing to verify it against, and the only thing that can be wrong with it is
// that it is not one of the three, which [ProofExceptionReason.Valid] answers.
type Recording struct {
	Milestone  Milestone
	RecordedAt time.Time
	Reason     string
	Key        string

	Proof     VerifiedProof
	Exception ProofExceptionReason
}

// hasEvidence reports whether this recording carries a photograph or a reasoned exception.
//
// One reader rather than the disjunction written out at each call site, because the two are one
// concept — what stands behind this claim — and SHIP-118 turns that concept into the condition
// 'Delivered' is accepted on.
func (rec Recording) hasEvidence() bool { return rec.Proof.present() || rec.Exception != "" }

// normalise trims what the client sent into what the columns should hold.
func (rec Recording) normalise() Recording {
	rec.Reason = collapse(rec.Reason)
	return rec
}

// problems reports what is wrong with a recording, in the error contract's shape.
//
// The milestone is checked here rather than left to ck_milestones_milestone for the reason
// Docs/10 §4.6 gives: a client that sent a value the platform does not know should be told which
// field was wrong and what is accepted, not handed a constraint name in a 500.
//
// **`driver_assigned` is refused, and it is refused here rather than being absent from the
// enumeration.** Docs/01 §4.4 numbers five and the database accepts five; what this endpoint will
// not do is write the fifth. See [Service.RecordMilestone].
func (rec Recording) problems() validate.Errors {
	var e validate.Errors

	switch {
	case rec.Milestone == "":
		e.Add("milestone", validate.CodeRequired, "Say which milestone you are recording.")

	case !rec.Milestone.Valid():
		e.Add("milestone", validate.CodeInvalid,
			"That is not a milestone. Use one of %s.", strings.Join(recordableWire(), ", "))

	case rec.Milestone == MilestoneDriverAssigned:
		e.Add("milestone", validate.CodeNotAllowed,
			"A driver is put on a job through its own endpoint, not recorded as a milestone.")
	}

	if rec.Reason != "" {
		e.Length("reason", rec.Reason, 1, maxMilestoneReason)
	}

	// The evidence, checked here as well as at the wire (SHIP-116).
	//
	// [recordingFrom] refuses both of these before a [Recording] is built, so neither is reachable
	// through the endpoint — and they are checked in the domain anyway, because the alternative is
	// a rule that holds only for callers that came in through one decoder. The field names are the
	// wire's, because that is what a client is being told about.
	switch {
	case rec.Proof.present() && rec.Exception != "":
		e.Add("proof.exception_reason", validate.CodeNotAllowed,
			"Send the photograph you uploaded or a reason there is none, not both.")

	case rec.Exception != "" && !rec.Exception.Valid():
		// Named rather than left to validate.OneOf, whose message is "that is not one of the
		// available options". Docs/01 §4.4 has the driver *select* a reason, so the refusal is
		// where a client learns which three there are — the same call [UploadRequest.problems]
		// makes about the accepted media types.
		e.Add("proof.exception_reason", validate.CodeInvalid,
			"That is not a reason a photograph can be missing. Use one of %s.",
			strings.Join(proofExceptionWire(), ", "))
	}

	return e
}

// recordableWire is the wire form of every milestone this endpoint accepts.
//
// Derived from [Milestones] minus the one with an endpoint of its own, so the message a client is
// refused with cannot list a value the handler would then reject — which is what a hand-written
// string in the message above would eventually do.
func recordableWire() []string {
	wire := make([]string, 0, len(Milestones))
	for _, m := range Milestones {
		if m == MilestoneDriverAssigned {
			continue
		}
		wire = append(wire, m.Wire())
	}
	return wire
}

// Record is one row of milestones — what somebody recorded, and when, on both clocks.
//
// It is evidence rather than state. The table is append-only (000601), so nothing here is ever
// updated: a milestone that turned out to be wrong is another milestone and another row.
//
// ActorRecordedAt is what the actor claims and ServerRecordedAt is when it arrived. **Neither is
// derived from the other**, and the pair is what makes an offline delivery reconstructable — which
// is why the second is filled by a trigger that refuses to be told what to say.
type Record struct {
	ID    uuid.UUID
	JobID uuid.UUID

	Milestone Milestone

	Actor   ActorType
	ActorID uuid.UUID

	Reason string
	Key    string

	ActorRecordedAt  time.Time
	ServerRecordedAt time.Time
}

// Outcome is what the platform did with a recording (SHIP-112).
//
// # It replaced a bool, and the third value is the reason
//
// SHIP-111 answered "was anything written?", which had two answers and needed no type. SHIP-112
// adds a third thing that can happen to a perfectly good milestone — it is written, and the job is
// deliberately left where it is — and a second bool beside the first would have made the caller
// work out which pairs are possible. Three named values do not.
//
// It travels no further than [Handler.RecordMilestone]. **The response body is unchanged and
// carries no outcome field**, which is a decision rather than an omission: the milestone response
// has never told a client what status the job is in (see [milestoneResponse]), Docs/02 §3.1 puts
// reconciliation on the job resource — "the app displays optimistic local state, clearly marked as
// pending, and reconciles to whatever the platform returns" — and a field here would be answered
// from a *stored* fact on a replay and a *computed* one on the first attempt, which is two answers
// to one question. What the client is told is that the request succeeded, which before SHIP-112 was
// the one thing a late milestone could not be told.
type Outcome int

const (
	// OutcomeUnrecognised is the zero value and never accompanies a nil error.
	//
	// First for the reason [JobMoveUnrecognised] is first: a path that forgets to say what it did
	// must not be indistinguishable from one that recorded something.
	OutcomeUnrecognised Outcome = iota

	// OutcomeRecorded means the row was written and the job is where the milestone says it is —
	// either because the milestone moved it, or because it was already there (Docs/02 §5's
	// failed pickup attempt).
	//
	// The two are one outcome because nothing downstream treats them differently: in both, what
	// the actor recorded and what the platform holds agree.
	OutcomeRecorded

	// OutcomeAbsorbed means the row was written and the job was deliberately not moved, because
	// it has already been past this point (SHIP-112, Docs/02 §3.1).
	//
	// Nothing else happened. No job_status_history row, no status change, no event — see
	// [Service.RecordMilestone].
	OutcomeAbsorbed

	// OutcomeAlreadyRecorded means this idempotency key had already recorded this milestone and
	// nothing was written. The record returned is the one the first attempt wrote.
	OutcomeAlreadyRecorded
)

func (o Outcome) String() string {
	switch o {
	case OutcomeRecorded:
		return "recorded"
	case OutcomeAbsorbed:
		return "absorbed"
	case OutcomeAlreadyRecorded:
		return "already recorded"
	default:
		return "unrecognised"
	}
}

// ActorType is who recorded a milestone.
//
// Narrower than job_status_history's five on purpose. Docs/02 §3: "delivery-status updates must
// be made only by the awarded provider, their assigned driver, or an administrator acting with an
// audit reason". A customer is not among them — confirming a delivery is not recording one, and
// that confirmation is a status transition rather than a milestone.
//
// The platform is here because Docs/02 §2 permits Picked up → In transit as "an automatic
// presentation change", which is a milestone with nobody behind it.
type ActorType string

const (
	ActorProvider ActorType = "provider"

	// ActorDriver identifies the actor by their driver_assignments row rather than by an
	// account, because a driver has none: the driver portal is link-authenticated and holds a
	// job-scoped token that cannot be exchanged for a session (Docs/07 §3). This is the
	// convention 000401 declared for job_status_history and 000601 follows.
	ActorDriver ActorType = "driver"

	// ActorAdmin has no users row either. ck_users_role refuses 'admin' because admin sign-in
	// is a separate system (SHIP-147).
	ActorAdmin ActorType = "admin"

	// ActorSystem names no identity at all, which ck_milestones_actor_id enforces.
	ActorSystem ActorType = "system"
)

// ActorTypes is every actor that may record a milestone.
//
// Paired with ck_milestones_actor_type by TestMilestoneActorConstraintMatchesTheGoConstants, for
// the reason Docs/10 §3.4 gives: a value the database accepts and Go has no constant for is a
// state no code handles.
var ActorTypes = []ActorType{
	ActorProvider,
	ActorDriver,
	ActorAdmin,
	ActorSystem,
}

// Valid reports whether a is one of the four.
func (a ActorType) Valid() bool {
	for _, known := range ActorTypes {
		if a == known {
			return true
		}
	}
	return false
}

func (a ActorType) String() string { return string(a) }
