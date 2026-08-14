package admin

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/passwords"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/ratelimit"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/redistest"
)

// SHIP-147 against a real PostgreSQL and a real Redis.
//
// Docs/06 §4.1 is the argument for not mocking either: the twelve-hour cap is a CHECK constraint,
// the one-account-per-address rule is a partial-free unique index, and the rate limit is a Lua
// script. A store behind an interface would let every test here pass while none of the three was
// doing anything.
//
// # What the *Done when* asks for, and where each half is proved
//
// "Admin sign-in is independent and cannot be reached with a user token." The first clause is the
// sign-in below — no `users` row is touched, no access token is issued, and nothing here imports
// `identity`, which the boundary lint would refuse anyway. The second is
// [TestAMobileAccessTokenIsNotAnAdministratorSession] in this file and its mirror in
// cmd/api/adminauth_test.go, which is the only place both verifiers are visible at once.

// testProfile is a reduced argon2id cost. 64 MiB per hash across parallel tests will thrash a
// laptop (Docs/10 §5), and what these tests exercise is the flow rather than the cost.
var testProfile = passwords.Argon2Profile{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1}

// testPassword is the fixture credential. Long enough to satisfy the creation floor.
const testPassword = "correct-horse-battery-staple"

// adminAuth is a credentials service, a verifier over the same pool, and the clock both read.
//
// The clock is a *clock.Fixed so that a test can move time rather than wait for it: an idle window
// of thirty minutes and a cap of twelve hours are not durations a test can sleep through, and a
// test that shortened them by reading a variable would be testing a variable.
func adminAuth(t *testing.T) (*Credentials, *Authenticator, *pgxpool.Pool, *clock.Fixed) {
	t.Helper()

	pool := pgtest.DB(t)
	clk := clock.NewFixed(time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC))

	hasher, err := passwords.NewHasher(testProfile)
	if err != nil {
		t.Fatalf("building the hasher: %v", err)
	}

	client, prefix := redistest.Client(t)
	limiter, err := ratelimit.New(client, prefix, clk)
	if err != nil {
		t.Fatalf("building the rate limiter: %v", err)
	}

	creds, err := NewCredentials(pool, hasher, limiter, clk)
	if err != nil {
		t.Fatalf("building the credentials service: %v", err)
	}

	auth, err := NewAuthenticator(pool, clk)
	if err != nil {
		t.Fatalf("building the authenticator: %v", err)
	}
	return creds, auth, pool, clk
}

// anAdministrator creates one through the domain's own path.
func anAdministrator(t *testing.T, creds *Credentials, email string, role Role) Administrator {
	t.Helper()

	a, err := creds.Create(t.Context(), CreateCommand{
		Email: email, Name: "A Person", Password: testPassword, Role: role,
	})
	if err != nil {
		t.Fatalf("creating %s: %v", email, err)
	}
	return a
}

func signIn(t *testing.T, creds *Credentials, email, password, ip string) (Issued, Administrator, error) {
	t.Helper()
	return func() (Issued, Administrator, error) {
		return creds.SignIn(t.Context(), SignInCommand{Email: email, Password: password, ClientIP: ip})
	}()
}

// TestSignInIssuesASessionThatResolves is the first half of the *Done when*.
//
// It checks the credential is *usable* rather than merely returned: a token that does not resolve
// would satisfy a shallower reading, and an administrator holding one would find every
// administrative route answering 401 while sign-in reported success.
func TestSignInIssuesASessionThatResolves(t *testing.T) {
	creds, auth, _, clk := adminAuth(t)
	created := anAdministrator(t, creds, "moderator@example.com", RoleModerator)

	issued, administrator, err := signIn(t, creds, "Moderator@example.com", testPassword, "10.0.0.1")
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}
	if administrator.ID != created.ID {
		t.Errorf("signed in as %s, want %s", administrator.ID, created.ID)
	}
	if issued.Token == "" {
		t.Fatal("no token was issued, so nothing can be presented")
	}

	// The two lifetimes, as the constants state them. Asserted because a session with the wrong
	// window is the defect nobody notices until an administrator is signed out mid-decision or
	// never signed out at all.
	if want := clk.Now().Add(idleWindow); !issued.Session.IdleExpiresAt.Equal(want) {
		t.Errorf("idle expiry = %s, want %s", issued.Session.IdleExpiresAt, want)
	}
	if want := clk.Now().Add(absoluteLifetime); !issued.Session.AbsoluteExpiresAt.Equal(want) {
		t.Errorf("absolute expiry = %s, want %s", issued.Session.AbsoluteExpiresAt, want)
	}

	grant, err := auth.Resolve(t.Context(), issued.Token)
	if err != nil {
		t.Fatalf("resolving the credential that was just issued: %v", err)
	}
	if grant.Administrator.ID != created.ID {
		t.Errorf("the credential resolved to %s, want %s", grant.Administrator.ID, created.ID)
	}
	if grant.Administrator.Role != RoleModerator {
		t.Errorf("role = %q, want %q", grant.Administrator.Role, RoleModerator)
	}
	if grant.SessionID != issued.Session.ID {
		t.Errorf("session = %s, want %s", grant.SessionID, issued.Session.ID)
	}
}

// TestTheSessionTokenIsNeverStored is the property every other guarantee here rests on.
//
// `admin_sessions` is read by every support query, every backup and every replica. If the token
// were in it, all three would be a way into the console.
func TestTheSessionTokenIsNeverStored(t *testing.T) {
	creds, _, pool, _ := adminAuth(t)
	anAdministrator(t, creds, "stored@example.com", RoleSupport)

	issued, _, err := signIn(t, creds, "stored@example.com", testPassword, "10.0.0.1")
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}

	var stored string
	if err := pool.QueryRow(t.Context(),
		`SELECT token_hash FROM admin_sessions WHERE id = $1`, issued.Session.ID).Scan(&stored); err != nil {
		t.Fatalf("reading the session row: %v", err)
	}

	if stored == issued.Token {
		t.Fatal("the session token is stored verbatim, so the whole console is readable from a backup")
	}
	if stored != hashSessionToken(issued.Token) {
		t.Errorf("the stored digest is not this package's hash of the token, so the two have drifted")
	}
	if strings.Contains(stored, issued.Token) {
		t.Error("the stored value contains the token")
	}
}

// TestUnknownAddressAndWrongPasswordAreOneAnswer is the disclosure half of sign-in.
//
// Two answers would make an unauthenticated endpoint an oracle for which addresses are
// administrators — a more valuable list than which addresses have marketplace accounts.
func TestUnknownAddressAndWrongPasswordAreOneAnswer(t *testing.T) {
	creds, _, _, _ := adminAuth(t)
	anAdministrator(t, creds, "known@example.com", RoleSupport)

	for name, tc := range map[string]struct{ email, password string }{
		"no such administrator": {"nobody@example.com", testPassword},
		"wrong password":        {"known@example.com", "not-the-password"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := signIn(t, creds, tc.email, tc.password, "10.0.1.1")
			if !errors.Is(err, ErrAdminCredentialsInvalid) {
				t.Fatalf("error = %v, want ErrAdminCredentialsInvalid — two distinguishable "+
					"answers make this endpoint an administrator-address oracle", err)
			}
		})
	}
}

// TestSignInSpendsTheSameWorkWhetherOrNotTheAdministratorExists is the timing half of the same
// disclosure, and the half a plausible implementation leaves open.
//
// One code for both failures is worth nothing if the response time separates them: argon2id costs
// tens of milliseconds and a lookup that misses costs none of them. The comparison is a ratio
// rather than an absolute figure, so it holds at any profile.
//
// **Every sample uses a fresh account and a fresh network address**, so the rate limiter never
// enters the measurement — the buckets are per account and per address, and reusing either would
// make the fifth reading a throttle rather than a derivation.
//
// Mutation-checked: deleting the SpendEquivalentWork call makes the unknown-address path finish in
// microseconds and this fails.
func TestSignInSpendsTheSameWorkWhetherOrNotTheAdministratorExists(t *testing.T) {
	if testing.Short() {
		t.Skip("times argon2id derivations; -short is for the runs that skip infrastructure")
	}

	creds, _, _, _ := adminAuth(t)

	const samples = 5
	for i := range samples {
		anAdministrator(t, creds, fmt.Sprintf("timed-%d@example.com", i), RoleSupport)
	}

	measure := func(email func(int) string) time.Duration {
		var taken [samples]time.Duration
		for i := range samples {
			start := time.Now()
			if _, _, err := signIn(t, creds, email(i), "not-the-password",
				fmt.Sprintf("10.9.%d.%d", i, i)); !errors.Is(err, ErrAdminCredentialsInvalid) {
				t.Fatalf("sample %d: error = %v, want ErrAdminCredentialsInvalid", i, err)
			}
			taken[i] = time.Since(start)
		}
		for i := 1; i < samples; i++ {
			for j := i; j > 0 && taken[j] < taken[j-1]; j-- {
				taken[j], taken[j-1] = taken[j-1], taken[j]
			}
		}
		return taken[samples/2]
	}

	known := measure(func(i int) string { return fmt.Sprintf("timed-%d@example.com", i) })
	unknown := measure(func(i int) string { return fmt.Sprintf("absent-%d@example.com", i) })

	// A quarter, not a half: the true ratio is one — both are a single derivation at the same
	// profile — and the margin is there so a loaded machine cannot fail an honest build. What it
	// still catches is the defect, which is a path that does no work at all.
	if unknown < known/4 {
		t.Errorf("an unknown administrator address costs %s and a wrong password costs %s.\n"+
			"The response time answers what admin_credentials_invalid refuses to: a caller can "+
			"time two requests and learn which addresses are administrators.", unknown, known)
	}
}

// TestADisabledAccountIsToldSoAtSignInAndNotAtResolve is the one asymmetry in this file, and it is
// deliberate.
//
// At sign-in the caller has just proved they hold the account, so naming its standing tells them
// nothing they had not already established. At session resolution they hold only a token, which may
// have been taken — so the account's standing is not disclosed there, and a disabled administrator's
// live session is refused as though the credential were simply not valid.
// `identity.Service.Refresh` takes exactly this position.
func TestADisabledAccountIsToldSoAtSignInAndNotAtResolve(t *testing.T) {
	creds, auth, pool, _ := adminAuth(t)
	created := anAdministrator(t, creds, "leaver@example.com", RoleModerator)

	issued, _, err := signIn(t, creds, "leaver@example.com", testPassword, "10.0.2.1")
	if err != nil {
		t.Fatalf("signing in while active: %v", err)
	}

	if _, err := pool.Exec(t.Context(),
		`UPDATE admin_users SET status = 'disabled' WHERE id = $1`, created.ID); err != nil {
		t.Fatalf("disabling the account: %v", err)
	}

	// The live session stops working at once, with no second write and no waiting for an expiry.
	// That is the whole reason this credential is a row rather than a signature.
	_, err = auth.Resolve(t.Context(), issued.Token)
	if !errors.Is(err, ErrAdminAccountDisabled) {
		t.Errorf("resolving a disabled administrator's live session: error = %v, want "+
			"ErrAdminAccountDisabled.\nA signed token would have kept working until it expired, "+
			"which is the window this design exists to close.", err)
	}

	// And a fresh sign-in says why, because the password has just been proved.
	if _, _, err := signIn(t, creds, "leaver@example.com", testPassword, "10.0.2.2"); !errors.Is(err, ErrAdminAccountDisabled) {
		t.Errorf("signing in to a disabled account: error = %v, want ErrAdminAccountDisabled", err)
	}
}

// TestSigningOutEndsTheSessionImmediatelyAndIsSafeToRepeat.
//
// Immediately is the claim worth checking: revocation is a column on a row the guard reads, so
// there is no interval in which a signed-out credential still works.
func TestSigningOutEndsTheSessionImmediatelyAndIsSafeToRepeat(t *testing.T) {
	creds, auth, _, _ := adminAuth(t)
	anAdministrator(t, creds, "signout@example.com", RoleSupport)

	issued, _, err := signIn(t, creds, "signout@example.com", testPassword, "10.0.3.1")
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}
	if _, err := auth.Resolve(t.Context(), issued.Token); err != nil {
		t.Fatalf("the session did not resolve before sign-out: %v", err)
	}

	if err := creds.SignOut(t.Context(), issued.Session.ID); err != nil {
		t.Fatalf("signing out: %v", err)
	}
	if _, err := auth.Resolve(t.Context(), issued.Token); !errors.Is(err, ErrAdminSessionInvalid) {
		t.Errorf("a signed-out credential still resolves: error = %v, want ErrAdminSessionInvalid", err)
	}

	// A retrying browser must not be told its sign-out failed when it had not.
	if err := creds.SignOut(t.Context(), issued.Session.ID); err != nil {
		t.Errorf("signing out twice: %v", err)
	}
}

// TestTheIdleWindowSlidesWhileTheConsoleIsUsedAndLapsesWhenItIsNot.
//
// Both halves in one test, because they are one mechanism seen from two ends and a test that only
// checked the lapse would pass against an implementation that never slid at all — which is the
// version an administrator notices by being signed out every half hour while working.
func TestTheIdleWindowSlidesWhileTheConsoleIsUsedAndLapsesWhenItIsNot(t *testing.T) {
	creds, auth, _, clk := adminAuth(t)
	anAdministrator(t, creds, "sliding@example.com", RoleSupport)

	issued, _, err := signIn(t, creds, "sliding@example.com", testPassword, "10.0.4.1")
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}

	// Twenty minutes in, still inside the window. Resolving must push it forward.
	clk.Advance(20 * time.Minute)
	if _, err := auth.Resolve(t.Context(), issued.Token); err != nil {
		t.Fatalf("resolving inside the idle window: %v", err)
	}

	// Another twenty. Without a slide the original thirty-minute window would have gone; with
	// one there are ten minutes left.
	clk.Advance(20 * time.Minute)
	if _, err := auth.Resolve(t.Context(), issued.Token); err != nil {
		t.Fatalf("the idle window did not slide: %v\n"+
			"A console in continuous use is being signed out on the original window, which is "+
			"the failure a lapse-only test cannot see.", err)
	}

	// And now nothing happens for longer than the window.
	clk.Advance(idleWindow + time.Minute)
	if _, err := auth.Resolve(t.Context(), issued.Token); !errors.Is(err, ErrAdminSessionExpired) {
		t.Errorf("an idle session still resolves: error = %v, want ErrAdminSessionExpired", err)
	}
}

// TestASessionCannotOutliveItsAbsoluteCapHoweverBusyItIs is the control SHIP-39 deliberately did
// not add for a phone, and this ticket deliberately added for a console.
//
// The session is used every twenty minutes for the whole of its life, so the idle window never
// lapses. What ends it is the cap, and this is the test that fails if somebody removes the clamp
// while the sliding still works — the failure mode being a privileged credential that lives for
// ever as long as somebody keeps a tab open.
func TestASessionCannotOutliveItsAbsoluteCapHoweverBusyItIs(t *testing.T) {
	creds, auth, _, clk := adminAuth(t)
	anAdministrator(t, creds, "capped@example.com", RoleSupport)

	issued, _, err := signIn(t, creds, "capped@example.com", testPassword, "10.0.5.1")
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}

	for elapsed := time.Duration(0); elapsed < absoluteLifetime-20*time.Minute; elapsed += 20 * time.Minute {
		clk.Advance(20 * time.Minute)
		if _, err := auth.Resolve(t.Context(), issued.Token); err != nil {
			t.Fatalf("the session lapsed after %s of continuous use: %v", elapsed, err)
		}
	}

	clk.Advance(30 * time.Minute)
	if _, err := auth.Resolve(t.Context(), issued.Token); !errors.Is(err, ErrAdminSessionExpired) {
		t.Errorf("an administrator session outlived its %s cap by being used: error = %v, want "+
			"ErrAdminSessionExpired", absoluteLifetime, err)
	}
}

// TestAMobileAccessTokenIsNotAnAdministratorSession is the *Done when*'s second clause, from this
// side of it.
//
// The strings below are what the mobile system's credentials look like: a three-part HS256 JWT, and
// a 43-character base64url refresh token of exactly the shape this package's own tokens have. **The
// second is the interesting one** — it is indistinguishable from an administrator credential by
// inspection, and it is refused because the digest is not in `admin_sessions` rather than because
// anything about it looks wrong. There is no format check standing in for a lookup.
//
// The other direction — an administrator credential presented on a mobile route — is proved in
// cmd/api/adminauth_test.go, which is the only package where both verifiers are visible.
func TestAMobileAccessTokenIsNotAnAdministratorSession(t *testing.T) {
	creds, auth, _, _ := adminAuth(t)
	anAdministrator(t, creds, "separate@example.com", RoleOwner)

	// A real session exists, so a test that passed by having nothing to resolve against would
	// not be evidence of anything.
	if _, _, err := signIn(t, creds, "separate@example.com", testPassword, "10.0.6.1"); err != nil {
		t.Fatalf("signing in: %v", err)
	}

	for name, credential := range map[string]string{
		"a mobile access token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCIsImtpZCI6ImRldiJ9." +
			"eyJzdWIiOiIwMTk4ZjJjMS02YjQwLTdhMTEtOWMzZS0yZjlhNGQ1MWI3ZTAiLCJyb2xlIjoiY3VzdG9tZXIiLCJhdWQiOiJzaGlwcGVyLW1vYmlsZSJ9." +
			"c2lnbmF0dXJlLXRoYXQtaXMtbm90LWNoZWNrZWQtaGVyZQ",
		"a driver token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJzaGlwcGVyLWRyaXZlciJ9.c2ln",
		"a refresh token of exactly this package's shape": "9dW3nP1qTzR7lK0sYbV2xC8gH4jM6oA5eF1uI3rQ7wY",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := auth.Resolve(t.Context(), credential)
			if !errors.Is(err, ErrAdminSessionInvalid) {
				t.Fatalf("%s resolved to an administrator grant, or failed for the wrong "+
					"reason: %v\nCLAUDE.md: the credential systems are separate and neither "+
					"can be exchanged for the other.", name, err)
			}
		})
	}
}

// TestRequireAdminPutsAGrantOnTheContextAndNeverASubject.
//
// The grant is what a handler reads. An [authctx.Subject] is what every *other* domain in the
// service reads, and an administrator producing one would be the exchange CLAUDE.md forbids,
// performed by the platform on their behalf — `jobs`, `bidding` and `delivery` would then be
// serving a moderator as though a customer had signed in.
func TestRequireAdminPutsAGrantOnTheContextAndNeverASubject(t *testing.T) {
	creds, auth, _, _ := adminAuth(t)
	created := anAdministrator(t, creds, "granted@example.com", RoleModerator)

	issued, _, err := signIn(t, creds, "granted@example.com", testPassword, "10.0.7.1")
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}

	var (
		sawGrant   bool
		sawSubject bool
		gotID      string
	)
	guarded := RequireAdmin(auth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		grant, ok := grantFrom(r.Context())
		sawGrant = ok
		gotID = grant.Administrator.ID.String()
		_, sawSubject = authctx.SubjectFrom(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/me", nil)
	req.Header.Set("Authorization", "Bearer "+issued.Token) // spelling:ok — HTTP header name, RFC 9110
	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (%s)", rec.Code, rec.Body)
	}
	if !sawGrant {
		t.Error("the handler ran with no administrator grant on the context")
	}
	if gotID != created.ID.String() {
		t.Errorf("the grant names %s, want %s", gotID, created.ID)
	}
	if sawSubject {
		t.Error("a RequireAdmin request carried an authctx.Subject, so the administrator session " +
			"and the mobile session have been joined")
	}
}

// TestRequireAdminRefusesEveryWayOfPresentingNothingUsable.
//
// The status is 401 in every case and the *code* is what a client branches on. A lapsed session is
// separated because it is the one an administrator can act on without wondering whether something
// is broken; nothing else is, because no legitimate administrator could do anything differently
// about any of them.
func TestRequireAdminRefusesEveryWayOfPresentingNothingUsable(t *testing.T) {
	creds, auth, _, clk := adminAuth(t)
	anAdministrator(t, creds, "refused@example.com", RoleSupport)

	issued, _, err := signIn(t, creds, "refused@example.com", testPassword, "10.0.8.1")
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}

	guarded := RequireAdmin(auth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	probe := func(header string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/v1/admin/me", nil)
		if header != "" {
			req.Header.Set("Authorization", header) // spelling:ok — HTTP header name, RFC 9110
		}
		rec := httptest.NewRecorder()
		guarded.ServeHTTP(rec, req)
		return rec
	}

	for name, tc := range map[string]struct{ header, wantCode string }{
		"nothing at all":     {"", "unauthenticated"},
		"an empty bearer":    {"Bearer ", "unauthenticated"},
		"a different scheme": {"Basic YWRtaW46YWRtaW4=", "unauthenticated"},
		"an unknown token":   {"Bearer " + strings.Repeat("A", 43), "unauthenticated"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := probe(tc.header)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body)
			}
			if !strings.Contains(rec.Body.String(), tc.wantCode) {
				t.Errorf("body = %s, want error code %q", rec.Body, tc.wantCode)
			}
			if rec.Header().Get("WWW-Authenticate") == "" {
				t.Error("no WWW-Authenticate challenge, which RFC 9110 requires on a 401")
			}
		})
	}

	// The lapse, which is the one with its own code.
	clk.Advance(absoluteLifetime + time.Minute)
	rec := probe("Bearer " + issued.Token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), string(CodeAdminSessionExpired)) {
		t.Errorf("a lapsed session answered %s, want %q — the console cannot tell a timeout from "+
			"a credential that was never valid", rec.Body, CodeAdminSessionExpired)
	}
}

// TestWithNoDatabaseTheGuardAnswers503RatherThan401.
//
// main.go starts with an unreachable database on purpose, so the guard holds a nil pool for as long
// as the failover lasts. Answering 401 would tell every administrator their credential was bad and
// send them to reset a password that was never wrong.
func TestWithNoDatabaseTheGuardAnswers503RatherThan401(t *testing.T) {
	auth, err := NewAuthenticator(nil, clock.System{})
	if err != nil {
		t.Fatalf("building an authenticator with no pool: %v", err)
	}

	guarded := RequireAdmin(auth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the handler ran with no database behind the guard")
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/me", nil)
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("A", 43)) // spelling:ok — HTTP header name, RFC 9110
	rec := httptest.NewRecorder()
	guarded.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (%s)", rec.Code, rec.Body)
	}
}

// TestSignInIsThrottledPerAccount is SHIP-47's mechanism on this domain's figures.
//
// The limit is checked *before* the pool and before argon2id, which is the point of a rate limit:
// what it protects is the work, and a limit checked after the derivation has been paid for is a
// limit on nothing.
func TestSignInIsThrottledPerAccount(t *testing.T) {
	creds, _, _, _ := adminAuth(t)
	anAdministrator(t, creds, "throttled@example.com", RoleSupport)

	var throttled *ThrottledError
	for attempt := range signInAccountCapacity + 1 {
		_, _, err := signIn(t, creds, "throttled@example.com", "not-the-password", "10.0.9.1")
		if errors.As(err, &throttled) {
			if attempt < signInAccountCapacity {
				t.Fatalf("throttled after %d attempts, want at least %d",
					attempt, signInAccountCapacity)
			}
			if throttled.RetryAfter <= 0 {
				t.Error("the refusal carries no wait, so a client is told to come back later " +
					"without being told when")
			}
			return
		}
		if !errors.Is(err, ErrAdminCredentialsInvalid) {
			t.Fatalf("attempt %d: error = %v, want ErrAdminCredentialsInvalid", attempt, err)
		}
	}
	t.Errorf("%d failed sign-ins against one administrator address were all admitted",
		signInAccountCapacity+1)
}

// TestCreatingAnAdministratorDefaultsToTheLeastPrivilegedRole is SHIP-148's *Done when* at the
// creation path.
//
// 000801's column default is the same rule for a row that never came through here; this is the one
// that reports the answer back to the caller, so an owner who omits the field is *told* they made a
// support account rather than finding out later.
func TestCreatingAnAdministratorDefaultsToTheLeastPrivilegedRole(t *testing.T) {
	creds, _, _, _ := adminAuth(t)

	created, err := creds.Create(t.Context(), CreateCommand{
		Email: "unspecified@example.com", Name: "A Person", Password: testPassword,
	})
	if err != nil {
		t.Fatalf("creating an administrator with no role: %v", err)
	}

	// Roles is ordered least privileged first, so the first entry is the minimum by definition.
	if want := Roles[0]; created.Role != want {
		t.Errorf("an administrator created with no role got %q, want %q.\n"+
			"Docs/04 §9 asks for least privilege, and a default that is anything but the "+
			"smallest bundle turns a forgotten field into a privilege escalation.",
			created.Role, want)
	}
}

// TestAnUnrecognisedRoleIsRefusedRatherThanQuietlyMinimised.
//
// Sending nothing and sending "administrator" are different mistakes. The first is a client that
// did not care; the second is a client that believes in a role this platform does not have, and
// silently giving it the minimum would mean an owner creating what they thought was a moderator and
// getting something else without being told.
func TestAnUnrecognisedRoleIsRefusedRatherThanQuietlyMinimised(t *testing.T) {
	creds, _, _, _ := adminAuth(t)

	_, err := creds.Create(t.Context(), CreateCommand{
		Email: "bogus@example.com", Name: "A Person", Password: testPassword, Role: "administrator",
	})
	if err == nil {
		t.Fatal("an unrecognised role was accepted")
	}

	// The field has to be named, so an owner who mistyped one is told which one. A refusal that
	// only said "some details need attention" would be indistinguishable from a bad address.
	var apiErr *httpx.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want a validation error in the contract's shape", err)
	}
	named := false
	for _, detail := range apiErr.Details {
		if detail.Field == "role" {
			named = true
		}
	}
	if !named {
		t.Errorf("the refusal does not name `role`: %+v", apiErr.Details)
	}
}

// TestOneAdministratorPerAddress is uq_admin_users_email reaching the domain as a legible refusal.
func TestOneAdministratorPerAddress(t *testing.T) {
	creds, _, _, _ := adminAuth(t)
	anAdministrator(t, creds, "duplicate@example.com", RoleSupport)

	_, err := creds.Create(t.Context(), CreateCommand{
		Email: "Duplicate@example.com", Name: "Another Person", Password: testPassword,
	})
	if !errors.Is(err, ErrAdminEmailTaken) {
		t.Errorf("error = %v, want ErrAdminEmailTaken", err)
	}
}

// TestASubjectOnTheContextIsNotAnAdministrator is the test SHIP-147's mutation sweep found missing,
// and the one that closes the *Done when*'s second clause where it is actually reachable.
//
// # Why the other tests in this file cannot see this
//
// They exercise [Authenticator.Resolve] directly, or they drive [RequireAdmin] over a bare request.
// Neither has an [authctx.Subject] on the context — but **every request the real service serves
// does**: `httpx.ResolveSubject` runs group-wide, outside the per-route guard (cmd/api/routes.go),
// and it never rejects. So a signed-in customer's subject is sitting on the context of every
// administrative request they make, waiting for somebody to read it.
//
// The mutation is five lines and entirely plausible: resolve the session, and *if that fails*, fall
// back to the subject already there. It is the "one helper function later" CLAUDE.md names, and it
// passed every Go test in the repository before this one existed. `make verify` caught it, because
// it presents a real mobile access token to `/v1/admin/me` through the real chain — but a defect
// that only a shell script can see is a defect that reaches a branch nobody ran the harness on.
//
// The subject here claims `role: admin`, which is the strongest form of the temptation: it is a
// claim `ck_users_role` does not permit and `identity` refuses to issue, so the only way one could
// exist is if somebody had already joined the two systems somewhere else.
func TestASubjectOnTheContextIsNotAnAdministrator(t *testing.T) {
	creds, auth, _, _ := adminAuth(t)
	anAdministrator(t, creds, "real@example.com", RoleOwner)

	guarded := RequireAdmin(auth)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		grant, _ := grantFrom(r.Context())
		t.Errorf("a request carrying only a mobile subject reached an administrator handler as "+
			"%s with role %q.\nCLAUDE.md: the credential systems are separate and neither can be "+
			"exchanged for the other. httpx.ResolveSubject puts a subject on every request in the "+
			"/v1 group, so a fallback to it hands the console to every signed-in account.",
			grant.Administrator.ID, grant.Administrator.Role)
		w.WriteHeader(http.StatusOK)
	}))

	for name, credential := range map[string]string{
		"with no credential at all":  "",
		"with an unrecognised one":   "Bearer " + strings.Repeat("Z", 43),
		"with a mobile access token": "Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.c2ln",
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/admin/me", nil)
			if credential != "" {
				req.Header.Set(httpx.HeaderAuthorization, credential)
			}

			// Exactly what the served chain does before the guard runs.
			req = req.WithContext(authctx.WithSubject(req.Context(), authctx.Subject{
				UserID:    "0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0",
				Role:      "admin",
				SessionID: "0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e1",
			}))

			rec := httptest.NewRecorder()
			guarded.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body)
			}
		})
	}
}

// TestASessionIsInternallyConsistentWhateverTheWallClockSays is the test that would have caught the
// defect this file shipped with, and it is written to be **independent of when it runs**.
//
// # What went wrong
//
// [postgresStore.insertSession] named the two expiries and not `created_at`, so `DEFAULT now()`
// filled the latter. The expiries came from the injected clock and the origin they are measured
// from came from PostgreSQL's — two clocks in one row, on either side of
// `ck_admin_sessions_idle_expiry`.
//
// Every test above fixes the clock at 09:00 UTC on the day they were written. While real time was
// before 09:30 the row satisfied the constraint and the suite was green; **after 09:30 every
// sign-in violated it**. The gate passed at 07:00 and failed at 10:40 on a tree nobody had touched.
// That is a time bomb rather than a flake: re-running never clears it, and the two runs disagree
// because the wall clock moved rather than because anything raced.
//
// # Why this test cannot rot the same way
//
// It signs in at a clock **far in the past and far in the future**, so at least one of the two is
// always on the wrong side of `now()` whenever the suite runs. A row that depended on the wall clock
// could not satisfy both, and no choice of "today" makes this pass by luck.
//
// It then asserts the stored `created_at` **is** the injected instant, which is the property the
// constraint actually rests on: the origin and the expiries measured from it come from one clock.
func TestASessionIsInternallyConsistentWhateverTheWallClockSays(t *testing.T) {
	creds, _, pool, clk := adminAuth(t)
	anAdministrator(t, creds, "clocks@example.com", RoleSupport)

	for name, instant := range map[string]time.Time{
		"long before now": time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC),
		"long after now":  time.Date(2039, 11, 12, 13, 14, 15, 0, time.UTC),
	} {
		t.Run(name, func(t *testing.T) {
			clk.Instant = instant

			issued, _, err := signIn(t, creds, "clocks@example.com", testPassword, "10.0.12.1")
			if err != nil {
				t.Fatalf("signing in with the clock at %s: %v\n"+
					"A session row must be consistent with the clock that produced it. A "+
					"timestamp left to a column default puts the database's clock on one side "+
					"of ck_admin_sessions_idle_expiry and the caller's on the other, and the "+
					"row then satisfies the constraint only while real time happens to fall "+
					"inside one idle window of the injected instant.", instant, err)
			}

			var createdAt, idleExpiresAt, absoluteExpiresAt, lastUsedAt time.Time
			if err := pool.QueryRow(t.Context(), `
				SELECT created_at, idle_expires_at, absolute_expires_at, last_used_at
				FROM admin_sessions WHERE id = $1`, issued.Session.ID,
			).Scan(&createdAt, &idleExpiresAt, &absoluteExpiresAt, &lastUsedAt); err != nil {
				t.Fatalf("reading the session row: %v", err)
			}

			// Every timestamp on the row comes from the one clock. `updated_at` deliberately
			// does not and is deliberately not read here — see the note below.
			for field, got := range map[string]time.Time{
				"created_at":          createdAt,
				"last_used_at":        lastUsedAt,
				"idle_expires_at":     idleExpiresAt.Add(-idleWindow),
				"absolute_expires_at": absoluteExpiresAt.Add(-absoluteLifetime),
			} {
				if !got.Equal(instant) {
					t.Errorf("%s implies an origin of %s, want the injected %s",
						field, got.UTC(), instant)
				}
			}
		})
	}
}

// TestOnlyTheBookkeepingColumnUsesTheDatabaseClock names the one timestamp that is meant to come
// from PostgreSQL, so that "which clock owns which column" is written down rather than rediscovered.
//
// `updated_at` answers *when did this row last change*, which is a fact about the write rather than
// about the session, and it is maintained by the `set_updated_at` trigger exactly as
// `device_sessions` maintains its own. **Nothing compares it to anything**, which is what makes it
// safe for it to disagree with the injected clock — and is precisely the property `created_at`
// lacked.
//
// The check is that a slide moves it. If a later change ever put `updated_at` into a constraint or
// into a lifetime, this test would still pass and the one above would start failing, which is the
// right way round.
func TestOnlyTheBookkeepingColumnUsesTheDatabaseClock(t *testing.T) {
	creds, auth, pool, clk := adminAuth(t)
	anAdministrator(t, creds, "bookkeeping@example.com", RoleSupport)

	issued, _, err := signIn(t, creds, "bookkeeping@example.com", testPassword, "10.0.13.1")
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}

	var before time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT updated_at FROM admin_sessions WHERE id = $1`, issued.Session.ID).Scan(&before); err != nil {
		t.Fatalf("reading updated_at: %v", err)
	}

	// Far enough that the slide is worth a write (see slideGranularity).
	clk.Advance(5 * time.Minute)
	if _, err := auth.Resolve(t.Context(), issued.Token); err != nil {
		t.Fatalf("resolving: %v", err)
	}

	var after time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT updated_at FROM admin_sessions WHERE id = $1`, issued.Session.ID).Scan(&after); err != nil {
		t.Fatalf("reading updated_at: %v", err)
	}

	if !after.After(before) {
		t.Errorf("updated_at did not move when the session slid: %s then %s.\n"+
			"It is the trigger's column and it answers when the row last changed.", before, after)
	}
}
