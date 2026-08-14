package delivery

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-108 — the driver token validation middleware.
//
// The *Done when* is two clauses and this file is written around them:
//
//	"grants access to exactly one job and nothing else"  → the job in the token is checked against
//	                                                       the job in the path, by the guard, so a
//	                                                       route cannot skip it
//	"cannot be exchanged for a user session"             → in both directions, and the direction
//	                                                       SHIP-107 could not reach is the one where
//	                                                       a mobile token meets a driver route
//
// # This file imports internal/identity, which non-test code in this package may not
//
// The same licence token_test.go takes and for the same reason: internal/boundaries skips test
// files, and the separation between the two token systems is a statement about *both*, so proving
// it needs both in one process. The mobile token below is a real one from identity's own issuer,
// **signed with the same key material as the driver keyset**, which is the worst case and the only
// one where the audience is doing the work alone.

// testDriverVerifier is the verifier every guard below is built from.
func testDriverVerifier(t *testing.T, clk clock.Clock) *DriverTokenVerifier {
	t.Helper()

	verifier, err := NewDriverTokenVerifier(testDriverKeyset(t), clk)
	if err != nil {
		t.Fatalf("building the driver token verifier: %v", err)
	}
	return verifier
}

// driverProbe is a handler that records whether the guard let a request through, and what it was
// handed.
//
// It exists so a refusal can be checked for what it *did not* do as well as for what it answered: a
// guard that refused with a 401 and still called the handler would pass a status assertion.
type driverProbe struct {
	reached bool
	grant   DriverGrant
	subject bool
}

func (p *driverProbe) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.reached = true
		p.grant, _ = driverGrantFrom(r.Context())
		_, p.subject = authctx.SubjectFrom(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})
}

// guardedProbe mounts a probe behind the guard on the pattern cmd/api registers the driver route
// under.
//
// A real ServeMux, because the path parameter is the whole point: the guard reads {id} through
// r.PathValue, which is populated by the mux when it dispatches. A guard invoked directly would see
// an empty one and every test here would pass for the wrong reason.
func guardedProbe(t *testing.T, clk clock.Clock) (*driverProbe, http.Handler) {
	t.Helper()

	probe := &driverProbe{}
	mux := http.NewServeMux()
	mux.Handle("GET /v1/driver/jobs/{id}",
		RequireDriverToken(testDriverVerifier(t, clk))(probe.handler()))
	return probe, mux
}

// openLink presents a credential to a driver route.
//
// The credential is a whole header value rather than a bare token so that "no header at all" and
// "a header that is not a bearer credential" are expressible.
func openLink(h http.Handler, jobID uuid.UUID, header string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/v1/driver/jobs/"+jobID.String(), nil)
	if header != "" {
		req.Header.Set("Authorization", header) // spelling:ok — HTTP header name, RFC 9110
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// bearer is the ordinary form of the header above.
func bearer(token string) string { return "Bearer " + token }

// mintDriverLink signs a link for one job, from the keyset every test here verifies against.
func mintDriverLink(t *testing.T, jobID, assignmentID uuid.UUID) DriverToken {
	t.Helper()

	token, err := newIssuer(t, clock.NewFixed(testDriverIssuedAt)).Issue(jobID, assignmentID)
	if err != nil {
		t.Fatalf("issuing a driver link: %v", err)
	}
	return token
}

// mintMobileSession is a real access token from internal/identity, **signed with the driver
// keyset's own key**.
//
// Sharing the key material is deliberate and is what makes this worth testing: with two keysets the
// signature alone would refuse it, and the test would prove nothing about the audience. Configuration
// refuses this arrangement in a deployment (internal/config), which is exactly why the guarantee
// must not depend on it.
func mintMobileSession(t *testing.T) string {
	t.Helper()

	keys, err := identity.NewKeyset(map[string][]byte{testDriverKID: testDriverKey}, testDriverKID)
	if err != nil {
		t.Fatalf("building the mobile keyset from the driver key: %v", err)
	}
	issuer, err := identity.NewAccessTokenIssuer(keys, 15*time.Minute, clock.NewFixed(testDriverIssuedAt))
	if err != nil {
		t.Fatalf("building the access token issuer: %v", err)
	}

	access, err := issuer.Issue(uuid.New(), uuid.New(), identity.RoleProvider)
	if err != nil {
		t.Fatalf("issuing an access token: %v", err)
	}
	return access.Value
}

// errorCodeOf reads error.code out of a refusal.
func errorCodeOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var body errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the refusal is not JSON: %v (%s)", err, rec.Body)
	}
	return body.Error.Code
}

// TestADriverLinkOpensTheJobItNames is the guard's happy path, and the baseline every refusal below
// is measured against.
func TestADriverLinkOpensTheJobItNames(t *testing.T) {
	jobID, assignmentID := uuid.New(), uuid.New()
	link := mintDriverLink(t, jobID, assignmentID)

	probe, router := guardedProbe(t, clock.NewFixed(testDriverIssuedAt))

	rec := openLink(router, jobID, bearer(link.Value))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (%s)", rec.Code, rec.Body)
	}
	if !probe.reached {
		t.Fatal("the guard refused a link for the job it was presented on")
	}

	switch {
	case probe.grant.JobID != jobID:
		t.Errorf("the grant names job %s, want %s", probe.grant.JobID, jobID)
	case probe.grant.AssignmentID != assignmentID:
		t.Errorf("the grant names assignment %s, want %s", probe.grant.AssignmentID, assignmentID)
	case probe.grant.TokenID == "":
		t.Error("the grant carries no token id, so SHIP-109 has nothing to name a link by")
	case !probe.grant.ExpiresAt.Equal(link.ExpiresAt):
		t.Errorf("the grant expires at %s, want the issued %s", probe.grant.ExpiresAt, link.ExpiresAt)
	}
}

// TestADriverLinkOpensNoOtherJob is **the test this ticket turns on**.
//
// "Grants access to exactly one job and nothing else" is not a property of the token — a token is
// only a signed claim — it is a property of what happens when the claim meets a request. A valid
// link presented on another job must be refused, and refused before the handler runs, because a
// handler that ran would answer with somebody else's delivery.
//
// The refusal is 404 rather than 403, deliberately: 403 would confirm to a driver holding one link
// that the job they probed exists, which is exactly what [apiError] refuses to tell a provider about
// a competitor's job.
func TestADriverLinkOpensNoOtherJob(t *testing.T) {
	granted, other := uuid.New(), uuid.New()
	link := mintDriverLink(t, granted, uuid.New())

	probe, router := guardedProbe(t, clock.NewFixed(testDriverIssuedAt))

	rec := openLink(router, other, bearer(link.Value))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a link for %s opened %s with status %d, want 404 (%s)",
			granted, other, rec.Code, rec.Body)
	}
	if probe.reached {
		t.Fatal("the handler ran for a job the link does not grant; the scope check is not in front of it")
	}
	if code := errorCodeOf(t, rec); code != "not_found" {
		t.Errorf("error.code = %q, want not_found — a 403 would confirm the other job exists", code)
	}

	// And the same link still opens its own job, so the refusal above is about the job rather
	// than about the link having been spent.
	if rec := openLink(router, granted, bearer(link.Value)); rec.Code != http.StatusNoContent {
		t.Errorf("the link stopped working on its own job after being refused elsewhere: %d", rec.Code)
	}
}

// TestARouteWithNoJobInItsPatternRefusesEveryRequest pins the failure direction of the one shape the
// guard cannot check at startup.
//
// The comparison needs a job in the path, and the guard is handed a handler rather than a pattern —
// so a route declaring RequireDriverToken on a pattern with no {id} cannot be refused when it is
// attached. What it gets instead is PathValue answering "", which is not an identifier, so every
// request is refused: a route that visibly never works rather than one served with no scope check.
//
// This is the whole of what "prefer a shape where forgetting is impossible" costs here, and it is
// worth a test because the tempting alternative — treating an absent {id} as "no job to check
// against, let it through" — is a one-line change that would pass every other test in this file.
func TestARouteWithNoJobInItsPatternRefusesEveryRequest(t *testing.T) {
	link := mintDriverLink(t, uuid.New(), uuid.New())

	probe := &driverProbe{}
	mux := http.NewServeMux()
	mux.Handle("GET /v1/driver/anything",
		RequireDriverToken(testDriverVerifier(t, clock.NewFixed(testDriverIssuedAt)))(probe.handler()))

	req := httptest.NewRequest(http.MethodGet, "/v1/driver/anything", nil)
	req.Header.Set("Authorization", bearer(link.Value)) // spelling:ok — HTTP header name, RFC 9110

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("a driver route with no job in its pattern answered %d, want 404 (%s)",
			rec.Code, rec.Body)
	}
	if probe.reached {
		t.Fatal("a driver-token route with no job in its pattern was served with no scope check at " +
			"all, which is the one outcome the class exists to prevent")
	}
}

// TestTheGuardRefusesEveryUnusableLink walks the ways a credential can fail to be one.
//
// The expired case is the only one that gets a code of its own. Everything else is
// `unauthenticated`, deliberately undifferentiated: a legitimate driver can do nothing differently
// about a bad signature than about the wrong audience, and saying which check refused a credential
// is free help to somebody probing (httpx.ResolveSubject makes the same call).
func TestTheGuardRefusesEveryUnusableLink(t *testing.T) {
	jobID := uuid.New()
	link := mintDriverLink(t, jobID, uuid.New())

	// A payload edited after signing. The last character of the claims segment is changed, so the
	// signature no longer covers what the token says.
	parts := strings.Split(link.Value, ".")
	if len(parts) != 3 {
		t.Fatalf("a signed token has three parts, got %d", len(parts))
	}
	tampered := parts[0] + "." + parts[1][:len(parts[1])-1] + "A." + parts[2]

	unrelated, err := NewKeyset(map[string][]byte{testDriverKID: testUnrelatedKey}, testDriverKID)
	if err != nil {
		t.Fatalf("building an unrelated keyset: %v", err)
	}
	elsewhere, err := NewDriverTokenIssuer(unrelated, testDriverTokenTTL, clock.NewFixed(testDriverIssuedAt))
	if err != nil {
		t.Fatalf("building an issuer on an unrelated keyset: %v", err)
	}
	foreign, err := elsewhere.Issue(jobID, uuid.New())
	if err != nil {
		t.Fatalf("issuing from an unrelated keyset: %v", err)
	}

	for _, tc := range []struct {
		name   string
		header string
		clk    clock.Clock
		code   string
	}{
		{name: "no credential at all", header: "", code: "unauthenticated"},
		{name: "a scheme this service does not issue", header: "Basic c2FtOnBhdGVs", code: "unauthenticated"},
		{name: "the scheme and nothing after it", header: "Bearer", code: "unauthenticated"},
		{name: "an empty bearer credential", header: "Bearer ", code: "unauthenticated"},
		{name: "something that is not a token", header: bearer("not-a-token"), code: "unauthenticated"},
		{name: "a payload edited after signing", header: bearer(tampered), code: "unauthenticated"},
		{name: "a link signed with somebody else's key", header: bearer(foreign.Value), code: "unauthenticated"},

		// The one refusal a driver can act on, eight days after a seven-day link was issued.
		{
			name:   "a link that has run out",
			header: bearer(link.Value),
			clk:    clock.NewFixed(testDriverIssuedAt.Add(8 * 24 * time.Hour)),
			code:   "delivery_driver_link_expired",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clk := tc.clk
			if clk == nil {
				clk = clock.NewFixed(testDriverIssuedAt)
			}

			probe, router := guardedProbe(t, clk)

			rec := openLink(router, jobID, tc.header)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body)
			}
			if probe.reached {
				t.Fatal("the handler ran behind a refused credential")
			}
			if code := errorCodeOf(t, rec); code != tc.code {
				t.Errorf("error.code = %q, want %q", code, tc.code)
			}
			// RFC 9110 requires a challenge on a 401. A client with no way to tell that the
			// endpoint wanted a bearer credential is a client that guesses.
			if rec.Header().Get("WWW-Authenticate") == "" {
				t.Error("the 401 carries no WWW-Authenticate challenge")
			}
		})
	}
}

// TestAMobileSessionCannotOpenADriverRoute is the direction of CLAUDE.md's invariant that could not
// exist before this ticket.
//
// SHIP-107 proved both directions of the *parser* — Docs/11 §8 keeps both verifiers with one owner
// so that TestTheTwoTokenSystemsCannotBeExchanged can — but the HTTP half only went one way, because
// no route accepted a driver token. This is the other half in Go, and
// scripts/verify/70-delivery.sh is the same pair against the running binary.
//
// The mobile token here is real and is signed with the driver keyset's own key, so the signature
// verifies and the **audience** is the only thing refusing it.
func TestAMobileSessionCannotOpenADriverRoute(t *testing.T) {
	probe, router := guardedProbe(t, clock.NewFixed(testDriverIssuedAt))

	rec := openLink(router, uuid.New(), bearer(mintMobileSession(t)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a mobile session token opened a driver route with status %d, want 401 (%s)",
			rec.Code, rec.Body)
	}
	if probe.reached {
		t.Fatal("a mobile session token reached a driver-token handler")
	}
	if code := errorCodeOf(t, rec); code != "unauthenticated" {
		t.Errorf("error.code = %q, want unauthenticated — which check refused it is not a "+
			"client's business", code)
	}
}

// TestADriverRouteProducesNoSubject holds the negative half of Docs/10 §5 where the code could
// break it.
//
// A driver token must never resolve into an authctx.Subject: that type is what a signed-in account
// looks like to every domain in the service, so producing one from a driver's link would be the
// exchange the invariant forbids, performed by the platform itself. SHIP-107 made it structurally
// impossible — no `sub`, no `role`, no `sid`, so there is nothing to build one from — and this is
// the assertion that the middleware did not put the material back.
//
// cmd/api/driverauth_test.go asserts the same thing from outside, against the router. Both are
// worth having: that one holds the seam, and this one holds the code that fills it.
func TestADriverRouteProducesNoSubject(t *testing.T) {
	jobID := uuid.New()
	probe, router := guardedProbe(t, clock.NewFixed(testDriverIssuedAt))

	if rec := openLink(router, jobID, bearer(mintDriverLink(t, jobID, uuid.New()).Value)); rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (%s)", rec.Code, rec.Body)
	}
	if probe.subject {
		t.Fatal("a verified driver link produced an authctx.Subject; the two token systems have " +
			"been joined (Docs/10 §5, CLAUDE.md)")
	}
}

// --- the endpoint, against a real database ----------------------------------------------------

// newDriverRouter mounts the real handler behind the real guard, on the pattern cmd/api serves.
func newDriverRouter(t *testing.T, pool *pgxpool.Pool, clk clock.Clock) http.Handler {
	t.Helper()

	handler, err := NewHandler(newTestService(), pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /v1/driver/jobs/{id}",
		RequireDriverToken(testDriverVerifier(t, clk))(handler.DriverJob()))
	return mux
}

type driverJobBody struct {
	JobID         string `json:"job_id"`
	AssignmentID  string `json:"assignment_id"`
	DriverName    string `json:"driver_name"`
	AssignedAt    string `json:"assigned_at"`
	LinkExpiresAt string `json:"link_expires_at"`
}

// TestTheDriverEndpointAnswersWithTheAssignmentTheLinkGrants is SHIP-108 at the wire, end to end:
// a provider assigns, the token that assignment minted is presented, and the delivery opens.
func TestTheDriverEndpointAnswersWithTheAssignmentTheLinkGrants(t *testing.T) {
	pool := pgtest.DB(t)
	router := newDriverRouter(t, pool, clock.NewFixed(testDriverIssuedAt))

	customer := newAccount(t, pool, "driver-open-c@example.com", "+61400000680", "customer")
	provider := newAccount(t, pool, "driver-open-p@example.com", "+61400000681", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	assignment, link, _, err := assignGranting(t, pool, newTestService(), provider, jobID, Nomination{
		DriverName:   "Sam Patel",
		DriverMobile: "0412 345 678",
	})
	if err != nil {
		t.Fatalf("assigning a driver: %v", err)
	}

	rec := openLink(router, jobID, bearer(link.Value))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	body := decode[driverJobBody](t, rec)
	switch {
	case body.JobID != jobID.String():
		t.Errorf("job_id = %q, want %s", body.JobID, jobID)
	case body.AssignmentID != assignment.ID.String():
		t.Errorf("assignment_id = %q, want %s", body.AssignmentID, assignment.ID)
	case body.DriverName != "Sam Patel":
		t.Errorf("driver_name = %q", body.DriverName)
	case !strings.HasSuffix(body.AssignedAt, "Z"):
		t.Errorf("assigned_at = %q, want UTC", body.AssignedAt)
	case !strings.HasSuffix(body.LinkExpiresAt, "Z"):
		t.Errorf("link_expires_at = %q, want UTC", body.LinkExpiresAt)
	}

	// The response is held to a closed set of keys rather than searched for the field it must not
	// carry, which is the argument SHIP-83's budget test makes: a search for "driver_mobile"
	// catches driver_mobile and misses "mobile" or "phone". The driver already knows their own
	// number, and a link can be forwarded again — see [driverJobResponse].
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("the response is not JSON: %v", err)
	}
	// **`delivered_at` is deliberately absent here and is not missing from this set** (SHIP-123).
	// It is `omitempty` and this fixture's delivery has not been delivered, so an undelivered job
	// still answers with exactly five fields — which is what makes the closed set worth keeping
	// after a sixth field was added to the shape. `TestTheDriverIsToldWhenTheDeliveryIsFinished`
	// holds the other side: delivered, and the sixth key appears.
	want := map[string]bool{
		"job_id": true, "assignment_id": true, "driver_name": true,
		"assigned_at": true, "link_expires_at": true,
	}
	for key := range raw {
		if !want[key] {
			t.Errorf("the driver's view carries an unexpected field %q; SHIP-120 adds the "+
				"delivery detail deliberately rather than by accident", key)
		}
	}
	if len(raw) != len(want) {
		t.Errorf("the driver's view has %d fields, want %d: %v", len(raw), len(want), raw)
	}
}

// TestADriverLinkOpensNoOtherDeliveryOverHTTP is the *Done when*'s first clause against the real
// handler and a real second job — the same assertion as [TestADriverLinkOpensNoOtherJob], with
// nothing stubbed, so that a refusal cannot be an artefact of the fixture.
func TestADriverLinkOpensNoOtherDeliveryOverHTTP(t *testing.T) {
	pool := pgtest.DB(t)
	router := newDriverRouter(t, pool, clock.NewFixed(testDriverIssuedAt))
	svc := newTestService()

	customer := newAccount(t, pool, "driver-scope-c@example.com", "+61400000682", "customer")
	provider := newAccount(t, pool, "driver-scope-p@example.com", "+61400000683", "provider")

	first := awardedJob(t, pool, customer, provider)
	second := awardedJob(t, pool, customer, provider)

	_, firstLink, _, err := assignGranting(t, pool, svc, provider, first,
		Nomination{DriverName: "Sam Patel", DriverMobile: "0412 345 678"})
	if err != nil {
		t.Fatalf("assigning the first job: %v", err)
	}
	_, secondLink, _, err := assignGranting(t, pool, svc, provider, second,
		Nomination{DriverName: "Ravi Chandra", DriverMobile: "0412 000 999"})
	if err != nil {
		t.Fatalf("assigning the second job: %v", err)
	}

	// Each link opens its own delivery.
	for name, tc := range map[string]struct {
		job  uuid.UUID
		link DriverToken
	}{
		"the first driver on the first job":   {job: first, link: firstLink},
		"the second driver on the second job": {job: second, link: secondLink},
	} {
		if rec := openLink(router, tc.job, bearer(tc.link.Value)); rec.Code != http.StatusOK {
			t.Errorf("%s: status = %d, want 200 (%s)", name, rec.Code, rec.Body)
		}
	}

	// And neither opens the other's, in both directions — a one-way check would pass against a
	// guard that compared the wrong pair of identifiers.
	for name, tc := range map[string]struct {
		job  uuid.UUID
		link DriverToken
	}{
		"the first driver on the second job": {job: second, link: firstLink},
		"the second driver on the first job": {job: first, link: secondLink},
	} {
		rec := openLink(router, tc.job, bearer(tc.link.Value))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404 (%s)", name, rec.Code, rec.Body)
		}
	}
}

// TestALinkWhoseAssignmentHasEndedOpensNothing is the question a claim cannot answer.
//
// A token is stateless and cannot be recalled, so it keeps verifying after the driver has been stood
// down. [Service.AssignmentFor] asks the row instead, which is the lookup [Service.Driver]'s comment
// reserved for this ticket and the mechanism SHIP-109 will reissue against.
//
// Nothing writes `unassigned_at` through an endpoint yet, so the fixture writes it directly. That is
// the honest way to test a state the platform can hold and cannot yet reach.
func TestALinkWhoseAssignmentHasEndedOpensNothing(t *testing.T) {
	pool := pgtest.DB(t)
	router := newDriverRouter(t, pool, clock.NewFixed(testDriverIssuedAt))

	customer := newAccount(t, pool, "driver-stood-c@example.com", "+61400000684", "customer")
	provider := newAccount(t, pool, "driver-stood-p@example.com", "+61400000685", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	assignment, link, _, err := assignGranting(t, pool, newTestService(), provider, jobID,
		Nomination{DriverName: "Sam Patel", DriverMobile: "0412 345 678"})
	if err != nil {
		t.Fatalf("assigning a driver: %v", err)
	}

	if rec := openLink(router, jobID, bearer(link.Value)); rec.Code != http.StatusOK {
		t.Fatalf("the link did not open its delivery before the stand-down: %d", rec.Code)
	}

	if _, err := pool.Exec(t.Context(),
		`UPDATE driver_assignments SET unassigned_at = now() WHERE id = $1`, assignment.ID); err != nil {
		t.Fatalf("standing the driver down: %v", err)
	}

	rec := openLink(router, jobID, bearer(link.Value))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a link whose assignment has ended still opened the delivery: %d (%s)",
			rec.Code, rec.Body)
	}
	// 404 rather than 401: the credential is fine and the assignment behind it is gone, so
	// telling the driver their link is invalid would send them asking for the wrong thing.
	if code := errorCodeOf(t, rec); code != "not_found" {
		t.Errorf("error.code = %q, want not_found", code)
	}
}
