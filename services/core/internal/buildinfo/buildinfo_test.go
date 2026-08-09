package buildinfo

import (
	"strings"
	"testing"
)

// Every field must be populated. A health endpoint reporting an empty version gives a
// client nothing to compare against, which is worse than reporting "dev".
func TestGetNeverReturnsEmptyFields(t *testing.T) {
	info := Get()

	if info.Version == "" {
		t.Error("Version was empty, want at least \"dev\"")
	}
	if info.Commit == "" {
		t.Error("Commit was empty, want at least \"unknown\"")
	}
	if info.BuiltAt == "" {
		t.Error("BuiltAt was empty, want at least \"unknown\"")
	}
}

func TestGetIsStable(t *testing.T) {
	if first, second := Get(), Get(); first != second {
		t.Errorf("Get() returned %+v then %+v; the value must be computed once", first, second)
	}
}

func TestShortCommitTruncates(t *testing.T) {
	got := ShortCommit()

	if len(got) > 12 {
		t.Errorf("ShortCommit() = %q, want at most 12 characters", got)
	}
	if full := Get().Commit; !strings.HasPrefix(full, got) {
		t.Errorf("ShortCommit() = %q, want a prefix of %q", got, full)
	}
}

// resolve is exercised directly because Get caches, so the fallback path can only be
// observed once per process otherwise.
func TestResolveFallsBackWhenNothingIsInjected(t *testing.T) {
	// The package-level stamps are empty in a test binary unless -ldflags set them.
	if version != "" || commit != "" {
		t.Skip("build stamps were injected; the fallback path is not reachable here")
	}

	info := resolve()
	if info.Version != "dev" {
		t.Errorf("Version = %q, want \"dev\" when nothing is injected", info.Version)
	}
	// Commit comes from the toolchain's VCS stamp when available, and "unknown" when not.
	if info.Commit == "" {
		t.Error("Commit was empty, want \"unknown\" or a VCS revision")
	}
}
