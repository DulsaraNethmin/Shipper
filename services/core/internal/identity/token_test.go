package identity

import (
	"bytes"
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
)

// Throwaway keys, thirty-two bytes each, so the keyset's own length rule is satisfied without
// any of this looking like key material anybody should reuse.
var (
	testKeyOne = []byte("test-signing-key-one-0123456789ab")
	testKeyTwo = []byte("test-signing-key-two-0123456789ab")
)

const (
	kidOne = "k1"
	kidTwo = "k2"
)

// testIssuedAt is a fixed instant, so every expiry assertion is arithmetic rather than a race
// with the wall clock (Docs/10 §6.3).
var testIssuedAt = time.Date(2026, time.August, 10, 9, 30, 0, 0, time.UTC)

func testKeyset(t *testing.T, active string) *Keyset {
	t.Helper()
	ks, err := NewKeyset(map[string][]byte{kidOne: testKeyOne, kidTwo: testKeyTwo}, active)
	if err != nil {
		t.Fatalf("building the keyset: %v", err)
	}
	return ks
}

func testIssuer(t *testing.T, active string) (*AccessTokenIssuer, *clock.Fixed) {
	t.Helper()
	clk := clock.NewFixed(testIssuedAt)
	issuer, err := NewAccessTokenIssuer(testKeyset(t, active), 15*time.Minute, clk)
	if err != nil {
		t.Fatalf("building the issuer: %v", err)
	}
	return issuer, clk
}

// segments splits a JWT and decodes its header and payload.
//
// Deliberately done with encoding/base64 and encoding/json rather than with the JWT library:
// the assertion is about what is on the wire, and asking the library that produced it to tell us
// what it produced would prove very little.
func segments(t *testing.T, raw string) (header, payload map[string]any) {
	t.Helper()

	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		t.Fatalf("a JWT has three segments, this has %d", len(parts))
	}

	decode := func(segment string) map[string]any {
		b, err := base64.RawURLEncoding.DecodeString(segment)
		if err != nil {
			t.Fatalf("decoding %q: %v", segment, err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("parsing %s: %v", b, err)
		}
		return m
	}

	return decode(parts[0]), decode(parts[1])
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestAccessTokenClaimSetIsExactlyAsSpecified is Docs/10 §5's claim list, checked as a set
// rather than as a series of presence assertions.
//
// Checking the whole set is what catches an *added* claim, which is the direction that matters:
// a missing claim breaks something immediately and loudly, and an extra one sits there being
// believed by whatever reads it next.
func TestAccessTokenClaimSetIsExactlyAsSpecified(t *testing.T) {
	issuer, _ := testIssuer(t, kidOne)

	user := uuid.MustParse("018f6b0c-1111-7000-8000-000000000001")
	session := uuid.MustParse("018f6b0c-2222-7000-8000-000000000002")

	token, err := issuer.Issue(user, session, RoleProvider)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}

	// Logged so scripts/verify-foundation.sh can decode the real token end to end rather than
	// trusting this test's own reading of it. Signed with the throwaway key above.
	t.Logf("access token: %s", token.Value)

	header, payload := segments(t, token.Value)

	if got, want := header["alg"], "HS256"; got != want {
		t.Errorf("alg = %v, want %v", got, want)
	}
	if header["kid"] != kidOne {
		t.Errorf("kid = %v, want %v", header["kid"], kidOne)
	}

	want := []string{"aud", "exp", "iat", "iss", "jti", "role", "sid", "sub"}
	if got := keysOf(payload); !equalStrings(got, want) {
		t.Fatalf("claims are %v, and Docs/10 §5 says exactly %v", got, want)
	}

	if payload["sub"] != user.String() {
		t.Errorf("sub = %v, want %s", payload["sub"], user)
	}
	if payload["sid"] != session.String() {
		t.Errorf("sid = %v, want %s", payload["sid"], session)
	}
	if payload["role"] != "provider" {
		t.Errorf("role = %v, want provider", payload["role"])
	}
	if payload["iss"] != IssuerName {
		t.Errorf("iss = %v, want %s", payload["iss"], IssuerName)
	}
	if payload["aud"] != MobileAudience {
		t.Errorf("aud = %v, want %s", payload["aud"], MobileAudience)
	}
	if jti, _ := payload["jti"].(string); jti == "" {
		t.Error("jti is empty, so no single token could ever be named on a denylist")
	}
}

// TestAccessTokenCarriesNoPermissionOrVerificationState is the invariant, asserted by name.
//
// It overlaps with the claim-set test above deliberately. That one fails if the set changes at
// all; this one says *why* it must not gain these particular members, so that whoever is tempted
// to add `verified` for the sake of one round trip reads the reason in the failure.
func TestAccessTokenCarriesNoPermissionOrVerificationState(t *testing.T) {
	issuer, _ := testIssuer(t, kidOne)

	token, err := issuer.Issue(uuid.New(), uuid.New(), RoleCustomer)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}
	_, payload := segments(t, token.Value)

	forbidden := []string{
		"permissions", "permission", "perms", "scope", "scopes", "abilities", "can",
		"verified", "is_verified", "verification", "verification_state",
		"email_verified", "phone_verified", "verified_at", "status", "account_status",
	}
	for _, claim := range forbidden {
		if _, present := payload[claim]; present {
			t.Errorf("the token carries %q. Docs/07 §3 puts every authorisation decision on "+
				"the platform, and verification state changes during a session — a cached "+
				"flag would let a customer who was unverified at sign-in publish a job "+
				"(Docs/10 §5). SHIP-63 reads verification fresh.", claim)
		}
	}
}

// TestAccessTokenTTLIsFifteenMinutes uses a stopped clock, so the assertion is exact rather than
// approximate and the expiry can be crossed without waiting for it.
func TestAccessTokenTTLIsFifteenMinutes(t *testing.T) {
	issuer, clk := testIssuer(t, kidOne)

	token, err := issuer.Issue(uuid.New(), uuid.New(), RoleCustomer)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}

	_, payload := segments(t, token.Value)
	iat, _ := payload["iat"].(float64)
	exp, _ := payload["exp"].(float64)

	if int64(iat) != testIssuedAt.Unix() {
		t.Errorf("iat = %d, want the injected clock's instant %d", int64(iat), testIssuedAt.Unix())
	}
	if got := int64(exp - iat); got != 900 {
		t.Errorf("the token lives %d seconds, and Docs/10 §5 says 900", got)
	}
	if !token.ExpiresAt.Equal(testIssuedAt.Add(15 * time.Minute)) {
		t.Errorf("ExpiresAt = %s, want %s", token.ExpiresAt, testIssuedAt.Add(15*time.Minute))
	}

	if _, err := issuer.parse(token.Value); err != nil {
		t.Fatalf("a freshly issued token does not verify: %v", err)
	}

	clk.Advance(14*time.Minute + 59*time.Second)
	if _, err := issuer.parse(token.Value); err != nil {
		t.Errorf("the token expired before its fifteen minutes were up: %v", err)
	}

	clk.Advance(2 * time.Second)
	if _, err := issuer.parse(token.Value); !errors.Is(err, jwt.ErrTokenExpired) {
		t.Errorf("after fifteen minutes the token gave %v, want an expiry failure", err)
	}
}

// TestAccessTokenKidSelectsTheSigningKey is the rotation mechanism, in both directions: the
// active key signs, and a key that is no longer active still verifies.
func TestAccessTokenKidSelectsTheSigningKey(t *testing.T) {
	signedWithOne, _ := testIssuer(t, kidOne)

	old, err := signedWithOne.Issue(uuid.New(), uuid.New(), RoleCustomer)
	if err != nil {
		t.Fatalf("issuing under %s: %v", kidOne, err)
	}
	header, _ := segments(t, old.Value)
	if header["kid"] != kidOne {
		t.Fatalf("kid = %v, want %s", header["kid"], kidOne)
	}

	// Rotation: k2 becomes active, k1 stays in the set.
	rotated, _ := testIssuer(t, kidTwo)

	fresh, err := rotated.Issue(uuid.New(), uuid.New(), RoleCustomer)
	if err != nil {
		t.Fatalf("issuing under %s: %v", kidTwo, err)
	}
	header, _ = segments(t, fresh.Value)
	if header["kid"] != kidTwo {
		t.Errorf("after rotation the new token names %v, want %s", header["kid"], kidTwo)
	}

	// The point of the whole exercise: every token signed by the outgoing key is still valid
	// until it expires. Dropping k1 at the moment k2 became active would sign out every
	// client holding a token less than fifteen minutes old.
	if _, err := rotated.parse(old.Value); err != nil {
		t.Errorf("a token signed by the rotated-out key no longer verifies: %v", err)
	}

	// And a key that has genuinely been retired is refused rather than guessed at.
	onlyTwo, err := NewKeyset(map[string][]byte{kidTwo: testKeyTwo}, kidTwo)
	if err != nil {
		t.Fatalf("building the reduced keyset: %v", err)
	}
	retired, err := NewAccessTokenIssuer(onlyTwo, 15*time.Minute, clock.NewFixed(testIssuedAt))
	if err != nil {
		t.Fatalf("building the issuer: %v", err)
	}
	if _, err := retired.parse(old.Value); err == nil {
		t.Error("a token naming a key that is no longer in the set was accepted")
	}
}

// TestAccessTokenWithTheDriverAudienceIsRejected is CLAUDE.md's invariant that the driver's
// job-scoped token and the mobile auth token are separate systems.
//
// The token below is signed with this issuer's own key — the worst case, where key material has
// somehow been shared — so what refuses it is the audience alone. That is the property Docs/10
// §5 wants: separation by construction rather than by anyone remembering.
func TestAccessTokenWithTheDriverAudienceIsRejected(t *testing.T) {
	issuer, _ := testIssuer(t, kidOne)

	claims := AccessClaims{
		Subject:   uuid.New().String(),
		Role:      RoleProvider,
		SessionID: uuid.New().String(),
		IssuedAt:  testIssuedAt.Unix(),
		ExpiresAt: testIssuedAt.Add(15 * time.Minute).Unix(),
		TokenID:   uuid.New().String(),
		Issuer:    IssuerName,
		// The driver portal's audience (Docs/10 §5). Named as a literal rather than as a
		// constant, because the delivery domain owns that token and identity does not
		// import it.
		Audience: "shipper-driver",
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = kidOne
	raw, err := token.SignedString(testKeyOne)
	if err != nil {
		t.Fatalf("signing the driver-audience token: %v", err)
	}

	if _, err := issuer.parse(raw); err == nil {
		t.Fatal("a token for the driver portal was accepted as a mobile session token")
	} else if !errors.Is(err, jwt.ErrTokenInvalidAudience) {
		t.Errorf("expected the audience check to refuse it, got: %v", err)
	}
}

func TestAccessTokenIssueRefusesWhatItCannotHonestlySay(t *testing.T) {
	issuer, _ := testIssuer(t, kidOne)

	t.Run("no user", func(t *testing.T) {
		if _, err := issuer.Issue(uuid.Nil, uuid.New(), RoleCustomer); err == nil {
			t.Error("a token was issued for nobody")
		}
	})
	t.Run("no session", func(t *testing.T) {
		if _, err := issuer.Issue(uuid.New(), uuid.Nil, RoleCustomer); err == nil {
			t.Error("a token was issued against no device session")
		}
	})
	t.Run("a role the platform does not have", func(t *testing.T) {
		for _, role := range []Role{"", "admin", "Customer", "driver"} {
			if _, err := issuer.Issue(uuid.New(), uuid.New(), role); !errors.Is(err, ErrInvalidRole) {
				t.Errorf("role %q was accepted (err=%v)", role, err)
			}
		}
	})
}

func TestKeysetRefusesWhatCannotSign(t *testing.T) {
	short := []byte("too-short")

	cases := map[string]struct {
		keys   map[string][]byte
		active string
	}{
		"no keys":              {map[string][]byte{}, kidOne},
		"no active identifier": {map[string][]byte{kidOne: testKeyOne}, ""},
		"active names nothing": {map[string][]byte{kidOne: testKeyOne}, "k9"},
		"a key with no name":   {map[string][]byte{"": testKeyOne}, kidOne},
		"a key too short":      {map[string][]byte{kidOne: short}, kidOne},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewKeyset(c.keys, c.active); err == nil {
				t.Fatal("the keyset was accepted")
			} else if !errors.Is(err, ErrInvalidKeyset) {
				t.Errorf("unexpected error kind: %v", err)
			}
		})
	}
}

// TestKeysetCopiesItsKeys keeps a caller's map from becoming a way to change the signing
// material of a running service.
func TestKeysetCopiesItsKeys(t *testing.T) {
	supplied := map[string][]byte{kidOne: bytes.Clone(testKeyOne)}
	ks, err := NewKeyset(supplied, kidOne)
	if err != nil {
		t.Fatalf("building the keyset: %v", err)
	}

	supplied[kidOne][0] = 'X'
	delete(supplied, kidOne)

	key, err := ks.key(kidOne)
	if err != nil {
		t.Fatalf("the key vanished when the caller's map was changed: %v", err)
	}
	if key[0] == 'X' {
		t.Error("changing the caller's slice changed the signing key")
	}
}

func TestNewAccessTokenIssuerRefusesAnImpossibleConfiguration(t *testing.T) {
	ks := testKeyset(t, kidOne)

	if _, err := NewAccessTokenIssuer(nil, time.Minute, clock.System{}); err == nil {
		t.Error("an issuer with no keyset was accepted")
	}
	if _, err := NewAccessTokenIssuer(ks, 0, clock.System{}); err == nil {
		t.Error("an issuer with no TTL was accepted")
	}
	if _, err := NewAccessTokenIssuer(ks, time.Minute, nil); err == nil {
		t.Error("an issuer with no clock was accepted; every TTL would then be untestable")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
