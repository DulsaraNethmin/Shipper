package delivery

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-107 — the driver's job-scoped token.
//
// The *Done when* is four words and each one is a test below: **signed**, **time-limited**,
// **single-job**, **generated on assignment**. The last of those is the only one that needs a
// database, and it needs one because "on assignment" is a claim about where the token comes from
// rather than about what it contains.
//
// # This file imports internal/identity, which non-test code in this package may not
//
// internal/boundaries skips test files for the reason assignment_test.go gives, and here it buys
// something the lint could not: the separation between the two token systems is a statement about
// *both*, so proving it needs both in one process. A hand-built token proves less — identity's own
// TestAccessTokenWithTheDriverAudienceIsRejected constructs a driver-audience token by hand because
// it cannot import this package, and says so.
//
// Both directions are exercised here with **the key material deliberately shared**, which is the
// worst case and the only one where the audience is doing the work alone. Configuration refuses that
// arrangement in a real deployment (internal/config), so this is a case that cannot occur and is
// tested precisely because the guarantee should not depend on that.

const (
	testDriverKID      = "driver-test"
	testDriverRotated  = "driver-test-outgoing"
	testDriverTokenTTL = 7 * 24 * time.Hour
)

var (
	testDriverKey      = []byte("a-driver-token-signing-key-of-at-least-32-bytes")
	testDriverOutgoing = []byte("the-previous-driver-token-key-also-32-bytes-long")
	testUnrelatedKey   = []byte("a-key-from-somewhere-else-entirely-32-bytes-plus")
	testDriverIssuedAt = time.Date(2026, 8, 12, 3, 30, 0, 0, time.UTC)
)

// testDriverKeyset is the keyset every test below signs with: the active key and one rotated-out
// key, so that rotation is exercised rather than asserted.
func testDriverKeyset(t *testing.T) *Keyset {
	t.Helper()

	keys, err := NewKeyset(map[string][]byte{
		testDriverKID:     testDriverKey,
		testDriverRotated: testDriverOutgoing,
	}, testDriverKID)
	if err != nil {
		t.Fatalf("building the driver keyset: %v", err)
	}
	return keys
}

// testDriverIssuer is the issuer the service fixtures are built with.
//
// It takes no *testing.T because newTestService has none to give it, so a failure here panics rather
// than failing a named test — which is the correct trade for a helper whose inputs are constants in
// this file.
func testDriverIssuer(clk clock.Clock) *DriverTokenIssuer {
	keys, err := NewKeyset(map[string][]byte{
		testDriverKID:     testDriverKey,
		testDriverRotated: testDriverOutgoing,
	}, testDriverKID)
	if err != nil {
		panic("delivery tests: building the driver keyset: " + err.Error())
	}
	if clk == nil {
		clk = clock.NewFixed(testDriverIssuedAt)
	}

	issuer, err := NewDriverTokenIssuer(keys, testDriverTokenTTL, clk)
	if err != nil {
		panic("delivery tests: building the driver token issuer: " + err.Error())
	}
	return issuer
}

// newIssuer is the issuer with a clock the test controls, for the expiry cases.
func newIssuer(t *testing.T, clk clock.Clock) *DriverTokenIssuer {
	t.Helper()

	issuer, err := NewDriverTokenIssuer(testDriverKeyset(t), testDriverTokenTTL, clk)
	if err != nil {
		t.Fatalf("building the driver token issuer: %v", err)
	}
	return issuer
}

// payloadOf decodes a token's claims without verifying anything, which is how a test asserts what is
// *on the wire* rather than what the struct would have marshalled.
//
// The distinction matters for the closed-claim-set test: a `sub` that the parser ignores would still
// be a `sub` a future verifier could read.
func payloadOf(t *testing.T, raw string) map[string]any {
	t.Helper()

	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		t.Fatalf("a signed token has three parts, got %d", len(parts))
	}

	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decoding the payload: %v", err)
	}

	var claims map[string]any
	if err := json.Unmarshal(decoded, &claims); err != nil {
		t.Fatalf("parsing the payload: %v", err)
	}
	return claims
}

// headerOf is the same for the header, which is where the key identifier lives.
func headerOf(t *testing.T, raw string) map[string]any {
	t.Helper()

	decoded, err := base64.RawURLEncoding.DecodeString(strings.Split(raw, ".")[0])
	if err != nil {
		t.Fatalf("decoding the header: %v", err)
	}

	var header map[string]any
	if err := json.Unmarshal(decoded, &header); err != nil {
		t.Fatalf("parsing the header: %v", err)
	}
	return header
}

// TestADriverTokenNamesOneJobAndCarriesNothingElse is the claim set, held closed.
//
// The list is asserted as a set of *keys on the wire* rather than field by field, for the reason
// SHIP-83's budget test gives about closed sets: checking that `sub` is absent catches `sub` and
// misses `user_id`. Seven keys, and a new one fails this test — which is the point, because the
// three that must never appear are the three an authenticated subject is built from.
func TestADriverTokenNamesOneJobAndCarriesNothingElse(t *testing.T) {
	issuer := newIssuer(t, clock.NewFixed(testDriverIssuedAt))
	jobID, assignmentID := uuid.New(), uuid.New()

	token, err := issuer.Issue(jobID, assignmentID)
	if err != nil {
		t.Fatalf("Issue() = %v", err)
	}

	claims := payloadOf(t, token.Value)

	var got []string
	for key := range claims {
		got = append(got, key)
	}
	sort.Strings(got)

	want := []string{"assignment_id", "aud", "exp", "iat", "iss", "job_id", "jti"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the claim set is %v, want exactly %v", got, want)
	}

	if claims["job_id"] != jobID.String() {
		t.Errorf("job_id = %v, want %s", claims["job_id"], jobID)
	}
	if claims["assignment_id"] != assignmentID.String() {
		t.Errorf("assignment_id = %v, want %s", claims["assignment_id"], assignmentID)
	}
	if claims["aud"] != DriverAudience {
		t.Errorf("aud = %v, want %s", claims["aud"], DriverAudience)
	}
	if claims["iss"] != IssuerName {
		t.Errorf("iss = %v, want %s", claims["iss"], IssuerName)
	}

	// Stated separately from the closed set above, because this is the invariant rather than a
	// consequence of it: an authctx.Subject is built from a user id, and there is none here to
	// build one from.
	for _, forbidden := range []string{"sub", "role", "sid"} {
		if _, present := claims[forbidden]; present {
			t.Errorf("the driver token carries %q, which is what a mobile session is built from", forbidden)
		}
	}

	if headerOf(t, token.Value)["kid"] != testDriverKID {
		t.Errorf("kid = %v, want %s", headerOf(t, token.Value)["kid"], testDriverKID)
	}
	if headerOf(t, token.Value)["alg"] != "HS256" {
		t.Errorf("alg = %v, want HS256", headerOf(t, token.Value)["alg"])
	}
}

// TestADriverTokenIsTimeLimited is the second word of the *Done when*.
//
// Both halves: the window is exactly the configured one, and the token stops verifying at the end of
// it. The second needs a clock that can be moved, which is why the issuer takes one (Docs/10 §6.3) —
// waiting seven days is not a test.
func TestADriverTokenIsTimeLimited(t *testing.T) {
	clk := clock.NewFixed(testDriverIssuedAt)
	issuer := newIssuer(t, clk)

	token, err := issuer.Issue(uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("Issue() = %v", err)
	}

	if got := token.ExpiresAt.Sub(testDriverIssuedAt); got != testDriverTokenTTL {
		t.Errorf("the token is valid for %s, want %s", got, testDriverTokenTTL)
	}

	claims := payloadOf(t, token.Value)
	iat, exp := claims["iat"].(float64), claims["exp"].(float64)
	if want := testDriverTokenTTL.Seconds(); exp-iat != want {
		t.Errorf("exp - iat = %v seconds, want %v", exp-iat, want)
	}

	t.Run("still valid a second before it expires", func(t *testing.T) {
		clk.Instant = token.ExpiresAt.Add(-time.Second)
		if _, err := issuer.parse(token.Value); err != nil {
			t.Errorf("a token one second short of expiry was refused: %v", err)
		}
	})

	t.Run("refused once it has expired", func(t *testing.T) {
		// One second past, not one nanosecond: `exp` is a count of seconds and the library
		// compares whole ones, so a nanosecond over is the same second and legitimately
		// still valid.
		clk.Instant = token.ExpiresAt.Add(time.Second)

		_, err := issuer.parse(token.Value)
		if err == nil {
			t.Fatal("an expired driver token was accepted")
		}
		// Distinguishably expired, which SHIP-108 needs in order to tell a driver whose link
		// has lapsed ("ask for a new one") from one presenting rubbish.
		if !errors.Is(err, jwt.ErrTokenExpired) {
			t.Errorf("expected the expiry check to refuse it, got: %v", err)
		}
	})
}

// TestADriverTokenCannotBeRepointedAtAnotherJob is the third word, and it is the invariant the
// ticket exists for: the token grants exactly one job.
//
// The test does what an attacker holding a valid link would do — rewrite the job it names and
// re-encode — and the signature is what refuses it. Asserting that the claim contains one job id
// would prove nothing on its own; what makes it *single-job* is that the identifier is inside the
// signature and cannot be changed without the key.
func TestADriverTokenCannotBeRepointedAtAnotherJob(t *testing.T) {
	issuer := newIssuer(t, clock.NewFixed(testDriverIssuedAt))

	mine, theirs := uuid.New(), uuid.New()

	token, err := issuer.Issue(mine, uuid.New())
	if err != nil {
		t.Fatalf("Issue() = %v", err)
	}

	// The honest reading first: the token names my job and not theirs.
	claims, err := issuer.parse(token.Value)
	if err != nil {
		t.Fatalf("parse(a token we just signed) = %v", err)
	}
	granted, err := claims.Job()
	if err != nil {
		t.Fatalf("Job() = %v", err)
	}
	if granted != mine {
		t.Fatalf("the token grants %s, want %s", granted, mine)
	}
	if granted == theirs {
		t.Fatal("the token grants the other job as well, which is not a single-job token")
	}

	// And now the widening attempt.
	parts := strings.Split(token.Value, ".")
	payload := payloadOf(t, token.Value)
	payload["job_id"] = theirs.String()

	rewritten, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("re-encoding the payload: %v", err)
	}
	parts[1] = base64.RawURLEncoding.EncodeToString(rewritten)

	if _, err := issuer.parse(strings.Join(parts, ".")); err == nil {
		t.Fatal("a token repointed at another job was accepted")
	} else if !errors.Is(err, jwt.ErrSignatureInvalid) {
		t.Errorf("expected the signature to refuse it, got: %v", err)
	}
}

// TestADriverTokenIsSigned is the fourth word, from both sides: a tampered token and a token signed
// with a key this service does not hold.
func TestADriverTokenIsSigned(t *testing.T) {
	issuer := newIssuer(t, clock.NewFixed(testDriverIssuedAt))

	token, err := issuer.Issue(uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("Issue() = %v", err)
	}

	t.Run("a tampered signature is refused", func(t *testing.T) {
		parts := strings.Split(token.Value, ".")
		// Flip one character of the signature rather than truncating it, so that what is
		// being refused is a *wrong* signature and not a malformed token.
		signature := []byte(parts[2])
		if signature[0] == 'A' {
			signature[0] = 'B'
		} else {
			signature[0] = 'A'
		}
		parts[2] = string(signature)

		if _, err := issuer.parse(strings.Join(parts, ".")); err == nil {
			t.Fatal("a token with a rewritten signature was accepted")
		} else if !errors.Is(err, jwt.ErrSignatureInvalid) {
			t.Errorf("expected the signature to refuse it, got: %v", err)
		}
	})

	t.Run("a token signed with the wrong key is refused", func(t *testing.T) {
		// Signed with a key this service does not hold, but naming a `kid` it does — the
		// shape somebody forging a token would use, because naming an unknown key
		// identifier fails earlier and tells them less.
		claims := DriverClaims{
			JobID:        uuid.New().String(),
			AssignmentID: uuid.New().String(),
			IssuedAt:     testDriverIssuedAt.Unix(),
			ExpiresAt:    testDriverIssuedAt.Add(testDriverTokenTTL).Unix(),
			TokenID:      uuid.New().String(),
			Issuer:       IssuerName,
			Audience:     DriverAudience,
		}
		forged := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		forged.Header["kid"] = testDriverKID

		raw, err := forged.SignedString(testUnrelatedKey)
		if err != nil {
			t.Fatalf("signing the forgery: %v", err)
		}

		if _, err := issuer.parse(raw); err == nil {
			t.Fatal("a token signed with an unrelated key was accepted")
		} else if !errors.Is(err, jwt.ErrSignatureInvalid) {
			t.Errorf("expected the signature to refuse it, got: %v", err)
		}
	})

	t.Run("a token naming no key we hold is refused", func(t *testing.T) {
		claims := DriverClaims{
			JobID:        uuid.New().String(),
			AssignmentID: uuid.New().String(),
			IssuedAt:     testDriverIssuedAt.Unix(),
			ExpiresAt:    testDriverIssuedAt.Add(testDriverTokenTTL).Unix(),
			TokenID:      uuid.New().String(),
			Issuer:       IssuerName,
			Audience:     DriverAudience,
		}
		forged := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		forged.Header["kid"] = "a-key-that-was-never-in-the-set"

		raw, err := forged.SignedString(testDriverKey)
		if err != nil {
			t.Fatalf("signing the forgery: %v", err)
		}

		if _, err := issuer.parse(raw); !errors.Is(err, ErrNoSigningKey) {
			t.Errorf("parse(unknown kid) = %v, want ErrNoSigningKey", err)
		}
	})

	t.Run("an unsigned token is refused", func(t *testing.T) {
		// `alg: none` is the classic way in, and the parser pins HS256 against exactly
		// this. Worth a case of its own because it is the failure that looks like success:
		// the token parses, the claims are readable, and nothing signed them.
		claims := DriverClaims{
			JobID:        uuid.New().String(),
			AssignmentID: uuid.New().String(),
			IssuedAt:     testDriverIssuedAt.Unix(),
			ExpiresAt:    testDriverIssuedAt.Add(testDriverTokenTTL).Unix(),
			TokenID:      uuid.New().String(),
			Issuer:       IssuerName,
			Audience:     DriverAudience,
		}
		unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
		unsigned.Header["kid"] = testDriverKID

		raw, err := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
		if err != nil {
			t.Fatalf("building the unsigned token: %v", err)
		}

		if _, err := issuer.parse(raw); err == nil {
			t.Fatal("an unsigned token was accepted")
		}
	})
}

// TestARotatedOutKeyStillVerifies is what makes rotating the driver keyset a configuration change.
//
// It matters more here than it does for the access token: fifteen minutes after a rotation every
// mobile token signed by the outgoing key has expired, whereas a driver link lives a week and the
// driver has nothing that can refresh it.
func TestARotatedOutKeyStillVerifies(t *testing.T) {
	outgoing, err := NewKeyset(map[string][]byte{
		testDriverKID:     testDriverKey,
		testDriverRotated: testDriverOutgoing,
	}, testDriverRotated)
	if err != nil {
		t.Fatalf("building the outgoing keyset: %v", err)
	}

	before, err := NewDriverTokenIssuer(outgoing, testDriverTokenTTL, clock.NewFixed(testDriverIssuedAt))
	if err != nil {
		t.Fatalf("building the issuer: %v", err)
	}

	token, err := before.Issue(uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("Issue() = %v", err)
	}

	// The rotation: the same set, a different active identifier.
	after := newIssuer(t, clock.NewFixed(testDriverIssuedAt))
	if _, err := after.parse(token.Value); err != nil {
		t.Errorf("a link signed before the rotation stopped working: %v", err)
	}
}

// TestTheTwoTokenSystemsCannotBeExchanged is CLAUDE.md's invariant, in both directions, in one
// process.
//
// Docs/11 §8 has kept both verifiers with one owner precisely so that this test can exist: identity
// proves the first direction against a hand-built token because it cannot import this package, and
// this proves both against real ones.
//
// **The key material is deliberately shared here**, which is the worst case and the only case where
// the audience is doing the work by itself. internal/config refuses that arrangement in a deployment,
// so what this test asserts is that the guarantee does not depend on it.
func TestTheTwoTokenSystemsCannotBeExchanged(t *testing.T) {
	const sharedKID = "shared"
	shared := []byte("one-key-used-by-both-systems-which-is-32-plus")

	driverKeys, err := NewKeyset(map[string][]byte{sharedKID: shared}, sharedKID)
	if err != nil {
		t.Fatalf("building the driver keyset: %v", err)
	}
	driverIssuer, err := NewDriverTokenIssuer(driverKeys, testDriverTokenTTL, clock.NewFixed(testDriverIssuedAt))
	if err != nil {
		t.Fatalf("building the driver token issuer: %v", err)
	}

	mobileKeys, err := identity.NewKeyset(map[string][]byte{sharedKID: shared}, sharedKID)
	if err != nil {
		t.Fatalf("building the mobile keyset: %v", err)
	}
	mobileIssuer, err := identity.NewAccessTokenIssuer(mobileKeys, 15*time.Minute, clock.NewFixed(testDriverIssuedAt))
	if err != nil {
		t.Fatalf("building the access token issuer: %v", err)
	}
	mobileVerifier, err := identity.NewAccessTokenVerifier(mobileKeys, clock.NewFixed(testDriverIssuedAt))
	if err != nil {
		t.Fatalf("building the access token verifier: %v", err)
	}

	t.Run("a driver token is not a mobile session", func(t *testing.T) {
		driverToken, err := driverIssuer.Issue(uuid.New(), uuid.New())
		if err != nil {
			t.Fatalf("Issue() = %v", err)
		}

		if _, err := mobileVerifier.Verify(driverToken.Value); err == nil {
			t.Fatal("a driver's job-scoped token was accepted as a mobile session token")
		}
	})

	t.Run("a mobile session is not a driver token", func(t *testing.T) {
		access, err := mobileIssuer.Issue(uuid.New(), uuid.New(), identity.RoleProvider)
		if err != nil {
			t.Fatalf("Issue() = %v", err)
		}

		_, err = parseDriverToken(driverKeys, clock.NewFixed(testDriverIssuedAt), access.Value)
		if err == nil {
			t.Fatal("a mobile session token was accepted as a driver's job-scoped token")
		}
		if !errors.Is(err, jwt.ErrTokenInvalidAudience) {
			t.Errorf("expected the audience check to refuse it, got: %v", err)
		}
	})
}

// TestIssueRefusesWhatItCannotHonestlySay.
//
// A token is a statement this service will later believe, and a valid signature over uuid.Nil is a
// grant over a job that does not exist which a verifier has no way to tell from a real one.
func TestIssueRefusesWhatItCannotHonestlySay(t *testing.T) {
	issuer := newIssuer(t, clock.NewFixed(testDriverIssuedAt))

	if _, err := issuer.Issue(uuid.Nil, uuid.New()); err == nil {
		t.Error("a token was issued for no job")
	}
	if _, err := issuer.Issue(uuid.New(), uuid.Nil); err == nil {
		t.Error("a token was issued against no assignment")
	}
}

// TestKeysetRefusesMaterialItCannotSignWith.
//
// internal/config catches every one of these at startup, which is where they are actually caught. It
// is checked here as well because NewKeyset is exported and a caller that is not config — a test, a
// future cmd/ — should not be able to build a service that signs with four bytes.
func TestKeysetRefusesMaterialItCannotSignWith(t *testing.T) {
	for _, tc := range []struct {
		name   string
		keys   map[string][]byte
		active string
	}{
		{name: "empty", keys: map[string][]byte{}, active: testDriverKID},
		{name: "no active identifier", keys: map[string][]byte{testDriverKID: testDriverKey}},
		{name: "active names no key", keys: map[string][]byte{testDriverKID: testDriverKey}, active: "other"},
		{name: "a key with no identifier", keys: map[string][]byte{"": testDriverKey}, active: testDriverKID},
		{name: "a key shorter than HS256's output",
			keys: map[string][]byte{testDriverKID: []byte("too short")}, active: testDriverKID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewKeyset(tc.keys, tc.active); !errors.Is(err, ErrInvalidKeyset) {
				t.Errorf("NewKeyset() = %v, want ErrInvalidKeyset", err)
			}
		})
	}
}

// TestTheKeysetCopiesItsMaterial: a caller holding the map it passed in must not be able to change
// what a running service signs with.
func TestTheKeysetCopiesItsMaterial(t *testing.T) {
	original := map[string][]byte{testDriverKID: append([]byte(nil), testDriverKey...)}

	keys, err := NewKeyset(original, testDriverKID)
	if err != nil {
		t.Fatalf("NewKeyset() = %v", err)
	}

	original[testDriverKID][0] = 'X'
	delete(original, testDriverKID)

	signing, err := keys.key(testDriverKID)
	if err != nil {
		t.Fatalf("the key was removed from under the keyset: %v", err)
	}
	if string(signing) != string(testDriverKey) {
		t.Error("the signing key changed when the caller's map did")
	}
}

// TestMalformedClaimsAreNotAGrant: a token this service signed whose claims are not identifiers.
//
// Unreachable through [DriverTokenIssuer.Issue], which refuses uuid.Nil, and checked because the
// alternative reading — uuid.Parse failing and the caller taking uuid.Nil — is a grant over the nil
// job that looks exactly like a grant over a real one.
func TestMalformedClaimsAreNotAGrant(t *testing.T) {
	claims := DriverClaims{JobID: "not-a-uuid", AssignmentID: ""}

	if _, err := claims.Job(); !errors.Is(err, ErrMalformedDriverToken) {
		t.Errorf("Job() = %v, want ErrMalformedDriverToken", err)
	}
	if _, err := claims.Assignment(); !errors.Is(err, ErrMalformedDriverToken) {
		t.Errorf("Assignment() = %v, want ErrMalformedDriverToken", err)
	}
}

// TestATokenIsGeneratedOnAssignment is the *Done when* itself, against a real database.
//
// The four words above are properties of the issuer; this is the sentence they are properties of.
// It asserts three things a unit test cannot: that a successful assignment produces a token, that
// the token names the assignment that was actually written, and that **two jobs get two tokens**,
// neither of which opens the other's job.
func TestATokenIsGeneratedOnAssignment(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "token-c@example.com", "+61400000640", "customer")
	provider := newAccount(t, pool, "token-p@example.com", "+61400000641", "provider")

	svc := newTestService()

	first := awardedJob(t, pool, customer, provider)
	second := awardedJob(t, pool, customer, provider)

	assignment, token, created, err := assignGranting(t, pool, svc, provider, first, Nomination{
		DriverName:   "Sam Patel",
		DriverMobile: "+61412345678",
	})
	if err != nil || !created {
		t.Fatalf("AssignDriver() = %v (created = %v)", err, created)
	}

	if token.Value == "" {
		t.Fatal("an assignment succeeded and minted no token, which is SHIP-107's whole Done when")
	}
	if token.JobID != first {
		t.Errorf("the token grants %s, want the job that was assigned, %s", token.JobID, first)
	}
	if token.AssignmentID != assignment.ID {
		t.Errorf("the token names assignment %s, want the row that was written, %s",
			token.AssignmentID, assignment.ID)
	}
	if !token.ExpiresAt.After(testInstant) {
		t.Errorf("the token expires at %s, which is not after it was issued", token.ExpiresAt)
	}

	// Read back through the parser rather than trusting the struct: what a verifier will act on
	// is the claim, and a DriverToken whose fields disagreed with its own payload would pass
	// every assertion above.
	claims, err := svc.tokens.parse(token.Value)
	if err != nil {
		t.Fatalf("the token minted on assignment does not verify: %v", err)
	}
	granted, err := claims.Job()
	if err != nil {
		t.Fatalf("Job() = %v", err)
	}
	if granted != first {
		t.Errorf("the token's claim grants %s, want %s", granted, first)
	}

	_, other, _, err := assignGranting(t, pool, svc, provider, second, Nomination{
		DriverName:   "Ravi Chandra",
		DriverMobile: "+61412000999",
	})
	if err != nil {
		t.Fatalf("assigning the second job = %v", err)
	}

	if other.Value == token.Value {
		t.Fatal("two jobs were assigned and both got the same token")
	}
	if other.JobID != second {
		t.Errorf("the second token grants %s, want %s", other.JobID, second)
	}

	otherClaims, err := svc.tokens.parse(other.Value)
	if err != nil {
		t.Fatalf("the second token does not verify: %v", err)
	}
	otherJob, err := otherClaims.Job()
	if err != nil {
		t.Fatalf("Job() = %v", err)
	}
	if otherJob == first {
		t.Error("the token issued for the second job grants the first, which is not a single-job token")
	}
}

// TestARepeatedNominationIsGrantedAFreshToken.
//
// The absorbed repeat — a phone that lost its connection and generated a new idempotency key for
// the same intent — answers 200 with the existing assignment, and it must answer with a *link* too:
// a provider who never saw the first response has no other way to obtain one. Both tokens grant the
// same one job to the same one driver, so nothing is widened. Invalidating the previous link is
// SHIP-109 and is deliberately not this.
func TestARepeatedNominationIsGrantedAFreshToken(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "retoken-c@example.com", "+61400000642", "customer")
	provider := newAccount(t, pool, "retoken-p@example.com", "+61400000643", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	svc := newTestService()
	nomination := Nomination{DriverName: "Sam Patel", DriverMobile: "+61412345678"}

	assignment, first, created, err := assignGranting(t, pool, svc, provider, jobID, nomination)
	if err != nil || !created {
		t.Fatalf("AssignDriver() = %v (created = %v)", err, created)
	}

	repeat, second, createdAgain, err := assignGranting(t, pool, svc, provider, jobID, nomination)
	if err != nil {
		t.Fatalf("AssignDriver(repeat) = %v", err)
	}
	if createdAgain {
		t.Error("the repeat wrote a second assignment")
	}
	if repeat.ID != assignment.ID {
		t.Errorf("the repeat answered with assignment %s, want %s", repeat.ID, assignment.ID)
	}

	if second.Value == "" {
		t.Fatal("an absorbed repeat answered with no link, so a provider who lost the first response has none")
	}
	if second.AssignmentID != assignment.ID {
		t.Errorf("the reissued token names %s, want the live assignment %s",
			second.AssignmentID, assignment.ID)
	}
	if second.JobID != jobID {
		t.Errorf("the reissued token grants %s, want %s", second.JobID, jobID)
	}

	// Both work. This is the state SHIP-109 changes, and it is recorded as an assertion rather
	// than as a comment so that the ticket which changes it has a test to change.
	if _, err := svc.tokens.parse(first.Value); err != nil {
		t.Errorf("the first link stopped working when a second was issued: %v", err)
	}
	if _, err := svc.tokens.parse(second.Value); err != nil {
		t.Errorf("the reissued link does not verify: %v", err)
	}
}

// TestARefusedAssignmentMintsNothing.
//
// The rollback SHIP-106 already asserts, with the token added: a job that answered a refusal and
// handed out a link anyway would be a grant over a delivery that never started.
func TestARefusedAssignmentMintsNothing(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "notoken-c@example.com", "+61400000644", "customer")
	provider := newAccount(t, pool, "notoken-p@example.com", "+61400000645", "provider")
	stranger := newAccount(t, pool, "notoken-s@example.com", "+61400000646", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	_, token, _, err := assignGranting(t, pool, newTestService(), stranger, jobID, Nomination{
		DriverName:   "Sam Patel",
		DriverMobile: "+61412345678",
	})
	if !errors.Is(err, ErrNotAwardedProvider) {
		t.Fatalf("AssignDriver(stranger) = %v, want ErrNotAwardedProvider", err)
	}
	if token.Value != "" {
		t.Error("a refused assignment handed out a job-scoped link")
	}
}
