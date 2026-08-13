package main

import (
	"reflect"
	"strings"
	"testing"
)

// The guard's decision is a pure function of three things — the migrations compiled in, the
// version the database records, and what the ledger says ran — so it is tested without a
// database. The database half is exercised by make verify, which runs the real commands.

func TestSkippedFindsAMigrationBelowTheCurrentVersion(t *testing.T) {
	// The exact shape of the hazard: identity's 105 written after jobs' 400s landed.
	versions := []int{1, 2, 100, 105, 400, 401}
	applied := map[int]bool{1: true, 2: true, 100: true, 400: true, 401: true}

	got := skipped(versions, 401, applied)
	if want := []int{105}; !reflect.DeepEqual(got, want) {
		t.Errorf("skipped = %v, want %v — this is the failure the guard exists to catch", got, want)
	}
}

func TestSkippedIsSilentWhenEverythingBelowHasRun(t *testing.T) {
	versions := []int{1, 2, 100, 400}
	applied := map[int]bool{1: true, 2: true, 100: true, 400: true}

	if got := skipped(versions, 400, applied); len(got) != 0 {
		t.Errorf("skipped = %v, want none; an ordinary up must not be refused", got)
	}
}

// A migration above the current version is pending, not skipped. golang-migrate will apply it,
// which is the whole normal case, and reporting it here would refuse every ordinary run.
func TestSkippedIgnoresMigrationsAboveTheCurrentVersion(t *testing.T) {
	versions := []int{1, 100, 400, 405}
	applied := map[int]bool{1: true, 100: true, 400: true}

	if got := skipped(versions, 400, applied); len(got) != 0 {
		t.Errorf("skipped = %v, want none; 405 is pending and will be applied normally", got)
	}
}

func TestBackfillSeedsOnlyWhatTheVersionImplies(t *testing.T) {
	versions := []int{1, 2, 100, 400, 401, 405}

	got := backfill(versions, 401)
	want := []int{1, 2, 100, 400, 401}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("backfill = %v, want %v", got, want)
	}
}

// After a `down all` the version is zero and the ledger must end up empty, or the next `up`
// would treat every migration as already applied and the guard would never fire again.
func TestBackfillSeedsNothingAtVersionZero(t *testing.T) {
	if got := backfill([]int{1, 2, 100}, 0); len(got) != 0 {
		t.Errorf("backfill = %v, want none at version 0", got)
	}
}

// The guard is only as good as its view of what is compiled in. If this stops matching the
// filenames, skipped() sees an empty list and passes vacuously — the same failure mode
// TestTheVariableScannerStillWorks guards against in internal/config.
func TestEmbeddedVersionsReadsTheRealMigrations(t *testing.T) {
	got, err := embeddedVersions()
	if err != nil {
		t.Fatalf("embeddedVersions: %v", err)
	}
	if len(got) < 10 {
		t.Fatalf("only %d migrations found; the filename scanner has stopped matching, so the "+
			"out-of-order guard is passing vacuously", len(got))
	}

	for i := 1; i < len(got); i++ {
		if got[i] <= got[i-1] {
			t.Fatalf("versions are not strictly ascending at %d: %v", i, got[i-1:i+1])
		}
	}

	// One known migration, as a spot check that these are real numbers.
	var found bool
	for _, v := range got {
		if v == 1 {
			found = true
		}
	}
	if !found {
		t.Error("000001_init is not in the embedded list; the scanner is matching something else")
	}
}

// The message is the whole user interface of this guard: it fires on a developer's machine, at a
// moment when the obvious reading is that the tool is broken.
func TestTheSkippedMessageNamesTheMigrationAndTheFix(t *testing.T) {
	err := errSkipped([]int{105})
	msg := err.Error()

	for _, want := range []string{
		"000105",   // which migration
		"identity", // whose it is
		"make migrate-down n=all && make migrate-up", // what to do
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the message does not mention %q:\n%s", want, msg)
		}
	}
}
