package main

import (
	"fmt"
	"strings"
)

// dartWidth is the width apps/mobile's own Dart is written at.
const dartWidth = 100

// unknownDoc is the sentinel every generated Dart enumeration carries, and the one part of the
// Dart output that is the generator's opinion rather than the specification's.
//
// It belongs to the language rather than to any one vocabulary. Docs/07 §6 is built on old builds
// living on devices indefinitely: the alternative to decoding a value this build has never heard
// of is throwing on it, and a client that crashes rather than degrades has no over-the-air fix.
// Neither Go nor TypeScript gets one — the service is the thing that defines the vocabulary, and a
// web page is reloaded from the server that served it.
var unknownDoc = []string{
	"A value this build has never heard of.",
	"Not one the platform sends: the contract enumerates exactly the values above. It exists because " +
		"the alternative to decoding an unrecognised one is throwing on it, and Docs/07 §6 is built on old " +
		"builds living on devices indefinitely — a client that crashes rather than degrades has no " +
		"over-the-air fix.",
	"Deliberately last, so that `values` stays in the source document's own order and a screen grouping " +
		"by this enumeration gets that order without writing a second list.",
}

// renderDart produces the Dart form: a json_serializable enum with a wire name and a label.
//
// # Why the generated file is not the file anybody imports
//
// It is `<name>.gen.dart`, and a hand-written `<name>.dart` beside it exports it. The shim costs a
// line and buys the one thing a wholly generated file cannot have: somewhere for behaviour to live.
// bidding's isLive predicate and the BidParty enumeration are in that file today, and a generator
// that owned `bid_status.dart` outright would have deleted both — which is precisely the failure
// mode of generating over hand-written code. Every consumer keeps importing the name it always
// imported.
func renderDart(e Enum) string {
	var b strings.Builder

	for _, l := range comment("// ", append([]string{generatedBy}, regenerateNote...)) {
		b.WriteString(l + "\n")
	}
	b.WriteString("\nimport 'package:json_annotation/json_annotation.dart';\n\n")

	for _, l := range comment("/// ", docLines(capitalised(e.Doc), len(e.Values), dartWidth-4)) {
		b.WriteString(l + "\n")
	}
	fmt.Fprintf(&b, "enum %s {\n", e.Dart.Type)

	for _, v := range e.Values {
		for _, l := range indent("  ", comment("/// ", docLines(capitalised(v.Doc), len(e.Values), dartWidth-6))) {
			b.WriteString(l + "\n")
		}
		fmt.Fprintf(&b, "  @JsonValue('%s')\n  %s,\n\n", v.Wire, v.Camel())
	}

	for _, l := range indent("  ", comment("/// ", docLines(strings.Join(unknownDoc, "\n\n"), len(e.Values), dartWidth-6))) {
		b.WriteString(l + "\n")
	}
	b.WriteString("  unknown;\n\n")

	// wireName. Present for query parameters and for tests that assert against the contract,
	// not because anything renders it.
	wireDoc := []string{
		"What the wire calls this value: lower snake case, per Docs/10 §4.7.",
		"Present for query parameters and for tests that assert against the published contract, not " +
			"because anything renders it.",
	}
	for _, l := range indent("  ", comment("/// ", docLines(strings.Join(wireDoc, "\n\n"), len(e.Values), dartWidth-6))) {
		b.WriteString(l + "\n")
	}
	b.WriteString("  String get wireName => switch (this) {\n")
	for _, v := range e.Values {
		fmt.Fprintf(&b, "        %s.%s => '%s',\n", e.Dart.Type, v.Camel(), v.Wire)
	}
	fmt.Fprintf(&b, "        %s.unknown => 'unknown',\n      };\n\n", e.Dart.Type)

	// label. The exact name the source document uses, which CLAUDE.md requires of user-facing
	// copy: a customer reading it on a screen and support reading it in an audit entry have to be
	// reading about the same thing.
	labelDoc := []string{
		fmt.Sprintf("The value as a person reads it — the exact name %s uses.", e.Authority),
		"Not free-form copy. CLAUDE.md requires the document's own names, because a customer reading one " +
			"on a screen and support reading it in an audit entry have to be reading about the same thing.",
	}
	for _, l := range indent("  ", comment("/// ", docLines(strings.Join(labelDoc, "\n\n"), len(e.Values), dartWidth-6))) {
		b.WriteString(l + "\n")
	}
	b.WriteString("  String get label => switch (this) {\n")
	for _, v := range e.Values {
		fmt.Fprintf(&b, "        %s.%s => '%s',\n", e.Dart.Type, v.Camel(), dartString(v.Stored))
	}
	b.WriteString("\n")
	for _, l := range indent("        ", comment("// ", wrap(
		"Deliberately not \"Unknown\", which reads as a fault. The record is fine; this build is old, "+
			"and saying which is the difference between an update and a support call.", dartWidth-14))) {
		b.WriteString(l + "\n")
	}
	fmt.Fprintf(&b, "        %s.unknown => 'Not shown by this version',\n      };\n}\n", e.Dart.Type)

	return b.String()
}

// dartString escapes a value for a single-quoted Dart literal.
//
// No stored form needs it today. It is here because the day one does — an apostrophe in a status
// name is not far-fetched — the failure would be a client that does not compile, found by CI on a
// change to a YAML file, which is a long way from the cause.
func dartString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `'`, `\'`)
}
