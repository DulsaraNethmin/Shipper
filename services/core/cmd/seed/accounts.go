package main

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/passwords"
)

// The accounts, which are the one part of the seed that PostgreSQL writes directly (SHIP-186).
//
// # Two things have no endpoint, and this file is both of them
//
// Everything downstream of an account goes through the public API — see client.go for why. These
// two cannot:
//
//  1. **Confirming an address and a number.** `POST /v1/auth/register` issues a token to an inbox
//     and a code to a handset, and `Docs/04` §2 requires both confirmed before a customer may
//     publish or a provider may bid. The demonstration's inbox belongs to nobody and its handsets
//     do not exist, so there is no channel to read the code back from. The acceptance harness has
//     the same problem and solves it by scraping the console transport out of the API's own stdout,
//     which works when the harness starts the process and does not when the API is a container on
//     another machine.
//  2. **The first administrator.** `POST /v1/admin/administrators` needs an administrator session
//     to call it, so the first one in any deployment cannot come from it. `000801`'s header says so
//     and calls an INSERT the bootstrap path; `scripts/verify/90-admin.sh` takes it.
//
// # What that costs, stated plainly
//
// A seeded account skips registration, so nothing here demonstrates registration. That is the right
// trade for a *dataset* — the buyer registers their own account if they want to watch it happen,
// and `Docs/01` §8's gate has them do exactly that — but it means the seed is not a test of the
// identity domain and must not be read as one.
//
// It does **not** skip anything else. The rows written here are ordinary accounts: the same
// columns, the same constraints, the same argon2id profile the service hashes with, and the
// `provider_verifications` trigger fires on each provider insert exactly as it does at
// registration, so every seeded provider starts Pending and is moved to Verified by an
// administrator through the endpoint.

// seedNamespace derives an account's identifier from its address.
//
// # A derived identifier rather than gen_random_uuid(), and it is what makes a re-run cheap
//
// Idempotency in this file is `ON CONFLICT (email)`, which does not need the identifier to be
// stable — but everything that reads the seeded data afterwards does. An operator holding the
// customer's identifier from last week can still find them; a support conversation about
// `naomi.fletcher@example.com` names one row for the life of the instance; and a database dropped
// and re-seeded comes back with the same identifiers, so a bookmark into the admin panel survives
// it. None of that is true of a random identifier, and all of it is free.
//
// The namespace is a fixed UUID with no meaning beyond making these five names collide with
// nothing else.
var seedNamespace = uuid.MustParse("6f1b4a52-3c8e-4f2a-9a55-9b1d0d5c7e11")

// accountID is the identifier a seeded account always has.
func accountID(email string) uuid.UUID {
	return uuid.NewSHA1(seedNamespace, []byte(email))
}

// minSeedPasswordLength is the floor the seed holds its own credentials to.
//
// **Stricter than the platform's own rule, on purpose.** `internal/identity` requires ten
// characters of a password somebody chooses for themselves, following NIST SP 800-63B. These are
// different: they are typed into a deployment's environment by an operator, they open an account on
// a hostname anybody can reach, and one of them is an administrator with the `owner` role. Twelve
// is not a meaningful defence on its own — the defence is that these come from the environment and
// are not in the repository — but it stops the five-second placeholder that was going to be
// replaced later.
const minSeedPasswordLength = 12

// credentials are the passwords the run was given.
//
// **Both are required and neither has a default**, which is this ticket's one decision about
// security rather than about data. A committed default would be a working credential for every
// demonstration instance anybody ever deploys from this repository, readable by everybody who can
// read the repository — and the instance is on a public hostname by construction, because that is
// what M8 is for. Refusing to start is a worse first experience than a default and a much better
// one than the alternative.
type credentials struct {
	// User opens every marketplace account: the customer and all three providers. One password
	// rather than five, because a buyer being handed five is a buyer who signs in as one.
	User string

	// Administrator opens the panel. Separate from User because the blast radius is not
	// comparable: a marketplace account can publish a job, and this one can read every account
	// on the instance and decide who is verified.
	Administrator string
}

// validate refuses a run that would create an account nobody meant to create.
func (c credentials) validate() error {
	for _, held := range []struct {
		name  string
		value string
	}{
		{seedUserPasswordVar, c.User},
		{seedAdminPasswordVar, c.Administrator},
	} {
		if held.value == "" {
			return fmt.Errorf("%s is not set, and the seed has no default for it: "+
				"a committed password would open every demonstration instance deployed "+
				"from this repository", held.name)
		}
		if len(held.value) < minSeedPasswordLength {
			return fmt.Errorf("%s is %d characters; the seed requires at least %d",
				held.name, len(held.value), minSeedPasswordLength)
		}
	}
	if c.User == c.Administrator {
		return fmt.Errorf("%s and %s are the same password; the administrator opens every "+
			"account on the instance and a marketplace account should not be a way in to it",
			seedUserPasswordVar, seedAdminPasswordVar)
	}
	return nil
}

// writeAccounts creates or refreshes every seeded account, and returns nothing because every
// identifier it wrote is derivable from [accountID].
//
// # Re-running converges rather than duplicates
//
// Each statement is an upsert on the address, so a second run rewrites the password hash, the name
// and the verification timestamps and writes no second row. That is the useful reading of
// idempotent for this half: an operator who rotates `SEED_USER_PASSWORD` and runs the seed again
// gets accounts that take the new password, with every job, bid and delivery they own untouched.
//
// **`role` is deliberately not in either update.** `000005` makes a user's role immutable and
// enforces it with a trigger, so an update naming it would fail rather than be ignored — and it
// should: a provider whose role became `customer` would silently rewrite the meaning of every bid
// they have placed.
func writeAccounts(ctx context.Context, pool *pgxpool.Pool, hasher *passwords.Hasher, creds credentials) error {
	userHash, err := hasher.Hash(creds.User)
	if err != nil {
		return fmt.Errorf("hashing the marketplace password: %w", err)
	}
	adminHash, err := hasher.Hash(creds.Administrator)
	if err != nil {
		return fmt.Errorf("hashing the administrator password: %w", err)
	}

	accounts := []demoAccount{demoCustomer}
	for _, provider := range demoProviders {
		accounts = append(accounts, provider.Account)
	}

	for _, account := range accounts {
		if err := writeUser(ctx, pool, account, userHash); err != nil {
			return err
		}
	}
	return writeAdministrator(ctx, pool, adminHash)
}

// writeUser upserts one marketplace account.
//
// `email_verified_at` and `phone_verified_at` are set to `now()` rather than to a moment the seed
// chose, so the timestamps read as "when this instance was seeded" — which is what they mean. The
// columns record when, not whether, precisely so a support conversation can ask that question.
func writeUser(ctx context.Context, pool *pgxpool.Pool, account demoAccount, hash string) error {
	const statement = `
		INSERT INTO users (id, email, phone, password_hash, role, name,
		                   status, email_verified_at, phone_verified_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'active', now(), now())
		ON CONFLICT (email) DO UPDATE
		   SET phone             = excluded.phone,
		       password_hash     = excluded.password_hash,
		       name              = excluded.name,
		       status            = 'active',
		       email_verified_at = coalesce(users.email_verified_at, now()),
		       phone_verified_at = coalesce(users.phone_verified_at, now())`

	_, err := pool.Exec(ctx, statement,
		accountID(account.Email), account.Email, account.Phone, hash, account.Role, account.Name)
	if err != nil {
		return fmt.Errorf("writing the account for %s: %w", account.Email, err)
	}
	return nil
}

// writeAdministrator upserts the bootstrap administrator.
//
// The role is `owner`, which is the widest bundle. It is what the demonstration needs — a narrower
// account could not decide a verification, and deciding one is half of `Docs/01` §8's gate — and it
// is the account from which a *narrower* one is created through `POST /v1/admin/administrators`,
// which is the part of the panel that demonstrates least privilege actually working.
//
// `status` is forced back to `active` on a re-run for the same reason the password is rewritten: an
// operator re-seeding after disabling this account during a demonstration means to get it back.
func writeAdministrator(ctx context.Context, pool *pgxpool.Pool, hash string) error {
	const statement = `
		INSERT INTO admin_users (id, email, name, password_hash, role, status)
		VALUES ($1, $2, $3, $4, 'owner', 'active')
		ON CONFLICT (email) DO UPDATE
		   SET name          = excluded.name,
		       password_hash = excluded.password_hash,
		       role          = 'owner',
		       status        = 'active'`

	_, err := pool.Exec(ctx, statement,
		accountID(demoAdministratorEmail), demoAdministratorEmail, demoAdministratorName, hash)
	if err != nil {
		return fmt.Errorf("writing the bootstrap administrator: %w", err)
	}
	return nil
}
