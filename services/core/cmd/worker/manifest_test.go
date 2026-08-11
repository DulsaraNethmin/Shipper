package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// nothing is a task body that does nothing, for the manifest tests, which are about declaration
// rather than about work.
func nothing(context.Context, db.Runner) (int, error) { return 0, nil }

// TestTasksRefusesADeclarationThatCannotWork checks each way a registration goes wrong.
//
// These are returned as errors rather than panicked, unlike cmd/api's register: a task is built
// from Deps at start-up rather than declared as a literal at init, so the failure has somewhere
// to be reported and main can print it instead of a stack trace.
func TestTasksRefusesADeclarationThatCannotWork(t *testing.T) {
	cases := map[string]struct {
		tasks []Registration
		want  string
	}{
		"no name": {
			[]Registration{func(Deps) Task { return Task{Every: time.Minute, Run: nothing} }},
			"no name",
		},
		"no interval": {
			[]Registration{func(Deps) Task { return Task{Name: "a", Run: nothing} }},
			"no interval",
		},
		"nothing to do": {
			[]Registration{func(Deps) Task { return Task{Name: "a", Every: time.Minute} }},
			"does nothing",
		},
		"two of the same name": {
			[]Registration{
				func(Deps) Task { return Task{Name: "expiry", Every: time.Minute, Run: nothing} },
				func(Deps) Task { return Task{Name: "expiry", Every: time.Hour, Run: nothing} },
			},
			"two tasks are called",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			defer withRegistry(t, c.tasks)()

			_, err := tasks(Deps{})
			if err == nil {
				t.Fatal("the declaration was accepted")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("the error does not say what is wrong (%q): %v", c.want, err)
			}
		})
	}
}

// TestTasksAreBuiltInAStableOrder keeps the order two init functions happened to run in out of
// the worker's behaviour.
func TestTasksAreBuiltInAStableOrder(t *testing.T) {
	defer withRegistry(t, []Registration{
		func(Deps) Task { return Task{Name: "job-expiry", Every: time.Minute, Run: nothing} },
		func(Deps) Task { return Task{Name: "auto-complete", Every: time.Hour, Run: nothing} },
		func(Deps) Task { return Task{Name: "bid-expiry", Every: time.Minute, Run: nothing} },
	})()

	built, err := tasks(Deps{})
	if err != nil {
		t.Fatalf("building the tasks: %v", err)
	}

	var names []string
	for _, task := range built {
		names = append(names, task.Name)
	}
	want := []string{"auto-complete", "bid-expiry", "job-expiry"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf("tasks() = %v, want %v", names, want)
	}
}

// TestRegisterRefusesNothing is the one thing register can catch on its own; everything else is
// a property of the Task it produces, which does not exist until Deps do.
func TestRegisterRefusesNothing(t *testing.T) {
	defer withRegistry(t, nil)()

	defer func() {
		if recover() == nil {
			t.Error("a nil registration was accepted")
		}
	}()
	register(nil)
}

// TestATaskWithNoTimeoutGetsTheDefault keeps a forgotten field from meaning "no bound at all".
func TestATaskWithNoTimeoutGetsTheDefault(t *testing.T) {
	if got := (Task{Name: "a"}).timeout(); got != defaultTaskTimeout {
		t.Errorf("timeout() = %s, want the default %s", got, defaultTaskTimeout)
	}
	if got := (Task{Name: "a", Timeout: time.Minute}).timeout(); got != time.Minute {
		t.Errorf("timeout() = %s, want the task's own minute", got)
	}
}

// withRegistry swaps the package registry for the duration of a test and puts it back.
//
// The registry is package state populated from init functions, and there are none yet — but
// there will be four (SHIP-68, SHIP-69, SHIP-89, SHIP-119), and a test that left its own
// declarations behind would change what every later test in this package sees.
func withRegistry(t *testing.T, replacement []Registration) func() {
	t.Helper()

	previous := registry
	registry = replacement
	return func() { registry = previous }
}
