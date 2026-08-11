// Package pgtest gives a test its own PostgreSQL database.
//
// Docs/06 §4.1 requires tests to run against a real PostgreSQL rather than a mocked
// repository, and gives the reason: "a mock happily accepts a write that the actual constraint
// would reject". The constraint that matters most is SHIP-91's partial unique index, whose
// entire job is to reject a write the application logic would otherwise allow.
//
// # Why a database per test binary rather than one shared database
//
// This is needed even with one developer. `go test ./...` runs different packages in parallel
// by default, so two packages sharing one database will truncate each other's rows midway
// through a test. Add several branches building at once and the interference is constant and
// looks like flakiness.
//
// # Why a template rather than running the migrations each time
//
// CREATE DATABASE ... TEMPLATE copies the files of an existing database, which takes
// milliseconds regardless of how many migrations there are. Running the migration chain per
// test binary would get slower with every ticket, and a test suite that gets slower with every
// ticket is one people stop running.
//
// The template is built once by `make test-db-template`.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TemplateName is the fallback template name, used only when TEST_TEMPLATE_DB is unset.
//
// `make test` always exports one, derived from the worktree's directory name — see the note on
// templateName. So this is reached only by a bare `go test`, where the template it names has
// almost certainly never been built, and the failure in unavailable is the intended outcome
// rather than an accident: a name that resolved to somebody else's template would be worse.
const TemplateName = "shipper_test_template"

// DB returns a pool over a fresh database cloned from the template, and drops it when the test
// ends.
//
// Each call produces its own database, so tests within a package may run in parallel and tests
// across packages cannot see each other's rows.
func DB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := connectAdmin(ctx)
	if err != nil {
		unavailable(t, err)
		return nil
	}
	defer admin.Close(context.Background())

	name := cloneName(t)

	// CREATE DATABASE cannot run inside a transaction, which is why this is a bare Exec.
	// The template must have no other connections at the moment of the copy; `make
	// test-db-template` leaves none, and clones are never used as templates themselves.
	if _, err := admin.Exec(ctx,
		fmt.Sprintf(`CREATE DATABASE %s TEMPLATE %s`, quote(name), quote(templateName())),
	); err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			t.Fatalf("pgtest: the template database %q does not exist — run `make test-db-template` (%v)",
				templateName(), err)
		}
		t.Fatalf("pgtest: cloning %s into %s: %v", templateName(), name, err)
	}

	pool, err := pgxpool.New(ctx, urlForDatabase(baseURL(), name))
	if err != nil {
		t.Fatalf("pgtest: connecting to %s: %v", name, err)
	}

	t.Cleanup(func() {
		pool.Close()

		// A fresh context: the test's own may already be cancelled, and a database left
		// behind is a database that will still be there tomorrow.
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dropCancel()

		conn, err := pgx.Connect(dropCtx, baseURL())
		if err != nil {
			t.Logf("pgtest: could not connect to drop %s: %v", name, err)
			return
		}
		defer conn.Close(context.Background())

		// WITH (FORCE) terminates stragglers. Without it a leaked connection in the code
		// under test turns a cleanup into a failure, which reports the wrong problem.
		if _, err := conn.Exec(dropCtx,
			fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, quote(name)),
		); err != nil {
			t.Logf("pgtest: could not drop %s: %v", name, err)
		}
	})

	return pool
}

// connectAdmin opens a connection to the database named in the base URL, from which other
// databases can be created.
func connectAdmin(ctx context.Context) (*pgx.Conn, error) {
	return pgx.Connect(ctx, baseURL())
}

// unavailable decides what an unreachable database means.
//
// Skipping is reserved for `-short`, and for nothing else. The existing Redis tests skipped
// themselves whenever Redis was absent, and the CI notes in .github/workflows/README.md already
// name the consequence: "they pass by being skipped, which reads as green". A test that quietly
// does not run is worse than a test that does not exist, because it is counted.
//
// So: `-short` skips, everything else fails. The message says how to fix it, because the fix is
// almost always the same two commands.
func unavailable(t *testing.T, err error) {
	t.Helper()

	if testing.Short() {
		t.Skipf("pgtest: no PostgreSQL at %s and -short was given (%v)", redacted(baseURL()), err)
		return
	}
	t.Fatalf("pgtest: no PostgreSQL at %s (%v)\n"+
		"run `make test` from the repository root — it starts from deploy/.env and builds the\n"+
		"template first. `go test` on its own does not read deploy/.env, so it looks for\n"+
		"PostgreSQL on the default port. Pass -short to skip tests that need a database.",
		redacted(baseURL()), err)
}

// baseURL is the database to connect to in order to create others, and the template for a
// clone's own URL.
//
// TEST_DATABASE_URL comes first so a worktree can point at an entirely separate PostgreSQL. It
// does **not** isolate two worktrees sharing one cluster, which is the usual arrangement here —
// COMPOSE_PROJECT_NAME is pinned so that every worktree uses the same containers. What isolates
// them is TEST_TEMPLATE_DB; see templateName (Docs/10 §7.1).
func baseURL() string {
	if u := os.Getenv("TEST_DATABASE_URL"); u != "" {
		return u
	}
	if u := os.Getenv("DATABASE_URL"); u != "" {
		return u
	}
	return "postgres://shipper:shipper@localhost:5432/shipper?sslmode=disable"
}

// templateName is the template every clone in this process is copied from.
//
// This is the variable that separates two git worktrees running tests at once, and the reason is
// worth stating where somebody will find it. `CREATE DATABASE … TEMPLATE …` resolves the name at
// *cluster* scope, and every worktree shares one cluster deliberately. Two worktrees with the
// same template name are two worktrees using one database: `make test` in either drops and
// rebuilds it while the other is midway through a clone.
//
// The Makefile derives a per-directory default, so this is set for anything run through `make`
// and nobody has to remember it.
func templateName() string {
	if n := os.Getenv("TEST_TEMPLATE_DB"); n != "" {
		return n
	}
	return TemplateName
}

// cloneName builds a database name that identifies the test that owns it.
//
// PostgreSQL truncates identifiers at 63 bytes, and a name that has been truncated into
// ambiguity is no use when a leftover database has to be traced back to what created it — so
// the random suffix goes first, where it cannot be cut off, and the test name is trimmed.
func cloneName(t *testing.T) string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("pgtest: generating a database name: %v", err)
	}

	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '_'
		}
	}, t.Name())

	const maxSafe = 30
	if len(safe) > maxSafe {
		safe = safe[:maxSafe]
	}
	return fmt.Sprintf("test_%s_%s", hex.EncodeToString(b[:]), safe)
}

// urlForDatabase rewrites the path of a connection URL to point at a different database,
// keeping credentials, host and options.
func urlForDatabase(raw, database string) string {
	u, err := url.Parse(raw)
	if err != nil {
		// baseURL was already used to connect successfully by the time this is reached,
		// so an unparseable URL here is not a situation a test can recover from.
		panic(fmt.Sprintf("pgtest: unparseable base URL: %v", err))
	}
	u.Path = "/" + database
	return u.String()
}

// quote wraps an identifier in double quotes, doubling any it contains.
//
// Every name reaching this is generated by cloneName or comes from configuration, so this is
// not guarding against a hostile input — but a database name is interpolated into DDL that
// cannot be parameterised, and leaving that unquoted is the kind of thing that is safe until
// somebody sets TEST_TEMPLATE_DB to something with a hyphen in it.
func quote(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

// redacted removes the password from a URL so it can appear in a failure message.
func redacted(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "<unparseable>"
	}
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(u.User.Username(), "xxxxx")
		}
	}
	return u.String()
}
