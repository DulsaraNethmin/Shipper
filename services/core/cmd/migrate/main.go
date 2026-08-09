// Command migrate applies and reverses the database schema (SHIP-7).
//
// It wraps golang-migrate as a library rather than shelling out to its CLI. The CLI
// selects its database drivers with build tags, which makes "works on my machine" a
// property of how the tool was compiled rather than of this repository. Building it here
// pins the driver, the source, and the migration files together in one reproducible
// binary.
//
// Usage:
//
//	migrate up [n]        apply all pending migrations, or the next n
//	migrate down [n|all]  reverse the last migration, the last n, or everything
//	migrate version       print the current version and whether it is dirty
//	migrate force <v>     mark the schema as being at version v without running anything
//	migrate create <name> write the next pair of empty migration files
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/migrations"
)

// defaultDir is relative to services/core, which is where the go commands in the root
// Makefile run from.
const defaultDir = "migrations"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "shipper-migrate: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	dir := flag.String("dir", defaultDir, "directory holding migration files (used by create only)")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
		return errors.New("no command given")
	}

	switch cmd := args[0]; cmd {
	case "up":
		return up(args[1:])
	case "down":
		return down(args[1:])
	case "version":
		return version()
	case "force":
		return force(args[1:])
	case "create":
		return create(*dir, args[1:])
	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `shipper-migrate — database schema migrations

  migrate up [n]         apply all pending migrations, or only the next n
  migrate down [n|all]   reverse the last migration (default 1), the last n, or all
  migrate version        print the current schema version
  migrate force <v>      set the recorded version to v without running migrations
  migrate create <name>  write the next pair of empty migration files

The database is taken from DATABASE_URL. Migrations are compiled into this binary; the
-dir flag only affects where `+"`create`"+` writes new files.
`)
}

// open builds a Migrate over the embedded migrations and the configured database.
func open() (*migrate.Migrate, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("reading embedded migrations: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, cfg.Database.URL)
	if err != nil {
		return nil, fmt.Errorf("connecting to database: %w", err)
	}
	m.Log = logger{}
	return m, nil
}

// closeMigrate reports the errors Close returns, which are easy to discard and are the
// ones that tell you a migration left a connection or a lock behind.
func closeMigrate(m *migrate.Migrate) {
	if srcErr, dbErr := m.Close(); srcErr != nil || dbErr != nil {
		fmt.Fprintf(os.Stderr, "shipper-migrate: close: source=%v database=%v\n", srcErr, dbErr)
	}
}

func up(args []string) error {
	m, err := open()
	if err != nil {
		return err
	}
	defer closeMigrate(m)

	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil || n <= 0 {
			return fmt.Errorf("up: %q is not a positive number of steps", args[0])
		}
		return report(m, m.Steps(n))
	}
	return report(m, m.Up())
}

func down(args []string) error {
	m, err := open()
	if err != nil {
		return err
	}
	defer closeMigrate(m)

	// golang-migrate's Down() reverses everything. That is a reasonable library default
	// and a poor command-line one, so an unqualified `down` here steps back exactly one
	// migration and dropping the whole schema has to be asked for by name.
	if len(args) == 0 {
		return report(m, m.Steps(-1))
	}

	if strings.EqualFold(args[0], "all") {
		return report(m, m.Down())
	}

	n, err := strconv.Atoi(args[0])
	if err != nil || n <= 0 {
		return fmt.Errorf("down: %q is not a positive number of steps, nor \"all\"", args[0])
	}
	return report(m, m.Steps(-n))
}

func version() error {
	m, err := open()
	if err != nil {
		return err
	}
	defer closeMigrate(m)

	v, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		fmt.Println("no migrations applied")
		return nil
	}
	if err != nil {
		return err
	}

	if dirty {
		// A dirty version means a migration failed part-way and the schema is in an
		// unknown state. Nothing else should run until a human has looked at it.
		fmt.Printf("version %d (DIRTY — a migration failed part-way; inspect the schema, "+
			"then `migrate force <version>` once it matches)\n", v)
		return errors.New("schema is dirty")
	}
	fmt.Printf("version %d\n", v)
	return nil
}

func force(args []string) error {
	if len(args) != 1 {
		return errors.New("force: expected exactly one version")
	}
	v, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("force: %q is not a version number", args[0])
	}

	m, err := open()
	if err != nil {
		return err
	}
	defer closeMigrate(m)

	if err := m.Force(v); err != nil {
		return err
	}
	fmt.Printf("forced version to %d\n", v)
	return nil
}

var (
	sequencePrefix = regexp.MustCompile(`^(\d+)_`)
	nonNameChars   = regexp.MustCompile(`[^a-z0-9]+`)
)

// create writes the next numbered up/down pair.
//
// Sequence numbers rather than timestamps: with a single ordered backlog there is no
// concurrent-branch problem for them to solve, and a gap in a sequence is immediately
// visible where a missing timestamp is not.
func create(dir string, args []string) error {
	if len(args) != 1 {
		return errors.New("create: expected exactly one name, e.g. `create users_table`")
	}

	name := strings.Trim(nonNameChars.ReplaceAllString(strings.ToLower(args[0]), "_"), "_")
	if name == "" {
		return fmt.Errorf("create: %q leaves no usable name", args[0])
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading %s: %w", dir, err)
	}

	next := 1
	for _, e := range entries {
		match := sequencePrefix.FindStringSubmatch(e.Name())
		if match == nil {
			continue
		}
		if n, err := strconv.Atoi(match[1]); err == nil && n >= next {
			next = n + 1
		}
	}

	for _, direction := range []string{"up", "down"} {
		path := filepath.Join(dir, fmt.Sprintf("%06d_%s.%s.sql", next, name, direction))
		// O_EXCL: never silently overwrite a migration that already exists.
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return fmt.Errorf("creating %s: %w", path, err)
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("closing %s: %w", path, err)
		}
		fmt.Println(path)
	}
	return nil
}

// report turns golang-migrate's "nothing to do" sentinel into a normal outcome. It is
// not an error, and treating it as one makes `make migrate-up` fail on a second run.
func report(m *migrate.Migrate, err error) error {
	if errors.Is(err, migrate.ErrNoChange) {
		fmt.Println("no change")
		return nil
	}
	if err != nil {
		return err
	}

	v, dirty, verr := m.Version()
	if verr != nil {
		if errors.Is(verr, migrate.ErrNilVersion) {
			fmt.Println("ok — no migrations applied")
			return nil
		}
		return verr
	}
	fmt.Printf("ok — now at version %d (dirty=%t)\n", v, dirty)
	return nil
}

// logger adapts golang-migrate's logging to plain stderr output.
type logger struct{}

func (logger) Printf(format string, v ...any) { fmt.Fprintf(os.Stderr, format, v...) }
func (logger) Verbose() bool                  { return false }
