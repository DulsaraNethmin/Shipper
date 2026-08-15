package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// SHIP-147a, from the side internal/config cannot see.
//
// `config.TestThereIsOneArgon2CostSetting` proves there is one cost *setting*. It cannot prove
// that every hasher reads it: a profile is a plain struct of three integers, so a second one is a
// composite literal in this package away, and the composition root is where a domain and its
// configuration meet (Docs/06 §4.1) — which makes this the only file that can watch for it.
//
// The failure is the one SHIP-15r moved argon2id into internal/passwords to prevent, one level up.
// A literal profile here would verify every hash it is handed, because the parameters travel in
// each hash's PHC string, and nothing would report that half the platform's passwords were written
// at a different cost until somebody raised the setting and only one of them moved.

// argon2ProfileLiteral matches a hasher being built: `passwords.Argon2Profile{`.
var argon2ProfileLiteral = regexp.MustCompile(`passwords\.Argon2Profile\{`)

// theOneCost is the field every such literal must be built from.
const theOneCost = "Config.Passwords.Argon2"

func TestEveryPasswordHasherIsBuiltFromTheOnePlatformCost(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading cmd/api: %v", err)
	}

	built := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}

		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		lines := strings.Split(string(source), "\n")

		for i, line := range lines {
			if !argon2ProfileLiteral.MatchString(line) {
				continue
			}
			built++

			// The three fields follow the literal. Four lines is the whole of a profile
			// plus its brace, and reading past it would start matching the next thing.
			body := strings.Join(lines[i:min(i+5, len(lines))], "\n")
			if !strings.Contains(body, theOneCost) {
				t.Errorf("%s:%d builds an argon2id profile that does not read %s:\n%s\n\n"+
					"There is one platform password cost (SHIP-147a). A second profile here "+
					"agrees with the first by comment until somebody raises one, and every "+
					"hash stays verifiable either way — so nothing reports the divergence.",
					name, i+1, theOneCost, body)
			}
		}
	}

	// Two today: identity's hasher and admin's. Zero would mean the pattern stopped matching
	// and this test had started passing by finding nothing, which is the failure mode a
	// source-scanning guard has available to it.
	if built < 2 {
		t.Errorf("only %d argon2id profiles were found in cmd/api; the scanner has stopped "+
			"matching how a hasher is built, so this check is passing vacuously", built)
	}
}
