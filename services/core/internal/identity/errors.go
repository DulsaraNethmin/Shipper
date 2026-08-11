package identity

import (
	"errors"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// The machine-readable error codes this domain raises (SHIP-30).
//
// Declared here, in the domain, rather than in a shared list — Docs/10 §4.4. A single file of
// every code in the platform is a file every domain edits, which is the merge hazard the route
// manifest and the contract fragments both exist to avoid. httpx.RegisterCode refuses a
// duplicate, so two domains cannot quietly mean different things by one string.
//
// The description is what Docs/10-api-error-codes.md says the code means, and that document is
// what three client codebases branch on. Regenerate it after adding one:
//
//	go test ./cmd/api -run TestErrorCodeDocumentIsCurrent -update
var (
	// CodeEmailTaken means the address already has an account.
	//
	// Registration is the one place this platform tells an unauthenticated caller whether an
	// address is known, and it is a deliberate trade rather than an oversight. The
	// alternative — accepting the registration and sending "you already have an account" by
	// email — is what a bank does; here it would leave a person who mistyped their address
	// staring at a success screen for an account that does not exist. The resend and OTP
	// endpoints, where the disclosure buys the caller nothing, do not make it.
	CodeEmailTaken = httpx.RegisterCode("identity_email_taken",
		"An account already exists for this email address. Sign in, or reset the password.")

	// CodePhoneTaken means the number already has an account. Same trade as CodeEmailTaken,
	// and the number has to be unique for a different reason as well: two accounts sharing
	// one number would make an OTP ambiguous about which of them it verifies.
	CodePhoneTaken = httpx.RegisterCode("identity_phone_taken",
		"An account already exists for this mobile number. Sign in, or reset the password.")

	// CodeVerificationTokenInvalid covers every way an email verification token is not
	// usable except one: unknown, already used, superseded by a resend, or issued for an
	// address the account no longer has.
	//
	// Deliberately undifferentiated. A caller can do nothing different about any of them, and
	// telling somebody holding a guessed token which part of the guess was wrong is free help.
	CodeVerificationTokenInvalid = httpx.RegisterCode("identity_verification_token_invalid",
		"This verification link is no longer valid. Ask for a new one.")

	// CodeVerificationTokenExpired means the token was genuine and its day is up.
	//
	// Reported separately from `identity_verification_token_invalid` because the remedy is
	// specific — ask for another — and because it is safe: only somebody holding a token this
	// platform issued can see it, and they could have worked out its age from the day they
	// received it.
	CodeVerificationTokenExpired = httpx.RegisterCode("identity_verification_token_expired",
		"This verification link has expired. Ask for a new one.")
)

// The sentinel errors this domain raises.
//
// Docs/10 §2.1 puts them here rather than beside the code that returns them, so that a caller
// deciding what to do about a failure has one file to read. The codes above are what a *client*
// sees; these are for Go callers, and http.go is where one becomes the other.
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

	// ErrEmailTaken means the address already has an account (SHIP-30).
	//
	// It is raised from the unique-index violation rather than from a SELECT that ran first.
	// A check-then-insert is a race with a window wide enough to lose in practice — two
	// registrations for one address arriving together — and the database already refuses the
	// second one. Docs/06 §4.1 is the same argument the one-accepted-bid index rests on.
	ErrEmailTaken = errors.New("identity: that email address already has an account")

	// ErrPhoneTaken means the number already has an account (SHIP-30).
	ErrPhoneTaken = errors.New("identity: that mobile number already has an account")

	// ErrVerificationTokenInvalid is every unusable email verification token except an expired
	// one (SHIP-33). See CodeVerificationTokenInvalid for why they are not distinguished.
	ErrVerificationTokenInvalid = errors.New("identity: the verification token is not usable")

	// ErrVerificationTokenExpired means the token was genuine and has lapsed (SHIP-33).
	ErrVerificationTokenExpired = errors.New("identity: the verification token has expired")

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
