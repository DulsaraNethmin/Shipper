package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/migrations"
)

// The out-of-order guard (SHIP-15g).
//
// # The failure it exists to catch
//
// golang-migrate records one integer: the highest version applied. `up` then applies only the
// files numbered above it. That is correct for a single ascending sequence and wrong for this
// repository, because migration numbers are drawn from reserved per-domain blocks
// (migrations/blocks.go) rather than allocated in time order.
//
// So a development database that has the jobs block applied sits at version 000404, and a new
// *identity* migration at 000105 — a lower number, written later — is silently skipped.
// `make migrate-up` prints "no change", the column never appears, and the next thing to fail is
// a query mentioning a column that the developer can see in a file in front of them.
//
// It is not hypothetical and it is not rare. The identity block is 100–199, profiles 200–299 and
// fleet 300–399, and all three sit below jobs at 400–499. Every future migration in those three
// domains hits this on any database that has jobs applied.
//
// CI and tests never see it: make test-db-template rebuilds from an empty database, and CI runs
// up → down all → up. **It bites interactively only**, which is precisely why it presents as a
// mystery rather than as an error.
//
// # Why this fails rather than reorders
//
// Applying out of order was the alternative. It would mean running 000105 against a schema that
// already has 000400–000404 on top of it — an ordering that migration was never written for and
// that no test covers. A migration is a program, not a patch: one that assumes it runs before the
// jobs tables exist may create a constraint they contradict, and discovering that halfway through
// leaves a dirty schema.
//
// Refusing is smaller, and it matches what this repository does elsewhere: migrate-create refuses
// a missing -domain rather than guessing a block, and the idempotency middleware fails closed
// rather than letting a request through unrecorded. The fix is one command and the message says
// it.
//
// # Why a ledger table rather than a cleverer read of schema_migrations
//
// Because the question cannot be answered from schema_migrations. It holds a single row — the
// version and a dirty flag — so "the database is at 404" is all it says, and whether 000105 ran
// before the file existed is not recoverable from it. Recording each applied version as it is
// applied is the only thing that makes the question answerable at all.
//
// The table is created by this tool rather than by a migration, which would otherwise have to
// migrate the thing that records migrations.

// ledgerTable records which migration versions have actually been applied.
//
// Deliberately not named with a shipper_ prefix: it sits beside golang-migrate's own
// schema_migrations and the pairing is the clearest documentation of what it is for.
const ledgerTable = "schema_migrations_applied"

// ledger is the applied-version record for one database.
type ledger struct{ pool *pgxpool.Pool }

// openLedger connects to the configured database and returns the ledger and its closer.
//
// A second connection alongside golang-migrate's own, rather than borrowing it: the library does
// not expose its handle, and a migration tool that held one connection for two purposes would
// make a lock held by one half look like a hang in the other.
func openLedger() (*ledger, func(), error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.Database.URL)
	if err != nil {
		return nil, nil, fmt.Errorf("connecting for the migration ledger: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("connecting for the migration ledger: %w", err)
	}
	return &ledger{pool: pool}, pool.Close, nil
}

// ensure creates the ledger if it is not there yet.
//
// IF NOT EXISTS rather than a migration, for the chicken-and-egg reason in the package comment.
// applied_at is recorded because the first question asked of a surprising schema is "when".
func (l *ledger) ensure(ctx context.Context) error {
	_, err := l.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS `+ledgerTable+` (
			version    bigint      PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`)
	if err != nil {
		return fmt.Errorf("creating %s: %w", ledgerTable, err)
	}
	return nil
}

// applied reads the set of versions the ledger knows about.
func (l *ledger) applied(ctx context.Context) (map[int]bool, error) {
	rows, err := l.pool.Query(ctx, `SELECT version FROM `+ledgerTable)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", ledgerTable, err)
	}
	defer rows.Close()

	out := map[int]bool{}
	for rows.Next() {
		var v int64
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[int(v)] = true
	}
	return out, rows.Err()
}

// record adds versions to the ledger, ignoring any it already holds.
func (l *ledger) record(ctx context.Context, versions []int) error {
	for _, v := range versions {
		_, err := l.pool.Exec(ctx,
			`INSERT INTO `+ledgerTable+` (version) VALUES ($1) ON CONFLICT DO NOTHING`, v)
		if err != nil {
			return fmt.Errorf("recording migration %d: %w", v, err)
		}
	}
	return nil
}

// forget drops every recorded version above a ceiling, which is what a `down` leaves behind.
func (l *ledger) forget(ctx context.Context, above int) error {
	_, err := l.pool.Exec(ctx, `DELETE FROM `+ledgerTable+` WHERE version > $1`, above)
	if err != nil {
		return fmt.Errorf("forgetting migrations above %d: %w", above, err)
	}
	return nil
}

// embeddedVersions lists every migration compiled into this binary, ascending.
func embeddedVersions() ([]int, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("reading embedded migrations: %w", err)
	}

	var out []int
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		match := sequencePrefix.FindStringSubmatch(name)
		if match == nil {
			continue
		}
		n, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	sort.Ints(out)
	return out, nil
}

// skipped reports the migrations golang-migrate will silently pass over.
//
// A version is skipped when it is at or below the recorded version — so `up` will not consider
// it — and the ledger has no record of it ever running.
func skipped(versions []int, current int, applied map[int]bool) []int {
	var out []int
	for _, v := range versions {
		if v <= current && !applied[v] {
			out = append(out, v)
		}
	}
	return out
}

// backfill seeds the ledger for a database that predates it.
//
// Every database in existence when SHIP-15g landed has a version and no ledger, and the only
// defensible reading of that state is that the versions at or below it were applied in order —
// which is exactly what golang-migrate guaranteed up to that point. Seeding them is what stops
// the first run after this change reporting every historical migration as skipped.
//
// The window this misses is narrow and worth naming: a database that had *already* been bitten
// before the guard existed is recorded as healthy. Nothing can distinguish that case, and
// `make migrate-down n=all && make migrate-up` fixes it the same way.
func backfill(versions []int, current int) []int {
	var out []int
	for _, v := range versions {
		if v <= current {
			out = append(out, v)
		}
	}
	return out
}

// errSkipped is returned when a migration would be passed over, with the fix in the message.
func errSkipped(missing []int) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%d migration(s) would be skipped silently:\n", len(missing))
	for _, v := range missing {
		name := "unknown domain"
		if block, ok := migrations.BlockContaining(v); ok {
			name = block.Domain
		}
		fmt.Fprintf(&b, "  %06d  (%s)\n", v, name)
	}
	b.WriteString(
		"\nThese are numbered below the version this database already records, so `up` will\n" +
			"not consider them: golang-migrate applies only migrations above the current\n" +
			"version, and this repository draws numbers from reserved per-domain blocks rather\n" +
			"than in time order (migrations/blocks.go).\n\n" +
			"Rebuild the schema, which is safe on a development database and is what CI does\n" +
			"on every run:\n\n" +
			"    make migrate-down n=all && make migrate-up\n")
	return errors.New(b.String())
}
