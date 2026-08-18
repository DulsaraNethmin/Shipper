package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/fleet"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
)

// The identity domain's scheduled work (SHIP-171).
//
// The **sixth** registered task, and the first one whose subject is a person rather than a job, an
// offer or a row of the outbox. Docs/05 §3.1 gives the platform thirty days to execute a deletion
// request and puts execution out of an ordinary administrator's reach, so it happens here or it
// happens nowhere.
//
// # What a pass does to every other registered task, which is the paragraph manifest_test.go asks for
//
// cmd/worker is one binary and a start runs **every** registered task, so this now runs inside every
// `scripts/verify` section that starts the worker — 50-jobs.sh, 51-jobs-autocomplete.sh, 61-bidding.sh,
// 80-notifications.sh and 81-notifier.sh — whether or not any of them mentions deletion.
//
// **It claims what is *due*, which is the property that makes the standing convention sufficient.**
// Docs/11 §9 settled the `--only=<task>` question as "fence what you assert on, own what you assert
// about", and named exactly one case that would reopen it: *a task that sweeps rows due by wall-clock
// alone.* This is not that task. A request is due when `complete_by` has passed, and `complete_by` is
// a **stored** column written from the row's own request time plus [identity.DeletionWindow] — thirty
// days — so every request any endpoint can create is thirty days away from being claimable at the
// moment it is created. That is `job-expiry`'s shape and `job-auto-complete`'s, and it is
// conspicuously not `outbox-publisher`'s, which has no "not due" state at all.
//
// **What it does to the existing sections is nothing, and that was measured rather than assumed.**
// `account_deletion_requests` is written by exactly one section — 40-identity.sh, which sorts before
// every section that starts the worker — and every row it leaves behind was created through
// `POST /v1/account/deletion` with a `complete_by` thirty days out. No section backdates one, and a
// `grep -rn 'account_deletion' scripts/verify/` returns that file and nothing else. So a worker
// started in section 50, 51, 61, 80 or 81 finds nothing due and pseudonymises nobody.
//
// **That mattered more here than for the five tasks before it**, and the reason is worth stating:
// this is the only task that can reach a row another section is still using. An expiry moves a job
// or an offer somebody else created; a pass of this one would replace a *user's* email address, and
// every section downstream signs somebody in.
func init() {
	register(func(d Deps) Task {
		// Two ports, both required, and the composition root is where they meet the domain.
		//
		// activeJobs is a second copy of cmd/api's activeJobLookup, exactly as presentedJobs
		// in tasks_bidding.go is a second copy of routes_bidding.go's — two binaries, neither
		// importing the other, both satisfying one interface `internal/identity` declared.
		// What keeps the copies honest is that both are exercised: that one by the deletion
		// endpoint and 40-identity.sh, this one by tasks_identity_test.go against a real
		// database.
		executor, err := identity.NewPseudonymiser(
			activeJobLookup{},
			personalDetails{fleet: fleet.NewService(d.Clock)},
			d.Clock)
		if err != nil {
			// init-time, like every other registration: a malformed task is a programming
			// mistake and the alternative to stopping at startup is a worker that runs
			// everything except the one thing somebody got wrong.
			panic("worker: identity pseudonymiser: " + err.Error())
		}

		return Task{
			Name:    "account-pseudonymisation",
			Every:   pseudonymisationInterval,
			Timeout: pseudonymisationTimeout,
			Run:     pseudonymiseDueAccounts(d, executor),
		}
	})
}

// pseudonymisationInterval is how often the sweep runs.
//
// An hour rather than the five minutes the job and bid sweeps use, and the difference is what the
// deadline is made of. A collection time is a date and an hour and a customer is looking at the
// offer; a deletion window is **thirty days**, published in a privacy policy, and nobody is watching
// the minute it expires. Docs/05 §3.1 promises execution *within* thirty days, so an hour of latency
// against a thirty-day promise is four hundredths of one per cent of it.
//
// The other end of the argument is cost: this is the most expensive pass in the binary per claimed
// row — five tables and a session revocation each — and running it twelve times more often would
// mean twelve times as many empty passes for a marketplace where deletion is rare.
//
// A constant rather than configuration, on tasks_jobs.go's reasoning: internal/config is a shared
// surface a domain branch does not edit (Docs/10 §9.2), and the deadline itself is per request and
// already stored, so tuning this changes only how promptly a decision the request already made is
// acted on.
const pseudonymisationInterval = time.Hour

// pseudonymisationTimeout bounds one pass.
//
// Longer than the manifest default and longer than the other sweeps', because a pass does up to
// [identity.DeletionBatch] executions and each one writes five tables inside the single transaction
// the pass holds. Still far shorter than the interval, so a wedged pass ends well before the next
// one would start.
const pseudonymisationTimeout = 5 * time.Minute

// pseudonymiseDueAccounts is one pass: claim the open requests whose promised date has arrived, and
// decide each.
//
// The shape tasks_jobs.go established and tasks_bidding.go repeated — claim inside the caller's
// transaction, act on what was claimed, return the count — deliberately, because two tasks that
// claimed work differently would be two things to reason about when a pass misbehaves at three in
// the morning.
//
// **The count is claims rather than executions**, which is the same thing every other task returns
// and is not the same thing here: a claimed request may be held or restarted rather than executed
// (see [identity.Pseudonymiser.Execute]). The three outcomes are logged per row, so a pass that
// claimed twenty and executed none is legible as twenty people mid-delivery rather than as a stuck
// sweep.
func pseudonymiseDueAccounts(d Deps, executor *identity.Pseudonymiser) Work {
	return func(ctx context.Context, r db.Runner) (int, error) {
		due, err := ClaimIDs(ctx, r, identity.DueDeletionClaim, d.Clock.Now(), identity.DeletionBatch)
		if err != nil {
			return 0, err
		}

		for _, id := range due {
			outcome, err := executor.Execute(ctx, r, id)
			if err != nil {
				// Returned rather than logged and skipped, exactly as the other sweeps
				// do. The transaction rolls back, every claim is released, and the next
				// pass tries again with the failure in the log — which for this task is
				// the only safe direction, because a partial execution would leave an
				// account pseudonymised in one table and named in another.
				return 0, err
			}

			// **Info rather than debug, and it is the one line here worth keeping.**
			// Every other sweep logs at debug because a marketplace clearing a backlog
			// would write a line per row; a person being erased is rare, irreversible and
			// has a legal clock attached, and "when did this happen" is the first question
			// asked afterwards.
			//
			// The request identifier and the outcome, and **nothing about the person** —
			// not the account identifier, not the name that was replaced, not the address.
			// A log line naming the account beside the moment it was pseudonymised would
			// be a reverse mapping in a file, which is the one thing this ticket claims
			// does not exist anywhere.
			d.Logger.Info("account deletion request executed",
				slog.String("deletion_request_id", id.String()),
				slog.String("outcome", string(outcome)),
				slog.Time("judged_at", d.Clock.Now()))
		}
		return len(due), nil
	}
}

// activeJobLookup implements identity.ActiveJobs over `jobs` and `bids` (SHIP-170, SHIP-171).
//
// **A second copy of cmd/api/routes_identity.go's adapter of the same name**, and the duplication is
// deliberate rather than an oversight, for the reason presentedJobs gives above it: the two
// composition roots are two binaries, neither imports the other, and `internal/identity` names
// neither `jobs` nor `bids` nor this type. What keeps the copies honest is that both satisfy one
// interface and both are exercised.
//
// It reads `jobs`, which is `internal/jobs`', and `bids`, which is `internal/bidding`'s, and
// `internal/identity` may import neither — the boundary lint refuses both directions. The
// composition root is where a dependency between domains is allowed to be visible.
type activeJobLookup struct{}

// activeJobStatuses is Docs/02 §1's six committed statuses, in the document's own order, and is
// character for character cmd/api's constant of the same name.
//
// Written out rather than expressed as a complement: "not Draft, Open, Negotiating, Completed,
// Cancelled or Disputed" would silently absorb a thirteenth status into the *deferring* set, which
// is the direction that quietly stops people being deleted.
//
// The range is Docs/05 §3.1's own — "a request made between Awarded and Delivered" — inclusive at
// both ends, because a job at `Delivered` can still move to `Disputed` and erasing a party at that
// moment is precisely what strands the counterparty. `Disputed` is deliberately absent: a dispute is
// a delivery that has stopped rather than one in flight.
const activeJobStatuses = `('Awarded', 'Driver assigned', 'En route to pickup', 'Picked up', 'In transit', 'Delivered')`

// HasActiveJob reports whether this account is party to a job in flight, on either side.
//
// Both parties, and the OR is the whole of it: the customer whose goods are moving
// (`jobs.customer_id`) or the provider whose offer was accepted (`bids.provider_id` where the bid is
// `Accepted`). A query that checked only the customer would pass every customer-side assertion and
// erase a driver mid-delivery.
//
// `uq_bids_one_accepted_per_job` is partial on `status = 'Accepted'`, so the LEFT JOIN cannot
// multiply rows however many bids a job carries. `EXISTS` rather than a count: the question is yes
// or no, and the planner can stop at the first row.
func (activeJobLookup) HasActiveJob(ctx context.Context, r db.Runner, userID uuid.UUID) (bool, error) {
	const q = `
		SELECT EXISTS (
		       SELECT 1
		         FROM jobs j
		         LEFT JOIN bids b ON b.job_id = j.id AND b.status = 'Accepted'
		        WHERE j.status IN ` + activeJobStatuses + `
		          AND (j.customer_id = $1 OR b.provider_id = $1))`

	var active bool
	if err := r.QueryRow(ctx, q, userID).Scan(&active); err != nil {
		return false, fmt.Errorf("worker: reading whether %s is carrying a delivery: %w", userID, err)
	}
	return active, nil
}

// personalDetails implements identity.PersonalDetails over the two stores outside
// `internal/identity` that keep a copy of the account holder's own details (SHIP-171).
//
// # The two, and how they were found
//
// A sweep of every text column in the schema for a copy of a name, an address or a number that can
// be joined back to an account. Two qualify:
//
//   - **`provider_profiles.display_name`** (000303) — what a provider trades under, keyed by
//     `provider_id`. Profile data by Docs/05 §3.1's own division, and reached through
//     `fleet.Service` rather than by a statement here, because that domain owns what a trading name
//     may be and this file has no business holding a second opinion about it.
//   - **`notifications.address`** (000700) — "an email address, or an E.164 number. Resolved once,
//     when the row is written, from users", keyed by `recipient_id`. It is contact data by any
//     reading, and leaving it would make the criterion false in one join.
//
// Three more were considered and are out of reach or out of scope, and each is recorded in
// Docs/11 §3 rather than left for somebody to rediscover: `driver_assignments.driver_name` and
// `driver_mobile` (000600) carry **no account reference at all**, because a driver may be a third
// party with no account, so nothing here can decide whether a given row is the provider's own
// details; `job_messages.body` and `disputes.description` are prose somebody wrote rather than
// identifying columns, and are SHIP-172's; `device_tokens.token` is a device identifier and is
// named in SHIP-172's own *Done when*.
//
// # Why `notifications` is written from here rather than through its domain
//
// `internal/notifications` declares no port for this and nothing in it has a reading of deletion.
// The precedent is the file this one sits beside: `cmd/api/routes_bidding.go` reads `vehicles` and
// `users` with statements of its own, and `activeJobLookup` above reads `jobs` and `bids`. The
// asymmetry with `provider_profiles` is deliberate and is the same asymmetry `routes_bidding.go`
// records — a column with a policy attached is reached through the domain that owns the policy, and
// a column that is simply a stored value is reached directly.
type personalDetails struct {
	fleet *fleet.Service
}

// Replace overwrites both copies and reports how many rows changed.
//
// Zero is an ordinary answer: a customer has no provider profile, and an account that has never been
// notified has no notifications. What is not ordinary is an error, and either statement failing
// aborts the caller's transaction — so the account is not pseudonymised in one table and named in
// another, and the request stays open for the next pass.
func (p personalDetails) Replace(
	ctx context.Context,
	r db.Runner,
	userID uuid.UUID,
	with identity.Pseudonym,
) (int, error) {
	if p.fleet == nil {
		return 0, fmt.Errorf("worker: no fleet service is wired, so %s keeps their trading name", userID)
	}

	changed := 0
	renamed, err := p.fleet.PseudonymiseProfile(ctx, r, userID, with.TradingName)
	if err != nil {
		return 0, err
	}
	if renamed {
		changed++
	}

	// **`push` is excluded, and that is a scope line rather than an omission.** On an email or
	// SMS row `address` is the person's address or number; on a push row it is a device token,
	// which is a device identifier and is named in SHIP-172's *Done when* rather than this one's.
	// Writing a contact pseudonym over a device token would also destroy the only value that says
	// which handset a failed push was aimed at.
	//
	// One token for both channels, because [identity.PseudonymFor] writes one into both contact
	// columns — so there is no CASE here and nothing that has to know which channel is which.
	tag, err := r.Exec(ctx, `
		UPDATE notifications
		   SET address = $2
		 WHERE recipient_id = $1 AND channel IN ('email', 'sms')`, userID, with.Token)
	if err != nil {
		return 0, fmt.Errorf("worker: pseudonymising the notification addresses of %s: %w", userID, err)
	}
	changed += int(tag.RowsAffected())

	return changed, nil
}

// Compile-time proof that both adapters satisfy the ports identity declared, which is the only place
// in the build where that can be established — identity names neither type, and neither type names
// any domain but identity and fleet.
var (
	_ identity.ActiveJobs      = activeJobLookup{}
	_ identity.PersonalDetails = personalDetails{}
)
