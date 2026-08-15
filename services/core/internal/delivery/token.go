// SHIP-107: the driver's job-scoped token.
//
// Signed, time-limited, and naming exactly one job — minted on assignment and on nothing else, so
// that a token cannot exist without a driver_assignments row behind it.
//
// # This is a second token system, and none of it is shared with the first
//
// Docs/10 §5 gives the driver token three properties and every one of them is a separation from
// the mobile access token identity issues: **its own signing key material**, `aud=shipper-driver`,
// and exactly one `job_id`. CLAUDE.md states the invariant the three exist to make structural —
// "the driver's job-scoped token and the mobile auth token are separate systems, and neither can
// be exchanged for the other".
//
// So nothing below imports internal/identity, and that is a rule rather than an inconvenience:
// domains do not import each other (Docs/06 §4.1) and the import lint refuses it. The keyset, the
// claim shape and the parser are this package's own. What keeps the two honest is not shared code
// but the audience: identity's parser pins `shipper-mobile` and this one pins `shipper-driver`, so
// a token from either side is refused by the other **even if the key material were somehow the
// same** — which is exactly the case both tests use, because it is the only one where the audience
// is doing the work alone.
//
// # The claim set has no `sub`, and that is the load-bearing omission
//
// An [authctx.Subject] is built from a user id. This token carries none: there is no `sub`, no
// `role` and no `sid`, because a driver has no account, no role and no session (Docs/07 §3). A
// helper that turned a verified driver token into a subject would therefore have nothing to put
// in it — the exchange Docs/10 §5 forbids is not merely refused, it has no material to work from.
// That is a stronger guarantee than a rule somebody has to remember, and it is why the claim set
// is closed and a test holds it to exactly these seven keys.
//
// # What SHIP-108 adds and what it must not
//
// [parseDriverToken] is the whole cryptographic check and it stays unexported, exactly as
// identity's `parseAccessToken` does: this package's tests use it to assert the mechanics, and
// SHIP-108 builds the exported verifier and the guard on top of it. One function rather than two,
// because a verifier that drifted from the thing issuing the tokens is the defect that matters
// most here. SHIP-108 must still not resolve what it returns into an `authctx.Subject`; see
// cmd/api/driverauth.go, which carries that rule at the seam.
//
// The blank line below keeps this a file note rather than a second package comment.

package delivery

import (
	"bytes"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
)

const (
	// IssuerName and DriverAudience are the `iss` and `aud` claims of every driver token.
	//
	// **The issuer is the same string identity uses and the audience is deliberately not.**
	// The issuer names the platform, and there is one platform; the audience names who the
	// token is for, and that is the whole separation. Writing "shipper" out here rather than
	// importing identity's constant is the rule, not an oversight — domains do not import each
	// other, and normalisePhone in assignment.go is the same duplication for the same reason.
	// What holds the two in step is that a token failing either check is refused, so a drift in
	// the issuer would be caught by the first driver token that failed to verify rather than by
	// a compile error nobody would get.
	IssuerName     = "shipper"
	DriverAudience = "shipper-driver"

	// minimumSigningKeyBytes is the shortest key HS256 is worth using — HMAC-SHA256 produces
	// 256 bits, and a key shorter than its own output is the part of the scheme an attacker
	// goes at first.
	//
	// A third copy of the same constant, beside internal/identity's and internal/config's, and
	// the reason is the same both times: neither of those may be imported from here. internal/config
	// refuses a short key at startup, which is where it is actually caught; this refuses one
	// handed straight to [NewKeyset], which is what a test or a future caller might do.
	minimumSigningKeyBytes = 32
)

// Keyset is the HMAC signing material for driver tokens, indexed by key identifier.
//
// Structurally identical to identity.Keyset and deliberately a separate type holding separate
// bytes. Two keysets is what "separate signing key material" means in practice, and
// internal/config refuses a configuration in which the two sets share a secret — so a deployment
// cannot arrive at one keyset by accident.
//
// More than one key so that rotation is a configuration change: the incoming key becomes active
// and signs new tokens while the outgoing one stays in the set and keeps verifying the tokens it
// already signed, until the last of them has expired. Removing it before then would break every
// link a provider has already forwarded to a driver, and unlike a mobile client there is nothing
// on the other end that can refresh.
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

// DriverClaims is the whole claim set of a driver token, and the list is closed.
//
// Seven claims: `job_id`, `assignment_id`, `iat`, `exp`, `jti`, `iss`, `aud`. Nothing else — and
// specifically **no `sub`, no `role` and no `sid`**, which is the file note's point. A driver has
// no account to be the subject of, no role the platform would read from a claim (Docs/07 §3 puts
// every authorisation decision on the platform), and no device session.
//
// The fields are declared here rather than embedding jwt.RegisteredClaims so that the JSON on the
// wire is exactly this and nothing more. An embedded struct would emit whatever it happens to
// carry, which is a claim set that changes when a dependency is upgraded — and `sub` is one of the
// fields it carries.
type DriverClaims struct {
	// JobID is the one job this token opens. Docs/10 §5 names it in full, and it is the whole
	// of what "single-job" means: the identifier is inside the signature, so a token cannot be
	// repointed at another job without being re-signed.
	JobID string `json:"job_id"`

	// AssignmentID is the driver_assignments row the grant belongs to.
	//
	// **Not decoration.** A driver has no account, so the assignment row *is* their identity
	// for the length of one job — assignment.go says so and 000601 followed 000401's convention
	// in expecting it: milestone.go's ActorDriver "identifies the actor by their
	// driver_assignments row rather than by an account". A milestone recorded by a driver has to
	// name something, and this is the only thing there is to name. Without this claim SHIP-108
	// could authenticate a driver and still not be able to attribute their work.
	//
	// It is also what makes SHIP-109 cheap: a token whose assignment is no longer live names a
	// row that says so, so revocation is a lookup rather than a denylist.
	AssignmentID string `json:"assignment_id"`

	IssuedAt  int64 `json:"iat"`
	ExpiresAt int64 `json:"exp"`

	// TokenID identifies this token uniquely.
	//
	// Two links issued for one assignment in the same second are otherwise byte-identical, which
	// would make "reissue, invalidating the previous one" (SHIP-109) impossible to express: there
	// would be nothing to name the outgoing one by.
	TokenID string `json:"jti"`

	Issuer string `json:"iss"`

	// Audience is a single string rather than an array, matching the mobile token's shape. RFC
	// 7519 permits either for one audience, and this platform issues and verifies its own
	// tokens, so the plain form is the one with fewer ways to be read.
	Audience string `json:"aud"`
}

// The jwt.Claims interface, so the library's validator can read the registered claims out of a
// struct that does not embed its RegisteredClaims.

func (c DriverClaims) GetExpirationTime() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.ExpiresAt, 0)), nil
}

func (c DriverClaims) GetIssuedAt() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.IssuedAt, 0)), nil
}

// GetNotBefore reports none. A driver token is valid the moment it is issued — the provider
// forwards the link immediately (Docs/01 §4.5) — so `nbf` would be a claim with nothing to say.
func (c DriverClaims) GetNotBefore() (*jwt.NumericDate, error) { return nil, nil }

func (c DriverClaims) GetIssuer() (string, error) { return c.Issuer, nil }

// GetSubject reports none, and the empty string is the honest answer rather than a gap.
//
// The interface requires the method; this token has no `sub` claim to answer it with, by design.
// Nothing in this package reads it, and a verifier that wanted an identity out of a driver token
// finds an empty string — which is the point made in the file note above.
func (c DriverClaims) GetSubject() (string, error) { return "", nil }

func (c DriverClaims) GetAudience() (jwt.ClaimStrings, error) {
	return jwt.ClaimStrings{c.Audience}, nil
}

// Job and Assignment parse the two identifiers back out of a claim set.
//
// A token is a string somebody else held, so its claims are input: a `job_id` that is not a UUID
// is a token nothing should act on, and reporting that as an error rather than as uuid.Nil is what
// stops a malformed grant reading as a grant over the nil job.

func (c DriverClaims) Job() (uuid.UUID, error) {
	id, err := uuid.Parse(c.JobID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: job_id %q is not an identifier", ErrMalformedDriverToken, c.JobID)
	}
	return id, nil
}

func (c DriverClaims) Assignment() (uuid.UUID, error) {
	id, err := uuid.Parse(c.AssignmentID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: assignment_id %q is not an identifier",
			ErrMalformedDriverToken, c.AssignmentID)
	}
	return id, nil
}

// DriverToken is an issued token, what it grants, and when it stops being valid.
//
// The expiry travels with the value because the assignment response tells the provider how long
// the link they are about to forward will work for, and recomputing it from a TTL at the call site
// is how the two end up disagreeing.
type DriverToken struct {
	Value string

	// ID is the token's own `jti`, which is what SHIP-109 records against the assignment to make
	// this the one link that opens it. It is returned rather than re-parsed out of Value, which is
	// a thing nothing outside the verifier should be doing to a credential.
	ID string

	JobID        uuid.UUID
	AssignmentID uuid.UUID

	ExpiresAt time.Time
}

// DriverTokenIssuer signs job-scoped driver tokens.
type DriverTokenIssuer struct {
	keys  *Keyset
	ttl   time.Duration
	clock clock.Clock
}

// NewDriverTokenIssuer builds an issuer.
//
// The clock is injected rather than read inline, per Docs/10 §6.3. It is load-bearing twice over
// here: a seven-day TTL is otherwise untestable without waiting for it, and the expiry test that
// matters — that an expired token is refused — needs a clock that can be moved past the expiry
// rather than a token minted in the past.
func NewDriverTokenIssuer(keys *Keyset, ttl time.Duration, clk clock.Clock) (*DriverTokenIssuer, error) {
	if keys == nil {
		return nil, fmt.Errorf("%w: no keyset", ErrInvalidKeyset)
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("delivery: the driver token TTL must be positive, got %s", ttl)
	}
	if clk == nil {
		return nil, fmt.Errorf("delivery: a driver token issuer needs a clock")
	}
	return &DriverTokenIssuer{keys: keys, ttl: ttl, clock: clk}, nil
}

// TTL is how long the tokens this issuer signs remain valid.
func (i *DriverTokenIssuer) TTL() time.Duration { return i.ttl }

// Issue signs a token granting one driver access to one job.
//
// Both identifiers are required and neither may be nil. A token is a statement this service will
// later believe, and a perfectly valid signature over uuid.Nil would be a grant over a job that
// does not exist — which a verifier has no way to tell from a grant over one that does.
func (i *DriverTokenIssuer) Issue(jobID, assignmentID uuid.UUID) (DriverToken, error) {
	tokenID, err := uuid.NewV7()
	if err != nil {
		return DriverToken{}, fmt.Errorf("delivery: generating a driver token id: %w", err)
	}
	return i.IssueAs(jobID, assignmentID, tokenID.String())
}

// IssueAs signs a token carrying a link identifier the caller already holds (SHIP-109).
//
// # It exists so that "nothing was written" can also mean "nothing was revoked"
//
// `driver_assignments.link_token_id` names the one link that opens an assignment, so minting a
// fresh identifier is what ends the previous link. That is right for a reissue and wrong for the
// path [Service.AssignDriver] takes when a provider nominates the same driver twice: the second
// request wrote no row, moved no job and emitted no event, and a provider whose phone lost the
// first response must not silently cut off a driver already holding the link. Re-signing the same
// identifier hands them a working credential and leaves the driver's alone.
//
// The two tokens are different values — `iat` and `exp` are read from the clock — and both verify
// and both match the column, which is the intended outcome rather than an accident: they are two
// renderings of one grant.
//
// **Only [Service.ReissueDriverLink] and a new assignment mint a fresh identifier**, which is what
// makes revocation something a provider asks for rather than something a retry causes.
func (i *DriverTokenIssuer) IssueAs(jobID, assignmentID uuid.UUID, tokenID string) (DriverToken, error) {
	if jobID == uuid.Nil {
		return DriverToken{}, fmt.Errorf("delivery: a driver token needs the job it grants")
	}
	if assignmentID == uuid.Nil {
		return DriverToken{}, fmt.Errorf("delivery: a driver token needs the assignment it belongs to")
	}
	if _, err := uuid.Parse(tokenID); err != nil {
		// Refused rather than replaced with a fresh one, because a caller that passed something
		// unusable meant to re-sign a specific link and would otherwise get a *different* link
		// back — which is a revocation nobody asked for. ck_driver_assignments_link_token_id
		// holds the same shape in the database.
		return DriverToken{}, fmt.Errorf("delivery: %q is not a link identifier: %w",
			tokenID, ErrMalformedDriverToken)
	}

	key, err := i.keys.key(i.keys.active)
	if err != nil {
		return DriverToken{}, err
	}

	// Truncated to the second, because `iat` and `exp` are counts of seconds. Without it the
	// difference between them is whatever the sub-second remainder happened to be, and a TTL of
	// exactly seven days becomes a TTL of about seven days.
	now := i.clock.Now().UTC().Truncate(time.Second)
	expires := now.Add(i.ttl)

	claims := DriverClaims{
		JobID:        jobID.String(),
		AssignmentID: assignmentID.String(),
		IssuedAt:     now.Unix(),
		ExpiresAt:    expires.Unix(),
		TokenID:      tokenID,
		Issuer:       IssuerName,
		Audience:     DriverAudience,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// The key identifier goes in the header, not the payload: a verifier has to know which key
	// to use before it can trust anything it reads.
	token.Header["kid"] = i.keys.active

	signed, err := token.SignedString(key)
	if err != nil {
		return DriverToken{}, fmt.Errorf("delivery: signing a driver token: %w", err)
	}

	return DriverToken{
		Value:        signed,
		ID:           tokenID,
		JobID:        jobID,
		AssignmentID: assignmentID,
		ExpiresAt:    expires,
	}, nil
}

// parse verifies a token and returns its claims, reporting the JWT library's own errors.
//
// It stays unexported and stays on the issuer, exactly as identity's does: this package's tests
// use it to assert the mechanics — that seven days is seven days, that a tampered payload is
// refused, that a rotated-out kid still verifies, that a mobile token is not accepted here — and
// those assertions want the library's sentinel errors (jwt.ErrTokenExpired,
// jwt.ErrTokenInvalidAudience, jwt.ErrSignatureInvalid) rather than this package's.
//
// **SHIP-108 is what builds the exported verifier over [parseDriverToken]**, returning this
// domain's errors and the grant a request is served under. Nothing outside this package can verify
// a driver token today, which is correct: no route accepts one.
func (i *DriverTokenIssuer) parse(raw string) (*DriverClaims, error) {
	return parseDriverToken(i.keys, i.clock, raw)
}

// parseDriverToken is the whole of the cryptographic check, shared by the issuer's own tests and
// by the verifier SHIP-108 adds.
//
// One function rather than two, deliberately. A verifier that drifted from the thing that issues
// the tokens is the defect that matters most here, and the way it happens is two people
// maintaining two parsers.
//
// # The audience is pinned, and Docs/10 §5 says it is checked before anything else
//
// jwt.WithAudience is applied by the library's validator, after the signature — which is the right
// order and is not what that sentence is about. What it is about is that **no code path here reads
// a claim out of a token whose audience has not been checked**: the parser either returns a claim
// set that passed every check or it returns an error, so a caller cannot act on a
// `shipper-mobile` token by forgetting a step.
func parseDriverToken(keys *Keyset, clk clock.Clock, raw string) (*DriverClaims, error) {
	claims := &DriverClaims{}

	parser := jwt.NewParser(
		// The algorithm is pinned, so a token presenting `alg: none` or an asymmetric
		// algorithm cannot talk the verifier into treating the signing key as a public one.
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(IssuerName),
		jwt.WithAudience(DriverAudience),
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
