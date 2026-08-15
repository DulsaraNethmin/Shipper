package identity

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/passwords"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/ratelimit"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/redistest"
)

// SHIP-41 against a real PostgreSQL, per Docs/06 §4.1.
//
// What sign-in claims is about rows: a session exists afterwards, the account it names is the one
// whose password was presented, and the credential column has been rewritten when the profile has
// moved on. A mocked store reports every one of those as passing while proving none of them.

// testProfile is the reduced profile Docs/10 §5 calls for: the production one is 64 MiB per hash,
// and this package builds dozens of hashers across tests that `go test ./...` runs beside other
// packages.
//
// It lived in password_test.go until SHIP-15r moved the hashing to internal/passwords, and it did
// not move with it — every test here that needs a service needs a cheap hasher, so the fixture
// belongs beside the constructor that takes one. internal/passwords keeps its own copy for its own
// tests, and the two are deliberately independent: a package's test fixture is not an interface
// between packages.
var testProfile = passwords.Argon2Profile{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1}

// signInService builds a service over a hasher at the given profile, and registers one account
// through the real path so the credential it verifies is one this package wrote.
func signInService(t *testing.T, profile passwords.Argon2Profile) (*Service, *pgxpool.Pool, User) {
	t.Helper()

	pool := pgtest.DB(t)

	hasher, err := passwords.NewHasher(profile)
	if err != nil {
		t.Fatalf("building the hasher: %v", err)
	}

	svc, err := NewService(pool, hasher, testServiceIssuer(t, clock.System{}), testLimiter(t),
		&recordingSender{}, &recordingTexter{}, clock.System{})
	if err != nil {
		t.Fatalf("building the service: %v", err)
	}

	user, err := svc.Register(t.Context(), validRegistration())
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	return svc, pool, user
}

func validSignIn() SignInCommand {
	return SignInCommand{
		Email:       validRegistration().Email,
		Password:    validRegistration().Password,
		DeviceLabel: "Nethmin's iPhone",

		// SHIP-47 counts per address as well as per account, and refuses a command that
		// carries none — an address the platform cannot determine is not an address with
		// no limit. Tests supply one for the same reason a client does.
		ClientIP: "203.0.113.7",
	}
}

// testLimiter builds SHIP-47's token bucket over the real Redis, in a namespace of its own.
//
// A limiter per test rather than a shared one, because these are buckets: two tests sharing a
// namespace would have the first one's failed sign-ins throttle the second's, which reads as an
// unrelated test being flaky. redistest.Client gives the prefix and removes the keys afterwards.
//
// Real Redis rather than a fake, per Docs/10 §7.2 and for the reason internal/ratelimit's own
// tests give: what is being relied on is Redis running a script to completion.
func testLimiter(t *testing.T) *ratelimit.Limiter {
	t.Helper()

	client, prefix := redistest.Client(t)

	limiter, err := ratelimit.New(client, prefix, clock.System{})
	if err != nil {
		t.Fatalf("building the rate limiter: %v", err)
	}
	return limiter
}

// TestSignInReturnsAPairAndCreatesTheDevice is SHIP-41's acceptance criterion: the endpoint
// returns an access and refresh token pair. Both halves are checked to be *usable* rather than
// merely present — an access token that does not verify and a refresh token that cannot be
// exchanged would satisfy a shallower reading of the criterion.
func TestSignInReturnsAPairAndCreatesTheDevice(t *testing.T) {
	svc, pool, user := signInService(t, testProfile)

	pair, err := svc.SignIn(t.Context(), validSignIn())
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}

	t.Run("the access token verifies and names the caller and the device", func(t *testing.T) {
		verifier, err := NewAccessTokenVerifier(testKeyset(t, kidOne), clock.System{})
		if err != nil {
			t.Fatalf("building a verifier: %v", err)
		}
		claims, err := verifier.Verify(pair.Access.Value)
		if err != nil {
			t.Fatalf("the issued access token does not verify: %v", err)
		}
		if claims.Subject != user.ID.String() {
			t.Errorf("sub = %q, want %q", claims.Subject, user.ID)
		}
		if claims.SessionID != pair.SessionID.String() {
			t.Errorf("sid = %q, want %q — the token must name the device it was issued to, "+
				"or signing one device out cannot reach its access tokens", claims.SessionID, pair.SessionID)
		}
	})

	t.Run("the refresh token is one the platform will exchange", func(t *testing.T) {
		next, err := svc.Refresh(t.Context(), pair.Refresh.Value)
		if err != nil {
			t.Fatalf("the refresh token sign-in issued was refused: %v", err)
		}
		if next.SessionID != pair.SessionID {
			t.Errorf("refreshing moved the caller to session %s, want %s", next.SessionID, pair.SessionID)
		}
	})

	t.Run("the device session is the caller's, and carries the label they sent", func(t *testing.T) {
		var (
			owner uuid.UUID
			label string
		)
		if err := pool.QueryRow(t.Context(),
			`SELECT user_id, device_label FROM device_sessions WHERE id = $1`,
			pair.SessionID).Scan(&owner, &label); err != nil {
			t.Fatalf("reading the device session: %v", err)
		}
		if owner != user.ID {
			t.Errorf("the session belongs to %s, want %s", owner, user.ID)
		}
		if label != "Nethmin's iPhone" {
			t.Errorf("device_label = %q, want the label that was sent", label)
		}
	})

	t.Run("the refresh token is stored hashed and nowhere in plain form", func(t *testing.T) {
		var plain int
		if err := pool.QueryRow(t.Context(),
			`SELECT count(*) FROM device_sessions WHERE refresh_token_hash = $1`,
			pair.Refresh.Value).Scan(&plain); err != nil {
			t.Fatalf("searching for the plain token: %v", err)
		}
		if plain != 0 {
			t.Error("the refresh token itself is in device_sessions; only its hash may be")
		}
	})
}

// TestSignInIsNotAnAccountExistenceOracle. The two failures a caller can provoke without holding
// anything must be indistinguishable, or an unauthenticated endpoint answers "does this address
// have an account" for any address anybody cares to try.
func TestSignInIsNotAnAccountExistenceOracle(t *testing.T) {
	svc, _, _ := signInService(t, testProfile)

	wrongPassword := validSignIn()
	wrongPassword.Password = "not-the-password-that-was-registered"

	noSuchAccount := validSignIn()
	noSuchAccount.Email = "nobody@example.com"

	for name, cmd := range map[string]SignInCommand{
		"the wrong password": wrongPassword,
		"no such account":    noSuchAccount,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.SignIn(t.Context(), cmd)
			if !errors.Is(err, ErrCredentialsInvalid) {
				t.Fatalf("err = %v, want ErrCredentialsInvalid", err)
			}
			// Through the transport as well, because it is the *code* a client sees and
			// two sentinels mapped to two codes would disclose exactly what one sentinel
			// was hiding.
			var apiErr *httpx.Error
			if !errors.As(apiError(err), &apiErr) {
				t.Fatalf("apiError produced %T, want *httpx.Error", apiError(err))
			}
			if apiErr.Code != CodeCredentialsInvalid {
				t.Errorf("code = %q, want %q", apiErr.Code, CodeCredentialsInvalid)
			}
			if apiErr.Status != 400 {
				t.Errorf("status = %d, want 400 — a body-borne credential is refused with "+
					"400 across this domain, and 401 sends SHIP-50's interceptor round its loop",
					apiErr.Status)
			}
		})
	}
}

// TestSignInSpendsTheSameWorkWhetherOrNotTheAccountExists is the timing half of the same
// disclosure, and the half a plausible implementation leaves open.
//
// One code for both failures is worth nothing if the response time separates them: argon2id costs
// tens of milliseconds and a lookup that misses costs none of them. The comparison is against the
// wrong-password path rather than against an absolute figure, so it holds at any profile.
//
// Mutation-checked: emptying SpendEquivalentWork's body makes the unknown-account path finish in
// microseconds and this fails.
func TestSignInSpendsTheSameWorkWhetherOrNotTheAccountExists(t *testing.T) {
	if testing.Short() {
		t.Skip("times two argon2id derivations; -short is for the runs that skip infrastructure")
	}

	hasher, err := passwords.NewHasher(testProfile)
	if err != nil {
		t.Fatalf("building the hasher: %v", err)
	}
	stored, err := hasher.Hash("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}

	// Medians rather than single readings: this runs beside other packages under `go test
	// ./... -race`, and one descheduled sample would otherwise decide the result.
	verify := medianDuration(t, func() {
		if _, err := hasher.Verify(stored, "the-wrong-password"); err != nil {
			t.Fatalf("verifying: %v", err)
		}
	})
	spend := medianDuration(t, func() { hasher.SpendEquivalentWork("the-wrong-password") })

	// A quarter, not a half: the true ratio is one — both are a single derivation at the same
	// profile — and the margin is there so a loaded machine cannot fail an honest build. What
	// it still catches is the defect, which is a path that does no work at all.
	if spend < verify/4 {
		t.Errorf("an unknown account costs %s and a wrong password costs %s.\n"+
			"The response time answers what CodeCredentialsInvalid refuses to: a caller can "+
			"time two requests and learn which addresses have accounts.", spend, verify)
	}
}

func medianDuration(t *testing.T, fn func()) time.Duration {
	t.Helper()

	const samples = 5
	var taken [samples]time.Duration
	for i := range taken {
		start := time.Now()
		fn()
		taken[i] = time.Since(start)
	}

	for i := 1; i < samples; i++ {
		for j := i; j > 0 && taken[j] < taken[j-1]; j-- {
			taken[j], taken[j-1] = taken[j-1], taken[j]
		}
	}
	return taken[samples/2]
}

// TestASuspendedAccountIsToldSo, and only after the password verified.
//
// This is the one place account standing is disclosed. Refresh deliberately does not disclose it
// — the caller there holds a token, which may not be theirs — and the difference is that here
// they have just proved they own the account.
func TestASuspendedAccountIsToldSo(t *testing.T) {
	svc, pool, user := signInService(t, testProfile)

	if _, err := pool.Exec(t.Context(),
		`UPDATE users SET status = 'suspended' WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("suspending the account: %v", err)
	}

	t.Run("the right password is told the account is suspended", func(t *testing.T) {
		_, err := svc.SignIn(t.Context(), validSignIn())
		if !errors.Is(err, ErrAccountSuspended) {
			t.Fatalf("err = %v, want ErrAccountSuspended", err)
		}

		var apiErr *httpx.Error
		if !errors.As(apiError(err), &apiErr) {
			t.Fatalf("apiError produced %T, want *httpx.Error", apiError(err))
		}
		if apiErr.Status != 403 {
			t.Errorf("status = %d, want 403 — the caller is known and is not permitted", apiErr.Status)
		}
	})

	t.Run("the wrong password is told nothing about the account", func(t *testing.T) {
		cmd := validSignIn()
		cmd.Password = "not-the-password-that-was-registered"

		_, err := svc.SignIn(t.Context(), cmd)
		if !errors.Is(err, ErrCredentialsInvalid) {
			t.Fatalf("err = %v, want ErrCredentialsInvalid — account standing was disclosed "+
				"to somebody who has not proved they own the account", err)
		}
	})

	t.Run("no session was created", func(t *testing.T) {
		var sessions int
		if err := pool.QueryRow(t.Context(),
			`SELECT count(*) FROM device_sessions WHERE user_id = $1`, user.ID).Scan(&sessions); err != nil {
			t.Fatalf("counting sessions: %v", err)
		}
		if sessions != 0 {
			t.Errorf("%d sessions exist for a suspended account, want 0", sessions)
		}
	})
}

// TestSignInUpgradesAPasswordHashedAtAWeakerProfile.
//
// Docs/10 §5 claims the argon2id cost can be raised without a migration and without asking anyone
// to reset anything, and internal/passwords' NeedsRehash names sign-in as where that happens. This is
// the test that makes the claim true rather than available: the account registers under one
// profile, signs in under a stronger one, and the stored hash has moved.
//
// Mutation-checked: removing the upgradeStoredPassword call leaves the stored hash at the weaker
// profile and the first subtest fails.
func TestSignInUpgradesAPasswordHashedAtAWeakerProfile(t *testing.T) {
	weaker := passwords.Argon2Profile{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1}
	stronger := passwords.Argon2Profile{MemoryKiB: 8 * 1024, Iterations: 2, Parallelism: 1}

	svc, pool, user := signInService(t, weaker)

	before := storedPasswordHash(t, pool, user)
	if !hasCost(before, "t=1") {
		t.Fatalf("the account was not registered at the weaker profile: %s", before)
	}

	// The same service, now writing at the stronger profile — which is what a deployment that
	// raised the cost looks like from the database's side.
	hasher, err := passwords.NewHasher(stronger)
	if err != nil {
		t.Fatalf("building the stronger hasher: %v", err)
	}
	svc.hasher = hasher

	if _, err := svc.SignIn(t.Context(), validSignIn()); err != nil {
		t.Fatalf("signing in: %v", err)
	}

	t.Run("the stored hash has been rewritten at the current profile", func(t *testing.T) {
		after := storedPasswordHash(t, pool, user)
		if !hasCost(after, "t=2") {
			t.Errorf("password_hash is still %s.\nDocs/10 §5's claim that the profile can be "+
				"raised without a migration only holds if sign-in takes the opportunity.", after)
		}
	})

	t.Run("the password still verifies against the rewritten hash", func(t *testing.T) {
		if _, err := svc.SignIn(t.Context(), validSignIn()); err != nil {
			t.Fatalf("the upgraded credential no longer accepts the password: %v", err)
		}
	})

	t.Run("a second sign-in leaves it alone", func(t *testing.T) {
		first := storedPasswordHash(t, pool, user)
		if _, err := svc.SignIn(t.Context(), validSignIn()); err != nil {
			t.Fatalf("signing in: %v", err)
		}
		if second := storedPasswordHash(t, pool, user); second != first {
			t.Error("the hash was rewritten again on a sign-in that needed no upgrade, which " +
				"is a write on every sign-in rather than on the first after a profile change")
		}
	})
}

// TestSignInValidatesEveryFieldAtOnce. Docs/10 §4.6: a form answered one error at a time takes
// one round trip per field, on a phone, in a truck yard.
func TestSignInValidatesEveryFieldAtOnce(t *testing.T) {
	svc, _, _ := signInService(t, testProfile)

	_, err := svc.SignIn(t.Context(), SignInCommand{})

	var apiErr *httpx.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v (%T), want a validation error", err, err)
	}

	named := map[string]bool{}
	for _, d := range apiErr.Details {
		named[d.Field] = true
	}
	for _, want := range []string{"email", "password", "device_label"} {
		if !named[want] {
			t.Errorf("the validation error does not name %q; a client cannot put the message "+
				"beside the input that caused it. Got %v", want, named)
		}
	}
}

// TestSignInDoesNotApplyTheRegistrationPasswordLength.
//
// minPasswordLength is a rule about what may be *chosen*. Applying it at sign-in would refuse
// every account whose password predates the last time the floor moved — an account that still has
// that password, told at the sign-in screen that it is too short, with no way to act on it.
func TestSignInDoesNotApplyTheRegistrationPasswordLength(t *testing.T) {
	svc, pool, user := signInService(t, testProfile)

	hasher, err := passwords.NewHasher(testProfile)
	if err != nil {
		t.Fatalf("building the hasher: %v", err)
	}
	short, err := hasher.Hash("short")
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`UPDATE users SET password_hash = $2 WHERE id = $1`, user.ID, short); err != nil {
		t.Fatalf("planting the short password: %v", err)
	}

	cmd := validSignIn()
	cmd.Password = "short"

	if _, err := svc.SignIn(t.Context(), cmd); err != nil {
		t.Fatalf("a password below the current registration floor was refused at sign-in: %v", err)
	}
}

// TestSignInNormalisesTheAddressAndTheLabel. The address is compared in the shape it is stored
// in, and the label is trimmed before the column's own length check sees it.
func TestSignInNormalisesTheAddressAndTheLabel(t *testing.T) {
	svc, pool, _ := signInService(t, testProfile)

	cmd := validSignIn()
	cmd.Email = "  Alice@Example.COM  "
	cmd.DeviceLabel = "  Nethmin's Pixel  "

	pair, err := svc.SignIn(t.Context(), cmd)
	if err != nil {
		t.Fatalf("signing in with a padded address: %v", err)
	}

	var label string
	if err := pool.QueryRow(t.Context(),
		`SELECT device_label FROM device_sessions WHERE id = $1`, pair.SessionID).Scan(&label); err != nil {
		t.Fatalf("reading the device session: %v", err)
	}
	if label != "Nethmin's Pixel" {
		t.Errorf("device_label = %q, want it trimmed", label)
	}
}

// TestSignInWithoutADatabaseIsUnavailable, for the reason Register's equivalent gives: 503 tells
// a mobile client to retry and 500 tells it to give up.
func TestSignInWithoutADatabaseIsUnavailable(t *testing.T) {
	svc := serviceWithoutADatabase(t)

	if _, err := svc.SignIn(t.Context(), validSignIn()); !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v, want errUnavailable", err)
	}
}

// serviceWithoutADatabase builds a service over a nil pool, which is a state the process starts in
// deliberately — see the note on Deps in cmd/api. Every path has to answer errUnavailable rather
// than dereferencing it.
func serviceWithoutADatabase(t *testing.T) *Service {
	t.Helper()

	hasher, err := passwords.NewHasher(testProfile)
	if err != nil {
		t.Fatalf("building the hasher: %v", err)
	}
	svc, err := NewService(nil, hasher, testServiceIssuer(t, clock.System{}), testLimiter(t),
		&recordingSender{}, &recordingTexter{}, clock.System{})
	if err != nil {
		t.Fatalf("building the service: %v", err)
	}
	return svc
}

func storedPasswordHash(t *testing.T, pool *pgxpool.Pool, user User) string {
	t.Helper()

	var hash string
	if err := pool.QueryRow(t.Context(),
		`SELECT password_hash FROM users WHERE id = $1`, user.ID).Scan(&hash); err != nil {
		t.Fatalf("reading the stored password hash: %v", err)
	}
	return hash
}

// hasCost reports whether a PHC string carries the given cost parameter — `$argon2id$v=19$m=…`,
// field four. Read out of the stored form rather than through parsePHC, so the test would still
// notice a parser that had started agreeing with a bug.
func hasCost(encoded, want string) bool {
	fields := strings.Split(encoded, "$")
	if len(fields) < 5 {
		return false
	}
	return slices.Contains(strings.Split(fields[3], ","), want)
}

// SHIP-47: the two limits on sign-in, against the real Redis.
//
// "Repeated failures are throttled per account and per IP" is two claims, and each has an
// independence half that the other cannot show: filling one account's bucket must not refuse
// another account from the same address, and filling one address's bucket must not refuse that
// account from somewhere else. A test that only counts to the limit passes with either bucket
// missing.

// failSignIn presents the wrong password once and returns whatever came back.
func failSignIn(t *testing.T, svc *Service, cmd SignInCommand) error {
	t.Helper()

	cmd.Password = "not-the-password-that-was-registered"
	_, err := svc.SignIn(t.Context(), cmd)
	if err == nil {
		t.Fatal("a wrong password was accepted")
	}
	return err
}

// throttledAfter reports how many attempts were admitted before the limiter refused, running at
// most limit of them.
func throttledAfter(t *testing.T, svc *Service, cmd SignInCommand, limit int) int {
	t.Helper()

	for i := range limit {
		if errors.As(failSignIn(t, svc, cmd), new(*ThrottledError)) {
			return i
		}
	}
	return limit
}

// TestRepeatedFailuresAreThrottledPerAccount is the first half of SHIP-47's criterion.
func TestRepeatedFailuresAreThrottledPerAccount(t *testing.T) {
	svc, _, _ := signInService(t, testProfile)

	admitted := throttledAfter(t, svc, validSignIn(), signInAccountCapacity+5)
	if admitted != signInAccountCapacity {
		t.Fatalf("%d failures were admitted before the throttle, want the capacity of %d",
			admitted, signInAccountCapacity)
	}

	t.Run("the refusal says when to come back", func(t *testing.T) {
		var throttled *ThrottledError
		if !errors.As(failSignIn(t, svc, validSignIn()), &throttled) {
			t.Fatal("the attempt after the limit was not throttled")
		}
		if throttled.RetryAfter <= 0 || throttled.RetryAfter > signInAccountInterval {
			t.Errorf("RetryAfter = %s, want (0, %s]", throttled.RetryAfter, signInAccountInterval)
		}
	})

	t.Run("it is a 429 with the rate-limited code", func(t *testing.T) {
		err := failSignIn(t, svc, validSignIn())

		var apiErr *httpx.Error
		if !errors.As(apiError(err), &apiErr) {
			t.Fatalf("apiError produced %T, want *httpx.Error", apiError(err))
		}
		if apiErr.Status != 429 || apiErr.Code != httpx.CodeRateLimited {
			t.Errorf("status = %d code = %q, want 429 and %q",
				apiErr.Status, apiErr.Code, httpx.CodeRateLimited)
		}
	})

	t.Run("and the right password is refused too", func(t *testing.T) {
		// The limit is checked before the credential, which is the point: what it protects
		// is the argon2id derivation and the database round trip. A throttle that let a
		// correct password through would be a throttle an attacker escapes by guessing right.
		_, err := svc.SignIn(t.Context(), validSignIn())
		if !errors.As(err, new(*ThrottledError)) {
			t.Errorf("err = %v, want a throttle — the limit is checked before the credential", err)
		}
	})
}

// TestOneAccountsFailuresDoNotThrottleAnother, from the same address.
//
// Mutation-checked: keying the account bucket on something shared — the address, a constant —
// makes this fail. Without it, one account being guessed at locks out everybody on the same
// network.
func TestOneAccountsFailuresDoNotThrottleAnother(t *testing.T) {
	svc, _, _ := signInService(t, testProfile)

	other, err := svc.Register(t.Context(), RegisterCommand{
		Name:     "Bec Okafor",
		Email:    "other@example.com",
		Phone:    "0412 345 679",
		Password: "correct-horse-battery-staple",
		Role:     RoleProvider,
	})
	if err != nil {
		t.Fatalf("registering the second account: %v", err)
	}

	if admitted := throttledAfter(t, svc, validSignIn(), signInAccountCapacity+3); admitted != signInAccountCapacity {
		t.Fatalf("%d failures admitted, want %d", admitted, signInAccountCapacity)
	}

	second := validSignIn()
	second.Email = other.Email

	if _, err := svc.SignIn(t.Context(), second); err != nil {
		t.Fatalf("a second account on the same address could not sign in: %v", err)
	}
}

// TestRepeatedFailuresAreThrottledPerAddress is the other half, and the one an attacker working
// through a list of addresses runs into.
//
// Each attempt uses an address of its own, so every per-account bucket stays full and the only
// thing that can refuse is the per-address limit.
func TestRepeatedFailuresAreThrottledPerAddress(t *testing.T) {
	svc, _, _ := signInService(t, testProfile)

	const from = "198.51.100.4"

	admitted := 0
	for i := range signInAddressCapacity + 5 {
		cmd := validSignIn()
		cmd.Email = fmt.Sprintf("nobody-%d@example.com", i)
		cmd.ClientIP = from

		if errors.As(failSignIn(t, svc, cmd), new(*ThrottledError)) {
			break
		}
		admitted++
	}

	if admitted != signInAddressCapacity {
		t.Fatalf("%d attempts were admitted from one address, want the capacity of %d",
			admitted, signInAddressCapacity)
	}

	t.Run("the account it was working through is refused from that address", func(t *testing.T) {
		cmd := validSignIn()
		cmd.ClientIP = from

		if _, err := svc.SignIn(t.Context(), cmd); !errors.As(err, new(*ThrottledError)) {
			t.Errorf("err = %v, want a throttle", err)
		}
	})

	t.Run("but not from anywhere else", func(t *testing.T) {
		// Mutation-checked: dropping the address from the key makes this fail, because one
		// exhausted address would then throttle the whole platform.
		cmd := validSignIn()
		cmd.ClientIP = "198.51.100.5"

		if _, err := svc.SignIn(t.Context(), cmd); err != nil {
			t.Errorf("a different address was throttled by another's failures: %v", err)
		}
	})
}

// TestSuccessfulSignInsAreNotThrottled. The limit counts *failures*, so somebody who knows their
// password is never refused for using it — which is what stops a shared device or a busy office
// running into a control aimed at guessing.
func TestSuccessfulSignInsAreNotThrottled(t *testing.T) {
	svc, _, _ := signInService(t, testProfile)

	for i := range signInAccountCapacity * 3 {
		if _, err := svc.SignIn(t.Context(), validSignIn()); err != nil {
			t.Fatalf("sign-in %d was refused: %v", i, err)
		}
	}
}

// TestASuspendedAccountIsNotCountedAsAGuess. Its owner has just proved they own it, so refusing
// them is not evidence of anything and throttling them would leave somebody locked out of the
// endpoint that tells them why.
func TestASuspendedAccountIsNotCountedAsAGuess(t *testing.T) {
	svc, pool, user := signInService(t, testProfile)

	if _, err := pool.Exec(t.Context(),
		`UPDATE users SET status = 'suspended' WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("suspending the account: %v", err)
	}

	for i := range signInAccountCapacity + 3 {
		_, err := svc.SignIn(t.Context(), validSignIn())
		if !errors.Is(err, ErrAccountSuspended) {
			t.Fatalf("attempt %d answered %v, want ErrAccountSuspended — being suspended was "+
				"counted as an attempt at somebody's password", i, err)
		}
	}
}

// TestSignInWithoutARateLimiterCacheIsRefused is the fail-closed direction, through the domain.
//
// A limiter that failed open would be one an attacker turns off by taking Redis down, on the
// endpoint the limiter exists to protect. It is a 503 rather than a 429 because "this is
// temporarily unavailable" is true and "you have done too much" is not.
func TestSignInWithoutARateLimiterCacheIsRefused(t *testing.T) {
	pool := pgtest.DB(t)

	hasher, err := passwords.NewHasher(testProfile)
	if err != nil {
		t.Fatalf("building the hasher: %v", err)
	}
	limiter, err := ratelimit.New(nil, "unreachable:", clock.System{})
	if err != nil {
		t.Fatalf("building the limiter: %v", err)
	}
	svc, err := NewService(pool, hasher, testServiceIssuer(t, clock.System{}), limiter,
		&recordingSender{}, &recordingTexter{}, clock.System{})
	if err != nil {
		t.Fatalf("building the service: %v", err)
	}
	if _, err := svc.Register(t.Context(), validRegistration()); err != nil {
		t.Fatalf("registering: %v", err)
	}

	_, err = svc.SignIn(t.Context(), validSignIn())
	if !errors.Is(err, errUnavailable) {
		t.Fatalf("err = %v, want errUnavailable — an unreachable cache let a sign-in through "+
			"with nothing counting the attempts", err)
	}

	var apiErr *httpx.Error
	if !errors.As(apiError(err), &apiErr) {
		t.Fatalf("apiError produced %T, want *httpx.Error", apiError(err))
	}
	if apiErr.Status != 503 {
		t.Errorf("status = %d, want 503 rather than 429", apiErr.Status)
	}
}

// TestSignInWithNoClientAddressIsRefused. An address the platform could not determine is not an
// address with no limit, and defaulting to one shared bucket would be a limit anybody escapes by
// arriving over a transport that does not report one.
func TestSignInWithNoClientAddressIsRefused(t *testing.T) {
	svc, _, _ := signInService(t, testProfile)

	cmd := validSignIn()
	cmd.ClientIP = ""

	if _, err := svc.SignIn(t.Context(), cmd); err == nil {
		t.Fatal("a sign-in with no client address was accepted, so it counted against nothing")
	}
}
