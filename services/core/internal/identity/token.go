// SHIP-37: access token issue.
//
// HS256 over a keyset selected by a `kid` header, exactly as Docs/10 §5 specifies. Issue only:
// the authentication middleware that verifies these on the way in is SHIP-44, and both token
// verifiers in this platform are deliberately written by one person, with a test proving each
// rejects the other's tokens.
//
// The blank line below keeps this a file note rather than a second package comment.

package identity

import (
	"bytes"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
)

const (
	// IssuerName and MobileAudience are the `iss` and `aud` claims of every access token.
	//
	// The audience is what makes "the mobile token and the driver token are separate systems"
	// a property of the verifier rather than a rule somebody has to remember. The driver
	// portal's token carries a different audience and different key material entirely
	// (Docs/10 §5), and neither verifier will accept the other's tokens even if a key were
	// somehow shared.
	IssuerName     = "shipper"
	MobileAudience = "shipper-mobile"

	// minimumSigningKeyBytes is the shortest key HS256 is worth using: HMAC-SHA256 produces
	// 256 bits, and a key shorter than its own output is the part of the scheme an attacker
	// goes at first. internal/config refuses a short key at startup, which is the only place
	// it can be caught — nothing about a token signed with a four-byte key looks wrong.
	minimumSigningKeyBytes = 32
)

// Keyset is the HMAC signing material, indexed by key identifier.
//
// More than one key so that rotation is a configuration change: the incoming key becomes active
// and signs new tokens while the outgoing one stays in the set and keeps verifying the tokens it
// already signed, until the last of them has expired fifteen minutes later. Removing it before
// then would sign every holder of a valid token out.
//
// Asymmetric signing would buy nothing here. There is no BFF (Docs/06 §2.1) and this service is
// the only verifier, so a keyset with a `kid` gives rotation without the operational weight of
// distributing public keys.
type Keyset struct {
	active string
	keys   map[string][]byte
}

// NewKeyset validates the keys and copies them, so that a caller holding the original map cannot
// change the signing material of a running service.
func NewKeyset(keys map[string][]byte, active string) (*Keyset, error) {
	if len(keys) == 0 {
		return nil, fmt.Errorf("%w: the set is empty", ErrInvalidKeyset)
	}
	if active == "" {
		return nil, fmt.Errorf("%w: no active key identifier", ErrInvalidKeyset)
	}

	copied := make(map[string][]byte, len(keys))
	for kid, secret := range keys {
		if kid == "" {
			return nil, fmt.Errorf("%w: a key has no identifier, so no token could name it",
				ErrInvalidKeyset)
		}
		if len(secret) < minimumSigningKeyBytes {
			return nil, fmt.Errorf("%w: key %q is %d bytes, and HS256 wants at least %d",
				ErrInvalidKeyset, kid, len(secret), minimumSigningKeyBytes)
		}
		copied[kid] = bytes.Clone(secret)
	}

	if _, ok := copied[active]; !ok {
		return nil, fmt.Errorf("%w: the active identifier %q names no key in the set",
			ErrInvalidKeyset, active)
	}

	return &Keyset{active: active, keys: copied}, nil
}

// ActiveKID is the identifier of the key new tokens are signed with.
func (k *Keyset) ActiveKID() string { return k.active }

// key returns the signing key with the given identifier.
func (k *Keyset) key(kid string) ([]byte, error) {
	secret, ok := k.keys[kid]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNoSigningKey, kid)
	}
	return secret, nil
}

// AccessClaims is the whole claim set of an access token, and the list is closed.
//
// Docs/10 §5: sub, role, sid, iat, exp, jti, iss and aud. Nothing else — and specifically **no
// permissions and no verification state**. Docs/07 §3 puts every authorisation decision on the
// platform, and verification state changes during a session: a cached "verified" flag would let
// a customer who was unverified at sign-in publish a job fourteen minutes later, because the
// token would still be saying what was true when it was issued. SHIP-63 reads verification
// fresh, from the database, every time it matters.
//
// The fields are declared here rather than embedding jwt.RegisteredClaims so that the JSON on
// the wire is exactly this and nothing more. An embedded struct would emit whatever it happens
// to carry, which is a claim set that changes when a dependency is upgraded.
type AccessClaims struct {
	// Subject is the user id, as a string. RFC 7519 makes `sub` a string claim.
	Subject string `json:"sub"`

	// Role is what shell the client should show. It is *not* an authorisation decision —
	// the domain being called still decides whether this account may do the thing.
	Role Role `json:"role"`

	// SessionID is the device_sessions row this token was issued against (SHIP-38), which is
	// what makes "sign this device out" reach the access token as well as the refresh one.
	SessionID string `json:"sid"`

	IssuedAt  int64 `json:"iat"`
	ExpiresAt int64 `json:"exp"`

	// TokenID identifies this token uniquely, which is what a denylist entry names when a
	// single token has to be refused before it expires.
	TokenID string `json:"jti"`

	Issuer string `json:"iss"`

	// Audience is a single string rather than an array. RFC 7519 permits either for one
	// audience, and this platform issues and verifies its own tokens, so the plain form is
	// the one with fewer ways to be read.
	Audience string `json:"aud"`
}

// The jwt.Claims interface, so the library's validator can read the registered claims out of a
// struct that does not embed its RegisteredClaims.

func (c AccessClaims) GetExpirationTime() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.ExpiresAt, 0)), nil
}

func (c AccessClaims) GetIssuedAt() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.IssuedAt, 0)), nil
}

// GetNotBefore reports none. An access token is valid the moment it is issued; `nbf` would be a
// claim with nothing to say, and the specification is that the claim set carries nothing extra.
func (c AccessClaims) GetNotBefore() (*jwt.NumericDate, error) { return nil, nil }

func (c AccessClaims) GetIssuer() (string, error)  { return c.Issuer, nil }
func (c AccessClaims) GetSubject() (string, error) { return c.Subject, nil }

func (c AccessClaims) GetAudience() (jwt.ClaimStrings, error) {
	return jwt.ClaimStrings{c.Audience}, nil
}

// AccessToken is an issued token and the instant it stops being valid.
//
// The expiry travels with the value because the endpoints that return one (SHIP-41, SHIP-42)
// tell the client how long it has, and recomputing it from a TTL at each call site is how the
// two end up disagreeing.
type AccessToken struct {
	Value     string
	ExpiresAt time.Time
}

// AccessTokenIssuer signs access tokens.
type AccessTokenIssuer struct {
	keys  *Keyset
	ttl   time.Duration
	clock clock.Clock
}

// NewAccessTokenIssuer builds an issuer.
//
// The clock is injected rather than read inline, per Docs/10 §6.3: every token TTL is otherwise
// untestable without sleeping for it, and an inline time.Now() is invisible until somebody tries
// to test around it.
func NewAccessTokenIssuer(keys *Keyset, ttl time.Duration, clk clock.Clock) (*AccessTokenIssuer, error) {
	if keys == nil {
		return nil, fmt.Errorf("%w: no keyset", ErrInvalidKeyset)
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("identity: the access token TTL must be positive, got %s", ttl)
	}
	if clk == nil {
		return nil, fmt.Errorf("identity: an access token issuer needs a clock")
	}
	return &AccessTokenIssuer{keys: keys, ttl: ttl, clock: clk}, nil
}

// TTL is how long the tokens this issuer signs remain valid.
func (i *AccessTokenIssuer) TTL() time.Duration { return i.ttl }

// Issue signs an access token for one account on one device session.
//
// Every input is checked, because a token is a statement this service will later believe: a nil
// subject or a role outside the two would produce a perfectly valid signature over a claim that
// is not true of anybody.
func (i *AccessTokenIssuer) Issue(userID, sessionID uuid.UUID, role Role) (AccessToken, error) {
	if userID == uuid.Nil {
		return AccessToken{}, fmt.Errorf("identity: an access token needs a user")
	}
	if sessionID == uuid.Nil {
		return AccessToken{}, fmt.Errorf("identity: an access token needs a device session")
	}
	if !role.Valid() {
		return AccessToken{}, fmt.Errorf("%w: %q", ErrInvalidRole, role)
	}

	key, err := i.keys.key(i.keys.active)
	if err != nil {
		return AccessToken{}, err
	}

	tokenID, err := uuid.NewV7()
	if err != nil {
		return AccessToken{}, fmt.Errorf("identity: generating a token id: %w", err)
	}

	// Truncated to the second, because `iat` and `exp` are counts of seconds. Without it the
	// difference between them is whatever the sub-second remainder happened to be, and a TTL
	// of exactly fifteen minutes becomes a TTL of about fifteen minutes.
	now := i.clock.Now().UTC().Truncate(time.Second)
	expires := now.Add(i.ttl)

	claims := AccessClaims{
		Subject:   userID.String(),
		Role:      role,
		SessionID: sessionID.String(),
		IssuedAt:  now.Unix(),
		ExpiresAt: expires.Unix(),
		TokenID:   tokenID.String(),
		Issuer:    IssuerName,
		Audience:  MobileAudience,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// The key identifier goes in the header, not the payload: a verifier has to know which key
	// to use before it can trust anything it reads. This is the whole of the rotation
	// mechanism — an old kid keeps verifying while a new one signs.
	token.Header["kid"] = i.keys.active

	signed, err := token.SignedString(key)
	if err != nil {
		return AccessToken{}, fmt.Errorf("identity: signing an access token: %w", err)
	}

	return AccessToken{Value: signed, ExpiresAt: expires}, nil
}

// parse verifies a token and returns its claims, reporting the JWT library's own errors.
//
// It stays unexported and stays on the issuer, because this package's tests use it to assert the
// mechanics — that fifteen minutes is fifteen minutes, that a rotated-out kid still verifies,
// that the driver audience is refused. Those assertions want the library's sentinel errors
// (jwt.ErrTokenExpired, jwt.ErrTokenInvalidAudience) rather than this package's.
//
// What a caller outside this package should use is AccessTokenVerifier (SHIP-44), which returns
// identity's own errors and validates the claims as well as the signature.
func (i *AccessTokenIssuer) parse(raw string) (*AccessClaims, error) {
	return parseAccessToken(i.keys, i.clock, raw)
}

// parseAccessToken is the whole of the cryptographic check, shared by the issuer's own tests and
// by AccessTokenVerifier.
//
// One function rather than two, deliberately. A verifier that drifted from the thing that issues
// the tokens is the defect that matters most here, and the way it happens is two people
// maintaining two parsers.
func parseAccessToken(keys *Keyset, clk clock.Clock, raw string) (*AccessClaims, error) {
	claims := &AccessClaims{}

	parser := jwt.NewParser(
		// The algorithm is pinned, so a token presenting `alg: none` or an asymmetric
		// algorithm cannot talk the verifier into treating the signing key as a public one.
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(IssuerName),
		jwt.WithAudience(MobileAudience),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(func() time.Time { return clk.Now() }),
	)

	if _, err := parser.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		kid, ok := t.Header["kid"].(string)
		if !ok {
			return nil, fmt.Errorf("%w: the token names no key", ErrNoSigningKey)
		}
		return keys.key(kid)
	}); err != nil {
		return nil, err
	}

	return claims, nil
}
