package bidding

import (
	"context"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// What this domain needs of other domains, declared by the consumer (Docs/06 §4.1, Docs/10 §2.3).
//
// internal/bidding imports no domain and the boundary lint refuses one. cmd/api holds the
// implementation and is the only place the two packages meet.
//
// # There is exactly one port, and the reason it exists is that SHIP-81 already answered the question
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
