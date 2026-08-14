package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/bidding"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
)

// The bidding domain's scheduled work (SHIP-89).
//
// The **fourth** registered task, and the file exists so that adding it edited nothing shared —
// which is the arrangement manifest.go set up before there was a second task and tasks_jobs.go
// then used. Nothing was added to Deps: a domain service is a pure function of the pool, the clock
// and configuration, and this one needs two of the three.
//
// # It is one task rather than two, unlike jobs'
//
// tasks_jobs.go splits expiry from its warning because their failure modes differ — a transition
// that must not be skipped beside a notification that must not be repeated. There is no warning
// here to split off: Docs/02 §6.3 gives the *job* a forty-eight-hour warning and an extension the
// customer can make in one action, and an offer has neither. A provider whose offer is about to
// run out revises it, which is the same request they would make if they were warned.
//
// # What a pass does to every other registered task, which is the half SHIP-15r asked for
//
// cmd/worker is one binary, so this task now runs in every start that job expiry, the expiry
// warning and the outbox publisher run in. It claims what is **due** — a live offer whose
// collection time has passed — and every offer the API can create is not due when it is created,
// because internal/bidding refuses a `pickup_at` that is not in the future. So a caller that wants
// this task to do nothing leaves nothing due, which is the convention's own test.
func init() {
	register(func(d Deps) Task {
		// The real event sink, as cmd/api passes. An expiry that wrote the status and no
		// event would be a state change nobody downstream hears about, which is the one
		// thing bidding.NewService panics rather than tolerates.
		//
		// Three of the four ports are nil, and that is a supported state here rather than
		// a gap. This pass calls exactly one method — bidding.Service.Expire — and it
		// consults none of the three: an offer running out on its own terms is not a
		// question about whether a provider may bid, whether an account owns a job, or
		// whether a job can still be awarded. Handing the worker a fleet filter and two
		// job adapters it would never call would be three dependencies in a process that
		// has no reason for any of them, which is tasks_jobs.go's argument for passing a
		// nil geocoder.
		//
		// **The fourth is not optional (SHIP-90).** Docs/02 §2's `Negotiating → Open` names
		// "all active bids expire" first, so an expiry that took the last live offer on a
		// job has to return that job to Open — through `jobs`' one guarded function, in the
		// transaction that closed the offer.
		service := bidding.NewService(events.NewOutbox(), nil, nil, nil,
			presentedJobs{jobs: jobs.NewService(events.NewOutbox(), d.Clock, nil)}, d.Clock)

		return Task{
			Name:    "bid-expiry",
			Every:   bidExpiryInterval,
			Timeout: bidExpiryTimeout,
			Run:     expireBids(d, service),
		}
	})
}

// bidExpiryInterval is how often the sweep runs.
//
// The same five minutes tasks_jobs.go chose, and for the second of its two reasons rather than the
// first. Precision is not the constraint — a collection time is a date and an hour, not a second —
// and the number is chosen against the other end: the longest a customer should be looking at an
// offer to collect at a time that has been and gone. Five minutes is short enough that nobody
// awards a dead offer and long enough that an idle marketplace runs one indexed query per worker
// per five minutes, against a partial index sized to the negotiations in flight (000503).
//
// A constant rather than configuration, on tasks_jobs.go's reasoning: internal/config is a shared
// surface a domain branch does not edit (Docs/10 §9.2), and nothing here changes under operational
// pressure. The deadline itself is per offer and already stored, so tuning this changes only how
// promptly a decision the offer already made is acted on.
//
// A separate constant from `jobExpiryInterval` rather than a reference to it, because the two
// files belong to two domains and two branches. They happen to agree; a shared constant would make
// one domain's tuning the other's.
const bidExpiryInterval = 5 * time.Minute

// bidExpiryTimeout bounds one pass.
//
// Longer than the manifest's default, for `jobExpiryTimeout`'s reason: a pass does up to
// [bidding.ExpiryBatch] expiries, each of which writes a status and an event, all in one
// transaction that must not be cut off halfway through a slow moment on the database. Still far
// shorter than the interval, so a wedged pass ends well before the next one would start.
const bidExpiryTimeout = 2 * time.Minute

// expireBids is one pass: claim the live offers whose collection time has passed, and end each.
//
// The shape tasks_jobs.go established — claim inside the caller's transaction, act on what was
// claimed, return the count — and it is the same shape on purpose. Two tasks that claimed work
// differently would be two things to reason about when a pass misbehaves at three in the morning.
//
// Both halves are inside the caller's transaction, which is what makes two workers safe: the claim
// locks with `FOR UPDATE SKIP LOCKED` and the expiries run against those same locks, so a second
// worker sweeping at the same instant is handed the rows this one did not take. If the pass fails
// part way the transaction rolls back and every claimed offer is released unexpired, which is what
// makes a crashed worker harmless.
//
// The clock is the worker's, not the database's ([bidding.ExpiryClaim] takes the instant as a
// parameter), so a test advances a clock.Fixed and watches an offer expire instead of writing a
// collection time into the past to fake one.
func expireBids(d Deps, service *bidding.Service) Work {
	return func(ctx context.Context, r db.Runner) (int, error) {
		due, err := ClaimIDs(ctx, r, bidding.ExpiryClaim, d.Clock.Now(), bidding.ExpiryBatch)
		if err != nil {
			return 0, err
		}

		for _, id := range due {
			if _, err := service.Expire(ctx, r, id); err != nil {
				// Returned rather than logged and skipped, exactly as the job sweep
				// does. One offer that cannot be expired means the rule is not being
				// applied, and a pass reporting success while leaving a dead offer
				// awardable is the failure this arrangement exists to avoid. The
				// transaction rolls back, the claim is released, and the next pass
				// tries again with the failure in the log.
				return 0, err
			}

			// Debug rather than info, for the reason the job sweep's line is: the pass
			// already reports how many it claimed, and a marketplace clearing a backlog
			// would otherwise write a line per offer.
			d.Logger.Debug("bid expired",
				slog.String("bid_id", id.String()),
				slog.Time("judged_at", d.Clock.Now()))
		}
		return len(due), nil
	}
}

// presentedJobs implements bidding.Presentation over the real `jobs` service (SHIP-90).
//
// **A second copy of cmd/api/routes_bidding.go's adapter of the same name**, and the duplication is
// deliberate rather than an oversight. The two composition roots are two binaries; neither imports
// the other, and `internal/bidding` names neither `jobs` nor this type. What keeps the copies
// honest is that both satisfy one interface and both are exercised — that one by the served
// endpoints and `scripts/verify/61-bidding.sh`, this one by cmd/worker/tasks_bidding_test.go
// running the registered task against a real database. internal/bidding's own fixtures keep a third
// copy for the same reason and say so.
//
// Only `LeaveNegotiation` is ever called from here: a sweep closes offers and never opens one.
// `EnterNegotiation` is implemented because the interface has it, and because a partial
// implementation would be a type that compiles into the wrong shape the day a second bidding task
// is registered.
type presentedJobs struct {
	jobs *jobs.Service
}

// The two reasons, matching cmd/api's exactly. Support searches for these strings and a customer's
// status timeline shows them, so a job that reached Open through the sweep and one that reached it
// through a withdrawal must read the same.
const (
	negotiationOpenedReason = "An offer was made on the job (Docs/02 §2)."
	negotiationEndedReason  = "The last live offer on the job was withdrawn or expired (Docs/02 §2)."
)

func (p presentedJobs) EnterNegotiation(
	ctx context.Context,
	r db.Runner,
	jobID uuid.UUID,
) (bidding.JobPresentation, error) {
	return p.move(p.jobs.Transition(ctx, r, jobs.Move{
		JobID:  jobID,
		To:     jobs.StatusNegotiating,
		Actor:  jobs.System(),
		Reason: negotiationOpenedReason,
	}))
}

// LeaveNegotiation takes the job row `FOR UPDATE SKIP LOCKED` and moves it only if it got it.
//
// **This is the method the sweep needs and the reason it cannot block.** A pass claims its offers
// with `FOR UPDATE SKIP LOCKED` and holds those `bids` rows for the rest of its transaction, so a
// blocking lock on `jobs` here would be `bids` → `jobs` against the award's `jobs` → `bids` — and
// the first deadlock would be between a sweep and a customer awarding a job. bidding.Presentation
// carries the full reasoning and the cost.
func (p presentedJobs) LeaveNegotiation(
	ctx context.Context,
	r db.Runner,
	jobID uuid.UUID,
) (bidding.JobPresentation, error) {
	const q = `SELECT status FROM jobs WHERE id = $1 FOR UPDATE SKIP LOCKED`

	var status jobs.Status
	switch err := r.QueryRow(ctx, q, jobID).Scan(&status); {
	case errors.Is(err, db.ErrNoRows):
		return bidding.JobPresentationHeld, nil
	case err != nil:
		return bidding.JobPresentationUnrecognised,
			fmt.Errorf("worker: holding %s to leave Negotiating: %w", jobID, err)
	}

	if !jobs.Permitted(status, jobs.StatusOpen) {
		if status == jobs.StatusOpen {
			return bidding.JobPresentationAlreadyThere, nil
		}
		return bidding.JobPresentationClosed, nil
	}

	return p.move(p.jobs.Transition(ctx, r, jobs.Move{
		JobID:  jobID,
		To:     jobs.StatusOpen,
		Actor:  jobs.System(),
		Reason: negotiationEndedReason,
	}))
}

func (presentedJobs) move(_ jobs.Job, err error) (bidding.JobPresentation, error) {
	switch {
	case err == nil:
		return bidding.JobPresentationMoved, nil
	case errors.Is(err, jobs.ErrAlreadyInStatus):
		return bidding.JobPresentationAlreadyThere, nil
	case errors.Is(err, jobs.ErrTransitionNotPermitted), errors.Is(err, jobs.ErrJobNotFound):
		return bidding.JobPresentationClosed, nil
	default:
		return bidding.JobPresentationUnrecognised, err
	}
}

// Compile-time proof that the adapter satisfies the port bidding declared, in the binary that uses
// it. cmd/api carries the same line for its own copy.
var _ bidding.Presentation = presentedJobs{}
