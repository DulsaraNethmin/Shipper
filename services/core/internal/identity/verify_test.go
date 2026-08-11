package identity

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
)

func testVerifier(t *testing.T, active string) (*AccessTokenVerifier, *clock.Fixed) {
	t.Helper()
	clk := clock.NewFixed(testIssuedAt)
	v, err := NewAccessTokenVerifier(testKeyset(t, active), clk)
	if err != nil {
		t.Fatalf("building the verifier: %v", err)
	}
	return v, clk
}

// signWith mints a token from an arbitrary claim set, so a test can produce something the issuer
// would refuse to create and check that the verifier refuses to believe it.
func signWith(t *testing.T, claims AccessClaims, kid string, key []byte) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	token.Header["kid"] = kid
	raw, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	return raw
}

func validClaims() AccessClaims {
	return AccessClaims{
		Subject:   uuid.New().String(),
		Role:      RoleCustomer,
		SessionID: uuid.New().String(),
		IssuedAt:  testIssuedAt.Unix(),
		ExpiresAt: testIssuedAt.Add(15 * time.Minute).Unix(),
		TokenID:   uuid.New().String(),
		Issuer:    IssuerName,
		Audience:  MobileAudience,
	}
}

// SHIP-44's acceptance criterion at the domain layer: a token this platform issued verifies, and
// the claims come back intact.
func TestVerifyAcceptsATokenThisPlatformIssued(t *testing.T) {
	issuer, _ := testIssuer(t, kidOne)
	verifier, _ := testVerifier(t, kidOne)

	userID, sessionID := uuid.New(), uuid.New()
	token, err := issuer.Issue(userID, sessionID, RoleProvider)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}

	claims, err := verifier.Verify(token.Value)
	if err != nil {
		t.Fatalf("a freshly issued token did not verify: %v", err)
	}

	if claims.Subject != userID.String() {
		t.Errorf("sub = %q, want %q", claims.Subject, userID)
	}
	if claims.SessionID != sessionID.String() {
		t.Errorf("sid = %q, want %q", claims.SessionID, sessionID)
	}
	if claims.Role != RoleProvider {
		t.Errorf("role = %q, want %q", claims.Role, RoleProvider)
	}
}

// Expiry is its own error because it is the one failure a client acts on differently.
func TestVerifyReportsExpiryDistinctly(t *testing.T) {
	// Two clocks, deliberately: the issuer's stays at the instant the token was minted while
	// the verifier's moves forward. That is the real arrangement — the token is a fixed
	// statement about the past, and it is the verifier's now that decides whether it still
	// holds.
	issuer, _ := testIssuer(t, kidOne)
	verifier, verifierClock := testVerifier(t, kidOne)

	token, err := issuer.Issue(uuid.New(), uuid.New(), RoleCustomer)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}

	verifierClock.Advance(14*time.Minute + 59*time.Second)
	if _, err := verifier.Verify(token.Value); err != nil {
		t.Errorf("the token expired before its fifteen minutes were up: %v", err)
	}

	verifierClock.Advance(2 * time.Second)
	_, err = verifier.Verify(token.Value)
	if !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("after fifteen minutes Verify gave %v, want ErrTokenExpired", err)
	}
	// Expiry must not also read as the general failure, or a caller cannot branch on it.
	if errors.Is(err, ErrTokenInvalid) {
		t.Error("an expired token also reports ErrTokenInvalid; the two must be distinguishable")
	}
	// The library's own sentinel survives the wrapping, which is what keeps the log useful.
	if !errors.Is(err, jwt.ErrTokenExpired) {
		t.Error("the underlying jwt error was replaced rather than wrapped")
	}
}

// Everything that is not expiry is one error, on purpose: a caller can do nothing different
// about any of them, and naming which check failed helps only somebody probing.
func TestVerifyRefusesEverythingElseAsOneError(t *testing.T) {
	cases := []struct {
		name  string
		token func(t *testing.T) string
	}{
		{
			name:  "empty",
			token: func(*testing.T) string { return "" },
		},
		{
			name:  "not a token at all",
			token: func(*testing.T) string { return "not.a.token" },
		},
		{
			name: "signed with a key this service does not hold",
			token: func(t *testing.T) string {
				return signWith(t, validClaims(), kidOne, []byte("an-entirely-different-32-byte-key"))
			},
		},
		{
			name: "naming a key that has been retired",
			token: func(t *testing.T) string {
				return signWith(t, validClaims(), "k3-retired", testKeyOne)
			},
		},
		{
			name: "the wrong issuer",
			token: func(t *testing.T) string {
				c := validClaims()
				c.Issuer = "somebody-else"
				return signWith(t, c, kidOne, testKeyOne)
			},
		},
		{
			// The claim set is otherwise perfect and signed with this service's own key.
			// What refuses it is the audience alone — CLAUDE.md's invariant that the
			// driver's job-scoped token and the mobile token are separate systems.
			name: "the driver portal's audience",
			token: func(t *testing.T) string {
				c := validClaims()
				c.Audience = "shipper-driver"
				return signWith(t, c, kidOne, testKeyOne)
			},
		},
		{
			name: "a role this platform does not issue",
			token: func(t *testing.T) string {
				c := validClaims()
				c.Role = Role("administrator")
				return signWith(t, c, kidOne, testKeyOne)
			},
		},
		{
			name: "an empty subject",
			token: func(t *testing.T) string {
				c := validClaims()
				c.Subject = ""
				return signWith(t, c, kidOne, testKeyOne)
			},
		},
		{
			name: "a subject that is not an identifier this service issues",
			token: func(t *testing.T) string {
				c := validClaims()
				c.Subject = "1"
				return signWith(t, c, kidOne, testKeyOne)
			},
		},
		{
			name: "the nil identifier as the subject",
			token: func(t *testing.T) string {
				c := validClaims()
				c.Subject = uuid.Nil.String()
				return signWith(t, c, kidOne, testKeyOne)
			},
		},
		{
			name: "no session",
			token: func(t *testing.T) string {
				c := validClaims()
				c.SessionID = ""
				return signWith(t, c, kidOne, testKeyOne)
			},
		},
		{
			name: "no jti",
			token: func(t *testing.T) string {
				c := validClaims()
				c.TokenID = ""
				return signWith(t, c, kidOne, testKeyOne)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verifier, _ := testVerifier(t, kidOne)

			_, err := verifier.Verify(tc.token(t))
			if !errors.Is(err, ErrTokenInvalid) {
				t.Fatalf("Verify gave %v, want ErrTokenInvalid", err)
			}
			if errors.Is(err, ErrTokenExpired) {
				t.Error("reported as expired, which is a different thing entirely")
			}
		})
	}
}

// alg:none is the classic JWT attack and it has to be refused by construction rather than by
// anyone remembering. parseAccessToken pins the algorithm; this proves it.
func TestVerifyRefusesAnUnsignedToken(t *testing.T) {
	verifier, _ := testVerifier(t, kidOne)

	token := jwt.NewWithClaims(jwt.SigningMethodNone, validClaims())
	token.Header["kid"] = kidOne
	raw, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("signing with alg none: %v", err)
	}

	if _, err := verifier.Verify(raw); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("an unsigned token gave %v, want ErrTokenInvalid", err)
	}
}

// Rotation, from the verifier's side: the outgoing key keeps verifying the tokens it signed.
// Dropping it at the moment the new key became active would sign out every client holding a
// token less than fifteen minutes old.
func TestVerifyAcceptsATokenFromARotatedOutKey(t *testing.T) {
	signedWithOne, _ := testIssuer(t, kidOne)

	old, err := signedWithOne.Issue(uuid.New(), uuid.New(), RoleCustomer)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}

	// k2 is now active; k1 is still in the set.
	verifier, _ := testVerifier(t, kidTwo)

	if _, err := verifier.Verify(old.Value); err != nil {
		t.Errorf("a token signed by the rotated-out key no longer verifies: %v", err)
	}
}

// The response must not repeat what the token said. A verification failure that echoed the
// claims would put a forged subject into a log line beside the words "rejected", which reads as
// though it were real.
func TestVerificationFailuresDoNotEchoTheClaims(t *testing.T) {
	verifier, _ := testVerifier(t, kidOne)

	secret := uuid.New().String()
	c := validClaims()
	c.Subject = secret
	c.Role = Role("administrator") // fails validation, so the error is ours rather than jwt's

	_, err := verifier.Verify(signWith(t, c, kidOne, testKeyOne))
	if err == nil {
		t.Fatal("the token was accepted")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("the error repeats the subject the token claimed: %v", err)
	}
}

func TestNewAccessTokenVerifierRefusesWhatItCannotUse(t *testing.T) {
	t.Run("no keyset", func(t *testing.T) {
		if _, err := NewAccessTokenVerifier(nil, clock.NewFixed(testIssuedAt)); !errors.Is(err, ErrInvalidKeyset) {
			t.Errorf("got %v, want ErrInvalidKeyset", err)
		}
	})
	t.Run("no clock", func(t *testing.T) {
		if _, err := NewAccessTokenVerifier(testKeyset(t, kidOne), nil); err == nil {
			t.Error("a verifier with no clock was accepted; every expiry check would panic")
		}
	})
}
