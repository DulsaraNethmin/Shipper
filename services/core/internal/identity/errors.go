package identity

import "errors"

// The sentinel errors this domain raises.
//
// Docs/10 §2.1 puts them here rather than beside the code that returns them, so that a caller
// deciding what to do about a failure has one file to read. The machine-readable error codes
// that reach a client are a separate list and arrive with the first endpoint (SHIP-30); these
// are for Go callers.
var (
	// ErrEmptyPassword is returned rather than hashing the empty string, which would
	// otherwise produce a perfectly valid hash that any empty submission then matches.
	// Length and strength rules are the registration endpoint's (SHIP-30); this is only the
	// floor below which hashing is meaningless.
	ErrEmptyPassword = errors.New("identity: the password is empty")

	// ErrMalformedPasswordHash means the stored PHC string could not be read: a truncated
	// column, a hash written by something else, or a value someone has edited.
	//
	// It is deliberately distinct from "the password did not match". A wrong password is an
	// ordinary event; an unreadable hash is a data defect, and answering "wrong password" to
	// it would hide the defect behind a sign-in failure the owner cannot explain.
	ErrMalformedPasswordHash = errors.New("identity: the stored password hash is malformed")

	// ErrInvalidArgon2Profile means the cost parameters are outside the range this package
	// will run — either configured that way, or read out of a hash that has been tampered
	// with. See argon2Bounds for why the range exists.
	ErrInvalidArgon2Profile = errors.New("identity: the argon2id profile is out of range")

	// ErrNoSigningKey means the keyset does not hold the key a token needs: an active key
	// identifier naming a key that was never supplied, or a token presenting a kid that has
	// been retired.
	ErrNoSigningKey = errors.New("identity: no signing key with that identifier")

	// ErrInvalidKeyset means the keyset itself cannot be used — no keys, no active key
	// identifier, or a key too short to sign with.
	ErrInvalidKeyset = errors.New("identity: the signing keyset is unusable")

	// ErrInvalidRole means a role outside the two the users table permits. Docs/01 has no
	// third role: administrators sign in through a separate system entirely (SHIP-147).
	ErrInvalidRole = errors.New("identity: not a role this platform issues tokens for")

	// ErrTokenExpired means the access token was genuine and its fifteen minutes are up
	// (SHIP-44).
	//
	// Distinct from ErrTokenInvalid because it is the one verification failure a client can
	// act on: refresh and retry, rather than sign in again. Reporting it precisely leaks
	// nothing — `exp` sits in the payload the client already holds and can decode without
	// any key, so this tells them only what they could have worked out themselves.
	ErrTokenExpired = errors.New("identity: the access token has expired")

	// ErrTokenInvalid means the token is not one this platform will honour: unparseable,
	// wrongly signed, the wrong algorithm, the wrong audience, signed by a retired key, or
	// carrying claims the issuer would never have written.
	//
	// Deliberately one error for all of those. A caller can do nothing different about any of
	// them, and telling an unauthenticated caller which part of their forgery was detected is
	// help they have not earned. The underlying cause is wrapped, so the log keeps what the
	// response withholds.
	ErrTokenInvalid = errors.New("identity: the access token is not valid")
)
