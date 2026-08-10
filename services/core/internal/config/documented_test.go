package config

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The header of deploy/.env.example says the authoritative list of variables is config.go, and
// asks whoever adds one there to add it here too. That instruction has been correct and
// unenforced since SHIP-8.
//
// An undocumented variable is a quiet failure. Nothing breaks: the default applies, the service
// starts, and the setting is simply invisible to whoever is trying to configure a staging
// environment — discovered eventually by reading the source, which is exactly what the example
// file exists to prevent.
//
// A generator was the other option: derive .env.example from config.go and have CI fail on a
// diff. It would need every key restructured into a declarative table first, which is a large
// change to a file three tracks will be editing. This gets the same guarantee — a missing key
// fails a test at merge — for a fraction of the disturbance.

// envKey matches the loader calls that read a variable: l.str("HTTP_PORT", …), l.duration(…),
// and so on. The key is always the first argument and always a literal.
var envKey = regexp.MustCompile(`l\.\w+\("([A-Z][A-Z0-9_]*)"`)

// exampleAssignment matches a variable named in deploy/.env.example, whether it is set or
// merely shown commented out. A commented example counts as documented: it is how an optional
// override is meant to appear.
var exampleAssignment = regexp.MustCompile(`(?m)^\s*#?\s*([A-Z][A-Z0-9_]*)=`)

const (
	configSource = "config.go"
	envExample   = "../../../../deploy/.env.example"
)

func TestEveryVariableIsDocumented(t *testing.T) {
	source, err := os.ReadFile(configSource)
	if err != nil {
		t.Fatalf("reading %s: %v", configSource, err)
	}

	example, err := os.ReadFile(envExample)
	if err != nil {
		t.Fatalf("reading %s: %v", envExample, err)
	}

	documented := map[string]bool{}
	for _, m := range exampleAssignment.FindAllStringSubmatch(string(example), -1) {
		documented[m[1]] = true
	}

	var missing []string
	for _, m := range envKey.FindAllStringSubmatch(string(source), -1) {
		if !documented[m[1]] {
			missing = append(missing, m[1])
		}
	}

	sort.Strings(missing)
	for _, key := range missing {
		t.Errorf("%s is read by config.go but does not appear in deploy/.env.example.\n"+
			"Add it with its default and a line on what it is for — that file is where anyone\n"+
			"configuring an environment goes looking.", key)
	}
}

// The reverse direction: a variable documented as though the service reads it, which it does
// not.
//
// This one is worth having because the failure is actively misleading rather than merely
// unhelpful. Someone sets it in staging, restarts, and watches nothing change — and the obvious
// conclusion is that the deployment is broken rather than that the variable was renamed and the
// example file was not.
//
// Variables consumed by docker-compose or by the Makefile rather than by the binaries are
// listed as exceptions, because they are legitimately documented here and legitimately absent
// from config.go.
func TestNothingIsDocumentedThatIsNotRead(t *testing.T) {
	// Read by deploy/docker-compose.yml or by the Makefile, never by the service.
	notReadByTheService := map[string]bool{
		"POSTGRES_USER":     true,
		"POSTGRES_PASSWORD": true,
		"POSTGRES_DB":       true,
		"POSTGRES_PORT":     true,
		"REDIS_PORT":        true,
		"KAFKA_PORT":        true,
		"KAFKA_CLUSTER_ID":  true,

		// Read by the test harness in internal/testsupport, not by config.
		"TEST_DATABASE_URL": true,
		"TEST_REDIS_URL":    true,
		"TEST_TEMPLATE_DB":  true,
	}

	source, err := os.ReadFile(configSource)
	if err != nil {
		t.Fatalf("reading %s: %v", configSource, err)
	}

	read := map[string]bool{}
	for _, m := range envKey.FindAllStringSubmatch(string(source), -1) {
		read[m[1]] = true
	}

	example, err := os.ReadFile(envExample)
	if err != nil {
		t.Fatalf("reading %s: %v", envExample, err)
	}

	var orphaned []string
	for _, m := range exampleAssignment.FindAllStringSubmatch(string(example), -1) {
		key := m[1]
		if read[key] || notReadByTheService[key] {
			continue
		}
		orphaned = append(orphaned, key)
	}

	sort.Strings(orphaned)
	for _, key := range orphaned {
		t.Errorf("deploy/.env.example documents %s but nothing reads it.\n"+
			"Either config.go stopped reading it, or it belongs to compose or the Makefile —\n"+
			"in which case add it to notReadByTheService in %s.", key, "documented_test.go")
	}
}

// A sanity check on the regular expression itself. If envKey stopped matching — a loader helper
// renamed, the call style changed — both tests above would pass by finding nothing, which is
// the failure mode that makes a test like this worth writing.
func TestTheVariableScannerStillWorks(t *testing.T) {
	source, err := os.ReadFile(configSource)
	if err != nil {
		t.Fatalf("reading %s: %v", configSource, err)
	}

	found := envKey.FindAllStringSubmatch(string(source), -1)
	if len(found) < 10 {
		t.Fatalf("only %d environment variables found in %s; the scanner has stopped matching "+
			"the loader's call style, so the documentation checks are passing vacuously",
			len(found), configSource)
	}

	// One key known to be there, as a spot check.
	var keys []string
	for _, m := range found {
		keys = append(keys, m[1])
	}
	if !strings.Contains(strings.Join(keys, " "), "DATABASE_URL") {
		t.Error("DATABASE_URL was not found; the scanner is matching something other than config keys")
	}
}
