package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
)

// SHIP-120a's wiring, through the real router.
//
// # Why these are here and not in internal/delivery
//
// The domain's own tests drive [delivery.Service.RecordDriverMilestone] against a real database and
// prove what it writes. What they cannot reach is the half this ticket is actually about: that the
// route is *declared* with the driver's auth class, that the guard the composition root builds is
// the one attached to it, and that the two credential systems refuse each other on the endpoints a
// client calls. All three live in package main.
//
// # These ask what the wire answered, not what the source says
//
// Wave 7 recorded a driver surface that derived the job it acted on from its own credential rather
// than from what was asked for, and it survived a full suite while rendering another job's delivery
// with a 200 (Docs/11 §7). The finding that came out of it is the shape of the test rather than the
// mutation: **ask the process what it answered for a job the credential does not name**, because a
// source scan is what §7b's budget guard was already defeated by.
//
// The granted job answers 503 rather than 201 throughout, and that is the assertion working:
// testDeps carries no pool, so a request that got past the guard reaches a handler with no
// database. What matters is that it *reached* one, which is what tells a refusal apart from a route
// that refuses everything.

// driverMilestonePath is the route SHIP-120a adds. Written out rather than built from
// [driverJobPath], because the two are separate declarations in the manifest and a test that
// derived one from the other could not notice them diverging.
const driverMilestonePath = "/v1/driver/jobs/"

func driverMilestoneURL(job uuid.UUID) string {
	return driverMilestonePath + job.String() + "/milestones"
}

// recordMilestone posts one milestone, with whatever credential and key the caller names.
func recordMilestone(router http.Handler, path, credential, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if credential != "" {
		req.Header.Set(httpx.HeaderAuthorization, "Bearer "+credential)
	}
	if key != "" {
		req.Header.Set(httpx.HeaderIdempotencyKey, key)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// milestoneErrorCode is `error.code` out of a refusal, which is what a client branches on.
//
// A local reader rather than contract_test.go's errorCodeOf, which takes an already-decoded body:
// every assertion here starts from a recorder.
func milestoneErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, rec.Body.String())
	}
	return body.Error.Code
}

// TestTheDriverMilestoneRouteRecordsOnOneJobAndNoOther is the first half of SHIP-120a's *Done
// when*, asked of the running router.
//
// A link for one job, presented on another, answers exactly what a job that does not exist answers.
// It is the same property SHIP-108 established for the read, and it has to be re-established for
// the write rather than inherited: the guard is attached per route, so a route declared with the
// wrong class — or with a path parameter the guard cannot find — is scoped by nothing at all.
func TestTheDriverMilestoneRouteRecordsOnOneJobAndNoOther(t *testing.T) {
	router := testRouter()
	granted, other := uuid.New(), uuid.New()
	link := testDriverLink(t, granted)

	const body = `{"milestone":"picked_up"}`

	if rec := recordMilestone(router, driverMilestoneURL(granted), link, "key-granted", body); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("the link's own job answered %d, want 503 from a handler with no database "+
			"— anything else means the guard did not let it through (%s)", rec.Code, rec.Body)
	}

	rec := recordMilestone(router, driverMilestoneURL(other), link, "key-other", body)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a link for one job recorded a milestone on another with status %d, want 404 "+
			"— the token grants exactly one job (CLAUDE.md) (%s)", rec.Code, rec.Body)
	}
	if code := milestoneErrorCode(t, rec); code != string(httpx.CodeNotFound) {
		t.Errorf("code = %q, want %q — a stranger's job must answer what a missing one does",
			code, httpx.CodeNotFound)
	}
}

// TestNeitherTokenSystemRecordsAMilestoneOnTheOthersRoute is the second half of the *Done when*,
// and it is the reason the driver's write is a second route rather than a second auth class.
//
// Both directions, because only one of them is the obvious one. A mobile access token on the
// driver's route is what a confused client does; a driver's link on `POST /v1/jobs/{id}/milestones`
// is what an attacker does, and it is the direction that would quietly widen a seven-day
// forwardable credential into a provider's endpoint if the classes were ever merged.
func TestNeitherTokenSystemRecordsAMilestoneOnTheOthersRoute(t *testing.T) {
	router := testRouter()
	jobID := uuid.New()

	access, _, _ := testAccessToken(t, identity.RoleProvider)
	link := testDriverLink(t, jobID)

	const body = `{"milestone":"picked_up"}`

	for name, tc := range map[string]struct {
		path       string
		credential string
		key        string
	}{
		"a mobile session on the driver's milestone route": {
			path: driverMilestoneURL(jobID), credential: access, key: "key-session-on-driver-route",
		},
		"a driver link on the user milestone route": {
			path: "/v1/jobs/" + jobID.String() + "/milestones", credential: link, key: "key-link-on-user-route",
		},
	} {
		t.Run(name, func(t *testing.T) {
			rec := recordMilestone(router, tc.path, tc.credential, tc.key, body)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 — one token system was accepted by the "+
					"other (Docs/10 §5, CLAUDE.md)\n  body: %s", rec.Code, rec.Body)
			}
		})
	}
}

// TestTheDriverMilestoneRouteIsRefusedWithoutAnIdempotencyKey holds the invariant on the one route
// it matters most for.
//
// SHIP-15 applies the middleware group-wide, so this is not a property of the route — which is
// exactly why it is asserted here anyway: **a driver on a mobile browser is the retry case the rule
// exists for**, and a change that moved this route out of the group would break the guarantee with
// no other test noticing.
//
// The refusal comes before the guard, so no credential is presented: what is being checked is that
// the request never reaches a handler, and a 401 here would mean the ordering had changed.
func TestTheDriverMilestoneRouteIsRefusedWithoutAnIdempotencyKey(t *testing.T) {
	rec := recordMilestone(testRouter(), driverMilestoneURL(uuid.New()), "", "", `{"milestone":"picked_up"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if code := milestoneErrorCode(t, rec); code != string(httpx.CodeIdempotencyKeyRequired) {
		t.Errorf("code = %q, want %q", code, httpx.CodeIdempotencyKeyRequired)
	}
}

// TestEveryDriverTokenRouteNamesItsJobInThePath is the check that has no other home.
//
// delivery.RequireDriverToken reads `{id}` and compares it with the job inside the token, and
// `r.PathValue` answers the empty string for a parameter the pattern never declared. A route
// declaring this class with a differently named parameter is therefore refused for every request —
// the loud direction, and still a route that has to be found by hand. This finds it at build time,
// for every route the class is ever put on rather than for the two that exist today.
func TestEveryDriverTokenRouteNamesItsJobInThePath(t *testing.T) {
	found := 0
	for _, r := range routes() {
		if r.Auth != RequireDriverToken {
			continue
		}
		found++
		if !strings.Contains(r.Pattern, "/{id}") {
			t.Errorf("%s %s is served on a driver token and has no {id} in its pattern; the "+
				"guard would have nothing to compare the token against", r.Method, r.fullPath())
		}
	}
	if found == 0 {
		t.Fatal("no route declares RequireDriverToken; this test is asserting nothing")
	}
}
