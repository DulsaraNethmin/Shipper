// Command seed populates a demonstration instance with a marketplace worth looking at (SHIP-186).
//
// # What it makes
//
// `Docs/09`'s *Done when*, clause by clause: three verified providers with fleets and service
// areas; open jobs, one of them with nothing bid on it yet; bids in flight from providers competing
// on price and date; a delivery in progress with a driver part-way through it; and one completed
// delivery with a photograph behind it. Plus the accounts to sign in as, because a buyer handed a
// link and no credentials sees a marketplace from the outside only.
//
// # Why it is a command
//
// The same shape `cmd/migrate` and `cmd/topics` have, and for the same reason: a deployment already
// runs steps like this, and a schema, a topic set and a dataset are all "apply a declaration held in
// the code, idempotently, from a step somebody runs". A hosted demonstration (SHIP-188) applies
// migrations and creates topics before it serves anything; this is the third line.
//
// # How it writes
//
// **Through the public API, with two exceptions it cannot make.** client.go carries the argument at
// length; the short version is that the eight adapter types joining `bidding` and `delivery` to the
// rest live in `cmd/api`, so a second composition root would be a second copy of all eight, and
// writing rows with SQL instead is refused by the schema — SHIP-57's trigger will not accept a job
// whose status moved without the history row describing the move.
//
// accounts.go is the exception, and it is exactly two things: confirming an address and a number
// that were sent to a mailbox and a handset nobody owns, and the first administrator, which no
// authenticated administrator endpoint can create.
//
// # Running it twice is the intended usage
//
// Every step reads before it writes, on a key the product already carries — a job by its goods
// description, a vehicle by its registration, a bid by who offered it. So a second run writes
// nothing new, and a run that died halfway is resumed by running it again. marketplace.go says why
// that is per-step rather than one flag at the top.
//
// **Measured rather than asserted, and one table legitimately grows.** On a seeded instance a second
// run leaves `users`, `jobs`, `bids`, `milestones`, `proofs`, `vehicles`,
// `provider_verification_documents`, `job_status_history`, `driver_assignments` and `outbox` at
// identical counts. `audit_log` gains exactly one row: `administrator.signed_in`, because the
// administrator did sign in and audit entries are append-only (CLAUDE.md). The three
// `verification.decided` entries do not repeat, which is the half that would have been a defect.
// Idempotent here means "creates no second copy of anything", not "leaves no trace of having run".
//
// # It needs its passwords from the environment and has no defaults
//
//	SEED_USER_PASSWORD    opens the customer and all three providers
//	SEED_ADMIN_PASSWORD   opens the administrator panel
//
// A committed default would be a working credential for every demonstration instance ever deployed
// from this repository, and the instance is on a public hostname by construction. See
// [credentials].
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/passwords"
)

const (
	seedUserPasswordVar  = "SEED_USER_PASSWORD"
	seedAdminPasswordVar = "SEED_ADMIN_PASSWORD"
	seedAPIBaseURLVar    = "SEED_API_BASE_URL"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "shipper-seed: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	fs.SetOutput(out)

	// The API's address, overriding SEED_API_BASE_URL. Both, for the reason cmd/topics takes
	// both: a deployment sets the variable once and runs the step with no arguments, and an
	// operator seeding one instance by hand types the address and needs nothing in their
	// environment.
	base := fs.String("api", "",
		"base URL of the API to seed through, overriding "+seedAPIBaseURLVar)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	creds := credentials{
		User:          os.Getenv(seedUserPasswordVar),
		Administrator: os.Getenv(seedAdminPasswordVar),
	}
	if err := creds.validate(); err != nil {
		return err
	}

	target := chosenBase(*base, cfg)
	fmt.Fprintf(out, "seeding %s\n", target)

	// The pool is opened before anything is written and closed after, and it is used for the
	// accounts alone. A failure here is fatal rather than warned about, unlike cmd/api's: that
	// process has to survive a database that is briefly away, and this one has nothing to do
	// without it.
	openCtx, cancelOpen := context.WithTimeout(ctx, 10*time.Second)
	pool, err := db.Open(openCtx, cfg.Database.URL, db.PoolOptions{MaxConns: 4, MinConns: 1})
	cancelOpen()
	if err != nil {
		return fmt.Errorf("connecting to the database: %w", err)
	}
	defer pool.Close()

	hasher, err := passwords.NewHasher(passwords.Argon2Profile{
		MemoryKiB:   cfg.Passwords.Argon2.MemoryKiB,
		Iterations:  cfg.Passwords.Argon2.Iterations,
		Parallelism: cfg.Passwords.Argon2.Parallelism,
	})
	if err != nil {
		return fmt.Errorf("building the password hasher: %w", err)
	}

	fmt.Fprintln(out, "accounts")
	if err := writeAccounts(ctx, pool, hasher, creds); err != nil {
		return err
	}
	fmt.Fprintf(out, "  %d marketplace accounts and 1 administrator\n", 1+len(demoProviders))

	seed := &seeder{anonymous: newClient(target), out: out, now: time.Now()}
	if err := seed.signIn(ctx, creds); err != nil {
		return err
	}

	fmt.Fprintln(out, "providers")
	if err := seed.prepareProviders(ctx); err != nil {
		return err
	}

	fmt.Fprintln(out, "jobs")
	if err := seed.seedJobs(ctx); err != nil {
		return err
	}

	summarise(out, target)
	return nil
}

// chosenBase reconciles the flag, the environment and the configured listener.
//
// The listener is the last resort rather than the first, because it is right only when the seed
// runs on the same host as the API — true locally and false for every deployment. It is kept
// because locally it is *always* right, and an operator running `make seed` after `make run` should
// not have to say where the API they just started is listening.
func chosenBase(flagged string, cfg *config.Config) string {
	if flagged != "" {
		return strings.TrimRight(flagged, "/")
	}
	if fromEnv := os.Getenv(seedAPIBaseURLVar); fromEnv != "" {
		return strings.TrimRight(fromEnv, "/")
	}
	return "http://localhost:" + strconv.Itoa(cfg.HTTP.Port)
}

// summarise prints what a buyer needs to know, and prints no password.
//
// **The passwords are deliberately not echoed.** Whoever ran the command set them, so they know
// them; anything else scrolls a working administrator credential into a terminal, a CI log and
// whatever collects that log.
func summarise(out io.Writer, target string) {
	fmt.Fprintf(out, "\nseeded %s\n\n", target)
	fmt.Fprintf(out, "  customer       %s (%s)\n", demoCustomer.Email, demoCustomer.Name)
	for _, provider := range demoProviders {
		fmt.Fprintf(out, "  provider       %s (%s)\n",
			provider.Account.Email, provider.DisplayName)
	}
	fmt.Fprintf(out, "  administrator  %s (%s)\n", demoAdministratorEmail, demoAdministratorName)
	fmt.Fprintf(out, "\n  passwords are the ones %s and %s were set to.\n",
		seedUserPasswordVar, seedAdminPasswordVar)
}
