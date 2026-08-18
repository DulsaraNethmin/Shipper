package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/admin"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/delivery"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/idempotency"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/passwords"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/ratelimit"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/redistest"
)

// SHIP-17b — the fixture world the contract check is driven from.
//
// # What this replaces
//
// SHIP-17a's exercisable() filter reached **4 of the 86 routes** on the manifest: it drove
// /health, /v1/app/minimum-version, /v1/app/policy and /v1/{$}, and named the other 82 in a log
// line. Naming them was the right call at the time — a test that quietly stops covering half the
// surface reads exactly like one that covers all of it — but a named gap is still a gap, and
// SHIP-159's `expires_at` drifted through it for a whole wave.
//
// The three things the filter was standing in for are supplied here: a credential of each of the
// three kinds, rows for the parameterised paths to address, and a request body per operation.
//
// # Why the expected status is part of every fixture
//
// **Every one of the 86 operations declares a 500.** So a route driven against an empty world
// answers 500, `openapi3filter.ValidateResponse` finds a declared response that matches, and the
// check passes having proved nothing. That is the vacuous pass this file has to avoid, and the
// defence is that each case states the status it expects and the status is asserted **before** the
// body is validated. A fixture that stops reaching its handler fails loudly instead of quietly
// validating an error envelope.
//
// It is the same argument TestDocumentExpiryIsInTheContract makes about rejection cases: a schema
// check fed only bodies it likes passes just as well with validation switched off.
//
// # Why request bodies are literal JSON
//
// The handler request structs are unexported in their domain packages, so no test here can reflect
// over them. That reads as a wall and is not one: the contract is a statement about **bytes**, and
// a fixture that marshalled a Go struct would check the service against itself. Literal JSON is
// what a client sends.

// contractAuth is which credential a case presents.
type contractAuth int

const (
	asAnonymous contractAuth = iota
	asCustomer
	asProvider
	asAdmin
	asDriver
)

func (a contractAuth) String() string {
	switch a {
	case asCustomer:
		return "customer"
	case asProvider:
		return "provider"
	case asAdmin:
		return "administrator"
	case asDriver:
		return "driver"
	default:
		return "anonymous"
	}
}

// contractPassword is the password both seeded credentials use.
const contractPassword = "correct-horse-battery-staple"

// contractWorld is one router over one database, with a credential of every kind and the rows the
// parameterised paths address.
type contractWorld struct {
	router http.Handler
	pool   *pgxpool.Pool

	customerToken, customerEmail string
	customerID                   uuid.UUID
	providerToken                string
	providerID                   uuid.UUID
	adminToken, adminEmail       string
	adminID                      uuid.UUID
	driverToken                  string

	// jobID is a job the customer owns and the driver token is scoped to.
	jobID uuid.UUID
	// bidID is a bid the provider holds on that job.
	bidID uuid.UUID
	// vehicleID is an active vehicle the provider owns, and retiredVehicleID a deactivated one —
	// two rows because deactivate and reactivate each need the state the other produces.
	vehicleID        uuid.UUID
	retiredVehicleID uuid.UUID
	// refreshToken and deviceSessionID come from a real sign-in.
	refreshToken    string
	deviceSessionID uuid.UUID
	// verificationObjectKey is a key the platform signed for this provider.
	verificationObjectKey string
}

// newContractWorld seeds the smallest world every route can be addressed in.
//
// A world is not shared across a mutating case, and the probe that wrote these fixtures is why:
// DELETE /v1/admin/sessions/current ends the administrator session, so every admin route driven
// after it in the same world answers 401 — and POST /v1/jobs/{id}/cancel moves the job to
// Cancelled, so POST /v1/jobs/{id}/extend then answers 409. Both read as real failures and are
// fixture bleed.
func newContractWorld(t *testing.T) *contractWorld {
	t.Helper()

	pool := pgtest.DB(t)
	client, prefix := redistest.Client(t)

	deps := testDeps()
	deps.Pool = pool
	deps.Redis = client

	adminGuard, err := newAdminGuard(deps.Config, pool, deps.Clock)
	if err != nil {
		t.Fatalf("building the administrator guard: %v", err)
	}

	w := &contractWorld{
		pool: pool,
		router: newRouter(deps, idempotency.NewMemoryStore(), testAuthenticator(),
			testDriverGuard(), adminGuard),
	}

	hasher, err := passwords.NewHasher(passwords.Argon2Profile{
		MemoryKiB:   deps.Config.Passwords.Argon2.MemoryKiB,
		Iterations:  deps.Config.Passwords.Argon2.Iterations,
		Parallelism: deps.Config.Passwords.Argon2.Parallelism,
	})
	if err != nil {
		t.Fatalf("building the hasher: %v", err)
	}

	// Two mobile users. The token is minted rather than signed in for, because what is under
	// test is the shape of every response and not the sign-in path — which has tests of its own.
	customerToken, customerID, _ := testAccessToken(t, identity.RoleCustomer)
	providerToken, providerID, _ := testAccessToken(t, identity.RoleProvider)
	w.customerToken, w.customerID = customerToken, customerID
	w.providerToken, w.providerID = providerToken, providerID

	// The rows behind those tokens. A minted token names a user id no row carries, and a handler
	// reading users.role then answers something the fixture did not ask for.
	//
	// The password hash is real rather than a placeholder so that POST /v1/auth/login can be
	// driven as itself — it is one of the few operations whose success needs a credential the
	// request body carries rather than the header.
	hashed, err := hasher.Hash(contractPassword)
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	w.customerEmail = "contract-customer@example.com"
	insertContractUser(t, pool, customerID, w.customerEmail, "+61400900001", "customer", hashed)
	insertContractUser(t, pool, providerID, "contract-provider@example.com", "+61400900002", "provider", hashed)

	w.seedAdministrator(t, deps, pool, client, prefix, hasher)
	w.seedJob(t)
	w.seedDriverToken(t, deps)
	w.seedVehicle(t)
	w.seedSignedInDevice(t)
	w.seedVerificationUpload(t)

	return w
}

// drive sends one request through the router and returns the decoded body.
//
// Seeding through the API rather than with an INSERT wherever the API can do it: a hand-written
// INSERT names columns, and a fixture that names columns breaks on a migration that renames one
// while the endpoint it was standing in for still works.
func (w *contractWorld) drive(t *testing.T, method, path string, auth contractAuth, body string) (int, map[string]any) {
	t.Helper()

	rec := httptest.NewRecorder()
	w.router.ServeHTTP(rec, w.request(t, method, path, auth, body))

	decoded := map[string]any{}
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &decoded)
	}
	return rec.Code, decoded
}

// seedVehicle adds the vehicle the /v1/fleet/vehicles/{id} routes address.
func (w *contractWorld) seedVehicle(t *testing.T) {
	t.Helper()

	status, body := w.drive(t, http.MethodPost, "/v1/fleet/vehicles", asProvider,
		`{"registration":"XCT471","vehicle_type":"van","make":"Ford","model":"Transit"}`)
	if status != http.StatusCreated {
		t.Fatalf("seeding a vehicle: status %d, body %v", status, body)
	}
	id, _ := body["id"].(string)
	parsed, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("the created vehicle has no id: %v", body)
	}
	w.vehicleID = parsed

	status, body = w.drive(t, http.MethodPost, "/v1/fleet/vehicles", asProvider,
		`{"registration":"XCT472","vehicle_type":"van","make":"Ford","model":"Transit"}`)
	if status != http.StatusCreated {
		t.Fatalf("seeding the second vehicle: status %d, body %v", status, body)
	}
	id, _ = body["id"].(string)
	if parsed, err = uuid.Parse(id); err != nil {
		t.Fatalf("the second vehicle has no id: %v", body)
	}
	w.retiredVehicleID = parsed

	if status, body = w.drive(t, http.MethodPost,
		"/v1/fleet/vehicles/"+w.retiredVehicleID.String()+"/deactivate", asProvider, ""); status != http.StatusOK {
		t.Fatalf("deactivating the second vehicle: status %d, body %v", status, body)
	}
}

// seedVerificationUpload asks for an upload URL and keeps the key it answers with.
//
// The key is signed for one provider, so POST /v1/provider/verification/documents refuses one this
// file invented. Asking the endpoint is both shorter than forging a signature and the only version
// that stays true when the signing changes.
func (w *contractWorld) seedVerificationUpload(t *testing.T) {
	t.Helper()

	status, body := w.drive(t, http.MethodPost, "/v1/provider/verification/documents/uploads", asProvider,
		`{"content_type":"image/jpeg","content_length":1048576}`)
	if status != http.StatusOK {
		t.Fatalf("asking for a verification upload URL: status %d, body %v", status, body)
	}
	w.verificationObjectKey, _ = body["object_key"].(string)
	if w.verificationObjectKey == "" {
		t.Fatalf("the upload response carries no object_key: %v", body)
	}
}

// seedSignedInDevice signs the customer in over the wire, which is the only way to get a refresh
// token and a device-session id — both are minted by the sign-in path and neither is a row this
// file may reasonably forge.
func (w *contractWorld) seedSignedInDevice(t *testing.T) {
	t.Helper()

	status, body := w.drive(t, http.MethodPost, "/v1/auth/login", asAnonymous,
		`{"email":"`+w.customerEmail+`","password":"`+contractPassword+`","device_label":"Seed device"}`)
	if status != http.StatusOK {
		t.Fatalf("signing the customer in: status %d, body %v", status, body)
	}
	w.refreshToken, _ = body["refresh_token"].(string)
	if w.refreshToken == "" {
		t.Fatalf("the sign-in response carries no refresh_token: %v", body)
	}

	// The minted token is replaced by the signed-in one, and this is not tidying. A minted token
	// names a device_sessions row that does not exist, and POST /v1/notifications/device-tokens
	// binds its registration to that row — so the fixture answered 500 where a real client gets a
	// 201. Every customer case is better off on a credential a sign-in actually produced.
	if access, _ := body["access_token"].(string); access != "" {
		w.customerToken = access
	}

	status, sessions := w.drive(t, http.MethodGet, "/v1/auth/sessions", asCustomer, "")
	if status != http.StatusOK {
		t.Fatalf("listing sessions: status %d, body %v", status, sessions)
	}
	data, _ := sessions["data"].([]any)
	if len(data) == 0 {
		t.Fatalf("the signed-in device is not listed: %v", sessions)
	}
	first, _ := data[0].(map[string]any)
	id, _ := first["id"].(string)
	parsed, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("the listed device has no id: %v", first)
	}
	w.deviceSessionID = parsed
}

func insertContractUser(t *testing.T, pool *pgxpool.Pool, id uuid.UUID, email, phone, role, hash string) {
	t.Helper()

	if _, err := pool.Exec(t.Context(),
		`INSERT INTO users (id, email, phone, password_hash, role, email_verified_at)
		 VALUES ($1, $2, $3, $4, $5, now())`,
		id, email, phone, hash, role); err != nil {
		t.Fatalf("inserting %s: %v", email, err)
	}
}

// seedAdministrator signs in a real owner, because an administrator session is a separate system
// with its own table and no token this package can mint.
func (w *contractWorld) seedAdministrator(
	t *testing.T, deps Deps, pool *pgxpool.Pool, client *redis.Client, prefix string, hasher *passwords.Hasher,
) {
	t.Helper()

	limiter, err := ratelimit.New(client, prefix, deps.Clock)
	if err != nil {
		t.Fatalf("building the rate limiter: %v", err)
	}
	auditor, err := admin.NewAuditor(deps.Clock)
	if err != nil {
		t.Fatalf("building the audit writer: %v", err)
	}
	creds, err := admin.NewCredentials(pool, hasher, limiter, deps.Clock, auditor)
	if err != nil {
		t.Fatalf("building the credentials service: %v", err)
	}

	// Owner rather than moderator: the fixtures drive every administrator route, and a role that
	// lacks a permission answers 403 — which is a real response and the wrong one to be checking
	// the success schema against.
	w.adminEmail = "contract-owner@example.com"
	created, err := creds.Create(t.Context(), admin.CreateCommand{
		Email: w.adminEmail, Name: "A Person", Password: contractPassword,
		Role: admin.RoleOwner, ActorID: uuid.New(),
	})
	if err != nil {
		t.Fatalf("creating the administrator: %v", err)
	}
	issued, _, err := creds.SignIn(t.Context(), admin.SignInCommand{
		Email: w.adminEmail, Password: contractPassword, ClientIP: "10.0.0.1",
	})
	if err != nil {
		t.Fatalf("signing the administrator in: %v", err)
	}
	w.adminID, w.adminToken = created.ID, issued.Token
}

// seedJob writes the job every parameterised path addresses, and a bid on it.
func (w *contractWorld) seedJob(t *testing.T) {
	t.Helper()

	w.jobID = uuid.New()
	if _, err := w.pool.Exec(t.Context(),
		`INSERT INTO jobs (id, customer_id) VALUES ($1, $2)`, w.jobID, w.customerID); err != nil {
		t.Fatalf("inserting the job: %v", err)
	}

	w.bidID = uuid.New()
	// pickup_at and deliver_by are set, and leaving them NULL is what the first version of this
	// fixture did. Every bid the API can create carries both — POST /v1/jobs/{id}/bids requires
	// them — so a row without them is a state no client can reach, and the handler renders a NULL
	// instant as "", which is not a date-time. That failure was the fixture's and not the
	// service's, but it is worth keeping the reason: `ck_bids_offer_has_timing` was removed in
	// 000501 and is Docs/09's SHIP-87a, so the database still permits the row this once wrote.
	if _, err := w.pool.Exec(t.Context(),
		`INSERT INTO bids (id, job_id, provider_id, pickup_at, deliver_by)
		 VALUES ($1, $2, $3, now() + interval '1 day', now() + interval '2 days')`,
		w.bidID, w.jobID, w.providerID); err != nil {
		t.Fatalf("inserting the bid: %v", err)
	}
}

func (w *contractWorld) seedDriverToken(t *testing.T, deps Deps) {
	t.Helper()

	keys, err := delivery.NewKeyset(deps.Config.Delivery.DriverTokenKeys, deps.Config.Delivery.DriverTokenActiveKID)
	if err != nil {
		t.Fatalf("building the driver keyset: %v", err)
	}
	issuer, err := delivery.NewDriverTokenIssuer(keys, deps.Config.Delivery.DriverTokenTTL, clock.System{})
	if err != nil {
		t.Fatalf("building the driver token issuer: %v", err)
	}
	token, err := issuer.Issue(w.jobID, uuid.New())
	if err != nil {
		t.Fatalf("issuing a driver token: %v", err)
	}
	w.driverToken = token.Value
}

// request builds one request against the seeded world, with the credential the case asks for.
func (w *contractWorld) request(t *testing.T, method, path string, auth contractAuth, body string) *http.Request {
	t.Helper()

	req := httptest.NewRequest(method, serverBase+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	switch auth {
	case asCustomer:
		req.Header.Set(httpx.HeaderAuthorization, "Bearer "+w.customerToken)
	case asProvider:
		req.Header.Set(httpx.HeaderAuthorization, "Bearer "+w.providerToken)
	case asAdmin:
		req.Header.Set(httpx.HeaderAuthorization, "Bearer "+w.adminToken)
	case asDriver:
		req.Header.Set(httpx.HeaderAuthorization, "Bearer "+w.driverToken)
	}

	// Every state-changing request inside /v1 is refused without a key (SHIP-15), and the
	// middleware wraps the group rather than the route, so this is not per-endpoint knowledge.
	if method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
		req.Header.Set(httpx.HeaderIdempotencyKey, uuid.New().String())
	}
	return req
}

// concretePath substitutes the ids the seeded world addresses into a route pattern.
func (w *contractWorld) concretePath(r Route) string {
	path := strings.TrimSuffix(r.fullPath(), "{$}")
	path = strings.ReplaceAll(path, "{bid_id}", w.bidID.String())
	path = strings.ReplaceAll(path, "{id}", w.jobID.String())
	return path
}

// contractCase is how one route is driven.
type contractCase struct {
	// auth is the credential to present.
	auth contractAuth

	// want is the status this fixture expects, asserted before the body is validated. See the
	// header: without it every case can pass on a declared 500.
	want int

	// body builds the request body. Nil sends none.
	body func(w *contractWorld) string

	// path overrides concretePath for the routes whose id is not the seeded job's.
	path func(w *contractWorld) string
}

// contractCases drives one route each, keyed "<METHOD> <full path>".
//
// The statuses were measured by driving the real router rather than predicted — a fixture whose
// expected status is a guess fails on the first run and gets "corrected" towards whatever the
// service does, which is how a contract test becomes a change detector.
var contractCases = map[string]contractCase{
	// Operational and public.
	"GET /health":                 {auth: asAnonymous, want: 200},
	"GET /v1/{$}":                 {auth: asAnonymous, want: 200},
	"GET /v1/app/minimum-version": {auth: asAnonymous, want: 200},
	"GET /v1/app/policy":          {auth: asAnonymous, want: 200},

	// Identity.
	"POST /v1/auth/login": {auth: asAnonymous, want: 200, body: func(w *contractWorld) string {
		return `{"email":"` + w.customerEmail + `","password":"` + contractPassword + `","device_label":"Contract test"}`
	}},
	"POST /v1/auth/logout":  {auth: asCustomer, want: 204},
	"GET /v1/auth/sessions": {auth: asCustomer, want: 200},
	"POST /v1/auth/register": {auth: asAnonymous, want: 201, body: func(*contractWorld) string {
		return `{"name":"A Person","email":"contract-new@example.com","phone":"+61400900009",` +
			`"password":"` + contractPassword + `","role":"customer"}`
	}},
	"POST /v1/auth/request-otp": {auth: asAnonymous, want: 202, body: func(w *contractWorld) string {
		return `{"phone":"+61400900001"}`
	}},
	"POST /v1/auth/resend-verify": {auth: asAnonymous, want: 202, body: func(w *contractWorld) string {
		return `{"email":"` + w.customerEmail + `"}`
	}},
	"POST /v1/account/deletion": {auth: asCustomer, want: 202},

	// Jobs.
	"GET /v1/jobs":      {auth: asCustomer, want: 200},
	"POST /v1/jobs":     {auth: asCustomer, want: 201, body: func(*contractWorld) string { return `{}` }},
	"GET /v1/jobs/{id}": {auth: asCustomer, want: 200},
	"PATCH /v1/jobs/{id}": {auth: asCustomer, want: 200, body: func(*contractWorld) string {
		return `{"goods_description":"Two pallets of tinned peaches"}`
	}},
	"POST /v1/jobs/{id}/cancel": {auth: asCustomer, want: 200, body: func(*contractWorld) string {
		return `{"reason":"No longer needed"}`
	}},
	"GET /v1/jobs/{id}/history":                {auth: asCustomer, want: 200},
	"GET /v1/jobs/{id}/bids/received":          {auth: asCustomer, want: 200},
	"GET /v1/jobs/{id}/bids/{bid_id}/history":  {auth: asCustomer, want: 200},
	"GET /v1/jobs/{id}/bids/{bid_id}/messages": {auth: asCustomer, want: 200},
	"GET /v1/jobs/{id}/delivery/detail":        {auth: asCustomer, want: 200},
	"GET /v1/jobs/{id}/delivery/milestones":    {auth: asCustomer, want: 200},
	"GET /v1/jobs/{id}/delivery/proof":         {auth: asCustomer, want: 200},

	// Fleet.
	"GET /v1/fleet/bids":      {auth: asProvider, want: 200},
	"GET /v1/fleet/jobs":      {auth: asProvider, want: 200},
	"GET /v1/fleet/jobs/{id}": {auth: asProvider, want: 200},
	"GET /v1/fleet/profile":   {auth: asProvider, want: 200},
	// display_name is required the first time a profile is written, so a PATCH naming only
	// operates_as is refused. Both fields together is the shape a first save actually takes.
	"PATCH /v1/fleet/profile": {auth: asProvider, want: 200, body: func(*contractWorld) string {
		return `{"display_name":"Peach Haulage","operates_as":"business"}`
	}},
	"GET /v1/fleet/vehicles": {auth: asProvider, want: 200},
	"POST /v1/fleet/vehicles": {auth: asProvider, want: 201, body: func(*contractWorld) string {
		return `{"registration":"ABC123","vehicle_type":"van"}`
	}},

	// Notifications.
	"GET /v1/notifications/preferences": {auth: asCustomer, want: 200},
	"PUT /v1/notifications/preferences": {auth: asCustomer, want: 200, body: func(*contractWorld) string {
		return `{"muted":[]}`
	}},
	"POST /v1/notifications/device-tokens": {auth: asCustomer, want: 201, body: func(*contractWorld) string {
		return `{"token":"a-device-token","platform":"android"}`
	}},
	"DELETE /v1/notifications/device-tokens/current": {auth: asCustomer, want: 204},

	// Profiles.
	"GET /v1/provider/verification":           {auth: asProvider, want: 200},
	"GET /v1/provider/verification/documents": {auth: asProvider, want: 200},
	"POST /v1/provider/verification/documents/uploads": {auth: asProvider, want: 200, body: func(*contractWorld) string {
		return `{"content_type":"image/jpeg","content_length":1048576}`
	}},

	// Administration.
	"POST /v1/admin/sessions": {auth: asAnonymous, want: 200, body: func(w *contractWorld) string {
		return `{"email":"` + w.adminEmail + `","password":"` + contractPassword + `"}`
	}},
	"DELETE /v1/admin/sessions/current": {auth: asAdmin, want: 204},
	"GET /v1/admin/me":                  {auth: asAdmin, want: 200},
	"GET /v1/admin/audit":               {auth: asAdmin, want: 200},
	"GET /v1/admin/jobs":                {auth: asAdmin, want: 200},
	"GET /v1/admin/jobs/{id}":           {auth: asAdmin, want: 200},
	"GET /v1/admin/disputes":            {auth: asAdmin, want: 200},
	"GET /v1/admin/users":               {auth: asAdmin, want: 200},
	"GET /v1/admin/suspensions":         {auth: asAdmin, want: 200},
	// state is a required query parameter, so the bare path answers 422 rather than an empty page.
	"GET /v1/admin/verifications": {auth: asAdmin, want: 200,
		path: func(*contractWorld) string { return "/v1/admin/verifications?state=Pending" }},
	"GET /v1/admin/moderation/cancellations":      {auth: asAdmin, want: 200},
	"GET /v1/admin/moderation/exceptions":         {auth: asAdmin, want: 200},
	"GET /v1/admin/moderation/expiring-documents": {auth: asAdmin, want: 200},
	"POST /v1/admin/administrators": {auth: asAdmin, want: 201, body: func(*contractWorld) string {
		return `{"email":"contract-second@example.com","name":"Another Person",` +
			`"password":"` + contractPassword + `","role":"support"}`
	}},
	"POST /v1/admin/notes": {auth: asAdmin, want: 201, body: func(w *contractWorld) string {
		return `{"subject_type":"job","subject_id":"` + w.jobID.String() + `","body":"A note."}`
	}},
	"POST /v1/admin/jobs/{id}/unpublish": {auth: asAdmin, want: 200, body: func(*contractWorld) string {
		return `{"reason":"Prohibited goods"}`
	}},
	"POST /v1/admin/users/{id}/standing": {auth: asAdmin, want: 200,
		path: func(w *contractWorld) string { return "/v1/admin/users/" + w.customerID.String() + "/standing" },
		body: func(*contractWorld) string {
			// At least ten characters: the domain refuses a reason too short to be a record.
			return `{"standing":"restricted","reason":"Reviewed after a customer report"}`
		}},
	"GET /v1/admin/notes": {auth: asAdmin, want: 200,
		path: func(w *contractWorld) string {
			return "/v1/admin/notes?subject_type=job&subject_id=" + w.jobID.String()
		}},

	// The vehicle the seeded world owns, rather than the job id concretePath would substitute.
	"GET /v1/fleet/vehicles/{id}": {auth: asProvider, want: 200,
		path: func(w *contractWorld) string { return "/v1/fleet/vehicles/" + w.vehicleID.String() }},
	"PATCH /v1/fleet/vehicles/{id}": {auth: asProvider, want: 200,
		path: func(w *contractWorld) string { return "/v1/fleet/vehicles/" + w.vehicleID.String() },
		body: func(*contractWorld) string { return `{"make":"Isuzu"}` }},
	"POST /v1/fleet/vehicles/{id}/deactivate": {auth: asProvider, want: 200,
		path: func(w *contractWorld) string {
			return "/v1/fleet/vehicles/" + w.vehicleID.String() + "/deactivate"
		}},
	"POST /v1/fleet/vehicles/{id}/reactivate": {auth: asProvider, want: 200,
		path: func(w *contractWorld) string {
			return "/v1/fleet/vehicles/" + w.retiredVehicleID.String() + "/reactivate"
		}},

	// A refresh token and a device-session id, both from the sign-in the world performs.
	"POST /v1/auth/refresh": {auth: asAnonymous, want: 200, body: func(w *contractWorld) string {
		return `{"refresh_token":"` + w.refreshToken + `"}`
	}},
	"DELETE /v1/auth/sessions/{id}": {auth: asCustomer, want: 204,
		path: func(w *contractWorld) string { return "/v1/auth/sessions/" + w.deviceSessionID.String() }},

	"POST /v1/jobs/{id}/bids/{bid_id}/messages": {auth: asCustomer, want: 201, body: func(*contractWorld) string {
		return `{"body":"When can you collect?"}`
	}},

	"POST /v1/admin/users/{id}/suspension": {auth: asAdmin, want: 202,
		path: func(w *contractWorld) string { return "/v1/admin/users/" + w.customerID.String() + "/suspension" },
		body: func(*contractWorld) string { return `{"reason":"Under review"}` }},
}

// contractUnreached names every route no fixture drives, with the reason.
//
// **This is not the old skip list.** A route here is named and counted; a route in neither map
// fails the test. The third clause of SHIP-17b's *Done when* is that whatever is still unreached is
// named in the output rather than counted, and an exemption that has to be written by hand is the
// only form of that which cannot rot silently.
var contractUnreached = map[string]string{
	// # The seeded job is a Draft, and these need it further along
	//
	// Every one of these lifts the same way: seed the job at the status the operation requires and
	// write the fixture. That is a job-state builder rather than a line each, which is why it is
	// one decision recorded here rather than nine.
	"POST /v1/jobs/{id}/bids":          "needs the job Open to providers; the seeded job is a Draft",
	"POST /v1/jobs/{id}/award":         "needs an Open job carrying a Submitted bid to award",
	"POST /v1/jobs/{id}/extend":        "needs an Open job with an expiry; a Draft has no offer window",
	"POST /v1/jobs/{id}/driver":        "needs the job Awarded, which needs an accepted bid",
	"POST /v1/jobs/{id}/driver/link":   "needs a driver already assigned to the job",
	"POST /v1/jobs/{id}/milestones":    "needs a delivery under way and a driver on it",
	"POST /v1/jobs/{id}/proof-uploads": "needs a delivery under way; proof has nothing to attach to",
	"POST /v1/jobs/{id}/disputes":      "needs a delivery far enough along to be disputed",

	// The seeded bid is a Draft the fixture inserts. These three act on an offer a provider has
	// actually submitted, which is a different row and a different set of guards.
	"PATCH /v1/jobs/{id}/bids/{bid_id}":         "needs a Submitted bid; the seeded bid is a Draft",
	"POST /v1/jobs/{id}/bids/{bid_id}/counter":  "needs a Submitted bid to counter",
	"POST /v1/jobs/{id}/bids/{bid_id}/withdraw": "needs a Submitted bid to withdraw",

	// # The driver surface needs an assignment
	//
	// The world mints a driver token for the seeded job, which is why these answer 404 rather than
	// 401: the token is accepted and there is no delivery behind it. One assignment row lifts all
	// four together.
	"GET /v1/driver/jobs/{id}":                "the driver token is valid; no delivery exists for the job",
	"GET /v1/driver/jobs/{id}/milestones":     "same assignment as above",
	"POST /v1/driver/jobs/{id}/milestones":    "same assignment as above",
	"POST /v1/driver/jobs/{id}/proof-uploads": "same assignment as above",

	// # Administration acting on a subject that has to exist first
	"GET /v1/admin/disputes/{id}":                "needs a dispute, which needs a delivery to dispute",
	"POST /v1/admin/disputes/{id}/resolution":    "needs a dispute in an open state",
	"POST /v1/admin/suspensions/{id}/approval":   "needs a suspension requested by a *different* administrator — the two-person rule refuses the requester's own approval, so the world needs a second signed-in owner",
	"POST /v1/admin/verifications/{id}/decision": "needs the provider's verification id, which no response the world already drives returns",
	"GET /v1/admin/verifications/{id}/documents": "needs a verification carrying a stored document — see the object-store note below",

	// # One-time secrets the platform sends out of band
	//
	// Both are reachable, and reaching them means reading back what the platform generated rather
	// than sending a plausible value. Neither is hard; both are a fixture that inspects a table the
	// endpoint is supposed to be the only reader of.
	"POST /v1/auth/verify-email": "needs the token the verification email carried",
	"POST /v1/auth/verify-phone": "needs the OTP the SMS carried",

	// # The one that needs bytes in the object store
	//
	// The world asks for a signed upload URL and gets one, so the key is real. Recording the
	// document then inspects the *stored object* — Docs/06 §5.2 keeps the platform out of the
	// upload path, so the only moment it can learn the size and media type is when it asks the
	// store. Nothing has been uploaded and the bucket testStorageConfig names does not exist, so
	// the lookup fails and the handler answers 500.
	//
	// **That 500 is worth a second look and is not this ticket's to take.** A key that is well
	// formed and names no object is a client mistake rather than a platform fault, and
	// internal/profiles has ErrDocumentRejected for exactly the neighbouring case.
	"POST /v1/provider/verification/documents": "needs an object actually in the store; the fixture uploads no bytes and the bucket is not created",
}

// TestEveryRouteIsDrivenOrNamed is the gate SHIP-17b adds: no route may be silently skipped.
//
// The failure it exists to catch is a new endpoint arriving with no fixture. Under exercisable()
// that endpoint joined an 82-line log message nobody reads; here it fails the build until somebody
// either drives it or writes down why they did not.
func TestEveryRouteIsDrivenOrNamed(t *testing.T) {
	served := map[string]bool{}

	for _, r := range routes() {
		key := r.Method + " " + r.fullPath()
		served[key] = true

		_, driven := contractCases[key]
		_, named := contractUnreached[key]

		switch {
		case driven && named:
			t.Errorf("%s is both driven and named unreached; delete one", key)
		case !driven && !named:
			t.Errorf("%s has no fixture and no stated reason.\n"+
				"  Add a contractCases entry that drives it, or a contractUnreached entry saying why not.\n"+
				"  A route that is in neither is one the contract check does not cover and nobody decided that.", key)
		}
	}

	// A stale exemption is how coverage rots: the route goes, the excuse stays, and the count of
	// unreached routes stops meaning anything.
	for key, reason := range contractUnreached {
		if !served[key] {
			t.Errorf("contractUnreached names %q, which no route serves — %s\n"+
				"  The route was renamed or removed; delete the exemption.", key, reason)
		}
	}
	for key := range contractCases {
		if !served[key] {
			t.Errorf("contractCases drives %q, which no route serves; delete it", key)
		}
	}

	// Named, not counted. SHIP-17a's version of this printed a bare tally and a list nobody had to
	// justify; every line below carries the state that would lift it, so the gap is a work item
	// rather than a number that drifts.
	var named []string
	for key, reason := range contractUnreached {
		named = append(named, key+" — "+reason)
	}
	sort.Strings(named)

	t.Logf("contract coverage: %d of %d routes driven, %d named unreached\n  %s",
		len(contractCases), len(served), len(named), strings.Join(named, "\n  "))
}

// TestResponsesMatchTheContract drives every route a fixture covers and validates what comes back.
//
// This is SHIP-17a's criterion widened to the whole surface by SHIP-17b. The status assertion in
// front of the schema check is what keeps it honest — see this file's header on the declared 500.
func TestResponsesMatchTheContract(t *testing.T) {
	doc := loadContract(t)

	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("building a router from the contract: %v", err)
	}

	for _, r := range routes() {
		key := r.Method + " " + r.fullPath()
		c, ok := contractCases[key]
		if !ok {
			continue // TestEveryRouteIsDrivenOrNamed owns the accounting.
		}

		t.Run(key, func(t *testing.T) {
			w := newContractWorld(t)

			path := w.concretePath(r)
			if c.path != nil {
				path = c.path(w)
			}
			body := ""
			if c.body != nil {
				body = c.body(w)
			}

			req := w.request(t, r.Method, path, c.auth, body)
			rec := httptest.NewRecorder()
			w.router.ServeHTTP(rec, req)

			// Before the schema check, not after: every operation declares a 500, so a fixture
			// that has stopped reaching its handler would otherwise validate an error envelope
			// against a declared response and pass.
			if rec.Code != c.want {
				t.Fatalf("status = %d, want %d as %s\n  body: %s\n"+
					"  Either the fixture has gone stale or the endpoint changed. Do not simply\n"+
					"  move `want` to whatever came back — that turns this into a change detector.",
					rec.Code, c.want, c.auth, rec.Body)
			}

			validateAgainstContract(t, router, req, rec)
		})
	}
}

// TestRequestBodiesAreClosed is the half of the contract that had no check at all.
//
// TestResponsesMatchTheContract checks what the service *writes*. Nothing checked what it is
// documented to *accept*, and the two drift independently: SHIP-159 added `expires_at` to the Go
// request struct and to admin.yaml and not to profiles.yaml, so for a wave the service accepted a
// field the published contract forbade.
//
// # It is two assertions, and the first version of it was one and was vacuous
//
// The obvious check is to send a body carrying an undeclared field and require the contract to
// refuse it. **That passes for the wrong reason on every schema with a required field**: a body of
// nothing but the undeclared field is also missing everything required, so it is refused whether
// additionalProperties is false or true. A mutation opening the login schema survived it, which is
// how this was found rather than reasoned about.
//
// So there are two:
//
//  1. **Structural** — every request schema states `additionalProperties: false` explicitly. This
//     cannot pass vacuously because it reads the declaration rather than a consequence of it.
//  2. **Behavioural** — for every operation a fixture supplies a valid body for, that body *plus*
//     an undeclared field must be refused. Valid-but-for-the-extra-field is the only probe that
//     isolates the property, and it is what proves the declaration is enforced rather than merely
//     written.
//
// The service's own half is already true and is why this matters: httpx.DecodeJSON sets
// DisallowUnknownFields, so a handler refuses the same body with 400. A contract that permitted it
// would be publishing a promise the service does not keep.
func TestRequestBodiesAreClosed(t *testing.T) {
	doc := loadContract(t)

	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("building a router from the contract: %v", err)
	}

	// The field no schema declares. Deliberately not a plausible name: a schema that happened to
	// declare it would make this test pass while asserting nothing.
	const undeclared = `"ship_17b_undeclared_field":"x"`

	type operation struct {
		method, path string
	}
	declared := map[operation]bool{}

	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			if op.RequestBody == nil || op.RequestBody.Value == nil {
				continue
			}
			media := op.RequestBody.Value.Content.Get("application/json")
			if media == nil || media.Schema == nil || media.Schema.Value == nil {
				continue
			}
			declared[operation{method, path}] = true

			t.Run("declares/"+method+" "+path, func(t *testing.T) {
				has := media.Schema.Value.AdditionalProperties.Has
				if has == nil || *has {
					t.Errorf("the request schema does not state additionalProperties: false.\n" +
						"  A client can then send any field it likes and the contract says nothing,\n" +
						"  while httpx.DecodeJSON refuses the same body with 400 — so the published\n" +
						"  document and the service disagree about what is legal.")
				}
			})
		}
	}

	if len(declared) == 0 {
		t.Fatal("no operation declares a JSON request body; this test is asserting nothing")
	}

	// The behavioural half. One world, because the bodies only read seeded ids and none of this
	// reaches a handler.
	w := newContractWorld(t)

	probed := 0
	for key, c := range contractCases {
		if c.body == nil {
			continue
		}
		method, path, ok := strings.Cut(key, " ")
		if !ok {
			t.Fatalf("malformed fixture key %q", key)
		}
		if !declared[operation{method, path}] {
			continue // no JSON request body in the contract; nothing to close.
		}

		valid := c.body(w)
		if !strings.HasPrefix(strings.TrimSpace(valid), "{") || strings.TrimSpace(valid) == "{}" {
			continue // nothing to append an extra field to.
		}

		probed++
		t.Run("refuses/"+key, func(t *testing.T) {
			// The valid body with one undeclared field spliced in, so refusal can only be about
			// that field.
			body := strings.TrimSpace(valid)
			body = body[:len(body)-1] + "," + undeclared + "}"

			concrete := strings.ReplaceAll(path, "{id}", w.jobID.String())
			concrete = strings.ReplaceAll(concrete, "{bid_id}", w.bidID.String())

			req := httptest.NewRequest(method, serverBase+concrete, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(httpx.HeaderIdempotencyKey, uuid.New().String())

			route, pathParams, err := router.FindRoute(req)
			if err != nil {
				t.Fatalf("the contract has no operation for %s %s: %v", method, concrete, err)
			}

			err = openapi3filter.ValidateRequest(req.Context(), &openapi3filter.RequestValidationInput{
				Request:    req,
				PathParams: pathParams,
				Route:      route,
				Options: &openapi3filter.Options{
					// The credential is the service's business; the shape of the body is this
					// test's.
					AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
				},
			})
			if err == nil {
				t.Errorf("the contract accepts a valid body with an undeclared field added.\n"+
					"  body: %s\n"+
					"  additionalProperties: false is declared and is not being enforced.", body)
			}
		})
	}

	if probed == 0 {
		t.Fatal("no fixture body reached the behavioural half; it is asserting nothing")
	}
	t.Logf("request schemas: %d declared closed, %d proved closed against a valid body",
		len(declared), probed)
}
