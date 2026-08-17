package migrations_test

import (
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/admin"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-164's columns, checked where their guarantees live.
//
// `000804` adds three things to `disputes` and every one of them is a constraint rather than
// application logic: the outcome vocabulary, the rule that a dispute is open or resolved and never
// half of each, and the foreign key to the administrator who settled it. Docs/06 §4.1 puts the
// demonstration here — "a mock happily accepts a write that the actual constraint would reject" —
// and `internal/admin`'s own tests drive the endpoints that produce these rows rather than the
// writes that the constraints refuse.
//
// `disputeFixture` and `raise` come from disputes_test.go, `constraintLiterals` from
// milestones_test.go, and `aModerator` from suspension_reviews_test.go — all in this external test
// package.

// TestDisputeOutcomeConstraintMatchesTheGoConstants is Docs/10 §3.4's pairing, in both directions.
//
// An outcome missing from the database is one an administrator can never record; an outcome missing
// from Go is a value the database will accept and no code knows how to display.
//
// **The list is a reading rather than a quotation, and that is why the strings are named below as
// well as compared.** Docs/04 §7 lists five outcomes in prose; the stored forms shorten them,
// because the wire mapping is derived by lower-snake-casing and a semicolon, a comma or a slash has
// no legal form under it — the same call `ck_disputes_category` made for the intake list. Naming
// them here makes a change to that reading visible as a change to this test rather than as two lists
// quietly drifting together.
func TestDisputeOutcomeConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	if len(admin.Outcomes) != 5 {
		t.Errorf("admin.Outcomes holds %d values; Docs/04 §7 lists five", len(admin.Outcomes))
	}

	inGo := map[string]bool{}
	for _, o := range admin.Outcomes {
		inGo[string(o)] = true
	}
	if len(inGo) != len(admin.Outcomes) {
		t.Errorf("admin.Outcomes contains a duplicate: %d constants, %d distinct values",
			len(admin.Outcomes), len(inGo))
	}

	inDatabase := constraintLiterals(t, pool, "ck_disputes_outcome")

	for o := range inGo {
		if !inDatabase[o] {
			t.Errorf("Go has the outcome %q and ck_disputes_outcome does not permit it; "+
				"an administrator could never record it", o)
		}
	}
	for o := range inDatabase {
		if !inGo[o] {
			t.Errorf("ck_disputes_outcome permits %q and Go has no constant for it", o)
		}
	}

	for _, want := range []string{
		"Delivery completed as agreed",
		"Delivery issue acknowledged",
		"Failed delivery recorded",
		"Warning restriction or suspension",
		"Referred to legal insurer or authorities",
	} {
		if !inDatabase[want] {
			t.Errorf("ck_disputes_outcome does not permit %q", want)
		}
	}

	// The two vocabularies are separate and this is where that stays true. A job status in the
	// outcome list would be somebody collapsing Docs/04 §7 into Docs/02 §2 — the reduction
	// `000804`s header exists to refuse — and it would compile, pass every Go test that reads
	// admin.Outcomes, and only be wrong about what the column means.
	for _, notAnOutcome := range []string{"Completed", "Cancelled", "Disputed", "completed", "cancelled"} {
		if inDatabase[notAnOutcome] {
			t.Errorf("ck_disputes_outcome permits %q, which is a job status rather than a "+
				"Docs/04 §7 outcome. Where the job goes is Docs/02 §2's vocabulary and lives "+
				"on jobs.status; this column records what was *found*.", notAnOutcome)
		}
	}
}

// TestADisputeIsOpenOrResolvedAndNeverHalfOfEach is `ck_disputes_resolution`.
//
// # The failure it prevents is specific and unrecoverable
//
// `resolved_at` alone takes the row off `idx_disputes_open` — the partial index Docs/04 §5's sixth
// queue reads — so a dispute with `resolved_at` and no outcome is one **nobody will ever see again**
// and which records nothing about why it stopped being open. An `outcome` alone is the mirror: a
// finding recorded against a dispute the queue still shows as waiting.
//
// It is a constraint rather than a rule in Go because a convention does not apply to a repair
// script, a support query typed at a psql prompt, or the next endpoint somebody adds without reading
// `internal/admin/resolution.go`. `000005_users_role_is_immutable` argues the same position.
//
// Every combination is exercised, including both valid ones, because a constraint written the wrong
// way round would refuse the states the platform actually writes.
func TestADisputeIsOpenOrResolvedAndNeverHalfOfEach(t *testing.T) {
	pool, job, customer, at := disputeFixture(t, "804a")

	settler := aModerator(t, pool, "resolution-constraint@example.com")

	for _, tc := range []struct {
		name   string
		set    string
		args   []any
		accept bool
	}{
		{
			name:   "open — all three NULL",
			set:    `resolved_at = NULL, outcome = NULL, resolved_by = NULL`,
			accept: true,
		},
		{
			name:   "resolved — all three set",
			set:    `resolved_at = now(), outcome = $2, resolved_by = $3`,
			args:   []any{"Failed delivery recorded", settler},
			accept: true,
		},
		{
			name:   "closed with nothing documented",
			set:    `resolved_at = now()`,
			accept: false,
		},
		{
			name:   "an outcome on a dispute the queue still shows as open",
			set:    `outcome = $2`,
			args:   []any{"Failed delivery recorded"},
			accept: false,
		},
		{
			name:   "an administrator named against no outcome",
			set:    `resolved_at = now(), resolved_by = $2`,
			args:   []any{settler},
			accept: false,
		},
		{
			name:   "an outcome nobody is accountable for",
			set:    `resolved_at = now(), outcome = $2`,
			args:   []any{"Failed delivery recorded"},
			accept: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, err := raise(t, pool, job, customer, "customer", "Goods damaged or missing",
				at, nil, nil)
			if err != nil {
				t.Fatalf("raising a dispute: %v", err)
			}
			// Off the open queue immediately, so the next subtest's raise is not refused by
			// uq_disputes_open_per_job. Done as one legal resolution rather than a delete,
			// because nothing in this platform deletes a dispute.
			defer func() {
				if _, err := pool.Exec(t.Context(), `
					UPDATE disputes
					SET resolved_at = now(), outcome = 'Failed delivery recorded', resolved_by = $2
					WHERE id = $1 AND resolved_at IS NULL`, id, settler); err != nil {
					t.Fatalf("clearing the fixture dispute: %v", err)
				}
			}()

			args := append([]any{id}, tc.args...)
			_, err = pool.Exec(t.Context(),
				`UPDATE disputes SET `+tc.set+` WHERE id = $1`, args...)

			switch {
			case tc.accept && err != nil:
				t.Errorf("ck_disputes_resolution refused a state the platform writes: %v", err)
			case !tc.accept && err == nil:
				t.Error("a half-resolved dispute was accepted.\n" +
					"resolved_at alone takes the row off idx_disputes_open, so it is a " +
					"dispute nobody sees again with nothing recorded about why it closed.")
			}
		})
	}
}

// TestTheSettlingAdministratorCannotBeDeletedAwayFromTheirDecision is `fk_disputes_resolved_by`.
//
// ON DELETE RESTRICT, per Docs/10 §3.3 and for `fk_disputes_complainant`'s reason: a resolution that
// could lose the name of who took it is a resolution nobody is accountable for, and Docs/04 §9's
// controls are about being able to say who did what.
//
// **It points at `admin_users` and not at `users`.** The two are separate credential systems that
// cannot be exchanged for each other (SHIP-147), and resolving a dispute is an act on the
// administrator credential — so a `users` reference would have been a column no administrator's
// identifier could satisfy.
func TestTheSettlingAdministratorCannotBeDeletedAwayFromTheirDecision(t *testing.T) {
	pool, job, customer, at := disputeFixture(t, "804c")
	settler := aModerator(t, pool, "resolution-fk@example.com")

	id, err := raise(t, pool, job, customer, "customer", "Delivery is late", at, nil, nil)
	if err != nil {
		t.Fatalf("raising a dispute: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		UPDATE disputes
		SET resolved_at = now(), outcome = 'Delivery completed as agreed', resolved_by = $2
		WHERE id = $1`, id, settler); err != nil {
		t.Fatalf("resolving the dispute: %v", err)
	}

	if _, err := pool.Exec(t.Context(),
		`DELETE FROM admin_users WHERE id = $1`, settler); err == nil {
		t.Error("the administrator who resolved a dispute was deleted, leaving the decision " +
			"with nobody's name on it")
	}

	// A `users` row is not what the column holds, and this is what says so: a customer's
	// identifier must be refused outright rather than accepted and left dangling.
	if _, err := pool.Exec(t.Context(),
		`UPDATE disputes SET resolved_by = $2 WHERE id = $1`, id, customer); err == nil {
		t.Error("a `users` identifier was accepted in resolved_by; the administrator and " +
			"mobile credential systems are separate and neither can stand for the other")
	}
}

// TestTheResolvedQueueHasItsOwnIndex is `idx_disputes_resolved`.
//
// Asserted by name and by predicate rather than by an EXPLAIN, which is what every other index test
// in this package does and for the same reason: a plan is the planner's decision at one row count,
// and what a migration owes the queue is that the index exists, is partial on the right predicate,
// and carries the tie-break column the cursor needs.
//
// The tie-break is the half worth checking. `resolved_at` is not unique — an administrator working
// through a morning's queue settles several disputes in one sitting — so a single-column index would
// leave the cursor repeating a row or skipping one at exactly the page boundary.
func TestTheResolvedQueueHasItsOwnIndex(t *testing.T) {
	pool := pgtest.DB(t)

	var definition string
	if err := pool.QueryRow(t.Context(),
		`SELECT indexdef FROM pg_indexes WHERE indexname = 'idx_disputes_resolved'`,
	).Scan(&definition); err != nil {
		t.Fatalf("reading idx_disputes_resolved: %v", err)
	}

	for _, want := range []string{"resolved_at DESC", "id DESC", "resolved_at IS NOT NULL"} {
		if !strings.Contains(definition, want) {
			t.Errorf("idx_disputes_resolved does not carry %q: %s", want, definition)
		}
	}
}
