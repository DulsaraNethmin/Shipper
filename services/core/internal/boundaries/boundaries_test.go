package boundaries

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testModule = "github.com/DulsaraNethmin/Shipper/services/core"

// writeModule builds a throwaway module whose files are given as package path to import
// list, and returns its root.
//
// The rules are about which package imports which, so the fixtures need nothing else: a
// package clause and its imports are the whole of the input.
func writeModule(t *testing.T, packages map[string][]string) string {
	t.Helper()
	root := t.TempDir()

	if err := os.WriteFile(filepath.Join(root, "go.mod"),
		[]byte("module "+testModule+"\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for pkgPath, imports := range packages {
		dir := filepath.Join(root, filepath.FromSlash(pkgPath))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}

		name := filepath.Base(pkgPath)
		if strings.HasPrefix(pkgPath, "cmd/") {
			name = "main"
		}

		var b strings.Builder
		fmt.Fprintf(&b, "package %s\n\n", name)
		for _, imp := range imports {
			fmt.Fprintf(&b, "import _ %q\n", imp)
		}

		if err := os.WriteFile(filepath.Join(dir, "pkg.go"), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func local(pkgPath string) string { return testModule + "/" + pkgPath }

// ---------------------------------------------------------------------------------------
// The acceptance criterion, in both directions: the real tree passes, and each rule fires
// on a tree that breaks it.

// SHIP-11: CI fails when one domain package imports another directly, or when an adapter
// imports a domain. This is the half that keeps the repository honest.
func TestNoBoundaryIsCrossed(t *testing.T) {
	root, err := ModuleRoot(".")
	if err != nil {
		t.Fatal(err)
	}

	violations, err := Check(root)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	for _, v := range violations {
		t.Errorf("%s", v)
	}
}

func TestEachRuleIsCaught(t *testing.T) {
	tests := []struct {
		name     string
		packages map[string][]string
		wantRule string
		wantFile string
		wantLine int
	}{
		{
			name: "a domain importing another domain",
			packages: map[string][]string{
				"internal/jobs":    {local("internal/bidding")},
				"internal/bidding": nil,
			},
			wantRule: "domain imports domain",
			wantFile: "internal/jobs/pkg.go",
			wantLine: 3,
		},
		{
			// internal/jobs/pricing is still jobs. Nesting a package is not a way out of
			// the rule, and it is the obvious thing to try.
			name: "a domain subpackage importing another domain",
			packages: map[string][]string{
				"internal/jobs/pricing": {local("internal/bidding")},
				"internal/bidding":      nil,
			},
			wantRule: "domain imports domain",
			wantFile: "internal/jobs/pricing/pkg.go",
			wantLine: 3,
		},
		{
			name: "an adapter importing a domain",
			packages: map[string][]string{
				"internal/platform/email": {local("internal/identity")},
				"internal/identity":       nil,
			},
			wantRule: "adapter imports domain",
			wantFile: "internal/platform/email/pkg.go",
			wantLine: 3,
		},
		{
			name: "a domain importing an adapter",
			packages: map[string][]string{
				"internal/jobs":               {local("internal/platform/geocoding")},
				"internal/platform/geocoding": nil,
			},
			wantRule: "domain imports adapter",
			wantFile: "internal/jobs/pkg.go",
			wantLine: 3,
		},
		{
			// SHIP-15c, and the edge SHIP-44 had the first real motive to create. Every
			// domain imports httpx, so this single import couples all eight to identity
			// through a file no domain package contains.
			name: "infrastructure importing a domain",
			packages: map[string][]string{
				"internal/httpx":    {local("internal/identity")},
				"internal/identity": nil,
			},
			wantRule: "infrastructure imports domain",
			wantFile: "internal/httpx/pkg.go",
			wantLine: 3,
		},
		{
			// The same rule from the adapter side. Infrastructure is underneath both, so
			// depending on either makes it a prerequisite of packages unrelated to it.
			name: "infrastructure importing an adapter",
			packages: map[string][]string{
				"internal/events":        {local("internal/platform/push")},
				"internal/platform/push": nil,
			},
			wantRule: "infrastructure imports adapter",
			wantFile: "internal/events/pkg.go",
			wantLine: 3,
		},
		{
			name:     "an internal package that is neither a domain nor infrastructure",
			packages: map[string][]string{"internal/matching": nil},
			wantRule: "unclassified package",
			wantFile: "internal/matching/pkg.go",
			// Reported against the package rather than an import, so the position is
			// the top of the file.
			wantLine: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			violations, err := Check(writeModule(t, tc.packages))
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if len(violations) != 1 {
				t.Fatalf("got %d violations, want 1: %v", len(violations), violations)
			}
			if violations[0].Rule != tc.wantRule {
				t.Errorf("rule = %q, want %q", violations[0].Rule, tc.wantRule)
			}
			if violations[0].File != tc.wantFile {
				t.Errorf("file = %q, want %q", violations[0].File, tc.wantFile)
			}
			if violations[0].Line != tc.wantLine {
				t.Errorf("line = %d, want %d", violations[0].Line, tc.wantLine)
			}
		})
	}
}

func TestPermittedImportsAreLeftAlone(t *testing.T) {
	tests := []struct {
		name     string
		packages map[string][]string
	}{
		{
			// The composition root is the one place a domain and its adapter meet.
			name: "the entrypoint wiring everything together",
			packages: map[string][]string{
				"cmd/api": {
					local("internal/jobs"),
					local("internal/bidding"),
					local("internal/platform/email"),
					local("internal/httpx"),
				},
				"internal/jobs":           nil,
				"internal/bidding":        nil,
				"internal/platform/email": nil,
				"internal/httpx":          nil,
			},
		},
		{
			name: "a domain on top of infrastructure",
			packages: map[string][]string{
				"internal/jobs":   {local("internal/httpx"), local("internal/config")},
				"internal/httpx":  nil,
				"internal/config": nil,
			},
		},
		{
			name: "a domain and its own subpackage",
			packages: map[string][]string{
				"internal/jobs":         {local("internal/jobs/pricing")},
				"internal/jobs/pricing": nil,
			},
		},
		{
			name: "an adapter on top of infrastructure",
			packages: map[string][]string{
				"internal/platform/email": {local("internal/config")},
				"internal/config":         nil,
			},
		},
		{
			name: "adapters alongside each other",
			packages: map[string][]string{
				"internal/platform/push":    {local("internal/platform/storage")},
				"internal/platform/storage": nil,
			},
		},
		{
			// Rule 4 is about domains and adapters only. Infrastructure sitting on
			// infrastructure is the seam working: httpx really does import idempotency,
			// and authctx is what SHIP-44's middleware is meant to reach for instead of
			// identity.
			name: "infrastructure on infrastructure",
			packages: map[string][]string{
				"internal/httpx":       {local("internal/authctx"), local("internal/idempotency")},
				"internal/authctx":     nil,
				"internal/idempotency": nil,
			},
		},
		{
			// The composition root is where the closure over a domain is supplied, which
			// is the way out rule 4's message names.
			name: "the entrypoint closing over a domain for infrastructure",
			packages: map[string][]string{
				"cmd/api":           {local("internal/httpx"), local("internal/identity")},
				"internal/httpx":    {local("internal/authctx")},
				"internal/authctx":  nil,
				"internal/identity": nil,
			},
		},
		{
			// The rules govern first-party edges. A domain reaching for the standard
			// library or a third-party module is not what they are about.
			name: "imports from outside the module",
			packages: map[string][]string{
				"internal/jobs": {"context", "github.com/redis/go-redis/v9"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			violations, err := Check(writeModule(t, tc.packages))
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			for _, v := range violations {
				t.Errorf("unexpected violation: %s", v)
			}
		})
	}
}

// A test wires domains together the way cmd/api does, so the lint reads production files
// only. This records that as a decision rather than leaving it to be rediscovered.
func TestTestFilesAreNotLinted(t *testing.T) {
	root := writeModule(t, map[string][]string{
		"internal/jobs":    nil,
		"internal/bidding": nil,
	})

	src := fmt.Sprintf("package jobs\n\nimport _ %q\n", local("internal/bidding"))
	if err := os.WriteFile(filepath.Join(root, "internal", "jobs", "wiring_test.go"),
		[]byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	violations, err := Check(root)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	for _, v := range violations {
		t.Errorf("unexpected violation: %s", v)
	}
}

// The message has to say what to do instead. A lint that only says "no" gets worked
// around, and the workaround is usually worse than the import.
func TestTheMessageNamesTheWayOut(t *testing.T) {
	violations, err := Check(writeModule(t, map[string][]string{
		"internal/jobs":               {local("internal/platform/geocoding")},
		"internal/platform/geocoding": nil,
	}))
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("got %d violations, want 1", len(violations))
	}

	detail := violations[0].Detail
	for _, want := range []string{"internal/jobs/ports.go", "Docs/06 §4.1"} {
		if !strings.Contains(detail, want) {
			t.Errorf("message does not mention %q:\n%s", want, detail)
		}
	}
}

// ---------------------------------------------------------------------------------------
// Classification

func TestClassify(t *testing.T) {
	tests := []struct {
		path       string
		wantKind   Kind
		wantDomain string
	}{
		{"internal/jobs", KindDomain, "jobs"},
		{"internal/jobs/pricing", KindDomain, "jobs"},
		{"internal/notifications", KindDomain, "notifications"},
		{"internal/platform", KindAdapter, ""},
		{"internal/platform/email", KindAdapter, ""},
		{"internal/httpx", KindInfrastructure, ""},
		{"internal/config", KindInfrastructure, ""},
		{"cmd/api", KindEntrypoint, ""},
		{"cmd/migrate", KindEntrypoint, ""},
		{"migrations", KindInfrastructure, ""},
		{"internal/matching", KindUnclassified, ""},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got := Classify(tc.path)
			if got.Kind != tc.wantKind {
				t.Errorf("kind = %q, want %q", got.Kind, tc.wantKind)
			}
			if got.Domain != tc.wantDomain {
				t.Errorf("domain = %q, want %q", got.Domain, tc.wantDomain)
			}
		})
	}
}

// ---------------------------------------------------------------------------------------
// SHIP-10's acceptance criterion: the skeleton exists, and it is the one Docs/06 §3
// describes.

func TestEveryDomainInDocs06HasAPackage(t *testing.T) {
	root, err := ModuleRoot(".")
	if err != nil {
		t.Fatal(err)
	}

	if len(Domains) != 8 {
		t.Errorf("Domains has %d entries, want the eight of Docs/06 §3", len(Domains))
	}

	for _, d := range Domains {
		dir := filepath.Join(root, "internal", d)
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("domain %q has no package at internal/%s: %v", d, d, err)
		}
	}
}

func TestEveryAdapterInDocs06HasAPackage(t *testing.T) {
	root, err := ModuleRoot(".")
	if err != nil {
		t.Fatal(err)
	}

	// The five integrations Docs/06 §4.1 lists as having a second implementation today.
	for _, a := range []string{"email", "sms", "push", "storage", "geocoding"} {
		dir := filepath.Join(root, filepath.FromSlash(AdapterRoot), a)
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("adapter %q has no package at %s/%s: %v", a, AdapterRoot, a, err)
		}
	}
}
