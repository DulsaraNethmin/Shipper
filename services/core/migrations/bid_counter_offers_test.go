package migrations_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/bidding"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// 000502's two new columns, checked where their guarantees live.
//
// SHIP-87 adds `offered_by` and `superseded_by` to `bids`, and the assertions here are the ones about
// the *schema* rather than about the endpoint: the enumeration pairing Docs/10 §3.4 requires of every
// list held in two places, and the index that keeps one party's idempotency key out of the other's
// rows.
//
// They run against a real database for the reason bids_test.go gives — "a mock happily accepts a write
// that the actual constraint would reject". **SHIP-88 adds the rest of this file**, which is the pair
// of `CHECK` constraints that hold the award transaction to the latest valid offer.

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
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO bids (id, job_id, provider_id, offered_by, status, amount)
		VALUES ($1, $2, $3, $4, $5, $6)`,
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
			INSERT INTO bids (id, job_id, provider_id, offered_by, status, amount, idempotency_key)
			VALUES ($1, $2, $3, $4, 'Superseded', '400.00', $5)`,
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
