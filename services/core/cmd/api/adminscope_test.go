package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/admin"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/idempotency"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/passwords"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/ratelimit"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/redistest"
)

// SHIP-147b, end to end, and the only place it can be proved.
//
// # Why it is here and not in internal/httpx or internal/admin
//
// The clause is about three things meeting: a real administrator session, the group-wide
// resolution `newRouter` wires, and the idempotency middleware's namespacing. `internal/httpx`
// has no administrator and no database; `internal/admin` has both and cannot build a router.
// cmd/api is the composition root, which is the one place all three are visible — the same
// argument adminauth_test.go's header makes about the two verifiers.
//
// # Why it drives the real router against real sessions
//
// The wave-11 finding this is written against: *a test can derive its expectation from the thing
// it tests*. A test that computed a scope with `httpx.SubjectScope` and compared the two strings
// would agree with any mutation of that function by construction, and would pass with the hole
// wide open. So nothing here mentions a scope. Two administrators sign in for real, send the same
// method, path, body and `Idempotency-Key`, and the assertions are on **the response bodies and
// the rows in `admin_notes`** — the observable difference between two executions and one replay.
//
// # Why POST /v1/admin/notes
//
// It is a RequireAdmin state change whose 201 carries `author_id` and a fresh `id`, so two
// executions produce visibly different responses rather than two identical 204s that only a
// header could tell apart. Its subject is deliberately not a foreign key (SHIP-162), so no user
// or job has to exist for the note to be about something.

// twoAdministrators is the fixture: a router over a real database and cache, and two signed-in
// moderators — the least-privileged role that holds `notes.write`.
type twoAdministrators struct {
	router http.Handler
	alice  administratorSession
	bob    administratorSession
}

type administratorSession struct {
	id    uuid.UUID
	token string
}

func signedInAdministrators(t *testing.T) twoAdministrators {
	t.Helper()

	pool := pgtest.DB(t)
	client, prefix := redistest.Client(t)

	deps := testDeps()
	deps.Pool = pool
	deps.Redis = client

	// The router main.go builds, over this test's database: the real guard, the real resolver,
	// the real middleware order. Nothing is stubbed but the idempotency store, which is in
	// memory so that one test's keys cannot reach another's.
	adminAuthn, err := newAdminGuard(deps.Config, pool, deps.Clock)
	if err != nil {
		t.Fatalf("building the administrator guard: %v", err)
	}
	router := newRouter(deps, idempotency.NewMemoryStore(), testAuthenticator(),
		testDriverGuard(), adminAuthn)

	// The credentials service is built here rather than reached through the router because the
	// *first* administrator in a database has no creator inside the platform — `000801`'s header
	// records that it arrives by hand — so there is no sign-in to bootstrap from.
	hasher, err := passwords.NewHasher(passwords.Argon2Profile{
		MemoryKiB:   deps.Config.Passwords.Argon2.MemoryKiB,
		Iterations:  deps.Config.Passwords.Argon2.Iterations,
		Parallelism: deps.Config.Passwords.Argon2.Parallelism,
	})
	if err != nil {
		t.Fatalf("building the hasher: %v", err)
	}
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

	const password = "correct-horse-battery-staple"
	bootstrap := uuid.New()

	signIn := func(email string) administratorSession {
		t.Helper()
		created, err := creds.Create(t.Context(), admin.CreateCommand{
			Email: email, Name: "A Person", Password: password,
			Role: admin.RoleModerator, ActorID: bootstrap,
		})
		if err != nil {
			t.Fatalf("creating %s: %v", email, err)
		}
		issued, _, err := creds.SignIn(t.Context(), admin.SignInCommand{
			Email: email, Password: password, ClientIP: "10.0.0.1",
		})
		if err != nil {
			t.Fatalf("signing in %s: %v", email, err)
		}
		return administratorSession{id: created.ID, token: issued.Token}
	}

	return twoAdministrators{
		router: router,
		alice:  signIn("alice@example.com"),
		bob:    signIn("bob@example.com"),
	}
}

// TestTwoAdministratorSessionsDoNotShareAnIdempotencyScope is SHIP-147b's *Done when*.
//
// **This is the test the ticket requires to fail when the scope reverts to its user-only form.**
// Reverting `httpx.SubjectScope` to reading only the `authctx.Subject` puts both administrators in
// `idem:v1:anonymous:<key>`, and the second request is then handed the first's stored 201 — with
// the first administrator's `author_id` in it, and no second row in `admin_notes`.
func TestTwoAdministratorSessionsDoNotShareAnIdempotencyScope(t *testing.T) {
	fixture := signedInAdministrators(t)

	// One key, one method, one path, one body. replayOrRefuse fingerprints method, path and
	// body, so with all three identical **the scope is the only thing separating these two
	// requests** — which is what makes the assertion below a statement about the scope.
	const sharedKey = "01J9ZK4P2M8SB3TC6VE9XA0N7D"
	subject := uuid.New()
	body := `{"subject_type":"user","subject_id":"` + subject.String() + `",` +
		`"body":"Called about the delayed pickup."}`

	post := func(t *testing.T, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/v1/admin/notes", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(httpx.HeaderIdempotencyKey, sharedKey)
		req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)

		rec := httptest.NewRecorder()
		fixture.router.ServeHTTP(rec, req)
		return rec
	}

	authorOf := func(t *testing.T, rec *httptest.ResponseRecorder) (author, id string) {
		t.Helper()
		var note struct {
			ID       string `json:"id"`
			AuthorID string `json:"author_id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &note); err != nil {
			t.Fatalf("the response is not a note: %v\n%s", err, rec.Body.String())
		}
		return note.AuthorID, note.ID
	}

	first := post(t, fixture.alice.token)
	if first.Code != http.StatusCreated {
		t.Fatalf("the first administrator's note was refused with %d: %s",
			first.Code, first.Body.String())
	}
	if replayed := first.Header().Get(httpx.HeaderIdempotencyReplayed); replayed != "" {
		t.Fatalf("the first request was already a replay (%q); the store is not empty", replayed)
	}
	firstAuthor, firstNote := authorOf(t, first)
	if firstAuthor != fixture.alice.id.String() {
		t.Fatalf("the note names %s as its author, want %s", firstAuthor, fixture.alice.id)
	}

	second := post(t, fixture.bob.token)

	if second.Header().Get(httpx.HeaderIdempotencyReplayed) == "true" {
		t.Error("a second administrator using the same idempotency key was handed the first " +
			"administrator's stored response.\n" +
			"Two administrator sessions share an idempotency namespace, which is the hole " +
			"SHIP-147b exists to close — check that newRouter applies httpx.ResolvePrincipal " +
			"outside httpx.Idempotent, that newAdminGuard supplies its Scope, and that " +
			"httpx.SubjectScope reads a Principal when there is no authctx.Subject " +
			"(Docs/11 §9).")
	}
	if second.Code != http.StatusCreated {
		t.Fatalf("the second administrator's note was refused with %d: %s",
			second.Code, second.Body.String())
	}

	secondAuthor, secondNote := authorOf(t, second)
	if secondAuthor != fixture.bob.id.String() {
		t.Errorf("the second response names %s as its author, want %s (%s is the first "+
			"administrator) — the second administrator was served the first one's note",
			secondAuthor, fixture.bob.id, fixture.alice.id)
	}
	if secondNote == firstNote {
		t.Errorf("both administrators were given note %s; only one execution happened", firstNote)
	}

	// The responses are what a client sees; the rows are what happened. A replay would leave
	// exactly one.
	assertNoteCount(t, fixture, subject, 2)

	// And idempotency still works *within* one administrator, or the fix above would be a way of
	// disabling the mechanism rather than of scoping it.
	replay := post(t, fixture.alice.token)
	if replay.Header().Get(httpx.HeaderIdempotencyReplayed) != "true" {
		t.Error("the same administrator repeating the same key did not get a replay; " +
			"idempotency has been scoped into uselessness rather than scoped correctly")
	}
	if replay.Body.String() != first.Body.String() {
		t.Errorf("the replay differs from the stored response:\n got %s\nwant %s",
			replay.Body.String(), first.Body.String())
	}
	assertNoteCount(t, fixture, subject, 2)
}

// assertNoteCount reads what actually happened, rather than what was answered.
//
// A replay returns a stored body with no handler running, so the response alone cannot distinguish
// "executed twice" from "answered twice". The rows can.
func assertNoteCount(t *testing.T, fixture twoAdministrators, subject uuid.UUID, want int) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet,
		"/v1/admin/notes?subject_type=user&subject_id="+subject.String(), nil)
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+fixture.alice.token)

	rec := httptest.NewRecorder()
	fixture.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("reading the notes back answered %d: %s", rec.Code, rec.Body.String())
	}

	var read struct {
		Data []struct {
			AuthorID string `json:"author_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &read); err != nil {
		t.Fatalf("the note list is not readable: %v\n%s", err, rec.Body.String())
	}

	if len(read.Data) != want {
		t.Errorf("%d notes were recorded about this subject, want %d.\n"+
			"A replay answers without the handler running, so the row count is what "+
			"separates two executions from one execution answered twice.", len(read.Data), want)
	}

	authors := map[string]bool{}
	for _, item := range read.Data {
		authors[item.AuthorID] = true
	}
	if want == 2 && len(authors) != 2 {
		t.Errorf("the recorded notes have %d distinct authors, want 2 — both administrators "+
			"should have written one", len(authors))
	}
}
