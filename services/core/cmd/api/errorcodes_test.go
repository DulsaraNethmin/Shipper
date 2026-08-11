package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// The whole error code registry, checked and published (SHIP-15c).
//
// These tests live in cmd/api rather than in internal/httpx for one reason: this is the package
// that imports every domain, so it is the only place where the whole registry exists at once. A
// uniqueness check inside httpx would only ever see httpx's own codes.
//
// That also means the coverage here grows on its own. When SHIP-30 adds routes_identity.go and
// cmd/api starts importing internal/identity, identity's codes appear in this document with no
// edit to anything shared — which is the property the registry exists for.

// errorCodeDocPath is the generated list clients branch on (Docs/10 §4.4), relative to this
// package.
const errorCodeDocPath = "../../../../Docs/10-api-error-codes.md"

// TestErrorCodesAreUniqueAndWellFormed is the assertion Docs/10 §4.4 promises.
//
// Registration already panics on a duplicate, which fires at init and would take this whole
// package down before any test ran — so in the ordinary case this test passes trivially and the
// panic is the real enforcement. It is written out anyway because "a test in cmd/api asserts
// uniqueness across the whole registry" is what the convention document tells people is true,
// and a mechanism nobody can point at is one nobody trusts.
func TestErrorCodesAreUniqueAndWellFormed(t *testing.T) {
	registered := httpx.RegisteredCodes()
	if len(registered) == 0 {
		t.Fatal("the registry is empty; internal/httpx should have registered its protocol codes")
	}

	seen := map[httpx.Code]bool{}
	for _, info := range registered {
		if seen[info.Code] {
			t.Errorf("%q is registered twice", info.Code)
		}
		seen[info.Code] = true

		if info.Description == "" {
			t.Errorf("%q has no description", info.Code)
		}
		if strings.ToLower(string(info.Code)) != string(info.Code) {
			t.Errorf("%q is not lower snake case", info.Code)
		}
	}
}

// TestErrorCodeDocumentIsCurrent regenerates the published code list and diffs it.
//
// The same mechanism as routes_golden.txt, and for the same reason: the document is what three
// client codebases read, and one that is written by hand is one that describes last month's
// codes. Because it is generated, a merge conflict in it is resolved by regenerating rather than
// by choosing a side — which matters, since choosing a side silently drops a domain's codes.
//
// Regenerate with `go test ./cmd/api -run TestErrorCodeDocumentIsCurrent -update`.
func TestErrorCodeDocumentIsCurrent(t *testing.T) {
	got := renderErrorCodeDocument(httpx.RegisteredCodes())

	if *updateGolden {
		if err := os.WriteFile(errorCodeDocPath, []byte(got), 0o644); err != nil {
			t.Fatalf("writing %s: %v", errorCodeDocPath, err)
		}
		t.Logf("wrote %s", filepath.Clean(errorCodeDocPath))
		return
	}

	want, err := os.ReadFile(errorCodeDocPath)
	if err != nil {
		t.Fatalf("reading %s: %v\nregenerate with "+
			"`go test ./cmd/api -run TestErrorCodeDocumentIsCurrent -update`", errorCodeDocPath, err)
	}

	if got != string(want) {
		t.Errorf("%s is out of date.\n\n"+
			"Regenerate it — never hand-edit it, and never resolve a conflict in it by hand:\n"+
			"    go test ./cmd/api -run TestErrorCodeDocumentIsCurrent -update\n",
			filepath.Base(errorCodeDocPath))
	}
}

// TestProtocolCodesMatchTheContract holds the published contract to the registry.
//
// contracts/components/schemas/error.yaml lists the protocol codes under x-protocol-codes, by
// hand, because a closed enum there would make every domain adding a code edit a shared file. A
// hand-written list is a list that drifts, and this is the check that stops it — the same
// both-directions discipline SHIP-17a applied to the route surface.
func TestProtocolCodesMatchTheContract(t *testing.T) {
	doc := loadContract(t)

	schema, ok := doc.Components.Schemas["Error"]
	if !ok {
		t.Fatal("the contract has no Error schema")
	}
	code, ok := schema.Value.Properties["error"].Value.Properties["code"]
	if !ok {
		t.Fatal("the Error schema has no error.code property")
	}

	published := extensionStrings(t, code.Value.Extensions["x-protocol-codes"])
	if len(published) == 0 {
		t.Fatal("error.code has no x-protocol-codes; the contract has stopped listing them")
	}

	var registered []string
	for _, info := range httpx.RegisteredCodes() {
		if info.Protocol {
			registered = append(registered, string(info.Code))
		}
	}

	sort.Strings(published)
	sort.Strings(registered)

	if strings.Join(published, ",") != strings.Join(registered, ",") {
		t.Errorf("the contract's protocol codes and the registry disagree.\n"+
			"  contract: %v\n  registry: %v\n"+
			"Update x-protocol-codes in contracts/components/schemas/error.yaml.",
			published, registered)
	}
}

// extensionStrings reads a YAML/JSON extension value as a list of strings.
//
// kin-openapi has carried extensions as json.RawMessage in some versions and as a decoded any in
// others, so both are handled rather than pinning this test to one of them.
func extensionStrings(t *testing.T, value any) []string {
	t.Helper()

	switch v := value.(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				t.Fatalf("x-protocol-codes holds %T, want a string", item)
			}
			out = append(out, s)
		}
		return out
	case json.RawMessage:
		var out []string
		if err := json.Unmarshal(v, &out); err != nil {
			t.Fatalf("decoding x-protocol-codes: %v", err)
		}
		return out
	default:
		t.Fatalf("x-protocol-codes is a %T, which this test does not know how to read", value)
		return nil
	}
}

// renderErrorCodeDocument writes the registry as the published list.
//
// The two tables are separated because a client handles them differently rather than because it
// reads better: the protocol codes can come back from any endpoint and are handled once in the
// transport layer, while a domain code is handled at the call site that can do something about
// it.
func renderErrorCodeDocument(registered []httpx.CodeInfo) string {
	var b strings.Builder

	b.WriteString(`# Shipper — API error codes

**Generated. Do not edit.** Regenerate with:

` + "```\ngo test ./cmd/api -run TestErrorCodeDocumentIsCurrent -update\n```" + `

Source: the registry in ` + "`internal/httpx/codes.go`" + `, plus every code each domain declares
with ` + "`httpx.RegisterCode`" + `. A merge conflict in this file is resolved by regenerating it,
never by choosing a side — choosing a side drops a domain's codes silently.

## How to use this list

Every failure the platform returns has the same shape, and ` + "`error.code`" + ` is the only part of
it a client may branch on (` + "`Docs/10`" + ` §4.4). Messages are reworded, translated and made
friendlier; a store build that switched on message text would break on a copy edit, with no
over-the-air path to fix it.

**Protocol codes** can be returned by any endpoint, because they come from the transport, the
middleware, or ` + "`net/http`" + ` itself rather than from a business rule. Handle them once, centrally.

**Domain codes** are named ` + "`<domain>_<condition>`" + ` and are raised by one domain's rules. Handle
them where the call is made, beside the thing the user was trying to do.

`)

	protocol, domain := split(registered)

	b.WriteString("## Protocol codes\n\n")
	writeTable(&b, protocol)

	b.WriteString("\n## Domain codes\n\n")
	if len(domain) == 0 {
		b.WriteString("None yet. The first arrives with the first domain rule that needs one — a\n" +
			"registration in that domain's own package, and no edit to anything shared.\n")
	} else {
		writeTable(&b, domain)
	}

	return b.String()
}

func split(registered []httpx.CodeInfo) (protocol, domain []httpx.CodeInfo) {
	for _, info := range registered {
		if info.Protocol {
			protocol = append(protocol, info)
		} else {
			domain = append(domain, info)
		}
	}
	return protocol, domain
}

func writeTable(b *strings.Builder, infos []httpx.CodeInfo) {
	b.WriteString("| Code | Meaning |\n|---|---|\n")
	for _, info := range infos {
		fmt.Fprintf(b, "| `%s` | %s |\n", info.Code, info.Description)
	}
}
