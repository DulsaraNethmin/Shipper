package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/admin"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/delivery"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/idempotency"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
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

// contractCustomerPhone is the customer's number, named because two fixtures have to agree on it
// (SHIP-17d).
//
// `Service.VerifyPhone` reads the account by number and then refuses a code whose row names a
// different one, so the value in `users.phone` and the value in `phone_otps.phone` are the same
// fact twice. As two literals they were one careless edit from disagreeing, and the endpoint folds
// every such failure into one `identity_otp_invalid`.
const contractCustomerPhone = "+61400900001"

// The two out-of-band secrets, chosen by the fixture rather than read back from the platform
// (SHIP-17d).
//
// Both endpoints are reached by *being* the person who received the message, and the platform keeps
// nothing either could be recovered from — `email_verification_tokens.token_hash` is a digest and
// `phone_otps.code_hash` is argon2id. So the fixture picks the secret, stores it the way the
// platform would have, and sends it back. Named here because the seed writes one half and the
// request body sends the other, and the endpoint folds every mismatch into a single opaque error.
const (
	contractEmailToken = "contract-email-verification-token"

	// Numeric: Service.VerifyPhone refuses anything else before it hashes, so that an endpoint
	// needing no account cannot be made to allocate 64 MiB a call.
	contractPhoneCode = "314159"
)

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
	// disputeID is set by raiseDispute, for the two administrator routes that address one.
	disputeID uuid.UUID
	// suspensionReviewID is set by suspensionAwaitingApproval, which also leaves a *second*
	// administrator holding adminToken.
	suspensionReviewID uuid.UUID

	// store is the object store both storage adapters talk to, and it starts empty.
	store *contractStore

	// hasher is the world's own argon2id, kept because one fixture has to write a credential the
	// platform will later verify — see seedPhoneCode.
	hasher *passwords.Hasher
}

// contractStore is an object store with nothing in it, which is what the world needs and is also
// the one fixture that could not be written as a setup hook (SHIP-17d).
//
// # Why a fake store exists at all, when four of the five other adapters are real
//
// `POST /v1/provider/verification/documents` is the request that turns an object in a bucket into
// one of Docs/04 §3's four documents, and Docs/06 §5.2 keeps the platform out of the upload path —
// so the only moment it can learn what was actually stored is when it asks. `profiles.Documents`
// asks, through `DocumentObjects.Stored`, and that is a real HEAD over the network. Every other
// storage call on the served surface signs a URL and returns it: an HMAC over a string, which needs
// no store to be reachable and is why the other verification routes were driven long before this
// one.
//
// So this is not "the store is slow, stub it". It is the one place where a route's success depends
// on a fact about the world that only the store holds.
//
// # It must be wired before newRouter, and that is what made this route structural
//
// `cmd/api` builds its signers inside attach, from `Deps.Config` — `profileDocuments(d)` reads
// `d.Config.Storage.Endpoint` and hands it to `storage.NewS3`. A `setup:` hook runs against a
// router that has already been constructed, so an endpoint rewritten there is a value the signer
// read a moment ago and will never read again. Hence the constructor rather than a hook, and hence
// the one route out of five that could not simply be added to contractCases.
//
// # Three headers, and every one of them is load-bearing
//
// `profiles.stored` refuses an object whose media type is not in STORAGE_ACCEPTED_CONTENT_TYPES,
// whose length is not positive, or whose entity tag is blank — and `storage.S3.Stored` refuses a
// response with no length at all before profiles ever sees it, because Go reports an absent
// Content-Length as -1 and a negative number passes every "under the limit" comparison there is.
// Omitting any of the three produces the same opaque 500, which is exactly the answer this route
// gave before it was driven.
type contractStore struct {
	// bucket is the path-style first segment, stripped before a key is looked up.
	bucket string

	mu      sync.Mutex
	objects map[string]contractObject
}

// contractObject is one object as a store would report it on a HEAD.
type contractObject struct {
	contentType   string
	contentLength int64
	etag          string
}

func newContractStore(bucket string) *contractStore {
	return &contractStore{bucket: bucket, objects: map[string]contractObject{}}
}

// put makes key exist. Safe to call from a setup hook, which runs after the server is serving.
func (s *contractStore) put(key string, object contractObject) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = object
}

// ServeHTTP answers HEAD the way an S3-compatible store does, and 404 for everything it does not
// hold.
//
// **404 rather than a connection error, and the difference is not cosmetic.** `S3.Stored` reads 404
// as "no object" — an answer — and anything else as a fault, so a store that refuses the connection
// makes every unseeded key a 500. That is what `http://localhost:9000` did here, and it is why the
// note this fixture replaces recorded a 500 where the domain has ErrDocumentNotUploaded for exactly
// the case.
//
// The signature is not checked. What is under test is the contract the platform publishes, and
// SigV4 has `TestTheAWSExampleSignsToThePublishedSignature` in the adapter's own package.
func (s *contractStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/"+s.bucket+"/")

	s.mu.Lock()
	object, found := s.objects[key]
	s.mu.Unlock()

	if !found {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	// Set explicitly, all three. Go sends no Content-Length of its own for a HEAD — there is no
	// body to measure — so this header is the only reason the adapter sees a length rather than
	// the -1 it refuses.
	w.Header().Set("Content-Type", object.contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(object.contentLength, 10))
	w.Header().Set("ETag", `"`+object.etag+`"`)
	w.WriteHeader(http.StatusOK)
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

	w := &contractWorld{pool: pool, store: newContractStore(deps.Config.Storage.Bucket)}

	// Before newRouter, not after: the signers are built during attach and read this value once.
	// See contractStore's own note — this ordering is the whole reason the document-recording
	// route needed a change here rather than a fixture of its own.
	objects := httptest.NewServer(w.store)
	t.Cleanup(objects.Close)
	deps.Config.Storage.Endpoint = objects.URL

	w.router = newRouter(deps, idempotency.NewMemoryStore(), testLimiter(), testAuthenticator(),
		testDriverGuard(), adminGuard)

	hasher, err := passwords.NewHasher(passwords.Argon2Profile{
		MemoryKiB:   deps.Config.Passwords.Argon2.MemoryKiB,
		Iterations:  deps.Config.Passwords.Argon2.Iterations,
		Parallelism: deps.Config.Passwords.Argon2.Parallelism,
	})
	if err != nil {
		t.Fatalf("building the hasher: %v", err)
	}
	w.hasher = hasher

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
	insertContractUser(t, pool, customerID, w.customerEmail, contractCustomerPhone, "customer", hashed)
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

// storeVerificationObject makes the key the platform signed actually hold something (SHIP-17d).
//
// The type and the length are the ones seedVerificationUpload asked permission for, which is not
// tidiness: `profiles.stored` judges STORAGE_ACCEPTED_CONTENT_TYPES and STORAGE_MAX_UPLOAD_BYTES
// against **what the store reports**, deliberately, so that the limits bind the object that exists
// rather than only the request that asked to create one. A fixture reporting something else would
// be testing the refusal path while claiming to test the success one.
func (w *contractWorld) storeVerificationObject() {
	w.store.put(w.verificationObjectKey, contractObject{
		contentType:   "image/jpeg",
		contentLength: 1048576,
		etag:          "d41d8cd98f00b204e9800998ecf8427e",
	})
}

// submitVerificationDocument records that object as one of Docs/04 §3's four documents.
//
// Driven through the endpoint rather than INSERTed, for the reason `drive` gives: a hand-written
// INSERT names columns, and `provider_verification_documents` is append-only with a no-update
// trigger, so a fixture that wrote it directly would be the only writer in the repository that
// bypasses the one check the platform makes about the bytes.
func (w *contractWorld) submitVerificationDocument(t *testing.T) {
	t.Helper()

	w.storeVerificationObject()

	status, body := w.drive(t, http.MethodPost, "/v1/provider/verification/documents", asProvider,
		`{"kind":"licence","object_key":"`+w.verificationObjectKey+`"}`)
	if status != http.StatusCreated {
		t.Fatalf("recording the verification document: status %d, body %v", status, body)
	}
}

// suspensionAwaitingApproval files a permanent suspension and leaves a **different** administrator
// signed in to approve it (SHIP-17d).
//
// # The second administrator is the control, not a detail of the fixture
//
// Docs/04 §9 asks for two-person review, and SHIP-166 enforces it in two places: the service refuses
// `approved_by = requested_by`, and `ck_suspension_reviews_two_people` refuses it again in the
// database. So a fixture that filed and approved on one token would answer `admin_same_administrator`
// — a real response, and the wrong one to be checking the success schema against.
//
// # Moderator rather than support, and the difference is one permission
//
// The approval takes `users.restrict`, which [RoleModerator] and [RoleOwner] hold and `support` does
// not. The neighbouring `POST /v1/admin/administrators` case creates a `support` administrator, and
// reusing that shape here answers 403 — which looks enough like a near miss to tempt somebody into
// moving `want` rather than fixing the role.
//
// {id} is the review's identifier and not the user's, which is the other thing worth stating: the
// request answers with the review, and the approval addresses it.
func (w *contractWorld) suspensionAwaitingApproval(t *testing.T) {
	t.Helper()

	status, body := w.drive(t, http.MethodPost,
		"/v1/admin/users/"+w.customerID.String()+"/suspension", asAdmin,
		`{"reason":"Three unresolved safety reports in a fortnight; see the exception queue."}`)
	if status != http.StatusAccepted {
		t.Fatalf("requesting the suspension: status %d, body %v", status, body)
	}
	reviewID, _ := body["id"].(string)
	parsed, err := uuid.Parse(reviewID)
	if err != nil {
		t.Fatalf("the review response carries no id: %v", body)
	}
	w.suspensionReviewID = parsed

	const approver = "contract-approver@example.com"
	if status, body = w.drive(t, http.MethodPost, "/v1/admin/administrators", asAdmin,
		`{"email":"`+approver+`","name":"A Second Person",`+
			`"password":"`+contractPassword+`","role":"moderator"}`); status != http.StatusCreated {
		t.Fatalf("creating the second administrator: status %d, body %v", status, body)
	}

	if status, body = w.drive(t, http.MethodPost, "/v1/admin/sessions", asAnonymous,
		`{"email":"`+approver+`","password":"`+contractPassword+`"}`); status != http.StatusOK {
		t.Fatalf("signing the second administrator in: status %d, body %v", status, body)
	}
	token, _ := body["token"].(string)
	if token == "" {
		t.Fatalf("the second administrator's session carries no token: %v", body)
	}

	// Every later asAdmin request in this world is the approver. Reassigned rather than carried
	// beside the first, because the case that follows drives one request and it is that one.
	w.adminToken = token
}

// seedEmailVerificationToken writes a live token for the customer and returns the secret half
// (SHIP-17d).
//
// # The fixture chooses the token, because the platform keeps nothing it could be read back from
//
// `email_verification_tokens.token_hash` is the SHA-256 of the token, hex encoded — plain and
// unkeyed, by `000101`'s argument that 256 bits from crypto/rand has nothing for a work factor to
// protect. The token itself "exists only in the email". So there is no table to inspect and no
// message to intercept: what a fixture can do is pick a token, store its digest, and let the
// endpoint do the same arithmetic in the other direction.
//
// **The row's email is citext and must equal `users.email`.** `Service.VerifyEmail` compares them —
// a token proves control of the address it was sent to, not of whatever address the account holds
// now — and every failure in this endpoint folds into one `identity_verification_token_invalid`, so
// a mismatch here gives no clue which half is wrong.
func (w *contractWorld) seedEmailVerificationToken(t *testing.T) {
	t.Helper()

	digest := sha256.Sum256([]byte(contractEmailToken))

	if _, err := w.pool.Exec(t.Context(),
		`INSERT INTO email_verification_tokens (id, user_id, email, token_hash, expires_at)
		 VALUES ($1, $2, $3, $4, now() + interval '1 hour')`,
		uuid.New(), w.customerID, w.customerEmail, hex.EncodeToString(digest[:])); err != nil {
		t.Fatalf("seeding an email verification token: %v", err)
	}
}

// seedPhoneCode writes a live one-time code for the customer and returns it (SHIP-17d).
//
// # This one cannot be read back even in principle, which is why the world keeps its hasher
//
// `phone_otps.code_hash` is an argon2id PHC string, exactly as `users.password_hash` is — `000102`
// makes the opposite choice from the email token and gives the reason: six digits is 10^6, which a
// laptop inverts in under a second if it is only SHA-256. A fixture written on the assumption that
// the code is recoverable is rewritten from scratch, so it is stated here.
//
// The code is hashed with the world's own hasher at the world's own profile, and the row is written
// beside the number it was "sent" to — `Service.VerifyPhone` refuses a code whose row names a
// different one.
//
// **Nothing here drives POST /v1/auth/request-otp first, and it must not.** `uq_phone_otps_live`
// allows one live code per account and a request supersedes the outstanding one, so asking for a
// code after seeding this row retires it — and the endpoint then answers the same undiagnosable
// `identity_otp_invalid` it answers for a wrong guess.
func (w *contractWorld) seedPhoneCode(t *testing.T) {
	t.Helper()

	hashed, err := w.hasher.Hash(contractPhoneCode)
	if err != nil {
		t.Fatalf("hashing a one-time code: %v", err)
	}
	if _, err := w.pool.Exec(t.Context(),
		`INSERT INTO phone_otps (id, user_id, phone, code_hash, expires_at)
		 VALUES ($1, $2, $3, $4, now() + interval '10 minutes')`,
		uuid.New(), w.customerID, contractCustomerPhone, hashed); err != nil {
		t.Fatalf("seeding a one-time code: %v", err)
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

// # The job-state builder (SHIP-17c)
//
// SHIP-17b named 23 routes it could not drive and grouped most of them under one sentence: every
// one lifts by seeding the job at the status the operation requires, which is "a job-state builder
// rather than a line each". This is that builder.
//
// ## Why it is not an UPDATE
//
// The obvious fixture — `UPDATE jobs SET status = 'Open'` — does not work, and its failing is the
// point rather than an inconvenience. 000402 refuses any status change unless a job_status_history
// row written in the *same transaction* already describes it, so a direct write does not quietly
// bypass CLAUDE.md's invariant; it raises. The fixture therefore has to do what the platform does:
// call jobs.Service.Transition inside a transaction.
//
// That makes the builder worth more than the routes it unlocks. A fixture that seeded status by
// UPDATE would still compile the day somebody weakened the trigger; this one stops working.
//
// ## Why the ladder is written out rather than searched
//
// Docs/02 §2's table is a graph, and a shortest path through it is not the path a job takes. From
// `Awarded` the table permits both `Driver assigned` and `En route to pickup`, so a search asked
// for a delivery under way would skip the assignment — and the four driver routes need exactly
// that row to exist. The ladder below is the happy path from Docs/02 §2, named once, and a case
// asks for the rung it needs.
var contractJobLadder = []jobs.Status{
	jobs.StatusDraft,
	jobs.StatusOpen,
	jobs.StatusAwarded,
	jobs.StatusDriverAssigned,
	jobs.StatusEnRouteToPickup,
	jobs.StatusPickedUp,
	jobs.StatusInTransit,
	jobs.StatusDelivered,
}

// contractServiceState is the state the job's pickup sits in and the provider covers. One constant
// because the two have to agree — fleet's predicate matches `a.area = j.pickup_state` exactly, and
// two literals that drifted apart would make the provider ineligible for a reason no failure names.
const contractServiceState = "NSW"

// makeProviderEligible satisfies fleet's four-part bidding predicate for the seeded provider.
//
// **The job reaching Open is necessary and is not sufficient**, which is what the first version of
// this fixture got wrong: `POST /v1/jobs/{id}/bids` answered 404 on an Open job, because eligibility
// is `fleet`'s answer rather than `bidding`'s and it asks for four things (internal/fleet's
// `eligible`). Clause 1 is the status, which advanceJobTo supplies. The other three are here:
//
//   - **2a/2b — verified, in both places.** A `provider_verifications` row at `Verified` *and* an
//     account that is active with both contacts confirmed. The seeded user has an email timestamp
//     and no phone one, so the account half fails on its own.
//   - **3 — service area.** The provider must cover the job's pickup, and the seeded job has no
//     address at all. Both sides are set here rather than in seedJob, so the 63 routes that were
//     already driven keep the world they were measured against.
//   - **4 — vehicle capability.** Already true: seedVehicle leaves one vehicle in service, and a
//     vehicle with no stated dimensions clears a job with none.
//
// Written as SQL rather than driven through the API because two of the three are not the caller's
// to set. A provider cannot verify themselves — that is an administrator's decision (SHIP-81a) —
// and the job's address belongs to the customer. A fixture that asked the API for these would be
// asking it to permit what it correctly refuses.
func (w *contractWorld) makeProviderEligible(t *testing.T) {
	t.Helper()

	exec := func(what, q string, args ...any) {
		if _, err := w.pool.Exec(t.Context(), q, args...); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}

	exec("confirming the provider's phone",
		`UPDATE users SET phone_verified_at = now() WHERE id = $1`, w.providerID)
	// Through the guarded function, not an INSERT or an UPDATE, and for the reason advanceJobTo
	// goes through jobs.Service.Transition: 000200 guards this state exactly as 000402 guards a
	// job's. The record already exists — a trigger writes it when a provider account is created —
	// so what is needed is the move, and provider_verification_decide is the only thing that can
	// make it. It records the decision with its actor and reason in the same transaction.
	exec("verifying the provider",
		`SELECT provider_verification_decide($1, 'Verified', 'admin', $2, $3)`,
		w.providerID, w.adminID, "SHIP-17c contract fixture")

	// A non-status column, so 000402's guard passes it straight through — its own comment says so.
	exec("giving the job a pickup address",
		`UPDATE jobs SET pickup_state = $2 WHERE id = $1`, w.jobID, contractServiceState)
	exec("giving the provider a service area",
		`INSERT INTO provider_service_areas (id, provider_id, scope, area) VALUES ($1, $2, 'state', $3)`,
		uuid.New(), w.providerID, contractServiceState)
}

// openToBidding is the pair every bidding route needs: the job published, and the provider allowed
// to see it. Named once because no caller wants one without the other.
func (w *contractWorld) openToBidding(t *testing.T) {
	t.Helper()
	w.advanceJobTo(t, jobs.StatusOpen)
	w.makeProviderEligible(t)
}

// submitBid places a real offer and rebinds bidID to it.
//
// The seeded bid is a Draft this file INSERTs, and three routes act only on an offer a provider
// actually submitted. Placing one through POST /v1/jobs/{id}/bids is both shorter than reproducing
// the domain's own writes and the version that stays true when they change — the same argument
// seedVehicle makes.
func (w *contractWorld) submitBid(t *testing.T) {
	t.Helper()

	status, body := w.drive(t, http.MethodPost, "/v1/jobs/"+w.jobID.String()+"/bids", asProvider,
		contractOffer(45000))
	if status != http.StatusCreated {
		t.Fatalf("placing an offer: status %d, body %v", status, body)
	}
	id, _ := body["id"].(string)
	parsed, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("the placed offer has no id: %v", body)
	}
	w.bidID = parsed
}

// awardBid takes the job to Awarded the way a customer does — by accepting a live offer.
//
// Not advanceJobTo(StatusAwarded), and the difference is the point. The ladder moves the status and
// nothing else; an award also writes the accepted bid that the one-accepted-bid index holds, and
// every route past this rung reads that row rather than the status. A job moved to Awarded with no
// accepted bid is a state the platform cannot produce.
func (w *contractWorld) awardBid(t *testing.T) {
	t.Helper()

	w.openToBidding(t)
	w.submitBid(t)

	status, body := w.drive(t, http.MethodPost, "/v1/jobs/"+w.jobID.String()+"/award", asCustomer,
		`{"bid_id":"`+w.bidID.String()+`"}`)
	if status != http.StatusOK {
		t.Fatalf("awarding the offer: status %d, body %v", status, body)
	}
}

// assignDriver carries the job to Driver assigned, which is the rung the whole driver surface needs.
//
// The four /v1/driver routes answer 404 on the seeded world with a *valid* token — the token is
// scoped to the job and there is no assignment behind it. One row lifts all four, and asking the
// endpoint for it is what keeps the fixture honest about how a driver comes to exist.
func (w *contractWorld) assignDriver(t *testing.T) {
	t.Helper()

	w.awardBid(t)

	status, body := w.drive(t, http.MethodPost, "/v1/jobs/"+w.jobID.String()+"/driver", asProvider,
		`{"driver_name":"Sam Patel","driver_mobile":"0412 345 678"}`)
	if status != http.StatusCreated && status != http.StatusOK {
		t.Fatalf("assigning a driver: status %d, body %v", status, body)
	}

	// The seeded token is replaced by the assignment's own, and this is the whole reason the four
	// driver routes were unreachable rather than merely unwritten. seedDriverToken mints a token
	// over `uuid.New()` — a signature the verifier accepts, naming an assignment that does not
	// exist. So the world had a *valid* credential for nothing, which is exactly the 404 SHIP-17b
	// recorded against all four. The response carries the real one; taking it from there rather
	// than re-signing keeps the fixture on the path a driver actually receives.
	if token, _ := body["driver_token"].(string); token != "" {
		w.driverToken = token
	} else {
		t.Fatalf("the assignment carries no driver_token: %v", body)
	}
}

// deliveryUnderWay carries the job to In transit, which is the rung proof can attach to.
//
// Proof is what a driver captures on the way, so an upload URL for a job nobody has set off on has
// nothing to be proof *of*. The milestones are driven rather than laddered because each one is a
// recording the delivery domain makes — the job's status moves as a consequence, which is the
// direction the platform works in.
func (w *contractWorld) deliveryUnderWay(t *testing.T) {
	t.Helper()

	w.assignDriver(t)
	for _, m := range []string{"en_route_to_pickup", "picked_up", "in_transit"} {
		status, body := w.drive(t, http.MethodPost, "/v1/jobs/"+w.jobID.String()+"/milestones", asProvider,
			`{"milestone":"`+m+`"}`)
		if status != http.StatusCreated {
			t.Fatalf("recording %s: status %d, body %v", m, status, body)
		}
	}
}

// raiseDispute takes the job far enough along to be disputed and then disputes it.
//
// The two administrator dispute routes were named unreached for want of "a dispute, which needs a
// delivery to dispute" — both halves of which the ladder now supplies. It is the cascade worth
// noticing about a job-state builder: the routes it unlocks directly are the job's, and the ones
// it unlocks by consequence belong to other domains entirely.
func (w *contractWorld) raiseDispute(t *testing.T) {
	t.Helper()

	w.deliveryUnderWay(t)

	status, body := w.drive(t, http.MethodPost, "/v1/jobs/"+w.jobID.String()+"/disputes", asCustomer,
		`{"category":"goods_damaged_or_missing",`+
			`"description":"Two of the four crates arrived with the sides staved in.",`+
			`"desired_outcome":"A record of the damage, and the provider contacted about it.",`+
			`"occurred_at":"`+time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)+`"}`)
	if status != http.StatusCreated {
		t.Fatalf("raising a dispute: status %d, body %v", status, body)
	}
	id, _ := body["id"].(string)
	parsed, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("the raised dispute has no id: %v", body)
	}
	w.disputeID = parsed
}

// contractOffer is the body a provider posts to place or revise an offer.
//
// The two instants are computed rather than literal because both are validated as future — a fixed
// date-time in a test file is a fixture with an expiry date, and this one would start failing on a
// day nobody was looking at this file.
func contractOffer(cents int) string {
	pickup := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	deliver := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	return `{"amount_cents":` + strconv.Itoa(cents) + `,"pickup_at":"` + pickup + `","deliver_by":"` + deliver + `"}`
}

// advanceJobTo walks the seeded job up the ladder to the named status, through the guarded
// transition and one transaction per rung.
//
// One transaction per rung rather than one for the walk, because that is how the platform moves a
// job: each transition is its own committed act with its own history row. A single transaction
// spanning four rungs would test a shape nothing in production produces.
func (w *contractWorld) advanceJobTo(t *testing.T, to jobs.Status) {
	t.Helper()

	from := -1
	target := -1
	for i, s := range contractJobLadder {
		if s == jobs.StatusDraft {
			from = i
		}
		if s == to {
			target = i
		}
	}
	if target < 0 {
		t.Fatalf("advanceJobTo: %s is not on the contract ladder; add it or drive the route that reaches it", to)
	}

	svc := newJobService(testDeps())
	for i := from + 1; i <= target; i++ {
		rung := contractJobLadder[i]
		err := db.InTx(t.Context(), w.pool, func(ctx context.Context, tx db.Runner) error {
			_, err := svc.Transition(ctx, tx, jobs.Move{
				JobID:  w.jobID,
				To:     rung,
				Actor:  jobs.User(jobs.ActorCustomer, w.customerID),
				Reason: "SHIP-17c contract fixture",
			})
			return err
		})
		if err != nil {
			t.Fatalf("advancing the job to %s: %v", rung, err)
		}
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

	// assert is an extra check on the decoded response, for a route whose status does not by
	// itself prove the handler found anything (SHIP-17d).
	//
	// **Nearly every route here needs none, and that is the point of having it on so few.** A 200
	// from `POST /v1/auth/verify-phone` means a code was matched and consumed — anything else is
	// `identity_otp_invalid` — so the status *is* the assertion. A 200 from a collection is not:
	// an empty page is a valid response against the contract, so a fixture that seeded nothing
	// validates the envelope and none of the fields inside it, and passes. That is the vacuous
	// pass this file's header is written about, one level down.
	//
	// Runs after the schema check, so a body that fails both reports the contract violation first.
	assert func(t *testing.T, body map[string]any)

	// setup advances the world past what newContractWorld seeds — a job further up the ladder, an
	// offer actually submitted, a driver assigned. Nil leaves the seeded world alone.
	//
	// It runs against a world of this case's own, which is what makes it safe for it to mutate:
	// TestResponsesMatchTheContract builds one per subtest for the fixture-bleed reason
	// newContractWorld's comment gives, and a setup that moves a job would otherwise be the
	// loudest possible example of it.
	setup func(t *testing.T, w *contractWorld)
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

	// Bidding — every one of these needs the job past Draft (SHIP-17c).
	"POST /v1/jobs/{id}/bids": {auth: asProvider, want: 201,
		setup: func(t *testing.T, w *contractWorld) { w.openToBidding(t) },
		body:  func(w *contractWorld) string { return contractOffer(45000) }},

	"POST /v1/jobs/{id}/extend": {auth: asCustomer, want: 200,
		setup: func(t *testing.T, w *contractWorld) { w.advanceJobTo(t, jobs.StatusOpen) },
		body:  func(*contractWorld) string { return `{}` }},

	"POST /v1/jobs/{id}/award": {auth: asCustomer, want: 200,
		setup: func(t *testing.T, w *contractWorld) { w.openToBidding(t); w.submitBid(t) },
		body:  func(w *contractWorld) string { return `{"bid_id":"` + w.bidID.String() + `"}` }},

	"PATCH /v1/jobs/{id}/bids/{bid_id}": {auth: asProvider, want: 200,
		setup: func(t *testing.T, w *contractWorld) { w.openToBidding(t); w.submitBid(t) },
		body:  func(*contractWorld) string { return `{"amount_cents":39900}` }},

	"POST /v1/jobs/{id}/bids/{bid_id}/counter": {auth: asCustomer, want: 201,
		setup: func(t *testing.T, w *contractWorld) { w.openToBidding(t); w.submitBid(t) },
		body:  func(*contractWorld) string { return `{"amount_cents":39900}` }},

	"POST /v1/jobs/{id}/bids/{bid_id}/withdraw": {auth: asProvider, want: 200,
		setup: func(t *testing.T, w *contractWorld) { w.openToBidding(t); w.submitBid(t) },
		body:  func(*contractWorld) string { return `{}` }},

	// Delivery — the rungs past an award.
	"POST /v1/jobs/{id}/driver": {auth: asProvider, want: 201,
		setup: func(t *testing.T, w *contractWorld) { w.awardBid(t) },
		body: func(*contractWorld) string {
			return `{"driver_name":"Sam Patel","driver_mobile":"0412 345 678"}`
		}},

	"POST /v1/jobs/{id}/driver/link": {auth: asProvider, want: 200,
		setup: func(t *testing.T, w *contractWorld) { w.assignDriver(t) },
		body:  func(*contractWorld) string { return `{}` }},

	"POST /v1/jobs/{id}/milestones": {auth: asProvider, want: 201,
		setup: func(t *testing.T, w *contractWorld) { w.assignDriver(t) },
		body:  func(*contractWorld) string { return `{"milestone":"en_route_to_pickup"}` }},

	"POST /v1/jobs/{id}/proof-uploads": {auth: asProvider, want: 200,
		setup: func(t *testing.T, w *contractWorld) { w.deliveryUnderWay(t) },
		body:  func(*contractWorld) string { return `{"content_type":"image/jpeg","content_length":1048576}` }},

	// The driver surface. All four need the assignment the token names to exist (SHIP-17c).
	"GET /v1/driver/jobs/{id}": {auth: asDriver, want: 200,
		setup: func(t *testing.T, w *contractWorld) { w.assignDriver(t) }},

	"GET /v1/driver/jobs/{id}/milestones": {auth: asDriver, want: 200,
		setup: func(t *testing.T, w *contractWorld) { w.assignDriver(t) }},

	"POST /v1/driver/jobs/{id}/milestones": {auth: asDriver, want: 201,
		setup: func(t *testing.T, w *contractWorld) { w.assignDriver(t) },
		body:  func(*contractWorld) string { return `{"milestone":"en_route_to_pickup"}` }},

	"POST /v1/driver/jobs/{id}/proof-uploads": {auth: asDriver, want: 200,
		setup: func(t *testing.T, w *contractWorld) { w.deliveryUnderWay(t) },
		body:  func(*contractWorld) string { return `{"content_type":"image/jpeg","content_length":1048576}` }},

	"POST /v1/jobs/{id}/disputes": {auth: asCustomer, want: 201,
		setup: func(t *testing.T, w *contractWorld) { w.deliveryUnderWay(t) },
		body: func(*contractWorld) string {
			return `{"category":"goods_damaged_or_missing",` +
				`"description":"Two of the four crates arrived with the sides staved in.",` +
				`"desired_outcome":"A record of the damage, and the provider contacted about it.",` +
				`"occurred_at":"` + time.Now().Add(-time.Hour).UTC().Format(time.RFC3339) + `"}`
		}},

	"GET /v1/admin/disputes/{id}": {auth: asAdmin, want: 200,
		setup: func(t *testing.T, w *contractWorld) { w.raiseDispute(t) },
		path:  func(w *contractWorld) string { return "/v1/admin/disputes/" + w.disputeID.String() }},

	"POST /v1/admin/disputes/{id}/resolution": {auth: asAdmin, want: 200,
		setup: func(t *testing.T, w *contractWorld) { w.raiseDispute(t) },
		path: func(w *contractWorld) string {
			return "/v1/admin/disputes/" + w.disputeID.String() + "/resolution"
		},
		body: func(*contractWorld) string {
			return `{"outcome":"delivery_issue_acknowledged","job_outcome":"completed",` +
				`"reason":"Damage recorded and the provider contacted about it."}`
		}},

	// The verification record exists from registration, at Pending — a trigger writes it when a
	// provider account is created. So this route needs no seeding beyond the provider the world
	// already has, and the {id} it addresses is the provider's: provider_verifications is keyed by
	// provider_id. What it must *not* have is makeProviderEligible, which decides the same record
	// to Verified — and `from_state <> to_state` refuses a decision that moves nothing.
	"POST /v1/admin/verifications/{id}/decision": {auth: asAdmin, want: 200,
		path: func(w *contractWorld) string {
			return "/v1/admin/verifications/" + w.providerID.String() + "/decision"
		},
		body: func(*contractWorld) string {
			return `{"state":"Verified","reason":"Licence, registration and insurance all current."}`
		}},

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
	"POST /v1/auth/request-otp": {auth: asAnonymous, want: 202, body: func(*contractWorld) string {
		return `{"phone":"` + contractCustomerPhone + `"}`
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

	// The last five (SHIP-17d). Each needed something the job ladder cannot produce — a second
	// administrator, bytes in a bucket, or a secret the platform deliberately sends out of band —
	// which is why they outlived the eighteen SHIP-17c lifted in one move.

	// {id} is the review, and asAdmin here is the *second* administrator: the setup reassigns the
	// token, because the two-person rule is what this route is.
	"POST /v1/admin/suspensions/{id}/approval": {auth: asAdmin, want: 200,
		setup: func(t *testing.T, w *contractWorld) { w.suspensionAwaitingApproval(t) },
		path: func(w *contractWorld) string {
			return "/v1/admin/suspensions/" + w.suspensionReviewID.String() + "/approval"
		}},

	// A document has to have been submitted, or this answers 200 with an empty page — a pass that
	// validates none of the eight fields the contract declares for one. {id} is the provider's
	// account id: provider_verifications is keyed by provider_id.
	"GET /v1/admin/verifications/{id}/documents": {auth: asAdmin, want: 200,
		setup: func(t *testing.T, w *contractWorld) { w.submitVerificationDocument(t) },
		path: func(w *contractWorld) string {
			return "/v1/admin/verifications/" + w.providerID.String() + "/documents"
		},
		assert: func(t *testing.T, body map[string]any) {
			// The reason this route needed the assert hook. It answered 200 with an empty page
			// for as long as it went undriven, and an empty page validates against the contract
			// perfectly well — so without this the case would prove the envelope and none of the
			// eight fields a document carries.
			data, _ := body["data"].([]any)
			if len(data) == 0 {
				t.Fatalf("the evidence page is empty, so this case validated an envelope and "+
					"no document.\n  body: %v\n"+
					"  The setup submits one against the provider this path addresses; an empty "+
					"page means it is being read back under a different id.", body)
			}
		}},

	"POST /v1/auth/verify-email": {auth: asAnonymous, want: 200,
		setup: func(t *testing.T, w *contractWorld) { w.seedEmailVerificationToken(t) },
		body: func(*contractWorld) string {
			return `{"token":"` + contractEmailToken + `"}`
		}},

	"POST /v1/auth/verify-phone": {auth: asAnonymous, want: 200,
		setup: func(t *testing.T, w *contractWorld) { w.seedPhoneCode(t) },
		body: func(*contractWorld) string {
			return `{"phone":"` + contractCustomerPhone + `","code":"` + contractPhoneCode + `"}`
		}},

	// The one that needed the store wired before the router. See contractStore.
	"POST /v1/provider/verification/documents": {auth: asProvider, want: 201,
		setup: func(_ *testing.T, w *contractWorld) { w.storeVerificationObject() },
		body: func(w *contractWorld) string {
			return `{"kind":"licence","object_key":"` + w.verificationObjectKey + `"}`
		}},
}

// contractUnreached names every route no fixture drives, with the reason.
//
// **This is not the old skip list.** A route here is named and counted; a route in neither map
// fails the test. The third clause of SHIP-17b's *Done when* is that whatever is still unreached is
// named in the output rather than counted, and an exemption that has to be written by hand is the
// only form of that which cannot rot silently.
var contractUnreached = map[string]string{
	// # Empty, as of SHIP-17d — and the map stays because emptiness is the finding
	//
	// SHIP-17b named 23 routes and grouped 18 of them under one prediction: that they lift together
	// once the job can be seeded past Draft. SHIP-17c built that builder and the prediction held —
	// the job ladder, an eligible provider, an award, an assignment and three milestones took the
	// gate from 63 to 81 of 86. **Two of those 18 were not in the group at all**: the administrator
	// dispute routes came free, because a dispute needs a delivery and the ladder now reaches one.
	//
	// The five that were left were not a smaller version of that problem, and each needed something
	// the lifecycle could not produce: a second administrator, an object in a bucket, or a secret
	// the platform sends out of band and keeps no readable copy of. They are driven now — the two
	// one-time secrets by minting the credential and storing what the platform would have stored,
	// the document pair by giving the world an object store, and the approval by signing a second
	// administrator in.
	//
	// **Leaving the map here rather than deleting it is deliberate.** It is the half of
	// [TestEveryRouteIsDrivenOrNamed] that makes a gap cost a sentence somebody has to write: a new
	// route with no fixture fails the build until it is either driven or entered here with a
	// reason. Deleting it would make the next unreached route a silent one, which is the state
	// SHIP-17a's log line was replaced for.
	//
	// One thing an entry must not become is a place to park a route that is merely awkward. Each of
	// the five above was named with a reason that was true when written, and one of them was wrong
	// about the mechanics in a way that would have cost somebody an afternoon: the verification
	// documents route was recorded as needing the object store, and it does not — `PresignDownload`
	// is an HMAC over a string and touches no I/O. What it needed was a document to list.
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
			if c.setup != nil {
				c.setup(t, w)
			}

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

			if c.assert != nil {
				var decoded map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
					t.Fatalf("decoding the response to assert on it: %v\n  body: %s", err, rec.Body)
				}
				c.assert(t, decoded)
			}
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
