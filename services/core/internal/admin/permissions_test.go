package admin

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/profiles"
)

// testDisputeService is a [Service] the administrator handlers do not use.
//
// [NewHandler] requires one — the dispute endpoint is served by the same handler — and every test
// below calls an administrative method that never reaches it. The two ports are service_test.go's,
// with no job service behind them, which is safe precisely because nothing here raises a dispute.
func testDisputeService(t *testing.T) *Service {
	t.Helper()
	return NewService(testJobs{}, testParties{}, clock.System{})
}

// testLogger discards. What these tests assert is on the wire, and the refusal's log line is the
// half deliberately kept off it (see Handler.permitted).
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testUsers is an account search with no pool, for the same reason as [testModeration]:
// [NewHandler] requires one (SHIP-151) and no test in this file reaches the search endpoint.
func testUsers(t *testing.T) *Users {
	t.Helper()
	u, err := NewUsers(nil)
	if err != nil {
		t.Fatalf("building the account search: %v", err)
	}
	return u
}

// testModeration is a queue service with no pool, for the same reason as [testDisputeService]:
// [NewHandler] requires one and no test below reaches the queue endpoint.
func testModeration(t *testing.T) *Moderation {
	t.Helper()
	m, err := NewModeration(testExceptionQueue{}, testUnsyncedThreshold, nil)
	if err != nil {
		t.Fatalf("building the moderation service: %v", err)
	}
	return m
}

// testServices is every collaborator [NewHandler] requires, built against one pool.
//
// **One helper rather than a call site per test, adopted with [HandlerServices] at SHIP-152.** Each
// test in this package is about one endpoint and supplies real collaborators only for that one; the
// rest exist because the handler serves the whole domain. Before this, adding a service to the
// handler meant editing every construction in the package — which is the churn that makes a test
// suite something people work around rather than extend.
//
// The pool may be nil. Every service in this package accepts one, because the process starts with an
// unreachable database on purpose, and the tests that pass nil are the ones whose endpoint never
// reaches it.
func testServices(t *testing.T, creds *Credentials, pool *pgxpool.Pool, clk clock.Clock) HandlerServices {
	t.Helper()

	users, err := NewUsers(pool)
	if err != nil {
		t.Fatalf("building the account search: %v", err)
	}

	moderation, err := NewModeration(testExceptionQueue{}, testUnsyncedThreshold, pool)
	if err != nil {
		t.Fatalf("building the moderation service: %v", err)
	}

	cancellations, err := NewCancellations(testCancellationQueue{}, pool)
	if err != nil {
		t.Fatalf("building the cancellation queue: %v", err)
	}

	jobConsole, err := NewJobConsole(&testJobDirectory{}, testJobStatuses, pool)
	if err != nil {
		t.Fatalf("building the job search: %v", err)
	}

	trail, err := NewAuditTrail(pool)
	if err != nil {
		t.Fatalf("building the audit trail: %v", err)
	}

	// SHIP-160, SHIP-161. The auditor takes the clock the caller's other rows take, because an
	// entry has to sort against the session and status-history rows it describes (Docs/11 §9 —
	// one row, one clock). A clock of this helper's own would make every assertion about an
	// entry's instant fail for a reason that has nothing to do with the ticket under test.
	auditor, err := NewAuditor(clk)
	if err != nil {
		t.Fatalf("building the audit writer: %v", err)
	}

	enforce, err := NewEnforcement(
		testJobs{svc: jobs.NewService(events.NewOutbox(), clk, nil)}, auditor, pool)
	if err != nil {
		t.Fatalf("building enforcement: %v", err)
	}

	notes, err := NewNotes(auditor, pool)
	if err != nil {
		t.Fatalf("building the notes service: %v", err)
	}

	suspensions, err := NewSuspensions(auditor, pool)
	if err != nil {
		t.Fatalf("building the suspension review: %v", err)
	}

	// SHIP-153, SHIP-154. The port is the *real* one over `internal/profiles`, not a double, and
	// the states come from that package rather than from a list written here — see
	// verifications_test.go. A test file may import another domain (the boundary lint skips
	// `_test.go` for exactly this reason: a test wires domains together the way cmd/api does).
	verifications, err := NewVerifications(
		testProviderVerifications(), testVerificationStates(), auditor, pool)
	if err != nil {
		t.Fatalf("building the verification console: %v", err)
	}

	// SHIP-155. The real `internal/profiles` reader over a fake object store, which is
	// evidence_test.go's split and its reasoning: the records are the schema's to enforce and a
	// double would accept every one of them, while the signature itself is exercised against
	// MinIO by the harness. A suite whose subject is a *different* endpoint still needs this
	// wired, because NewHandler refuses a nil — a console that could not show a reviewer the
	// documents is one that cannot perform Docs/04 §3's review at all.
	evidenceStore := newFakeEvidenceStore()
	evidence, err := NewEvidence(
		testEvidence{documents: profiles.NewDocuments(
			clock.System{}, evidenceStore, evidenceStore, testDocumentPolicy())},
		auditor, pool)
	if err != nil {
		t.Fatalf("building the document viewer: %v", err)
	}

	// SHIP-159. The real `internal/profiles` reader again, over a **configured** lead time
	// rather than an empty one: an empty map is the shipping default and would make every
	// "expiring" assertion vacuous, and expiry_test.go is where both configurations are driven
	// deliberately. The clock is the fixture's, because the boundary between expired and
	// expiring is measured against it.
	expiry, err := NewExpiryQueue(testExpiringDocuments(clk), pool)
	if err != nil {
		t.Fatalf("building the expiry queue: %v", err)
	}

	// SHIP-164. The real lifecycle adapter over the real guard, like enforcement above: the
	// resolution moves a job out of `Disputed` through `jobs.Transition`, so a stub here would
	// prove that `admin` writes an outcome and nothing about whether the job unfroze.
	disputeWorkflow, err := NewDisputeWorkflow(
		testJobs{svc: jobs.NewService(events.NewOutbox(), clk, nil)}, auditor, clk, pool)
	if err != nil {
		t.Fatalf("building the dispute workflow: %v", err)
	}

	return HandlerServices{
		Disputes:      testDisputeService(t),
		Credentials:   creds,
		Moderation:    moderation,
		Cancellations: cancellations,
		Users:         users,
		Jobs:          jobConsole,
		Trail:         trail,
		Enforcement:   enforce,
		Notes:         notes,
		Suspensions:   suspensions,
		Verifications: verifications,
		Evidence:      evidence,
		Expiry:        expiry,

		DisputeWorkflow: disputeWorkflow,

		// SHIP-155a. The real party and message adapters, like the workflow above: the whole of
		// what intake refuses is read out of `jobs`, `bids` and `job_messages`, so a stub would
		// prove that `admin` writes a row and nothing about who was allowed to.
		Reports: NewReports(testParties{}, testMessages{}),
	}
}

// SHIP-148, and the two claims in its *Done when* checked separately.
//
// "Permissions are granular and default to the minimum" fails in two unrelated ways. Granularity
// fails by a handler asking about a *role*, which no test of the permission table would notice.
// The default fails by a role inheriting something it was never given, which is invisible to any
// test that only ever asks about roles that exist.
//
// The database halves are in migrations/admin_authentication_test.go: the column default, and the
// pairing between `ck_admin_users_role` and [Roles]. These are the Go halves.

// TestEveryPermissionConstantIsInTheCatalogue.
//
// A constant declared and left out of [Permissions] is a permission no role can hold and
// [Permission.Valid] refuses — while every other test here passes, because they all read the
// catalogue.
func TestEveryPermissionConstantIsInTheCatalogue(t *testing.T) {
	declared := []Permission{
		PermissionUsersRead,
		PermissionUsersRestrict,
		PermissionJobsRead,
		PermissionJobsUnpublish,
		PermissionVerificationsRead,
		PermissionVerificationsDecide,
		PermissionModerationRead,
		PermissionDisputesRead,
		PermissionDisputesResolve,
		PermissionNotesWrite,
		PermissionAuditRead,
		PermissionAdminsManage,
	}

	if len(declared) != len(Permissions) {
		t.Errorf("%d permission constants and %d in admin.Permissions", len(declared), len(Permissions))
	}
	for _, p := range declared {
		if !p.Valid() {
			t.Errorf("%q is a declared permission the catalogue omits, so no role can hold it", p)
		}
	}

	seen := map[Permission]bool{}
	for _, p := range Permissions {
		if seen[p] {
			t.Errorf("%q appears twice in the catalogue", p)
		}
		seen[p] = true
	}
}

// TestEveryRoleHoldsOnlyCataloguedPermissions.
//
// A permission in a role's bundle that is not in the catalogue is a grant nothing can ever check
// for — the endpoint would name the constant, the constant would not be the string in the map, and
// the refusal would be permanent and silent.
func TestEveryRoleHoldsOnlyCataloguedPermissions(t *testing.T) {
	for _, role := range Roles {
		for _, held := range role.Permissions() {
			if !held.Valid() {
				t.Errorf("%s holds %q, which is not in the catalogue", role, held)
			}
		}
	}
}

// TestTheLeastPrivilegedRoleCanOnlyLook is what "the minimum" means in practice.
//
// [RoleSupport] is what a forgotten role becomes, in Go and in the column default, so what it holds
// is the blast radius of every mistake in that direction. Every permission it holds must be a
// permission to read.
//
// The acting permissions are named individually rather than derived, because deriving them would
// mean writing the rule twice and testing the copy.
func TestTheLeastPrivilegedRoleCanOnlyLook(t *testing.T) {
	// Roles is ordered least privileged first, so this is the minimum by definition rather than
	// by a literal that a reordering would leave stale.
	minimum := Roles[0]

	for _, acting := range []Permission{
		PermissionUsersRestrict,
		PermissionJobsUnpublish,
		PermissionVerificationsDecide,
		PermissionDisputesResolve,
		PermissionNotesWrite,
		PermissionAdminsManage,
	} {
		if minimum.Holds(acting) {
			t.Errorf("the least-privileged role (%s) holds %q, which changes something.\n"+
				"This is the role a forgotten field produces, so everything it holds is the "+
				"blast radius of an omission (Docs/04 §9).", minimum, acting)
		}
	}

	// And it is not empty either — a role nobody can use is not least privilege, it is a broken
	// account, and somebody would fix it by widening something.
	if len(minimum.Permissions()) == 0 {
		t.Errorf("the least-privileged role holds nothing at all, so a support account cannot " +
			"open the console and the first fix anybody reaches for is to widen it")
	}
}

// TestARoleWithNoBundleHoldsNothing is the default-deny property, and it is the one that makes
// adding a fourth role safe.
//
// The zero [Role], a role read from a database that has drifted from `ck_admin_users_role`, and a
// role somebody adds to the vocabulary and forgets to write into [rolePermissions] all reach the
// same place. **The direction matters**: a lookup that fell back to any *existing* role's bundle
// would give an unknown role somebody else's permissions, and the pairing test in `migrations` is
// what stops the third case going unnoticed for a wave.
func TestARoleWithNoBundleHoldsNothing(t *testing.T) {
	for _, unknown := range []Role{"", "administrator", "Support", "superuser", "root"} {
		if held := unknown.Permissions(); len(held) != 0 {
			t.Errorf("the role %q holds %v; a role with no bundle must hold nothing", unknown, held)
		}
		for _, p := range Permissions {
			if unknown.Holds(p) {
				t.Errorf("the role %q holds %q", unknown, p)
			}
		}
	}
}

// TestNoPermissionAuthorisesDeletingAnAuditEntry.
//
// CLAUDE.md's invariant: audit entries are append-only and ordinary administrators cannot delete
// one. SHIP-148 is the ticket most able to break it, because a permission model is exactly where
// somebody adds a delete — and the whole of the defence in Go is that **the catalogue has no such
// permission and therefore no endpoint can name one**.
//
// The check is on the catalogue rather than on the role bundles on purpose. A permission that
// existed and was granted to nobody would still be an endpoint waiting to be written, and the
// endpoint would be an application-level rule standing where a database-level one already stands:
// `000003`'s trigger refuses `UPDATE` and `DELETE` from any connection, which is what makes the rule
// true of a `psql` prompt as well as of this service. The trigger's own test is in
// migrations/schema_test.go and its wire demonstration is in scripts/verify/90-admin.sh.
func TestNoPermissionAuthorisesDeletingAnAuditEntry(t *testing.T) {
	for _, p := range Permissions {
		subject, verb, found := strings.Cut(p.String(), ".")
		if !found {
			t.Errorf("%q is not named <subject>.<verb>", p)
			continue
		}
		if subject != "audit" {
			continue
		}
		if verb != "read" {
			t.Errorf("the catalogue has an audit permission other than `audit.read`: %q.\n"+
				"Audit entries are append-only and ordinary administrators cannot delete them "+
				"(CLAUDE.md, Docs/04 §9). 000003 enforces it with a trigger, so a permission "+
				"here would be an endpoint standing where a database rule already stands.", p)
		}
	}

	// And the same statement from the other end: no role holds anything that writes to audit.
	for _, role := range Roles {
		for _, held := range role.Permissions() {
			if strings.HasPrefix(held.String(), "audit.") && held != PermissionAuditRead {
				t.Errorf("%s holds %q", role, held)
			}
		}
	}
}

// TestPermitsReadsTheRoleOnTheGrantAndNothingElse.
//
// The grant carries the administrator the guard read out of the database **on this request**, which
// is what makes a demotion take effect at once rather than at the next expiry. This is the unit-level
// statement of that; the end-to-end one is in the verify section, where a role is changed by SQL
// between two calls with the same credential.
func TestPermitsReadsTheRoleOnTheGrantAndNothingElse(t *testing.T) {
	for role, want := range map[Role]bool{
		RoleSupport:   false,
		RoleModerator: false,
		RoleOwner:     true,
		"":            false,
		"superuser":   false,
	} {
		grant := Grant{Administrator: Administrator{Role: role, Status: StatusActive}}
		if got := grant.Permits(PermissionAdminsManage); got != want {
			t.Errorf("%q permits admins.manage = %t, want %t", role, got, want)
		}
	}

	// A disabled account permits nothing, whatever its role. Authenticator.Resolve refuses one
	// before a grant exists, so this is unreachable through the served chain — and a Grant is an
	// ordinary struct, so "out of service" must not depend on where the value came from.
	disabled := Grant{Administrator: Administrator{Role: RoleOwner, Status: StatusDisabled}}
	for _, p := range Permissions {
		if disabled.Permits(p) {
			t.Errorf("a disabled owner permits %q", p)
		}
	}
}

// TestARoleCannotBeWidenedByWhatIsHandedToACaller.
//
// [Role.Permissions] returns a copy. A caller that appended to what it was handed — a response
// builder adding "and also this one for the current user" — would otherwise widen the role for the
// whole process, for every administrator, until the next deploy.
func TestARoleCannotBeWidenedByWhatIsHandedToACaller(t *testing.T) {
	held := RoleSupport.Permissions()
	held = append(held, PermissionAdminsManage)
	_ = held

	if RoleSupport.Holds(PermissionAdminsManage) {
		t.Error("appending to what Role.Permissions returned widened the role itself, for every " +
			"administrator in the process")
	}
	if len(RoleSupport.Permissions()) != len(rolePermissions[RoleSupport]) {
		t.Error("the stored bundle changed length")
	}
}

// TestAnUnpermittedAdministratorIsRefusedWithoutBeingToldWhichPermission.
//
// 403 rather than 404: the caller is a verified administrator and the endpoint's existence is not a
// secret from them — telling them "no such thing" would send them looking for a routing fault
// instead of asking for access. That is the opposite of [ErrNotAParty]'s reasoning, and the
// difference is who is asking.
//
// The body must not name the permission. A refusal that enumerated what the platform can do would
// make the console's own surface readable by anybody with the least-privileged account.
func TestAnUnpermittedAdministratorIsRefusedWithoutBeingToldWhichPermission(t *testing.T) {
	creds, auth, _, clk := adminAuth(t)
	anAdministrator(t, creds, "support-only@example.com", RoleSupport)

	issued, _, err := signIn(t, creds, "support-only@example.com", testPassword, "10.0.10.1")
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}

	handler, err := NewHandler(testServices(t, creds, nil, clk), nil, testLogger())
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	guarded := RequireAdmin(auth)(handler.CreateAdministrator())

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/administrators",
		strings.NewReader(`{"email":"new@example.com","name":"New","password":"correct-horse-battery-staple"}`))
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+issued.Token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, string(CodeAdminPermissionDenied)) {
		t.Errorf("body = %s, want %q", body, CodeAdminPermissionDenied)
	}
	if strings.Contains(body, PermissionAdminsManage.String()) {
		t.Errorf("the refusal names the permission it wanted: %s", body)
	}

	// And nothing was created, which is the half a status code alone does not establish.
	if _, _, err := signIn(t, creds, "new@example.com", "correct-horse-battery-staple", "10.0.10.2"); err == nil {
		t.Error("the refused request created the account anyway")
	}
}

// TestAnOwnerCreatesAnAdministratorAndTheDefaultIsStillTheMinimum.
//
// The permitted half of the same endpoint, and it checks the thing a permission test would
// otherwise let through: that being *allowed* to create an administrator does not mean the created
// one inherits anything.
func TestAnOwnerCreatesAnAdministratorAndTheDefaultIsStillTheMinimum(t *testing.T) {
	creds, auth, _, clk := adminAuth(t)
	anAdministrator(t, creds, "owner@example.com", RoleOwner)

	issued, _, err := signIn(t, creds, "owner@example.com", testPassword, "10.0.11.1")
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}

	handler, err := NewHandler(testServices(t, creds, nil, clk), nil, testLogger())
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}
	guarded := RequireAdmin(auth)(handler.CreateAdministrator())

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/administrators",
		strings.NewReader(`{"email":"made@example.com","name":"Made","password":"correct-horse-battery-staple"}`))
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+issued.Token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"role":"`+RoleSupport.String()+`"`) {
		t.Errorf("an owner created an administrator with no role stated and got %s.\n"+
			"The default is the minimum, never the creator's role (SHIP-148).", body)
	}
	if strings.Contains(body, PermissionAdminsManage.String()) {
		t.Errorf("the created administrator holds admins.manage: %s", body)
	}
	if strings.Contains(body, "password") {
		t.Errorf("the response echoes password material: %s", body)
	}
}
