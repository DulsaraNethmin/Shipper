package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
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

// --- SHIP-121a: the driver reads the milestones on their own delivery -----------------------------

// listMilestones gets one milestone page, with whatever credential the caller names.
//
// A reader beside [recordMilestone] rather than a method on it: the write carries a body and an
// idempotency key and the read carries neither, and a helper taking both and using half would
// invite a GET to be asserted with a key it never sends.
func listMilestones(router http.Handler, path, credential string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if credential != "" {
		req.Header.Set(httpx.HeaderAuthorization, "Bearer "+credential)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// partyMilestonePath is the parties' shelf, which SHIP-121a must leave closed to a driver.
func partyMilestoneURL(job uuid.UUID) string {
	return "/v1/jobs/" + job.String() + "/delivery/milestones"
}

// TestNeitherTokenSystemListsMilestonesOnTheOthersRoute is SHIP-121a's second and third clauses,
// asked of the running router rather than of the source.
//
// The domain's own tests mount the read behind the real guard and prove what it answers for a job
// the link does not name. What they cannot reach is the direction that matters most here: **a
// driver's link on the parties' shelf**, `GET /v1/jobs/{id}/delivery/milestones`, which is
// `RequireUser` and carries `recipient_name` and `delivery_note` on a delivered milestone. Adding a
// driver read next to it is exactly the change that would tempt someone to widen that class, and
// this is what would fail if they did.
//
// Both directions, because only one is the obvious one — the same bargain
// [TestNeitherTokenSystemRecordsAMilestoneOnTheOthersRoute] strikes for the write.
func TestNeitherTokenSystemListsMilestonesOnTheOthersRoute(t *testing.T) {
	router := testRouter()
	jobID := uuid.New()

	access, _, _ := testAccessToken(t, identity.RoleProvider)
	link := testDriverLink(t, jobID)

	for name, tc := range map[string]struct {
		path       string
		credential string
	}{
		"a mobile session on the driver's milestone read": {
			path: driverMilestoneURL(jobID), credential: access,
		},
		"a driver link on the parties' milestone shelf": {
			path: partyMilestoneURL(jobID), credential: link,
		},
	} {
		t.Run(name, func(t *testing.T) {
			rec := listMilestones(router, tc.path, tc.credential)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 — one token system was accepted by the "+
					"other (Docs/10 §5, CLAUDE.md)\n  body: %s", rec.Code, rec.Body)
			}
		})
	}
}

// TestTheDriverMilestoneReadIsServedOnTheDriversOwnLink is the assertion the refusals above are
// worth nothing without.
//
// A route that refused every credential would pass every row of
// [TestNeitherTokenSystemListsMilestonesOnTheOthersRoute]. This is what tells a scoped route apart
// from a closed one: the link's own job gets *past* the guard and reaches a handler, which answers
// 503 because testDeps carries no pool. Reaching a database is the signal; what it then says is the
// domain package's half.
func TestTheDriverMilestoneReadIsServedOnTheDriversOwnLink(t *testing.T) {
	jobID := uuid.New()
	rec := listMilestones(testRouter(), driverMilestoneURL(jobID), testDriverLink(t, jobID))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("the link's own job answered %d, want 503 from a handler with no database "+
			"— anything else means the guard did not let it through (%s)", rec.Code, rec.Body)
	}
}

// TestTheDriverMilestoneReadCarriesNoIdempotencyKey records that a GET is outside SHIP-15's rule.
//
// The write beside it is refused without a key ([TestTheDriverMilestoneRouteIsRefusedWithoutAnIdempotencyKey]).
// The read must not be: the invariant is about state-changing requests, and a middleware that
// started demanding keys on safe methods would break every client for no gain. Asserted because the
// two routes share a path and a class and differ only by method, which is the shape a later change
// is most likely to flatten.
func TestTheDriverMilestoneReadCarriesNoIdempotencyKey(t *testing.T) {
	rec := listMilestones(testRouter(), driverMilestoneURL(uuid.New()), "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 — an unauthenticated GET must reach the guard, not be "+
			"turned back for a missing idempotency key (%s)", rec.Code, rec.Body)
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

// TestTheOutOfDeliveryStatusesAreWhatTheTransitionTableSays derives SHIP-113's set from Docs/02 §2
// rather than trusting the three names written beside [refusal].
//
// [outOfTheDeliveryStatuses] is a hand-written list, deliberately — see its comment for why naming
// them is what keeps SHIP-112's premature refusals untouched. This is the other half of that
// bargain: the list is *checked* against the transition table, so a status added to Docs/02 §2, or
// an edge added out of Disputed, fails here instead of silently changing which milestones the
// platform retains.
//
// # The derivation, and the one subtlety in it
//
// A job has left the delivery when **no milestone status is reachable from where it stands**,
// searching Docs/02 §2's graph and counting the job's own status as reachable from itself. That
// last clause is what keeps 'Delivered' out of the set: a job standing at 'Delivered' is still on
// its delivery, and a queued milestone against it is late or premature rather than overruled.
//
// The search asks [jobs.Permitted] rather than reading a table of its own, so there is no second
// copy of Docs/02 §2 anywhere in this test.
func TestTheOutOfDeliveryStatusesAreWhatTheTransitionTableSays(t *testing.T) {
	// The five milestone statuses, named as the four move methods above name them — a delivery
	// is *on* one of these or on its way to one.
	onADelivery := map[jobs.Status]bool{
		jobs.StatusDriverAssigned:  true,
		jobs.StatusEnRouteToPickup: true,
		jobs.StatusPickedUp:        true,
		jobs.StatusInTransit:       true,
		jobs.StatusDelivered:       true,
	}

	// Breadth-first over Docs/02 §2, including the starting status itself.
	reaches := func(start jobs.Status) bool {
		seen := map[jobs.Status]bool{start: true}
		queue := []jobs.Status{start}
		for len(queue) > 0 {
			at := queue[0]
			queue = queue[1:]
			if onADelivery[at] {
				return true
			}
			for _, next := range jobs.Statuses {
				if !seen[next] && jobs.Permitted(at, next) {
					seen[next] = true
					queue = append(queue, next)
				}
			}
		}
		return false
	}

	var derived []jobs.Status
	for _, status := range jobs.Statuses {
		if !reaches(status) {
			derived = append(derived, status)
		}
	}

	slices.Sort(derived)
	named := slices.Clone(outOfTheDeliveryStatuses)
	slices.Sort(named)

	if !slices.Equal(derived, named) {
		t.Errorf("Docs/02 §2 says a job has left the delivery in %v; outOfTheDeliveryStatuses "+
			"names %v.\nOne of the two changed without the other, and the difference decides "+
			"whether a queued milestone is retained (SHIP-113) or refused (SHIP-112).",
			derived, named)
	}

	// Named as well as derived, because the point of the ticket is which statuses these are and
	// a derivation that silently produced the empty set would satisfy an equality test alone.
	for _, status := range []jobs.Status{jobs.StatusCancelled, jobs.StatusCompleted, jobs.StatusDisputed} {
		if !outOfTheDelivery(status) {
			t.Errorf("a job at %s is still on its delivery; Docs/02 §2 offers it no way back", status)
		}
	}
	for _, status := range []jobs.Status{
		jobs.StatusOpen, jobs.StatusAwarded, jobs.StatusPickedUp, jobs.StatusInTransit, jobs.StatusDelivered,
	} {
		if outOfTheDelivery(status) {
			t.Errorf("a job at %s was treated as having lost its delivery; a milestone against it "+
				"is late or premature, which is SHIP-112's question and not SHIP-113's", status)
		}
	}
}
