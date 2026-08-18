// SHIP-147: administrator sign-in, sign-out, and the creation of administrator accounts.
//
// adminauth.go spends the credential this issues. The two are one system and are kept in one
// package for the reason Docs/11 §8 keeps the driver's issuer and verifier together: "neither can
// be exchanged for the other" is a statement about both ends, and only somebody holding both can
// prove it.
//
// # Everything SHIP-41 established about disclosure is reproduced here, not re-argued
//
// A sign-in endpoint is an account-existence oracle unless three separate things are true, and an
// administrator sign-in is a *better* target than a user one: the set of addresses that can suspend
// accounts is worth enumerating in a way the set of addresses that can post a job is not.
//
//   - **One answer for both halves.** No account with that address and the right address with the
//     wrong password are one refusal, [CodeAdminCredentialsInvalid].
//   - **One response time for both halves.** [passwords.Hasher.SpendEquivalentWork] derives against
//     a decoy salt when the address is unknown, so the argon2id cost is paid either way. SHIP-15r
//     moved that method into `internal/passwords` and its doc comment names this ticket: "SHIP-147
//     gets it for free — an administrator sign-in has exactly the same enumeration problem." It is
//     a disclosure control, not a performance detail, and a sign-in that skipped it would answer
//     an unknown address in microseconds and a known one in tens of milliseconds.
//   - **A limit that counts unknown addresses too.** Counting only real accounts would make the
//     throttle itself the oracle: a caller who is never refused has learnt the address is unknown.
//
// The one thing said after the password verifies is that the account is disabled, and that is safe
// for SHIP-41's reason: the caller has just proved they hold it.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/passwords"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/ratelimit"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// The lifetimes of an administrator session, and how the sliding one is written.
//
// Constants here rather than configuration, which is the scope decision SHIP-39's refresh TTL made
// and SHIP-47's sign-in limits repeated: `internal/config` is a shared surface (Docs/10 §9.2) and
// four tracks are open. Neither is a lever anybody has asked to pull without a deploy, and 000801's
// header is where the reasoning for the values lives so that it is beside the columns that hold
// them.
const (
	// idleWindow is how long an administrator session survives with nothing happening.
	//
	// Thirty minutes, against thirty *days* for a phone (SHIP-39). The credential is different in
	// kind: it authorises suspending accounts and unpublishing jobs, it is held by a browser on a
	// desk that other people walk past, and the cost of it lapsing is one password entry rather
	// than a driver being signed out mid-delivery.
	idleWindow = 30 * time.Minute

	// absoluteLifetime is how long a session survives however busy it is.
	//
	// **SHIP-39 deliberately refused an absolute cap and this deliberately adds one.** That
	// paragraph's reason was a product consequence — "every user signed out on a schedule,
	// including a driver mid-delivery" — and it does not transfer to a console: twelve hours is
	// longer than a shift, so in practice this bounds the session nobody closed rather than the
	// one somebody is using.
	//
	// It is a column and a CHECK rather than arithmetic (000801), so a slide cannot outrun it even
	// if the clamp below is deleted.
	absoluteLifetime = 12 * time.Hour

	// slideGranularity is the smallest advance of the idle window worth an UPDATE.
	//
	// See [Authenticator.slide]: a console makes several requests a second, and writing the row on
	// each of them would be amplification for no gain against a thirty-minute window.
	slideGranularity = time.Minute

	// sessionTokenBytes is the entropy in an administrator session token: 32 bytes from
	// crypto/rand, base64url encoded to 43 characters.
	//
	// The same construction as `identity`'s refresh token, and the same reasoning: this is
	// something the platform generated rather than something a person chose, so its strength is
	// the entropy and nothing else.
	sessionTokenBytes = 32
)

// The two limits on administrator sign-in (SHIP-47's mechanism, this domain's figures).
//
// # Both figures are identity's, and the second one changed at SHIP-183a
//
// Five failures per address in a burst is far past mistyping a password and far short of useful
// guessing, and that reasoning is independent of who is signing in.
//
// **The per-network-address figure was twenty and is now thirty**, which is identity's. The
// argument for the tighter number ran: an administration console has a handful of accounts, so
// twenty is generous against somebody working through a list of them. Docs/12 §3 reviewed both
// buckets across the whole surface and found that reasoning pointed the wrong way. The
// per-*account* bucket is the anti-guessing control and is identical at five in both systems; the
// address bucket exists to bound the list-walk, and **there are very few administrators to walk
// through** — so the tighter figure buys almost nothing against the case it was chosen for. What
// the administration console does have is an office sharing one address, which argues for the
// looser figure rather than the tighter one, and a locked-out administrator is who the platform
// needs during an incident.
//
// **`make verify` runs every request from 127.0.0.1**, so this bucket is shared across sections and
// across concurrent worktrees. `scripts/verify/90-admin.sh` clears `rl:v1:admin-signin:*` at both
// ends for the reason `40-identity.sh` does, and says so in its header.
const (
	signInAccountCapacity = 5
	signInAccountInterval = 2 * time.Minute

	signInAddressCapacity = 30
	signInAddressInterval = 20 * time.Second
)

// The bounds on what may be submitted.
//
// Length only. What a strong administrator password looks like is a policy for the account
// creation path (see [Credentials.Create]) rather than something sign-in judges: applying a
// strength rule at sign-in would lock out every account created before the floor was last raised,
// which is SHIP-41's argument and holds here unchanged.
const (
	maxAdminEmailLength    = 320
	maxAdminNameLength     = 200
	minAdminPasswordLength = 12
	maxAdminPasswordLength = 512
)

// Credentials is administrator sign-in, sign-out, and account creation.
//
// Deliberately a different type from [Authenticator], which only resolves a presented session. The
// split is not tidiness: resolving needs a pool and a clock, and issuing needs a password hasher and
// a rate limiter as well. Keeping them apart is what lets cmd/api's `newAdminGuard` — which is
// handed no Redis client by SHIP-15r's seam — build a complete verifier rather than a half-built
// one with a nil limiter nobody notices until the day something calls SignIn on it.
type Credentials struct {
	pool    *pgxpool.Pool
	hasher  *passwords.Hasher
	limiter *ratelimit.Limiter
	clock   clock.Clock
	audit   *Auditor
	store   postgresStore
}

// NewCredentials builds the sign-in service.
//
// The pool may be nil — the process starts with an unreachable database on purpose — and everything
// else is required. None may be defaulted to something harmless: a nil hasher is a sign-in that
// verifies nothing, a nil limiter is a password endpoint with nothing counting the guesses, a nil
// clock is a session with no lifetime, and a nil auditor is three privileged actions with no record
// that they happened (SHIP-150).
//
// **The auditor is a constructor argument rather than something built inside**, which is the same
// arrangement the clock has and for the same reason: a service that made its own would make one with
// whatever clock was to hand, and the entries would then disagree with the rows they describe.
func NewCredentials(
	pool *pgxpool.Pool,
	hasher *passwords.Hasher,
	limiter *ratelimit.Limiter,
	clk clock.Clock,
	auditor *Auditor,
) (*Credentials, error) {
	if hasher == nil {
		return nil, errors.New("admin: administrator sign-in needs a password hasher")
	}
	if limiter == nil {
		// A limiter whose Redis client is nil is legitimate — it refuses everything, which is
		// the fail-closed direction internal/ratelimit argues for. No limiter at all is a
		// password endpoint served with nothing counting the attempts.
		return nil, errors.New("admin: administrator sign-in needs a rate limiter")
	}
	if clk == nil {
		return nil, errors.New("admin: administrator sign-in needs a clock (Docs/10 §6.3)")
	}
	if auditor == nil {
		// SHIP-150. Refused rather than treated as "auditing is off": every state change this
		// type makes is a privileged action, and a deployment that quietly recorded none of them
		// would look identical to one that recorded all of them until somebody went looking.
		return nil, errors.New("admin: administrator sign-in needs an audit writer (SHIP-150)")
	}
	return &Credentials{
		pool: pool, hasher: hasher, limiter: limiter, clock: clk, audit: auditor,
	}, nil
}

// SignInCommand is one administrator sign-in request, in the domain's own terms.
//
// A domain type rather than the HTTP request struct, per Docs/10 §2.1: the rules live with the
// domain, and the admin panel validates the same fields for a better error experience while the
// platform still decides (Docs/07 §3 — the client may hide or disable, the platform rules).
type SignInCommand struct {
	Email    string
	Password string

	// ClientIP is where the attempt came from, for the second of the two limits.
	//
	// On the command rather than read at the transport edge because "throttle per account and per
	// address" is a policy this domain owns, and a handler that decided which limits applied
	// would be a handler making a security decision. Empty is refused rather than treated as
	// unlimited: an address the platform cannot determine is not an address with no limit.
	ClientIP string
}

// Normalise puts the command into the form the stored row is in.
//
// The password is deliberately untrimmed: leading and trailing spaces are characters somebody may
// have chosen, and removing them here would refuse the password creation accepted.
func (c *SignInCommand) Normalise() {
	c.Email = strings.ToLower(strings.TrimSpace(c.Email))
}

// Validate reports every problem with the command rather than the first (Docs/10 §4.6).
//
// The password is checked for presence only. See the note on the length constants.
func (c SignInCommand) Validate() error {
	var v validate.Errors

	if v.Required("email", c.Email) {
		v.Length("email", c.Email, 3, maxAdminEmailLength)
	}
	v.Required("password", c.Password)

	return v.Err()
}

// Issued is a session and the one time its token is ever readable.
//
// The token is returned rather than stored on [Session] so that there is no struct in this package
// carrying a usable credential past the moment it is handed over. Everything else about the session
// can be read back from the database; this cannot, by construction.
type Issued struct {
	Session Session
	Token   string
}

// SignIn verifies a password and starts an administrator session.
//
// # Why the whole of it is one transaction
//
// Two writes can happen: the session row, and — on an account whose hash predates a raised argon2id
// profile — a replacement credential. A sign-in that created a session against a password upgrade
// that then failed, or upgraded a credential for a session that was never created, are both states
// nothing would ever reconcile. Docs/10 §3.2 puts the transaction with the domain that owns the
// invariant.
//
// # Why the refusal is charged outside it
//
// A rolled-back attempt still happened. Only a refused credential is charged: a validation error
// never reaches the limiter, a disabled account is not a guess, and an unavailable database is the
// platform's fault rather than the caller's.
func (c *Credentials) SignIn(ctx context.Context, cmd SignInCommand) (Issued, Administrator, error) {
	cmd.Normalise()
	if err := cmd.Validate(); err != nil {
		return Issued{}, Administrator{}, err
	}

	// Before the pool and before argon2id, which is the point of a rate limit: what it protects
	// is the work, and a limit checked after the derivation has been paid for is a limit on
	// nothing (SHIP-47).
	buckets, err := c.signInBuckets(cmd)
	if err != nil {
		return Issued{}, Administrator{}, err
	}
	if err := c.admit(ctx, buckets); err != nil {
		return Issued{}, Administrator{}, err
	}

	if c.pool == nil {
		return Issued{}, Administrator{}, ErrAdminUnavailable
	}

	var (
		issued        Issued
		administrator Administrator
	)
	if err := db.InTx(ctx, c.pool, func(ctx context.Context, r db.Runner) error {
		var err error
		issued, administrator, err = c.signIn(ctx, r, cmd)
		return err
	}); err != nil {
		if errors.Is(err, ErrAdminCredentialsInvalid) {
			c.chargeFailure(ctx, buckets)
		}
		return Issued{}, Administrator{}, err
	}

	c.clearAccount(ctx, buckets)
	return issued, administrator, nil
}

// clearAccount returns the administrator's allowance after a sign-in that worked (SHIP-183a).
//
// Docs/12 §6's decision, applied here for the same reason it is applied in internal/identity and
// with the same two constraints: the admission check stays in **front** of the password check, or
// the 429/200 split becomes an oracle an attacker can guess against indefinitely; and only the
// **account** bucket is cleared, never the address one, which exists to bound somebody working
// through a list of administrators and would be reset by anybody holding one valid credential.
//
// identity.Service.clearSignInAccount carries the full argument and the arithmetic.
//
// A failure is logged rather than returned: the sign-in has already succeeded, and refusing an
// administrator who presented the right password because Redis blinked is a worse answer than an
// allowance that refills on its own.
func (c *Credentials) clearAccount(ctx context.Context, limits signInLimits) {
	if err := c.limiter.Clear(ctx, limits.accountKey); err != nil {
		httpx.LoggerFrom(ctx).LogAttrs(ctx, slog.LevelWarn,
			"a successful administrator sign-in could not clear its account rate limit",
			slog.String("error", err.Error()))
	}
}

// signIn is the body of a sign-in, inside the caller's transaction.
func (c *Credentials) signIn(ctx context.Context, r db.Runner, cmd SignInCommand) (Issued, Administrator, error) {
	administrator, hash, err := c.store.credentialByEmail(ctx, r, cmd.Email)
	switch {
	case isNoRows(err):
		// No such administrator. The work a real verification would have cost is spent anyway,
		// so that the response time does not answer the question the status code refuses to.
		c.hasher.SpendEquivalentWork(cmd.Password)
		return Issued{}, Administrator{}, ErrAdminCredentialsInvalid
	case err != nil:
		return Issued{}, Administrator{}, fmt.Errorf("admin: reading an administrator credential: %w", err)
	}

	matched, err := c.hasher.Verify(hash, cmd.Password)
	if err != nil {
		// An unreadable stored hash is a data defect rather than a wrong password, and
		// answering "those credentials do not match" would hide it behind a sign-in failure
		// nobody can explain. It becomes a 500, which is the honest status for a row that
		// should not exist — and ck_admin_users_password_hash is what stops most of them
		// being written in the first place.
		return Issued{}, Administrator{}, fmt.Errorf("admin: verifying an administrator password: %w", err)
	}
	if !matched {
		return Issued{}, Administrator{}, ErrAdminCredentialsInvalid
	}

	// Only now, with the password proved, is anything said about the account itself.
	if !administrator.CanSignIn() {
		return Issued{}, Administrator{}, ErrAdminAccountDisabled
	}

	if err := c.upgradeStoredPassword(ctx, r, administrator.ID, cmd.Password, hash); err != nil {
		return Issued{}, Administrator{}, err
	}

	issued, err := c.startSession(ctx, r, administrator.ID)
	if err != nil {
		return Issued{}, Administrator{}, err
	}

	// SHIP-150, in the same transaction as the session row. A console session that exists with
	// no record of it starting is the gap a support timeline is least able to work around: every
	// later entry names an administrator, and this is the only thing that says when they arrived.
	//
	// The session identifier goes in the metadata rather than in `target_id`, so that a query for
	// everything about this administrator returns their sign-ins alongside the actions they then
	// took. See AuditActionAdministratorSignedIn.
	if _, err := c.audit.Record(ctx, r, AuditEntry{
		Actor:      AdminActor(administrator.ID),
		Action:     AuditActionAdministratorSignedIn,
		TargetType: AuditTargetAdministrator,
		TargetID:   administrator.ID,
		Metadata: map[string]any{
			"session_id": issued.Session.ID.String(),
			"role":       administrator.Role.String(),
		},
	}); err != nil {
		return Issued{}, Administrator{}, err
	}
	return issued, administrator, nil
}

// upgradeStoredPassword rewrites a credential hashed at a weaker profile.
//
// This is the whole of "the costs travel with the hash, so raising the profile needs no migration"
// (Docs/10 §5), and sign-in is the only moment the plaintext exists to take the opportunity with.
// It runs inside the caller's transaction, so a failure has already aborted it and the session
// INSERT could not have succeeded either — there is no "carry on regardless" available from in
// here, and moving it outside would let a credential be replaced for a session that was never
// created.
func (c *Credentials) upgradeStoredPassword(
	ctx context.Context,
	r db.Runner,
	id uuid.UUID,
	plaintext, stored string,
) error {
	stale, err := c.hasher.NeedsRehash(stored)
	if err != nil {
		// Unreachable: Verify parsed this same string a moment ago and would have refused an
		// unreadable one. Reported rather than ignored, because the two disagreeing means one
		// of them has been changed.
		return fmt.Errorf("admin: reading the stored argon2id profile: %w", err)
	}
	if !stale {
		return nil
	}

	next, err := c.hasher.Hash(plaintext)
	if err != nil {
		return fmt.Errorf("admin: rehashing at the current profile: %w", err)
	}
	return c.store.updatePasswordHash(ctx, r, id, next)
}

// startSession writes the session row and returns the credential, once.
//
// Unexported deliberately. Creating a session is how a caller obtains an administrative credential,
// and the only path that may do it is the one above, which has first verified a password. A handler
// in cmd/api must not be able to mint one — `identity.Service.startSession` is unexported for
// exactly this reason.
func (c *Credentials) startSession(ctx context.Context, r db.Runner, adminID uuid.UUID) (Issued, error) {
	raw, hash, err := newSessionToken()
	if err != nil {
		return Issued{}, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Issued{}, fmt.Errorf("admin: generating a session id: %w", err)
	}

	now := c.clock.Now().UTC()
	session := Session{
		ID:      id,
		AdminID: adminID,

		// The absolute cap is written once, here, and never moved. The idle window starts at
		// the same place every slide will put it, clamped by the cap for the session that is
		// somehow shorter than one idle window — which cannot happen at today's constants and
		// would silently violate ck_admin_sessions_idle_within_absolute if it ever could.
		AbsoluteExpiresAt: now.Add(absoluteLifetime),
		IdleExpiresAt:     now.Add(min(idleWindow, absoluteLifetime)),

		LastUsedAt: now,
		CreatedAt:  now,
	}

	if err := c.store.insertSession(ctx, r, session, hash); err != nil {
		return Issued{}, err
	}
	return Issued{Session: session, Token: raw}, nil
}

// SignOut ends the session the caller is presenting.
//
// It takes the session id rather than the token, because the guard has already resolved one and
// asking the handler to hand the credential back would put it through a second code path for no
// gain.
//
// **Idempotent by state rather than by key.** Signing out a session that is already revoked is a
// success: the caller wanted it ended and it is ended. A second call from a retrying browser must
// not be a 404, which would tell somebody their sign-out failed when it had not.
//
// # The audit entry is written every time, including on the repeat
//
// SHIP-150, and it is the one place the idempotence and the trail pull against each other. The
// revocation is a no-op the second time — `revoked_at IS NULL` in the WHERE keeps the recorded
// instant truthful — while the *entry* is appended again, because a person pressed sign out again
// and an append-only table has no way to say "the same thing, once more". A reader sees two entries
// a second apart and one revocation, which is what happened.
//
// The alternative was to read the row back and skip the entry when nothing changed. It was rejected
// because it makes the trail depend on a race: two tabs signing out together would record one entry
// or two depending on which committed first, and a trail that is sometimes missing an action is
// worse than one that occasionally repeats it.
func (c *Credentials) SignOut(ctx context.Context, adminID, sessionID uuid.UUID) error {
	if c.pool == nil {
		return ErrAdminUnavailable
	}

	// One transaction, so the revocation and the record of it commit together. Two statements
	// auto-committed would leave a session ended with nothing saying who ended it, in exactly the
	// case worth recording: something failed between them.
	return db.InTx(ctx, c.pool, func(ctx context.Context, r db.Runner) error {
		if err := c.store.revokeSession(ctx, r, sessionID, c.clock.Now().UTC()); err != nil {
			return err
		}

		_, err := c.audit.Record(ctx, r, AuditEntry{
			Actor:      AdminActor(adminID),
			Action:     AuditActionAdministratorSignedOut,
			TargetType: AuditTargetAdministrator,
			TargetID:   adminID,
			Metadata:   map[string]any{"session_id": sessionID.String()},
		})
		return err
	})
}

// CreateCommand is a new administrator account.
//
// The role is a field rather than an argument so that omitting it is expressible: the zero value is
// the empty string, and [Credentials.Create] turns that into [RoleSupport] rather than refusing it.
// See its note — this is where "default to the minimum" is a line of code rather than a sentence.
type CreateCommand struct {
	Email string
	Name  string

	// Password is the initial password, chosen by whoever is creating the account.
	//
	// There is no invitation email and no reset flow yet — SHIP-147 is the credential system and
	// not the account-recovery product. What exists is a floor on the length, which is the one
	// rule that cannot be added later without locking people out.
	Password string

	// Role is what the new administrator may do. Empty means [RoleSupport].
	Role Role

	// ActorID is the administrator doing the creating (SHIP-150).
	//
	// **On the command rather than read from a grant inside**, because this domain must be able
	// to record who acted without depending on how the caller was authenticated — and because a
	// required field is the cheapest way to make an unattributed account creation impossible.
	// [CreateCommand.Validate] deliberately does not check it: it is not something a client sends
	// and a validation error naming it would be a field the console cannot fix. It is refused in
	// [Credentials.Create] as the wiring mistake it would be.
	//
	// The first administrator in a deployment has no actor and is not created through here — see
	// the note on Create and `000801`'s header.
	ActorID uuid.UUID
}

// Normalise puts the command into the form the stored row is in.
func (c *CreateCommand) Normalise() {
	c.Email = strings.ToLower(strings.TrimSpace(c.Email))
	c.Name = strings.TrimSpace(c.Name)
}

// Validate reports every problem rather than the first.
//
// **An unrecognised role is a field error and an absent one is not.** Those are different mistakes:
// sending nothing is a client that did not care, and sending "administrator" is a client that
// believes in a role this platform does not have. Silently giving the second one the minimum would
// mean an owner creating what they thought was a moderator and getting something else.
func (c CreateCommand) Validate() error {
	var v validate.Errors

	if v.Required("email", c.Email) {
		v.Length("email", c.Email, 3, maxAdminEmailLength)
	}
	if v.Required("name", c.Name) {
		v.Length("name", c.Name, 1, maxAdminNameLength)
	}
	if v.Required("password", c.Password) {
		v.Length("password", c.Password, minAdminPasswordLength, maxAdminPasswordLength)
	}
	if c.Role != "" && !c.Role.Valid() {
		v.Add("role", validate.CodeInvalid,
			"That is not an administrator role. Use one of %s.", strings.Join(roleNames(), ", "))
	}

	return v.Err()
}

// Create adds an administrator account.
//
// # The default is the minimum, and this function is where SHIP-148's *Done when* is enforced
//
// A command with no role becomes [RoleSupport], which holds only the permissions to look. That is
// asserted in two places on purpose — here, and as `DEFAULT 'support'` on the column (000801) — for
// the reason Docs/06 §4.1 gives about constraints generally: this one reports the decision as a
// value the caller can read back, and the column's one is still true for a row written by something
// that never came through here.
//
// **Nothing in this function can produce an account with more than the caller asked for.** There is
// no "inherit the creator's role", no widening, and no default that is anything but the smallest
// bundle. Whether the *caller* may create administrators at all is [PermissionAdminsManage], checked
// at the handler.
func (c *Credentials) Create(ctx context.Context, cmd CreateCommand) (Administrator, error) {
	cmd.Normalise()
	if err := cmd.Validate(); err != nil {
		return Administrator{}, err
	}
	if cmd.ActorID == uuid.Nil {
		// Not a validation error, because no client sends this — the handler takes it from the
		// grant the guard resolved. Reaching here without one means a caller built the command
		// by hand, and creating the most privileged kind of account with nobody named for it is
		// precisely the entry SHIP-150 exists to make impossible to omit.
		return Administrator{}, errors.New(
			"admin: creating an administrator needs the administrator doing it (SHIP-150)")
	}
	if c.pool == nil {
		return Administrator{}, ErrAdminUnavailable
	}

	// The whole of "default to the minimum". Written as a statement rather than left to the
	// column default so that the value returned to the caller is the value stored, and so that a
	// mutation to it fails a test rather than only a migration.
	role := cmd.Role
	if role == "" {
		role = RoleSupport
	}

	hash, err := c.hasher.Hash(cmd.Password)
	if err != nil {
		return Administrator{}, fmt.Errorf("admin: hashing an administrator password: %w", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Administrator{}, fmt.Errorf("admin: generating an administrator id: %w", err)
	}

	administrator := Administrator{
		ID:     id,
		Email:  cmd.Email,
		Name:   cmd.Name,
		Role:   role,
		Status: StatusActive,
	}

	// One transaction, so the account and the record of who created it commit together
	// (SHIP-150). An administrator account that exists with nothing saying who made it is the
	// worst single hole this trail could have: it is the action that grants every other
	// permission, and Docs/04 §9 asks for least-privilege administrative access, which is
	// unenforceable if nobody can say where an account came from.
	var created Administrator
	if err := db.InTx(ctx, c.pool, func(ctx context.Context, r db.Runner) error {
		var err error
		created, err = c.store.insertAdministrator(ctx, r, administrator, hash)
		if err != nil {
			return err
		}

		// The role goes in the metadata because it is the field that makes the entry worth
		// reading. "An account was created" is half a fact; "an account was created that can
		// create accounts" is the one somebody reviewing this trail is looking for.
		_, err = c.audit.Record(ctx, r, AuditEntry{
			Actor:      AdminActor(cmd.ActorID),
			Action:     AuditActionAdministratorCreated,
			TargetType: AuditTargetAdministrator,
			TargetID:   created.ID,
			Metadata: map[string]any{
				"email": created.Email,
				"role":  created.Role.String(),
			},
		})
		return err
	}); err != nil {
		return Administrator{}, err
	}
	return created, nil
}

// newSessionToken mints a credential and the digest that will be stored in its place.
//
// The raw value is returned to the caller once and never again; only the digest reaches the
// database. 000801 says why: this table is read by every support query, every backup and every
// replica, and a readable administrator credential in any of those is the whole console.
func newSessionToken() (raw, hash string, err error) {
	buf := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("admin: reading session token entropy: %w", err)
	}

	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, hashSessionToken(raw), nil
}

// hashSessionToken is the one-way function `admin_sessions.token_hash` stores.
//
// SHA-256 rather than argon2id, and this is the one place in the domain where that is the right
// answer: the input is 32 bytes from crypto/rand rather than something a person chose, so there is
// nothing to guess, and a per-verification cost of tens of milliseconds would be paid on **every**
// administrative request rather than once per sign-in. `identity`'s refresh token takes the same
// position for the same reason (000100).
//
// It is deliberately its own function rather than a call to identity's — which it could not be
// anyway, the two domains cannot see each other — and deliberately not shared with anything else in
// this package. Two credentials with two lifetimes in two tables are how a shared helper named after
// one of them silently changes the other's storage.
func hashSessionToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// signInLimits is the pair of buckets one attempt is counted against.
type signInLimits struct {
	accountKey string
	addressKey string
}

// signInBuckets derives the keys one attempt counts against.
//
// The address is hashed for SHIP-47's reason: a bucket per address means every address anybody has
// ever tried to sign in as is enumerable from a cache dump, and on this endpoint that list is
// "addresses somebody believes are administrators". Hashing costs nothing — the key is only ever
// compared with itself.
//
// The prefix is `admin-signin:` rather than `signin:` so that an administrator's attempts and a
// user's attempts against the same address are separate allowances. They are separate systems, and
// a shared bucket would let failures against one throttle the other.
func (c *Credentials) signInBuckets(cmd SignInCommand) (signInLimits, error) {
	ip := strings.TrimSpace(cmd.ClientIP)
	if ip == "" {
		// Not a validation error the caller could act on: they did not supply this, the
		// transport did. An address the platform cannot determine is not an address with no
		// limit, so this fails rather than defaulting to unlimited.
		return signInLimits{}, errors.New(
			"admin: the sign-in command carries no client address, so the per-address limit " +
				"has nothing to count against")
	}

	account := sha256.Sum256([]byte(cmd.Email))
	return signInLimits{
		accountKey: "admin-signin:account:" + hex.EncodeToString(account[:]),
		addressKey: "admin-signin:address:" + ip,
	}, nil
}

// buckets pairs each key with its allowance, in the order they are checked.
//
// The account bucket is first so that a caller hammering one address waits on the shorter of the
// two, and so that they are not told how much of the network-wide allowance somebody *else* has
// used.
func (l signInLimits) buckets() []struct {
	key    string
	bucket ratelimit.Bucket
} {
	return []struct {
		key    string
		bucket ratelimit.Bucket
	}{
		{l.accountKey, ratelimit.Bucket{Capacity: signInAccountCapacity, Interval: signInAccountInterval}},
		{l.addressKey, ratelimit.Bucket{Capacity: signInAddressCapacity, Interval: signInAddressInterval}},
	}
}

// admit refuses an attempt whose allowance is gone, on either limit.
func (c *Credentials) admit(ctx context.Context, limits signInLimits) error {
	for _, limit := range limits.buckets() {
		decision, err := c.limiter.Allow(ctx, limit.key, limit.bucket)
		if err != nil {
			// Fail closed, and answer 503 rather than 429: "this is temporarily unavailable"
			// is true and "you have done too much" is not. A limiter an attacker turns off by
			// taking Redis down is not a limiter.
			return fmt.Errorf("%w: %w", ErrAdminUnavailable, err)
		}
		if !decision.Allowed {
			return &ThrottledError{RetryAfter: decision.RetryAfter}
		}
	}
	return nil
}

// chargeFailure records one refused credential against both buckets.
//
// A failure to record is logged rather than returned: the sign-in has already been refused, and
// turning a Redis blip into a 503 would tell the client to retry — which is the one thing that must
// not happen at this point. The admission check above is where an unreachable Redis fails closed.
func (c *Credentials) chargeFailure(ctx context.Context, limits signInLimits) {
	for _, limit := range limits.buckets() {
		if _, err := c.limiter.Spend(ctx, limit.key, limit.bucket); err != nil {
			httpx.LoggerFrom(ctx).LogAttrs(ctx, slog.LevelWarn,
				"a failed administrator sign-in could not be counted against its rate limit",
				slog.String("error", err.Error()))
			return
		}
	}
}

// roleNames is [Roles] as strings, for a validation message.
func roleNames() []string {
	out := make([]string, 0, len(Roles))
	for _, r := range Roles {
		out = append(out, r.String())
	}
	return out
}
