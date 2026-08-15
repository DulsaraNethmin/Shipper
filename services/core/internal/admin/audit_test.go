package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/passwords"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/ratelimit"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/redistest"
)

// SHIP-150 against a real PostgreSQL, with the *Done when* read as a claim about **completeness**.
//
// "All admin mutations write an audit entry; verified by test." A suite that checks two of the three
// mutations proves nothing about the third, and the value of this ticket is entirely in the third —
// an audit trail with a hole in it is worse than no trail, because a reader draws conclusions from
// the absence of an entry. So the test that matters here is
// [TestEveryAdminMutationWritesAnAuditEntry], and it is written to fail when a mutation stops
// writing rather than to pass when the ones somebody remembered still do.
//
// # Why the mutations are driven over HTTP rather than by calling the service
//
// The audit write for creating an administrator depends on the handler reading the acting
// administrator out of the grant and putting it on the command. A test that called
// [Credentials.Create] directly would supply that itself and would pass against a handler that had
// stopped doing it — which is exactly the mutation this suite has to catch. Driving the handler
// through [RequireAdmin] means the actor comes from a real session resolved against a real row,
// which is the only arrangement where "the entry names who actually did it" is being checked.
//
// The one exception is the store-level and validation tests at the bottom, which are about the
// writer rather than about a mutation.

// auditFixture is a credentials service, a verifier, a handler and the clock all three read.
type auditFixture struct {
	creds   *Credentials
	auth    *Authenticator
	pool    *pgxpool.Pool
	clk     *clock.Fixed
	handler *Handler
}

func newAuditFixture(t *testing.T) auditFixture {
	t.Helper()

	creds, auth, pool, clk := adminAuth(t)

	handler, err := NewHandler(testServices(t, creds, pool, clk), pool, testLogger())
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}
	return auditFixture{creds: creds, auth: auth, pool: pool, clk: clk, handler: handler}
}

// signedIn creates an administrator and returns them with a live session token.
func (f auditFixture) signedIn(t *testing.T, email string, role Role, ip string) (Administrator, string) {
	t.Helper()

	administrator := anAdministrator(t, f.creds, email, role)
	issued, _, err := signIn(t, f.creds, email, testPassword, ip)
	if err != nil {
		t.Fatalf("signing %s in: %v", email, err)
	}
	return administrator, issued.Token
}

// storedEntry is one `audit_log` row as a test reads it back.
type storedEntry struct {
	ID         uuid.UUID
	ActorType  string
	ActorID    *uuid.UUID
	Action     string
	TargetType string
	TargetID   uuid.UUID
	Reason     *string
	Metadata   map[string]any
	CreatedAt  time.Time
}

// entryIDs is every audit entry in the database right now.
//
// A set rather than a count, because "exactly one new entry" is the assertion and a count would be
// satisfied by one entry disappearing while another arrived — which the append-only triggers make
// impossible, and which is precisely the kind of thing a test should not be quietly assuming.
func entryIDs(t *testing.T, pool *pgxpool.Pool) map[uuid.UUID]bool {
	t.Helper()

	rows, err := pool.Query(t.Context(), `SELECT id FROM audit_log`)
	if err != nil {
		t.Fatalf("reading the audit log: %v", err)
	}
	defer rows.Close()

	seen := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("reading an audit entry id: %v", err)
		}
		seen[id] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the audit log: %v", err)
	}
	return seen
}

// entriesAddedSince returns every entry written after the given snapshot, oldest first.
func entriesAddedSince(t *testing.T, pool *pgxpool.Pool, before map[uuid.UUID]bool) []storedEntry {
	t.Helper()

	rows, err := pool.Query(t.Context(), `
		SELECT id, actor_type, actor_id, action, target_type, target_id, reason, metadata, created_at
		FROM audit_log
		ORDER BY id`)
	if err != nil {
		t.Fatalf("reading the audit log: %v", err)
	}
	defer rows.Close()

	var added []storedEntry
	for rows.Next() {
		var e storedEntry
		if err := rows.Scan(&e.ID, &e.ActorType, &e.ActorID, &e.Action, &e.TargetType,
			&e.TargetID, &e.Reason, &e.Metadata, &e.CreatedAt); err != nil {
			t.Fatalf("reading an audit entry: %v", err)
		}
		if !before[e.ID] {
			added = append(added, e)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the audit log: %v", err)
	}
	return added
}

// adminMutation is one state-changing thing this service lets an administrator cause.
//
// The table below is the *Done when*. Adding a privileged action without adding a row here fails
// [TestEveryAdminMutationWritesAnAuditEntry] through the catalogue check, because the action it
// declares will have no mutation exercising it — and adding one without writing the entry fails
// through the assertion.
type adminMutation struct {
	// name is what the mutation is called in a failure message.
	name string

	// action is the entry the mutation must write.
	action AuditAction

	// targetType is the kind of thing the entry names.
	//
	// Added at SHIP-160, which was the first audited mutation whose target is not an
	// administrator. Before it every row here targeted [AuditTargetAdministrator] and the
	// assertion below said so as a constant — which would have passed a job removal that
	// recorded its target as an administrator, in the field a support search filters on.
	//
	// Empty means [AuditTargetAdministrator], so the three SHIP-147 rows read as they did.
	targetType string

	// run performs it, through the handler and the guard, and returns the account the entry must
	// be attributed to and the thing it must name as its target.
	//
	// # ready separates the fixture from the mutation, and it is not a convenience
	//
	// Every mutation here needs an administrator, and two need a live session — all of which are
	// themselves audited. So a snapshot taken before `run` would count the fixture's own entries
	// and the assertion would be "some entries were written", which is satisfied by a mutation
	// that writes none. The mutation calls `ready()` in the instant between its last setup step
	// and the act being measured, and the caller snapshots there.
	run func(t *testing.T, f auditFixture, ready func()) (actor, target uuid.UUID)
}

// adminMutations is every state-changing administrative action the service serves today.
//
// Six, matching the six mutating routes in `cmd/api/routes_golden.txt` under `/v1/admin/`. The
// pairing between this list and the served surface is checked from the other side by
// TestEveryMutatingAdminRouteIsAudited in cmd/api, which is the only place the route table is
// visible.
func adminMutations() []adminMutation {
	return []adminMutation{
		{
			name:   "signing in",
			action: AuditActionAdministratorSignedIn,
			run: func(t *testing.T, f auditFixture, ready func()) (uuid.UUID, uuid.UUID) {
				t.Helper()

				administrator := anAdministrator(t, f.creds, "in@example.com", RoleSupport)
				ready()

				body := fmt.Sprintf(`{"email":%q,"password":%q}`,
					"in@example.com", testPassword)
				req := httptest.NewRequest(http.MethodPost, "/v1/admin/sessions",
					strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.RemoteAddr = "10.0.50.1:41000"

				rec := httptest.NewRecorder()
				f.handler.SignIn().ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("signing in: status = %d, want 200 (%s)", rec.Code, rec.Body)
				}
				return administrator.ID, administrator.ID
			},
		},
		{
			name:   "signing out",
			action: AuditActionAdministratorSignedOut,
			run: func(t *testing.T, f auditFixture, ready func()) (uuid.UUID, uuid.UUID) {
				t.Helper()

				administrator, token := f.signedIn(t, "out@example.com", RoleSupport, "10.0.51.1")
				ready()

				req := httptest.NewRequest(http.MethodDelete, "/v1/admin/sessions/current", nil)
				req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)

				rec := httptest.NewRecorder()
				RequireAdmin(f.auth)(f.handler.SignOut()).ServeHTTP(rec, req)
				if rec.Code != http.StatusNoContent {
					t.Fatalf("signing out: status = %d, want 204 (%s)", rec.Code, rec.Body)
				}
				return administrator.ID, administrator.ID
			},
		},
		{
			name:   "creating an administrator",
			action: AuditActionAdministratorCreated,
			run: func(t *testing.T, f auditFixture, ready func()) (uuid.UUID, uuid.UUID) {
				t.Helper()

				owner, token := f.signedIn(t, "owner@example.com", RoleOwner, "10.0.52.1")
				ready()

				body := fmt.Sprintf(`{"email":"made@example.com","name":"Made","password":%q,"role":"moderator"}`,
					testPassword)
				req := httptest.NewRequest(http.MethodPost, "/v1/admin/administrators",
					strings.NewReader(body))
				req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")

				rec := httptest.NewRecorder()
				RequireAdmin(f.auth)(f.handler.CreateAdministrator()).ServeHTTP(rec, req)
				if rec.Code != http.StatusCreated {
					t.Fatalf("creating an administrator: status = %d, want 201 (%s)",
						rec.Code, rec.Body)
				}

				// The target is the account that was made, read back rather than guessed —
				// the identifier is the platform's and nothing in the request carries it.
				var created uuid.UUID
				if err := f.pool.QueryRow(t.Context(),
					`SELECT id FROM admin_users WHERE email = 'made@example.com'`,
				).Scan(&created); err != nil {
					t.Fatalf("reading the created administrator: %v", err)
				}
				return owner.ID, created
			},
		},
		{
			name:       "unpublishing a job",
			action:     AuditActionJobUnpublished,
			targetType: AuditTargetJob,
			run: func(t *testing.T, f auditFixture, ready func()) (uuid.UUID, uuid.UUID) {
				t.Helper()

				moderator, token := f.signedIn(t, "unpub@example.com", RoleModerator, "10.0.53.1")

				customerID := newAccount(t, f.pool, "unpub-cust@example.com", "+61400530", "customer")
				jobID := newDraft(t, f.pool, customerID)
				moveJob(t, f.pool, jobID, jobs.User(jobs.ActorCustomer, customerID), jobs.StatusOpen)
				ready()

				req := httptest.NewRequest(http.MethodPost,
					"/v1/admin/jobs/"+jobID.String()+"/unpublish",
					strings.NewReader(`{"reason":"Prohibited goods; removed after review."}`))
				req.SetPathValue("id", jobID.String())
				req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")

				rec := httptest.NewRecorder()
				RequireAdmin(f.auth)(f.handler.UnpublishJob()).ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("unpublishing: status = %d, want 200 (%s)", rec.Code, rec.Body)
				}
				return moderator.ID, jobID
			},
		},
		{
			name:       "changing an account's standing",
			action:     AuditActionUserStandingChanged,
			targetType: AuditTargetUser,
			run: func(t *testing.T, f auditFixture, ready func()) (uuid.UUID, uuid.UUID) {
				t.Helper()

				moderator, token := f.signedIn(t, "standing@example.com", RoleModerator, "10.0.54.1")
				userID := newAccount(t, f.pool, "standing-subject@example.com", "+61400540", "provider")
				ready()

				req := httptest.NewRequest(http.MethodPost,
					"/v1/admin/users/"+userID.String()+"/standing",
					strings.NewReader(`{"standing":"suspended","reason":"Repeated no-shows across three jobs."}`))
				req.SetPathValue("id", userID.String())
				req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")

				rec := httptest.NewRecorder()
				RequireAdmin(f.auth)(f.handler.SetStanding()).ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Fatalf("setting a standing: status = %d, want 200 (%s)", rec.Code, rec.Body)
				}
				return moderator.ID, userID
			},
		},
		{
			name:   "adding a support note",
			action: AuditActionNoteAdded,

			// The subject, not the note. See AuditActionNoteAdded: an entry naming the note
			// would leave the account's own history with a gap where support's attention was.
			targetType: AuditTargetUser,
			run: func(t *testing.T, f auditFixture, ready func()) (uuid.UUID, uuid.UUID) {
				t.Helper()

				moderator, token := f.signedIn(t, "noter@example.com", RoleModerator, "10.0.55.1")
				userID := newAccount(t, f.pool, "note-subject@example.com", "+61400550", "customer")
				ready()

				body := fmt.Sprintf(
					`{"subject_type":"user","subject_id":%q,"body":"Rang about the damaged crates; sending photographs."}`,
					userID)
				req := httptest.NewRequest(http.MethodPost, "/v1/admin/notes",
					strings.NewReader(body))
				req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)
				req.Header.Set("Content-Type", "application/json")

				rec := httptest.NewRecorder()
				RequireAdmin(f.auth)(f.handler.AddNote()).ServeHTTP(rec, req)
				if rec.Code != http.StatusCreated {
					t.Fatalf("adding a note: status = %d, want 201 (%s)", rec.Code, rec.Body)
				}
				return moderator.ID, userID
			},
		},
	}
}

// TestEveryAdminMutationWritesAnAuditEntry is SHIP-150's *Done when*, read as a completeness claim.
//
// # What each subtest establishes
//
// One mutation, driven through the handler and the guard, with the audit log snapshotted
// immediately before. Afterwards there must be **exactly one** new entry, and it must name the
// action, the administrator who acted and the thing acted on. "Exactly one" is doing work in both
// directions: nothing written fails, and an action that writes two entries — the shape a retry or a
// misplaced call in a loop produces — fails as well.
//
// # The catalogue check is what makes it complete rather than merely thorough
//
// Every action in [AuditActions] must be exercised by a row in [adminMutations], and every row must
// declare an action in the catalogue. So a ticket that adds a privileged action has three things it
// cannot skip: the constant, the write, and a row here that drives it. The failure a person sees is
// the one that names what is missing.
func TestEveryAdminMutationWritesAnAuditEntry(t *testing.T) {
	mutations := adminMutations()

	exercised := map[AuditAction]bool{}
	for _, m := range mutations {
		if !m.action.Valid() {
			t.Errorf("the %s mutation declares %q, which is not in AuditActions.\n"+
				"Add the constant to the catalogue, or correct the mutation.", m.name, m.action)
		}
		if exercised[m.action] {
			t.Errorf("%q is exercised by two mutations; each action is written by one thing",
				m.action)
		}
		exercised[m.action] = true
	}
	for _, action := range AuditActions {
		if !exercised[action] {
			t.Errorf("%q is in the catalogue and no mutation in this file writes it.\n"+
				"SHIP-150's *Done when* is that **all** admin mutations write an entry, and an "+
				"action nothing exercises is a claim nothing checks. Add a row to "+
				"adminMutations, or remove the constant if it is not written yet.", action)
		}
	}

	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			f := newAuditFixture(t)

			// Filled by the mutation's own `ready`, in the instant between its last setup
			// step and the act being measured. Left nil until then so a mutation that never
			// calls it fails on the snapshot rather than silently measuring from zero.
			var before map[uuid.UUID]bool
			actor, target := m.run(t, f, func() { before = entryIDs(t, f.pool) })
			if before == nil {
				t.Fatalf("the %s mutation never called ready(), so there is no point to "+
					"measure from", m.name)
			}
			added := entriesAddedSince(t, f.pool, before)

			if len(added) != 1 {
				t.Fatalf("%s wrote %d audit entries, want exactly 1.\n"+
					"Every admin mutation writes one entry, in the transaction that "+
					"performs it (SHIP-150). None means the action is invisible to "+
					"support; more than one means it is recorded twice and a reader "+
					"cannot tell that from it having happened twice.", m.name, len(added))
			}

			entry := added[0]
			if entry.Action != m.action.String() {
				t.Errorf("action = %q, want %q", entry.Action, m.action)
			}
			if entry.ActorType != AuditActorAdmin.String() {
				t.Errorf("actor_type = %q, want %q", entry.ActorType, AuditActorAdmin)
			}
			if entry.ActorID == nil || *entry.ActorID != actor {
				t.Errorf("actor_id = %v, want the administrator who acted, %s",
					entry.ActorID, actor)
			}
			wantTarget := m.targetType
			if wantTarget == "" {
				wantTarget = AuditTargetAdministrator
			}
			if entry.TargetType != wantTarget {
				t.Errorf("target_type = %q, want %q", entry.TargetType, wantTarget)
			}
			if entry.TargetID != target {
				t.Errorf("target_id = %s, want %s", entry.TargetID, target)
			}
			if entry.Metadata == nil {
				t.Error("metadata is NULL; it is NOT NULL DEFAULT '{}' and the writer supplies " +
					"an object so a reader never has to check")
			}

			// The instant is the injected clock's, which is the decision audit.go's header
			// records. Asserted on every mutation rather than only in the clock test below,
			// because a single call site left on time.Now would pass everything else here.
			if !entry.CreatedAt.Equal(f.clk.Now()) {
				t.Errorf("created_at = %s, want the injected %s.\n"+
					"Docs/11 §9: one row, one clock. This domain injects one, so the "+
					"column takes its value from the domain and not from DEFAULT now().",
					entry.CreatedAt.UTC(), f.clk.Now())
			}
		})
	}
}

// TestEveryAuditActionConstantIsInTheCatalogue.
//
// The same guard [TestEveryPermissionConstantIsInTheCatalogue] puts on permissions, for the same
// failure: a constant declared and left out of [AuditActions] is an action [AuditAction.Valid]
// refuses, so [Auditor.Record] would reject every entry using it — at run time, in production, in
// the one code path nobody exercises by hand.
func TestEveryAuditActionConstantIsInTheCatalogue(t *testing.T) {
	declared := []AuditAction{
		AuditActionAdministratorCreated,
		AuditActionAdministratorSignedIn,
		AuditActionAdministratorSignedOut,
		AuditActionJobUnpublished,
		AuditActionUserStandingChanged,
		AuditActionNoteAdded,
	}

	if len(declared) != len(AuditActions) {
		t.Fatalf("this test lists %d actions and the catalogue holds %d.\n"+
			"Both lists are maintained by hand and this is the only place they meet.",
			len(declared), len(AuditActions))
	}
	for _, a := range declared {
		if !a.Valid() {
			t.Errorf("%q is declared and is not in AuditActions", a)
		}
	}
}

// TestAnAuditEntryTakesTheInjectedClockWhateverTheWallClockSays is the clock decision, written so it
// cannot rot.
//
// # The hazard
//
// `audit_log.created_at` is `DEFAULT now()`, the database's clock, and this domain computes
// everything else from an injected one. Docs/11 §9 names the pattern after this package produced it
// one table along: `admin_sessions.created_at` took the default while its expiries came from the Go
// clock, the suite agreed with the database for exactly one idle window, and then failed for ever.
// **A time bomb rather than a flake** — re-running never clears it.
//
// # Why this cannot rot the same way
//
// It records at a clock far in the past and far in the future, so whenever the suite runs, at least
// one of the two is on the wrong side of `now()`. A column taking the database default could not
// satisfy both, and no choice of "today" makes this pass by luck.
func TestAnAuditEntryTakesTheInjectedClockWhateverTheWallClockSays(t *testing.T) {
	for name, instant := range map[string]time.Time{
		"long before now": time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC),
		"long after now":  time.Date(2039, 11, 12, 13, 14, 15, 0, time.UTC),
	} {
		t.Run(name, func(t *testing.T) {
			f := newAuditFixture(t)

			// The clock is moved **before** the session is issued, not after. A session
			// carries expiries computed from the clock that issued it, so signing in at 2026
			// and then jumping to 2039 does not test the audit entry — it tests that the
			// guard refuses a session that has lapsed, which is another file's job.
			f.clk.Instant = instant
			_, token := f.signedIn(t, "clockowner@example.com", RoleOwner, "10.0.53.1")

			before := entryIDs(t, f.pool)

			body := fmt.Sprintf(`{"email":"at-%d@example.com","name":"At","password":%q}`,
				instant.Year(), testPassword)
			req := httptest.NewRequest(http.MethodPost, "/v1/admin/administrators",
				strings.NewReader(body))
			req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")

			rec := httptest.NewRecorder()
			RequireAdmin(f.auth)(f.handler.CreateAdministrator()).ServeHTTP(rec, req)
			if rec.Code != http.StatusCreated {
				t.Fatalf("creating with the clock at %s: status = %d (%s)",
					instant, rec.Code, rec.Body)
			}

			added := entriesAddedSince(t, f.pool, before)
			if len(added) != 1 {
				t.Fatalf("wrote %d entries, want 1", len(added))
			}
			if !added[0].CreatedAt.Equal(instant) {
				t.Errorf("created_at = %s, want the injected %s.\n"+
					"An entry has to sort against the session row and the status-history "+
					"row it describes, and both take this clock.",
					added[0].CreatedAt.UTC(), instant)
			}
		})
	}
}

// TestAMutationThatFailsLeavesNoAuditEntry is the other half of "the entry commits with the thing it
// describes".
//
// A refused creation must leave nothing behind — no account and no entry. The interesting direction
// is the second: an entry written before the work, or outside the transaction, would record an
// action that did not happen, and an append-only table has no way to take it back.
func TestAMutationThatFailsLeavesNoAuditEntry(t *testing.T) {
	f := newAuditFixture(t)

	// A support account may not create administrators, so this is refused at the permission
	// check — before anything is written.
	_, token := f.signedIn(t, "nopower@example.com", RoleSupport, "10.0.54.1")

	before := entryIDs(t, f.pool)

	body := fmt.Sprintf(`{"email":"never@example.com","name":"Never","password":%q}`, testPassword)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/administrators", strings.NewReader(body))
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.CreateAdministrator()).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body)
	}

	if added := entriesAddedSince(t, f.pool, before); len(added) != 0 {
		t.Errorf("a refused creation wrote %d audit entries: %+v.\n"+
			"An entry describes something that happened. One written for an action that "+
			"was refused cannot be removed — the table is append-only — so it stays as a "+
			"record of an account that was never created.", len(added), added)
	}

	// And the duplicate-address refusal, which fails *inside* the transaction rather than
	// before it. This is the case a best-effort write outside the transaction would get wrong.
	owner, ownerToken := f.signedIn(t, "dup-owner@example.com", RoleOwner, "10.0.54.2")
	anAdministratorCreatedBy(t, f.creds, owner.ID, "taken@example.com", RoleSupport)

	before = entryIDs(t, f.pool)

	body = fmt.Sprintf(`{"email":"taken@example.com","name":"Twice","password":%q}`, testPassword)
	req = httptest.NewRequest(http.MethodPost, "/v1/admin/administrators", strings.NewReader(body))
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+ownerToken)
	req.Header.Set("Content-Type", "application/json")

	rec = httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.CreateAdministrator()).ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("re-using an address: status = %d, want 409 (%s)", rec.Code, rec.Body)
	}
	if added := entriesAddedSince(t, f.pool, before); len(added) != 0 {
		t.Errorf("a refused duplicate wrote %d audit entries: %+v", len(added), added)
	}
}

// TestTheCreationEntryRecordsWhatTheAccountMayDo.
//
// The role is the field that makes the entry worth reading. "An account was created" is half a fact;
// "an account was created that can create accounts" is what somebody reviewing this trail is
// actually looking for, and Docs/04 §9's least-privilege control is unauditable without it.
func TestTheCreationEntryRecordsWhatTheAccountMayDo(t *testing.T) {
	f := newAuditFixture(t)
	_, token := f.signedIn(t, "roleowner@example.com", RoleOwner, "10.0.55.1")

	before := entryIDs(t, f.pool)

	body := fmt.Sprintf(`{"email":"promoted@example.com","name":"Promoted","password":%q,"role":"owner"}`,
		testPassword)
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/administrators", strings.NewReader(body))
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.CreateAdministrator()).ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body)
	}

	added := entriesAddedSince(t, f.pool, before)
	if len(added) != 1 {
		t.Fatalf("wrote %d entries, want 1", len(added))
	}
	if got := added[0].Metadata["role"]; got != RoleOwner.String() {
		t.Errorf("metadata.role = %v, want %q.\n"+
			"An entry saying an account was created without saying what it may do is the one "+
			"field a reader most needs.", got, RoleOwner)
	}
	if got := added[0].Metadata["email"]; got != "promoted@example.com" {
		t.Errorf("metadata.email = %v, want the address created", got)
	}
}

// TestAnEntryTheServiceWroteCannotBeRewrittenOrRemoved is CLAUDE.md's invariant, checked against a
// row this service produced rather than one a test inserted by hand.
//
// `migrations/schema_test.go` already holds the triggers to account over an entry it wrote itself.
// This is the same control read from the other end: the rows the platform actually writes are inside
// the guarantee, which is the claim an operator cares about and is not quite the same statement.
func TestAnEntryTheServiceWroteCannotBeRewrittenOrRemoved(t *testing.T) {
	f := newAuditFixture(t)

	before := entryIDs(t, f.pool)
	f.signedIn(t, "immutable@example.com", RoleSupport, "10.0.56.1")

	added := entriesAddedSince(t, f.pool, before)
	if len(added) == 0 {
		t.Fatal("nothing was recorded, so there is nothing to hold to account")
	}
	entry := added[len(added)-1].ID

	if _, err := f.pool.Exec(t.Context(),
		`UPDATE audit_log SET reason = 'rewritten' WHERE id = $1`, entry); err == nil {
		t.Error("an entry this service wrote was rewritten")
	}
	if _, err := f.pool.Exec(t.Context(),
		`DELETE FROM audit_log WHERE id = $1`, entry); err == nil {
		t.Error("an entry this service wrote was deleted")
	}
}

// TestTheWriterRefusesAnEntryItCannotRecordHonestly.
//
// Every case below mirrors a constraint in `000003`. The duplication is deliberate: the database
// refusal is the one that holds for a `psql` prompt, and this one names the field at the call site
// before the caller's whole transaction is aborted with a message about a check constraint.
//
// The system-actor pair is the one worth reading. `ck_audit_log_actor_id` is a two-sided rule — a
// system entry must not name an account and every other entry must — and a writer that enforced only
// one half would let through exactly the entry nobody can attribute afterwards.
func TestTheWriterRefusesAnEntryItCannotRecordHonestly(t *testing.T) {
	f := newAuditFixture(t)

	auditor, err := NewAuditor(f.clk)
	if err != nil {
		t.Fatalf("building the writer: %v", err)
	}

	someone := uuid.New()
	target := uuid.New()

	for name, entry := range map[string]AuditEntry{
		"an action outside the catalogue": {
			Actor:      AdminActor(someone),
			Action:     AuditAction("administrator.vanished"),
			TargetType: AuditTargetAdministrator,
			TargetID:   target,
		},
		"an actor kind the column refuses": {
			Actor:      AuditActor{Type: "robot", ID: someone},
			Action:     AuditActionAdministratorCreated,
			TargetType: AuditTargetAdministrator,
			TargetID:   target,
		},
		"an administrator with no account named": {
			Actor:      AuditActor{Type: AuditActorAdmin},
			Action:     AuditActionAdministratorCreated,
			TargetType: AuditTargetAdministrator,
			TargetID:   target,
		},
		"a system actor claiming an account": {
			Actor:      AuditActor{Type: AuditActorSystem, ID: someone},
			Action:     AuditActionAdministratorCreated,
			TargetType: AuditTargetAdministrator,
			TargetID:   target,
		},
		"nothing said about the target kind": {
			Actor:    AdminActor(someone),
			Action:   AuditActionAdministratorCreated,
			TargetID: target,
		},
		"nothing named as the target": {
			Actor:      AdminActor(someone),
			Action:     AuditActionAdministratorCreated,
			TargetType: AuditTargetAdministrator,
		},
	} {
		t.Run(name, func(t *testing.T) {
			before := entryIDs(t, f.pool)

			if _, err := auditor.Record(t.Context(), f.pool, entry); err == nil {
				t.Error("the writer accepted it")
			}
			if added := entriesAddedSince(t, f.pool, before); len(added) != 0 {
				t.Errorf("a refused entry was written anyway: %+v", added)
			}
		})
	}
}

// TestAFailedAuditWriteIsReportedRatherThanSwallowed is the test SHIP-150's own mutation sweep
// found missing, and it is the one design decision in audit.go that nothing else checks.
//
// # What survived, and why it survived
//
// The decision is that a failure to write the entry **fails the mutation** — the opposite of the
// usual instinct about logging, and right for this table because `Docs/09` puts audit on the
// do-not-cut list for being impossible to backfill. Making [Auditor.Record] return `nil` when the
// INSERT fails passed the entire suite: every other test drives a database where the insert
// succeeds, so the error branch was never taken. The decision was stated in a comment and
// demonstrated by nothing.
//
// # How the write is made to fail without touching the schema
//
// A statement the database refuses puts the transaction into the aborted state, after which every
// subsequent statement in it fails until rollback. That is a real failure at the point Record makes
// its call, needs no DDL, and cannot leave anything behind — the transaction is rolled back either
// way. Dropping the table inside a transaction would work too and would be a far heavier fixture for
// the same signal.
func TestAFailedAuditWriteIsReportedRatherThanSwallowed(t *testing.T) {
	f := newAuditFixture(t)

	auditor, err := NewAuditor(f.clk)
	if err != nil {
		t.Fatalf("building the writer: %v", err)
	}

	entry := AuditEntry{
		Actor:      AdminActor(uuid.New()),
		Action:     AuditActionAdministratorCreated,
		TargetType: AuditTargetAdministrator,
		TargetID:   uuid.New(),
	}

	rollback := errors.New("this transaction is deliberately abandoned")

	err = db.InTx(t.Context(), f.pool, func(ctx context.Context, r db.Runner) error {
		// Aborts the transaction. The error is deliberately ignored: it is the *state* this
		// leaves behind that the assertion is about, not the division.
		_, _ = r.Exec(ctx, `SELECT 1 / 0`)

		if _, err := auditor.Record(ctx, r, entry); err == nil {
			t.Error("a failed audit write was reported as a success.\n" +
				"An entry that cannot be written must fail the action it describes " +
				"(SHIP-150). Swallowing it leaves a privileged action that happened with " +
				"nothing recording it, in a table that cannot be backfilled.")
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("the fixture transaction did not roll back as intended: %v", err)
	}
}

// TestAWriterWithNoClockIsRefused.
//
// A writer that fell back to time.Now would be the two-clock defect arriving through a convenience,
// and it would be invisible — every entry would still look plausible. See audit.go's header.
func TestAWriterWithNoClockIsRefused(t *testing.T) {
	if _, err := NewAuditor(nil); err == nil {
		t.Error("an audit writer was built with no clock")
	}
}

// TestCredentialsWithNoAuditWriterAreRefused.
//
// The composition-root half of the same rule. A service built without a writer would serve three
// privileged actions and record none of them, and a deployment in that state looks identical to a
// working one until somebody goes looking for an entry that was never written.
func TestCredentialsWithNoAuditWriterAreRefused(t *testing.T) {
	pool := pgtest.DB(t)
	clk := clock.NewFixed(time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC))

	hasher, err := passwords.NewHasher(testProfile)
	if err != nil {
		t.Fatalf("building the hasher: %v", err)
	}
	client, prefix := redistest.Client(t)
	limiter, err := ratelimit.New(client, prefix, clk)
	if err != nil {
		t.Fatalf("building the limiter: %v", err)
	}

	if _, err := NewCredentials(pool, hasher, limiter, clk, nil); err == nil {
		t.Error("a credentials service was built with no audit writer")
	}
}

// TestCreatingAnAdministratorWithNobodyNamedIsRefused.
//
// The structural half of "every entry names who acted". [CreateCommand.ActorID] is required, so an
// account cannot be created through this domain without somebody to attribute it to — which is what
// stops the most privileged action in the console from being the one entry nobody can trace.
func TestCreatingAnAdministratorWithNobodyNamedIsRefused(t *testing.T) {
	f := newAuditFixture(t)

	before := entryIDs(t, f.pool)

	if _, err := f.creds.Create(t.Context(), CreateCommand{
		Email:    "unattributed@example.com",
		Name:     "Unattributed",
		Password: testPassword,
	}); err == nil {
		t.Error("an administrator was created with nobody named as having created them")
	}
	if added := entriesAddedSince(t, f.pool, before); len(added) != 0 {
		t.Errorf("a refused creation wrote %d entries", len(added))
	}
}
