package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
)

// The jobs domain's scheduled work (SHIP-68, SHIP-69, SHIP-119).
//
// This file exists so that adding a task adds a file and edits none, exactly as
// cmd/api/routes_jobs.go does for routes. manifest.go says why: four tasks in the backlog belong
// to three domains and three branches, and a registration dropped in a merge produces a task that
// silently never runs — which is worse than a dropped route, because nobody calls a task and
// notices it is gone.
//
// Nothing was added to Deps to make this work, which is the test manifest.go set for itself: a
// domain service is a pure function of the pool, the clock and configuration. SHIP-69 added a
// second task to the same file and still added nothing, which is the first time that claim has been
// tested by anything but its author.
//
// # Two tasks over one column, and why they are two
//
// Both sweeps read jobs.expires_at, and a single pass could warn and expire in one claim. They are
// separate because their failure modes are: expiry is a transition that must not be skipped, and a
// warning is a notification that must not be repeated. One task means a failure in either half
// rolls back both, so an outbox that cannot be written stops jobs expiring — which is the wrong
// trade, since a job that stays Open past its pickup date misleads providers while a warning that
// arrives late merely arrives late.
//
// # And a third, which is not over that column at all
//
// SHIP-119 sweeps Delivered jobs seventy-two hours after they were delivered (Docs/02 §6.1). It is
// in this file because it is a jobs transition — internal/jobs/autocomplete.go has the reading of
// Docs/02 that puts it in this domain rather than in delivery — and it is a third task rather than
// a branch of either of the others because it claims a different set of rows on a different
// deadline. The argument above applies unchanged: one task that swept two statuses would roll back
// an expiry because an auto-completion failed.

func init() {
	register(func(d Deps) Task {
		// No geocoder. Expiry moves a job that already exists and never touches an address,
		// and jobs.NewService is explicit that a nil Geocoder is a supported state rather
		// than a broken one. Handing the worker a maps vendor it would never call would be
		// an outbound dependency in a process that has no reason for one.
		service := jobs.NewService(events.NewOutbox(), d.Clock, nil)

		return Task{
			Name:    "job-expiry",
			Every:   jobExpiryInterval,
			Timeout: jobExpiryTimeout,
			Run:     expireJobs(d, service),
		}
	})

	register(func(d Deps) Task {
		service := jobs.NewService(events.NewOutbox(), d.Clock, nil)

		return Task{
			Name:    "job-expiry-warning",
			Every:   jobExpiryInterval,
			Timeout: jobExpiryTimeout,
			Run:     warnOfExpiry(d, service),
		}
	})

	register(func(d Deps) Task {
		service := jobs.NewService(events.NewOutbox(), d.Clock, nil)

		return Task{
			Name:    "job-auto-complete",
			Every:   autoCompleteInterval,
			Timeout: jobExpiryTimeout,
			Run:     autoCompleteDeliveries(d, service),
		}
	})
}

// jobExpiryInterval is how often each sweep runs.
//
// Docs/02 §6.3 measures expiry in days, so the precision this needs is coarse; five minutes is
// chosen against the other end instead — the longest a job should keep taking bids after its
// pickup window has closed. It is short enough that a provider is never looking at a listing that
// died half an hour ago, and long enough that an idle marketplace runs one indexed query per
// worker per five minutes.
//
// A constant rather than configuration, deliberately. internal/config is a shared surface a
// domain branch does not edit (Docs/10 §9.2), and nothing here changes under operational pressure
// in the way a validation limit does: the deadline itself is per job and already stored, so
// tuning this changes only how promptly a decision that has already been made is acted on.
//
// SHIP-69's warning sweep shares it rather than declaring a second one. Both read the same column
// and the warning's precision requirement is coarser still — forty-eight hours, measured to the
// nearest five minutes — so a separate constant would be a second number that could only ever
// disagree with this one by accident.
const jobExpiryInterval = 5 * time.Minute

// jobExpiryTimeout bounds one pass.
//
// Longer than the manifest's default because a pass does up to ExpiryBatch transitions, each of
// which locks a row, writes a history row and writes an event — and all of it in one transaction
// that must not be cut off halfway through a slow moment on the database. Still far shorter than
// the interval, so a wedged pass is ended well before the next one would start.
const jobExpiryTimeout = 2 * time.Minute

// expireJobs is one pass: claim the Open jobs whose deadline has passed, and end each of them.
//
// # Both halves are inside the caller's transaction, and that is what makes two workers safe
//
// The claim locks the rows with FOR UPDATE SKIP LOCKED and the transitions run against those same
// locks, so a second worker sweeping at the same instant is handed the rows this one did not take
// rather than queueing behind it or doing the same work twice. Nothing coordinates them: there is
// no lease table and no leader election, because a rolling deployment runs two workers as a matter
// of course and this shape is safe by construction (Docs/10 §6.2).
//
// If the pass fails part way, the transaction rolls back and every claimed job is released
// unexpired. The next pass finds them again, which is the property that makes a crashed worker
// harmless.
//
// # The clock is the worker's, not the database's
//
// jobs.ExpiryClaim takes the instant to judge against as a parameter. That is Docs/10 §6.3 being
// useful rather than ceremonial: a test advances a clock.Fixed and watches a job expire, instead
// of waiting fourteen days or writing a deadline into the past to fake one.
func expireJobs(d Deps, service *jobs.Service) Work {
	return func(ctx context.Context, r db.Runner) (int, error) {
		due, err := ClaimIDs(ctx, r, jobs.ExpiryClaim, d.Clock.Now(), jobs.ExpiryBatch)
		if err != nil {
			return 0, err
		}

		for _, id := range due {
			if _, err := service.Expire(ctx, r, id); err != nil {
				// Returned rather than logged and skipped. One job that cannot be
				// expired means the rule is not being applied, and a pass that
				// reported success while quietly leaving jobs open is the failure
				// this whole arrangement exists to avoid. The transaction rolls
				// back, the claim is released, and the next pass tries again — with
				// the failure in the log rather than in nobody's hands.
				return 0, err
			}

			// Debug rather than info: the pass already reports how many it claimed, and
			// a marketplace clearing a backlog would otherwise write a line per job. This
			// is the line somebody greps for when one particular job is asked about.
			d.Logger.Debug("job expired",
				slog.String("job_id", id.String()),
				slog.Time("judged_at", d.Clock.Now()))
		}
		return len(due), nil
	}
}

// warnOfExpiry is one pass of SHIP-69: claim the Open jobs coming up on their deadline that have
// not been told, and tell each of them.
//
// The same shape as [expireJobs] — claim inside the caller's transaction, act on what was claimed,
// return the count — and it is the same shape on purpose. Two tasks that claim work differently
// would be two things to reason about when a pass misbehaves at three in the morning.
//
// # The horizon is computed here and the forty-eight hours is not
//
// jobs.ExpiryWarning is the domain's constant and this adds it to the worker's clock, so the claim
// receives two instants and contains no interval of its own. That is the same division as the
// expiry sweep's: the domain says what the rule is, the worker says when it is being asked. A test
// moves a clock.Fixed and the window moves with it.
func warnOfExpiry(d Deps, service *jobs.Service) Work {
	return func(ctx context.Context, r db.Runner) (int, error) {
		now := d.Clock.Now()

		due, err := ClaimIDs(ctx, r, jobs.ExpiryWarningClaim,
			now, now.Add(jobs.ExpiryWarning), jobs.ExpiryBatch)
		if err != nil {
			return 0, err
		}

		for _, id := range due {
			if _, err := service.WarnOfExpiry(ctx, r, id); err != nil {
				// Returned rather than logged and skipped, exactly as the expiry sweep
				// does. The transaction rolls back, every claimed job keeps its NULL
				// mark, and the next pass finds them again — so a partial failure costs
				// a five-minute delay rather than a warning nobody ever receives.
				return 0, err
			}

			d.Logger.Debug("job expiry warning emitted",
				slog.String("job_id", id.String()),
				slog.Time("judged_at", now))
		}
		return len(due), nil
	}
}

// --- the seventy-two hour auto-complete (SHIP-119) ----------------------------------------------

// autoCompleteInterval is how often the auto-complete sweep runs.
//
// Fifteen minutes rather than the five the expiry sweeps use, and the difference is a statement
// about what being late costs. An Open job past its pickup date is actively misleading providers
// every minute it stays listed, which is why that sweep is tuned against how long a dead listing
// may sit. A Delivered job past its window is closed and nobody is looking at it; the customer has
// had three days to object and the transition changes nothing they can act on. Docs/02 §6.1
// measures the rule in days, so a quarter of an hour is three orders of magnitude inside its
// precision.
//
// A constant rather than configuration, for the reason jobExpiryInterval gives: internal/config is
// a shared surface a domain branch does not edit (Docs/10 §9.2), and nothing here changes under
// operational pressure — the deadline is per job and derived from a stored fact, so tuning this
// changes only how promptly a decision already made is acted on.
const autoCompleteInterval = 15 * time.Minute

// autoCompleteDeliveries is one pass: claim the Delivered jobs whose window has passed, and close
// each of them.
//
// The same shape as [expireJobs], deliberately — claim inside the caller transaction, act on what
// was claimed, return the count — because two claim loops that differed would be two things to
// reason about when a pass misbehaves at three in the morning.
//
// # Two clocks meet here, and which owns what is a decision rather than an accident
//
// The deadline is computed from job_status_history.server_recorded_at, which 000401 defaults from
// the database clock and forbids a caller to supply — a caller that could set it could backdate a
// transition, and the whole value of that column is that one of the two timestamps is the
// platform's. The instant it is judged against is d.Clock.Now(), the injected Go clock, because
// Docs/10 §6.3 puts every scheduled task behind one and a query that asked the database for the
// time would be a sweep no test could move without waiting three days.
//
// So the row timestamp is the database clock and the judgement is the Go clock, and a test that
// pinned a clock.Fixed to an absolute date would be comparing the two — which passes today and
// fails permanently on some later date, because the fixture clock stands still and now() does not.
// The tests around this anchor the fixed clock to the server_recorded_at they read back, so the
// seventy-two hours is measured between two instants that came from the same clock and the test
// cannot expire. autocomplete_test.go says so where it does it.
func autoCompleteDeliveries(d Deps, service *jobs.Service) Work {
	return func(ctx context.Context, r db.Runner) (int, error) {
		judgedAt := d.Clock.Now().Add(-jobs.AutoCompleteWindow)

		due, err := ClaimIDs(ctx, r, jobs.AutoCompleteClaim, judgedAt, jobs.AutoCompleteBatch)
		if err != nil {
			return 0, err
		}

		for _, id := range due {
			if _, err := service.AutoComplete(ctx, r, id); err != nil {
				// Returned rather than logged and skipped, as both expiry sweeps do.
				// The transaction rolls back, every claimed job stays Delivered, and
				// the next pass finds them again — with the failure in the log rather
				// than in nobody's hands.
				return 0, err
			}

			d.Logger.Debug("delivery auto-completed",
				slog.String("job_id", id.String()),
				slog.Time("delivered_before", judgedAt))
		}
		return len(due), nil
	}
}
