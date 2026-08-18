package main

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
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

// TestTheRegisteredTaskSetIsWhatItSaysItIs pins what this binary actually starts, so that adding a
// task is a decision somebody has to re-read rather than something that happens.
//
// # This test exists because a deadline passed unobserved
//
// `Docs/11` §9 set itself a trigger: the `--only=<task>` selector question had to be settled
// "before the fourth task registers, which is SHIP-89 or SHIP-119, whichever comes first". SHIP-89
// landed in wave 8 and registered the fourth. **Nothing noticed**, because the trigger was a count
// in a sentence and the only thing watching it was a reader who happened to remember. The same
// wave's log recorded "three registered tasks, not four — confirmed", which was true when it was
// measured and stale by the end of the wave it was written in.
//
// A count in prose cannot see itself go out of date. A count in a test can, so this is the count,
// and the next registration fails here with the question attached rather than passing silently.
//
// # Updating it is the point, not the cost
//
// A lane adding a task edits one line here and reads the paragraph attached to it. That is a
// deliberate speed bump on a decision worth four paragraphs in §9 — every start of `cmd/worker`
// runs **every** registered task, so each addition changes what every verify section that starts
// the worker is doing, whether or not that section mentions the new task.
func TestTheRegisteredTaskSetIsWhatItSaysItIs(t *testing.T) {
	// The real registry, deliberately: every other test in this file swaps it out, and this is
	// the one that has to see what init() actually declared.
	//
	// Deps is filled further than a name needs, for the reason outbox_test.go gives: tasks()
	// builds *every* registration, so a half-filled Deps fails inside somebody else's closure —
	// jobs.NewService panics without a clock and bidding's registration builds one.
	built, err := tasks(Deps{
		Config: &config.Config{},
		Logger: slog.New(slog.DiscardHandler),
		Clock:  clock.System{},
	})
	if err != nil {
		t.Fatalf("building the registered tasks: %v", err)
	}

	var names []string
	for _, task := range built {
		names = append(names, task.Name)
	}

	// Sorted, because tasks() sorts. Add a name here in the same change that registers it.
	//
	// # SHIP-119 added the fifth, and this is the paragraph it was made to read
	//
	// job-auto-complete sweeps Delivered jobs seventy-two hours after they were delivered
	// (Docs/02 §6.1). Two things were checked before the line was added rather than after.
	//
	// **§9's reopening trigger does not fire.** SHIP-15r settled the --only=<task> question as a
	// convention — fence what you assert on, own what you assert about — and named exactly one
	// case that would reopen it: "a task that sweeps rows due by wall-clock alone", which fencing
	// cannot cover. This is not that task. It claims what is *due*, on a deadline derived from the
	// row's own job_status_history entry, so a section that leaves no job Delivered and older than
	// seventy-two hours leaves it nothing to do. That is the property job-expiry and bid-expiry
	// have and the one outbox-publisher conspicuously does not.
	//
	// **What it does to the existing sections is nothing, and that was measured rather than
	// assumed.** No section leaves a job in Delivered with a backdated transition: 70-delivery.sh
	// records milestones against jobs it created in the same run, and a job delivered seconds ago
	// is not due for three days. The one section that makes a job due is 51-jobs-autocomplete.sh,
	// which creates it, demonstrates it, and owns it.
	//
	// # SHIP-171 added the sixth, and this is what it checked before adding the line
	//
	// account-pseudonymisation executes deletion requests whose promised date has arrived
	// (Docs/05 §3.1). The same two things were checked, in the same order.
	//
	// **§9's reopening trigger does not fire.** The one case the convention cannot cover is "a
	// task that sweeps rows due by wall-clock alone". This is not that task: `complete_by` is a
	// **stored** column, written from the row's own request time plus thirty days, so every
	// request any endpoint can create is thirty days from claimable at the moment it is created.
	// That is job-expiry's shape and job-auto-complete's.
	//
	// **What it does to the existing sections is nothing, and that was measured rather than
	// assumed.** `account_deletion_requests` is written by exactly one section — 40-identity.sh,
	// which sorts before every section that starts the worker — and every row it leaves is thirty
	// days out. No section backdates one. This is the first task that could reach a row another
	// section is still using, because it replaces a *user's* email address rather than moving a
	// job or an offer, which is why the sweep was run against the whole harness rather than
	// against its own section alone.
	want := []string{
		"account-pseudonymisation",
		"bid-expiry",
		"job-auto-complete",
		"job-expiry",
		"job-expiry-warning",
		"outbox-publisher",
	}

	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Errorf(`cmd/worker registers %v; this test expects %v.

If you have just added a task, add its name above — and read Docs/11 §9's cmd/worker entry
first. One binary runs every registered task on every start, so your task now runs inside
every scripts/verify section that starts the worker, including the ones that do not mention
it. §9 carries the open selector question and the reason the answer has been a convention
rather than a flag.

If you have just removed one, a task that stops being registered produces no compile error
and no other failure — which is the whole reason this test is a list rather than a count.`,
			names, want)
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
