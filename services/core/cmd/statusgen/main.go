// Command statusgen writes the status enumerations of contracts/statuses.yaml into Go, Dart and
// TypeScript (SHIP-56a).
//
//	make codegen
//
// # The decision this program embodies, so nobody has to reconstruct it
//
// Docs/10 §8.2 settled it before the ticket was written: "contracts/statuses.yaml is the source;
// make codegen produces the Go, Dart and TypeScript forms". A neutral specification produces all
// three, rather than Go being the source and the other two being derived from it. Both readings
// satisfy the ticket's "one source produces all three"; only the first satisfies the document,
// and CLAUDE.md is explicit that a contradiction with a document is resolved in the document
// first, never silently in code.
//
// It also happens to be the reading that leaves nothing lying. Under the alternative, Go keeps its
// documentation and Dart and TypeScript get comments derived from Go's — so the argument for why
// nothing writes the Countered bid status would still exist as one paragraph explaining a Go
// constant, and the Dart copy of it that already existed would still have to be deleted by hand.
// Here that paragraph is in the specification and is rendered into all three.
//
// # What is not generated, and why each is deliberate
//
//   - **The transition table** of Docs/02 §2, and bidding's liveness predicate. Decisions rather
//     than names. Docs/07 §3 puts every such decision on the platform, so no second language wants
//     a copy and the reason for generating disappears.
//   - **The SQL CHECK constraints.** Migrations are applied history and cannot be regenerated. The
//     pairing test per enumeration (Docs/10 §3.4) reads the constraint out of pg_constraint and
//     holds it to the generated constants in both directions — unchanged by this ticket, and
//     quietly stronger for it, because what it pairs is now the database and the specification.
//   - **Actor vocabularies.** jobs.ActorType, bidding.Party and Dart's BidParty name a kind of
//     person rather than a lifecycle state. This ticket is scoped to status enumerations; moving
//     them later is an entry in the specification and no new mechanism.
//   - **Behaviour beside a generated enumeration.** Dart's generated file is `<name>.gen.dart` and
//     a hand-written `<name>.dart` exports it, so bidding's isLive and BidParty keep a home. A
//     generator that owned the imported file outright would have deleted both.
//
// # Staleness
//
// The generated files are committed, and TestGeneratedFilesAreCurrent compares them with a fresh
// render. It runs under `go test ./...`, so `make check` and the Go workflow both catch a hand
// edit to a generated file and a specification edit that was never regenerated.
//
// A test rather than the `git diff --exit-code` after regenerating that Docs/10 §8.2 sketches,
// and strictly stronger than it in two ways. It does not rewrite the working tree, so it cannot
// produce the false failure CLAUDE.md records a tree-rewriting gate producing on a tree where
// nothing was wrong; and it fails on a generated file that is missing entirely, which a diff of
// tracked files does not see.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// specPath is where the source lives, relative to the repository root.
const specPath = "contracts/statuses.yaml"

func main() {
	root := flag.String("root", "../..", "the repository root")
	check := flag.Bool("check", false, "report what is stale and write nothing")
	flag.Parse()

	abs, err := filepath.Abs(*root)
	if err != nil {
		fail("resolving -root: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(abs, specPath))
	if err != nil {
		fail("reading %s: %v", specPath, err)
	}

	spec, err := Load(raw)
	if err != nil {
		fail("%v", err)
	}

	stale := 0
	for _, out := range Render(spec) {
		path := filepath.Join(abs, out.Path)

		current, readErr := os.ReadFile(path)
		unchanged := readErr == nil && string(current) == out.Body

		if *check {
			if !unchanged {
				fmt.Fprintf(os.Stderr, "  stale: %s\n", out.Path)
				stale++
			}
			continue
		}

		if unchanged {
			fmt.Printf("  unchanged  %s\n", out.Path)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fail("creating %s: %v", filepath.Dir(out.Path), err)
		}
		if err := os.WriteFile(path, []byte(out.Body), 0o644); err != nil {
			fail("writing %s: %v", out.Path, err)
		}
		fmt.Printf("  wrote      %s\n", out.Path)
	}

	if *check && stale > 0 {
		fail("%d generated file(s) do not match %s. Run `make codegen`.", stale, specPath)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "statusgen: "+format+"\n", args...)
	os.Exit(1)
}
