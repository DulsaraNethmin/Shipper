package migrations_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/bidding"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// 000502's supersede chain, checked where its guarantees live.
//
// **This file is the half of SHIP-88 that application logic cannot have.** Its *Done when* is "only
// the latest valid offer is acceptable; full chain remains readable", and the first clause is
// deliberately a pair of column constraints rather than a rule the award transaction has to remember.
// Docs/11 §8 makes SHIP-92 a single-owner branch precisely because a lock ordering nobody wrote down
// becomes a deadlock or a lost update; what is written down here is stronger than an ordering,
// because PostgreSQL refuses the write whatever order the award takes its locks in.
//
// Everything below runs against a real database for the reason bids_test.go gives: "a mock happily
// accepts a write that the actual constraint would reject", and these constraints are the whole
// guarantee.

// newCounter inserts one row into an existing negotiation, at the party and status named.
//
// It writes `bids` directly rather than going through `internal/bidding`, which is the point: these
// tests are about what the *schema* refuses, so they have to be able to attempt writes the domain
// would never make.
func newCounter(
	t *testing.T,
	pool *pgxpool.Pool,
	job, provider uuid.UUID,
	by bidding.Party,
	status bidding.Status,
	amount string,
) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	// The timing is SHIP-87a's `ck_bids_offer_has_timing`, which refuses a row past 'Draft' naming
	// neither instant. It is stated unconditionally because every caller here writes a status past
	// 'Draft' — the chain constraints this file is about have nothing to say about a draft.
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO bids (id, job_id, provider_id, offered_by, status, amount, pickup_at, deliver_by)
		VALUES ($1, $2, $3, $4, $5, $6, now() + interval '2 days', now() + interval '3 days')`,
		id, job, provider, string(by), string(status), amount); err != nil {
		t.Fatalf("inserting a %s offer by the %s: %v", status, by, err)
	}
	return id
}

// TestEveryOfferingPartyMatchesTheConstraint is Docs/10 §3.4's pairing, applied to 000502's
// enumeration.
//
// The same discipline TestEveryBidStatusConstraintMatchesTheGoConstants applies to the eight bid
// statuses, and it is required for the identical reason: the list exists in Go and in
// `ck_bids_offered_by`, and nothing but a test holds the two together. A party the database refuses
// and Go has is an offer nobody can write; a party the database accepts and Go does not know is a row
// no code can render.
//
// Two values rather than twelve makes it look unnecessary, which is exactly when a pairing test gets
// skipped and the third value somebody adds later goes in one place only.
func TestEveryOfferingPartyMatchesTheConstraint(t *testing.T) {
	pool := pgtest.DB(t)

	inGo := map[string]bool{}
	for _, party := range bidding.Parties {
		inGo[string(party)] = true
	}

	if len(bidding.Parties) != 2 {
		t.Errorf("bidding.Parties holds %d parties; a bid is offered by a provider or a customer",
			len(bidding.Parties))
	}
	if len(inGo) != len(bidding.Parties) {
		t.Errorf("bidding.Parties contains a duplicate: %d constants, %d distinct values",
			len(bidding.Parties), len(inGo))
	}

	var definition string
	if err := pool.QueryRow(t.Context(), `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conname = $1`, "ck_bids_offered_by",
	).Scan(&definition); err != nil {
		t.Fatalf("reading ck_bids_offered_by out of pg_constraint: %v", err)
	}

	inDatabase := map[string]bool{}
	for _, match := range quotedLiteral.FindAllStringSubmatch(definition, -1) {
		inDatabase[match[1]] = true
	}

	for party := range inGo {
		if !inDatabase[party] {
			t.Errorf("Go has the offering party %q and ck_bids_offered_by does not permit it; "+
				"the database would refuse an offer made by one", party)
		}
	}
	for party := range inDatabase {
		if !inGo[party] {
			t.Errorf("ck_bids_offered_by permits %q and Go has no constant for it; the database "+
				"would accept a row no code handles", party)
		}
	}

	// Named explicitly as well as compared, because both lists could drift together if somebody
	// capitalised one and then made the other match. These two strings are `users.role`'s and
	// `job_status_history.actor_type`'s, which is what makes them lower case.
	for _, want := range []string{"provider", "customer"} {
		if !inDatabase[want] {
			t.Errorf("ck_bids_offered_by does not permit %q", want)
		}
	}
}

// TestASupersededOfferCanBeNeitherLiveNorAccepted is SHIP-88's first *Done when* as a database rule.
//
// "Only the latest valid offer can be accepted" (Docs/02 §4). The link lives on the *displaced* row
// precisely so that this is expressible as an ordinary column constraint — with the link on the new
// row instead, "has this offer been displaced" would be a question about some other row and no CHECK
// could ask it.
//
// **This is what SHIP-92 may rely on.** An award that writes `status = 'Accepted'` over a countered
// offer — because it read the status before taking its lock, or did not re-read it at all — is
// refused by PostgreSQL rather than by a rule the award transaction had to remember.
func TestASupersededOfferCanBeNeitherLiveNorAccepted(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newUser(t, pool, "supersede@example.com", "+61400000520", "customer")
	provider := newUser(t, pool, "supersede-p@example.com", "+61400000521", "provider")
	job := newJob(t, pool, customer)

	first := newBid(t, pool, job, provider, "450.00")
	second := newCounter(t, pool, job, provider, bidding.PartyCustomer, bidding.StatusDraft, "400.00")

	// The chain, written the way internal/bidding writes it: the head leaves 'Submitted' first, and
	// the link lands afterwards.
	if _, err := pool.Exec(t.Context(),
		`UPDATE bids SET status = 'Superseded' WHERE id = $1`, first); err != nil {
		t.Fatalf("superseding the first offer: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`UPDATE bids SET superseded_by = $2 WHERE id = $1`, first, second); err != nil {
		t.Fatalf("linking the chain: %v", err)
	}

	for _, status := range []bidding.Status{bidding.StatusSubmitted, bidding.StatusAccepted} {
		t.Run("a displaced offer cannot become "+string(status), func(t *testing.T) {
			_, err := pool.Exec(t.Context(),
				`UPDATE bids SET status = $2 WHERE id = $1`, first, string(status))
			if err == nil {
				t.Fatalf("a superseded offer became %s; ck_bids_superseded_is_not_live is what makes "+
					"\"only the latest valid offer is acceptable\" true for SHIP-92 rather than "+
					"remembered", status)
			}
			if !strings.Contains(err.Error(), "ck_bids_superseded_is_not_live") {
				t.Errorf("expected ck_bids_superseded_is_not_live to refuse it, got: %v", err)
			}
		})
	}

	// The other five statuses are deliberately permitted on a displaced row. SHIP-93 closes *all*
	// other bids when a job is awarded (Docs/02 §3), and forbidding that here would be this migration
	// binding that ticket's design in the way 000501 refused to bind this one.
	if _, err := pool.Exec(t.Context(),
		`UPDATE bids SET status = 'Rejected' WHERE id = $1`, first); err != nil {
		t.Errorf("a superseded offer could not be closed by an award: %v — SHIP-93 needs this", err)
	}
}

// TestOnlyAProvidersOfferCanBeAccepted is the second constraint SHIP-92 inherits, and it is the one
// that only became necessary when counters arrived.
//
// With a chain, the head of a negotiation is sometimes the customer's own offer. Awarding it would
// commit a provider to a price and a date they never agreed to, `uq_bids_one_accepted_per_job` would
// allow it, and nothing else would notice. Docs/02 §1 defines Awarded as "Customer has accepted one
// **provider** bid; provider commitment exists", and this constraint is that sentence.
func TestOnlyAProvidersOfferCanBeAccepted(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newUser(t, pool, "award-party@example.com", "+61400000522", "customer")
	provider := newUser(t, pool, "award-party-p@example.com", "+61400000523", "provider")
	job := newJob(t, pool, customer)

	theirs := newCounter(t, pool, job, provider, bidding.PartyCustomer, bidding.StatusSubmitted, "400.00")

	_, err := pool.Exec(t.Context(), `UPDATE bids SET status = 'Accepted' WHERE id = $1`, theirs)
	if err == nil {
		t.Fatal("the customer's own counter was awarded; a provider would be bound to terms they " +
			"never offered")
	}
	if !strings.Contains(err.Error(), "ck_bids_only_a_providers_offer_is_accepted") {
		t.Errorf("expected ck_bids_only_a_providers_offer_is_accepted to refuse it, got: %v", err)
	}

	// And the provider's own offer at the same number is awardable, which is what makes the constraint
	// a shape rather than a wall: a customer who wants their number accepted waits for the provider to
	// counter at it, and that row is the provider's commitment.
	//
	// The customer's counter leaves 'Submitted' first, because that is what a counter does to the
	// offer it answers — and `uq_bids_one_submitted_per_provider_per_job` refuses the second live offer
	// otherwise, which is the third of the three guards 000502's header lists and the one that holds
	// with no application code involved at all.
	if _, err := pool.Exec(t.Context(),
		`UPDATE bids SET status = 'Superseded' WHERE id = $1`, theirs); err != nil {
		t.Fatalf("superseding the customer's counter: %v", err)
	}
	mine := newCounter(t, pool, job, provider, bidding.PartyProvider, bidding.StatusSubmitted, "400.00")
	if _, err := pool.Exec(t.Context(), `UPDATE bids SET status = 'Accepted' WHERE id = $1`, mine); err != nil {
		t.Fatalf("a provider's own offer could not be awarded: %v", err)
	}
}

// TestAChainCannotMergeOrPointAtItself is the pair of integrity rules that make a chain a list.
//
// "At most one successor per offer" is true by construction — `superseded_by` is a single column.
// This is the other direction and the reflexive case, neither of which the column shape gives.
func TestAChainCannotMergeOrPointAtItself(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newUser(t, pool, "chain-shape@example.com", "+61400000524", "customer")
	provider := newUser(t, pool, "chain-shape-p@example.com", "+61400000525", "provider")
	job := newJob(t, pool, customer)

	head := newCounter(t, pool, job, provider, bidding.PartyProvider, bidding.StatusSubmitted, "450.00")
	one := newCounter(t, pool, job, provider, bidding.PartyProvider, bidding.StatusSuperseded, "440.00")
	two := newCounter(t, pool, job, provider, bidding.PartyProvider, bidding.StatusSuperseded, "430.00")

	if _, err := pool.Exec(t.Context(),
		`UPDATE bids SET superseded_by = $2 WHERE id = $1`, one, head); err != nil {
		t.Fatalf("linking the first predecessor: %v", err)
	}

	t.Run("two offers cannot name one counter as what displaced them", func(t *testing.T) {
		_, err := pool.Exec(t.Context(),
			`UPDATE bids SET superseded_by = $2 WHERE id = $1`, two, head)
		if err == nil {
			t.Fatal("a negotiation merged: two offers were displaced by the same counter, so a " +
				"reader walking the links backwards finds a tree rather than a chain")
		}
		if !strings.Contains(err.Error(), "uq_bids_one_successor") {
			t.Errorf("expected uq_bids_one_successor to refuse it, got: %v", err)
		}
	})

	t.Run("an offer cannot displace itself", func(t *testing.T) {
		_, err := pool.Exec(t.Context(),
			`UPDATE bids SET superseded_by = id WHERE id = $1`, two)
		if err == nil {
			t.Fatal("an offer superseded itself")
		}
		if !strings.Contains(err.Error(), "ck_bids_supersession_is_not_reflexive") {
			t.Errorf("expected ck_bids_supersession_is_not_reflexive to refuse it, got: %v", err)
		}
	})
}

// TestOneKeyPerPartyPerNegotiation is 000501's privacy argument, extended by 000502 to the
// negotiation's second writer and checked at the index rather than at the endpoint.
//
// 000501 put `provider_id` in `uq_bids_idempotency` because "two providers whose clients happened to
// generate the same key would collide, and the second would be handed the first's bid". A negotiation
// now has two writers inside one `(job_id, provider_id)` pair, so the identical sentence applies to
// them.
func TestOneKeyPerPartyPerNegotiation(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newUser(t, pool, "key-party@example.com", "+61400000526", "customer")
	provider := newUser(t, pool, "key-party-p@example.com", "+61400000527", "provider")
	job := newJob(t, pool, customer)

	const shared = "a-key-both-clients-generated"

	insert := func(by bidding.Party) error {
		id, err := uuid.NewV7()
		if err != nil {
			t.Fatalf("generating an id: %v", err)
		}
		_, err = pool.Exec(t.Context(), `
			INSERT INTO bids (id, job_id, provider_id, offered_by, status, amount, idempotency_key,
			                  pickup_at, deliver_by)
			VALUES ($1, $2, $3, $4, 'Superseded', '400.00', $5,
			        now() + interval '2 days', now() + interval '3 days')`,
			id, job, provider, string(by), shared)
		return err
	}

	if err := insert(bidding.PartyProvider); err != nil {
		t.Fatalf("the provider's first write under the key: %v", err)
	}
	if err := insert(bidding.PartyCustomer); err != nil {
		t.Fatalf("the customer's write under the same key was refused: %v — a key is scoped to the "+
			"party that sent it, so the two cannot collide", err)
	}

	err := insert(bidding.PartyProvider)
	if err == nil {
		t.Fatal("the provider wrote twice under one key; the index no longer makes a retry a retry")
	}
	if !strings.Contains(err.Error(), "uq_bids_idempotency") {
		t.Errorf("expected uq_bids_idempotency to refuse the repeat, got: %v", err)
	}
}
