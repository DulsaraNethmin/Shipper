package httpx

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recover turns a panic in a handler into a 500 instead of a dropped connection and a
// dead server process.
//
// The response body is deliberately bare. SHIP-12 defines the standard error contract
// with a machine-readable code, and this is the one place that will need updating when
// it lands; until then, leaking a panic message to a client would be worse than saying
// nothing.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}

				// ErrAbortHandler is net/http's documented way for a handler to abandon
				// a response deliberately. It is not a defect, and net/http expects to
				// receive it, so pass it straight back up without logging a stack trace.
				if rec == http.ErrAbortHandler {
					panic(rec)
				}

				log.LogAttrs(r.Context(), slog.LevelError, "panic recovered",
					slog.String("request_id", RequestIDFrom(r.Context())),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())),
				)

				WriteJSON(w, http.StatusInternalServerError, map[string]string{
					"error": "internal_error",
				})
			}()

			next.ServeHTTP(w, r)
		})
	}
}
