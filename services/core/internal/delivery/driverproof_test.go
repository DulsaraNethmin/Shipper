package delivery

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-122 — the driver's own upload, against a real PostgreSQL and a stubbed store.
//
// The fixtures come from assignment_test.go, driverauth_test.go, drivermilestone_test.go and
// proof_test.go; nothing here duplicates them.
//
// # What this ticket actually changes, and it is not "one more route"
//
// Before it, `internal/delivery/http.go` refused an `object_key` on the driver's milestone route by
// name, because **there was no route by which a driver could obtain one**. The consequence is worth
// restating: a driver could reach 'Delivered' only through a reasoned exception, which flags the job
// into SHIP-117's moderation queue — so for a driver-recorded delivery that queue was the *only*
// path rather than one of two. Every delivery a driver completed was, by construction, a delivery
// with no photograph.
//
// So the tests below are about two things: that a driver can now produce a photograph the platform
// has actually looked at, and that the two ways to get that wrong are shut — a key for another
// delivery, and a link whose assignment has ended.
//
// # Every one of them takes a grant and never a job identifier
//
// That is the signature the ticket rests on. [Service.PresignDriverProofUpload] and
// [Service.VerifyDriverProof] have no job argument, so a handler cannot widen the scope by passing
// the wrong one — there is nothing to pass. Wave 7 recorded a mutation that survived a full suite,
// where a driver surface derived the job it acted on from its own credential rather than from what
// was asked for, and a signature with nothing to get wrong in it is the shape that makes that
// unwritable rather than merely tested for.

// driverProofService is a service whose signer and store a test can both inspect.
//
// One constructor answering three values, because SHIP-122's whole path runs through all three: the
// signer says what was asked of it, the store is where the object has to be *put* before it can be
// verified, and the service is what does both. [serviceWithUploads] gives the first and
// [serviceReadingProofFrom] the second, and a test that used one of them would have to fake the
// other end of the round trip it is trying to demonstrate.
func driverProofService() (*Service, *recordingUploads, *recordingObjects) {
	uploads := &recordingUploads{}
	objects := newRecordingObjects()

	svc := NewService(events.NewOutbox(),
		testJobs{svc: jobs.NewService(events.NewOutbox(), testClock(), nil)}, testAwards{}, testJobOwners(),
		testDriverIssuer(testClock()), uploads, objects, testUploadPolicy(), testClock())
	return svc, uploads, objects
}

// presignAsDriver asks for an upload URL the way the handler does: on the pool, outside any
// transaction.
func presignAsDriver(
	t *testing.T,
	pool *pgxpool.Pool,
	svc *Service,
	grant DriverGrant,
	req UploadRequest,
) (Upload, error) {
	t.Helper()
	return svc.PresignDriverProofUpload(t.Context(), pool, grant, req)
}

// TestADriverIsGivenSomewhereToPutOnePhotograph is the first half of the *Done when*: a browser
// camera has somewhere to upload to.
//
// The key is asserted to name **the job inside the grant**, which is the property everything else in
// this file depends on: [keyBelongsToJob] refuses a key that names another job, so a presign that
// signed for the wrong one would produce a URL whose object could never be recorded as proof.
func TestADriverIsGivenSomewhereToPutOnePhotograph(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "drvup-c@example.com", "+61400000740", "customer")
	provider := newAccount(t, pool, "drvup-p@example.com", "+61400000741", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	_, grant, _ := driverOnJob(t, pool, provider, jobID)

	svc, uploads, _ := driverProofService()
	upload, err := presignAsDriver(t, pool, svc, grant,
		UploadRequest{ContentType: "image/jpeg", ContentLength: 137_402})
	if err != nil {
		t.Fatalf("a driver holding the job's link could not obtain an upload URL: %v", err)
	}

	if !keyBelongsToJob(upload.ObjectKey, jobID) {
		t.Fatalf("the key %q does not name %s, so the milestone route would refuse it", upload.ObjectKey, jobID)
	}
	if upload.URL == "" || upload.ExpiresAt.IsZero() {
		t.Fatalf("the upload is not a credential: %+v", upload)
	}

	// What the signer was asked for, rather than what came back: the content type and the length
	// are *signed*, and a presign that did not bind them would authorise any body at all.
	call := uploads.last(t)
	if call.contentType != "image/jpeg" || call.contentLength != 137_402 {
		t.Fatalf("the signer bound %q/%d, want image/jpeg/137402", call.contentType, call.contentLength)
	}
	if call.ttl != testUploadPolicy().UploadTTL {
		t.Fatalf("the URL was signed for %s, want the policy's upload lifetime %s",
			call.ttl, testUploadPolicy().UploadTTL)
	}
}

// TestEachDriverPresignMintsAFreshKey is SHIP-114's argument holding on the driver's route.
//
// A key derived from anything the client controls would let a holder of an old value ask for a fresh
// URL over an object that already holds proof, and proof is evidence. Two presigns, two keys.
func TestEachDriverPresignMintsAFreshKey(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "drvup2-c@example.com", "+61400000742", "customer")
	provider := newAccount(t, pool, "drvup2-p@example.com", "+61400000743", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	_, grant, _ := driverOnJob(t, pool, provider, jobID)

	svc, _, _ := driverProofService()
	req := UploadRequest{ContentType: "image/jpeg", ContentLength: 1000}

	first, err := presignAsDriver(t, pool, svc, grant, req)
	if err != nil {
		t.Fatalf("the first presign: %v", err)
	}
	second, err := presignAsDriver(t, pool, svc, grant, req)
	if err != nil {
		t.Fatalf("the second presign: %v", err)
	}

	if first.ObjectKey == second.ObjectKey {
		t.Fatalf("both presigns named %s; a second URL over an object that may already hold proof "+
			"is what SHIP-114 rejected a derived key to prevent", first.ObjectKey)
	}
}

// TestAStoodDownDriverIsGivenNothingToUploadTo is the refusal that matters most on this route.
//
// A driver token is stateless and cannot be recalled, so it keeps verifying for its full seven days
// after the assignment behind it has ended. The row is the only thing that knows — which is why the
// live-assignment read is in this path at all, and why the signer must not be reached before it.
//
// **The signer is asserted to have been asked nothing.** A refusal that came back after a URL had
// been minted would be a credential issued to somebody the platform had just decided may not have
// one, and nothing revokes a pre-signed URL.
func TestAStoodDownDriverIsGivenNothingToUploadTo(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "drvup3-c@example.com", "+61400000744", "customer")
	provider := newAccount(t, pool, "drvup3-p@example.com", "+61400000745", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	_, grant, _ := driverOnJob(t, pool, provider, jobID)

	svc, uploads, _ := driverProofService()

	// The link is reissued, which leaves the assignment live and changes which link opens it —
	// SHIP-109's revocation, which is a read against a column rather than a denylist.
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, _, err := svc.ReissueDriverLink(ctx, r, provider, jobID)
		return err
	}); err != nil {
		t.Fatalf("reissuing the link: %v", err)
	}

	_, err := presignAsDriver(t, pool, svc, grant,
		UploadRequest{ContentType: "image/jpeg", ContentLength: 1000})
	if !errors.Is(err, ErrDriverLinkSuperseded) {
		t.Fatalf("the superseded link got %v, want ErrDriverLinkSuperseded", err)
	}
	if len(uploads.calls) != 0 {
		t.Fatalf("a URL was signed for a link that no longer opens the delivery: %+v", uploads.calls)
	}
}

// TestADriverPresignIsRefusedBeforeAnythingIsSigned holds the ordering the policy depends on.
//
// Validation first, and the signer untouched: the limits are what the URL *binds*, so a request the
// platform would not accept must not produce one that the store then would.
func TestADriverPresignIsRefusedBeforeAnythingIsSigned(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "drvup4-c@example.com", "+61400000746", "customer")
	provider := newAccount(t, pool, "drvup4-p@example.com", "+61400000747", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	_, grant, _ := driverOnJob(t, pool, provider, jobID)

	for _, tc := range []struct {
		name  string
		req   UploadRequest
		field string
	}{
		{"a format the platform does not accept", UploadRequest{ContentType: "image/svg+xml", ContentLength: 100}, "content_type"},
		{"no format at all", UploadRequest{ContentLength: 100}, "content_type"},
		{"larger than the limit", UploadRequest{ContentType: "image/jpeg", ContentLength: 9_000_000}, "content_length"},
		{"no size", UploadRequest{ContentType: "image/jpeg"}, "content_length"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, uploads, _ := driverProofService()

			_, err := presignAsDriver(t, pool, svc, grant, tc.req)
			var apiErr *httpx.Error
			if !errors.As(err, &apiErr) || apiErr.Code != httpx.CodeValidationFailed {
				t.Fatalf("got %v, want a validation failure naming %s", err, tc.field)
			}
			if len(uploads.calls) != 0 {
				t.Fatalf("a URL was signed for a request the platform refused: %+v", uploads.calls)
			}
		})
	}
}

// TestADriverPhotographsADelivery is the ticket end to end, and the reason it exists.
//
// Presign, put the object in the store, record 'Delivered' against it. Before SHIP-122 the last step
// was refused by name and a driver's only route to 'Delivered' was a reasoned exception — which is
// to say, through the moderation queue.
func TestADriverPhotographsADelivery(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "drvup5-c@example.com", "+61400000748", "customer")
	provider := newAccount(t, pool, "drvup5-p@example.com", "+61400000749", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	assignment, grant, _ := driverOnJob(t, pool, provider, jobID)

	svc, _, objects := driverProofService()

	// The delivery gets as far as 'In transit' on the driver's own link, which is the sequence a
	// portal produces and the one Docs/02 §2 permits.
	for i, milestone := range []Milestone{MilestoneEnRouteToPickup, MilestonePickedUp, MilestoneInTransit} {
		if _, _, err := recordAsDriver(t, pool, svc, grant,
			Recording{Milestone: milestone, Key: "drvup5-" + string(rune('a'+i))}); err != nil {
			t.Fatalf("recording %s: %v", milestone, err)
		}
	}

	upload, err := presignAsDriver(t, pool, svc, grant,
		UploadRequest{ContentType: "image/jpeg", ContentLength: 137_402})
	if err != nil {
		t.Fatalf("obtaining an upload URL: %v", err)
	}

	// The bytes never touch this service, so the fixture is the store holding the object rather
	// than a request being made. That is exactly what VerifyDriverProof has to check, because the
	// platform cannot tell an upload that succeeded from one that was never attempted.
	objects.holding(upload.ObjectKey, aPhotograph())

	proof, err := svc.VerifyDriverProof(t.Context(), pool, grant, upload.ObjectKey)
	if err != nil {
		t.Fatalf("the platform would not accept the driver's photograph: %v", err)
	}

	record, outcome, err := recordAsDriver(t, pool, svc, grant,
		Recording{Milestone: MilestoneDelivered, Key: "drvup5-delivered", Proof: proof, RecipientName: "R. Chen", DeliveryNote: "Left with reception"})
	if err != nil {
		t.Fatalf("recording a photographed delivery: %v", err)
	}
	if outcome != OutcomeRecorded {
		t.Fatalf("the delivery was %s, want recorded", outcome)
	}

	// The evidence is a photograph and not an exception, which is the whole difference this ticket
	// makes to SHIP-117's queue: a driver-recorded delivery no longer has to enter it.
	var (
		storedKey       string
		exceptionReason *string
	)
	if err := pool.QueryRow(t.Context(),
		`SELECT object_key, exception_reason FROM proofs WHERE milestone_id = $1`, record.ID).
		Scan(&storedKey, &exceptionReason); err != nil {
		t.Fatalf("reading the proof row: %v", err)
	}
	if storedKey != upload.ObjectKey {
		t.Fatalf("the proof row names %q, want %q", storedKey, upload.ObjectKey)
	}
	if exceptionReason != nil {
		t.Fatalf("a photographed delivery recorded the exception %q", *exceptionReason)
	}

	// And it is attributed to the driver's assignment, in both tables, exactly as an unphotographed
	// one is — the evidence changed and the actor did not.
	if actorType, actorID := milestoneActor(t, pool, record.ID); actorType != string(ActorDriver) || actorID != assignment.ID {
		t.Fatalf("the milestone is attributed to %s/%s, want driver/%s", actorType, actorID, assignment.ID)
	}
}

// TestADriverCannotAttachAnotherDeliverysPhotograph is the refusal a driver carrying two jobs makes
// reachable.
//
// It is decided from the string alone with no lookup, so it discloses nothing but the caller's own
// body — and it is the reason [Service.VerifyDriverProof] compares against the grant rather than
// against anything a request supplied.
func TestADriverCannotAttachAnotherDeliverysPhotograph(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "drvup6-c@example.com", "+61400000750", "customer")
	provider := newAccount(t, pool, "drvup6-p@example.com", "+61400000751", "provider")

	mine := awardedJob(t, pool, customer, provider)
	theirs := awardedJob(t, pool, customer, provider)
	_, grantOnMine, _ := driverOnJob(t, pool, provider, mine)
	_, grantOnTheirs, _ := driverOnJob(t, pool, provider, theirs)

	svc, _, objects := driverProofService()

	other, err := presignAsDriver(t, pool, svc, grantOnTheirs,
		UploadRequest{ContentType: "image/jpeg", ContentLength: 1000})
	if err != nil {
		t.Fatalf("presigning on the other delivery: %v", err)
	}
	objects.holding(other.ObjectKey, aPhotograph())

	_, err = svc.VerifyDriverProof(t.Context(), pool, grantOnMine, other.ObjectKey)
	if !errors.Is(err, ErrProofNotForThisJob) {
		t.Fatalf("got %v, want ErrProofNotForThisJob", err)
	}
	if len(objects.lookups) != 0 {
		t.Fatalf("the store was asked about %v; a key naming another job is refused before anything "+
			"is looked up, so this endpoint cannot be used to probe whether an object exists",
			objects.lookups)
	}
}

// TestADriverCannotRecordAPhotographThatNeverArrived is the failure the platform cannot infer.
//
// The bytes go straight to the store, so a client saying "I uploaded it" is a claim the platform has
// never checked — and Docs/01 §4.4 makes proof "the only evidence that the job happened as claimed".
func TestADriverCannotRecordAPhotographThatNeverArrived(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "drvup7-c@example.com", "+61400000752", "customer")
	provider := newAccount(t, pool, "drvup7-p@example.com", "+61400000753", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	_, grant, _ := driverOnJob(t, pool, provider, jobID)

	svc, _, _ := driverProofService()
	upload, err := presignAsDriver(t, pool, svc, grant,
		UploadRequest{ContentType: "image/jpeg", ContentLength: 1000})
	if err != nil {
		t.Fatalf("presigning: %v", err)
	}

	// The store is deliberately not holding it: the URL was issued and never spent.
	_, err = svc.VerifyDriverProof(t.Context(), pool, grant, upload.ObjectKey)
	if !errors.Is(err, ErrProofNotUploaded) {
		t.Fatalf("got %v, want ErrProofNotUploaded", err)
	}
}

// TestAStoodDownDriverCannotRecordAPhotographEither closes the other half of the revocation.
//
// A link that has stopped minting URLs must also stop *spending* a key it minted earlier, or a
// stood-down driver holding one from before the reissue could still attach it.
func TestAStoodDownDriverCannotRecordAPhotographEither(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "drvup8-c@example.com", "+61400000754", "customer")
	provider := newAccount(t, pool, "drvup8-p@example.com", "+61400000755", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	_, grant, _ := driverOnJob(t, pool, provider, jobID)

	svc, _, objects := driverProofService()
	upload, err := presignAsDriver(t, pool, svc, grant,
		UploadRequest{ContentType: "image/jpeg", ContentLength: 1000})
	if err != nil {
		t.Fatalf("presigning: %v", err)
	}
	objects.holding(upload.ObjectKey, aPhotograph())

	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, _, err := svc.ReissueDriverLink(ctx, r, provider, jobID)
		return err
	}); err != nil {
		t.Fatalf("reissuing the link: %v", err)
	}

	_, err = svc.VerifyDriverProof(t.Context(), pool, grant, upload.ObjectKey)
	if !errors.Is(err, ErrDriverLinkSuperseded) {
		t.Fatalf("got %v, want ErrDriverLinkSuperseded", err)
	}
}

// --- over HTTP, through the real guard ----------------------------------------------------------

// driverProofRouter is SHIP-122's route behind the real auth class, over a real database.
//
// Both driver routes are mounted, because the ticket is the pair: a key obtained from one is spent
// on the other, and mounting only the presign would prove that a URL can be issued and nothing about
// whether it is usable.
func driverProofRouter(t *testing.T, pool *pgxpool.Pool, svc *Service) http.Handler {
	t.Helper()

	handler, err := NewHandler(svc, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	guard := RequireDriverToken(testDriverVerifier(t, clock.NewFixed(testDriverIssuedAt)))
	mux := http.NewServeMux()
	mux.Handle("POST /v1/driver/jobs/{id}/proof-uploads", guard(handler.PresignDriverProofUpload()))
	mux.Handle("POST /v1/driver/jobs/{id}/milestones", guard(handler.RecordDriverMilestone()))
	return mux
}

func postProofUploadAs(h http.Handler, jobID uuid.UUID, header, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost,
		"/v1/driver/jobs/"+jobID.String()+"/proof-uploads", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if header != "" {
		req.Header.Set("Authorization", header) // spelling:ok — HTTP header name, RFC 9110
	}
	if key != "" {
		req.Header.Set(httpx.HeaderIdempotencyKey, key)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestTheDriverUploadRouteGrantsExactlyOneJob is the *Done when*'s scope clause over HTTP.
//
// The link is presented on a delivery it does not open, and the answer is the one a missing job gets
// — not a 403, which would confirm that somebody else's delivery exists.
func TestTheDriverUploadRouteGrantsExactlyOneJob(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "drvup9-c@example.com", "+61400000756", "customer")
	provider := newAccount(t, pool, "drvup9-p@example.com", "+61400000757", "provider")

	mine := awardedJob(t, pool, customer, provider)
	theirs := awardedJob(t, pool, customer, provider)
	_, _, token := driverOnJob(t, pool, provider, mine)
	driverOnJob(t, pool, provider, theirs)

	svc, uploads, _ := driverProofService()
	router := driverProofRouter(t, pool, svc)
	body := `{"content_type":"image/jpeg","content_length":1000}`

	if rec := postProofUploadAs(router, mine, "Bearer "+token.Value, "k1", body); rec.Code != http.StatusOK {
		t.Fatalf("the driver's own delivery answered %d, want 200: %s", rec.Code, rec.Body)
	}

	rec := postProofUploadAs(router, theirs, "Bearer "+token.Value, "k2", body)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a link for %s minted a URL on %s: %d", mine, theirs, rec.Code)
	}
	if len(uploads.calls) != 1 {
		t.Fatalf("the signer was asked %d times, want 1 — the refused request reached it",
			len(uploads.calls))
	}
}

// TestTheTwoTokenSystemsStayApartOnTheUploadRoute is CLAUDE.md's invariant on the new route.
//
// A mobile access token is not a driver's link, in either direction. The guard is what refuses it,
// which is why this is asserted through the router rather than against the service.
func TestTheTwoTokenSystemsStayApartOnTheUploadRoute(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "drvupa-c@example.com", "+61400000758", "customer")
	provider := newAccount(t, pool, "drvupa-p@example.com", "+61400000759", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	driverOnJob(t, pool, provider, jobID)

	svc, uploads, _ := driverProofService()
	router := driverProofRouter(t, pool, svc)
	body := `{"content_type":"image/jpeg","content_length":1000}`

	for _, tc := range []struct{ name, header string }{
		{"no credential at all", ""},
		{"a mobile session token", "Bearer " + mobileLookingToken},
		{"a driver token signed with the wrong key", "Bearer not.a.token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := postProofUploadAs(router, jobID, tc.header, "k-"+tc.name, body)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("got %d, want 401: %s", rec.Code, rec.Body)
			}
		})
	}

	if len(uploads.calls) != 0 {
		t.Fatalf("a URL was signed for a caller the guard refused: %+v", uploads.calls)
	}
}

// mobileLookingToken is a well-formed JWS that is not a driver link.
//
// Three base64url segments, so it passes the shape test a portal makes and is refused by the
// signature check rather than by looking malformed — which is the case worth exercising, because a
// token that fails to parse proves nothing about the two keysets being separate.
const mobileLookingToken = "eyJhbGciOiJIUzI1NiIsImtpZCI6Im1vYmlsZSIsInR5cCI6IkpXVCJ9." +
	"eyJzdWIiOiIwMTk4ZjJjMS02YjQwLTdhMTEtOWMzZS0yZjlhNGQ1MWI3ZTEiLCJhdWQiOiJzaGlwcGVyLW1vYmlsZSJ9." +
	"bm90LWEtZHJpdmVyLXNpZ25hdHVyZQ"
