// Package boundaries is the import lint that enforces the architecture rules in
// Docs/06 §4.1 (SHIP-11).
//
// Docs/08 puts it plainly: enforce the boundaries from the first commit, because they are
// almost impossible to reintroduce later. A single import is all it takes to weld two
// domains together, it is invisible in review once the file is a hundred lines long, and
// by the time anyone wants the seam back the edge has been used forty times.
//
// # The rules
//
// Four. The first three are Docs/06 §4.1:
//
//  1. A domain package may not import another domain package. Domains collaborate through
//     interfaces they declare themselves, wired together in cmd/api.
//  2. A platform adapter may not import a domain. The adapter knows nothing about who
//     uses it.
//  3. A domain may not import a platform adapter either. Interfaces are declared by the
//     consuming domain, never by the implementing package: jobs/ports.go says what jobs
//     needs from geocoding, and Go's structural interface satisfaction means no import is
//     required in either direction.
//  4. Infrastructure may import neither a domain nor an adapter.
//
// Rules 2 and 3 are one rule seen from both ends. Together they mean the arrow between a
// domain and its adapters does not exist at all — the two meet in the composition root
// and nowhere else.
//
// # Why rule 4 exists (SHIP-15c)
//
// The first three rules are all about domains and adapters, so for a long time
// infrastructure could import a domain and this lint stayed green. That is a bigger hole
// than it sounds, and the reason is transitivity: **every domain imports httpx**. One
// import of internal/identity from inside internal/httpx welds every domain in the service
// to identity, through an edge no domain's own file contains and no reviewer reading a
// domain package would see.
//
// SHIP-44 is the first ticket with a real motive to do it — the authentication middleware
// has to verify a token, and identity is where the verifier lives. The shape that keeps the
// seam is for httpx to take a function:
//
//	httpx.Authenticate(verify func(context.Context, string) (authctx.Subject, error))
//
// with the closure over identity supplied in cmd/api. httpx importing authctx is
// infrastructure on infrastructure and fine; httpx importing identity is what this rule now
// refuses.
//
// It was adoptable with no refactoring at all: nothing under internal/{authctx, clock,
// config, db, events, httpx, idempotency, logging, testsupport, validate} imported a domain
// or an adapter when the rule was written. That is the moment to add a rule — before the
// first edge, not after the fortieth.
//
// Note what rule 4 does *not* say. Infrastructure importing infrastructure is ordinary and
// unrestricted: httpx already imports idempotency, and that is the seam working rather than
// leaking.
//
// One consequence to expect rather than discover: internal/testsupport is infrastructure,
// and its files are not _test.go, so a shared fixture that builds a domain's aggregate would
// break this rule. That fixture belongs in the domain it is about — pgtest hands out a
// database, and what is written into it is the domain's own business.
//
// # Why every package must be classified
//
// The rules can only be applied to a package whose kind is known, so an internal package
// that is neither a listed domain nor listed infrastructure is itself reported. That is
// deliberate: it makes adding one a decision recorded here rather than something that
// happens by accident and quietly escapes the lint.
//
// # What is not checked
//
// Test files are skipped. A test wires domains together in the same way cmd/api does, and
// these rules are about the dependency edges in the built service.
package boundaries

import (
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Domains are the eight platform domains of Docs/06 §3, in the order that document lists
// them. The set is closed: a ninth domain is an architectural decision, and adding one
// here is where that decision gets recorded.
var Domains = []string{
	"identity",      // identity and access
	"profiles",      // customer and provider profiles
	"fleet",         // vehicle fleet and eligibility
	"jobs",          // jobs
	"bidding",       // bidding and negotiation
	"delivery",      // delivery execution and proof
	"notifications", // notifications
	"admin",         // administration, disputes, and audit
}

// AdapterRoot is the directory holding the integration adapter tree.
const AdapterRoot = "internal/platform"

// infrastructure names the internal packages that are not domains: the plumbing every
// domain is allowed to sit on top of. Extending this list is the deliberate act described
// in the package comment.
//
// The list is deliberately seeded ahead of the code. Everything the next several milestones
// need is already here, so that a domain package being built never has a reason to reach
// into this file — which matters because two domains can be under construction at once, and
// this file is shared (Docs/10 §9.2). An entry that is still unused is a commitment already
// made, not an oversight.
//
// `passwords` is the first entry added the other way round: not seeded ahead of a consumer, but
// registered at SHIP-15r because a *second* consumer arrived. It had been inside internal/identity
// since SHIP-29, and SHIP-147 gives an administrator a password in a domain that may not import
// identity. The alternative was two argon2id implementations agreeing by comment about a security
// parameter, which is what Docs/10 §3.4 exists to refuse — so the code moved rather than being
// copied, and the entry records that this list grows for a reason somebody wrote down.
var infrastructure = map[string]string{
	"authctx":     "the authenticated subject, readable by every domain",
	"boundaries":  "this lint",
	"buildinfo":   "version and commit, injected at link time",
	"clock":       "the injectable clock, so scheduled work is testable",
	"config":      "environment configuration",
	"db":          "the Runner seam and transaction helper",
	"events":      "the domain event sink and the transactional outbox writer",
	"httpx":       "HTTP middleware and helpers",
	"idempotency": "the Redis-backed idempotency store",
	"logging":     "structured logger construction",
	"money":       "minor-unit arithmetic in AUD",
	"pagination":  "cursor encoding, shared by every list endpoint",
	"passwords":   "argon2id password storage, shared by identity and admin",
	"ratelimit":   "the Redis token bucket behind every limited route",
	"testsupport": "real-database and real-Redis test harnesses",
	"validate":    "field-level validation returning the error contract's shape",
}

// Kind is what a package is, which is what decides the rules that apply to it.
type Kind string

const (
	KindDomain         Kind = "domain"
	KindAdapter        Kind = "adapter"
	KindInfrastructure Kind = "infrastructure"
	KindEntrypoint     Kind = "entrypoint"
	KindUnclassified   Kind = "unclassified"
)

// Package is a classified package, identified by its path relative to the module root.
type Package struct {
	Path string // e.g. "internal/jobs", "internal/platform/email", "cmd/api"
	Kind Kind
	// Domain is the domain a package belongs to, for a domain package and its
	// subpackages alike. Empty for every other kind.
	Domain string
}

// Violation is one broken rule, reported at the position that breaks it.
type Violation struct {
	File   string // relative to the module root
	Line   int
	Rule   string
	Detail string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", v.File, v.Line, v.Rule, v.Detail)
}

// Classify decides what kind of package sits at rel, a slash-separated path relative to
// the module root.
func Classify(rel string) Package {
	rel = path.Clean(filepath.ToSlash(rel))
	p := Package{Path: rel}

	switch {
	case rel == AdapterRoot || strings.HasPrefix(rel, AdapterRoot+"/"):
		p.Kind = KindAdapter

	case strings.HasPrefix(rel, "cmd/"):
		p.Kind = KindEntrypoint

	case strings.HasPrefix(rel, "internal/"):
		// The segment after internal/ decides: a domain's subpackages belong to that
		// same domain, so internal/jobs/pricing is still jobs and may not reach into
		// bidding either.
		name, _, _ := strings.Cut(strings.TrimPrefix(rel, "internal/"), "/")
		switch {
		case isDomain(name):
			p.Kind, p.Domain = KindDomain, name
		case infrastructure[name] != "":
			p.Kind = KindInfrastructure
		default:
			p.Kind = KindUnclassified
		}

	// Everything outside internal/ and cmd/ is support: migrations, and the module root
	// itself. None of it is a domain, and none of it is reachable from outside the
	// module, so the rules have nothing to say about it.
	default:
		p.Kind = KindInfrastructure
	}

	return p
}

func isDomain(name string) bool {
	for _, d := range Domains {
		if d == name {
			return true
		}
	}
	return false
}

// Check parses every non-test Go file under root and reports each rule broken.
//
// It reads imports out of the source rather than asking the build system for a package
// graph. That keeps the lint free of build tags, of a working module cache, and of
// anything else that might make it pass on one machine and fail on another — the check
// has to be believable, and one that runs differently in CI is not.
func Check(root string) ([]Violation, error) {
	modulePath, err := ModulePath(root)
	if err != nil {
		return nil, err
	}

	var violations []Violation
	unclassified := map[string]bool{}

	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDir(root, p, d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}

		rel, err := filepath.Rel(root, filepath.Dir(p))
		if err != nil {
			return err
		}
		relFile, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		relFile = filepath.ToSlash(relFile)

		from := Classify(rel)
		if from.Kind == KindUnclassified && !unclassified[from.Path] {
			unclassified[from.Path] = true
			violations = append(violations, Violation{
				File: relFile,
				Line: 1,
				Rule: "unclassified package",
				Detail: fmt.Sprintf("%s is neither one of the eight domains in Docs/06 §3 nor "+
					"listed infrastructure; add it to Domains or to infrastructure in "+
					"internal/boundaries/boundaries.go, whichever it is", from.Path),
			})
		}

		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("%s: %w", relFile, err)
		}

		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			// Only imports within this module can break an internal boundary.
			if !strings.HasPrefix(imported, modulePath+"/") {
				continue
			}
			to := Classify(strings.TrimPrefix(imported, modulePath+"/"))

			rule, detail, broken := violates(from, to)
			if !broken {
				continue
			}
			violations = append(violations, Violation{
				File:   relFile,
				Line:   fset.Position(spec.Pos()).Line,
				Rule:   rule,
				Detail: detail,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(violations, func(i, j int) bool {
		if violations[i].File != violations[j].File {
			return violations[i].File < violations[j].File
		}
		return violations[i].Line < violations[j].Line
	})
	return violations, nil
}

// violates applies the four rules to one import edge.
func violates(from, to Package) (rule, detail string, broken bool) {
	switch {
	case from.Kind == KindInfrastructure && to.Kind == KindDomain:
		return "infrastructure imports domain", fmt.Sprintf(
			"%s imports %s. Every domain sits on infrastructure, so this one edge couples "+
				"all eight to %s transitively — and it appears in no domain's own files. "+
				"Take what you need as a function or an interface declared here and let "+
				"cmd/api supply the closure over %s (Docs/06 §4.1, SHIP-15c)",
			from.Path, to.Path, to.Domain, to.Domain), true

	case from.Kind == KindInfrastructure && to.Kind == KindAdapter:
		return "infrastructure imports adapter", fmt.Sprintf(
			"%s imports %s. Infrastructure is what every domain and every adapter is built "+
				"on, so it cannot depend on one of them: this makes the adapter a "+
				"prerequisite of packages that have nothing to do with it. Declare the "+
				"interface here and wire the implementation in cmd/api (Docs/06 §4.1, "+
				"SHIP-15c)",
			from.Path, to.Path), true

	case from.Kind == KindDomain && to.Kind == KindDomain && from.Domain != to.Domain:
		return "domain imports domain", fmt.Sprintf(
			"%s imports %s. Domains do not depend on each other: declare what %s needs in "+
				"%s/ports.go and wire the implementation in cmd/api (Docs/06 §4.1)",
			from.Path, to.Path, from.Domain, path.Join("internal", from.Domain)), true

	case from.Kind == KindAdapter && to.Kind == KindDomain:
		return "adapter imports domain", fmt.Sprintf(
			"%s imports %s. An adapter knows nothing about the domain that uses it — the "+
				"interface belongs to %s, not here (Docs/06 §4.1)",
			from.Path, to.Path, to.Domain), true

	case from.Kind == KindDomain && to.Kind == KindAdapter:
		return "domain imports adapter", fmt.Sprintf(
			"%s imports %s. Interfaces are declared by the consuming domain: put what %s "+
				"needs in %s/ports.go and let the adapter satisfy it structurally "+
				"(Docs/06 §4.1)",
			from.Path, to.Path, from.Domain, path.Join("internal", from.Domain)), true
	}
	return "", "", false
}

// skipDir keeps the walk out of directories that hold no first-party source.
func skipDir(root, p, name string) bool {
	if p == root {
		return false
	}
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
		name == "testdata" || name == "vendor" || name == "node_modules"
}

// ModulePath reads the module path out of root/go.mod.
func ModulePath(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("reading the module path: %w", err)
	}
	for line := range strings.SplitSeq(string(b), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	return "", fmt.Errorf("%s has no module directive", filepath.Join(root, "go.mod"))
}

// ModuleRoot walks up from start until it finds the directory holding go.mod.
//
// Tests run with the working directory set to their own package, and the lint binary may
// be run from anywhere, so neither can assume it starts at the module root.
func ModuleRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod at or above %s", start)
		}
		dir = parent
	}
}
