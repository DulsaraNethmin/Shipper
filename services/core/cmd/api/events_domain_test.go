package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/boundaries"
)

// SHIP-136's *Done when* has two halves and only one of them is about events existing: "every state
// change in Docs/01 §4.5 emits its event **from the domain, not the API layer**."
//
// The first half is demonstrated in each domain's own events_test.go, against the outbox table, and
// by `make verify` against the running service. **This is the second half, and nothing else in the
// build checks it.** An event emitted from a handler passes every one of those tests: the row is
// written, the payload is right, it is on the topic. What is wrong with it is structural — the
// handler is one caller of the domain, so an event emitted there is an event the next caller does
// not emit, and the next caller is a scheduled task, an administrator's endpoint, or a second
// domain reaching in through a port.
//
// # Why it lives in cmd/api and is written as a source scan
//
// Here for the reason events_golden.txt is here: cmd/api is the one binary that links every domain,
// so it is the only place the whole surface exists at once. A copy of this in each domain would be
// three tests that each cover a third of the rule, and the domain that most needs it is the one
// somebody adds next.
//
// A source scan rather than a runtime check, for the reason internal/jobs' budget guard is one:
// there is nothing to observe at runtime. Both spellings produce an identical outbox row.
//
// # What it cannot do, said plainly
//
// It matches a call by its name — `events.New` and `.Emit` — so a handler that reached the outbox
// through an alias or an indirection would pass. That is the same limit SHIP-83 found in the budget
// source guard, and the same answer applies: this is the axis that catches the realistic mistake
// (somebody writes the emit where they are already working, which is the handler), and the domain
// tests are the axis that catches the rest.

// eventsAllowed is where an event may be built or written, and why.
//
// An allow-list rather than a rule about directory names, on the same reasoning as
// internal/jobs' budgetBearers and cmd/api's publicMutatingRoutes: the safe set is small, closed,
// and each entry has an argument behind it, so widening it is a deliberate act somebody reviews.
//
// The key is a path relative to services/core.
var eventsAllowed = map[string]string{
	"internal/events": "the seam itself — the catalogue, the writer, and the checks that keep a " +
		"permanently unpublishable row out of the table (SHIP-135)",

	"internal/jobs":     "the guarded transition and the two expiry tasks (SHIP-57, SHIP-69, SHIP-70)",
	"internal/bidding":  "the bid lifecycle (SHIP-136)",
	"internal/delivery": "the delivery record (SHIP-136)",
}

// forbiddenInDomainFiles is the file a domain's handlers live in.
//
// Docs/10 §4 puts handlers in the domain, in http.go, which is what makes "the domain emits it"
// harder to check than it looks: an emit written there is inside the domain package and is still
// the API layer. So the rule is by file as well as by package.
const forbiddenInDomainFiles = "http.go"

// TestOnlyADomainEmitsADomainEvent walks every Go file in the service and fails on an event built
// or written outside [eventsAllowed] — or inside one of them but in its handler file.
func TestOnlyADomainEmitsADomainEvent(t *testing.T) {
	root := servicesCoreRoot(t)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir():
			return nil
		case !strings.HasSuffix(path, ".go"):
			return nil
		case strings.HasSuffix(path, "_test.go"):
			// A test may arrange whatever it likes; what it cannot do is put an emit into the
			// served path, which is what this is about.
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)

		calls := eventCallsIn(t, path)
		if len(calls) == 0 {
			return nil
		}

		dir := filepath.ToSlash(filepath.Dir(rel))
		reason, permitted := eventsAllowed[dir]
		if !permitted {
			t.Errorf("%s calls %s, and %s is not somewhere an event may be emitted.\n"+
				"CLAUDE.md: domain events are emitted by the domain, not the API layer. A handler "+
				"is one caller of the domain; an event emitted there is one the next caller does "+
				"not emit.", rel, strings.Join(calls, " and "), dir)
			return nil
		}

		if filepath.Base(rel) == forbiddenInDomainFiles {
			t.Errorf("%s calls %s. %s is the handler file (Docs/10 §4), so this is the API layer "+
				"inside the domain package — which is exactly the shape the rule is about. Move it "+
				"beside the state change it describes.\n(%s is permitted to emit because it is %s.)",
				rel, strings.Join(calls, " and "), forbiddenInDomainFiles, dir, reason)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
}

// TestEveryDomainThatEmitsIsADomain keeps [eventsAllowed] honest against internal/boundaries.
//
// Without it the allow-list is a second, hand-maintained opinion about what a domain is, and the
// way it would go wrong is by someone adding an infrastructure package to it to unblock something.
// Infrastructure imports neither a domain nor an adapter (SHIP-15c) and has no state change to
// describe, so an event emitted from one would be an event with no owner.
func TestEveryDomainThatEmitsIsADomain(t *testing.T) {
	for dir := range eventsAllowed {
		pkg := strings.TrimPrefix(dir, "internal/")
		if pkg == "events" {
			continue
		}
		if !isDomain(pkg) {
			t.Errorf("%s is in the emit allow-list and internal/boundaries does not call it a "+
				"domain. An event describes a state change, and infrastructure has none to "+
				"describe.", dir)
		}
	}
}

func isDomain(pkg string) bool {
	for _, d := range boundaries.Domains {
		if d == pkg {
			return true
		}
	}
	return false
}

// eventCallsIn reports the event-emitting calls a file makes, by name.
//
// Two spellings, because they are the two ends of the seam and either alone is incomplete:
// `events.New` builds one and `Emit` writes it. A handler that built the event and handed it to
// the domain to write would still be the API layer deciding what an event says.
func eventCallsIn(t *testing.T, path string) []string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	found := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		switch {
		case sel.Sel.Name == "Emit":
			found["Emit"] = true
		case sel.Sel.Name == "New":
			if pkg, ok := sel.X.(*ast.Ident); ok && pkg.Name == "events" {
				found["events.New"] = true
			}
		}
		return true
	})

	var calls []string
	for _, name := range []string{"events.New", "Emit"} {
		if found[name] {
			calls = append(calls, name)
		}
	}
	return calls
}

// servicesCoreRoot is the module root, found by walking up from this package rather than assumed.
func servicesCoreRoot(t *testing.T) string {
	t.Helper()

	dir, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("locating the module root: %v", err)
	}
	return filepath.Dir(dir)
}
