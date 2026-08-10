package migrations

import (
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/boundaries"
)

// filename matches `000123_some_name.up.sql`.
var filename = regexp.MustCompile(`^(\d+)_([a-z0-9_]+)\.(up|down)\.sql$`)

type migrationFile struct {
	version   int
	name      string
	direction string
	file      string
}

// readMigrations reads the embedded filesystem rather than the directory, so these tests
// check what the binary actually ships.
func readMigrations(t *testing.T) []migrationFile {
	t.Helper()

	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatalf("reading the embedded migrations: %v", err)
	}

	var out []migrationFile
	for _, e := range entries {
		match := filename.FindStringSubmatch(e.Name())
		if match == nil {
			t.Errorf("%s does not look like a migration; expected `<number>_<snake_case_name>.(up|down).sql`", e.Name())
			continue
		}
		v, err := strconv.Atoi(match[1])
		if err != nil {
			t.Errorf("%s: unreadable version: %v", e.Name(), err)
			continue
		}
		out = append(out, migrationFile{version: v, name: match[2], direction: match[3], file: e.Name()})
	}
	return out
}

// TestNoDuplicateVersions is the test that catches the collision this whole scheme exists to
// prevent.
//
// Two branches that each add a migration produce files whose names differ, so git merges them
// without complaint and review sees two plausible migrations. golang-migrate then refuses the
// duplicate version at `migrate up` — on somebody's machine, after the work has been approved.
// This moves that failure to the merge, where it is a one-line diff.
func TestNoDuplicateVersions(t *testing.T) {
	seenName := map[int]string{}

	for _, m := range readMigrations(t) {
		previous, seen := seenName[m.version]
		if seen && previous != m.name {
			t.Errorf("version %d is used by two different migrations, %q and %q — "+
				"two branches drew the same number; renumber one within its block (migrations/blocks.go)",
				m.version, previous, m.name)
		}
		seenName[m.version] = m.name
	}
}

// TestEveryMigrationHasBothDirections catches the half-written pair.
//
// A missing down migration is invisible until someone rolls back, which is the worst possible
// moment to discover it — `make migrate-down` is what you reach for when something has already
// gone wrong.
func TestEveryMigrationHasBothDirections(t *testing.T) {
	directions := map[int]map[string]bool{}
	names := map[int]string{}

	for _, m := range readMigrations(t) {
		if directions[m.version] == nil {
			directions[m.version] = map[string]bool{}
		}
		directions[m.version][m.direction] = true
		names[m.version] = m.name
	}

	for version, dirs := range directions {
		for _, want := range []string{"up", "down"} {
			if !dirs[want] {
				t.Errorf("migration %d (%s) has no .%s.sql", version, names[version], want)
			}
		}
	}
}

// TestEveryMigrationIsInItsBlock stops a domain drawing a number from another domain's range,
// which would reintroduce the shared counter the blocks exist to remove.
func TestEveryMigrationIsInItsBlock(t *testing.T) {
	for _, m := range readMigrations(t) {
		if _, ok := BlockContaining(m.version); !ok {
			t.Errorf("%s has version %d, which falls in no reserved block; "+
				"create migrations with `make migrate-create name=%s domain=<domain>`",
				m.file, m.version, m.name)
		}
	}
}

// TestBlocksDoNotOverlap checks the registry itself. An overlap would let two domains draw the
// same number while every other test in this file still passed.
func TestBlocksDoNotOverlap(t *testing.T) {
	for i, a := range Blocks {
		if a.First > a.Last {
			t.Errorf("block %q is inverted: %d to %d", a.Domain, a.First, a.Last)
		}
		if a.First <= 0 {
			t.Errorf("block %q starts at %d; migration versions start at 1", a.Domain, a.First)
		}
		for _, b := range Blocks[i+1:] {
			if a.First <= b.Last && b.First <= a.Last {
				t.Errorf("blocks %q (%d-%d) and %q (%d-%d) overlap",
					a.Domain, a.First, a.Last, b.Domain, b.First, b.Last)
			}
			if a.Domain == b.Domain {
				t.Errorf("two blocks are reserved for %q", a.Domain)
			}
		}
	}
}

// TestEveryDomainHasABlock keeps this registry and the boundary lint's domain list in step.
//
// They are two expressions of the same set. A ninth domain added to one and not the other
// would be a domain that cannot create a migration, discovered at the least convenient moment.
func TestEveryDomainHasABlock(t *testing.T) {
	for _, domain := range boundaries.Domains {
		if _, err := BlockFor(domain); err != nil {
			t.Errorf("domain %q has no reserved migration block: %v", domain, err)
		}
	}

	if _, err := BlockFor(SharedDomain); err != nil {
		t.Errorf("the shared block is missing: %v", err)
	}

	// Every block is either one of the eight domains or the shared one. A block for
	// something else means a domain was renamed here and not in boundaries.go.
	for _, b := range Blocks {
		if b.Domain == SharedDomain {
			continue
		}
		known := false
		for _, domain := range boundaries.Domains {
			if b.Domain == domain {
				known = true
				break
			}
		}
		if !known {
			t.Errorf("block %q matches no domain in Docs/06 §3 and is not %q",
				b.Domain, SharedDomain)
		}
	}
}

// TestBlockForNamesTheAlternatives checks the error a mistyped domain produces, because that
// error is the entire user interface of this mechanism.
func TestBlockForNamesTheAlternatives(t *testing.T) {
	_, err := BlockFor("job") // singular; the domain is "jobs"
	if err == nil {
		t.Fatal("expected an unknown domain to be refused")
	}
	if !strings.Contains(err.Error(), "jobs") {
		t.Errorf("the error should name the real domains so the typo is obvious; got %q", err)
	}
}
