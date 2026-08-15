package migrations_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-166's table, checked where its guarantees live.
//
// Docs/04 §9's two-person review. Every assertion here is about a constraint or an index, so all of
// them run against a real PostgreSQL — Docs/06 §4.1: "a mock happily accepts a write that the actual
// constraint would reject", and here the write the constraint rejects is the whole control.

// aModerator writes an admin_users row and answers its id.
//
// A helper of its own rather than admin_authentication_test.go's [newAdministrator], which returns an
// error so that its own tests can assert on a refused role. Every caller here wants a moderator that
// exists, and a `(id, err)` pair at each call site would be noise around the thing being tested.
func aModerator(t *testing.T, pool *pgxpool.Pool, email string) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an administrator id: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO admin_users (id, email, name, password_hash, role)
		 VALUES ($1, $2, 'Verify Administrator', $3, 'moderator')`,
		id, email, aDeveloperHash); err != nil {
		t.Fatalf("inserting %s: %v", email, err)
	}
	return id
}

// newReview writes a pending review and answers its id.
func newReview(t *testing.T, pool *pgxpool.Pool, userID, requestedBy uuid.UUID) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating a review id: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO suspension_reviews (id, user_id, requested_by, reason)
		 VALUES ($1, $2, $3, 'Three unresolved safety reports in a fortnight.')`,
		id, userID, requestedBy); err != nil {
		t.Fatalf("inserting a review: %v", err)
	}
	return id
}

// TestTheApproverMayNotBeTheRequester is Docs/04 §9's control, in the database.
//
// **This is the constraint the whole ticket exists for**, and it is here as well as in
// `internal/admin` because application logic refusing to write is a convention — and a convention
// does not apply to a support query typed at a psql prompt, to a repair script, or to the next
// endpoint somebody adds without reading suspension.go. `000005_users_role_is_immutable` takes the
// same position for the same reason.
//
// Both directions are checked: the same administrator is refused, and a different one is accepted.
// Without the second, a constraint of `false` would pass.
func TestTheApproverMayNotBeTheRequester(t *testing.T) {
	pool := pgtest.DB(t)

	user := newUser(t, pool, "review-subject@example.com", "+61400000401", "provider")
	requester := aModerator(t, pool, "review-requester@example.com")
	approver := aModerator(t, pool, "review-approver@example.com")

	t.Run("the requester cannot approve their own request", func(t *testing.T) {
		review := newReview(t, pool, user, requester)

		_, err := pool.Exec(t.Context(),
			`UPDATE suspension_reviews
			    SET status = 'approved', approved_by = $2, approved_at = now()
			  WHERE id = $1`, review, requester)
		if err == nil {
			t.Fatal("one administrator completed a two-person review alone.\n" +
				"Docs/04 §9 requires two people, and a rule one person can satisfy is not a " +
				"control — it is a control-shaped comment.")
		}
		if !strings.Contains(err.Error(), "ck_suspension_reviews_two_people") {
			t.Errorf("refused by something other than the two-person constraint: %v", err)
		}

		// Cleared so the partial unique index below does not refuse the next case for the
		// wrong reason.
		if _, err := pool.Exec(t.Context(),
			`DELETE FROM suspension_reviews WHERE id = $1`, review); err != nil {
			t.Fatalf("clearing the review: %v", err)
		}
	})

	t.Run("a second administrator can", func(t *testing.T) {
		review := newReview(t, pool, user, requester)

		if _, err := pool.Exec(t.Context(),
			`UPDATE suspension_reviews
			    SET status = 'approved', approved_by = $2, approved_at = now()
			  WHERE id = $1`, review, approver); err != nil {
			t.Fatalf("a different administrator could not approve the review: %v\n"+
				"A constraint that refuses everybody is not the one this table needs.", err)
		}
	})
}

// TestAnAccountMayHaveOnlyOnePendingReview is `uq_suspension_reviews_one_pending`.
//
// More than tidiness: two pending reviews would let one administrator approve the *other's* request
// while their own waited — the letter of a two-person rule with one person driving both halves.
//
// The index is **partial**, so an account may have any number of settled reviews and exactly one
// waiting. Both halves are checked, because a plain unique index would pass the first and fail the
// second — and the second is the ordinary case after any account has been reviewed twice.
func TestAnAccountMayHaveOnlyOnePendingReview(t *testing.T) {
	pool := pgtest.DB(t)

	user := newUser(t, pool, "pending-subject@example.com", "+61400000402", "provider")
	first := aModerator(t, pool, "pending-first@example.com")
	second := aModerator(t, pool, "pending-second@example.com")

	review := newReview(t, pool, user, first)

	id, _ := uuid.NewV7()
	_, err := pool.Exec(t.Context(),
		`INSERT INTO suspension_reviews (id, user_id, requested_by, reason)
		 VALUES ($1, $2, $3, 'A second administrator asking for the same thing at once.')`,
		id, user, second)
	if err == nil {
		t.Fatal("an account has two suspension reviews waiting at once")
	}
	if !strings.Contains(err.Error(), "uq_suspension_reviews_one_pending") {
		t.Errorf("refused by something other than the partial unique index: %v", err)
	}

	// Settle the first, and a second may then be opened — which is what makes the index partial
	// rather than a rule that an account is reviewable once for ever.
	if _, err := pool.Exec(t.Context(),
		`UPDATE suspension_reviews
		    SET status = 'approved', approved_by = $2, approved_at = now()
		  WHERE id = $1`, review, second); err != nil {
		t.Fatalf("approving the first review: %v", err)
	}

	if _, err := pool.Exec(t.Context(),
		`INSERT INTO suspension_reviews (id, user_id, requested_by, reason)
		 VALUES ($1, $2, $3, 'A later review, after the first was settled.')`,
		id, user, second); err != nil {
		t.Errorf("a second review could not be opened after the first was settled: %v\n"+
			"The index is partial on status = 'pending' for exactly this case.", err)
	}
}

// TestAnApprovalIsCompleteOrAbsent is `ck_suspension_reviews_approval_is_complete`.
//
// A row claiming to be approved by nobody is the shape a partial write leaves, and it is the shape
// somebody reading this table would trust — "approved" with no name beside it reads as an approval
// whose record was lost rather than as one that never happened.
func TestAnApprovalIsCompleteOrAbsent(t *testing.T) {
	pool := pgtest.DB(t)

	user := newUser(t, pool, "complete-subject@example.com", "+61400000403", "provider")
	requester := aModerator(t, pool, "complete-requester@example.com")
	approver := aModerator(t, pool, "complete-approver@example.com")
	review := newReview(t, pool, user, requester)

	for name, statement := range map[string]string{
		"approved with no approver": `UPDATE suspension_reviews SET status = 'approved' WHERE id = $1`,
		"an approver with no instant": `UPDATE suspension_reviews
			 SET status = 'approved', approved_by = '` + approver.String() + `' WHERE id = $1`,
		"an approver on a pending review": `UPDATE suspension_reviews
			 SET approved_by = '` + approver.String() + `', approved_at = now() WHERE id = $1`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := pool.Exec(t.Context(), statement, review)
			if err == nil {
				t.Fatal("a half-approved review was stored, which reads to the next person " +
					"as an approval whose record was lost")
			}
			if !strings.Contains(err.Error(), "ck_suspension_reviews_approval_is_complete") {
				t.Errorf("refused by something other than the completeness constraint: %v", err)
			}
		})
	}
}

// TestASuspensionReviewMustRecordAReason is `ck_suspension_reviews_reason`.
//
// **The tab and the newline are the cases.** PostgreSQL's one-argument `btrim` strips spaces only,
// which `ck_admin_notes_body` (SHIP-162) got wrong: a body of a single newline satisfied the
// constraint while `strings.TrimSpace` in the service refused the same value. `000803` spells the
// character set out for that reason, and this is what holds it there.
//
// It matters more here than on a note: the reason is what the second administrator is being asked to
// agree with, and a review carrying a blank one is a signature on an empty page.
func TestASuspensionReviewMustRecordAReason(t *testing.T) {
	pool := pgtest.DB(t)

	user := newUser(t, pool, "reason-subject@example.com", "+61400000404", "provider")
	requester := aModerator(t, pool, "reason-requester@example.com")

	for _, blank := range []string{"", " ", "  ", "\t", "\n", "\r", " \t\r\n "} {
		id, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(),
			`INSERT INTO suspension_reviews (id, user_id, requested_by, reason)
			 VALUES ($1, $2, $3, $4)`, id, user, requester, blank)
		if err == nil {
			t.Errorf("a review with a reason of %q was stored", blank)
			// Cleared so the partial unique index does not make the next case fail for the
			// wrong reason.
			if _, err := pool.Exec(t.Context(),
				`DELETE FROM suspension_reviews WHERE id = $1`, id); err != nil {
				t.Fatalf("clearing the review: %v", err)
			}
			continue
		}
		if !strings.Contains(err.Error(), "ck_suspension_reviews_reason") {
			t.Errorf("a reason of %q was refused by something other than ck_suspension_reviews_reason: %v",
				blank, err)
		}
	}
}

// TestAReviewNeedsARealAccountAndARealAdministrator.
//
// Both foreign keys are RESTRICT, per Docs/10 §3.3. The cascade is the dangerous direction: a deleted
// administrator would take with them the record that they asked for a suspension, which is the half
// of a two-person control that says who the two people were.
func TestAReviewNeedsARealAccountAndARealAdministrator(t *testing.T) {
	pool := pgtest.DB(t)

	user := newUser(t, pool, "fk-subject@example.com", "+61400000405", "provider")
	requester := aModerator(t, pool, "fk-requester@example.com")

	t.Run("an account that does not exist", func(t *testing.T) {
		id, _ := uuid.NewV7()
		stranger, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(),
			`INSERT INTO suspension_reviews (id, user_id, requested_by, reason)
			 VALUES ($1, $2, $3, 'A review of an account nobody has.')`,
			id, stranger, requester)
		if err == nil {
			t.Fatal("a suspension review was opened against an account that does not exist")
		}
		if !strings.Contains(err.Error(), "fk_suspension_reviews_user") {
			t.Errorf("refused by something other than the foreign key: %v", err)
		}
	})

	t.Run("an administrator who does not exist", func(t *testing.T) {
		id, _ := uuid.NewV7()
		stranger, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(),
			`INSERT INTO suspension_reviews (id, user_id, requested_by, reason)
			 VALUES ($1, $2, $3, 'A review requested by nobody in particular.')`,
			id, user, stranger)
		if err == nil {
			t.Fatal("a suspension review names a requester who does not exist, which is a " +
				"two-person rule with an anonymous participant")
		}
		if !strings.Contains(err.Error(), "fk_suspension_reviews_requested_by") {
			t.Errorf("refused by something other than the foreign key: %v", err)
		}
	})
}
