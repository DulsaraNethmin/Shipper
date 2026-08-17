// SHIP-164, checked where each claim actually lives.
//
// The *Done when* is one sentence — "dispute moves through investigation to a documented outcome
// that unfreezes the job" — and it is three claims that fail independently:
//
//   - **found**: an administrator can reach an open dispute and read what was reported;
//   - **documented**: the outcome is recorded with the actor, the instant and a reason, and can be
//     read back afterwards;
//   - **unfrozen**: the job leaves `Disputed` through the guarded transition.
//
// The third is the one a plausible implementation gets almost right, and
// [TestAResolutionIsRefusedWhenItsAuditEntryCannotBeWritten] is the test aimed at *almost*: it takes
// the failing branch after the dispute row has been written and after the job has moved, so a
// resolution that is correct in the happy path and not atomic fails there and nowhere else.
//
// The instrument is enforcement_test.go's and the reasoning is wave 9's: an auditor that fails
// **before any SQL** leaves the transaction healthy, so only a real rollback keeps the job frozen. A
// trigger-based refusal would prove the rollback and prove nothing about the `if err != nil`,
// because PostgreSQL aborts the whole transaction as soon as a statement raises.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
)

// resolutionFixture is the workflow, a handler, and two administrators with live sessions.
//
// Two roles rather than one, because the permission split is half of Docs/04 §9's least privilege
// here: `support` may read a dispute and may not resolve one, and a fixture with only a moderator
// could not tell a working check from an absent one.
type resolutionFixture struct {
	pool     *pgxpool.Pool
	creds    *Credentials
	auth     *Authenticator
	clk      *clock.Fixed
	handler  *Handler
	workflow *DisputeWorkflow

	moderator Administrator
	modToken  string

	support    Administrator
	supToken   string
	customerID uuid.UUID
	providerID uuid.UUID
}

func newResolutionFixture(t *testing.T) resolutionFixture {
	t.Helper()

	creds, auth, pool, clk := adminAuth(t)

	auditor, err := NewAuditor(clk)
	if err != nil {
		t.Fatalf("building the audit writer: %v", err)
	}

	// The real guard with a real outbox, so the `job.status_changed` the customer is notified
	// from is written by the same transaction these tests watch.
	workflow, err := NewDisputeWorkflow(
		testJobs{svc: jobs.NewService(events.NewOutbox(), clk, nil)}, auditor, clk, pool)
	if err != nil {
		t.Fatalf("building the dispute workflow: %v", err)
	}

	services := testServices(t, creds, pool, clk)
	services.DisputeWorkflow = workflow

	handler, err := NewHandler(services, pool, testLogger())
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	moderator := anAdministrator(t, creds, "resolve-mod@example.com", RoleModerator)
	modIssued, _, err := signIn(t, creds, "resolve-mod@example.com", testPassword, "10.0.64.1")
	if err != nil {
		t.Fatalf("signing the moderator in: %v", err)
	}

	support := anAdministrator(t, creds, "resolve-sup@example.com", RoleSupport)
	supIssued, _, err := signIn(t, creds, "resolve-sup@example.com", testPassword, "10.0.64.2")
	if err != nil {
		t.Fatalf("signing the support administrator in: %v", err)
	}

	return resolutionFixture{
		pool: pool, creds: creds, auth: auth, clk: clk,
		handler: handler, workflow: workflow,
		moderator: moderator, modToken: modIssued.Token,
		support: support, supToken: supIssued.Token,
		customerID: newAccount(t, pool, "resolve-cust@example.com", "+61400640", "customer"),
		providerID: newAccount(t, pool, "resolve-prov@example.com", "+61400641", "provider"),
	}
}

// disputedJob is a delivered job frozen by an open dispute, and the dispute's identifier.
//
// Built through the real intake service rather than by inserting a `disputes` row, so the fixture
// exercises the freeze the same way a complainant does and the state under test is one the platform
// can actually reach. Every job transition goes through the guard — `000402`'s trigger refuses a
// bare `UPDATE jobs SET status`, which is what makes this a fixture rather than a shortcut.
func (f resolutionFixture) disputedJob(t *testing.T, suffix string) (jobID, disputeID uuid.UUID) {
	t.Helper()

	jobID = deliveredJob(t, f.pool, f.customerID, f.providerID)

	svc := NewService(
		testJobs{svc: jobs.NewService(events.NewOutbox(), f.clk, nil)}, testParties{}, f.clk)

	raised, _, err := raiseWith(t, f.pool, svc, f.customerID, jobID, Intake{
		Category:       CategoryGoodsDamaged,
		Description:    "Two of the four crates arrived with the sides staved in.",
		DesiredOutcome: "A record of the damage, and the provider contacted about it.",
		OccurredAt:     f.clk.Now().Add(-24 * time.Hour),
		Evidence:       []string{"Photographed the crates at the depot"},
		Key:            "resolve-intake-" + suffix,
	})
	if err != nil {
		t.Fatalf("raising the dispute on %s: %v", jobID, err)
	}

	if s := statusOf(t, f.pool, jobID); s != string(jobs.StatusDisputed) {
		t.Fatalf("the fixture job is %q, want Disputed — the freeze is the state under test", s)
	}
	return jobID, raised.ID
}

// resolve drives POST /v1/admin/disputes/{id}/resolution through the guard and the handler.
func (f resolutionFixture) resolve(t *testing.T, token string, disputeID uuid.UUID, body string) (int, string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost,
		"/v1/admin/disputes/"+disputeID.String()+"/resolution", strings.NewReader(body))
	req.SetPathValue("id", disputeID.String())
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.ResolveDispute()).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// queue drives GET /v1/admin/disputes.
func (f resolutionFixture) queue(t *testing.T, token, query string) (int, string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/disputes"+query, nil)
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.DisputeQueue()).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// open drives GET /v1/admin/disputes/{id}.
func (f resolutionFixture) open(t *testing.T, token string, disputeID uuid.UUID) (int, string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/disputes/"+disputeID.String(), nil)
	req.SetPathValue("id", disputeID.String())
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.OpenDispute()).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// resolvedRow is what the `disputes` row says after a resolution, read straight back.
type resolvedRow struct {
	ResolvedAt *time.Time
	Outcome    *string
	ResolvedBy *uuid.UUID
}

func disputeRow(t *testing.T, pool *pgxpool.Pool, disputeID uuid.UUID) resolvedRow {
	t.Helper()

	var row resolvedRow
	if err := pool.QueryRow(t.Context(),
		`SELECT resolved_at, outcome, resolved_by FROM disputes WHERE id = $1`, disputeID,
	).Scan(&row.ResolvedAt, &row.Outcome, &row.ResolvedBy); err != nil {
		t.Fatalf("reading dispute %s: %v", disputeID, err)
	}
	return row
}

// TestResolvingADisputeUnfreezesTheJob is the *Done when*, both destinations.
//
// # Every claim in the sentence is asserted separately
//
// The response is not the evidence and is checked last. What the test reads is the **rows**: the
// dispute's three resolution columns, the job's status, and the `job_status_history` row the guard
// wrote with the administrator's reason on it. A handler that answered correctly and wrote nothing
// passes an assertion about its own response and fails all three of these.
//
// # Both destinations, because they are two methods on the port
//
// Docs/02 §2 offers `Disputed → Completed` and `Disputed → Cancelled`, and `admin.Jobs` has a method
// for each. One subtest per method, because a mistake in `moveJob`'s switch — the two arms swapped,
// one arm calling the other — is invisible to a test that only exercises one.
func TestResolvingADisputeUnfreezesTheJob(t *testing.T) {
	for _, tc := range []struct {
		name       string
		outcome    Outcome
		jobOutcome JobOutcome
		want       jobs.Status
	}{
		{"delivery accepted", OutcomeDeliveryCompleted, JobOutcomeCompleted, jobs.StatusCompleted},
		{"failed delivery", OutcomeDeliveryFailed, JobOutcomeCancelled, jobs.StatusCancelled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newResolutionFixture(t)
			jobID, disputeID := f.disputedJob(t, tc.name)

			reason := "Reviewed the proof and the messages; " + tc.name + " is what happened."
			code, body := f.resolve(t, f.modToken, disputeID,
				`{"outcome":"`+tc.outcome.Wire()+`","job_outcome":"`+tc.jobOutcome.String()+`",`+
					`"reason":"`+reason+`"}`)
			if code != http.StatusOK {
				t.Fatalf("resolving: status = %d, want 200 (%s)", code, body)
			}

			// Unfrozen. The claim the *Done when* turns on.
			if s := statusOf(t, f.pool, jobID); s != string(tc.want) {
				t.Errorf("the job is %q, want %s — a resolved dispute that leaves the job "+
					"frozen leaves a delivery nothing can complete (Docs/02 §3, §6.1)", s, tc.want)
			}

			// Documented. All three columns, because ck_disputes_resolution binds them and a
			// row with resolved_at alone is off idx_disputes_open with nothing recorded.
			row := disputeRow(t, f.pool, disputeID)
			if row.ResolvedAt == nil {
				t.Fatal("resolved_at is NULL; the dispute is still on the open queue")
			}
			if !row.ResolvedAt.Equal(f.clk.Now()) {
				t.Errorf("resolved_at = %s, want the injected %s.\n"+
					"Docs/11 §9: one row, one clock. The dispute and the audit entry describing "+
					"it must agree about when it happened.", row.ResolvedAt.UTC(), f.clk.Now())
			}
			if row.Outcome == nil || Outcome(*row.Outcome) != tc.outcome {
				t.Errorf("outcome = %v, want %q", row.Outcome, tc.outcome)
			}
			if row.ResolvedBy == nil || *row.ResolvedBy != f.moderator.ID {
				t.Errorf("resolved_by = %v, want the moderator %s", row.ResolvedBy, f.moderator.ID)
			}

			// The reason reached job_status_history, which is what a customer's support
			// conversation reads. ck_job_status_history_admin_reason requires one of an
			// administrator's transition, so an empty one would have failed the insert —
			// what this checks is that the administrator's *own* words got there rather
			// than a placeholder.
			var recorded string
			if err := f.pool.QueryRow(t.Context(),
				`SELECT coalesce(reason, '') FROM job_status_history
				 WHERE job_id = $1 AND to_status = $2`, jobID, string(tc.want)).Scan(&recorded); err != nil {
				t.Fatalf("reading the transition: %v", err)
			}
			if recorded != reason {
				t.Errorf("job_status_history.reason = %q, want %q", recorded, reason)
			}

			// And the response says both vocabularies, so a console renders what happened
			// without a second request.
			var answered resolutionResponse
			if err := json.Unmarshal([]byte(body), &answered); err != nil {
				t.Fatalf("decoding the response: %v", err)
			}
			if answered.Outcome != tc.outcome.Wire() || answered.JobOutcome != tc.jobOutcome.String() {
				t.Errorf("the response says outcome=%q job_outcome=%q, want %q and %q",
					answered.Outcome, answered.JobOutcome, tc.outcome.Wire(), tc.jobOutcome)
			}
			if answered.JobID != jobID.String() {
				t.Errorf("the response names job %s, want %s", answered.JobID, jobID)
			}
		})
	}
}

// TestTheOutcomeAndTheJobDestinationAreIndependent is `000804`s argument, exercised.
//
// **The decision this ticket turns on is that Docs/04 §7's five outcomes and Docs/02 §2's two
// destinations are orthogonal**, and the cheapest way for that to quietly stop being true is for
// somebody to "simplify" the request by deriving one from the other. Three of §7's outcomes map to
// neither destination, so a derivation would have to invent an answer for the majority of the list.
//
// This test pins the pairing that a derivation would refuse: an issue *acknowledged* — the parties
// directed to settle between themselves — on a job that nevertheless **completes**. It is a real
// resolution rather than a contrived one: the customer accepts the delivery and takes the damage up
// with the provider directly, which is exactly what §7's second outcome describes.
func TestTheOutcomeAndTheJobDestinationAreIndependent(t *testing.T) {
	f := newResolutionFixture(t)
	jobID, disputeID := f.disputedJob(t, "orthogonal")

	code, body := f.resolve(t, f.modToken, disputeID,
		`{"outcome":"`+OutcomeIssueAcknowledged.Wire()+`","job_outcome":"completed",`+
			`"reason":"Damage acknowledged; parties are settling it between themselves."}`)
	if code != http.StatusOK {
		t.Fatalf("resolving: status = %d, want 200 (%s)", code, body)
	}

	if s := statusOf(t, f.pool, jobID); s != string(jobs.StatusCompleted) {
		t.Errorf("the job is %q, want Completed", s)
	}

	row := disputeRow(t, f.pool, disputeID)
	if row.Outcome == nil || Outcome(*row.Outcome) != OutcomeIssueAcknowledged {
		t.Fatalf("outcome = %v, want %q.\n"+
			"Docs/04 §7's outcome and Docs/02 §2's destination are two vocabularies. An "+
			"implementation deriving one from the other cannot record this pairing at all.",
			row.Outcome, OutcomeIssueAcknowledged)
	}
}

// TestAResolutionIsRefusedWhenItsAuditEntryCannotBeWritten is the atomicity claim, taken.
//
// # What it establishes that no other test here can
//
// `service.go` named the corrupt state two waves before there was code to produce it: *"The job says
// 'Disputed' and no dispute was open, which is the state a resolved dispute would leave behind if
// SHIP-164's outcome failed to move the job back."* Its mirror image is worse — a dispute marked
// resolved on a job that never moved.
//
// **Every assertion in this file except this one passes against an implementation that is not
// atomic.** A resolution that wrote the dispute in one transaction and moved the job in another
// still resolves the dispute, still unfreezes the job, and still answers correctly on a healthy
// database. Only a failure part-way through tells the two apart, and this is the test that arranges
// one.
//
// # The instrument, and why it is not a trigger
//
// A nil auditor. [Auditor.Record] refuses a nil receiver and returns **before touching the
// database**, so the transaction stays perfectly healthy and only a genuine rollback undoes the two
// writes that precede it. A `BEFORE INSERT` trigger would prove the rollback and prove nothing about
// the code — PostgreSQL aborts the whole transaction as soon as a statement raises, so the failure
// reaches the caller through the COMMIT whether or not anybody checked it. enforcement_test.go
// established both halves of this and this reuses the sharper one.
//
// Constructed by hand rather than through [NewDisputeWorkflow], which refuses a nil auditor. That is
// a test doing on purpose what audit.go's own note calls "something built the service by hand".
func TestAResolutionIsRefusedWhenItsAuditEntryCannotBeWritten(t *testing.T) {
	f := newResolutionFixture(t)
	jobID, disputeID := f.disputedJob(t, "unwritable")

	unwritable := &DisputeWorkflow{
		jobs:    testJobs{svc: jobs.NewService(events.NewOutbox(), f.clk, nil)},
		auditor: nil,
		clock:   f.clk,
		pool:    f.pool,
	}

	_, err := unwritable.Resolve(t.Context(), ResolutionCommand{
		DisputeID:  disputeID,
		ActorID:    f.moderator.ID,
		Outcome:    OutcomeDeliveryFailed,
		JobOutcome: JobOutcomeCancelled,
		Reason:     "Goods never arrived and the provider is unreachable.",
	})
	if err == nil {
		t.Fatal("a dispute was resolved with no audit entry behind it.\n" +
			"SHIP-150: the entry commits with the action or neither happens.")
	}

	// The job did not move. This is the assertion the whole test exists for: an
	// implementation that transitioned the job outside the transaction leaves it here.
	if s := statusOf(t, f.pool, jobID); s != string(jobs.StatusDisputed) {
		t.Errorf("the job is %q, want Disputed.\n"+
			"The resolution failed and the job moved anyway, which is the non-atomic "+
			"resolve service.go warned about: a job unfrozen by a resolution that was "+
			"rolled back, with an open dispute still against it.", s)
	}

	// And the dispute is still open. The mirror image, and the half a reader would assume
	// from the error alone.
	row := disputeRow(t, f.pool, disputeID)
	if row.ResolvedAt != nil {
		t.Errorf("resolved_at = %s on a resolution that failed; the dispute is off the open "+
			"queue with nothing documented about it", row.ResolvedAt)
	}

	// Nothing in the trail either. The `job_status_history` row is the one a customer's
	// support conversation reads, and a transition that survived a rolled-back resolution
	// would tell them the job was cancelled by an administrator nobody can name.
	var transitions int
	if err := f.pool.QueryRow(t.Context(),
		`SELECT count(*) FROM job_status_history WHERE job_id = $1 AND to_status = 'Cancelled'`,
		jobID).Scan(&transitions); err != nil {
		t.Fatalf("reading the transitions: %v", err)
	}
	if transitions != 0 {
		t.Errorf("%d cancellation rows survived an unwritable audit trail", transitions)
	}
}

// TestASecondResolutionIsRefused is the two-moderators-one-queue case.
//
// Refused rather than absorbed, on [ErrVerificationUnchanged]'s reasoning: a resolution is an act by
// a person that Docs/04 §6 step 6 requires be recorded with a reason, and a second one would
// overwrite the first administrator's finding with nothing to say which stands.
//
// It also asserts the **first** outcome survives, which is the half that distinguishes a refusal
// from a refusal that has already done the damage.
func TestASecondResolutionIsRefused(t *testing.T) {
	f := newResolutionFixture(t)
	_, disputeID := f.disputedJob(t, "twice")

	if code, body := f.resolve(t, f.modToken, disputeID,
		`{"outcome":"`+OutcomeDeliveryCompleted.Wire()+`","job_outcome":"completed",`+
			`"reason":"Proof of delivery is complete and the recipient signed."}`); code != http.StatusOK {
		t.Fatalf("the first resolution: status = %d, want 200 (%s)", code, body)
	}

	code, body := f.resolve(t, f.modToken, disputeID,
		`{"outcome":"`+OutcomeDeliveryFailed.Wire()+`","job_outcome":"cancelled",`+
			`"reason":"Changed my mind about what happened on this delivery."}`)
	if code != http.StatusConflict {
		t.Fatalf("the second resolution: status = %d, want 409 (%s)", code, body)
	}
	if !strings.Contains(body, "admin_dispute_already_resolved") {
		t.Errorf("the refusal does not carry admin_dispute_already_resolved: %s", body)
	}

	row := disputeRow(t, f.pool, disputeID)
	if row.Outcome == nil || Outcome(*row.Outcome) != OutcomeDeliveryCompleted {
		t.Errorf("outcome = %v, want the first administrator's %q — a refused second "+
			"resolution overwrote the finding it refused", row.Outcome, OutcomeDeliveryCompleted)
	}
}

// TestAJobThatHasMovedOnCannotBeResolved is the other half of the drift.
//
// [TestASecondResolutionIsRefused] is the dispute moving under the job; this is the job moving under
// the dispute — the state `service.go`'s `JobAlreadyDisputed` branch admits, where a job carries an
// open dispute and is not `Disputed`. An administrator meeting it needs to be told plainly rather
// than handed a 500, because the answer is "reload" and not "try again".
//
// The fixture reaches it the way the platform can: the dispute is resolved once, which unfreezes the
// job, and a *second* dispute is then raised — which `uq_disputes_open_per_job` permits, because the
// first is no longer open — while the job is Completed and therefore not disputable. Rather than
// contrive that, this drives the simpler and equally real path: resolve the job to `Completed`
// through the ordinary route, then attempt to resolve the same dispute's successor.
func TestAJobThatHasMovedOnCannotBeResolved(t *testing.T) {
	f := newResolutionFixture(t)
	jobID, disputeID := f.disputedJob(t, "moved")

	// The job leaves Disputed by a route other than this dispute's resolution, which is what a
	// stale console is looking at when it tries to resolve.
	moveJobBecause(t, f.pool, jobID, jobs.User(jobs.ActorAdmin, f.moderator.ID),
		"Cancelled under a different dispute on the same delivery.", jobs.StatusCancelled)

	code, body := f.resolve(t, f.modToken, disputeID,
		`{"outcome":"`+OutcomeDeliveryFailed.Wire()+`","job_outcome":"cancelled",`+
			`"reason":"Recording the failed delivery against this complaint."}`)
	if code != http.StatusConflict {
		t.Fatalf("resolving a job that has moved on: status = %d, want 409 (%s)", code, body)
	}
	if !strings.Contains(body, "admin_job_not_resolvable") {
		t.Errorf("the refusal does not carry admin_job_not_resolvable: %s", body)
	}

	// And the dispute is untouched, which is the point of the refusal being inside the
	// transaction: an outcome recorded against a job that never moved is the corrupt state
	// this endpoint exists not to produce.
	if row := disputeRow(t, f.pool, disputeID); row.ResolvedAt != nil {
		t.Errorf("the dispute was resolved anyway, against a job that had already moved")
	}
}

// TestSupportMayInvestigateAndMayNotResolve is Docs/04 §9's least privilege, both directions.
//
// `disputes.read` is held by every role and `disputes.resolve` by `moderator` and `owner`. The two
// assertions are independent: a handler checking the wrong permission on the read fails the first, a
// handler checking none on the resolution fails the second, and either alone would leave the other
// looking correct.
//
// It also asserts a **refused** resolution writes nothing, which is what makes a 403 a refusal
// rather than a slower success.
func TestSupportMayInvestigateAndMayNotResolve(t *testing.T) {
	f := newResolutionFixture(t)
	jobID, disputeID := f.disputedJob(t, "privilege")

	if code, body := f.open(t, f.supToken, disputeID); code != http.StatusOK {
		t.Errorf("a support administrator could not read a dispute: %d (%s).\n"+
			"Docs/01 §4.6 puts looking first, and `disputes.read` is held by every role.",
			code, body)
	}
	if code, body := f.queue(t, f.supToken, ""); code != http.StatusOK {
		t.Errorf("a support administrator could not read the queue: %d (%s)", code, body)
	}

	code, body := f.resolve(t, f.supToken, disputeID,
		`{"outcome":"`+OutcomeDeliveryCompleted.Wire()+`","job_outcome":"completed",`+
			`"reason":"Looks fine to me after reading the proof of delivery."}`)
	if code != http.StatusForbidden {
		t.Fatalf("a support administrator resolved a dispute: %d, want 403 (%s)", code, body)
	}

	if row := disputeRow(t, f.pool, disputeID); row.ResolvedAt != nil {
		t.Error("a refused resolution settled the dispute anyway")
	}
	if s := statusOf(t, f.pool, jobID); s != string(jobs.StatusDisputed) {
		t.Errorf("a refused resolution unfroze the job anyway; it is %q", s)
	}
}

// TestAResolutionIsRefusedOnAVocabularyTheDocumentsDoNotHave.
//
// Both fields, and both named in the refusal — Docs/10 §4.6 asks that a client be told which field
// is wrong, and this endpoint is the one place in the service where a caller sends two closed
// vocabularies at once. A refusal naming neither would leave a console guessing which half it got
// wrong.
//
// The reason is the third: it is required, it has a floor, and `Docs/04` §6 step 6 is what makes
// that floor mean something.
func TestAResolutionIsRefusedOnAVocabularyTheDocumentsDoNotHave(t *testing.T) {
	f := newResolutionFixture(t)
	_, disputeID := f.disputedJob(t, "vocab")

	for _, tc := range []struct {
		name  string
		body  string
		field string
	}{
		{
			"an outcome Docs/04 §7 does not list",
			`{"outcome":"compensation_awarded","job_outcome":"completed",` +
				`"reason":"Awarding the customer a refund for the damage."}`,
			"outcome",
		},
		{
			// The name of a job status rather than one of the two destinations. The exact
			// mistake a console makes if somebody reads `job_outcome` as a status field,
			// which is why the key is not called `status`.
			"a destination Docs/02 §2 does not offer",
			`{"outcome":"` + OutcomeDeliveryFailed.Wire() + `","job_outcome":"Disputed",` +
				`"reason":"Leaving this one where it is for now."}`,
			"job_outcome",
		},
		{
			"a reason too short to record anything",
			`{"outcome":"` + OutcomeDeliveryCompleted.Wire() + `","job_outcome":"completed",` +
				`"reason":"fine"}`,
			"reason",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, body := f.resolve(t, f.modToken, disputeID, tc.body)
			if code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (%s)", code, body)
			}
			if !strings.Contains(body, `"`+tc.field+`"`) {
				t.Errorf("the refusal does not name %q: %s", tc.field, body)
			}
			if row := disputeRow(t, f.pool, disputeID); row.ResolvedAt != nil {
				t.Error("a refused resolution settled the dispute anyway")
			}
		})
	}
}

// TestTheQueueServesBothHalvesAndAResolvedDisputeLeavesTheOpenOne.
//
// The queue is Docs/04 §5's sixth, and the assertion that matters is the **move between halves**: a
// resolved dispute must leave the open queue and appear on the resolved one. A queue that answered
// from `disputes` with no predicate would pass a test that only checked the entry was present.
//
// The open half is ordered oldest first, which Docs/04 §8's targets are the reason for. It is
// asserted by raising two disputes on two jobs and backdating the second's `created_at` — backdated
// in the **reverse** of the order they were raised, so a queue reading the insertion sequence rather
// than the clock fails here rather than passing by accident.
func TestTheQueueServesBothHalvesAndAResolvedDisputeLeavesTheOpenOne(t *testing.T) {
	f := newResolutionFixture(t)
	_, firstDispute := f.disputedJob(t, "queue-a")
	_, secondDispute := f.disputedJob(t, "queue-b")

	// The second raised is the older complaint. See the note above.
	if _, err := f.pool.Exec(t.Context(),
		`UPDATE disputes SET created_at = timestamptz '1990-01-01 00:00:00Z' WHERE id = $1`,
		secondDispute); err != nil {
		t.Fatalf("backdating a dispute: %v", err)
	}

	code, body := f.queue(t, f.modToken, "?state=open&limit=50")
	if code != http.StatusOK {
		t.Fatalf("the open queue: status = %d, want 200 (%s)", code, body)
	}
	ids := queueIDs(t, body)
	if len(ids) < 2 || ids[0] != secondDispute.String() {
		t.Fatalf("the open queue is %v; the longest-waiting complaint is %s and it is not "+
			"first. Docs/04 §8 sets an acknowledgement target, so the oldest entry is the "+
			"one closest to breaching it.", ids, secondDispute)
	}

	if code, body := f.resolve(t, f.modToken, firstDispute,
		`{"outcome":"`+OutcomeDeliveryCompleted.Wire()+`","job_outcome":"completed",`+
			`"reason":"Proof of delivery is complete and the recipient signed."}`); code != http.StatusOK {
		t.Fatalf("resolving: status = %d, want 200 (%s)", code, body)
	}

	_, body = f.queue(t, f.modToken, "?state=open&limit=50")
	if carriesID(queueIDs(t, body), firstDispute.String()) {
		t.Error("a resolved dispute is still on the open queue.\n" +
			"idx_disputes_open is partial on `resolved_at IS NULL`; a queue reading the " +
			"whole table would look correct until the first resolution.")
	}

	_, body = f.queue(t, f.modToken, "?state=resolved&limit=50")
	resolved := queueIDs(t, body)
	if !carriesID(resolved, firstDispute.String()) {
		t.Errorf("the resolved queue %v does not carry %s", resolved, firstDispute)
	}
	if carriesID(resolved, secondDispute.String()) {
		t.Error("an open dispute is on the resolved queue")
	}
}

// TestTheQueueEntryCarriesNothingCommercial holds the shape to a closed key set.
//
// SHIP-83's axis: a field named `max_price` passes a search for the word "budget" and leaks the same
// fact, so the guard is the key set rather than a spelling. Docs/01 §4.3's invariant names providers
// as the audience it protects, and an administrator is not a provider — but the safest
// administrative shape is one that never carried a commercial fact at all, which is the position
// [VerificationEntry] and [CancellationEntry] both record.
//
// It also pins the **absence of the account**: the description, desired outcome and evidence belong
// to the detail endpoint, and a queue that grew them would be a page of four thousand characters a
// row.
func TestTheQueueEntryCarriesNothingCommercial(t *testing.T) {
	f := newResolutionFixture(t)
	_, disputeID := f.disputedJob(t, "shape")

	_, body := f.queue(t, f.modToken, "?limit=50")

	var page struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decoding the page: %v", err)
	}

	var entry map[string]any
	for _, e := range page.Data {
		if e["id"] == disputeID.String() {
			entry = e
		}
	}
	if entry == nil {
		t.Fatalf("the queue does not carry %s: %s", disputeID, body)
	}

	allowed := map[string]bool{
		"id": true, "job_id": true, "complainant_id": true, "complainant_party": true,
		"category": true, "occurred_at": true, "raised_at": true,
		"resolved_at": true, "outcome": true, "resolved_by": true,
	}
	for key := range entry {
		if !allowed[key] {
			t.Errorf("the queue entry carries %q, which is not in the closed set.\n"+
				"A budget arrives on an administrative shape as a field somebody added, and "+
				"a spelling-based guard misses one named anything at all (SHIP-83).", key)
		}
	}
	for key := range allowed {
		if _, ok := entry[key]; !ok {
			t.Errorf("the queue entry is missing %q; a console rendering without checking "+
				"crashes on the ordinary case", key)
		}
	}
}

// TestTheDetailEndpointCarriesTheComplaintAndNotTheIdempotencyKey.
//
// Docs/04 §7's investigation read, and the two things it must and must not carry. The account, the
// desired outcome and the evidence are what an administrator reads; the idempotency key is the
// complainant's own value coming back at it, and `dispute.go` records why it is on no wire shape.
func TestTheDetailEndpointCarriesTheComplaintAndNotTheIdempotencyKey(t *testing.T) {
	f := newResolutionFixture(t)
	_, disputeID := f.disputedJob(t, "detail")

	code, body := f.open(t, f.modToken, disputeID)
	if code != http.StatusOK {
		t.Fatalf("opening the dispute: status = %d, want 200 (%s)", code, body)
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(body), &entry); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	for _, want := range []string{"description", "desired_outcome", "evidence"} {
		if _, ok := entry[want]; !ok {
			t.Errorf("the dispute does not carry %q; Docs/04 §7's investigation stage is "+
				"reading what was reported, and nothing else in the console has it", want)
		}
	}
	if entry["description"] != "Two of the four crates arrived with the sides staved in." {
		t.Errorf("description = %v, want the complainant's own words", entry["description"])
	}
	for _, forbidden := range []string{"idempotency_key", "key"} {
		if _, ok := entry[forbidden]; ok {
			t.Errorf("the dispute carries %q; it is the client's own value coming back at it, "+
				"and a response carrying it invites a reader to treat it as an identifier the "+
				"platform issued", forbidden)
		}
	}

	// The evidence is a list rather than null, even though this fixture supplies one item —
	// the assertion is about the type, which is what a console iterates without checking.
	if _, ok := entry["evidence"].([]any); !ok {
		t.Errorf("evidence is %T, want an array", entry["evidence"])
	}
}

// TestADisputeThatDoesNotExistIsFourOhFourRatherThanFiveHundred.
//
// Disclosed plainly, unlike intake's refusal of a stranger, and errors.go records the difference:
// the caller holds `disputes.read` on the administrator credential and there is nothing being kept
// from them, so a 404 that could not be told from a 403 would only make a mistyped identifier look
// like a permissions problem.
func TestADisputeThatDoesNotExistIsFourOhFourRatherThanFiveHundred(t *testing.T) {
	f := newResolutionFixture(t)
	missing := uuid.Must(uuid.NewV7())

	if code, body := f.open(t, f.modToken, missing); code != http.StatusNotFound {
		t.Errorf("reading a dispute that does not exist: %d, want 404 (%s)", code, body)
	}
	if code, body := f.resolve(t, f.modToken, missing,
		`{"outcome":"`+OutcomeDeliveryCompleted.Wire()+`","job_outcome":"completed",`+
			`"reason":"Resolving a dispute that does not exist."}`); code != http.StatusNotFound {
		t.Errorf("resolving a dispute that does not exist: %d, want 404 (%s)", code, body)
	}
}

// TestTheQueueRefusesAStateThatIsNotOneOfTheTwo, and defaults to the document's.
//
// The default is the interesting half and it is the opposite of the verification queue's rule.
// Docs/04 §5's sixth queue is named "Open disputes", so an omitted `state` has a document-given
// answer and a console that forgets the parameter gets the queue rather than an empty page. A state
// that is neither is still refused, because an ignored filter answers something that looks like an
// answer.
func TestTheQueueRefusesAStateThatIsNotOneOfTheTwo(t *testing.T) {
	f := newResolutionFixture(t)
	_, disputeID := f.disputedJob(t, "state")

	code, body := f.queue(t, f.modToken, "?limit=50")
	if code != http.StatusOK {
		t.Fatalf("the queue with no state: %d, want 200 (%s)", code, body)
	}
	if !carriesID(queueIDs(t, body), disputeID.String()) {
		t.Error("the queue with no state does not carry the open dispute; Docs/04 §5's sixth " +
			"queue is 'open disputes' and that is what an omitted filter must mean")
	}

	code, body = f.queue(t, f.modToken, "?state=pending")
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("a state that is not one of the two: %d, want 422 (%s)", code, body)
	}
	if !strings.Contains(body, `"state"`) {
		t.Errorf("the refusal does not name the field: %s", body)
	}
}

// TestOutcomeWireFormsAreLegalAndRoundTrip.
//
// The wire mapping is derived rather than tabulated ([Outcome.Wire]), which is what stops a second
// list drifting from the first — and it is also what decided every shortening of Docs/04 §7's
// sentences. So the property worth pinning is the one the shortenings exist to satisfy: **every wire
// form is a legal identifier in the three client languages**, which means no punctuation at all.
//
// A `Category` that kept "Customer unavailable at pickup/delivery" would fail the same check, and
// `dispute.go` records that it was shortened for exactly this reason. This test is that argument
// applied to the outcome list, so a future outcome carrying a comma fails here rather than in a
// generated Dart enum.
func TestOutcomeWireFormsAreLegalAndRoundTrip(t *testing.T) {
	if len(Outcomes) != 5 {
		t.Errorf("Outcomes holds %d values; Docs/04 §7 lists five", len(Outcomes))
	}

	seen := map[string]bool{}
	for _, o := range Outcomes {
		wire := o.Wire()

		for _, r := range wire {
			if !(r >= 'a' && r <= 'z') && r != '_' {
				t.Errorf("%q derives the wire form %q, which contains %q — not a legal "+
					"identifier in Dart, TypeScript or Go. Shorten the stored form, as "+
					"ck_disputes_category's list was shortened.", o, wire, r)
				break
			}
		}

		if seen[wire] {
			t.Errorf("two outcomes derive the wire form %q; the mapping is not reversible", wire)
		}
		seen[wire] = true

		back, ok := OutcomeFromWire(wire)
		if !ok || back != o {
			t.Errorf("OutcomeFromWire(%q) = %q, %v; want %q, true", wire, back, ok, o)
		}
	}

	if _, ok := OutcomeFromWire("Delivery completed as agreed"); ok {
		t.Error("the stored form was accepted as a wire form; the mapping is case-sensitive " +
			"on purpose, and a client sending it has misread the contract")
	}
}

// queueIDs is the dispute identifiers on a page, in order.
func queueIDs(t *testing.T, body string) []string {
	t.Helper()

	var page struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decoding the page: %v (%s)", err, body)
	}

	out := make([]string, 0, len(page.Data))
	for _, e := range page.Data {
		out = append(out, e.ID)
	}
	return out
}

// carriesID reports whether a page's identifiers include this one.
//
// Named rather than reusing auditsearch_test.go's `contains`, which takes uuid.UUID: these
// assertions are about the identifiers **as the page rendered them**, and comparing parsed values
// would pass a page that answered with a differently formatted string.
func carriesID(page []string, id string) bool {
	for _, s := range page {
		if s == id {
			return true
		}
	}
	return false
}
