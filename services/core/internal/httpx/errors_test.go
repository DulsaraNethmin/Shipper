package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// decodeError reads a response body as the standard error contract, failing the test if
// it is not in that shape at all.
func decodeError(t *testing.T, body []byte) errorBody {
	t.Helper()

	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("body is not JSON: %v (%s)", err, body)
	}
	raw, ok := envelope["error"]
	if !ok {
		t.Fatalf(`body has no "error" member: %s`, body)
	}

	var got errorBody
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("error member is not the contract: %v (%s)", err, raw)
	}
	return got
}

// SHIP-12's acceptance criterion: all errors return the same shape with a
// machine-readable code.
func TestEveryFailureUsesTheSameShape(t *testing.T) {
	tests := []struct {
		name     string
		handler  http.Handler
		wantCode Code
		wantHTTP int
	}{
		{
			name: "an unmatched path, answered by ServeMux itself",
			handler: func() http.Handler {
				mux := http.NewServeMux()
				mux.Handle("GET /health", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
				return mux
			}(),
			wantCode: CodeNotFound,
			wantHTTP: http.StatusNotFound,
		},
		{
			name: "a method ServeMux does not allow",
			handler: func() http.Handler {
				mux := http.NewServeMux()
				mux.Handle("GET /thing", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
				return mux
			}(),
			wantCode: CodeMethodNotAllowed,
			wantHTTP: http.StatusMethodNotAllowed,
		},
		{
			name: "http.Error, from anywhere in the stack",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "something plain", http.StatusForbidden)
			}),
			wantCode: CodeForbidden,
			wantHTTP: http.StatusForbidden,
		},
		{
			name: "a handler returning a typed error",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				WriteError(w, r, NewError(http.StatusUnprocessableEntity,
					CodeValidationFailed, "The job could not be published."))
			}),
			wantCode: CodeValidationFailed,
			wantHTTP: http.StatusUnprocessableEntity,
		},
		{
			name: "a panic",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic("status was set directly")
			}),
			wantCode: CodeInternal,
			wantHTTP: http.StatusInternalServerError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			log, _ := jsonLogger()
			h := Chain(tc.handler, RequestID, Logger(log), Recover(log), StandardErrors)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/thing", nil)
			req.Header.Set(HeaderRequestID, "known-request-id")
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantHTTP {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantHTTP)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Errorf("Content-Type = %q, want JSON", ct)
			}

			got := decodeError(t, rec.Body.Bytes())
			if got.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", got.Code, tc.wantCode)
			}
			if got.Message == "" {
				t.Error("message was empty")
			}
			if got.RequestID != "known-request-id" {
				t.Errorf("request_id = %q, want it carried in the body", got.RequestID)
			}
		})
	}
}

// A 405 is only useful with the Allow header, and ServeMux is the thing that knows what
// to put in it. Rewriting the body must not cost that.
func TestMethodNotAllowedKeepsTheAllowHeader(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /thing", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	StandardErrors(mux).ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/thing", nil))

	// ServeMux registers HEAD alongside GET, and reports both.
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("Allow = %q, want GET, HEAD", got)
	}
}

// The replaced body has a different length from the one ServeMux was about to write.
func TestReplacingABodyClearsTheStaleContentLength(t *testing.T) {
	h := StandardErrors(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Length", "5")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("plain"))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/thing", nil))

	if got := rec.Header().Get("Content-Length"); got == "5" {
		t.Error("Content-Length still describes the discarded body")
	}
	if strings.Contains(rec.Body.String(), "plain") {
		t.Errorf("the discarded body was written anyway: %s", rec.Body.String())
	}
	if !json.Valid(rec.Body.Bytes()) {
		t.Errorf("body is not valid JSON: %s", rec.Body.String())
	}
}

// A handler that has already said what it means in the contract must be left exactly as
// it is — otherwise a validation error with per-field details would be flattened into a
// generic one on its way out.
func TestAHandlerSuppliedErrorIsNotRewritten(t *testing.T) {
	h := StandardErrors(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, r, NewError(http.StatusUnprocessableEntity, CodeValidationFailed,
			"The job could not be published.").
			WithDetails(FieldError{
				Field:   "goods.category",
				Code:    "prohibited_category",
				Message: "Dangerous goods cannot be listed.",
			}))
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/jobs", nil))

	got := decodeError(t, rec.Body.Bytes())
	if got.Code != CodeValidationFailed {
		t.Errorf("code = %q, want the handler's own", got.Code)
	}
	if len(got.Details) != 1 {
		t.Fatalf("details = %v, want the field error preserved", got.Details)
	}
	if got.Details[0].Field != "goods.category" {
		t.Errorf("details[0].field = %q", got.Details[0].Field)
	}
}

// Successful responses pass through untouched. The middleware sits on every route,
// including the ones that work.
func TestSuccessIsUntouched(t *testing.T) {
	h := StandardErrors(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusCreated, map[string]string{"id": "job_123"})
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/jobs", nil))

	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"id":"job_123"}` {
		t.Errorf("body = %q", got)
	}
}

// An error that was never given a status and a code has not been considered. Guessing on
// its behalf is how a database message reaches a customer.
func TestAnUntypedErrorBecomesAnOpaqueFiveHundred(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/jobs", nil)

	WriteError(rec, req, errors.New(`pq: column "budget_cents" does not exist`))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "budget_cents") {
		t.Errorf("the underlying error reached the client: %s", rec.Body.String())
	}
	if got := decodeError(t, rec.Body.Bytes()); got.Code != CodeInternal {
		t.Errorf("code = %q, want %q", got.Code, CodeInternal)
	}
}

// The other half of the sentence above: the client is told nothing, so the *log* has to be
// told everything, or the failure is invisible from both ends at once (SHIP-15i).
//
// This is not hypothetical. A sign-in answered 500 against a stale local database during
// SHIP-55, and the only route to the cause was reading the handler.
func TestAnUnmappedErrorReachesTheLogWithItsRequestID(t *testing.T) {
	log, buf := jsonLogger()

	// Bound by the Logger middleware (SHIP-14), not by the caller — which is the whole
	// reason this logs through LoggerFrom rather than through a logger of its own.
	log = log.With(slog.String("request_id", "req_7f31"))

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login?email=someone@example.com", nil)
	req = req.WithContext(ContextWithLogger(req.Context(), log))

	WriteError(httptest.NewRecorder(), req, errors.New(`pq: relation "device_sessions" does not exist`))

	rec := recordWithMessage(t, buf, "unmapped error written as 500")

	if got, _ := rec["error"].(string); !strings.Contains(got, "device_sessions") {
		t.Errorf("error = %q, want the cause", got)
	}
	if got := rec["request_id"]; got != "req_7f31" {
		t.Errorf("request_id = %v, want the one the middleware bound", got)
	}
	if got := rec["path"]; got != "/v1/auth/login" {
		t.Errorf("path = %v", got)
	}
	if got := rec["method"]; got != http.MethodPost {
		t.Errorf("method = %v", got)
	}
	if got := rec["level"]; got != "ERROR" {
		t.Errorf("level = %v, want ERROR — a 500 is ours, not the caller's", got)
	}

	// The query string is left out for the same reason Logger leaves it out: it is the part
	// of a URL most likely to carry a token or somebody's address.
	if strings.Contains(buf.String(), "someone@example.com") {
		t.Errorf("the query string reached the log:\n%s", buf.String())
	}
}

// The response contract is published and its silence is deliberate. Logging the cause is an
// observability change and must not become a disclosure one, so this holds the bytes.
func TestLoggingTheCauseDoesNotChangeTheResponse(t *testing.T) {
	const want = `{"error":{"code":"internal_error",` +
		`"message":"Something went wrong at our end. Quote the request ID if you contact support."}}`

	log, buf := jsonLogger()
	req := httptest.NewRequest(http.MethodGet, "/v1/jobs", nil)
	req = req.WithContext(ContextWithLogger(req.Context(), log))

	unmapped := httptest.NewRecorder()
	WriteError(unmapped, req, errors.New(`pq: column "budget_cents" does not exist`))

	if got := unmapped.Body.String(); got != want {
		t.Errorf("body = %q\nwant   %q", got, want)
	}
	if buf.Len() == 0 {
		t.Error("the cause was not logged, so this test is holding the wrong branch unchanged")
	}

	// And byte-identical to the mapped path, which does not log at all: whether an error was
	// considered is something only the log may reveal.
	mapped := httptest.NewRecorder()
	WriteError(mapped, req, StatusError(http.StatusInternalServerError))

	if unmapped.Body.String() != mapped.Body.String() {
		t.Errorf("an unmapped 500 no longer looks like a mapped one:\n  unmapped %s\n  mapped   %s",
			unmapped.Body.String(), mapped.Body.String())
	}
	if unmapped.Header().Get("Content-Type") != mapped.Header().Get("Content-Type") {
		t.Errorf("content type = %q, want %q",
			unmapped.Header().Get("Content-Type"), mapped.Header().Get("Content-Type"))
	}
}

// WriteError already guards its request ID with `if r != nil`, so the logging has to survive
// the same call. A caller writing an error outside a request must not lose the cause as well
// as the correlation — and must certainly not panic while reporting one failure with another.
func TestWritingAnUnmappedErrorSurvivesANilRequest(t *testing.T) {
	log, buf := jsonLogger()
	previous := slog.Default()
	slog.SetDefault(log)
	t.Cleanup(func() { slog.SetDefault(previous) })

	rec := httptest.NewRecorder()
	WriteError(rec, nil, errors.New("the outbox drain has no request"))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if got := decodeError(t, rec.Body.Bytes()); got.RequestID != "" {
		t.Errorf("request_id = %q, want empty with no request", got.RequestID)
	}

	record := recordWithMessage(t, buf, "unmapped error written as 500")
	if got, _ := record["error"].(string); got != "the outbox drain has no request" {
		t.Errorf("error = %q, want the cause on slog.Default", got)
	}
	if _, ok := record["path"]; ok {
		t.Error("a path was logged for a call with no request")
	}
}

// The cause is for the log. It must be reachable through errors.Is and absent from the
// response.
func TestTheCauseIsLoggableAndNeverSerialised(t *testing.T) {
	sentinel := errors.New("no accepted bid for this job")
	err := NewError(http.StatusConflict, CodeConflict, "This job has already been awarded.").
		WithCause(sentinel)

	if !errors.Is(err, sentinel) {
		t.Error("errors.Is did not find the cause")
	}
	if !strings.Contains(err.Error(), sentinel.Error()) {
		t.Errorf("Error() = %q, want the cause included for the log", err.Error())
	}

	rec := httptest.NewRecorder()
	WriteError(rec, httptest.NewRequest(http.MethodPost, "/v1/jobs/1/award", nil), err)

	if strings.Contains(rec.Body.String(), sentinel.Error()) {
		t.Errorf("the cause was serialised: %s", rec.Body.String())
	}
}

// A code is what a client branches on, so the mapping from status to code is part of the
// contract rather than an implementation detail.
func TestCodeForStatus(t *testing.T) {
	tests := []struct {
		status int
		want   Code
	}{
		{http.StatusBadRequest, CodeBadRequest},
		{http.StatusUnauthorized, CodeUnauthenticated},
		{http.StatusForbidden, CodeForbidden},
		{http.StatusNotFound, CodeNotFound},
		{http.StatusMethodNotAllowed, CodeMethodNotAllowed},
		{http.StatusConflict, CodeConflict},
		{http.StatusUnprocessableEntity, CodeValidationFailed},
		{http.StatusTooManyRequests, CodeRateLimited},
		{http.StatusServiceUnavailable, CodeUnavailable},
		{http.StatusInternalServerError, CodeInternal},
		{http.StatusBadGateway, CodeInternal},
		{http.StatusTeapot, CodeBadRequest},
	}

	for _, tc := range tests {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			if got := CodeForStatus(tc.status); got != tc.want {
				t.Errorf("CodeForStatus(%d) = %q, want %q", tc.status, got, tc.want)
			}
		})
	}
}

// Every default message is a sentence someone could be shown, not a repeat of the status
// line.
func TestDefaultMessagesAreWrittenForPeople(t *testing.T) {
	for _, status := range []int{
		http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusConflict,
		http.StatusTooManyRequests, http.StatusServiceUnavailable,
		http.StatusInternalServerError,
	} {
		msg := StatusError(status).Message
		if msg == "" || msg == http.StatusText(status) {
			t.Errorf("%d: message = %q, want a sentence of its own", status, msg)
		}
		if !strings.HasSuffix(msg, ".") {
			t.Errorf("%d: message = %q, want it punctuated", status, msg)
		}
	}
}
