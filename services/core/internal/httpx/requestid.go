package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

// HeaderRequestID is the request-correlation header, read from the client if present and
// always echoed on the response.
const HeaderRequestID = "X-Request-Id"

type contextKey int

const requestIDKey contextKey = iota

// RequestID attaches a correlation ID to every request and echoes it on the response.
//
// A client-supplied ID is honoured, because a mobile client retrying a dropped request
// wants both attempts to correlate in the logs. It is validated first: the value ends up
// in log records, and an unvalidated header is how attacker-controlled text gets into a
// log aggregator. Anything unsuitable is silently replaced rather than rejected — a
// malformed correlation header is not worth failing a request over.
//
// SHIP-9 needs the ID present in the request log. SHIP-14 extends this to propagate the
// ID into downstream calls and to every log record emitted while handling the request.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := sanitiseRequestID(r.Header.Get(HeaderRequestID))
		if id == "" {
			id = newRequestID()
		}

		w.Header().Set(HeaderRequestID, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

// RequestIDFrom returns the correlation ID carried by ctx, or "" if there is none.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

const maxRequestIDLen = 64

// sanitiseRequestID accepts only [A-Za-z0-9._:-], up to a bounded length.
//
// That admits every identifier format worth correlating on — a UUID, a hex string, a
// ULID, a Datadog trace ID, a W3C traceparent — and nothing else. Quotes, backslashes,
// braces, whitespace, and control characters are excluded because no legitimate
// correlation ID contains them, and because this value is written into log records:
// slog's handlers escape it correctly today, but a value that cannot cause trouble in
// the first place does not depend on every future sink doing the same.
func sanitiseRequestID(v string) string {
	if v == "" || len(v) > maxRequestIDLen {
		return ""
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.', c == ':':
		default:
			return ""
		}
	}
	return v
}

func newRequestID() string {
	var b [16]byte
	// rand.Read is documented never to return an error as of Go 1.24; it panics on a
	// genuinely broken system entropy source, which is the correct outcome anyway.
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
