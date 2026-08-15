package main

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Spec is contracts/statuses.yaml.
//
// Every output path is in the specification rather than in this program, which is what makes
// adding a status enumeration a single-file edit: whoever adds one says where the three files go
// and runs `make codegen`. A table of paths compiled into the generator would be a second place
// to edit, and the second place is the one people forget.
type Spec struct {
	Version int `yaml:"version"`

	// TypeScript is one module for every enumeration, unlike Go and Dart. The reason is in the
	// specification's own header: TypeScript has no equivalent of a Go package or a Flutter
	// feature folder to put a single enumeration in.
	TypeScript struct {
		File string `yaml:"file"`
	} `yaml:"typescript"`

	Enums []Enum `yaml:"enums"`
}

// Enum is one vocabulary, and the three shapes it takes.
type Enum struct {
	Name string `yaml:"name"`

	// Authority is the document that fixes these names, quoted in the generated documentation
	// so a reader of any of the three languages can go and check.
	Authority string `yaml:"authority"`

	// Constraint is the SQL CHECK holding the same list, named for the same reason.
	//
	// **It is documentation and not an instruction.** Nothing here generates SQL: migrations are
	// applied history and cannot be regenerated, so the constraint stays hand-written and a test
	// per enumeration reads it out of pg_constraint and holds it to the Go constants in both
	// directions (Docs/10 §3.4). Those tests did not change when generation arrived, and they got
	// stronger for free — the constants they compare against are now this file, so what they
	// actually pair is the database and the specification.
	Constraint string `yaml:"constraint"`

	Doc string `yaml:"doc"`

	Go struct {
		File    string `yaml:"file"`
		Package string `yaml:"package"`
		Type    string `yaml:"type"`
		Prefix  string `yaml:"prefix"`
		Slice   string `yaml:"slice"`
	} `yaml:"go"`

	Dart struct {
		File string `yaml:"file"`
		Type string `yaml:"type"`
	} `yaml:"dart"`

	TypeScript struct {
		Type string `yaml:"type"`
	} `yaml:"typescript"`

	Values []Value `yaml:"values"`
}

// Value is one member: what the database holds, what a client sees, and why it exists.
type Value struct {
	Stored string `yaml:"stored"`
	Wire   string `yaml:"wire"`
	Doc    string `yaml:"doc"`
}

// Pascal is the identifier form, derived from Wire: driver_assigned becomes DriverAssigned.
//
// Derived rather than written down because an identifier is not a published contract. Nobody
// outside this repository can see it, so there is nothing to hold stable and no reason to spell
// it three times.
func (v Value) Pascal() string {
	parts := strings.Split(v.Wire, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// Camel is Dart's identifier form: driver_assigned becomes driverAssigned.
func (v Value) Camel() string {
	p := v.Pascal()
	if p == "" {
		return p
	}
	return strings.ToLower(p[:1]) + p[1:]
}

// GoConst is the Go constant: the enumeration's prefix and the value's identifier.
func (e Enum) GoConst(v Value) string { return e.Go.Prefix + v.Pascal() }

// wirePattern is Docs/10 §4.7's shape, enforced rather than assumed.
//
// The rule is worth checking mechanically because the wire form is the one thing here a client
// has already shipped against: a value that reached this file as `DriverAssigned` would compile
// in all three languages and break every installed build.
var wirePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// identPattern is the shape of a type or constant name in all three languages at once.
var identPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)

// openingPhrase checks that a doc block opens with a predicate phrase rather than a sentence.
//
// Every doc here is rendered into three languages with two different conventions: Go opens a
// comment with the identifier it documents ("StatusSubmitted is a live offer awaiting the other
// party"), and Dart and TypeScript want a sentence ("A live offer awaiting the other party"). One
// phrase written lower case serves both — Go prefixes it, the other two capitalise it — and a
// sentence serves neither, producing "StatusSubmitted is A live offer awaiting the other party".
//
// A name keeps its capital: "Docs/01 §4.4's first: the recipient objects to being photographed"
// is a phrase and lower-casing it would name a different document. The test for a name is an
// upper-case letter or a non-letter anywhere after the first character, which separates "Docs/01"
// and "CLAUDE.md" from "The" and "Published".
func openingPhrase(doc string) error {
	fields := strings.Fields(doc)
	if len(fields) == 0 {
		return fmt.Errorf("empty")
	}

	word := fields[0]
	if word[0] < 'A' || word[0] > 'Z' {
		return nil
	}
	for i := 1; i < len(word); i++ {
		c := word[i]
		if c < 'a' || c > 'z' {
			return nil // an upper-case letter or a non-letter: a name, left alone
		}
	}
	return fmt.Errorf("opens with the sentence %q; write a phrase Go can prefix with "+
		"\"<Identifier> is \" — lower case unless the first word is a name", word)
}

// Validate refuses a specification that would generate something wrong, and says which entry.
//
// **Missing documentation is a validation failure, not a warning.** The reasoning behind a status
// is the part that was genuinely duplicated before this file existed — the argument for why
// nothing writes Countered lived as two paragraphs in two languages, written twice by two people.
// A generator that let a value through undocumented would collect exactly the entries whose
// reasoning nobody could reconstruct later.
func (s *Spec) Validate() error {
	if s.Version != 1 {
		return fmt.Errorf("version: want 1, got %d", s.Version)
	}
	if s.TypeScript.File == "" {
		return fmt.Errorf("typescript.file is required")
	}
	if len(s.Enums) == 0 {
		return fmt.Errorf("enums: none")
	}

	seenEnum := map[string]bool{}
	seenFile := map[string]string{}
	seenTSType := map[string]bool{}

	for _, e := range s.Enums {
		if e.Name == "" {
			return fmt.Errorf("an enum has no name")
		}
		if seenEnum[e.Name] {
			return fmt.Errorf("%s: declared twice", e.Name)
		}
		seenEnum[e.Name] = true

		for _, f := range []struct{ what, value string }{
			{"authority", e.Authority},
			{"constraint", e.Constraint},
			{"doc", e.Doc},
			{"go.file", e.Go.File},
			{"go.package", e.Go.Package},
			{"go.type", e.Go.Type},
			{"go.prefix", e.Go.Prefix},
			{"go.slice", e.Go.Slice},
			{"dart.file", e.Dart.File},
			{"dart.type", e.Dart.Type},
			{"typescript.type", e.TypeScript.Type},
		} {
			if strings.TrimSpace(f.value) == "" {
				return fmt.Errorf("%s: %s is required", e.Name, f.what)
			}
		}

		for _, f := range []struct{ what, value string }{
			{"go.type", e.Go.Type},
			{"go.prefix", e.Go.Prefix},
			{"go.slice", e.Go.Slice},
			{"dart.type", e.Dart.Type},
			{"typescript.type", e.TypeScript.Type},
		} {
			if !identPattern.MatchString(f.value) {
				return fmt.Errorf("%s: %s %q is not an identifier", e.Name, f.what, f.value)
			}
		}

		// Two enumerations writing the same file would leave whichever ran second, silently.
		for _, f := range []string{e.Go.File, e.Dart.File} {
			if prev, ok := seenFile[f]; ok {
				return fmt.Errorf("%s and %s both generate %s", prev, e.Name, f)
			}
			seenFile[f] = e.Name
		}

		// The TypeScript module holds every enumeration, so its type names share one scope.
		if seenTSType[e.TypeScript.Type] {
			return fmt.Errorf("%s: typescript.type %s is already used by another enum in the same module",
				e.Name, e.TypeScript.Type)
		}
		seenTSType[e.TypeScript.Type] = true

		if err := openingPhrase(e.Doc); err != nil {
			return fmt.Errorf("%s: doc %w", e.Name, err)
		}

		if len(e.Values) == 0 {
			return fmt.Errorf("%s: no values", e.Name)
		}

		seenStored := map[string]bool{}
		seenWire := map[string]bool{}
		seenIdent := map[string]bool{}

		for _, v := range e.Values {
			switch {
			case strings.TrimSpace(v.Stored) == "":
				return fmt.Errorf("%s: a value has no stored form", e.Name)
			case strings.TrimSpace(v.Doc) == "":
				return fmt.Errorf("%s/%s: doc is required", e.Name, v.Stored)
			case !wirePattern.MatchString(v.Wire):
				return fmt.Errorf("%s/%s: wire %q is not lower snake case (Docs/10 §4.7)",
					e.Name, v.Stored, v.Wire)
			case seenStored[v.Stored]:
				return fmt.Errorf("%s: stored %q appears twice", e.Name, v.Stored)
			case seenWire[v.Wire]:
				return fmt.Errorf("%s: wire %q appears twice", e.Name, v.Wire)
			}

			if err := openingPhrase(v.Doc); err != nil {
				return fmt.Errorf("%s/%s: doc %w", e.Name, v.Stored, err)
			}

			// Two stored forms can differ and still collapse to one identifier — "Picked up"
			// and "Picked Up" would — which compiles in none of the three languages and is a
			// considerably clearer message here than there.
			if seenIdent[v.Pascal()] {
				return fmt.Errorf("%s: %q and an earlier value both produce the identifier %s",
					e.Name, v.Stored, v.Pascal())
			}

			seenStored[v.Stored] = true
			seenWire[v.Wire] = true
			seenIdent[v.Pascal()] = true
		}
	}

	return nil
}

// Load reads and validates the specification.
func Load(raw []byte) (*Spec, error) {
	var s Spec
	if err := yaml.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parsing the specification: %w", err)
	}
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("the specification is not usable: %w", err)
	}
	return &s, nil
}
