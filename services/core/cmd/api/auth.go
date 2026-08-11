package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
)

// The composition root for authentication (SHIP-44).
//
// This file is where two packages that must not know about each other meet. internal/httpx
// declares what it needs — an httpx.Authenticator, a function from a credential to a subject —
// and internal/identity knows how to verify a token and nothing about HTTP. Neither imports the
// other; since SHIP-15c the boundary lint refuses the edge that would join them, because every
// domain imports httpx and one import of identity from inside it would weld all eight to identity
// transitively.
//
// So the translation lives here, and it is three small conversions rather than one, each of which
// would otherwise be somebody's assumption.

// newAccessTokenAuthenticator builds the credential verifier the middleware needs.
//
// It returns an error rather than panicking because the keyset comes from configuration and a
// service that cannot verify tokens should say so at startup, beside the other configuration
// failures, rather than at the first sign-in.
func newAccessTokenAuthenticator(cfg config.Identity, clk clock.Clock) (httpx.Authenticator, error) {
	keys, err := identity.NewKeyset(cfg.AccessTokenKeys, cfg.AccessTokenActiveKID)
	if err != nil {
		return nil, fmt.Errorf("access token keyset: %w", err)
	}

	verifier, err := identity.NewAccessTokenVerifier(keys, clk)
	if err != nil {
		return nil, fmt.Errorf("access token verifier: %w", err)
	}

	return authenticatorFor(verifier), nil
}

// authenticatorFor adapts the identity verifier to what httpx declared.
func authenticatorFor(verifier *identity.AccessTokenVerifier) httpx.Authenticator {
	return func(_ context.Context, credential string) (authctx.Subject, error) {
		claims, err := verifier.Verify(credential)
		if err != nil {
			// The one distinction httpx makes, translated across the boundary. identity
			// does not know httpx's sentinel and httpx cannot name identity's, so this is
			// the only place the two vocabularies meet.
			if errors.Is(err, identity.ErrTokenExpired) {
				return authctx.Subject{}, fmt.Errorf("%w: %w", httpx.ErrCredentialExpired, err)
			}
			return authctx.Subject{}, err
		}
		return subjectFrom(claims)
	}
}

// subjectFrom converts verified claims into the authenticated subject.
//
// identity.Role and authctx.Role are distinct string types with identical values, and that is
// deliberate rather than an oversight: authctx is infrastructure every domain reads, and if it
// took identity's type then every domain checking a role would import identity. The conversion
// is a cast, and the check around it is the part that matters.
//
// Verify has already rejected a role outside the two, so this is belt and braces — but the two
// enumerations live in different packages and can be extended independently, and the failure if
// they ever diverge is a subject carrying a role no domain will match, which reads as "this user
// may do nothing" rather than as an error. Better to say so here.
func subjectFrom(claims *identity.AccessClaims) (authctx.Subject, error) {
	role := authctx.Role(claims.Role)
	if !role.Valid() {
		return authctx.Subject{}, fmt.Errorf(
			"%w: the token's role %q is not one authctx recognises — identity.Role and "+
				"authctx.Role have diverged", identity.ErrTokenInvalid, claims.Role)
	}

	// The identifiers are strings on both sides. identity.Issue takes uuid.UUID and writes
	// them with String(); Verify has already checked that what came back parses as one and is
	// not the nil identifier. authctx keeps them as strings so that no domain reading a
	// subject has to depend on the uuid package to compare one.
	return authctx.Subject{
		UserID:    claims.Subject,
		Role:      role,
		SessionID: claims.SessionID,
	}, nil
}
