package identity

import (
	"errors"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/passwords"
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

	// CodeOTPInvalid is every way a phone verification code fails, and there is deliberately
	// only one of them.
	//
	// Wrong code, expired code, no outstanding code, five wrong guesses already, a number with
	// no account at all: one answer. The email token can afford to distinguish expiry because
	// only somebody holding a genuine token sees it; a six-digit code is guessable, so every
	// distinction is a bit of information handed to whoever is guessing — including the one
	// that matters most, which is whether the number has an account.
	//
	// The remedy is the same for all of them: ask for a new code.
	CodeOTPInvalid = httpx.RegisterCode("identity_otp_invalid",
		"That code is not valid. Ask for a new one and try again.")

	// CodeRefreshTokenInvalid is every way a refresh token cannot be exchanged, and there is
	// deliberately only one of them (SHIP-42).
	//
	// Never issued, already rotated away, expired, a session that has been signed out or
	// revoked, an account that has been suspended: one answer, and the remedy is the same for
	// all of them — sign in again. Distinguishing them would tell somebody holding a stolen
	// token which part of it the platform recognised, and in the reuse case it would tell them
	// the theft had been noticed.
	//
	// **It is a 400 rather than a 401, and the reason is the client's control flow.** SHIP-50's
	// interceptor refreshes on a 401 and replays the request; a 401 from the refresh endpoint
	// itself is the one answer that can send a naive implementation round the loop again. The
	// credential here also travels in the request body rather than in the header a bearer
	// token uses, so a WWW-Authenticate challenge would describe a scheme this endpoint does
	// not accept. That matches how verify-email and verify-phone already answer for a
	// credential that arrives in the body.
	CodeRefreshTokenInvalid = httpx.RegisterCode("identity_refresh_token_invalid",
		"This session has ended. Sign in again.")

	// CodeCredentialsInvalid is every way a sign-in fails to identify an account holder, and
	// there is deliberately only one of them (SHIP-41).
	//
	// No account with that address, and the right address with the wrong password: one answer.
	// Two answers would make this endpoint an account-existence oracle that anybody can query
	// without a credential — worse than registration's disclosure, which at least costs the
	// caller an address they control and is bounded by SHIP-47's limit. The service also spends
	// the same argon2id work on both paths, because a distinction the *status code* refuses to
	// make is one the response *time* would otherwise make for it.
	//
	// **It is a 400 rather than a 401, and the rule is worth stating once for the whole
	// domain: a credential presented in the request body is refused with 400; a credential
	// presented in the bearer header is refused with 401.** [1] `WWW-Authenticate` is
	// required on a 401 (RFC 9110 §11.6.1) and would describe a bearer scheme this endpoint does
	// not accept. [2] SHIP-50's interceptor refreshes on a 401 and replays the request, and a
	// 401 from sign-in is the one answer that can send a naive implementation round that loop.
	// verify-email, verify-phone and refresh all already answer this way.
	CodeCredentialsInvalid = httpx.RegisterCode("identity_credentials_invalid",
		"That email address and password do not match an account. Deliberately one code for both halves, so this endpoint cannot be used to find out which addresses have accounts.")

	// CodeAccountSuspended means the password was right and the account may not be used
	// (SHIP-41).
	//
	// This is the one place account standing is disclosed, and it is safe here precisely
	// because it is said *after* the password verified: the caller has just proved they own the
	// account they are being told about. Refresh deliberately does not say it — the caller there
	// holds only a token, which may have been stolen.
	//
	// Restricted accounts sign in normally. Docs/01 §4.2 narrows what they may *do*, which is a
	// decision each domain makes at the point of doing it, and an account that cannot sign in
	// cannot read the messages explaining why it is restricted.
	CodeAccountSuspended = httpx.RegisterCode("identity_account_suspended",
		"The account has been suspended. Signing in is refused until support lifts it; contact support rather than retrying.")

	// CodeSessionNotFound means the device session named by the request is not one the caller
	// owns (SHIP-46).
	//
	// A session belonging to somebody else and a session that never existed are deliberately
	// indistinguishable, which is what stops the revoke endpoint being used to probe whether an
	// identifier is a real session.
	CodeSessionNotFound = httpx.RegisterCode("identity_session_not_found",
		"No such device session on this account. A session belonging to somebody else answers identically.")
)

// The sentinel errors this domain raises.
//
// Docs/10 §2.1 puts them here rather than beside the code that returns them, so that a caller
// deciding what to do about a failure has one file to read. The codes above are what a *client*
// sees; these are for Go callers, and http.go is where one becomes the other.
var (
	// The three password errors are internal/passwords' values, bound to the names this domain
	// has published since SHIP-29 (SHIP-15r).
	//
	// **They are the same values, not translations of them**, and that is the whole of why the
	// move was safe: `errors.Is(err, identity.ErrMalformedPasswordHash)` and
	// `errors.Is(err, passwords.ErrMalformedHash)` answer identically because there is one
	// `errors.New` behind both. A fresh sentinel here plus a mapping somewhere in between would be
	// two values agreeing by convention, and the first path that forgot the mapping would report an
	// unreadable stored hash as an unmapped 500 — collapsing exactly the distinction the third of
	// them exists to keep.
	//
	// Kept rather than deleted because Docs/10 §2.1 makes this file the one place a caller reads to
	// decide what to do about a failure, and [Service.SignIn] can return all three.

	// ErrEmptyPassword is returned rather than hashing the empty string, which would
	// otherwise produce a perfectly valid hash that any empty submission then matches.
	// Length and strength rules are the registration endpoint's (SHIP-30); this is only the
	// floor below which hashing is meaningless.
	ErrEmptyPassword = passwords.ErrEmptyPassword

	// ErrMalformedPasswordHash means the stored PHC string could not be read: a truncated
	// column, a hash written by something else, or a value someone has edited.
	//
	// It is deliberately distinct from "the password did not match". A wrong password is an
	// ordinary event; an unreadable hash is a data defect, and answering "wrong password" to
	// it would hide the defect behind a sign-in failure the owner cannot explain.
	ErrMalformedPasswordHash = passwords.ErrMalformedHash

	// ErrInvalidArgon2Profile means the cost parameters are outside the range internal/passwords
	// will run — either configured that way, or read out of a hash that has been tampered
	// with. See that package's argon2Bounds for why the range exists.
	ErrInvalidArgon2Profile = passwords.ErrInvalidProfile

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

	// ErrOTPInvalid is every unusable phone verification code (SHIP-36). See CodeOTPInvalid
	// for why there is only one.
	ErrOTPInvalid = errors.New("identity: the one-time code is not usable")

	// ErrRefreshTokenInvalid is every way a refresh token cannot be exchanged (SHIP-39):
	// empty, never issued, already rotated away, past its expiry, belonging to a revoked
	// session, or held by an account that may no longer sign in.
	//
	// Deliberately one error for all of them, and the remedy is the same in every case — sign
	// in again. Distinguishing them would tell somebody holding a stolen or guessed token
	// which part of it the platform recognised, and in the suspended-account case it would
	// disclose account standing to whoever is holding the token rather than to the person who
	// owns it.
	ErrRefreshTokenInvalid = errors.New("identity: the refresh token is not usable")

	// ErrRefreshTokenReused means a token that had already been rotated away was presented
	// again, and the whole device session has been revoked as a result (SHIP-40).
	//
	// **The caller is told exactly what ErrRefreshTokenInvalid tells them** — apiError maps
	// both to one code, and the remedy is the same. This exists so the service can log a
	// security event with the session attached, and so a test can tell "the session was
	// revoked" from "the token was refused", which are different claims about what happened.
	//
	// Docs/07 §3 is what makes it a revocation rather than a refusal: either the device
	// replayed the token, which SHIP-50's interceptor exists to prevent, or somebody else has
	// a copy — and the platform cannot tell those apart, so it ends the session for both.
	ErrRefreshTokenReused = errors.New("identity: the refresh token has already been used")

	// ErrCredentialsInvalid means the address and password together name no account
	// (SHIP-41). See CodeCredentialsInvalid for why the two halves are not distinguished.
	ErrCredentialsInvalid = errors.New("identity: the email address and password do not match an account")

	// ErrAccountSuspended means the password was right and the account may not hold a session
	// (SHIP-41). Raised only after the credential verified — see CodeAccountSuspended.
	ErrAccountSuspended = errors.New("identity: the account is suspended")

	// ErrSessionNotFound means the caller named a device session that is not theirs, or is not
	// one at all (SHIP-46). The two are one error deliberately.
	ErrSessionNotFound = errors.New("identity: no such device session on this account")

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
