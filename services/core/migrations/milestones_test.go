package migrations_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/delivery"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-110's table, checked where its guarantees live.
//
// The ticket reaches no HTTP endpoint, so it has no `make verify` section and these tests are the
// demonstration — which is not a weaker one, because everything asserted below is a constraint,
// an index or a trigger. Docs/06 §4.1: "a mock happily accepts a write that the actual constraint
// would reject."
//
// assign and newDeliveryJob come from driver_assignments_test.go; newUser and newJob from
// schema_test.go and jobs_test.go; all in this same external test package. A milestone recorded
// by a driver names their assignment rather than an account, following the convention 000401
// declared, so the two tables are deliberately exercised together here.

// record writes one milestone and returns its id and the error, so a caller can assert either.
func record(t *testing.T, pool *pgxpool.Pool, job uuid.UUID, milestone, actorType string, actorID, reason any, actorRecordedAt time.Time) (uuid.UUID, error) {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	_, err = pool.Exec(t.Context(), `
		INSERT INTO milestones (id, job_id, milestone, actor_type, actor_id, reason, actor_recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		id, job, milestone, actorType, actorID, reason, actorRecordedAt)
	return id, err
}

// TestMilestoneConstraintMatchesTheGoConstants is Docs/10 §3.4's pairing, in both directions.
//
// A milestone missing from the database is one a driver can never record; a milestone missing
// from Go is a value the database will accept and no code knows how to display.
func TestMilestoneConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	if len(delivery.Milestones) != 5 {
		t.Errorf("delivery.Milestones holds %d values; Docs/01 §4.4 numbers five",
			len(delivery.Milestones))
	}

	inGo := map[string]bool{}
	for _, m := range delivery.Milestones {
		inGo[string(m)] = true
	}
	if len(inGo) != len(delivery.Milestones) {
		t.Errorf("delivery.Milestones contains a duplicate: %d constants, %d distinct values",
			len(delivery.Milestones), len(inGo))
	}

	inDatabase := constraintLiterals(t, pool, "ck_milestones_milestone")

	for m := range inGo {
		if !inDatabase[m] {
			t.Errorf("Go has the milestone %q and ck_milestones_milestone does not permit it; "+
				"a driver could never record it", m)
		}
	}
	for m := range inDatabase {
		if !inGo[m] {
			t.Errorf("ck_milestones_milestone permits %q and Go has no constant for it", m)
		}
	}

	// Named as well as compared, because both lists could drift together if somebody
	// "corrected" the spacing on one and made the other match. Docs/02 §1 is the authority for
	// these exact strings and Docs/10 §3.4 stores them unaltered.
	for _, want := range []string{"Driver assigned", "En route to pickup", "Picked up", "In transit", "Delivered"} {
		if !inDatabase[want] {
			t.Errorf("ck_milestones_milestone does not permit %q", want)
		}
	}

	// The four Docs/02 §1 statuses that are deliberately *not* milestones. 'Completed' expires
	// into existence after seventy-two hours (Docs/02 §6.1) and nobody records it; the others
	// are not things that happen on a delivery. This is the half of the pairing that catches a
	// constraint written by copying ck_jobs_status.
	for _, notAMilestone := range []string{"Draft", "Open", "Awarded", "Completed", "Cancelled", "Disputed"} {
		if inDatabase[notAMilestone] {
			t.Errorf("ck_milestones_milestone permits %q, which is a job status and not "+
				"something a driver records (Docs/01 §4.4)", notAMilestone)
		}
	}
}

// TestMilestoneActorConstraintMatchesTheGoConstants is the same pairing for the actor.
//
// The exclusion is the point: Docs/02 §3 permits the awarded provider, their driver and an
// administrator, and nobody else. A customer confirming a delivery is a status transition rather
// than a milestone, and a constraint that accepted 'customer' would let the confirmation be
// recorded as though the customer had driven the job.
func TestMilestoneActorConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	inGo := map[string]bool{}
	for _, a := range delivery.ActorTypes {
		inGo[string(a)] = true
	}

	inDatabase := constraintLiterals(t, pool, "ck_milestones_actor_type")

	for a := range inGo {
		if !inDatabase[a] {
			t.Errorf("Go has the actor %q and ck_milestones_actor_type does not permit it", a)
		}
	}
	for a := range inDatabase {
		if !inGo[a] {
			t.Errorf("ck_milestones_actor_type permits %q and Go has no constant for it", a)
		}
	}
	if inDatabase["customer"] {
		t.Error("ck_milestones_actor_type permits 'customer'; Docs/02 §3 restricts " +
			"delivery-status updates to the provider, their driver and an administrator")
	}
}

// TestMilestoneRecordsBothClocksIndependently is SHIP-110's acceptance criterion.
//
// Docs/02 §3.1 requires the distinction because a driver records milestones where there is no
// signal and the batch arrives much later. The second half is what a single timestamp would
// destroy: four milestones recorded across a morning, synced in one burst, all sharing one
// arrival time and each keeping the time it was actually recorded at.
func TestMilestoneRecordsBothClocksIndependently(t *testing.T) {
	pool := pgtest.DB(t)
	job := newDeliveryJob(t, pool, "two-clocks-delivery@example.com", "+61400000610")
	assignment := assign(t, pool, job, "Dave Nguyen", "+61412345678")

	t.Run("the actor's clock is kept as given", func(t *testing.T) {
		// Forty minutes ago, in a loading bay with no signal.
		claimed := time.Now().UTC().Add(-40 * time.Minute).Truncate(time.Millisecond)

		id, err := record(t, pool, job, "Picked up", "driver", assignment, nil, claimed)
		if err != nil {
			t.Fatalf("recording a milestone: %v", err)
		}

		var actor, server time.Time
		if err := pool.QueryRow(t.Context(),
			`SELECT actor_recorded_at, server_recorded_at FROM milestones WHERE id = $1`, id,
		).Scan(&actor, &server); err != nil {
			t.Fatalf("reading it back: %v", err)
		}

		if !actor.Equal(claimed) {
			t.Errorf("actor_recorded_at is %s, want the %s the driver claimed; the platform "+
				"does not correct the actor's clock", actor, claimed)
		}
		if !server.After(actor) {
			t.Errorf("server_recorded_at (%s) is not after actor_recorded_at (%s); the two "+
				"clocks have been collapsed into one", server, actor)
		}
	})

	t.Run("a device clock in the future is recorded rather than refused", func(t *testing.T) {
		// A wrong or deliberately altered clock is evidence. Refusing the row would discard
		// the driver's work, which Docs/02 §3.1 forbids: the update is absorbed, not rejected.
		ahead := time.Now().UTC().Add(3 * time.Hour).Truncate(time.Millisecond)

		id, err := record(t, pool, job, "In transit", "driver", assignment, nil, ahead)
		if err != nil {
			t.Fatalf("a milestone with a clock three hours fast was refused: %v", err)
		}

		var actor, server time.Time
		if err := pool.QueryRow(t.Context(),
			`SELECT actor_recorded_at, server_recorded_at FROM milestones WHERE id = $1`, id,
		).Scan(&actor, &server); err != nil {
			t.Fatalf("reading it back: %v", err)
		}
		if !actor.Equal(ahead) {
			t.Errorf("actor_recorded_at is %s, want %s unaltered", actor, ahead)
		}
		if !server.Before(actor) {
			t.Errorf("server_recorded_at (%s) was dragged forward to the device's clock (%s)",
				server, actor)
		}
	})

	t.Run("an offline batch keeps four recorded times and one arrival", func(t *testing.T) {
		batchJob := newDeliveryJob(t, pool, "batch@example.com", "+61400000611")
		driver := assign(t, pool, batchJob, "Priya Sharma", "+61498765432")

		morning := time.Now().UTC().Add(-5 * time.Hour).Truncate(time.Millisecond)
		claimed := map[string]time.Time{
			"En route to pickup": morning,
			"Picked up":          morning.Add(30 * time.Minute),
			"In transit":         morning.Add(35 * time.Minute),
			"Delivered":          morning.Add(2 * time.Hour),
		}

		// One transaction, because that is how a sync worker drains its queue — and because
		// now() is transaction start time, so every row genuinely shares one arrival.
		tx, err := pool.Begin(t.Context())
		if err != nil {
			t.Fatalf("beginning: %v", err)
		}
		defer func() { _ = tx.Rollback(t.Context()) }()

		for milestone, at := range claimed {
			id, _ := uuid.NewV7()
			if _, err := tx.Exec(t.Context(), `
				INSERT INTO milestones (id, job_id, milestone, actor_type, actor_id, actor_recorded_at)
				VALUES ($1, $2, $3, 'driver', $4, $5)`,
				id, batchJob, milestone, driver, at); err != nil {
				t.Fatalf("recording %s: %v", milestone, err)
			}

			// The photograph, in the same transaction, because SHIP-118's deferred trigger
			// refuses a delivered milestone with nothing behind it — and because that is what a
			// sync worker actually drains: a driver who photographed a delivery in a yard with
			// no signal has a queue holding both, and they arrive together or not at all.
			if milestone == "Delivered" {
				if err := storeExceptionIn(t, tx, batchJob, id, "camera_unavailable"); err != nil {
					t.Fatalf("recording the evidence for the batch's delivery: %v", err)
				}
			}
		}
		if err := tx.Commit(t.Context()); err != nil {
			t.Fatalf("committing the batch: %v", err)
		}

		rows, err := pool.Query(t.Context(), `
			SELECT milestone, actor_recorded_at, server_recorded_at
			FROM milestones WHERE job_id = $1`, batchJob)
		if err != nil {
			t.Fatalf("reading the batch back: %v", err)
		}
		defer rows.Close()

		arrivals := map[time.Time]bool{}
		seen := 0
		for rows.Next() {
			var milestone string
			var actor, server time.Time
			if err := rows.Scan(&milestone, &actor, &server); err != nil {
				t.Fatalf("scanning: %v", err)
			}
			seen++
			arrivals[server] = true
			if want := claimed[milestone]; !actor.Equal(want) {
				t.Errorf("%s was recorded at %s, want %s", milestone, actor, want)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterating: %v", err)
		}
		if seen != len(claimed) {
			t.Errorf("read %d milestones back, want %d", seen, len(claimed))
		}
		if len(arrivals) != 1 {
			t.Errorf("the batch has %d arrival times, want 1: the rows were written in one "+
				"transaction and now() is transaction start time", len(arrivals))
		}
	})
}

// TestTheServerClockIsNotTheCallersToSet is the half of SHIP-110 a second column alone does not
// give you.
//
// Two columns are worth nothing if a caller can write the same value to both — the record would
// then say the platform received a milestone at exactly the moment a device claims to have
// recorded it, which is the one thing that never happens and precisely what somebody hiding a
// backdated delivery would write. 000401 keeps this by convention, because one function does its
// writing. Milestones are written from the driver portal, the app's sync worker and the admin
// panel, so it is refused instead.
func TestTheServerClockIsNotTheCallersToSet(t *testing.T) {
	pool := pgtest.DB(t)
	job := newDeliveryJob(t, pool, "server-clock@example.com", "+61400000612")
	assignment := assign(t, pool, job, "Dave Nguyen", "+61412345678")

	backdated := time.Now().UTC().Add(-6 * time.Hour)

	// **'In transit' rather than 'Delivered', and SHIP-118 is why.** The clock rule is the same for
	// every milestone; a delivered one now needs a `proofs` row in the same transaction (000605), so
	// using it here would make each subtest below pass or fail for two reasons at once — and the
	// second subtest commits, which is where the deferred trigger fires.
	t.Run("naming the column is refused", func(t *testing.T) {
		id, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(), `
			INSERT INTO milestones
				(id, job_id, milestone, actor_type, actor_id, actor_recorded_at, server_recorded_at)
			VALUES ($1, $2, 'In transit', 'driver', $3, $4, $5)`,
			id, job, assignment, backdated, backdated)
		if err == nil {
			t.Fatal("a caller set the platform's clock, so a backdated delivery is indistinguishable from a live one")
		}
		if !strings.Contains(err.Error(), "cannot be supplied") {
			t.Errorf("expected milestones_server_clock to refuse it, got: %v", err)
		}
	})

	t.Run("omitting it fills it from the platform's clock", func(t *testing.T) {
		id, err := record(t, pool, job, "In transit", "driver", assignment, nil, backdated)
		if err != nil {
			t.Fatalf("recording a milestone: %v", err)
		}

		// Aged against the database's own clock rather than the test process's. They are
		// different machines here — PostgreSQL is in a container — and a sub-millisecond skew
		// between them is normal and would make a comparison against time.Now() flake.
		var ageSeconds float64
		var server time.Time
		if err := pool.QueryRow(t.Context(), `
			SELECT server_recorded_at, EXTRACT(EPOCH FROM (now() - server_recorded_at))
			FROM milestones WHERE id = $1`, id).Scan(&server, &ageSeconds); err != nil {
			t.Fatalf("reading it back: %v", err)
		}

		if ageSeconds < 0 || ageSeconds > 60 {
			t.Errorf("server_recorded_at is %.3fs old by the database's own clock; it should "+
				"be the moment the row arrived", ageSeconds)
		}
		if server.Equal(backdated) {
			t.Errorf("server_recorded_at is %s, the time the caller wanted; the trigger did "+
				"not replace it", server)
		}
	})
}

// TestMilestoneIsAppendOnly is the same control 000003 puts on audit_log and 000401 on
// job_status_history.
//
// Docs/02 §3.1 requires a queued update that contradicts an administrative action to be
// *retained* with its reason, so this table holds attempts as well as outcomes — and the value of
// both rests on nobody being able to tidy them afterwards, including at a psql prompt. It is also
// the second half of the dual-timestamp guarantee: the actor's claim cannot be corrected later
// any more than the platform's record can.
func TestMilestoneIsAppendOnly(t *testing.T) {
	pool := pgtest.DB(t)
	job := newDeliveryJob(t, pool, "append-only-milestone@example.com", "+61400000613")
	assignment := assign(t, pool, job, "Dave Nguyen", "+61412345678")

	// 'In transit' rather than 'Delivered', because 000605 requires a delivered milestone to carry
	// evidence written in the same transaction and this test is about neither. Every milestone is
	// append-only, so any of the five demonstrates it.
	claimed := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	id, err := record(t, pool, job, "In transit", "driver", assignment, "left with neighbour", claimed)
	if err != nil {
		t.Fatalf("recording a milestone: %v", err)
	}

	t.Run("correcting the actor's clock is refused", func(t *testing.T) {
		_, err := pool.Exec(t.Context(),
			`UPDATE milestones SET actor_recorded_at = now() WHERE id = $1`, id)
		if err == nil {
			t.Fatal("the platform rewrote what the driver claimed")
		}
		if !strings.Contains(err.Error(), "append-only") {
			t.Errorf("expected the append-only trigger to refuse it, got: %v", err)
		}
	})

	t.Run("rewriting the reason is refused", func(t *testing.T) {
		_, err := pool.Exec(t.Context(),
			`UPDATE milestones SET reason = 'something else' WHERE id = $1`, id)
		if err == nil {
			t.Fatal("a recorded milestone was rewritten")
		}
		if !strings.Contains(err.Error(), "append-only") {
			t.Errorf("expected the append-only trigger to refuse it, got: %v", err)
		}
	})

	t.Run("delete is refused", func(t *testing.T) {
		_, err := pool.Exec(t.Context(), `DELETE FROM milestones WHERE id = $1`, id)
		if err == nil {
			t.Fatal("a recorded milestone was deleted")
		}
		if !strings.Contains(err.Error(), "append-only") {
			t.Errorf("expected the append-only trigger to refuse it, got: %v", err)
		}
	})

	t.Run("the milestone survived all three attempts", func(t *testing.T) {
		var actor time.Time
		var reason string
		if err := pool.QueryRow(t.Context(),
			`SELECT actor_recorded_at, reason FROM milestones WHERE id = $1`, id,
		).Scan(&actor, &reason); err != nil {
			t.Fatalf("reading it back: %v", err)
		}
		if !actor.Equal(claimed) || reason != "left with neighbour" {
			t.Errorf("the milestone reads %s / %q, want the original", actor, reason)
		}
	})
}

// TestMilestoneConstraints covers the rules 000601 carries, each of which is a product decision
// from Docs/01 §3 or Docs/02 §3 rather than a tidiness one.
func TestMilestoneConstraints(t *testing.T) {
	pool := pgtest.DB(t)
	job := newDeliveryJob(t, pool, "milestone-rules@example.com", "+61400000614")
	provider := newUser(t, pool, "milestone-provider@example.com", "+61400000615", "provider")
	assignment := assign(t, pool, job, "Dave Nguyen", "+61412345678")
	now := time.Now().UTC()

	t.Run("an administrator must say why", func(t *testing.T) {
		// Docs/02 §3: an administrator may act, "acting with an audit reason". Docs/01 §3
		// forbids changing a commercial record without one.
		//
		// 'In transit' rather than 'Delivered' since SHIP-118: a delivered milestone needs
		// evidence in the same transaction (000605) whoever records it, **an administrator
		// included** — CLAUDE.md's invariant has no actor exemption in it — so recording one
		// here would refuse for a second reason and the audit rule would go untested.
		if _, err := record(t, pool, job, "In transit", "admin", uuid.New(), nil, now); err == nil {
			t.Error("an administrator recorded a delivery with no reason")
		} else if !strings.Contains(err.Error(), "ck_milestones_admin_reason") {
			t.Errorf("expected ck_milestones_admin_reason to refuse it, got: %v", err)
		}

		if _, err := record(t, pool, job, "In transit", "admin", uuid.New(), "driver phone flat, confirmed by call", now); err != nil {
			t.Errorf("an administrator with a reason was refused: %v", err)
		}
	})

	t.Run("the platform has no identity and everyone else has one", func(t *testing.T) {
		// Picked up -> In transit as an automatic presentation change (Docs/02 §2).
		if _, err := record(t, pool, job, "In transit", "system", nil, nil, now); err != nil {
			t.Errorf("the platform cannot record its own presentation change: %v", err)
		}
		if _, err := record(t, pool, job, "In transit", "system", uuid.New(), nil, now); err == nil {
			t.Error("a system milestone claimed an identity")
		}
		if _, err := record(t, pool, job, "In transit", "driver", nil, nil, now); err == nil {
			t.Error("a driver milestone was recorded with no assignment attached")
		}
	})

	t.Run("a provider records against their account and a driver against their assignment", func(t *testing.T) {
		// The polymorphic actor_id 000401 declared, exercised in both of the forms that
		// matter. Neither has a foreign key, because the column points at three tables.
		if _, err := record(t, pool, job, "En route to pickup", "provider", provider, nil, now); err != nil {
			t.Errorf("a provider could not record a milestone: %v", err)
		}
		if _, err := record(t, pool, job, "En route to pickup", "driver", assignment, nil, now); err != nil {
			t.Errorf("a driver could not record a milestone against their assignment: %v", err)
		}
	})

	t.Run("a customer is not a delivery actor", func(t *testing.T) {
		customer := newUser(t, pool, "milestone-customer@example.com", "+61400000616", "customer")
		if _, err := record(t, pool, job, "Delivered", "customer", customer, nil, now); err == nil {
			t.Error("a customer recorded a delivery milestone; Docs/02 §3 restricts them to " +
				"the provider, their driver and an administrator")
		}
	})

	t.Run("a job status that is not a milestone is refused", func(t *testing.T) {
		for _, status := range []string{"Completed", "Cancelled", "Open", "Arrived"} {
			if _, err := record(t, pool, job, status, "driver", assignment, nil, now); err == nil {
				t.Errorf("%q was accepted as a milestone", status)
			}
		}
	})

	t.Run("a milestone belongs to a real job", func(t *testing.T) {
		stranger, _ := uuid.NewV7()
		if _, err := record(t, pool, stranger, "Picked up", "driver", assignment, nil, now); err == nil {
			t.Error("a milestone was recorded against a job that does not exist")
		} else if !strings.Contains(err.Error(), "fk_milestones_job") {
			t.Errorf("expected the foreign key to refuse it, got: %v", err)
		}
	})

	t.Run("deleting the job is refused while a milestone exists", func(t *testing.T) {
		_, err := pool.Exec(t.Context(), `DELETE FROM jobs WHERE id = $1`, job)
		if err == nil {
			t.Fatal("the job was deleted, taking the delivery record with it")
		}
	})
}

// TestARepeatedMilestoneIsRecorded is the absence of a unique constraint, tested because the
// absence is a decision and not an oversight.
//
// Two cases, both from Docs/02. A driver reaches a pickup, finds nobody there and returns later:
// Docs/02 §5 calls a failed attempt an ordinary outcome, and both attempts happened. And a queued
// "Picked up" arriving after "In transit" is already recorded "must be absorbed, not rejected as
// an error" (§3.1) — a unique index on (job_id, milestone) would refuse both, and SHIP-112 would
// have nowhere to write the late arrival.
func TestARepeatedMilestoneIsRecorded(t *testing.T) {
	pool := pgtest.DB(t)
	job := newDeliveryJob(t, pool, "repeat@example.com", "+61400000617")
	assignment := assign(t, pool, job, "Dave Nguyen", "+61412345678")

	base := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Millisecond)

	if _, err := record(t, pool, job, "En route to pickup", "driver", assignment, nil, base); err != nil {
		t.Fatalf("the first attempt: %v", err)
	}
	if _, err := record(t, pool, job, "En route to pickup", "driver", assignment, nil, base.Add(time.Hour)); err != nil {
		t.Errorf("a second attempt at the same pickup was refused: %v", err)
	}

	// Out of order: In transit arrives first, then the queued Picked up that preceded it.
	if _, err := record(t, pool, job, "In transit", "driver", assignment, nil, base.Add(90*time.Minute)); err != nil {
		t.Fatalf("recording In transit: %v", err)
	}
	if _, err := record(t, pool, job, "Picked up", "driver", assignment, nil, base.Add(80*time.Minute)); err != nil {
		t.Errorf("a late Picked up was refused after In transit was recorded; Docs/02 §3.1 "+
			"requires it to be absorbed: %v", err)
	}

	// The timeline reads in the order the driver acted, which is what idx_milestones_job
	// serves and what the customer is shown.
	rows, err := pool.Query(t.Context(), `
		SELECT milestone FROM milestones WHERE job_id = $1 ORDER BY actor_recorded_at`, job)
	if err != nil {
		t.Fatalf("reading the timeline: %v", err)
	}
	defer rows.Close()

	var timeline []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		timeline = append(timeline, m)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}

	want := []string{"En route to pickup", "En route to pickup", "Picked up", "In transit"}
	if strings.Join(timeline, " -> ") != strings.Join(want, " -> ") {
		t.Errorf("the timeline reads %v, want %v", timeline, want)
	}
}

// TestMilestoneForeignKeyIsIndexed is Docs/10 §3.3's foreign key rule, and here the index is the
// read path as well: every milestone on one job is what the customer's timeline and the driver
// portal both ask for.
func TestMilestoneForeignKeyIsIndexed(t *testing.T) {
	pool := pgtest.DB(t)

	var indexes int
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*)
		FROM pg_index i
		JOIN pg_class t     ON t.oid = i.indrelid
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = i.indkey[0]
		WHERE t.relname = 'milestones' AND a.attname = 'job_id'`,
	).Scan(&indexes); err != nil {
		t.Fatalf("looking for the index: %v", err)
	}
	if indexes == 0 {
		t.Error("milestones.job_id carries a foreign key and no index leading with it")
	}
}

// TestMilestoneTimestampsCarryTheirZone is Docs/10 §3.3's "timestamptz always", and it matters
// more on this table than anywhere else in the schema: these two columns exist to be compared
// with each other, and a driver's clock and the platform's are frequently not in the same zone.
func TestMilestoneTimestampsCarryTheirZone(t *testing.T) {
	assertTimestamptz(t, "milestones", "actor_recorded_at", "server_recorded_at")
}

// --- helpers ---------------------------------------------------------------------------

// constraintLiterals pulls the string literals out of a CHECK constraint's definition.
//
// PostgreSQL rewrites `x IN ('a', …)` as `x = ANY (ARRAY['a'::text, …])`, so what comes back from
// pg_get_constraintdef is not the text that was written, and comparing the definition as a string
// would fail on formatting rather than on content.
//
// quotedLiteral is jobs_test.go's, in this same external test package. Reusing it rather than
// declaring a second identical regexp, because two package-level names cannot both be
// quotedLiteral and a differently named copy of one regexp is how the two drift.
func constraintLiterals(t *testing.T, pool *pgxpool.Pool, constraint string) map[string]bool {
	t.Helper()

	var definition string
	if err := pool.QueryRow(t.Context(), `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1`, constraint,
	).Scan(&definition); err != nil {
		t.Fatalf("reading %s out of pg_constraint: %v", constraint, err)
	}

	found := map[string]bool{}
	for _, match := range quotedLiteral.FindAllStringSubmatch(definition, -1) {
		found[match[1]] = true
	}
	if len(found) == 0 {
		t.Fatalf("%s has no string literals in it: %s", constraint, definition)
	}
	return found
}
