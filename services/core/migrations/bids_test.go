package migrations_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/bidding"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
	"github.com/DulsaraNethmin/Shipper/services/core/migrations"
)

// SHIP-80's table, checked where its guarantees live.
//
// SHIP-80 adds no HTTP endpoint, so this file is the whole of its demonstration. Every assertion
// is about a constraint, an index or a trigger, so all of them run against a real PostgreSQL —
// Docs/06 §4.1: "a mock happily accepts a write that the actual constraint would reject."
//
// uq_bids_one_accepted_per_job is the sharpest case of that in the repository. A mocked
// repository accepts a second acceptance, an application-level "check then write" accepts it
// under a race, and both look correct in every test that does not run two transactions at once.
// TestOneAcceptedBidPerJobHoldsUnderARace does.

// offerTiming is the two instants every offer past Draft has to state (SHIP-87a).
//
// **Written as an interval against the database's own clock rather than as a parameter**, because
// nothing in this file is about *when* an offer collects — the tests here are about a CHECK, an
// index and two foreign keys. A fixture that took the timing as an argument would invite a caller to
// think it mattered, and every call site would then carry two values nobody reads.
//
// The order is the one ck_bids_timing_is_ordered (000501) requires: delivery after collection.
const offerTiming = `now() + interval '2 days', now() + interval '3 days'`

// newBid inserts one live offer and returns its id.
//
// 'Submitted' rather than the column default, because a Draft is the one status that may have no
// amount and every test below is about an offer somebody can act on.
//
// **It states its timing, and that is SHIP-87a rather than tidiness.** ck_bids_offer_has_timing
// refuses a row past 'Draft' that names neither instant, so a fixture written before that constraint
// existed now fails at the INSERT rather than at the assertion it was written for. 000501 predicted
// exactly this — "it failed six of SHIP-80's own migration tests" — and the fix is the one that
// makes the fixture write the row the platform would actually have written.
func newBid(t *testing.T, pool *pgxpool.Pool, job, provider uuid.UUID, amount string) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO bids (id, job_id, provider_id, status, amount, pickup_at, deliver_by)
		 VALUES ($1, $2, $3, 'Submitted', $4, `+offerTiming+`)`,
		id, job, provider, amount); err != nil {
		t.Fatalf("inserting a bid by %s on %s: %v", provider, job, err)
	}
	return id
}

// TestEveryBidStatusConstraintMatchesTheGoConstants is SHIP-80's acceptance criterion —
// "schema covers all eight bid statuses from Docs/02 §4" — checked in both directions.
//
// Docs/10 §3.4 requires the pairing for every enumeration, and names this list and the twelve
// job statuses as the pair it exists for: they are built on separate branches, so nothing but a
// test holds them to their document. One direction alone is not enough. A status the database
// refuses and Go has is a bid that cannot reach a state the product has; a status the database
// accepts and Go does not know is a row no code can render.
func TestEveryBidStatusConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	inGo := map[string]bool{}
	for _, status := range bidding.Statuses {
		inGo[string(status)] = true
	}

	if len(bidding.Statuses) != 8 {
		t.Errorf("bidding.Statuses holds %d statuses; Docs/02 §4 has eight", len(bidding.Statuses))
	}
	if len(inGo) != len(bidding.Statuses) {
		t.Errorf("bidding.Statuses contains a duplicate: %d constants, %d distinct values",
			len(bidding.Statuses), len(inGo))
	}

	var definition string
	if err := pool.QueryRow(t.Context(), `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conname = $1`, "ck_bids_status",
	).Scan(&definition); err != nil {
		t.Fatalf("reading ck_bids_status out of pg_constraint: %v", err)
	}

	inDatabase := map[string]bool{}
	for _, match := range quotedLiteral.FindAllStringSubmatch(definition, -1) {
		inDatabase[match[1]] = true
	}

	for status := range inGo {
		if !inDatabase[status] {
			t.Errorf("Go has the bid status %q and ck_bids_status does not permit it; "+
				"the database would refuse a bid that reached it", status)
		}
	}
	for status := range inDatabase {
		if !inGo[status] {
			t.Errorf("ck_bids_status permits %q and Go has no constant for it; "+
				"the database would accept a state no code handles", status)
		}
	}

	// Named explicitly as well as compared, because both lists could drift together if
	// somebody lower-cased one and then made the other match. Docs/02 §4 is the authority for
	// these exact strings and Docs/10 §3.4 for storing them unaltered.
	for _, want := range []string{
		"Draft", "Submitted", "Countered", "Accepted", "Rejected", "Withdrawn", "Expired", "Superseded",
	} {
		if !inDatabase[want] {
			t.Errorf("ck_bids_status does not permit %q; Docs/02 §4 writes the eight bid "+
				"statuses in sentence case and Docs/10 §3.4 stores them that way", want)
		}
	}
}

// TestAnUnknownBidStatusIsRefused proves the CHECK is real rather than decorative.
//
// Docs/10 §3.4 chooses text-plus-CHECK over a PostgreSQL enum type, and the trade is that the
// constraint has to be tested rather than assumed from the column type.
func TestAnUnknownBidStatusIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newUser(t, pool, "bid-status@example.com", "+61400000500", "customer")
	provider := newUser(t, pool, "bid-status-p@example.com", "+61400000501", "provider")
	job := newJob(t, pool, customer)

	// The timing is stated so that ck_bids_offer_has_timing (SHIP-87a) is not the constraint that
	// refuses this row. A write violating two CHECKs is reported by whichever PostgreSQL evaluates
	// first, and this test is about ck_bids_status alone.
	id, _ := uuid.NewV7()
	_, err := pool.Exec(t.Context(),
		`INSERT INTO bids (id, job_id, provider_id, status, amount, pickup_at, deliver_by)
		 VALUES ($1, $2, $3, $4, 500.00, `+offerTiming+`)`,
		id, job, provider, "Won")
	if err == nil {
		t.Fatal("'Won' was accepted as a bid status")
	}
	if !strings.Contains(err.Error(), "ck_bids_status") {
		t.Errorf("expected ck_bids_status to refuse it, got: %v", err)
	}
}

// TestOneAcceptedBidPerJob is the project invariant CLAUDE.md states, tested where it is
// actually enforced.
//
// "Exactly one accepted bid per job, enforced by a database constraint, not application logic
// alone." The three cases are the whole design of uq_bids_one_accepted_per_job:
//
//  1. a second acceptance on one job is refused, because that is two providers each believing
//     they have the work;
//  2. another job is unaffected, because the uniqueness is per job rather than global;
//  3. moving the accepted bid out of 'Accepted' frees the job to be awarded again, which is what
//     Docs/02 §6.2's provider cancellation needs — the index is partial precisely so that this
//     is possible.
func TestOneAcceptedBidPerJob(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newUser(t, pool, "award@example.com", "+61400000502", "customer")
	first := newUser(t, pool, "award-p1@example.com", "+61400000503", "provider")
	second := newUser(t, pool, "award-p2@example.com", "+61400000504", "provider")

	job := newJob(t, pool, customer)
	winner := newBid(t, pool, job, first, "450.00")
	loser := newBid(t, pool, job, second, "480.00")

	if _, err := pool.Exec(t.Context(),
		`UPDATE bids SET status = 'Accepted' WHERE id = $1`, winner); err != nil {
		t.Fatalf("accepting the first bid: %v", err)
	}

	t.Run("a second acceptance on the same job is refused", func(t *testing.T) {
		_, err := pool.Exec(t.Context(),
			`UPDATE bids SET status = 'Accepted' WHERE id = $1`, loser)
		if err == nil {
			t.Fatal("one job was awarded twice; two providers each believe they have it")
		}
		if !strings.Contains(err.Error(), "uq_bids_one_accepted_per_job") {
			t.Errorf("expected the partial unique index to refuse it, got: %v", err)
		}
	})

	t.Run("a bid inserted straight at Accepted is refused too", func(t *testing.T) {
		// The award moves an existing bid, but the constraint must not depend on that:
		// an INSERT is the same write as far as the index is concerned, and a migration
		// or a repair script would take this path.
		id, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(),
			`INSERT INTO bids (id, job_id, provider_id, status, amount, pickup_at, deliver_by)
			 VALUES ($1, $2, $3, 'Accepted', 400.00, `+offerTiming+`)`, id, job, second)
		if err == nil {
			t.Fatal("a second accepted bid was inserted alongside the first")
		}
		if !strings.Contains(err.Error(), "uq_bids_one_accepted_per_job") {
			t.Errorf("expected the partial unique index to refuse it, got: %v", err)
		}
	})

	t.Run("another job may have its own accepted bid", func(t *testing.T) {
		other := newJob(t, pool, customer)
		theirs := newBid(t, pool, other, second, "300.00")

		if _, err := pool.Exec(t.Context(),
			`UPDATE bids SET status = 'Accepted' WHERE id = $1`, theirs); err != nil {
			t.Errorf("a second job could not be awarded at all: %v", err)
		}
	})

	t.Run("releasing the award frees the job", func(t *testing.T) {
		// Docs/02 §6.2: a provider cancelling before pickup returns the job to Open with all
		// bids closed. The index is partial so that the accepted row leaving the predicate
		// makes room for the next award rather than blocking it forever.
		if _, err := pool.Exec(t.Context(),
			`UPDATE bids SET status = 'Rejected' WHERE id = $1`, winner); err != nil {
			t.Fatalf("releasing the award: %v", err)
		}
		if _, err := pool.Exec(t.Context(),
			`UPDATE bids SET status = 'Accepted' WHERE id = $1`, loser); err != nil {
			t.Errorf("the job could not be awarded again after the first award was released: %v", err)
		}
	})
}

// TestOneAcceptedBidPerJobHoldsUnderARace is the half of the invariant application logic cannot
// have.
//
// A SELECT that finds no accepted bid followed by an UPDATE that creates one passes every test
// in TestOneAcceptedBidPerJob and still loses this one: the window between the read and the
// write is small and it is not zero. Two transactions both writing 'Accepted' for one job is
// what arrives in it — two customers' requests, two retries of one request, or one request
// racing its own idempotency replay.
//
// The index serialises them without anybody arranging it. The second writer blocks on the
// first's uncommitted index entry and is then told the answer: unique_violation if the first
// committed, success if it rolled back. SHIP-92 inherits that and does not have to invent it,
// which is why Docs/09 puts the constraint before the award endpoint.
//
// This is not SHIP-95. That ticket owns withdraw-during-award and expiry-during-award against
// the real endpoint; this is the one race the schema alone can be held to.
func TestOneAcceptedBidPerJobHoldsUnderARace(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newUser(t, pool, "race@example.com", "+61400000505", "customer")
	first := newUser(t, pool, "race-p1@example.com", "+61400000506", "provider")
	second := newUser(t, pool, "race-p2@example.com", "+61400000507", "provider")

	job := newJob(t, pool, customer)
	mine := newBid(t, pool, job, first, "450.00")
	theirs := newBid(t, pool, job, second, "460.00")

	ahead, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("beginning the first transaction: %v", err)
	}
	defer func() { _ = ahead.Rollback(t.Context()) }()

	if _, err := ahead.Exec(t.Context(),
		`UPDATE bids SET status = 'Accepted' WHERE id = $1`, mine); err != nil {
		t.Fatalf("accepting in the first transaction: %v", err)
	}

	behind := make(chan error, 1)
	go func() {
		tx, err := pool.Begin(t.Context())
		if err != nil {
			behind <- err
			return
		}
		defer func() { _ = tx.Rollback(t.Context()) }()

		if _, err := tx.Exec(t.Context(),
			`UPDATE bids SET status = 'Accepted' WHERE id = $1`, theirs); err != nil {
			behind <- err
			return
		}
		behind <- tx.Commit(t.Context())
	}()

	// Only steers which path is exercised, and is not load-bearing. If the goroutine has not
	// reached the index yet it finds the first award already committed and is refused
	// immediately; if it has, it is blocked and refused on release. Both outcomes are the same
	// assertion below, so a slow machine changes nothing about what is proved.
	time.Sleep(250 * time.Millisecond)

	if err := ahead.Commit(t.Context()); err != nil {
		t.Fatalf("committing the first award: %v", err)
	}

	select {
	case err := <-behind:
		if err == nil {
			t.Fatal("both transactions awarded the same job; the second was never refused")
		}
		if !strings.Contains(err.Error(), "uq_bids_one_accepted_per_job") {
			t.Errorf("expected the partial unique index to refuse the second award, got: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("the second award never returned; it is still waiting on a lock nobody holds")
	}

	var accepted int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM bids WHERE job_id = $1 AND status = 'Accepted'`, job).Scan(&accepted); err != nil {
		t.Fatalf("counting the accepted bids: %v", err)
	}
	if accepted != 1 {
		t.Errorf("the job has %d accepted bids, want exactly one", accepted)
	}
}

// TestAnOfferPastDraftNamesAnAmount covers the two coherence constraints on the price together.
//
// A Draft may be incomplete, for the reason 000404 makes every job draft column nullable:
// refusing an incomplete row is refusing to save what somebody has typed so far. Everything past
// it is an offer the other party is expected to act on, and an offer with no price is not one —
// most sharply at 'Accepted', where the award would otherwise commit both parties to an unstated
// amount.
func TestAnOfferPastDraftNamesAnAmount(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newUser(t, pool, "amount@example.com", "+61400000508", "customer")
	provider := newUser(t, pool, "amount-p@example.com", "+61400000509", "provider")
	job := newJob(t, pool, customer)

	t.Run("a draft with no amount is allowed", func(t *testing.T) {
		// The draft states its timing and no amount, so ck_bids_offer_has_timing (SHIP-87a) is not
		// what refuses the submission below. The two constraints are twins with one predicate, and
		// a test that left both unstated could not say which of them held.
		id, _ := uuid.NewV7()
		if _, err := pool.Exec(t.Context(),
			`INSERT INTO bids (id, job_id, provider_id, pickup_at, deliver_by)
			 VALUES ($1, $2, $3, `+offerTiming+`)`,
			id, job, provider); err != nil {
			t.Fatalf("a provider could not start composing an offer: %v", err)
		}

		var status string
		if err := pool.QueryRow(t.Context(),
			`SELECT status FROM bids WHERE id = $1`, id).Scan(&status); err != nil {
			t.Fatalf("reading the bid back: %v", err)
		}
		if status != string(bidding.StatusDraft) {
			t.Errorf("a new bid is %q, want %q", status, bidding.StatusDraft)
		}

		// And it cannot leave Draft without one, which is the point of the pair.
		_, err := pool.Exec(t.Context(),
			`UPDATE bids SET status = 'Submitted' WHERE id = $1`, id)
		if err == nil {
			t.Fatal("an offer with no price was submitted to a customer")
		}
		if !strings.Contains(err.Error(), "ck_bids_offer_has_an_amount") {
			t.Errorf("expected ck_bids_offer_has_an_amount to refuse it, got: %v", err)
		}
	})

	t.Run("a submitted offer with no amount is refused", func(t *testing.T) {
		id, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(),
			`INSERT INTO bids (id, job_id, provider_id, status, pickup_at, deliver_by)
			 VALUES ($1, $2, $3, 'Submitted', `+offerTiming+`)`,
			id, job, provider)
		if err == nil {
			t.Fatal("an offer with no price reached the customer")
		}
		if !strings.Contains(err.Error(), "ck_bids_offer_has_an_amount") {
			t.Errorf("expected ck_bids_offer_has_an_amount to refuse it, got: %v", err)
		}
	})

	t.Run("a free or negative price is refused", func(t *testing.T) {
		for _, amount := range []string{"0.00", "-1.00"} {
			id, _ := uuid.NewV7()
			_, err := pool.Exec(t.Context(),
				`INSERT INTO bids (id, job_id, provider_id, status, amount, pickup_at, deliver_by)
				 VALUES ($1, $2, $3, 'Submitted', $4, `+offerTiming+`)`, id, job, provider, amount)
			if err == nil {
				t.Errorf("%s was accepted as a bid amount", amount)
				continue
			}
			if !strings.Contains(err.Error(), "ck_bids_amount") {
				t.Errorf("expected ck_bids_amount to refuse %s, got: %v", amount, err)
			}
		}
	})

	t.Run("the amount keeps its cents", func(t *testing.T) {
		// Docs/10 §3.3: numeric(12,2), never a float. Read back as text so the assertion is
		// about what PostgreSQL stored rather than about how Go parses it.
		id := newBid(t, pool, job, provider, "1234.56")

		var amount string
		if err := pool.QueryRow(t.Context(),
			`SELECT amount::text FROM bids WHERE id = $1`, id).Scan(&amount); err != nil {
			t.Fatalf("reading the amount back: %v", err)
		}
		if amount != "1234.56" {
			t.Errorf("amount came back as %q, want 1234.56", amount)
		}
	})
}

// TestAnOfferPastDraftNamesItsTiming is SHIP-87a, and it is the constraint 000501 deferred.
//
// **The demonstration is a direct INSERT, deliberately, and that is the whole argument for the
// ticket.** `Offer.validate` in internal/bidding already refuses an offer naming neither instant, so
// nothing reaching this table through `POST /v1/jobs/{id}/bids` could ever have produced one. What a
// validator cannot cover is every *other* writer — a worker, a repair script, an admin path, a
// migration, a future endpoint — and each of those writes SQL rather than calling the validator.
// This test writes SQL.
//
// # Why both instants, and why the pair is checked in three directions rather than one
//
// ck_bids_timing_is_ordered (000501) compares `deliver_by > pickup_at` and is written to pass when
// either is NULL, because it can only compare two values it has. So a row naming a collection time
// and no delivery satisfies every other constraint on this table and is still an offer nobody can be
// held to — which is why "neither", "pickup only" and "delivery only" are three cases here and not
// one.
//
// # The Draft exemption is asserted rather than assumed
//
// It is the same exemption ck_bids_offer_has_an_amount carries and it exists for the same reason
// 000404 gives about job drafts: refusing an incomplete row is refusing to save what somebody has
// typed so far. A constraint that had quietly lost the `status = 'Draft' OR` would pass every case
// above and break the one screen a provider spends the longest on.
func TestAnOfferPastDraftNamesItsTiming(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newUser(t, pool, "timing@example.com", "+61400000515", "customer")
	provider := newUser(t, pool, "timing-p@example.com", "+61400000516", "provider")
	job := newJob(t, pool, customer)

	// Every status past Draft, rather than 'Submitted' alone. The constraint names no status but
	// one, so a status it happened not to cover would be a status nobody checked — and 'Accepted'
	// is the sharpest of them, where the award would otherwise commit both parties to an unstated
	// date.
	for _, status := range []string{
		"Submitted", "Countered", "Accepted", "Rejected", "Withdrawn", "Expired", "Superseded",
	} {
		t.Run(strings.ToLower(status)+" with no timing is refused", func(t *testing.T) {
			id, _ := uuid.NewV7()
			_, err := pool.Exec(t.Context(),
				`INSERT INTO bids (id, job_id, provider_id, status, amount)
				 VALUES ($1, $2, $3, $4, 450.00)`, id, job, provider, status)
			if err == nil {
				t.Fatalf("a %s offer stating neither of its instants reached the table", status)
			}
			if !strings.Contains(err.Error(), "ck_bids_offer_has_timing") {
				t.Errorf("expected ck_bids_offer_has_timing to refuse it, got: %v", err)
			}
		})
	}

	t.Run("half the timing is refused as well", func(t *testing.T) {
		for _, half := range []struct {
			name    string
			columns string
			values  string
		}{
			{"pickup only", "pickup_at", "now() + interval '2 days'"},
			{"delivery only", "deliver_by", "now() + interval '3 days'"},
		} {
			t.Run(half.name, func(t *testing.T) {
				id, _ := uuid.NewV7()
				_, err := pool.Exec(t.Context(),
					`INSERT INTO bids (id, job_id, provider_id, status, amount, `+half.columns+`)
					 VALUES ($1, $2, $3, 'Submitted', 450.00, `+half.values+`)`, id, job, provider)
				if err == nil {
					t.Fatalf("an offer stating only its %s reached the table; "+
						"ck_bids_timing_is_ordered passes when either instant is NULL, so nothing "+
						"else here refuses it", half.columns)
				}
				if !strings.Contains(err.Error(), "ck_bids_offer_has_timing") {
					t.Errorf("expected ck_bids_offer_has_timing to refuse it, got: %v", err)
				}
			})
		}
	})

	t.Run("a draft may state neither", func(t *testing.T) {
		id, _ := uuid.NewV7()
		if _, err := pool.Exec(t.Context(),
			`INSERT INTO bids (id, job_id, provider_id) VALUES ($1, $2, $3)`,
			id, job, provider); err != nil {
			t.Fatalf("a provider could not start composing an offer: %v", err)
		}

		// And it cannot leave Draft on the strength of its price alone, which is the pair working:
		// the amount is stated here so that ck_bids_offer_has_an_amount is not what refuses it.
		if _, err := pool.Exec(t.Context(),
			`UPDATE bids SET amount = 450.00 WHERE id = $1`, id); err != nil {
			t.Fatalf("pricing the draft: %v", err)
		}
		_, err := pool.Exec(t.Context(),
			`UPDATE bids SET status = 'Submitted' WHERE id = $1`, id)
		if err == nil {
			t.Fatal("an offer with a price and no dates was submitted to a customer")
		}
		if !strings.Contains(err.Error(), "ck_bids_offer_has_timing") {
			t.Errorf("expected ck_bids_offer_has_timing to refuse it, got: %v", err)
		}
	})
}

// TestABidBelongsToARealJobAndProvider is Docs/10 §3.3's foreign key rule, both directions.
//
// ON DELETE RESTRICT rather than a cascade: SHIP-171 pseudonymises an account rather than
// deleting it, and the losing bids on an awarded job are the evidence that the award was a
// choice — Docs/05 §3.1 requires that record retained.
func TestABidBelongsToARealJobAndProvider(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newUser(t, pool, "bid-fk@example.com", "+61400000510", "customer")
	provider := newUser(t, pool, "bid-fk-p@example.com", "+61400000511", "provider")
	job := newJob(t, pool, customer)

	t.Run("an unknown job is refused", func(t *testing.T) {
		id, _ := uuid.NewV7()
		stranger, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(),
			`INSERT INTO bids (id, job_id, provider_id, status, amount, pickup_at, deliver_by)
			 VALUES ($1, $2, $3, 'Submitted', 100.00, `+offerTiming+`)`, id, stranger, provider)
		if err == nil {
			t.Fatal("a bid was placed on a job that does not exist")
		}
		if !strings.Contains(err.Error(), "fk_bids_job") {
			t.Errorf("expected fk_bids_job to refuse it, got: %v", err)
		}
	})

	t.Run("an unknown provider is refused", func(t *testing.T) {
		id, _ := uuid.NewV7()
		stranger, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(),
			`INSERT INTO bids (id, job_id, provider_id, status, amount, pickup_at, deliver_by)
			 VALUES ($1, $2, $3, 'Submitted', 100.00, `+offerTiming+`)`, id, job, stranger)
		if err == nil {
			t.Fatal("a bid was placed by an account that does not exist")
		}
		if !strings.Contains(err.Error(), "fk_bids_provider") {
			t.Errorf("expected fk_bids_provider to refuse it, got: %v", err)
		}
	})

	t.Run("deleting the job is refused while a bid exists", func(t *testing.T) {
		newBid(t, pool, job, provider, "100.00")

		if _, err := pool.Exec(t.Context(), `DELETE FROM jobs WHERE id = $1`, job); err == nil {
			t.Fatal("a job was deleted, taking every bid on it with it")
		}
	})

	t.Run("deleting the provider is refused while a bid exists", func(t *testing.T) {
		if _, err := pool.Exec(t.Context(), `DELETE FROM users WHERE id = $1`, provider); err == nil {
			t.Fatal("a provider with bids was deleted, taking the commercial record with them")
		}
	})
}

// TestBidForeignKeysAreIndexed is the other half of Docs/10 §3.3's foreign key rule.
//
// It asks for an index with no predicate on purpose. uq_bids_one_accepted_per_job also leads
// with job_id, so a naive count would be satisfied by it — and it would be the wrong answer:
// covering one status, it cannot serve the constraint check that runs when a job is deleted, and
// it cannot serve the customer's list of bids on a job either.
func TestBidForeignKeysAreIndexed(t *testing.T) {
	pool := pgtest.DB(t)

	for _, column := range []string{"job_id", "provider_id"} {
		t.Run(column, func(t *testing.T) {
			var indexes int
			if err := pool.QueryRow(t.Context(), `
				SELECT count(*)
				FROM pg_index i
				JOIN pg_class t     ON t.oid = i.indrelid
				JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = i.indkey[0]
				WHERE t.relname = 'bids' AND a.attname = $1 AND i.indpred IS NULL`,
				column,
			).Scan(&indexes); err != nil {
				t.Fatalf("looking for the index: %v", err)
			}
			if indexes == 0 {
				t.Errorf("bids.%s carries a foreign key and no unfiltered index leading with it", column)
			}
		})
	}
}

// TestTheAcceptedBidIndexIsPartial pins the predicate itself, not only its effect.
//
// The behavioural tests above would all still pass if somebody replaced the index with a plain
// unique one on job_id — right up to the moment a second provider tried to bid at all, which is
// every job in the marketplace. This reads the definition, so the mistake is caught here rather
// than in SHIP-84.
func TestTheAcceptedBidIndexIsPartial(t *testing.T) {
	pool := pgtest.DB(t)

	var definition string
	if err := pool.QueryRow(t.Context(),
		`SELECT indexdef FROM pg_indexes WHERE indexname = $1`,
		"uq_bids_one_accepted_per_job").Scan(&definition); err != nil {
		t.Fatalf("reading uq_bids_one_accepted_per_job: %v", err)
	}

	if !strings.Contains(definition, "UNIQUE") {
		t.Errorf("uq_bids_one_accepted_per_job is not unique: %s", definition)
	}
	if !strings.Contains(definition, "WHERE") || !strings.Contains(definition, "'Accepted'") {
		t.Errorf("uq_bids_one_accepted_per_job is not restricted to accepted bids, so it "+
			"refuses a second *bid* rather than a second *acceptance*: %s", definition)
	}

	// Two live bids on one job, which is the ordinary case and the thing a non-partial index
	// would break.
	customer := newUser(t, pool, "partial@example.com", "+61400000512", "customer")
	first := newUser(t, pool, "partial-p1@example.com", "+61400000513", "provider")
	second := newUser(t, pool, "partial-p2@example.com", "+61400000514", "provider")
	job := newJob(t, pool, customer)

	newBid(t, pool, job, first, "500.00")
	newBid(t, pool, job, second, "520.00")
}

// TestBidTimestampsCarryTheirZone is Docs/10 §3.3's "timestamptz always, never timestamp".
//
// Named rather than counted, for the reason jobs' equivalent was rewritten: a count is a
// snapshot of how many columns existed on the day it was written, and SHIP-84 and SHIP-89 both
// add timestamps here. The rule is about every timestamp column, not about how many there are.
func TestBidTimestampsCarryTheirZone(t *testing.T) {
	pool := pgtest.DB(t)

	rows, err := pool.Query(t.Context(), `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'bids'
		  AND data_type LIKE 'timestamp%'
		ORDER BY column_name`)
	if err != nil {
		t.Fatalf("reading the column types: %v", err)
	}
	defer rows.Close()

	found := map[string]bool{}
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		found[name] = true
		if dataType != "timestamp with time zone" {
			t.Errorf("bids.%s is %s, want timestamp with time zone", name, dataType)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}

	for _, required := range []string{"created_at", "updated_at"} {
		if !found[required] {
			t.Errorf("bids has no %s column", required)
		}
	}
}

// TestBidMigrationIsInTheBiddingBlock names the number, because the block scheme is only worth
// having if something checks that a migration landed inside its range.
func TestBidMigrationIsInTheBiddingBlock(t *testing.T) {
	block, ok := migrations.BlockContaining(500)
	if !ok {
		t.Fatal("version 500 falls in no reserved block")
	}
	if block.Domain != "bidding" {
		t.Errorf("version 500 is in the %q block, want bidding", block.Domain)
	}
}
