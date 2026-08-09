// Package logging builds the process-wide structured logger (SHIP-9).
//
// It deliberately takes primitives rather than a config struct. Logging is needed by
// almost everything, and a dependency from here into configuration would put the config
// package underneath every domain in the import graph — exactly the kind of edge the
// boundary lint in SHIP-11 exists to prevent.
package logging

import (
	"io"
	"log/slog"
)

// New returns a logger writing to w.
//
// format is "json" or "text". Anything other than "text" is treated as JSON, because a
// logger that refuses to construct leaves a failure with nowhere to be reported;
// validation of the value belongs to config.Load, which has already run by this point.
func New(w io.Writer, level slog.Level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: level,

		// Source location costs a caller lookup per record. At debug level someone is
		// already reading individual lines and the file:line is worth more than the
		// overhead; above it, the message and attributes should be enough.
		AddSource: level <= slog.LevelDebug,
	}

	var h slog.Handler
	if format == "text" {
		h = slog.NewTextHandler(w, opts)
	} else {
		h = slog.NewJSONHandler(w, opts)
	}
	return slog.New(h)
}
