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

// The context keys this package uses. They are unexported and of a private type, so
// nothing outside can collide with them or overwrite one. Both live in the same iota
// block to keep that guarantee visible in one place.
type contextKey int

const (
	requestIDKey contextKey = iota
	loggerKey
)

// RequestID attaches a correlation ID to every request and echoes it on the response.
//
// A client-supplied ID is honoured, because a mobile client retrying a dropped request
// wants both attempts to correlate in the logs. It is validated first: the value ends up
// in log records, and an unvalidated header is how attacker-controlled text gets into a
// log aggregator. Anything unsuitable is silently replaced rather than rejected — a
// malformed correlation header is not worth failing a request over.
//
// The ID is put in the request context, from where [Logger] binds it to a request-scoped
// logger and [PropagateRequestID] carries it onto outbound calls (SHIP-14). One request
// therefore has one identifier from the moment it arrives to the last thing it causes.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := sanitiseRequestID(r.Header.Get(HeaderRequestID))
		if id == "" {
			id = NewRequestID()
		}

		w.Header().Set(HeaderRequestID, id)
		next.ServeHTTP(w, r.WithContext(ContextWithRequestID(r.Context(), id)))
	})
}

// ContextWithRequestID carries id on ctx.
//
// Exported for work that starts outside an HTTP request but still needs to be correlated:
// a scheduled task (SHIP-68, SHIP-89), an event consumer (SHIP-137), or a test. Generate
// one with [NewRequestID] and the resulting logs join the same trail as everything else.
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFrom returns the correlation ID carried by ctx, or "" if there is none.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// PropagateRequestID returns a transport that copies the correlation ID from the
// outbound request's context onto its [HeaderRequestID] header (SHIP-14).
//
// This is what makes a call to an email provider, an SMS gateway, or object storage
// traceable back to the request that caused it — and, where the other end honours the
// header, traceable through it as well.
//
//	client := &http.Client{Transport: httpx.PropagateRequestID(nil)}
//
// An ID the caller has already set is left alone, and a context without one is left
// alone: an outbound call is never worth failing over correlation.
func PropagateRequestID(next http.RoundTripper) http.RoundTripper {
	if next == nil {
		next = http.DefaultTransport
	}
	return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		id := RequestIDFrom(r.Context())
		if id == "" || r.Header.Get(HeaderRequestID) != "" {
			return next.RoundTrip(r)
		}

		// RoundTrip must not modify the request it is given: net/http may still be
		// holding it, and a retry would see a request that has already been altered.
		clone := r.Clone(r.Context())
		clone.Header.Set(HeaderRequestID, id)
		return next.RoundTrip(clone)
	})
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

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

// NewRequestID generates a correlation ID, for work that begins outside an HTTP request
// and still has to be findable in the logs.
func NewRequestID() string {
	var b [16]byte
	// rand.Read is documented never to return an error as of Go 1.24; it panics on a
	// genuinely broken system entropy source, which is the correct outcome anyway.
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
