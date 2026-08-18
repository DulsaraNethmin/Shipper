package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-171 — "profile and contact data are irreversibly replaced by a stable pseudonym",
// demonstrated as a pass of the real registered task against a real database.
//
// The rules themselves are internal/identity's and are tested there: which requests are due, what a
// pseudonym is made of, what happens to somebody mid-delivery, and that nothing in the schema still
// holds the person afterwards. **What is tested here is the half that only exists as a running
// process** — that the registration is in the manifest, that the claim is a claim, and above all
// that the two adapters do what the domain's test doubles stood in for.
//
// That last one is the point of this file. `internal/identity` cannot name `provider_profiles`,
// `notifications`, `jobs` or `bids`, so its tests answer through ports it controls, which proves the
// rule and proves nothing about the statements behind it. Wave 12 recorded what happens when only
// one of the two layers is tested.

// pseudonymisationTask builds the registered task exactly as main would, over the given clock.
//
// Through `tasks()` rather than by calling [pseudonymiseDueAccounts] directly, for
// [registeredTask]'s reason: the registration in `init` is the thing a merge can drop, and a test
// that reached past it would keep passing after the task had stopped being registered.
func pseudonymisationTask(t *testing.T, pool *pgxpool.Pool, at clock.Clock) Task {
	t.Helper()
	return registeredTask(t, pool, at, "account-pseudonymisation")
}

// TestThePseudonymisationTaskIsDeclaredSensibly is the declaration rather than the work.
func TestThePseudonymisationTaskIsDeclaredSensibly(t *testing.T) {
	pool := pgtest.DB(t)
	task := pseudonymisationTask(t, pool, clock.System{})

	switch {
	case task.Every <= 0:
		t.Errorf("%s has no interval, so it would never run twice", task.Name)
	case task.timeout() >= task.Every:
		t.Errorf("%s: the timeout %s is not shorter than the interval %s",
			task.Name, task.timeout(), task.Every)
	case task.Close != nil:
		// It owns nothing: the pass runs queries inside the caller's transaction. A Close
		// here would mean a resource somebody added without saying why (SHIP-15g).
		t.Errorf("%s declares a Close, but it owns no long-lived resource", task.Name)
	}
}

// TestTheDueDeletionClaimIsAClaim is cmd/worker's own rule applied to the sixth task's query.
//
// Asserted directly as well as through [ClaimIDs], so that a domain constant edited into something
// that is no longer a claim is reported by a named test rather than by whichever pass happened to
// run. It matters more here than for the other sweeps: two workers claiming one deletion request
// would each pseudonymise the same account, and the second would fail on a row it had already
// rewritten rather than doing nothing.
func TestTheDueDeletionClaimIsAClaim(t *testing.T) {
	if err := checkClaim(identity.DueDeletionClaim); err != nil {
		t.Errorf("identity.DueDeletionClaim is not a claim: %v", err)
	}
}

// deletable is one account with every artefact the sweep has to reach.
type deletable struct {
	user      uuid.UUID
	email     string
	phone     string
	name      string
	requestID uuid.UUID
}

// newDeletable writes a provider with a public profile, three notifications and a deletion request
// whose promised date is already in the past.
//
// The rows are written directly rather than through the API, because what is being tested is the
// adapter's reach across four tables rather than how any of them came to be filled. `complete_by` is
// backdated here — the domain's own tests move a clock instead, which is the honest demonstration
// that the column is stored; this file needs a due row and does not need to re-prove that.
func newDeletable(t *testing.T, pool *pgxpool.Pool, suffix string) deletable {
	t.Helper()

	d := deletable{
		email: fmt.Sprintf("worker-pseudonym-%s@example.com", suffix),
		phone: "+6140000" + suffix,
		name:  "Worker Deletable " + suffix,
	}

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	d.user = id

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO users (id, name, email, phone, password_hash, role)
		VALUES ($1, $2, $3, $4, 'x', 'provider')`,
		d.user, d.name, d.email, d.phone); err != nil {
		t.Fatalf("inserting the account: %v", err)
	}

	// fleet's table, reached through fleet.Service by the adapter under test.
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO provider_profiles (provider_id, display_name, operates_as)
		VALUES ($1, $2, 'business')`,
		d.user, "Deletable Removals "+suffix); err != nil {
		t.Fatalf("declaring a trading name: %v", err)
	}

	// A job for the notifications to hang off. `notifications.job_id` is NOT NULL, because a
	// notification in this platform is always about a delivery. It is left at `Draft`, which is
	// not one of Docs/02 §1's six committed statuses, so it does not make this account a party
	// to anything — the delivery fixture below is what does that, deliberately and separately.
	var job uuid.UUID
	if err := pool.QueryRow(t.Context(),
		`INSERT INTO jobs (id, customer_id) VALUES (gen_random_uuid(), $1) RETURNING id`,
		d.user).Scan(&job); err != nil {
		t.Fatalf("creating the job the notifications hang off: %v", err)
	}

	// notifications' table. Three rows on purpose: the two contact channels the sweep must
	// replace, and a push row whose `address` is a *device token* and must survive — writing a
	// contact pseudonym over it would destroy the only value saying which handset a failed push
	// was aimed at, and device tokens are SHIP-172's.
	for _, n := range []struct{ channel, address string }{
		{"email", d.email},
		{"sms", d.phone},
		{"push", "device-token-" + suffix},
	} {
		event, err := uuid.NewV7()
		if err != nil {
			t.Fatalf("generating an event id: %v", err)
		}
		row, err := uuid.NewV7()
		if err != nil {
			t.Fatalf("generating a notification id: %v", err)
		}
		if _, err := pool.Exec(t.Context(), `
			INSERT INTO notifications
			    (id, event_id, event_type, job_id, recipient_id, channel, category, essential,
			     address, subject, body)
			VALUES ($1, $2, 'job.status_changed', $3, $4, $5, 'award', true, $6, 's', 'b')`,
			row, event, job, d.user, n.channel, n.address); err != nil {
			t.Fatalf("writing a %s notification: %v", n.channel, err)
		}
	}

	requested := time.Now().UTC().Add(-31 * 24 * time.Hour)
	if err := pool.QueryRow(t.Context(), `
		INSERT INTO account_deletion_requests (id, user_id, state, requested_at, complete_by)
		VALUES (gen_random_uuid(), $1, 'requested', $2, $3)
		RETURNING id`,
		d.user, requested, requested.Add(identity.DeletionWindow)).Scan(&d.requestID); err != nil {
		t.Fatalf("recording a due deletion request: %v", err)
	}
	return d
}

// TestAPassPseudonymisesTheAccountsWhoseWindowHasClosed is SHIP-171's *Done when* through the
// registered task, and it is the only test in the repository that exercises both adapters.
func TestAPassPseudonymisesTheAccountsWhoseWindowHasClosed(t *testing.T) {
	pool := pgtest.DB(t)
	subject := newDeletable(t, pool, "17100")

	// A second account with a request that is not due, so the pass is shown to claim by the
	// promised date rather than to sweep whatever it finds. This is the property every verify
	// section that starts cmd/worker depends on.
	bystander := newDeletable(t, pool, "17101")
	if _, err := pool.Exec(t.Context(),
		`UPDATE account_deletion_requests SET complete_by = now() + interval '20 days' WHERE id = $1`,
		bystander.requestID); err != nil {
		t.Fatalf("moving the bystander's promise into the future: %v", err)
	}

	if claimed := runPass(t, pool, pseudonymisationTask(t, pool, clock.System{})); claimed != 1 {
		t.Fatalf("the pass claimed %d requests, want exactly the one that is due", claimed)
	}

	want := identity.PseudonymFor(subject.user)

	t.Run("the account and both contact channels", func(t *testing.T) {
		var name, email, phone string
		if err := pool.QueryRow(t.Context(),
			`SELECT name, email::text, phone FROM users WHERE id = $1`, subject.user).
			Scan(&name, &email, &phone); err != nil {
			t.Fatalf("reading the account: %v", err)
		}
		if name != want.Name || email != want.Token || phone != want.Token {
			t.Errorf("users = (%q, %q, %q), want (%q, %q, %q)",
				name, email, phone, want.Name, want.Token, want.Token)
		}
	})

	t.Run("the trading name, through fleet rather than a statement in the worker", func(t *testing.T) {
		var displayName string
		if err := pool.QueryRow(t.Context(),
			`SELECT display_name FROM provider_profiles WHERE provider_id = $1`, subject.user).
			Scan(&displayName); err != nil {
			t.Fatalf("reading the trading name: %v", err)
		}
		if displayName != want.TradingName {
			t.Errorf("provider_profiles.display_name = %q, want %q", displayName, want.TradingName)
		}
	})

	t.Run("the notification addresses, and not the device token", func(t *testing.T) {
		rows, err := pool.Query(t.Context(),
			`SELECT channel, address FROM notifications WHERE recipient_id = $1 ORDER BY channel`,
			subject.user)
		if err != nil {
			t.Fatalf("reading the notifications: %v", err)
		}
		defer rows.Close()

		got := map[string]string{}
		for rows.Next() {
			var channel, address string
			if err := rows.Scan(&channel, &address); err != nil {
				t.Fatalf("scanning a notification: %v", err)
			}
			got[channel] = address
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("reading the notifications: %v", err)
		}

		if got["email"] != want.Token {
			t.Errorf("the email notification is addressed to %q, want %q — a join from this "+
				"table recovers the person from a column nobody was thinking about",
				got["email"], want.Token)
		}
		if got["sms"] != want.Token {
			t.Errorf("the SMS notification is addressed to %q, want %q", got["sms"], want.Token)
		}
		if got["push"] != "device-token-17100" {
			t.Errorf("the push row's address is %q — that column holds a device identifier, "+
				"which is SHIP-172's, and overwriting it destroys which handset it named",
				got["push"])
		}
	})

	t.Run("and the account whose promise has not arrived is untouched", func(t *testing.T) {
		var email string
		if err := pool.QueryRow(t.Context(),
			`SELECT email::text FROM users WHERE id = $1`, bystander.user).Scan(&email); err != nil {
			t.Fatalf("reading the bystander: %v", err)
		}
		if email != bystander.email {
			t.Errorf("the bystander's address is %q — a pass that sweeps rows that are not due "+
				"would be executing deletions inside every verify section that starts a worker",
				email)
		}
	})

	t.Run("and a second pass finds nothing left to do", func(t *testing.T) {
		if claimed := runPass(t, pool, pseudonymisationTask(t, pool, clock.System{})); claimed != 0 {
			t.Errorf("a second pass claimed %d requests; a completed request must fall out of "+
				"the claim or the sweep re-executes it forever", claimed)
		}
	})
}

// TestAPassLeavesAPartyToADeliveryAlone is the real [activeJobLookup] rather than a double, which is
// the half `internal/identity` cannot test.
//
// Docs/05 §3.1: "Erasing a party mid-delivery would strand the counterparty." The provider here is
// the *awarded* one, which is the side a lookup reading `jobs.customer_id` alone would miss — and
// missing it is the defect that deletes a driver mid-delivery.
func TestAPassLeavesAPartyToADeliveryAlone(t *testing.T) {
	pool := pgtest.DB(t)
	carrier := newDeletable(t, pool, "17102")

	customer, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO users (id, email, phone, password_hash, role)
		VALUES ($1, 'worker-pseudonym-customer@example.com', '+61400017102', 'x', 'customer')`,
		customer); err != nil {
		t.Fatalf("inserting the customer: %v", err)
	}

	var job uuid.UUID
	if err := pool.QueryRow(t.Context(),
		`INSERT INTO jobs (id, customer_id) VALUES (gen_random_uuid(), $1) RETURNING id`,
		customer).Scan(&job); err != nil {
		t.Fatalf("creating the job: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO bids (id, job_id, provider_id, status, amount, pickup_at, deliver_by)
		VALUES (gen_random_uuid(), $1, $2, 'Accepted', 185.00,
		        now() + interval '2 days', now() + interval '3 days')`,
		job, carrier.user); err != nil {
		t.Fatalf("accepting the bid: %v", err)
	}
	moveJobThroughTheGuard(t, pool, job, customer, "Draft", "Open")
	moveJobThroughTheGuard(t, pool, job, customer, "Open", "Awarded")

	if claimed := runPass(t, pool, pseudonymisationTask(t, pool, clock.System{})); claimed != 1 {
		t.Fatalf("the pass claimed %d requests, want the one that is due", claimed)
	}

	var email, state string
	if err := pool.QueryRow(t.Context(), `
		SELECT u.email::text, r.state
		  FROM users u JOIN account_deletion_requests r ON r.user_id = u.id
		 WHERE u.id = $1`, carrier.user).Scan(&email, &state); err != nil {
		t.Fatalf("reading the carrier: %v", err)
	}

	if email != carrier.email {
		t.Errorf("the provider carrying the delivery was pseudonymised — the customer's goods "+
			"are moving and nobody is named as carrying them (%q)", email)
	}
	if state != string(identity.DeletionDeferred) {
		t.Errorf("the request is %q, want %q — Docs/05 §3.1 defers rather than refuses",
			state, identity.DeletionDeferred)
	}
}

// moveJobThroughTheGuard walks a job to a status the way 000402 requires.
//
// The status column is not settable: an UPDATE has to be accompanied, in the same transaction, by a
// `job_status_history` row describing it and named to the trigger through a transaction-local
// setting. This is that protocol, and it is a local copy of the one in
// `cmd/api/routes_identity_test.go` and `scripts/verify/40-identity.sh` for the reason presentedJobs
// gives: two composition roots are two binaries and test helpers do not cross a package boundary.
func moveJobThroughTheGuard(t *testing.T, pool *pgxpool.Pool, job, actor uuid.UUID, from, to string) {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("opening the transition transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(t.Context()) }()

	var transition uuid.UUID
	if err := tx.QueryRow(t.Context(), `
		INSERT INTO job_status_history
		    (id, job_id, from_status, to_status, actor_type, actor_id, actor_recorded_at)
		VALUES (gen_random_uuid(), $1, $2, $3, 'customer', $4, now())
		RETURNING id`, job, from, to, actor).Scan(&transition); err != nil {
		t.Fatalf("recording %s -> %s: %v", from, to, err)
	}
	if _, err := tx.Exec(t.Context(),
		`SELECT set_config('shipper.job_status_transition', $1, true)`, transition.String()); err != nil {
		t.Fatalf("naming the transition to the guard: %v", err)
	}
	if _, err := tx.Exec(t.Context(),
		`UPDATE jobs SET status = $2 WHERE id = $1`, job, to); err != nil {
		t.Fatalf("moving the job to %s: %v", to, err)
	}
	if err := tx.Commit(t.Context()); err != nil {
		t.Fatalf("committing %s -> %s: %v", from, to, err)
	}
}
