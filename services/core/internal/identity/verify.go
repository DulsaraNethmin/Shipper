// SHIP-44: access token verification.
//
// The other half of SHIP-37. Issue signs a statement; this decides whether to believe one, and
// the two live in the same package and share one parser because a verifier that drifts from the
// issuer is the failure that matters most here.
//
// # What this returns, and what it does not
//
// It returns claims. It does not return an authctx.Subject, and does not import authctx at all —
// the conversion happens in cmd/api, where the domain and the request context legitimately meet.
// That is not ceremony: it keeps this package answering one question ("is this token true?")
// rather than two, and it keeps the HTTP layer's type out of a package that will also be called
// by the worker and by tests that have no request.
//
// # Verification is not authorisation
//
// A valid token says who is calling. It says nothing about what they may do. Docs/07 §3 puts
// every authorisation decision on the platform, and Docs/10 §5 keeps permissions and
// verification state out of the claim set entirely — a customer who was unverified at sign-in
// must not be able to publish for the next fifteen minutes on the strength of a stale claim.
//
// The blank line below keeps this a file note rather than a second package comment.

package identity

import (
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
)

// AccessTokenVerifier decides whether an access token is one this platform issued and still
// honours.
//
// Separate from AccessTokenIssuer because the two have genuinely different dependencies: issuing
// needs the active key and a TTL, verifying needs the whole keyset and no TTL at all. A rotation
// makes that concrete — the outgoing key must keep verifying long after it has stopped signing.
type AccessTokenVerifier struct {
	keys  *Keyset
	clock clock.Clock
}

// NewAccessTokenVerifier builds a verifier over the whole keyset.
func NewAccessTokenVerifier(keys *Keyset, clk clock.Clock) (*AccessTokenVerifier, error) {
	if keys == nil {
		return nil, fmt.Errorf("%w: no keyset", ErrInvalidKeyset)
	}
	if clk == nil {
		return nil, fmt.Errorf("identity: an access token verifier needs a clock")
	}
	return &AccessTokenVerifier{keys: keys, clock: clk}, nil
}

// Verify checks a token and returns its claims.
//
// Two error sentinels rather than a taxonomy, because two is what a caller can act on:
//
//   - [ErrTokenExpired] means refresh and try again. It is the ordinary case — an access token
//     lives fifteen minutes — and it is safe to report precisely, because `exp` is in the
//     payload the client already holds and can decode without any key. Saying so reveals
//     nothing it could not compute.
//   - [ErrTokenInvalid] means everything else: unparseable, wrong signature, wrong algorithm,
//     wrong audience, a retired key, or claims this platform would never have issued. They are
//     deliberately not distinguished on the way out. A caller can do nothing different about
//     them, and telling an unauthenticated caller *which* part of their forgery failed is free
//     help.
//
// The underlying failure is wrapped rather than replaced, so the log keeps the detail the
// response withholds and errors.Is still reaches the JWT library's own sentinels.
func (v *AccessTokenVerifier) Verify(raw string) (*AccessClaims, error) {
	if raw == "" {
		return nil, fmt.Errorf("%w: the token is empty", ErrTokenInvalid)
	}

	claims, err := parseAccessToken(v.keys, v.clock, raw)
	if err != nil {
		// Expiry is asked about with errors.Is rather than by comparison: the parser joins
		// its validation failures together, so the sentinel is nested rather than returned.
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, fmt.Errorf("%w: %w", ErrTokenExpired, err)
		}
		return nil, fmt.Errorf("%w: %w", ErrTokenInvalid, err)
	}

	if err := validateClaims(claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// validateClaims rejects a token that verified cryptographically but says something this
// platform would never have issued.
//
// This is not paranoia about the signature. It is what stops a token minted by an *earlier* or
// *buggier* version of this service — or by a key that leaked and was used carelessly — from
// producing a Subject with an empty user id, which would compare equal to nothing and yet be
// treated as somebody. Issue refuses to create any of these; Verify refuses to believe them.
func validateClaims(claims *AccessClaims) error {
	if err := requireUUID("sub", claims.Subject); err != nil {
		return err
	}
	if err := requireUUID("sid", claims.SessionID); err != nil {
		return err
	}
	if !claims.Role.Valid() {
		return fmt.Errorf("%w: the role claim is %q", ErrTokenInvalid, claims.Role)
	}
	if claims.TokenID == "" {
		return fmt.Errorf("%w: the token carries no jti", ErrTokenInvalid)
	}
	return nil
}

func requireUUID(claim, value string) error {
	if value == "" {
		return fmt.Errorf("%w: the %s claim is empty", ErrTokenInvalid, claim)
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return fmt.Errorf("%w: the %s claim is not an identifier this service issues", ErrTokenInvalid, claim)
	}
	if id == uuid.Nil {
		return fmt.Errorf("%w: the %s claim is the nil identifier", ErrTokenInvalid, claim)
	}
	return nil
}
