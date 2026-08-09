package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// Logger emits one structured record per request, carrying the fields SHIP-9 requires:
// method, path, status, duration, and request ID.
//
// It also puts a request-scoped logger on the context (SHIP-14). Every record written
// through [LoggerFrom] while handling the request carries the request ID without the
// caller doing anything, which is the difference between a correlation ID that works and
// one that works wherever somebody remembered it. Reading one request's story out of a
// log aggregator depends on every line of it being attributable, including the ones
// written five calls deep in a domain package.
//
// The request record itself is written after the handler returns, so the status and
// duration are the real ones. That means a request that never completes produces no line
// at all — which is what the server timeouts in the HTTP config are there to bound.
func Logger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			rec := &recorder{ResponseWriter: w, status: http.StatusOK}

			requestLog := log
			if id := RequestIDFrom(r.Context()); id != "" {
				requestLog = log.With(slog.String("request_id", id))
			}
			ctx := ContextWithLogger(r.Context(), requestLog)
			r = r.WithContext(ctx)

			next.ServeHTTP(rec, r)

			elapsed := time.Since(started)

			// The query string is deliberately omitted. It is the part of a URL most
			// likely to carry a token, an email address, or a filter revealing something
			// about a customer, and this line goes to a log aggregator (SHIP-174).
			requestLog.LogAttrs(ctx, levelFor(rec.status), "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int64("bytes", rec.written),
				slog.Float64("duration_ms", float64(elapsed.Nanoseconds())/1e6),
			)
		})
	}
}

// ContextWithLogger carries log on ctx, where [LoggerFrom] can find it.
func ContextWithLogger(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, log)
}

// LoggerFrom returns the request-scoped logger on ctx, already bound to the request ID.
//
// It falls back to slog.Default rather than returning nil, so a caller never has to guard
// against a context that has not been through the middleware — code that logs should not
// have to decide whether logging is available.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if log, ok := ctx.Value(loggerKey).(*slog.Logger); ok && log != nil {
		return log
	}
	return slog.Default()
}

// levelFor keeps ordinary traffic at info while making failures findable without a
// query. A 4xx is the client's mistake and only warrants a warning; a 5xx is ours.
func levelFor(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

// recorder captures the status code and response size, which net/http does not expose.
type recorder struct {
	http.ResponseWriter
	status      int
	written     int64
	wroteHeader bool
}

func (r *recorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.status = status
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(b)
	r.written += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer, so wrapping does not
// quietly break flushing or deadline control for later handlers.
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
