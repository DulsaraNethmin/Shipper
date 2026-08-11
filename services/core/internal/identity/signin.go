// SHIP-41: signing in — the endpoint that turns a password into a device session.
//
// This is the first place the three pieces built separately meet: SHIP-29's argon2id verification,
// SHIP-37's access token, and SHIP-39's rotating refresh token. It creates the session; everything
// after it — refresh (SHIP-42), sign-out (SHIP-43), the device list (SHIP-46) — only ever ends one
// or exchanges its token.
//
// # One answer for every failure that is not the account's own standing
//
// No account with that address, and the right address with the wrong password, are one refusal.
// Two would make an unauthenticated endpoint an account-existence oracle. The reasoning, and why
// it is a 400 rather than a 401, is on [CodeCredentialsInvalid] — and the *timing* half of the
// same disclosure is closed by [PasswordHasher.SpendEquivalentWork], because a status code that
// refuses to distinguish two cases is worth nothing if the response time distinguishes them.
//
// A suspended account is the one exception, and it is safe precisely because it is said after the
// password verified: the caller has just proved they own the account they are being told about.
//
// The blank line below keeps this a file note rather than a second package comment.

package identity

import (
	"context"
	"fmt"
	"strings"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// SignInCommand is one sign-in request, in the domain's own terms (SHIP-41).
//
// A domain type rather than the HTTP request struct, for the reason [RegisterCommand] gives: the
// rules live with the domain, and SHIP-55's screen validates the same fields for a better error
// experience while the platform still decides (Docs/07 §3).
type SignInCommand struct {
	Email    string
	Password string

	// DeviceLabel is what the person will see in their device list (SHIP-46). It is display
	// text they chose — "Nethmin's iPhone" — and authenticates nothing, so two handsets may
	// legitimately carry the same one.
	DeviceLabel string
}

// Normalise puts the command into the form the stored row is in.
//
// Separate from Validate and run before it, for the reason [RegisterCommand.Normalise] gives: a
// value is judged in the shape it will be compared in. The email column is citext, so case is
// already handled by the database, but the trim is not — " alice@example.com " would match
// nothing at all.
//
// The password is deliberately untrimmed. Leading and trailing spaces are characters somebody may
// have chosen, and removing them here would refuse the password registration accepted.
func (c *SignInCommand) Normalise() {
	c.Email = strings.ToLower(strings.TrimSpace(c.Email))
	c.DeviceLabel = normaliseDeviceLabel(c.DeviceLabel)
}

// Validate reports every problem with the command rather than the first (Docs/10 §4.6).
//
// # What is deliberately not checked
//
// The password's *length*. minPasswordLength is a rule about what may be chosen, and applying it
// at sign-in would lock out every account created before the floor was last raised — an account
// whose password no longer satisfies the current rule still has that password, and telling its
// owner "your password is too short" at the sign-in screen is both wrong and unactionable. It is
// checked for presence only, so that an empty submission is a field error rather than an argon2id
// derivation against nothing.
func (c SignInCommand) Validate() error {
	var v validate.Errors

	if v.Required("email", c.Email) {
		v.Length("email", c.Email, 3, maxEmailLength)
		if !plausibleEmail(c.Email) {
			// About the shape of the input, not about the account — so it discloses
			// nothing that [CodeCredentialsInvalid] withholds.
			v.Add("email", validate.CodeInvalid, "Enter a valid email address.")
		}
	}

	v.Required("password", c.Password)
	validateDeviceLabelInto(&v, c.DeviceLabel)

	return v.Err()
}

// SignIn verifies a password and starts a device session (SHIP-41).
//
// # Why the whole of it is one transaction
//
// Three writes can happen here: the session row, and — on an account whose hash predates a raised
// argon2id profile — a replacement credential. A sign-in that created a session against a
// password upgrade that then failed, or upgraded a credential for a session that was never
// created, are both states nothing would ever reconcile. Docs/10 §3.2 puts the transaction with
// the domain that owns the invariant, and this is it.
//
// # Why a failure needs no special handling
//
// [Service.Refresh] separates its refusal from its error because reuse detection writes a
// revocation that has to survive being refused. Nothing here does: a sign-in that fails writes
// nothing, so returning the refusal from inside the closure and letting db.InTx roll back is
// exactly right. The failure counting SHIP-47 adds is in Redis and outside this transaction for
// the same reason — a rolled-back attempt still happened.
func (s *Service) SignIn(ctx context.Context, cmd SignInCommand) (TokenPair, error) {
	cmd.Normalise()
	if err := cmd.Validate(); err != nil {
		return TokenPair{}, err
	}
	if s.pool == nil {
		return TokenPair{}, errUnavailable
	}

	var pair TokenPair
	if err := db.InTx(ctx, s.pool, func(ctx context.Context, r db.Runner) error {
		var err error
		pair, err = s.signIn(ctx, r, cmd)
		return err
	}); err != nil {
		return TokenPair{}, err
	}
	return pair, nil
}

// signIn is the body of a sign-in, inside the caller's transaction.
func (s *Service) signIn(ctx context.Context, r db.Runner, cmd SignInCommand) (TokenPair, error) {
	user, hash, err := s.store.credentialByEmail(ctx, r, cmd.Email)
	if err != nil {
		if isNoRows(err) {
			// No account. The work a real verification would have cost is spent anyway,
			// so that the response time does not answer the question the status code
			// refuses to.
			s.hasher.SpendEquivalentWork(cmd.Password)
			return TokenPair{}, ErrCredentialsInvalid
		}
		return TokenPair{}, fmt.Errorf("identity: reading the account for a sign-in: %w", err)
	}

	matched, err := s.hasher.Verify(hash, cmd.Password)
	if err != nil {
		// An unreadable stored hash is a data defect rather than a wrong password, and
		// answering "those credentials do not match" would hide it behind a sign-in failure
		// the account holder cannot explain or escape. It becomes a 500, which is the honest
		// status for a row that should not exist.
		return TokenPair{}, fmt.Errorf("identity: verifying a password: %w", err)
	}
	if !matched {
		return TokenPair{}, ErrCredentialsInvalid
	}

	// Only now, with the password proved, is anything said about the account itself.
	if !user.CanSignIn() {
		return TokenPair{}, ErrAccountSuspended
	}

	if err := s.upgradeStoredPassword(ctx, r, user, cmd.Password, hash); err != nil {
		return TokenPair{}, err
	}

	return s.startSession(ctx, r, user, cmd.DeviceLabel)
}

// upgradeStoredPassword rewrites a credential that was hashed at a weaker profile (SHIP-29).
//
// This is the whole of "the costs travel with the hash, so raising the profile needs no
// migration" (Docs/10 §5) — a property of the *format* until something takes the opportunity, and
// sign-in is the only moment the plaintext exists to take it with. No migration, no reset, and
// nobody is asked to do anything.
//
// A failure here fails the sign-in, and that is not a trade being made carelessly: the UPDATE
// runs inside the caller's transaction, so a failed statement has already aborted it and the
// session INSERT that follows could not succeed either. There is no version of "carry on
// regardless" available from inside a transaction, and moving the upgrade outside one would let a
// credential be replaced for a session that was never created.
func (s *Service) upgradeStoredPassword(ctx context.Context, r db.Runner, user User, plaintext, stored string) error {
	stale, err := s.hasher.NeedsRehash(stored)
	if err != nil {
		// Unreachable: Verify parsed this same string a moment ago and would have refused an
		// unreadable one. Reported rather than ignored, because the two disagreeing means one
		// of them has been changed.
		return fmt.Errorf("identity: reading the stored profile: %w", err)
	}
	if !stale {
		return nil
	}

	next, err := s.hasher.Hash(plaintext)
	if err != nil {
		return fmt.Errorf("identity: rehashing at the current profile: %w", err)
	}
	return s.store.updatePasswordHash(ctx, r, user.ID, next)
}
