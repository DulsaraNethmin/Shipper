package main

import (
	"fmt"
	"go/format"
	"strings"
)

// goWidth is the comment width the rest of this repository's Go is written at.
const goWidth = 96

// renderGo produces the Go form: the type, its values, the ordered list, and the two
// transformations between the stored form and the wire form.
//
// # What is here and what is deliberately left in the domain
//
// This is the vocabulary and nothing else. The transition table of Docs/02 §2 and bidding's
// liveness predicate stay hand-written in their packages, because they are decisions rather than
// names — and because Docs/07 §3 puts every such decision on the platform, so no second language
// wants a copy and the reason for generating disappears.
//
// The SQL CHECK constraint is likewise not generated. Migrations are applied history and cannot
// be regenerated; the pairing test per enumeration (Docs/10 §3.4) reads the constraint out of
// pg_constraint and holds it to the list below in both directions. That test is unchanged by
// generation and quietly stronger for it: what it pairs is now the database and the
// specification.
func renderGo(e Enum) string {
	var b strings.Builder

	for _, l := range comment("// ", append([]string{generatedBy}, regenerateNote...)) {
		b.WriteString(l + "\n")
	}
	fmt.Fprintf(&b, "\npackage %s\n\nimport \"slices\"\n\n", e.Go.Package)

	// The type, carrying the specification's own documentation.
	for _, l := range comment("// ", docLines(withLead(e.Doc, e.Go.Type+" is "), len(e.Values), goWidth-3)) {
		b.WriteString(l + "\n")
	}
	fmt.Fprintf(&b, "type %s string\n\nconst (\n", e.Go.Type)

	for i, v := range e.Values {
		if i > 0 {
			b.WriteString("\n")
		}
		doc := withLead(v.Doc, e.GoConst(v)+" is ")
		for _, l := range comment("\t// ", docLines(doc, len(e.Values), goWidth-6)) {
			b.WriteString(l + "\n")
		}
		fmt.Fprintf(&b, "\t%s %s = %q\n", e.GoConst(v), e.Go.Type, v.Stored)
	}
	b.WriteString(")\n\n")

	// The ordered list, which is what the pairing test compares against the CHECK constraint.
	sliceDoc := []string{
		fmt.Sprintf("%s is every value, in the order %s lists them.", e.Go.Slice, e.Authority),
		fmt.Sprintf("Ordered rather than a set because that order is the document's own, and because it is what "+
			"the pairing test in services/core/migrations compares against %s. Docs/10 §3.4 requires that "+
			"pairing for every enumeration and generation does not replace it: this list and the constraint "+
			"are written in different languages, and only a test can hold the two together.", e.Constraint),
	}
	for _, l := range comment("// ", docLines(strings.Join(sliceDoc, "\n\n"), len(e.Values), goWidth-3)) {
		b.WriteString(l + "\n")
	}
	fmt.Fprintf(&b, "var %s = []%s{\n", e.Go.Slice, e.Go.Type)
	for _, v := range e.Values {
		fmt.Fprintf(&b, "\t%s,\n", e.GoConst(v))
	}
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, "// Valid reports whether v is one of the %s above.\n", numberWord(len(e.Values)))
	fmt.Fprintf(&b, "func (v %s) Valid() bool { return slices.Contains(%s, v) }\n\n", e.Go.Type, e.Go.Slice)

	fmt.Fprintf(&b, "// String is the stored form — %s's own string, which is what the database holds.\n",
		e.Authority)
	fmt.Fprintf(&b, "func (v %s) String() string { return string(v) }\n\n", e.Go.Type)

	wireDoc := []string{
		"Wire is the value as it appears in a response body: lower snake case, per Docs/10 §4.7.",
		"Tabulated rather than derived from the stored form, and that is a change generation earned. " +
			"A hand-written table beside the constants is a second list that can disagree with the first, " +
			"which is why this used to be a transformation; a table produced from the same source as the " +
			"constants cannot disagree with them, and it keeps the published string visible beside the " +
			"value it belongs to rather than behind a rule.",
		"The empty string for an unrecognised value is unreachable through Valid and is not a wire form " +
			"anything may send.",
	}
	for _, l := range comment("// ", docLines(strings.Join(wireDoc, "\n\n"), len(e.Values), goWidth-3)) {
		b.WriteString(l + "\n")
	}
	fmt.Fprintf(&b, "func (v %s) Wire() string {\n\tswitch v {\n", e.Go.Type)
	for _, v := range e.Values {
		fmt.Fprintf(&b, "\tcase %s:\n\t\treturn %q\n", e.GoConst(v), v.Wire)
	}
	b.WriteString("\t}\n\treturn \"\"\n}\n\n")

	fromWire := e.Go.Type + "FromWire"
	fromDoc := []string{
		fromWire + " is Wire read backwards: the value a client named, or false.",
		"Generated for every enumeration rather than for the ones with a caller today. The argument " +
			"against writing it early was that an untested transformation with no caller is worse than " +
			"none — which was right about a hand-written one and does not survive generation, since this " +
			"is produced from the same source as Wire and round-tripped against it by the generator's own " +
			"tests.",
	}
	for _, l := range comment("// ", docLines(strings.Join(fromDoc, "\n\n"), len(e.Values), goWidth-3)) {
		b.WriteString(l + "\n")
	}
	fmt.Fprintf(&b, "func %s(wire string) (%s, bool) {\n", fromWire, e.Go.Type)
	fmt.Fprintf(&b, "\tfor _, known := range %s {\n\t\tif known.Wire() == wire {\n"+
		"\t\t\treturn known, true\n\t\t}\n\t}\n\treturn \"\", false\n}\n", e.Go.Slice)

	src, err := format.Source([]byte(b.String()))
	if err != nil {
		// Unreachable for a valid specification, and worth being loud about rather than
		// writing unformatted Go that gofmt would then flag somewhere else entirely.
		panic(fmt.Sprintf("statusgen produced Go that does not parse for %s: %v", e.Name, err))
	}
	return string(src)
}
