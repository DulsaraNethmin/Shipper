package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// captureTransport records the request it was given and answers 204.
type captureTransport struct{ got *http.Request }

func (c *captureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.got = r
	return &http.Response{
		StatusCode: http.StatusNoContent,
		Body:       http.NoBody,
		Header:     http.Header{},
		Request:    r,
	}, nil
}

// SHIP-14's acceptance criterion, first half: the ID flows from the middleware into the
// logs — into every record, not only the request line.
func TestLoggerBindsTheRequestIDToEveryRecord(t *testing.T) {
	log, buf := jsonLogger()

	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// What a domain package does five calls down, knowing nothing about
			// middleware.
			LoggerFrom(r.Context()).InfoContext(r.Context(), "job published",
				slog.String("job_id", "job_123"))
		}),
		RequestID, Logger(log),
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", nil)
	req.Header.Set(HeaderRequestID, "known-request-id")
	h.ServeHTTP(httptest.NewRecorder(), req)

	handlerRec := recordWithMessage(t, buf, "job published")
	if handlerRec["request_id"] != "known-request-id" {
		t.Errorf("handler record request_id = %v, want it bound automatically", handlerRec["request_id"])
	}
	if handlerRec["job_id"] != "job_123" {
		t.Errorf("handler record lost its own attributes: %v", handlerRec)
	}

	// The request summary must still carry it too.
	if reqRec := recordWithMessage(t, buf, "http request"); reqRec["request_id"] != "known-request-id" {
		t.Errorf("request record request_id = %v", reqRec["request_id"])
	}
}

// Code that logs should not have to guard against a context that never went through the
// middleware — a test, a migration, a background task started before wiring.
func TestLoggerFromFallsBackRatherThanReturningNil(t *testing.T) {
	if got := LoggerFrom(context.Background()); got == nil {
		t.Fatal("LoggerFrom returned nil")
	}
	// And it must not panic when used.
	LoggerFrom(context.Background()).Debug("no middleware here")
}

func TestLoggerFromReturnsWhatWasPutOnTheContext(t *testing.T) {
	log, _ := jsonLogger()
	ctx := ContextWithLogger(context.Background(), log)

	if got := LoggerFrom(ctx); got != log {
		t.Error("LoggerFrom did not return the logger from the context")
	}
}

// SHIP-14's acceptance criterion, second half: the ID flows into downstream calls.
func TestPropagateRequestIDSetsTheHeaderOnOutboundCalls(t *testing.T) {
	capture := &captureTransport{}
	client := &http.Client{Transport: PropagateRequestID(capture)}

	ctx := ContextWithRequestID(context.Background(), "known-request-id")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://email.example/send", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(req); err != nil {
		t.Fatal(err)
	}

	if got := capture.got.Header.Get(HeaderRequestID); got != "known-request-id" {
		t.Errorf("outbound %s = %q, want the ID from the context", HeaderRequestID, got)
	}
}

// The whole point is that an inbound request and the calls it causes share one ID.
func TestPropagateRequestIDCarriesTheInboundIDOutbound(t *testing.T) {
	capture := &captureTransport{}
	client := &http.Client{Transport: PropagateRequestID(capture)}

	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://maps.example/geocode", nil)
		if err != nil {
			t.Error(err)
			return
		}
		if _, err := client.Do(out); err != nil {
			t.Error(err)
		}
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/jobs", nil))

	inbound := rec.Header().Get(HeaderRequestID)
	if inbound == "" {
		t.Fatal("no request ID on the inbound response")
	}
	if got := capture.got.Header.Get(HeaderRequestID); got != inbound {
		t.Errorf("outbound ID = %q, inbound ID = %q; want them equal", got, inbound)
	}
}

func TestPropagateRequestIDLeavesAnExplicitHeaderAlone(t *testing.T) {
	capture := &captureTransport{}
	client := &http.Client{Transport: PropagateRequestID(capture)}

	ctx := ContextWithRequestID(context.Background(), "from-the-context")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(HeaderRequestID, "set-by-the-caller")

	if _, err := client.Do(req); err != nil {
		t.Fatal(err)
	}
	if got := capture.got.Header.Get(HeaderRequestID); got != "set-by-the-caller" {
		t.Errorf("%s = %q, want the caller's own value kept", HeaderRequestID, got)
	}
}

// A context without an ID is not a reason to fail, or to invent one: the outbound call
// still has to happen.
func TestPropagateRequestIDIsANoopWithoutAnID(t *testing.T) {
	capture := &captureTransport{}
	client := &http.Client{Transport: PropagateRequestID(capture)}

	resp, err := client.Get("https://example.test/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := capture.got.Header.Get(HeaderRequestID); got != "" {
		t.Errorf("%s = %q, want no header at all", HeaderRequestID, got)
	}
}

// RoundTrip is documented not to modify the request it is handed. net/http may still be
// holding it, and a retry would otherwise see one that has already been altered.
func TestPropagateRequestIDDoesNotMutateTheCallersRequest(t *testing.T) {
	client := &http.Client{Transport: PropagateRequestID(&captureTransport{})}

	ctx := ContextWithRequestID(context.Background(), "known-request-id")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(req); err != nil {
		t.Fatal(err)
	}

	if got := req.Header.Get(HeaderRequestID); got != "" {
		t.Errorf("the caller's request was modified: %s = %q", HeaderRequestID, got)
	}
}

// Scheduled tasks and event consumers have no inbound request to inherit from, and their
// logs still have to be findable.
func TestBackgroundWorkCanJoinTheSameTrail(t *testing.T) {
	log, buf := jsonLogger()

	id := NewRequestID()
	ctx := ContextWithLogger(ContextWithRequestID(context.Background(), id),
		log.With(slog.String("request_id", id)))

	LoggerFrom(ctx).InfoContext(ctx, "job expired")

	if got := recordWithMessage(t, buf, "job expired")["request_id"]; got != id {
		t.Errorf("request_id = %v, want %q", got, id)
	}
}
