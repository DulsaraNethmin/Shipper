package httpx

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// withCleanRegistry swaps in an empty registry for the duration of a test and puts the real one
// back afterwards.
//
// The registry is package-level state populated at init, which is right for the mechanism and
// awkward for testing it: registering a probe code would otherwise leak into the generated
// document that cmd/api compares against a committed file.
func withCleanRegistry(t *testing.T) {
	t.Helper()

	codesMu.Lock()
	saved := codes
	codes = map[Code]CodeInfo{}
	codesMu.Unlock()

	t.Cleanup(func() {
		codesMu.Lock()
		codes = saved
		codesMu.Unlock()
	})
}

func TestRegisterCodeReturnsTheCodeAndRecordsIt(t *testing.T) {
	withCleanRegistry(t)

	got := RegisterCode("jobs_prohibited_category", "The goods category may not be published.")
	if got != Code("jobs_prohibited_category") {
		t.Errorf("RegisterCode returned %q, want the code it was given", got)
	}

	registered := RegisteredCodes()
	if len(registered) != 1 {
		t.Fatalf("registry holds %d codes, want 1", len(registered))
	}
	if registered[0].Protocol {
		t.Error("a domain code was recorded as a protocol code")
	}
	if registered[0].Description == "" {
		t.Error("the description was not recorded")
	}
}

// The duplicate is the failure this whole mechanism exists to make impossible, and it has to be
// loud. Two domains meaning different things by one string is invisible on the wire: a client
// branches on it and handles one of the two cases correctly by accident.
func TestRegisteringACodeTwicePanics(t *testing.T) {
	withCleanRegistry(t)

	RegisterCode("jobs_expired", "The job has expired.")

	defer func() {
		p := recover()
		if p == nil {
			t.Fatal("registering a duplicate code did not panic")
		}
		if msg, _ := p.(string); !strings.Contains(msg, "jobs_expired") {
			t.Errorf("the panic does not name the colliding code: %v", p)
		}
	}()

	RegisterCode("jobs_expired", "Something else entirely.")
}

func TestMalformedCodesAreRefused(t *testing.T) {
	cases := []struct {
		name string
		code Code
	}{
		{"empty", ""},
		{"camel case", "prohibitedCategory"},
		{"upper case", "PROHIBITED_CATEGORY"},
		{"a hyphen", "prohibited-category"},
		{"a space", "prohibited category"},
		{"a dot", "jobs.prohibited"},
		{"a leading underscore", "_prohibited"},
		{"a trailing underscore", "prohibited_"},
		{"a doubled underscore", "jobs__prohibited"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withCleanRegistry(t)

			defer func() {
				if recover() == nil {
					t.Errorf("registering %q did not panic", tc.code)
				}
			}()

			RegisterCode(tc.code, "A description.")
		})
	}
}

// A code with no description reaches the generated document as a bare string, which leaves three
// client codebases guessing at its meaning from its name.
func TestACodeWithoutADescriptionIsRefused(t *testing.T) {
	withCleanRegistry(t)

	defer func() {
		if recover() == nil {
			t.Error("registering a code with no description did not panic")
		}
	}()

	RegisterCode("jobs_expired", "")
}

func TestRegisteredCodesAreSorted(t *testing.T) {
	withCleanRegistry(t)

	for _, c := range []Code{"jobs_zeta", "jobs_alpha", "jobs_mid"} {
		RegisterCode(c, "A description.")
	}

	got := RegisteredCodes()
	sorted := make([]string, len(got))
	for i, info := range got {
		sorted[i] = string(info.Code)
	}

	if !sort.StringsAreSorted(sorted) {
		t.Errorf("RegisteredCodes returned %v, which is not sorted — the generated document "+
			"would reorder itself between runs", sorted)
	}
}

// codeConstant matches the protocol code declarations in this package's own source:
//
//	CodeBadRequest Code = "bad_request"
var codeConstant = regexp.MustCompile(`Code\s*=\s*"([a-z][a-z0-9_]*)"`)

// TestEveryProtocolCodeIsRegistered stops the constants and the registry drifting apart.
//
// The constants stay in errors.go and idempotency.go beside the code that raises them, which is
// the right place to read them; the registry is what generates the document clients branch on.
// Two lists of the same thing drift, so this reads one out of the source and checks the other —
// the same approach internal/config/documented_test.go already takes to .env.example, and for the
// same reason: the failure is otherwise silent and only visible to somebody reading both.
func TestEveryProtocolCodeIsRegistered(t *testing.T) {
	declared := map[Code]bool{}

	for _, file := range []string{"errors.go", "idempotency.go"} {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("reading %s: %v", file, err)
		}
		for _, m := range codeConstant.FindAllStringSubmatch(string(source), -1) {
			declared[Code(m[1])] = true
		}
	}

	if len(declared) == 0 {
		t.Fatal("found no Code constants in the source; the pattern in codeConstant has stopped matching")
	}

	registered := map[Code]bool{}
	for _, info := range RegisteredCodes() {
		registered[info.Code] = true
	}

	for code := range declared {
		if !registered[code] {
			t.Errorf("the constant for %q is declared in this package but never registered.\n"+
				"Add it to the init in codes.go with a description — otherwise it is absent from\n"+
				"Docs/10-api-error-codes.md, which is the list three clients branch on.", code)
		}
	}
}

// Docs/10 §4.4 says "the fifteen that exist today", and that number is quoted in Docs/11 §3 as
// well. Pinning it means the document and the code cannot disagree quietly: adding a protocol
// code is a deliberate act that updates both.
func TestTheProtocolSetIsTheFifteenDocumented(t *testing.T) {
	var protocol []string
	for _, info := range RegisteredCodes() {
		if info.Protocol {
			protocol = append(protocol, string(info.Code))
		}
	}

	if len(protocol) != 15 {
		t.Errorf("there are %d protocol codes, and Docs/10 §4.4 says fifteen: %v\n"+
			"Adding one is fine — update §4.4 and this test in the same change.",
			len(protocol), protocol)
	}
}
