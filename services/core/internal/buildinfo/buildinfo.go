// Package buildinfo reports which build of the service is running (SHIP-6).
//
// This is not decoration. Once builds persist on devices indefinitely and more than one
// API version is live at a time (Docs/06 §5.3), "which commit is actually serving this
// request" becomes a question worth being able to answer in one call.
package buildinfo

import (
	"runtime/debug"
	"sync"
)

// Injected at link time by the Makefile:
//
//	-ldflags "-X <pkg>.version=... -X <pkg>.commit=... -X <pkg>.builtAt=..."
//
// Left unset, Get falls back to the VCS stamps the Go toolchain embeds automatically,
// so a plain `go run ./cmd/api` still reports a real commit rather than "unknown".
var (
	version string
	commit  string
	builtAt string
)

// Info is the build identity, as served by GET /health.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"built_at"`

	// Dirty reports that the working tree had uncommitted changes at build time.
	// A bug report against a dirty build is a bug report against a commit that does
	// not exist anywhere, which is worth knowing before spending an afternoon on it.
	Dirty bool `json:"dirty"`
}

var (
	once   sync.Once
	cached Info
)

// Get returns the build identity. The result is computed once and reused.
func Get() Info {
	once.Do(func() { cached = resolve() })
	return cached
}

func resolve() Info {
	info := Info{
		Version: version,
		Commit:  commit,
		BuiltAt: builtAt,
	}

	// debug.ReadBuildInfo carries VCS stamps whenever the binary was built inside a
	// repository without -buildvcs=false. They are only consulted for fields the
	// linker did not already set: an explicit -ldflags value is the more deliberate
	// statement of intent and should win.
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if info.Commit == "" {
					info.Commit = s.Value
				}
			case "vcs.time":
				if info.BuiltAt == "" {
					info.BuiltAt = s.Value
				}
			case "vcs.modified":
				info.Dirty = s.Value == "true"
			}
		}
	}

	if info.Version == "" {
		info.Version = "dev"
	}
	if info.Commit == "" {
		info.Commit = "unknown"
	}
	if info.BuiltAt == "" {
		info.BuiltAt = "unknown"
	}
	return info
}

// ShortCommit is the first 12 characters of the commit, for log lines where the full
// hash is noise.
func ShortCommit() string {
	c := Get().Commit
	if len(c) > 12 {
		return c[:12]
	}
	return c
}
