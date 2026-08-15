package main

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// The one query in this binary, tested against a real database (SHIP-137).
//
// # Why this file exists at all
//
// Because the alternative is the failure `Docs/11` §9 records against `internal/bidding`: a lock,
// or in this case a join, that lives in `cmd/` where nothing has a database and is therefore
// covered by no Go test at all — so removing `SKIP LOCKED` from it passes `make check` with zero
// failures. `cmd/api` has had `pgtest` fixtures since SHIP-30 and this binary can have them too;
// the composition root being thin is not a reason for the query inside it to be unverified.
//
// The domain's tests stub this port, which is right — internal/notifications must not know that
// "who is on this job" is a join across two other domains' tables. What a stub cannot show is that
// the join is the correct one, and `make verify` demonstrating it end to end shows only the happy
// path. The three cases below are the ones that differ.

// partyFixture is a job with a customer, and optionally an accepted bid.
func partyFixture(t *testing.T, pool *pgxpool.Pool, email, phone string, awarded bool) (job, customer, provider uuid.UUID) {
	t.Helper()

	customer = uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO users (id, email, phone, password_hash, role) VALUES ($1, $2, $3, 'x', 'customer')`,
		customer, "customer-"+email, phone+"1"); err != nil {
		t.Fatalf("inserting the customer: %v", err)
	}

	provider = uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO users (id, email, phone, password_hash, role) VALUES ($1, $2, $3, 'x', 'provider')`,
		provider, "provider-"+email, phone+"2"); err != nil {
		t.Fatalf("inserting the provider: %v", err)
	}

	job = uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO jobs (id, customer_id) VALUES ($1, $2)`, job, customer); err != nil {
		t.Fatalf("inserting the job: %v", err)
	}

	// A losing bid always, so that the LEFT JOIN has more than one row to choose between and a
	// query that forgot `AND b.status = 'Accepted'` would return the wrong provider or multiply
	// the row.
	loser := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO users (id, email, phone, password_hash, role) VALUES ($1, $2, $3, 'x', 'provider')`,
		loser, "loser-"+email, phone+"3"); err != nil {
		t.Fatalf("inserting the losing provider: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO bids (id, job_id, provider_id, status, amount) VALUES ($1, $2, $3, 'Rejected', 500.00)`,
		uuid.Must(uuid.NewV7()), job, loser); err != nil {
		t.Fatalf("inserting the losing bid: %v", err)
	}

	if awarded {
		if _, err := pool.Exec(t.Context(),
			`INSERT INTO bids (id, job_id, provider_id, status, amount) VALUES ($1, $2, $3, 'Accepted', 450.00)`,
			uuid.Must(uuid.NewV7()), job, provider); err != nil {
			t.Fatalf("accepting a bid: %v", err)
		}
	}
	return job, customer, provider
}

// TestPartiesOnAnAwardedJobNamesBothSides is the case every award, cancellation and completion
// notification depends on.
//
// The losing bid in the fixture is the point: uq_bids_one_accepted_per_job is partial on
// `status = 'Accepted'`, so the join cannot multiply — but only because it filters on that status,
// and a query that dropped the filter would return whichever bid the planner reached first.
func TestPartiesOnAnAwardedJobNamesBothSides(t *testing.T) {
	pool := pgtest.DB(t)

	job, customer, provider := partyFixture(t, pool, "awarded@example.com", "+6140000737", true)

	gotCustomer, gotProvider, found, err := jobPartiesLookup{}.PartiesOn(t.Context(), pool, job)
	if err != nil {
		t.Fatalf("reading the parties: %v", err)
	}
	switch {
	case !found:
		t.Fatal("the job was not found")
	case gotCustomer != customer:
		t.Errorf("the customer is %s, want %s", gotCustomer, customer)
	case gotProvider != provider:
		t.Errorf("the provider is %s, want %s — the accepted bid's, not the rejected one's",
			gotProvider, provider)
	}
}

// TestPartiesOnAnUnawardedJobNamesTheCustomerAlone is the case a bid notification hits, and the one
// a rule with ToAwardedProvider has to survive: a job that has bids and no winner.
//
// notifications.resolve drops a uuid.Nil audience rather than failing, so a cancellation before
// award reaches the customer alone.
func TestPartiesOnAnUnawardedJobNamesTheCustomerAlone(t *testing.T) {
	pool := pgtest.DB(t)

	job, customer, _ := partyFixture(t, pool, "unawarded@example.com", "+6140000738", false)

	gotCustomer, gotProvider, found, err := jobPartiesLookup{}.PartiesOn(t.Context(), pool, job)
	if err != nil {
		t.Fatalf("reading the parties: %v", err)
	}
	switch {
	case !found:
		t.Fatal("a job with bids and no winner was not found")
	case gotCustomer != customer:
		t.Errorf("the customer is %s, want %s", gotCustomer, customer)
	case gotProvider != uuid.Nil:
		t.Errorf("an unawarded job named provider %s; the only bid on it was rejected", gotProvider)
	}
}

// TestPartiesOnAJobThatIsGoneIsNotAnError is what keeps a consumer catching up after an outage from
// parking a partition on an event about a job that has since been pruned or pseudonymised
// (Docs/05 §3.1, SHIP-171).
func TestPartiesOnAJobThatIsGoneIsNotAnError(t *testing.T) {
	pool := pgtest.DB(t)

	customer, provider, found, err := jobPartiesLookup{}.PartiesOn(
		t.Context(), pool, uuid.Must(uuid.NewV7()))
	switch {
	case err != nil:
		t.Fatalf("a job that does not exist returned an error: %v", err)
	case found:
		t.Error("a job that does not exist was found")
	case customer != uuid.Nil || provider != uuid.Nil:
		t.Errorf("it named %s and %s", customer, provider)
	}
}
