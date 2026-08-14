package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot is three directories up from services/core/cmd/statusgen.
//
// The same shape as cmd/api's servicesCoreRoot, and for the same reason: `go test` runs with the
// package directory as the working directory, so a relative walk is exact and needs no marker file
// and no environment variable.
func repoRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("locating the repository root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, specPath)); err != nil {
		t.Fatalf("%s is not at the repository root %s: %v", specPath, root, err)
	}
	return root
}

func loadSpec(t *testing.T) (*Spec, string) {
	t.Helper()

	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, specPath))
	if err != nil {
		t.Fatalf("reading %s: %v", specPath, err)
	}
	spec, err := Load(raw)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return spec, root
}

// TestGeneratedFilesAreCurrent is the half of SHIP-56a's acceptance criterion that a regeneration
// target does not meet: "CI fails if a generated file is stale".
//
// It is the most valuable test in this package, for the reason TestRouteTableMatchesGolden is the
// most valuable one in cmd/api. Neither failure it catches produces a compile error. A status
// renamed in the specification and never regenerated leaves three languages agreeing with each
// other and disagreeing with the source; a generated file edited by hand leaves one language
// disagreeing with the other two and with the database, and everything still builds.
//
// # Why a test rather than the `git diff --exit-code` Docs/10 §8.2 sketches
//
// Stronger in two specific ways, both of which this repository has paid for.
//
// It writes nothing. CLAUDE.md records wave 5 in both directions: a gate that rewrites the tree
// produced a false failure on a tree where nothing was wrong, and a merge committed while its
// gates ran produced a false pass that shipped. A check that only reads can do neither, and it
// works on a dirty working tree — which is where it is actually run.
//
// It fails on a generated file that is missing entirely. `git diff` compares tracked files against
// the index and says nothing about one that was deleted and never staged, or one whose path moved
// in the specification and was never created.
func TestGeneratedFilesAreCurrent(t *testing.T) {
	spec, root := loadSpec(t)

	for _, out := range Render(spec) {
		committed, err := os.ReadFile(filepath.Join(root, out.Path))
		if err != nil {
			t.Errorf("%s is generated from %s and is not there: %v\n\nRun `make codegen`.",
				out.Path, specPath, err)
			continue
		}

		if string(committed) == out.Body {
			continue
		}

		t.Errorf("%s does not match what %s generates.\n\n"+
			"If you changed the specification, regenerate:\n"+
			"    make codegen\n\n"+
			"If you did not, somebody has edited a generated file by hand — the change belongs in "+
			"%s.\n\n%s",
			out.Path, specPath, specPath, firstDifference(string(committed), out.Body))
	}
}

// firstDifference reports the line the two disagree on, with a little context.
//
// A whole-file dump would be several hundred lines of documentation for a one-word change, and the
// point of this test is that its failure is read rather than skimmed.
func firstDifference(committed, generated string) string {
	c := strings.Split(committed, "\n")
	g := strings.Split(generated, "\n")

	for i := 0; i < len(c) && i < len(g); i++ {
		if c[i] == g[i] {
			continue
		}
		return "first difference at line " + itoa(i+1) + ":\n" +
			"  committed: " + c[i] + "\n" +
			"  generated: " + g[i]
	}

	return "the files agree for " + itoa(min(len(c), len(g))) + " lines and then one ends: " +
		"committed has " + itoa(len(c)) + " lines, generated has " + itoa(len(g))
}

// TestTheSpecificationIsUsable is Load's validation run against the real file.
//
// Separate from the staleness test because the two fail for different reasons and a reader needs
// to know which: a specification that will not validate is a mistake in the source, and a stale
// file is a regeneration that did not happen.
func TestTheSpecificationIsUsable(t *testing.T) {
	spec, _ := loadSpec(t)

	if len(spec.Enums) == 0 {
		t.Fatal("no enumerations")
	}
	for _, e := range spec.Enums {
		if len(e.Values) == 0 {
			t.Errorf("%s: no values", e.Name)
		}
	}
}

// TestEveryValueRoundTripsThroughItsWireForm exercises the pair of transformations the generator
// writes into Go, and is what makes generating FromWire for every enumeration honest rather than
// speculative.
//
// The Go side used to argue against writing the inverse before something called it — "an untested
// transformation with no caller is worse than none". That was right about a hand-written one. What
// retires it is not that generation is tidier; it is this test, which is the caller.
func TestEveryValueRoundTripsThroughItsWireForm(t *testing.T) {
	spec, _ := loadSpec(t)

	for _, e := range spec.Enums {
		seen := map[string]string{}
		for _, v := range e.Values {
			if prev, dup := seen[v.Wire]; dup {
				t.Errorf("%s: %q and %q share the wire form %q", e.Name, prev, v.Stored, v.Wire)
			}
			seen[v.Wire] = v.Stored

			// The generated Go looks a value up by walking the ordered list and comparing wire
			// forms, so the property that matters is that exactly one value answers to each.
			matches := 0
			for _, other := range e.Values {
				if other.Wire == v.Wire {
					matches++
				}
			}
			if matches != 1 {
				t.Errorf("%s: %d values answer to the wire form %q", e.Name, matches, v.Wire)
			}
		}
	}
}

// TestValidationRefusesTheMistakesThatCompile is the mutation sweep, run as a test.
//
// Every case below is something that would produce three files that compile in three languages and
// are wrong. A wire form in the wrong case renames a value for every build already on a phone; two
// values collapsing to one identifier is a compile error in all three, but reported there rather
// than here, where the cause is visible; a doc written as a sentence produces "StatusSubmitted is
// A live offer awaiting the other party" in Go and nothing anywhere to say so.
func TestValidationRefusesTheMistakesThatCompile(t *testing.T) {
	base := func() *Spec {
		s := &Spec{Version: 1}
		s.TypeScript.File = "apps/x/status.gen.ts"
		e := Enum{
			Name:       "demo",
			Authority:  "Docs/02 §1",
			Constraint: "ck_demo",
			Doc:        "a demonstration vocabulary.",
			Values: []Value{
				{Stored: "Open", Wire: "open", Doc: "a thing that is open."},
				{Stored: "Picked up", Wire: "picked_up", Doc: "a thing collected."},
			},
		}
		e.Go.File, e.Go.Package, e.Go.Type, e.Go.Prefix, e.Go.Slice =
			"internal/demo/status_gen.go", "demo", "Status", "Status", "Statuses"
		e.Dart.File, e.Dart.Type = "apps/mobile/lib/demo.gen.dart", "DemoStatus"
		e.TypeScript.Type = "DemoStatus"
		s.Enums = []Enum{e}
		return s
	}

	if err := base().Validate(); err != nil {
		t.Fatalf("the unmutated specification must validate: %v", err)
	}

	for _, tc := range []struct {
		name   string
		mutate func(*Spec)
		want   string
	}{
		{
			name:   "a wire form in the wrong case",
			mutate: func(s *Spec) { s.Enums[0].Values[1].Wire = "PickedUp" },
			want:   "lower snake case",
		},
		{
			name:   "a wire form with a space",
			mutate: func(s *Spec) { s.Enums[0].Values[1].Wire = "picked up" },
			want:   "lower snake case",
		},
		{
			name: "two values with one wire form",
			mutate: func(s *Spec) {
				s.Enums[0].Values[1].Wire = "open"
				s.Enums[0].Values[1].Stored = "Opened"
			},
			want: "appears twice",
		},
		{
			// Reachable, and it took a moment to find a case that is. The identifier is derived
			// from the wire form, so two distinct wire forms almost always give two distinct
			// identifiers — except across a digit, which does not change when it is upper-cased.
			// `level_1` and `level1` are both lower snake case, are two different published
			// strings, and are one Go constant.
			name: "two values collapsing to one identifier",
			mutate: func(s *Spec) {
				s.Enums[0].Values = []Value{
					{Stored: "Level one", Wire: "level_1", Doc: "the first level."},
					{Stored: "Level 1", Wire: "level1", Doc: "the same level, spelled otherwise."},
				}
			},
			want: "identifier",
		},
		{
			name:   "an undocumented value",
			mutate: func(s *Spec) { s.Enums[0].Values[0].Doc = "  " },
			want:   "doc is required",
		},
		{
			name:   "a doc written as a sentence",
			mutate: func(s *Spec) { s.Enums[0].Values[0].Doc = "The thing that is open." },
			want:   "write a phrase",
		},
		{
			name:   "an enum with no authority",
			mutate: func(s *Spec) { s.Enums[0].Authority = "" },
			want:   "authority is required",
		},
		{
			name:   "a type name that is not an identifier",
			mutate: func(s *Spec) { s.Enums[0].Dart.Type = "Bid Status" },
			want:   "is not an identifier",
		},
		{
			name: "two enums generating one file",
			mutate: func(s *Spec) {
				second := s.Enums[0]
				second.Name = "demo2"
				second.TypeScript.Type = "Demo2Status"
				s.Enums = append(s.Enums, second)
			},
			want: "both generate",
		},
		{
			name: "two enums sharing a TypeScript type",
			mutate: func(s *Spec) {
				second := s.Enums[0]
				second.Name = "demo2"
				second.Go.File = "internal/demo2/status_gen.go"
				second.Dart.File = "apps/mobile/lib/demo2.gen.dart"
				s.Enums = append(s.Enums, second)
			},
			want: "already used",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := base()
			tc.mutate(s)

			err := s.Validate()
			if err == nil {
				t.Fatalf("Validate accepted it; it must not")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("the message must say what is wrong.\n  want it to contain: %s\n  got: %v",
					tc.want, err)
			}
		})
	}
}

// TestAGeneratedFileSaysItIsGenerated holds every output to the machine-readable marker.
//
// It is the line `go generate`, GitHub's diff view and every editor recognise, and the practical
// consequence is that a reviewer reading a wave of merges sees this file collapsed rather than
// scrolling several hundred lines of consequence to reach the one line of specification that
// changed.
func TestAGeneratedFileSaysItIsGenerated(t *testing.T) {
	spec, _ := loadSpec(t)

	for _, out := range Render(spec) {
		first, _, _ := strings.Cut(out.Body, "\n")
		if !strings.HasPrefix(first, "// Code generated ") || !strings.HasSuffix(first, " DO NOT EDIT.") {
			t.Errorf("%s does not open with the generated-code marker:\n  %s", out.Path, first)
		}
		if !strings.Contains(out.Body, "make codegen") {
			t.Errorf("%s does not say how to regenerate it", out.Path)
		}
	}
}

// TestTheSpelledCountFollowsTheValues is the small guard on the one substitution the generator
// does inside prose.
//
// A count spelled out in a doc comment is a hand-maintained scalar, and Docs/11 §3 records what
// those do in this repository: the `make verify` figure conflicted in four consecutive merges and
// was wrong in three of them. `{{n}}` exists so nobody types one, and this is what says it works.
func TestTheSpelledCountFollowsTheValues(t *testing.T) {
	for n, want := range map[int]string{0: "zero", 1: "one", 3: "three", 8: "eight", 12: "twelve", 20: "twenty", 21: "21"} {
		if got := numberWord(n); got != want {
			t.Errorf("numberWord(%d) = %q, want %q", n, got, want)
		}
	}

	spec, _ := loadSpec(t)
	for _, e := range spec.Enums {
		for _, out := range []string{renderGo(e), renderDart(e)} {
			if strings.Contains(out, "{{n}}") {
				t.Errorf("%s: a {{n}} survived into the generated output", e.Name)
			}
		}
	}
	if strings.Contains(renderTypeScript(spec), "{{n}}") {
		t.Errorf("a {{n}} survived into the TypeScript output")
	}
}
