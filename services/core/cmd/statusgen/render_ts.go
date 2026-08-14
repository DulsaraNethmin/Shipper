package main

import (
	"fmt"
	"strings"
)

// tsWidth is the width apps/driver-portal's own TypeScript is written at.
const tsWidth = 100

// renderTypeScript produces one module holding every enumeration.
//
// # A const object and a union, not an `enum`
//
// TypeScript's `enum` emits a runtime object with a reverse mapping, is not erasable, and is the
// one construct `isolatedModules` and Node's type-stripping both refuse — and the driver portal
// runs its tests as `node --test` over the TypeScript directly, with no build step. A frozen const
// object plus a union type of its values is the idiom that survives all of that, and it gives a
// value whose type is the wire string rather than a number.
//
// # It has no consumer yet, and that is the point
//
// Neither web surface has a status module today. Generating one before a screen needs it is what
// makes the next screen a use rather than a decision: whoever writes it gets the vocabulary, the
// labels and the type guard already agreeing with the service and the phone.
func renderTypeScript(s *Spec) string {
	var b strings.Builder

	for _, l := range comment("// ", append([]string{generatedBy}, regenerateNote...)) {
		b.WriteString(l + "\n")
	}

	for _, e := range s.Enums {
		b.WriteString("\n")
		b.WriteString(jsdoc("", docLines(capitalised(e.Doc), len(e.Values), tsWidth-3)))

		fmt.Fprintf(&b, "export const %s = {\n", e.TypeScript.Type)
		for i, v := range e.Values {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(jsdoc("  ", docLines(capitalised(v.Doc), len(e.Values), tsWidth-5)))
			fmt.Fprintf(&b, "  %s: %q,\n", v.Pascal(), v.Wire)
		}
		b.WriteString("} as const;\n\n")

		typeDoc := []string{
			fmt.Sprintf("A value of %s: the wire string, not a numeric member.", e.TypeScript.Type),
			"The union is what makes an unhandled value a type error in a switch, which is the whole " +
				"reason this file is generated rather than typed as a string.",
		}
		b.WriteString(jsdoc("", docLines(strings.Join(typeDoc, "\n\n"), len(e.Values), tsWidth-3)))
		fmt.Fprintf(&b, "export type %s = (typeof %s)[keyof typeof %s];\n\n",
			e.TypeScript.Type, e.TypeScript.Type, e.TypeScript.Type)

		values := camel(e.TypeScript.Type) + "Values"
		b.WriteString(jsdoc("", docLines(
			fmt.Sprintf("Every value, in the order %s lists them — which is the order to offer or group them in.",
				e.Authority), len(e.Values), tsWidth-3)))
		fmt.Fprintf(&b, "export const %s: readonly %s[] = [\n", values, e.TypeScript.Type)
		for _, v := range e.Values {
			fmt.Fprintf(&b, "  %s.%s,\n", e.TypeScript.Type, v.Pascal())
		}
		b.WriteString("];\n\n")

		labels := camel(e.TypeScript.Type) + "Labels"
		labelDoc := []string{
			fmt.Sprintf("The value as a person reads it — the exact name %s uses.", e.Authority),
			"Not free-form copy. CLAUDE.md requires the document's own names, because a driver reading one " +
				"on a page and support reading it in an audit entry have to be reading about the same thing.",
		}
		b.WriteString(jsdoc("", docLines(strings.Join(labelDoc, "\n\n"), len(e.Values), tsWidth-3)))
		fmt.Fprintf(&b, "export const %s: Readonly<Record<%s, string>> = {\n", labels, e.TypeScript.Type)
		for _, v := range e.Values {
			fmt.Fprintf(&b, "  [%s.%s]: %q,\n", e.TypeScript.Type, v.Pascal(), v.Stored)
		}
		b.WriteString("};\n\n")

		guard := "is" + e.TypeScript.Type
		guardDoc := []string{
			fmt.Sprintf("Whether an arbitrary string is one of them, narrowing it to %s when it is.",
				e.TypeScript.Type),
			"Needed because everything arriving from the API is a string until something checks it, and " +
				"Docs/07 §6's tolerance of an unrecognised value has to be a decision the page takes rather " +
				"than a cast that hides it.",
		}
		b.WriteString(jsdoc("", docLines(strings.Join(guardDoc, "\n\n"), len(e.Values), tsWidth-3)))
		fmt.Fprintf(&b, "export function %s(value: string): value is %s {\n", guard, e.TypeScript.Type)
		fmt.Fprintf(&b, "  return (%s as readonly string[]).includes(value);\n}\n", values)
	}

	return b.String()
}

// jsdoc renders wrapped lines as a block comment at the given indentation.
func jsdoc(pad string, lines []string) string {
	var b strings.Builder
	b.WriteString(pad + "/**\n")
	for _, l := range lines {
		if l == "" {
			b.WriteString(pad + " *\n")
			continue
		}
		b.WriteString(pad + " * " + l + "\n")
	}
	b.WriteString(pad + " */\n")
	return b.String()
}
