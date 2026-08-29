package jobs

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// Publishing a job (SHIP-63).
//
// # This is the endpoint the whole marketplace was waiting on
//
// Until it existed, `POST /v1/jobs` created a draft, `PATCH /v1/jobs/{id}` edited it, and nothing
// anywhere moved a job to Open — so no provider ever saw a job, no bid was ever placed through the
// product, and every one of the finished tickets downstream of publication had only ever been
// driven by writing rows into PostgreSQL by hand. Docs/11 §1 records that the gap was invisible to
// every instrument in the repository: `make status` counts rows and `make verify` exercises
// endpoints one at a time, and neither asks whether the endpoints compose into a journey a person
// can walk.
//
// # What publication checks, and where each rule comes from
//
// Three things, which is exactly what Docs/09 asks for — "publishing validates required fields,
// checks customer verification, and transitions to Open" — plus the declaration Docs/04 §2 adds
// and the prohibition SHIP-59 built:
//
//   - **The goods are ones Shipper carries** (SHIP-59, Docs/01 §2, Docs/05 §4).
//   - **The customer has verified their email address and phone number** (Docs/04 §2). Explicitly
//     not required to create or edit a draft — that document says so in the same table.
//   - **The declaration was made with this request** (Docs/04 §2, "required for every job").
//   - **The job says enough to be bid on** — see [publishable].
//
// # Nothing here decides that Draft may become Open
//
// `permitted` in model.go is that decision, from Docs/02 §2, and [Service.Transition] is what
// applies it. This function establishes that the *customer* may make the move now; the guard
// establishes that the move exists. Keeping those apart is why Cancel reads the way it does and
// why this reads the same.

// Publish moves a customer's draft to Open (SHIP-63).
//
// # The order of the refusals, and why it is not free to tidy
//
//  1. a job that does not exist is [ErrJobNotFound];
//  2. a job belonging to somebody else is [ErrNotJobOwner];
//  3. a job already Open is absorbed and returned;
//  4. any other status is [ErrJobNotPublishable];
//  5. goods the platform does not carry are [ErrProhibitedCategory];
//  6. an unverified customer is [ErrCustomerNotVerified];
//  7. a missing declaration is [ErrTermsNotAccepted];
//  8. an incomplete job is a field-error list.
//
// **Ownership before status**, as [Service.UpdateDraft] and [Service.Cancel] both have it: the
// first two are one 404 on the wire, so a stranger who could tell "not publishable" from "no such
// job" would learn that the job exists and roughly what state it is in.
//
// **The category before everything about the job's completeness**, which is the one ordering here
// that is a judgement rather than a rule. A customer whose goods Shipper will not carry should be
// told that first — being sent away to fill in a drop-off address and a pickup window, and only
// then told the load was never going to be accepted, is the worst version of this conversation.
// The category is itself a required field, so a job that names none falls through to (8) and is
// told it has not chosen, which is a different sentence from "there is no such category".
//
// **Verification before the declaration**, because verification is about the account and takes the
// customer somewhere else entirely — there is no point asking them to tick a box on a form they
// are about to be sent away from.
//
// # Publishing a job that is already Open succeeds and writes nothing
//
// No second history row, no second event, and terms_accepted_at keeps the instant it already had.
// The idempotency middleware absorbs a retry that reuses its key; this absorbs the one that does
// not, which is the ordinary shape of a phone that lost its connection, was restarted and minted a
// fresh key for the same intent. [Service.Cancel] makes the same choice for the same reason, and
// Docs/02 §3.1 makes it for a queued update that has been overtaken.
//
// **It does not re-check anything.** An Open job whose category was withdrawn from the catalogue
// this morning stays Open, and that is correct: withdrawing a category decides what may be
// published from now on, and unpublishing what is already live is a moderation act with an
// audit trail (`POST /v1/admin/jobs/{id}/unpublish`), not a side effect of a customer's retry.
//
// r must be a transaction. The read, the ownership check, the acceptance and the transition are
// one decision against one version of the row, and [Service.Transition] refuses a pool anyway.
func (s *Service) Publish(
	ctx context.Context,
	r db.Runner,
	customerID, jobID uuid.UUID,
	accepted bool,
) (Job, error) {
	if _, inTx := r.(pgx.Tx); !inTx {
		return Job{}, fmt.Errorf("jobs: publishing %s: %w", jobID, ErrNotInTransaction)
	}

	job, err := s.store.lockJob(ctx, r, jobID)
	if err != nil {
		return Job{}, err
	}

	if job.CustomerID != customerID {
		return Job{}, fmt.Errorf("jobs: %s does not belong to %s: %w", jobID, customerID, ErrNotJobOwner)
	}
	if job.Status == StatusOpen {
		return job, nil
	}
	if job.Status != StatusDraft {
		return Job{}, fmt.Errorf("jobs: %s is %s: %w", jobID, job.Status, ErrJobNotPublishable)
	}

	if job.GoodsCategory != "" {
		if err := s.checkCategory(job.GoodsCategory); err != nil {
			return Job{}, err
		}
	}

	verified, err := s.store.contactVerification(ctx, r, customerID)
	if err != nil {
		return Job{}, err
	}
	if !verified.complete() {
		return Job{}, fmt.Errorf("jobs: %s has not verified %s: %w",
			customerID, verified.missing(), ErrCustomerNotVerified)
	}

	if !accepted {
		return Job{}, fmt.Errorf("jobs: publishing %s: %w", jobID, ErrTermsNotAccepted)
	}

	problems := publishable(job)
	if err := problems.Err(); err != nil {
		return Job{}, err
	}

	// Written before the transition rather than after, so that a job which is Open has an
	// acceptance by construction: the transition is the last thing in the transaction that can
	// fail, and a failure after it would have to be undone rather than merely not committed.
	if err := s.store.acceptTerms(ctx, r, job.ID, s.clock.Now()); err != nil {
		return Job{}, err
	}

	return s.Transition(ctx, r, Move{
		JobID: jobID,
		To:    StatusOpen,
		Actor: User(ActorCustomer, customerID),
	})
}

// contactVerification is which of the two contact details a customer has verified.
//
// Two booleans rather than one, so the refusal can say which is missing. Docs/04 §2 requires both
// and a customer who has verified their email and not their phone is one tap from publishing —
// telling them only "verify your contact details" sends them to look at the one that is already
// done.
type contactVerification struct {
	email bool
	phone bool
}

func (v contactVerification) complete() bool { return v.email && v.phone }

// missing names what is outstanding, for the error message.
func (v contactVerification) missing() string {
	switch {
	case !v.email && !v.phone:
		return "their email address or their phone number"
	case !v.email:
		return "their email address"
	default:
		return "their phone number"
	}
}

// The fields a job must have before it can be offered to providers.
//
// # This set is a judgement, and it is recorded here because no document makes it
//
// Docs/09 asks publication to "validate required fields" and does not say which. Docs/01 §4.1 lists
// what a customer *may* put on a job — "pickup/drop-off locations, date windows, goods description,
// dimensions/weight, vehicle requirement, handling notes, and optional maximum budget" — but that is
// a capability list rather than a required-field specification: only the budget is marked optional,
// and reading the rest as mandatory would refuse a job for having no handling notes, which is the
// ordinary case rather than an incomplete one.
//
// So the test applied here is **can a provider act on this?** A provider needs to know where to
// collect, where to deliver, when, and what the goods are. Those five are required. Everything else
// is priced by asking, and Docs/02 §5 gives them a message thread on the job to ask in.
//
// **Size — weight and dimensions — is deliberately not required, and it is the one entry in this
// list that is genuinely arguable.** A provider choosing between a van and a tray truck wants it,
// and a job without it will attract vaguer bids. It is left optional because refusing to publish is
// the more expensive failure: a customer who cannot say what their pallet weighs would be stopped
// at the last step of the flow with no way forward, whereas a provider who needs the number can ask
// for it. Docs/11 §9 carries this as a decision for the owner to revisit rather than as a settled
// answer — the whole set lives in this one function precisely so that changing it is one edit.
//
// # Only the start of the pickup window is required
//
// A customer who knows the earliest date they can release the goods has said something a provider
// can plan around; one who also knows the latest has said more, and being unable to say is not a
// reason to refuse the job. [TimeWindow] documents the same asymmetry. The drop-off window is not
// required at all: for most road transport it is a consequence of the pickup rather than an
// independent constraint.
//
// # What is deliberately not checked here
//
// **That the dates are in the future.** A draft can be weeks old and its pickup window can have
// passed while it sat there, which is a real thing to tell a customer — but it is Docs/02 §6.3's
// expiry rule, and 000406's trigger already derives the deadline from this window as the job
// becomes Open. A job published with a window that has gone expires almost immediately, by the
// mechanism that exists for it, rather than by a second rule here that would have to agree with it.
func publishable(j Job) validate.Errors {
	var e validate.Errors

	requiredAddress(&e, "pickup", j.Pickup)
	requiredAddress(&e, "dropoff", j.Dropoff)

	e.Required("goods_description", j.GoodsDescription)
	e.Required("goods_category", j.GoodsCategory)

	if j.PickupWindow.Start.IsZero() {
		e.Add("pickup_window.start", validate.CodeRequired,
			"Say the earliest date the goods can be collected, so a provider can plan around it.")
	}

	return e
}

// requiredAddress reports an address that is missing altogether.
//
// It checks presence and not shape: whatever is stored has already been through
// [Address.Validate] on the way in, so a stored address is a well-formed one and re-validating it
// here would report a problem the customer cannot have caused.
func requiredAddress(e *validate.Errors, field string, l Location) {
	if l.Address.IsZero() {
		e.Add(field, validate.CodeRequired,
			"This is required before the job can be published.")
	}
}
