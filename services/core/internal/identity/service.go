// The domain rules for registration, verification and the transaction boundaries around them.
//
// Docs/10 §2.2 fixes the layering at two: this file holds the rules and owns the transaction,
// and postgres.go holds the SQL. There is no repository interface between them, because
// Docs/06 §4.1 is explicit that PostgreSQL is not abstracted here — a mock accepts the write
// the unique index exists to reject, and the unique index is how "one account per address" is
// actually enforced.
//
// The blank line below keeps this a file note rather than a second package comment.

package identity

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/passwords"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/ratelimit"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// The limits registration applies.
//
// Constants here rather than configuration, and that is a scope decision worth naming.
// Docs/06 §5.3 requires anything expected to move under operational pressure to live
// server-side — which these do, in the sense that changing them is a deploy of this service
// rather than a store release. None of them is a lever anybody has asked to pull without one:
// a password floor that moves weekly is not a floor. The category lists and job limits that
// §5.3 is actually about are SHIP-58's, and they are reference data rather than constants.
const (
	// minPasswordLength follows NIST SP 800-63B: length, and no composition rules.
	//
	// Requiring an uppercase letter and a symbol produces `Password1!` — it narrows the
	// search space an attacker uses while making the password harder to remember, which is
	// the opposite of both intentions. What actually protects the credential here is
	// argon2id at m=64 MiB (SHIP-29) and the rate limit on sign-in (SHIP-47).
	minPasswordLength = 10

	// maxPasswordLength bounds what is handed to argon2id. There is no security reason for a
	// ceiling — the hash is fixed-width whatever goes in — but an unbounded field is CPU a
	// caller controls, and 128 characters is past any passphrase anyone types.
	maxPasswordLength = 128

	// maxEmailLength is RFC 5321's limit on a path, which is the longest an address can be
	// and still be deliverable.
	maxEmailLength = 254

	// maxPhoneLength bounds the *submitted* value before normalisation, generously enough to
	// admit spaces, brackets and a country code. E.164 itself caps the result at fifteen
	// digits, which normalisePhone enforces.
	maxPhoneLength = 32

	// minNameLength and maxNameLength bound what a person may call themselves (SHIP-30a).
	//
	// **The floor is one character and that is a decision rather than a slack default.** A name
	// is not a credential and it is not a format: mononyms exist, single-character given names
	// exist, and every rule anybody has ever written about "a real name" — two words, a space, a
	// minimum length, letters only — excludes somebody real. The one thing the platform can say
	// honestly is that the field was answered, which is what [validate.Errors.Required] and
	// `ck_users_name` between them say. Docs/04 §3 asks for identity *evidence* where identity
	// matters, and that is a verification document rather than a text field.
	//
	// The ceiling is what stops the column becoming a document store, on the same reasoning as
	// maxPasswordLength: an unbounded text field is storage a caller controls. 120 runes is past
	// any name in ISO/IEC 7501's machine-readable zone, which allocates 39.
	minNameLength = 1
	maxNameLength = 120
)

// Service holds the domain rules for identity.
//
// It takes the pool rather than a transaction: Docs/10 §3.2 puts the transaction with the
// domain that owns the invariant, and registration's invariant — one account per address, one
// per number — is this domain's. Persistence methods take a db.Runner so the same SQL works
// inside a transaction or straight against the pool, decided by the caller rather than by the
// method.
type Service struct {
	pool    *pgxpool.Pool
	hasher  *passwords.Hasher
	issuer  *AccessTokenIssuer
	limiter *ratelimit.Limiter
	email   EmailSender
	sms     SMSSender
	clock   clock.Clock
	store   postgresStore

	// activeJobs answers whether this account is in the middle of a delivery (SHIP-170).
	//
	// The first port this domain holds over another *domain* rather than over an adapter —
	// see ports.go. It is required rather than optional: a nil one would make every deletion
	// request live, which is the exact defect Docs/05 §3.1 forbids and which nothing in a
	// response would show.
	activeJobs ActiveJobs
}

// NewService builds the domain service.
//
// The pool may legitimately be nil at construction — the process starts with an unreachable
// database on purpose, so that a failover does not take every instance down at once (see the
// note on Deps in cmd/api). What is refused here is a *missing collaborator*, which is a wiring
// mistake rather than a transient condition: a service with no hasher would accept a password
// and store nothing derivable from it, and one with no email or SMS sender would register
// accounts that can never be verified.
func NewService(pool *pgxpool.Pool, hasher *passwords.Hasher, issuer *AccessTokenIssuer, limiter *ratelimit.Limiter, sender EmailSender, texter SMSSender, jobs ActiveJobs, clk clock.Clock) (*Service, error) {
	if hasher == nil {
		return nil, errors.New("identity: a service needs a password hasher")
	}
	if issuer == nil {
		// SHIP-39 onwards. A session is a refresh token and the access tokens it produces, so
		// a service that could create one without an issuer would be a service that hands out
		// half a credential — a refresh token the caller cannot exchange for anything.
		return nil, errors.New("identity: a service needs an access token issuer")
	}
	if limiter == nil {
		// SHIP-47. A limiter whose Redis client is nil is legitimate — it refuses
		// everything, which is the fail-closed direction — but no limiter at all is a
		// service that would serve sign-in with nothing counting the guesses.
		return nil, errors.New("identity: a service needs a rate limiter")
	}
	if sender == nil {
		return nil, errors.New("identity: a service needs an email sender")
	}
	if texter == nil {
		return nil, errors.New("identity: a service needs an SMS sender")
	}
	if jobs == nil {
		// SHIP-170. A service without it would answer every deletion request as live, and
		// Docs/05 §3.1's rule would be silently absent rather than visibly broken — the
		// person would be told a date, the request would look ordinary, and the failure
		// would only surface when SHIP-171 erased somebody mid-delivery.
		return nil, errors.New("identity: a service needs an active-job lookup")
	}
	if clk == nil {
		return nil, errors.New("identity: a service needs a clock")
	}
	return &Service{
		pool: pool, hasher: hasher, issuer: issuer, limiter: limiter,
		email: sender, sms: texter, activeJobs: jobs, clock: clk,
	}, nil
}

// RegisterCommand is one registration request, in the domain's own terms (SHIP-30).
//
// It is a domain type rather than the HTTP request struct so that validation lives with the
// rules rather than with the transport. The distinction matters at SHIP-51, where the Flutter
// client validates the same fields for a better error experience and the platform still decides
// — Docs/07 §3: the app may hide or disable, the platform rules.
type RegisterCommand struct {
	// Name is what the account holder is called (SHIP-30a).
	//
	// Required, and it is the only field here whose absence was a *later* ticket's problem
	// rather than this one's: SHIP-151's account search names four terms and could serve three,
	// because nothing had ever collected the fourth. Collected here because a name cannot be
	// backfilled — an account that exists without one has no source to recover it from.
	Name     string
	Email    string
	Phone    string
	Password string
	Role     Role
}

// Normalise puts the command into the form the database stores.
//
// Separate from Validate and run before it, so that a value is judged in the shape it will be
// kept in. " Alice@Example.COM " and "alice@example.com" are one address to every mail system
// and to the citext column; trimming after the uniqueness check would let the first through.
func (c *RegisterCommand) Normalise() {
	// Trimmed and never case-folded. `ck_users_name` refuses a blank name and this is what
	// makes "   " blank rather than three characters; lower-casing it would be the platform
	// deciding how somebody spells their own name, which it has no standing to do.
	c.Name = strings.TrimSpace(c.Name)
	c.Email = strings.ToLower(strings.TrimSpace(c.Email))
	c.Phone = normalisePhone(c.Phone)
	c.Role = Role(strings.ToLower(strings.TrimSpace(string(c.Role))))

	// The password is deliberately not trimmed. Leading and trailing spaces are characters a
	// person may have chosen, and silently removing them means the password that was accepted
	// at registration is not the one that will be accepted at sign-in.
}

// Validate reports every problem with the command rather than the first.
//
// A six-field form answered one error at a time takes six round trips to fill in, on a phone,
// in a truck yard (Docs/10 §4.6). The returned error is already in the API's shape: field paths
// are the JSON the client sent, so a client can put each message beside the input that caused
// it.
func (c RegisterCommand) Validate() error {
	var v validate.Errors

	if v.Required("name", c.Name) {
		v.Length("name", c.Name, minNameLength, maxNameLength)
	}

	if v.Required("email", c.Email) {
		v.Length("email", c.Email, 3, maxEmailLength)
		if !plausibleEmail(c.Email) {
			v.Add("email", validate.CodeInvalid, "Enter a valid email address.")
		}
	}

	if v.Required("phone", c.Phone) {
		if !validE164(c.Phone) {
			// The message names the shape a person actually types, not the standard. A
			// customer told their number "is not E.164" has learnt nothing.
			v.Add("phone", validate.CodeInvalid,
				"Enter an Australian mobile number, like 0412 345 678.")
		}
	}

	if v.Required("password", c.Password) {
		v.Length("password", c.Password, minPasswordLength, maxPasswordLength)
	}

	if v.Required("role", string(c.Role)) && !c.Role.Valid() {
		v.Add("role", validate.CodeNotAllowed, "Choose either customer or provider.")
	}

	return v.Err()
}

// validatePhoneField checks a normalised number the way RegisterCommand does, for the endpoints
// whose whole request body is a number.
//
// One function rather than a repeated pair of checks, so that "what counts as a phone number"
// has one answer across registration, OTP request and phone confirmation. Three copies is how
// an endpoint ends up accepting something the account can never match.
func validatePhoneField(phone string) error {
	var v validate.Errors
	if v.Required("phone", phone) && !validE164(phone) {
		v.Add("phone", validate.CodeInvalid, "Enter an Australian mobile number, like 0412 345 678.")
	}
	return v.Err()
}

// Register creates an unverified account (SHIP-30) with the role it will keep (SHIP-45).
//
// # What "unverified" means here
//
// Both verification timestamps are left null. Docs/04 §2 requires email and phone verified
// before a customer may publish, and Docs/01 §4.1 allows drafts before then — so an account
// that exists and is unverified is a state the product has a use for, not an intermediate one
// to be tidied away.
//
// # Why duplicates are detected by the database
//
// A SELECT then an INSERT is a race, and one that is lost in practice rather than in theory:
// two taps on a slow connection produce two registrations for one address within milliseconds
// of each other. uq_users_email and uq_users_phone refuse the second one whatever the
// application believed, and this turns that refusal into the error the client is given. Same
// reasoning as SHIP-91's partial unique index (Docs/06 §4.1).
//
// # Why the account and its verification token are one transaction
//
// SHIP-31 requires the token to be issued *on registration*. Two statements without a
// transaction would let a failure between them produce an account nobody can verify and no
// record of why — recoverable only through the resend endpoint, by somebody who has not been
// told they need it. The email goes out after the commit, because a message cannot be un-sent
// if the transaction rolls back.
func (s *Service) Register(ctx context.Context, cmd RegisterCommand) (User, error) {
	cmd.Normalise()
	if err := cmd.Validate(); err != nil {
		return User{}, err
	}

	hash, err := s.hasher.Hash(cmd.Password)
	if err != nil {
		return User{}, fmt.Errorf("identity: hashing the password: %w", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return User{}, fmt.Errorf("identity: generating a user id: %w", err)
	}

	user := User{
		ID:     id,
		Name:   cmd.Name,
		Email:  cmd.Email,
		Phone:  cmd.Phone,
		Role:   cmd.Role,
		Status: StatusActive,
	}

	if s.pool == nil {
		return User{}, errUnavailable
	}

	var (
		created User
		raw     string
	)
	if err := db.InTx(ctx, s.pool, func(ctx context.Context, r db.Runner) error {
		created, err = s.store.insertUser(ctx, r, user, hash)
		if err != nil {
			return err
		}
		raw, err = s.issueEmailVerification(ctx, r, created)
		return err
	}); err != nil {
		return User{}, err
	}

	s.sendVerificationEmail(ctx, created, raw)
	return created, nil
}

// errUnavailable is what every path reports when the pool is nil.
//
// Deps documents both the pool and the Redis client as nil-able, because the service starts
// with an unreachable database deliberately. Dereferencing would panic into httpx.Recover and
// answer 500, which is the correct status but the wrong code: 503 tells a mobile client to
// retry, and 500 tells it to give up.
var errUnavailable = errors.New("identity: the database is not available")

// plausibleEmail reports whether the address is one a mail system would accept.
//
// net/mail rather than a regular expression, and a deliberately shallow check either way. The
// only authority on whether an address exists is the address itself, which is what the
// verification email is for (SHIP-31, SHIP-33); a stricter pattern here buys nothing and
// reliably rejects somebody's genuine address.
//
// What it does catch is the mistake worth catching at the form: no @, no domain, a space in the
// middle, a display name pasted in with it.
func plausibleEmail(address string) bool {
	parsed, err := mail.ParseAddress(address)
	if err != nil {
		return false
	}
	// ParseAddress accepts `Alice <alice@example.com>`, which is a header value rather than
	// an address. Requiring the parse to be idempotent rejects it.
	if parsed.Address != address || parsed.Name != "" {
		return false
	}

	at := strings.LastIndex(address, "@")
	if at <= 0 || at == len(address)-1 {
		return false
	}
	domain := address[at+1:]
	return strings.Contains(domain, ".") &&
		!strings.HasPrefix(domain, ".") && !strings.HasSuffix(domain, ".")
}

// normalisePhone puts a submitted number into E.164, assuming Australia where the number does
// not say otherwise.
//
// 000002_users stores E.164 "normalised before it arrives", and this is where that happens.
// Doing it at the boundary rather than in SQL is what makes uq_users_phone meaningful: the
// index compares strings, so 0412 345 678 and +61412345678 are two accounts for one handset
// unless they are already one string by the time they reach it.
//
// The Australian default is the product's, not the standard's. Docs/01 §8 puts the pilot in one
// Australian metropolitan area, and a person there types their number with a leading zero. A
// number that already carries a country code is left alone, so an international provider is not
// locked out by the default.
func normalisePhone(submitted string) string {
	// Only the punctuation people actually write a phone number with is removed. Anything else
	// is *kept*, so that validE164 sees it and refuses the number — dropping unrecognised
	// characters instead would silently turn `0412 34a 678` into a valid-looking number
	// belonging to somebody else.
	const separators = " -(). –—"

	var b strings.Builder
	for _, r := range strings.TrimSpace(submitted) {
		if !strings.ContainsRune(separators, r) {
			b.WriteRune(r)
		}
	}
	digits := b.String()

	switch {
	case strings.HasPrefix(digits, "+"):
		return digits
	case strings.HasPrefix(digits, "0"):
		// 0412 345 678 -> +61412345678. The trunk prefix is a domestic dialling
		// convention and is not part of the international number.
		return "+61" + digits[1:]
	case strings.HasPrefix(digits, "61") && len(digits) > 2:
		// 61412345678, which is what a client that stripped the + sends.
		return "+" + digits
	case digits == "":
		return ""
	default:
		// Nothing to infer. It is returned unchanged so validE164 refuses it and the
		// person is told what shape is wanted, rather than being handed a guess.
		return digits
	}
}

// validE164 reports whether the normalised number is a well-formed international number.
//
// E.164 allows at most fifteen digits after the country code indicator, and at least a country
// code plus a subscriber number. This checks the shape and nothing about allocation: whether
// +61499999999 is a number anybody answers is what the OTP establishes (SHIP-34, SHIP-36).
func validE164(phone string) bool {
	if !strings.HasPrefix(phone, "+") {
		return false
	}
	digits := phone[1:]
	if len(digits) < 8 || len(digits) > 15 {
		return false
	}
	if digits[0] == '0' {
		// A country code never starts with zero, so this is a trunk prefix that was not
		// recognised — usually a number from outside Australia typed in its domestic form.
		return false
	}
	for _, r := range digits {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// asUniqueViolation turns a database refusal into the domain's own error.
//
// Named constraints rather than a bare "unique violation", because the two say different things
// to the person registering and answering "email taken" to a duplicate phone number would send
// them looking in the wrong place.
func asUniqueViolation(err error) error {
	switch {
	case db.IsUniqueViolation(err, "uq_users_email"):
		return ErrEmailTaken
	case db.IsUniqueViolation(err, "uq_users_phone"):
		return ErrPhoneTaken
	default:
		return err
	}
}
