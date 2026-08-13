package bidding

import (
	"context"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// What this domain needs of other domains, declared by the consumer (Docs/06 §4.1, Docs/10 §2.3).
//
// internal/bidding imports no domain and the boundary lint refuses one. cmd/api holds the
// implementations and is the only place the packages meet.
//
// # There are two ports, and the second arrived with the first endpoint a customer may call
//
// Everything before SHIP-87 was provider-only, so "who may do this" was one question with one
// answer — [Eligibility]. Docs/02 §4 lets *either* party counter, so the domain now has to recognise
// a customer as well, and "is this account the customer who owns that job, and can that job still be
// awarded" are `jobs`' facts rather than this domain's. [Negotiation] is how it asks.
//
// # There is one port for the provider side, and the reason it exists is that SHIP-81 already
// answered the question
//
// SHIP-84's *Done when* opens with "a verified, eligible provider", and eligibility is
// `internal/fleet`'s: Docs/01 §4.3's four filters — service area, vehicle capability, verification
// state, job status — are one SQL predicate in eligibility.go, and that file's own header says the
// second reader of it is "what SHIP-84 needs before it accepts a bid". So this domain does not decide
// who may bid. It asks.
//
// **Reimplementing the filter here would have been the defect, not the import.** Two definitions of
// who may bid is one more than Docs/07 §3 permits — the platform decides server-side in exactly one
// place — and the two would part company the first time one of them was corrected. The endpoint that
// shows a provider a job and the endpoint that accepts their bid on it would then disagree, which is
// the worst available outcome: a provider prices a job the feed offered them and is refused after
// doing the work.
//
// # A bool, and why the seam is this narrow
//
// [Eligibility] hands back a boolean and nothing else. A caller deciding whether to *permit*
// something wants a boolean, not a row — the argument fleet's own eligibility.go makes when it keeps
// `EligibleFor` distinct from the read SHIP-83 serves. A port that returned the job would put a
// provider-facing copy of a job inside this package, which is a second shape for Docs/01 §4.3's
// budget rule to be broken through, and this domain has no use for one: nothing it writes reads a
// field of the job.
//
// It also means the port needs no adapter at all. A port whose answer is a bool has no vocabulary to
// translate, so `cmd/api` satisfies it structurally and writes no translation type — unlike
// delivery.Jobs, where four transition outcomes had to be carried across without naming a `jobs`
// sentinel. The compile-time assertion in cmd/api/routes_bidding.go is the whole of the wiring.
type Eligibility interface {
	// EligibleFor reports whether this provider may bid on this job, right now.
	//
	// It takes the caller's [db.Runner] so the answer is read inside the transaction that writes the
	// bid (Docs/10 §3.2). A bid authorised by a check made in a different transaction is a bid
	// nobody checked: the job can be cancelled, expire, or be awarded in between.
	//
	// A job that does not exist is `false` rather than an error, and so is a job this provider may
	// not bid on. The two are deliberately one answer — distinguishing them would take a second
	// query whose only product is the knowledge that some identifier exists, which is the disclosure
	// this domain's 404 exists to prevent.
	//
	// A non-nil error is a failure of the mechanism rather than a refusal.
	EligibleFor(ctx context.Context, r db.Runner, providerID, jobID uuid.UUID) (bool, error)
}

// Negotiation is what `jobs` knows about the customer's side of a bid (SHIP-87, SHIP-88).
//
// # Two bools, and neither is derivable from the other
//
// They answer different questions at different moments, and collapsing them would make one of the
// two endpoints wrong.
//
//   - **A counter needs both.** The caller has to be the customer, *and* the job has to be one a
//     counter-offer could still lead anywhere.
//   - **Reading the chain needs only the first, and needs it to keep working afterwards.** SHIP-88's
//     *Done when* is "full chain remains readable", and a negotiation is at its most worth reading
//     once the job is over — the customer who awarded elsewhere, the provider reconstructing what
//     they offered, the administrator handling a dispute (Docs/02 §4). A read gated on the job still
//     being awardable would go dark at exactly the moment the record matters.
//
// # Why they are bools, and why that is what keeps this seam cheap
//
// The same reading [Eligibility] takes: a caller deciding whether to *permit* something wants a
// boolean, not a row. A port returning the job would put a customer-facing copy of a job inside this
// package — a second shape for Docs/01 §4.3's budget rule to be broken through — and this domain has
// no use for one, because nothing it writes reads a field of the job.
//
// It also means neither method needs an adapter *type* to translate a vocabulary, only a small one
// in cmd/api to turn `jobs`' sentinels into the answers below. That is the arrangement SHIP-84
// found: "a port is only as expensive as the vocabulary it has to carry."
type Negotiation interface {
	// CustomerOf reports whether this account is the customer who owns this job.
	//
	// **Whatever the job's status**, deliberately: this is what makes a chain readable to its parties
	// after the job has been awarded, cancelled or completed.
	//
	// A job that does not exist is `false` rather than an error, exactly as
	// [Eligibility.EligibleFor] treats one, and for the same reason — distinguishing the two would
	// take a second query whose only product is the knowledge that some identifier exists.
	CustomerOf(ctx context.Context, r db.Runner, userID, jobID uuid.UUID) (bool, error)

	// AwardableBy reports whether this customer's job could still be awarded.
	//
	// **That is the question a counter-offer turns on, and it is deliberately not "is the job
	// biddable".** The two happen to select the same two statuses today, and they are different
	// questions: a provider asks whether they may *offer*, which Docs/01 §4.3's four filters answer;
	// a customer countering is asking whether the negotiation can still end in an award. Answering
	// the second by copying `fleet`'s list of biddable statuses would put a third copy of that list
	// in the service — after `jobs`' own transition table and `fleet.biddableStatuses` — and the
	// third copy is the one nobody would remember to correct.
	//
	// The implementation in cmd/api reads `jobs`' authoritative transition table instead, so this
	// answer moves if and only if Docs/02 §2 does.
	//
	// It takes the customer as well as the job because a job nobody may see is not a job whose
	// status this domain has any business learning. `false` covers "not yours", "no such job" and
	// "past negotiating" together, which is the one answer a refusal is allowed to disclose.
	AwardableBy(ctx context.Context, r db.Runner, customerID, jobID uuid.UUID) (bool, error)
}
