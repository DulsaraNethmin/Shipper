package httpx

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// jsonLogger returns a logger writing JSON records into buf, and a helper that decodes
// the record with the given message.
func jsonLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

func recordWithMessage(t *testing.T, buf *bytes.Buffer, msg string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line %q is not JSON: %v", line, err)
		}
		if rec["msg"] == msg {
			return rec
		}
	}
	t.Fatalf("no log record with msg=%q in:\n%s", msg, buf.String())
	return nil
}

func TestRequestIDIsGeneratedWhenAbsent(t *testing.T) {
	var seen string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(seen) {
		t.Errorf("generated request ID was %q, want 32 hex characters", seen)
	}
	if got := rec.Header().Get(HeaderRequestID); got != seen {
		t.Errorf("response header %s = %q, want the context value %q", HeaderRequestID, got, seen)
	}
}

func TestRequestIDIsUniquePerRequest(t *testing.T) {
	var ids []string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids = append(ids, RequestIDFrom(r.Context()))
	}))

	for range 100 {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))
	}

	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("request ID %q was issued more than once", id)
		}
		seen[id] = true
	}
}

// A client retrying a dropped request wants both attempts to correlate, so a usable
// supplied ID is kept.
func TestRequestIDHonoursAUsableClientValue(t *testing.T) {
	const supplied = "01HQ8Z3K4M5N6P7Q8R9S0T1U2V"

	var seen string
	h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
	}))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set(HeaderRequestID, supplied)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if seen != supplied {
		t.Errorf("request ID = %q, want the supplied %q", seen, supplied)
	}
	if got := rec.Header().Get(HeaderRequestID); got != supplied {
		t.Errorf("response header = %q, want it echoed as %q", got, supplied)
	}
}

// The ID ends up in a log record, so an unvalidated header is a log-injection vector.
func TestRequestIDRejectsUnsuitableClientValues(t *testing.T) {
	tests := []struct {
		name, supplied string
	}{
		{"newline", "abc\ndef"},
		{"carriage return", "abc\rdef"},
		{"embedded json", `abc","level":"ERROR","msg":"fake`},
		{"space", "abc def"},
		{"tab", "abc\tdef"},
		{"null byte", "abc\x00def"},
		{"non-ascii", "abcédef"},
		{"too long", strings.Repeat("a", maxRequestIDLen+1)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var seen string
			h := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen = RequestIDFrom(r.Context())
			}))

			req := httptest.NewRequest(http.MethodGet, "/health", nil)
			// Set directly: http.Header.Set would not carry a raw control character.
			req.Header[HeaderRequestID] = []string{tc.supplied}
			h.ServeHTTP(httptest.NewRecorder(), req)

			if seen == tc.supplied {
				t.Errorf("request ID = %q, want the unsuitable value replaced", seen)
			}
			if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(seen) {
				t.Errorf("replacement request ID was %q, want a generated one", seen)
			}
		})
	}
}

func TestRequestIDFromReturnsEmptyWithoutMiddleware(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	if got := RequestIDFrom(req.Context()); got != "" {
		t.Errorf("RequestIDFrom() = %q, want empty", got)
	}
}

// SHIP-9's acceptance criterion, exactly: method, path, status, duration, request ID.
func TestLoggerRecordsTheRequiredFields(t *testing.T) {
	log, buf := jsonLogger()

	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
			_, _ = w.Write([]byte("hello"))
		}),
		RequestID,
		Logger(log),
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/jobs?token=secret", nil)
	req.Header.Set(HeaderRequestID, "known-request-id")
	h.ServeHTTP(httptest.NewRecorder(), req)

	rec := recordWithMessage(t, buf, "http request")

	if rec["method"] != http.MethodPost {
		t.Errorf("method = %v, want POST", rec["method"])
	}
	if rec["path"] != "/v1/jobs" {
		t.Errorf("path = %v, want /v1/jobs", rec["path"])
	}
	if rec["status"] != float64(http.StatusTeapot) {
		t.Errorf("status = %v, want %d", rec["status"], http.StatusTeapot)
	}
	if rec["request_id"] != "known-request-id" {
		t.Errorf("request_id = %v, want known-request-id", rec["request_id"])
	}
	if rec["bytes"] != float64(5) {
		t.Errorf("bytes = %v, want 5", rec["bytes"])
	}
	if _, ok := rec["duration_ms"].(float64); !ok {
		t.Errorf("duration_ms = %v, want a number", rec["duration_ms"])
	}
}

// The query string is where tokens and personal data end up, and this record goes to a
// log aggregator.
func TestLoggerOmitsTheQueryString(t *testing.T) {
	log, buf := jsonLogger()

	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		RequestID, Logger(log),
	)
	h.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/v1/jobs?access_token=super-secret", nil))

	if strings.Contains(buf.String(), "super-secret") {
		t.Errorf("log contained the query string:\n%s", buf.String())
	}
}

func TestLoggerDefaultsToTwoHundredWhenTheHandlerNeverSetsAStatus(t *testing.T) {
	log, buf := jsonLogger()

	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), RequestID, Logger(log))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec := recordWithMessage(t, buf, "http request"); rec["status"] != float64(200) {
		t.Errorf("status = %v, want 200", rec["status"])
	}
}

func TestLoggerLevelFollowsTheOutcome(t *testing.T) {
	tests := []struct {
		status    int
		wantLevel string
	}{
		{http.StatusOK, "INFO"},
		{http.StatusMovedPermanently, "INFO"},
		{http.StatusNotFound, "WARN"},
		{http.StatusUnprocessableEntity, "WARN"},
		{http.StatusInternalServerError, "ERROR"},
		{http.StatusBadGateway, "ERROR"},
	}

	for _, tc := range tests {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			log, buf := jsonLogger()
			h := Chain(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tc.status)
				}),
				RequestID, Logger(log),
			)
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

			if got := recordWithMessage(t, buf, "http request")["level"]; got != tc.wantLevel {
				t.Errorf("level for %d = %v, want %s", tc.status, got, tc.wantLevel)
			}
		})
	}
}

// A duplicate WriteHeader is a programming error net/http already warns about; the
// recorder must report the first status rather than the last.
func TestRecorderKeepsTheFirstStatus(t *testing.T) {
	log, buf := jsonLogger()

	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			w.WriteHeader(http.StatusInternalServerError)
		}),
		RequestID, Logger(log),
	)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	if got := recordWithMessage(t, buf, "http request")["status"]; got != float64(http.StatusCreated) {
		t.Errorf("status = %v, want 201", got)
	}
}

func TestRecoverTurnsAPanicIntoFiveHundred(t *testing.T) {
	log, buf := jsonLogger()

	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("job status was set directly")
		}),
		RequestID, Logger(log), Recover(log),
	)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/jobs", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want JSON", ct)
	}

	// The panic message must not reach the client.
	if strings.Contains(rec.Body.String(), "job status was set directly") {
		t.Errorf("response body leaked the panic message: %s", rec.Body.String())
	}

	panicRec := recordWithMessage(t, buf, "panic recovered")
	if panicRec["panic"] != "job status was set directly" {
		t.Errorf("panic = %v, want the message logged", panicRec["panic"])
	}
	if stack, _ := panicRec["stack"].(string); stack == "" {
		t.Error("panic record carried no stack trace")
	}

	// The request itself must still produce its own record, logged at ERROR.
	if got := recordWithMessage(t, buf, "http request")["status"]; got != float64(500) {
		t.Errorf("request record status = %v, want 500", got)
	}
}

func TestRecoverPassesAbortHandlerThrough(t *testing.T) {
	log, _ := jsonLogger()

	h := Recover(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		if rec := recover(); rec != http.ErrAbortHandler {
			t.Errorf("recovered %v, want ErrAbortHandler to propagate to net/http", rec)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
}

func TestWriteJSON(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusCreated, map[string]string{"id": "job_123"})

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"id":"job_123"}` {
		t.Errorf("body = %q", got)
	}
}

// Marshalling into a buffer first is what keeps a broken value from producing a 200 with
// a truncated body.
func TestWriteJSONFailsCleanlyOnAnUnmarshallableValue(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusOK, map[string]any{"ch": make(chan int)})

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if !json.Valid(rec.Body.Bytes()) {
		t.Errorf("body %q is not valid JSON", rec.Body.String())
	}
}

func TestChainAppliesOutermostFirst(t *testing.T) {
	var order []string

	mark := func(name string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}

	h := Chain(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { order = append(order, "handler") }),
		mark("first"), mark("second"),
	)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	want := []string{"first", "second", "handler"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", order, want)
	}
}
