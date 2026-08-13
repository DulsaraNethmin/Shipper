package delivery

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-116 — a reasoned exception recorded in place of a photograph.
//
// # What these tests are asserting that SHIP-115's do not
//
// The photograph path and the exception path share [Service.recordEvidence], one insert and one
// table, so the tests below deliberately do **not** re-prove what proof_test.go already proves about
// absorption, retries and access control — those run through the same lines and would fail there
// first. What is new is the fork: which of the two a request may carry, what the platform asks the
// object store when it carries the second (nothing), and what a reader is handed for a row that has
// no object behind it (no URL, and no signer call to make one).
//
// # The store is stubbed, and here that is the assertion rather than a convenience
//
// An exception must not reach the object store at all. That is a claim about a *call* — there is no
// answer that would distinguish it — so [recordingObjects.lookups] is what the tests read.

// theExceptionKey is one idempotency key for the exception tests, distinct from theKey so that a
// test recording both a photograph and an exception on one job does not collide with itself.
const theExceptionKey = "b7d0a3f5-2e18-4c66-9a1d-77c4e0b93f21"

// recordWithException records a milestone whose evidence is a reason rather than a photograph.
//
// Deliberately shaped like [recordWithProof] and one call shorter, which is the whole difference
// between the two paths: there is nothing to verify against the store, so nothing is asked of it.
func recordWithException(
	t *testing.T,
	pool *pgxpool.Pool,
	svc *Service,
	provider, jobID uuid.UUID,
	rec Recording,
	reason ProofExceptionReason,
) (Record, Outcome, error) {
	t.Helper()

	rec.Exception = reason
	return recordMilestone(t, pool, svc, provider, jobID, rec)
}

// storedEvidence is the one proofs row on a job, read from the table rather than from the service.
//
// The four photograph columns come back as pointers because a reasoned exception holds none of them,
// and "NULL" is the assertion in half these tests — a test reading them into strings could not tell
// an absent object key from an empty one.
func storedEvidence(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID) (
	objectKey, contentType, etag, reason *string, contentLength *int64,
) {
	t.Helper()

	if err := pool.QueryRow(t.Context(), `
		SELECT object_key, content_type, etag, exception_reason, content_length
		  FROM proofs WHERE job_id = $1`, jobID).
		Scan(&objectKey, &contentType, &etag, &reason, &contentLength); err != nil {
		t.Fatalf("reading the evidence on %s: %v", jobID, err)
	}
	return objectKey, contentType, etag, reason, contentLength
}

// --- the domain ----------------------------------------------------------------------------------

// TestAReasonedExceptionIsRecordedInPlaceOfAPhotograph is SHIP-116's *Done when*, in the database.
//
// One request, one milestone, one `proofs` row — and the row holds a reason and no object. That the
// object columns are NULL rather than empty is the half worth reading twice: 000604 counts NULLs, so
// a row that stored `''` would satisfy the CHECK by looking like a photograph.
func TestAReasonedExceptionIsRecordedInPlaceOfAPhotograph(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "exc-record-c@example.com", "+61400000800", "customer")
	provider := newAccount(t, pool, "exc-record-p@example.com", "+61400000801", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	objects := newRecordingObjects()
	svc := serviceReadingProofFrom(objects)

	record, outcome, err := recordWithException(t, pool, svc, provider, jobID,
		enRoute(theExceptionKey), ExceptionRecipientObjected)
	if err != nil {
		t.Fatalf("recording a milestone with a reasoned exception: %v", err)
	}
	if outcome != OutcomeRecorded {
		t.Fatalf("outcome = %s, want recorded", outcome)
	}

	if n := proofCount(t, pool, jobID); n != 1 {
		t.Fatalf("%d evidence rows, want 1", n)
	}

	objectKey, contentType, etag, reason, contentLength := storedEvidence(t, pool, jobID)

	switch {
	case reason == nil:
		t.Fatal("the row holds no exception_reason; nothing records why there is no photograph")
	case *reason != string(ExceptionRecipientObjected):
		t.Errorf("exception_reason = %q, want %q", *reason, ExceptionRecipientObjected)
	}

	if objectKey != nil || contentType != nil || etag != nil || contentLength != nil {
		t.Error("an exception row carries photograph columns; 000604 counts NULLs, so a row that " +
			"stores empty strings satisfies the CHECK by pretending to be a photograph")
	}

	// The milestone it stands behind, because evidence with no claim is nothing.
	var milestoneID uuid.UUID
	if err := pool.QueryRow(t.Context(),
		`SELECT milestone_id FROM proofs WHERE job_id = $1`, jobID).Scan(&milestoneID); err != nil {
		t.Fatalf("reading the milestone the exception belongs to: %v", err)
	}
	if milestoneID != record.ID {
		t.Errorf("the exception names milestone %s, want the one just recorded, %s",
			milestoneID, record.ID)
	}

	// The whole of what makes this path different from the photograph's.
	if len(objects.lookups) != 0 {
		t.Errorf("the object store was asked about %v; there is no object, and asking would make "+
			"a delivery that could not be photographed depend on the store being reachable",
			objects.lookups)
	}
}

// TestEveryReasonDocs01ListsCanBeRecorded walks the three, so a constant Go gains and SQL does not
// fails here as well as in the migrations pairing.
//
// Three jobs rather than three milestones on one, because uq_proofs_milestone permits one piece of
// evidence per claim and the point is the vocabulary rather than the index.
func TestEveryReasonDocs01ListsCanBeRecorded(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "exc-all-c@example.com", "+61400000802", "customer")
	provider := newAccount(t, pool, "exc-all-p@example.com", "+61400000803", "provider")
	svc := serviceReadingProofFrom(newRecordingObjects())

	for i, reason := range ProofExceptionReasons {
		jobID := awardedJob(t, pool, customer, provider)

		if _, outcome, err := recordWithException(t, pool, svc, provider, jobID,
			enRoute(theExceptionKey+string(rune('a'+i))), reason); err != nil || outcome != OutcomeRecorded {
			t.Fatalf("recording %q: %v (outcome = %s)", reason, err, outcome)
		}

		_, _, _, stored, _ := storedEvidence(t, pool, jobID)
		if stored == nil || *stored != string(reason) {
			t.Errorf("%q was accepted and the row does not hold it", reason)
		}
	}
}

// TestAPhotographAndAReasonTogetherAreRefused.
//
// Docs/01 §4.4's exception is "in place of" the photograph. A recording carrying both says two
// things about one claim, and the second of them is that there was nothing to photograph — which
// the first disproves.
func TestAPhotographAndAReasonTogetherAreRefused(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "exc-both-c@example.com", "+61400000804", "customer")
	provider := newAccount(t, pool, "exc-both-p@example.com", "+61400000805", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	objects := newRecordingObjects()
	svc := serviceReadingProofFrom(objects)

	key, err := proofObjectKey(jobID)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}
	objects.holding(key, aPhotograph())

	proof, err := svc.VerifyProof(t.Context(), pool, provider, jobID, key)
	if err != nil {
		t.Fatalf("verifying the photograph: %v", err)
	}

	rec := enRoute(theExceptionKey)
	rec.Proof = proof
	rec.Exception = ExceptionCameraUnavailable

	if _, _, err := recordMilestone(t, pool, svc, provider, jobID, rec); err == nil {
		t.Fatal("a milestone was recorded with a photograph and a reason there is none")
	}

	if n := milestoneCount(t, pool, jobID); n != 0 {
		t.Errorf("%d milestone rows survived a refused recording", n)
	}
	if n := proofCount(t, pool, jobID); n != 0 {
		t.Errorf("%d evidence rows survived a refused recording", n)
	}
}

// TestAnUnknownReasonIsRefusedAndNamesTheThree.
//
// Docs/01 §4.4 has the driver *select* a reason, so the refusal is where a client learns which three
// there are — the same call the accepted media types make. There is deliberately no free-text
// fallback: a reason nobody can group is a moderation queue nobody can triage (Docs/04 §5).
func TestAnUnknownReasonIsRefusedAndNamesTheThree(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "exc-bad-c@example.com", "+61400000806", "customer")
	provider := newAccount(t, pool, "exc-bad-p@example.com", "+61400000807", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	_, _, err := recordWithException(t, pool, serviceReadingProofFrom(newRecordingObjects()),
		provider, jobID, enRoute(theExceptionKey), ProofExceptionReason("could not be bothered"))
	if err == nil {
		t.Fatal("a reason nobody published was recorded")
	}

	// Read out of the field details rather than off err.Error(), which is the envelope's own
	// message: what a client acts on is the per-field message, and that is where the three have
	// to appear.
	var apiErr *httpx.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("RecordMilestone() = %v, want a validation failure a client can read", err)
	}
	if len(apiErr.Details) == 0 || apiErr.Details[0].Field != "proof.exception_reason" {
		t.Fatalf("details = %+v, want proof.exception_reason named", apiErr.Details)
	}
	for _, want := range proofExceptionWire() {
		if !strings.Contains(apiErr.Details[0].Message, want) {
			t.Errorf("the refusal does not name %q: %s", want, apiErr.Details[0].Message)
		}
	}

	if n := milestoneCount(t, pool, jobID); n != 0 {
		t.Errorf("%d milestone rows survived an unknown reason", n)
	}
}

// TestEvidenceThatIsNotOneOrTheOtherIsRefusedInsideThePackage is the in-package guard, driven
// directly.
//
// [Recording.problems] refuses all three of these before the insert, so none is reachable through
// the endpoint. They are checked again in [Service.recordEvidence] for the reason
// [VerifiedProof.complete] is checked: this is what a *future caller inside this package* meets when
// they assemble a recording by hand, and the layer below it is a CHECK constraint whose name
// explains nothing (Docs/10 §4.6).
func TestEvidenceThatIsNotOneOrTheOtherIsRefusedInsideThePackage(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "exc-guard-c@example.com", "+61400000808", "customer")
	provider := newAccount(t, pool, "exc-guard-p@example.com", "+61400000809", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	svc := serviceReadingProofFrom(newRecordingObjects())

	// A milestone to hang the attempts off, recorded honestly.
	record, _, err := recordMilestone(t, pool, svc, provider, jobID, enRoute(theKey))
	if err != nil {
		t.Fatalf("recording the milestone these attempts point at: %v", err)
	}

	for _, tc := range []struct {
		name      string
		proof     VerifiedProof
		exception ProofExceptionReason
		want      error
	}{
		{
			name: "neither",
			want: ErrEvidenceNotCoherent,
		},
		{
			name:      "both",
			proof:     VerifiedProof{objectKey: "k", contentType: "image/jpeg", contentLength: 1, etag: "e"},
			exception: ExceptionLocationUnsafe,
			want:      ErrEvidenceNotCoherent,
		},
		{
			name:      "a reason nobody published",
			exception: ProofExceptionReason("because"),
			want:      ErrEvidenceNotCoherent,
		},
		{
			name:  "a photograph the store was never asked about",
			proof: VerifiedProof{objectKey: "k"},
			want:  ErrProofNotVerified,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.recordEvidence(t.Context(), pool, jobID, record.ID, tc.proof, tc.exception)
			if !errors.Is(err, tc.want) {
				t.Fatalf("recordEvidence(%s) = %v, want %v", tc.name, err, tc.want)
			}
		})
	}

	if n := proofCount(t, pool, jobID); n != 0 {
		t.Errorf("%d evidence rows were written by attempts that were all refused", n)
	}
}

// TestAnExceptionIsReadBackWithNoDownloadURLAndNothingIsSignedForIt.
//
// **The second assertion is the one that matters and it is written against the signer rather than
// the response.** internal/platform/storage will sign a URL for any key it is handed and says so, so
// a service that asked it about an exception would get a perfectly good credential naming an object
// that does not exist — and a response assertion could pass while that happened, because the URL
// would then be dropped on the floor. Counting what the signer was asked is the only shape that
// fails.
func TestAnExceptionIsReadBackWithNoDownloadURLAndNothingIsSignedForIt(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "exc-read-c@example.com", "+61400000810", "customer")
	provider := newAccount(t, pool, "exc-read-p@example.com", "+61400000811", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	objects := newRecordingObjects()
	svc := serviceReadingProofFrom(objects)

	if _, _, err := recordWithException(t, pool, svc, provider, jobID,
		enRoute(theExceptionKey), ExceptionLocationUnsafe); err != nil {
		t.Fatalf("recording the exception: %v", err)
	}

	for _, reader := range []struct {
		name string
		id   uuid.UUID
	}{
		{"the job's customer", customer},
		{"the awarded provider", provider},
	} {
		links, err := svc.ProofFor(t.Context(), pool, reader.id, jobID)
		if err != nil {
			t.Fatalf("%s reading the evidence: %v", reader.name, err)
		}
		if len(links) != 1 {
			t.Fatalf("%s got %d records, want 1", reader.name, len(links))
		}

		got := links[0]
		switch {
		case !got.IsException():
			t.Errorf("%s was handed a record that does not read as an exception", reader.name)
		case got.ExceptionReason != ExceptionLocationUnsafe:
			t.Errorf("%s sees the reason %q, want %q", reader.name, got.ExceptionReason,
				ExceptionLocationUnsafe)
		case got.URL != "":
			t.Errorf("%s was handed the URL %q for a delivery with no photograph", reader.name, got.URL)
		case !got.ExpiresAt.IsZero():
			t.Errorf("%s was handed an expiry for a URL that does not exist", reader.name)
		}
	}

	if len(objects.downloads) != 0 {
		t.Errorf("the signer was asked for %v; a URL minted for an object that does not exist is "+
			"still a credential that left the building", objects.downloads)
	}
}

// --- the wire ------------------------------------------------------------------------------------

// TestAnExceptionIsRecordedAndReadBackOverHTTP is the *Done when* end to end through the handler:
// one request records a delivery that could not be photographed, and both parties can see why.
func TestAnExceptionIsRecordedAndReadBackOverHTTP(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "http-exc-c@example.com", "+61400000812", "customer")
	provider := newAccount(t, pool, "http-exc-p@example.com", "+61400000813", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	objects := newRecordingObjects()
	router := newTestRouterFor(t, pool, serviceReadingProofFrom(objects))

	rec := recordAs(t, router, provider, jobID, theExceptionKey,
		`{"milestone": "en_route_to_pickup", "reason": "the recipient asked me not to",
		  "proof": {"exception_reason": "recipient_objected"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("recording a milestone with an exception: %d (%s)", rec.Code, rec.Body)
	}

	// The store was not asked, at the wire as well as in the domain. This is the assertion that
	// fails if the handler ever starts verifying an object for a request that names none.
	if len(objects.lookups) != 0 {
		t.Errorf("the handler asked the store about %v for a request that carries no object",
			objects.lookups)
	}

	for _, reader := range []struct {
		name string
		id   uuid.UUID
	}{
		{"customer", customer},
		{"provider", provider},
	} {
		read := readingAs(t, router, reader.id, proofPath(jobID))
		if read.Code != http.StatusOK {
			t.Fatalf("the %s reading the evidence: %d (%s)", reader.name, read.Code, read.Body)
		}

		body := decode[proofListBody](t, read)
		if len(body.Data) != 1 {
			t.Fatalf("the %s got %d records, want 1", reader.name, len(body.Data))
		}

		got := body.Data[0]
		switch {
		case got.ExceptionReason != "recipient_objected":
			t.Errorf("the %s sees exception_reason %q", reader.name, got.ExceptionReason)
		case got.DownloadURL != "":
			t.Errorf("the %s was handed a download URL for a delivery with no photograph", reader.name)
		case got.ObjectKey != "":
			t.Errorf("the %s was handed an object key that does not exist", reader.name)
		}
	}
}

// TestTheExceptionResponseCarriesAClosedSetOfFields.
//
// Asserted as a whole set for SHIP-83's reason, and here it is doing a second job: it is what says a
// row with no photograph carries **no** `object_key`, `content_type`, `content_length`,
// `download_url` or `download_expires_at` rather than empty ones. A zero content length is a
// statement about a photograph that does not exist; an absent field says nothing.
func TestTheExceptionResponseCarriesAClosedSetOfFields(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "http-exc-keys-c@example.com", "+61400000814", "customer")
	provider := newAccount(t, pool, "http-exc-keys-p@example.com", "+61400000815", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	router := newTestRouterFor(t, pool, serviceReadingProofFrom(newRecordingObjects()))

	rec := recordAs(t, router, provider, jobID, theExceptionKey,
		`{"milestone": "en_route_to_pickup", "proof": {"exception_reason": "camera_unavailable"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("recording the exception: %d (%s)", rec.Code, rec.Body)
	}

	read := readingAs(t, router, customer, proofPath(jobID))

	envelope := decode[struct {
		Data []map[string]any `json:"data"`
	}](t, read)
	if len(envelope.Data) != 1 {
		t.Fatalf("%d records, want 1", len(envelope.Data))
	}

	got := make([]string, 0, len(envelope.Data[0]))
	for field := range envelope.Data[0] {
		got = append(got, field)
	}
	sort.Strings(got)

	want := []string{
		"accepted_at", "exception_reason", "id", "job_id", "milestone", "milestone_id",
		"recorded_at",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("an exception record carries\n  %s\nwant\n  %s",
			strings.Join(got, ","), strings.Join(want, ","))
	}
}

// TestTheEvidenceFieldRefusesBothAndNeitherAtTheWire.
//
// One object with two alternatives inside it, which is the shape SHIP-115 chose it for. A client
// that sends both has misunderstood what an exception is; one that sends `{}` meant to send
// something and lost it, and silently recording an unphotographed delivery for them is the one
// answer that must not happen.
func TestTheEvidenceFieldRefusesBothAndNeitherAtTheWire(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "http-exc-bad-c@example.com", "+61400000816", "customer")
	provider := newAccount(t, pool, "http-exc-bad-p@example.com", "+61400000817", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	router := newTestRouterFor(t, pool, serviceReadingProofFrom(newRecordingObjects()))

	for _, tc := range []struct {
		name  string
		body  string
		field string
	}{
		{
			name: "both a photograph and a reason",
			body: `{"milestone": "en_route_to_pickup",
			        "proof": {"object_key": "proof/x/y", "exception_reason": "recipient_objected"}}`,
			field: "proof.exception_reason",
		},
		{
			name:  "neither",
			body:  `{"milestone": "en_route_to_pickup", "proof": {}}`,
			field: "proof.object_key",
		},
		{
			name:  "a reason nobody published",
			body:  `{"milestone": "en_route_to_pickup", "proof": {"exception_reason": "raining"}}`,
			field: "proof.exception_reason",
		},
		{
			name:  "the right reason in the wrong case",
			body:  `{"milestone": "en_route_to_pickup", "proof": {"exception_reason": "Recipient_Objected"}}`,
			field: "proof.exception_reason",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := recordAs(t, router, provider, jobID, theExceptionKey+tc.name, tc.body)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("%s answered %d, want 422 (%s)", tc.name, rec.Code, rec.Body)
			}
			envelope := decode[errorEnvelope](t, rec)
			if len(envelope.Error.Details) == 0 || envelope.Error.Details[0].Field != tc.field {
				t.Errorf("details = %+v, want %s named", envelope.Error.Details, tc.field)
			}
		})
	}

	if n := milestoneCount(t, pool, jobID); n != 0 {
		t.Errorf("%d milestone rows survived four refused requests", n)
	}
}
