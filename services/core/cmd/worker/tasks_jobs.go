package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
)

// The jobs domain's scheduled work (SHIP-68, and SHIP-69 when it lands).
//
// This file exists so that adding a task adds a file and edits none, exactly as
// cmd/api/routes_jobs.go does for routes. manifest.go says why: four tasks in the backlog belong
// to three domains and three branches, and a registration dropped in a merge produces a task that
// silently never runs — which is worse than a dropped route, because nobody calls a task and
// notices it is gone.
//
// Nothing was added to Deps to make this work, which is the test manifest.go set for itself: a
// domain service is a pure function of the pool, the clock and configuration.

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
}

// jobExpiryInterval is how often the sweep runs.
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
