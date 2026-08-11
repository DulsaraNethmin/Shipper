package httpx

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/idempotency"
)

// countingHandler answers with the response it is given and records how many times it
// actually ran.
type countingHandler struct {
	mu     sync.Mutex
	calls  int
	status int
	body   string
	panics bool
}

func (c *countingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()

	if c.panics {
		panic("handler exploded")
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(c.status)
	_, _ = w.Write([]byte(c.body))
}

func (c *countingHandler) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func bidRequest(key, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/jobs/job_123/bids", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set(HeaderIdempotencyKey, key)
	}
	return r
}

// SHIP-15's acceptance criterion, exactly: a repeated key returns the stored original
// response without re-executing.
func TestARepeatedKeyReplaysWithoutReExecuting(t *testing.T) {
	handler := &countingHandler{status: http.StatusCreated, body: `{"bid_id":"bid_9"}`}
	h := Idempotent(idempotency.NewMemoryStore(), nil)(handler)

	first := httptest.NewRecorder()
	h.ServeHTTP(first, bidRequest("key-1", `{"price_aud":450}`))

	second := httptest.NewRecorder()
	h.ServeHTTP(second, bidRequest("key-1", `{"price_aud":450}`))

	if got := handler.callCount(); got != 1 {
		t.Errorf("the handler ran %d times, want 1", got)
	}
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Errorf("statuses = %d and %d, want 201 both times", first.Code, second.Code)
	}
	if first.Body.String() != second.Body.String() {
		t.Errorf("bodies differ:\n first: %s\nsecond: %s", first.Body, second.Body)
	}
	if ct := second.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("replayed Content-Type = %q, want it preserved", ct)
	}

	if first.Header().Get(HeaderIdempotencyReplayed) != "" {
		t.Error("the original response was marked as a replay")
	}
	if second.Header().Get(HeaderIdempotencyReplayed) != "true" {
		t.Errorf("%s = %q on the replay, want true",
			HeaderIdempotencyReplayed, second.Header().Get(HeaderIdempotencyReplayed))
	}
}

// The case this exists for: a phone whose connection drops after the server has already
// done the work. Only the client's view of the outcome was lost.
func TestARetryAfterADroppedConnectionGetsTheOriginalOutcome(t *testing.T) {
	store := idempotency.NewMemoryStore()
	awarded := 0

	h := Idempotent(store, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		awarded++
		WriteJSON(w, http.StatusOK, map[string]string{"status": "Awarded", "bid_id": "bid_9"})
	}))

	var bodies []string
	for range 5 {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, bidRequest("award-key", `{"bid_id":"bid_9"}`))
		bodies = append(bodies, rec.Body.String())
	}

	if awarded != 1 {
		t.Errorf("the award ran %d times, want 1", awarded)
	}
	for i, body := range bodies {
		if body != bodies[0] {
			t.Errorf("retry %d returned a different body:\n%s\n%s", i, bodies[0], body)
		}
	}
}

// Concurrent retries are the realistic case: a queue draining on reconnection sends the
// same operation more than once at nearly the same moment (SHIP-125).
func TestConcurrentRetriesExecuteOnce(t *testing.T) {
	handler := &countingHandler{status: http.StatusCreated, body: `{"bid_id":"bid_9"}`}
	h := Idempotent(idempotency.NewMemoryStore(), nil)(handler)

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.ServeHTTP(httptest.NewRecorder(), bidRequest("key-1", `{"price_aud":450}`))
		}()
	}
	wg.Wait()

	if got := handler.callCount(); got != 1 {
		t.Errorf("the handler ran %d times, want exactly 1", got)
	}
}

// A retry that arrives while the original is still running is told to try again, not
// given a half-formed answer.
func TestARequestStillInFlightIsToldToRetry(t *testing.T) {
	store := idempotency.NewMemoryStore()

	release := make(chan struct{})
	started := make(chan struct{})
	h := Idempotent(store, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		WriteJSON(w, http.StatusCreated, map[string]string{"bid_id": "bid_9"})
	}))

	go h.ServeHTTP(httptest.NewRecorder(), bidRequest("key-1", `{"price_aud":450}`))
	<-started

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, bidRequest("key-1", `{"price_aud":450}`))
	close(release)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	if got := decodeError(t, rec.Body.Bytes()); got.Code != CodeIdempotencyInProgress {
		t.Errorf("code = %q, want %q", got.Code, CodeIdempotencyInProgress)
	}
	if got := rec.Header().Get("Retry-After"); got == "" {
		t.Error("no Retry-After on an in-progress conflict")
	}
}

// Docs/01 §5.2 and Docs/02 §3.1 both say every state-changing request carries a key, so a
// missing one is refused rather than quietly waived.
func TestAStateChangingRequestWithoutAKeyIsRefused(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			handler := &countingHandler{status: http.StatusOK, body: `{}`}
			h := Idempotent(idempotency.NewMemoryStore(), nil)(handler)

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(method, "/jobs", strings.NewReader(`{}`)))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if got := decodeError(t, rec.Body.Bytes()); got.Code != CodeIdempotencyKeyRequired {
				t.Errorf("code = %q, want %q", got.Code, CodeIdempotencyKeyRequired)
			}
			if handler.callCount() != 0 {
				t.Error("the handler ran without a key")
			}
		})
	}
}

func TestReadOnlyMethodsNeedNoKey(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			handler := &countingHandler{status: http.StatusOK, body: `{"jobs":[]}`}
			h := Idempotent(idempotency.NewMemoryStore(), nil)(handler)

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(method, "/jobs", nil))

			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want 200", rec.Code)
			}
			if handler.callCount() != 1 {
				t.Errorf("the handler ran %d times, want 1", handler.callCount())
			}
		})
	}
}

func TestAnUnusableKeyIsRefused(t *testing.T) {
	tests := []struct{ name, key string }{
		{"a space", "key one"},
		{"a newline", "key\none"},
		{"a tab", "key\tone"},
		{"a null byte", "key\x00one"},
		{"non-ascii", "kéy"},
		{"too long", strings.Repeat("k", maxIdempotencyKeyLen+1)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := &countingHandler{status: http.StatusOK, body: `{}`}
			h := Idempotent(idempotency.NewMemoryStore(), nil)(handler)

			req := bidRequest("", `{}`)
			// Set directly: http.Header.Set would not carry a raw control character.
			req.Header[HeaderIdempotencyKey] = []string{tc.key}

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", rec.Code)
			}
			if handler.callCount() != 0 {
				t.Error("the handler ran with an unusable key")
			}
		})
	}
}

// A key reused for a different request is a client defect. Replaying the first response
// would tell the client that something it never sent had succeeded.
func TestAKeyReusedForADifferentRequestIsRefused(t *testing.T) {
	tests := []struct {
		name    string
		request func() *http.Request
	}{
		{"a different body", func() *http.Request { return bidRequest("key-1", `{"price_aud":999}`) }},
		{"a different path", func() *http.Request {
			r := httptest.NewRequest(http.MethodPost, "/jobs/job_456/bids", strings.NewReader(`{"price_aud":450}`))
			r.Header.Set(HeaderIdempotencyKey, "key-1")
			return r
		}},
		{"a different method", func() *http.Request {
			r := httptest.NewRequest(http.MethodDelete, "/jobs/job_123/bids", strings.NewReader(`{"price_aud":450}`))
			r.Header.Set(HeaderIdempotencyKey, "key-1")
			return r
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := &countingHandler{status: http.StatusCreated, body: `{"bid_id":"bid_9"}`}
			h := Idempotent(idempotency.NewMemoryStore(), nil)(handler)

			h.ServeHTTP(httptest.NewRecorder(), bidRequest("key-1", `{"price_aud":450}`))

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, tc.request())

			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409 (%s)", rec.Code, rec.Body)
			}
			if got := decodeError(t, rec.Body.Bytes()); got.Code != CodeIdempotencyKeyReused {
				t.Errorf("code = %q, want %q", got.Code, CodeIdempotencyKeyReused)
			}
			if handler.callCount() != 1 {
				t.Errorf("the handler ran %d times, want only the first request", handler.callCount())
			}
		})
	}
}

// Two callers using the same key must not see each other's responses. Until SHIP-44
// supplies the authenticated subject there is nothing to separate, which is why the
// separation is tested now rather than assumed later.
func TestScopesAreSeparate(t *testing.T) {
	handler := &countingHandler{status: http.StatusCreated, body: `{"bid_id":"bid_9"}`}

	var caller string
	h := Idempotent(idempotency.NewMemoryStore(), func(*http.Request) string { return caller })(handler)

	caller = "user_a"
	h.ServeHTTP(httptest.NewRecorder(), bidRequest("shared-key", `{"price_aud":450}`))

	caller = "user_b"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, bidRequest("shared-key", `{"price_aud":450}`))

	if rec.Header().Get(HeaderIdempotencyReplayed) == "true" {
		t.Error("one caller was served another caller's stored response")
	}
	if handler.callCount() != 2 {
		t.Errorf("the handler ran %d times, want once per caller", handler.callCount())
	}
}

// A 4xx is a settled outcome. Replaying it is what stops a retry loop turning one
// rejected bid into ten log entries and ten notifications.
func TestAClientErrorIsReplayed(t *testing.T) {
	handler := &countingHandler{status: http.StatusUnprocessableEntity, body: `{"error":{"code":"validation_failed"}}`}
	h := Idempotent(idempotency.NewMemoryStore(), nil)(handler)

	h.ServeHTTP(httptest.NewRecorder(), bidRequest("key-1", `{}`))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, bidRequest("key-1", `{}`))

	if handler.callCount() != 1 {
		t.Errorf("the handler ran %d times, want 1", handler.callCount())
	}
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want the original 422", rec.Code)
	}
}

// A 5xx says "this might work if you try again". Holding the key would turn a transient
// failure into one the client cannot retry past.
func TestAServerErrorReleasesTheKey(t *testing.T) {
	store := idempotency.NewMemoryStore()
	handler := &countingHandler{status: http.StatusInternalServerError, body: `{"error":{"code":"internal_error"}}`}
	h := Idempotent(store, nil)(handler)

	h.ServeHTTP(httptest.NewRecorder(), bidRequest("key-1", `{}`))

	if store.Held(idempotencyKeyPrefix + "anonymous:key-1") {
		t.Error("the key is still held after a 500")
	}

	handler.status = http.StatusCreated
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, bidRequest("key-1", `{}`))

	if handler.callCount() != 2 {
		t.Errorf("the handler ran %d times, want the retry to execute", handler.callCount())
	}
	if rec.Code != http.StatusCreated {
		t.Errorf("status = %d, want the retry's own 201", rec.Code)
	}
}

// A panic unwinds through this middleware on its way to Recover. If the key were left
// held, every retry would be refused until the in-flight TTL expired.
func TestAPanicReleasesTheKey(t *testing.T) {
	store := idempotency.NewMemoryStore()
	log, _ := jsonLogger()

	handler := &countingHandler{panics: true}
	h := Chain(handler, Recover(log), Idempotent(store, nil))

	h.ServeHTTP(httptest.NewRecorder(), bidRequest("key-1", `{}`))

	if store.Held(idempotencyKeyPrefix + "anonymous:key-1") {
		t.Error("the key is still held after a panic")
	}
}

// Fail closed. Executing without the guarantee is the more available choice and the wrong
// one: a request arriving while Redis is down is disproportionately likely to be a retry.
func TestAnUnreachableStoreRefusesTheRequest(t *testing.T) {
	store := idempotency.NewMemoryStore()
	store.FailWith = errors.New("dial tcp 127.0.0.1:6379: connect: connection refused")

	handler := &countingHandler{status: http.StatusCreated, body: `{}`}
	h := Idempotent(store, nil)(handler)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, bidRequest("key-1", `{}`))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if got := decodeError(t, rec.Body.Bytes()); got.Code != CodeUnavailable {
		t.Errorf("code = %q, want %q", got.Code, CodeUnavailable)
	}
	if handler.callCount() != 0 {
		t.Error("the handler ran without the idempotency guarantee")
	}
	if strings.Contains(rec.Body.String(), "127.0.0.1") {
		t.Errorf("the connection error reached the client: %s", rec.Body)
	}
}

// The middleware reads the body to fingerprint it, and the handler still has to be able
// to read the same body afterwards.
func TestTheHandlerStillSeesTheBody(t *testing.T) {
	var seen string
	h := Idempotent(idempotency.NewMemoryStore(), nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		seen = string(b)
		WriteJSON(w, http.StatusCreated, map[string]string{"ok": "true"})
	}))

	const body = `{"price_aud":450,"notes":"tail lift required"}`
	h.ServeHTTP(httptest.NewRecorder(), bidRequest("key-1", body))

	if seen != body {
		t.Errorf("the handler read %q, want %q", seen, body)
	}
}

func TestAnOversizedRequestBodyIsRejected(t *testing.T) {
	handler := &countingHandler{status: http.StatusCreated, body: `{}`}
	h := Idempotent(idempotency.NewMemoryStore(), nil)(handler)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, bidRequest("key-1", strings.Repeat("x", maxRequestBody+1)))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if got := decodeError(t, rec.Body.Bytes()); got.Code != CodePayloadTooLarge {
		t.Errorf("code = %q, want %q", got.Code, CodePayloadTooLarge)
	}
	if handler.callCount() != 0 {
		t.Error("the handler ran on an oversized body")
	}
}

// An oversized response is still sent — the work happened and the client needs the answer
// — but it is not held in a shared cache. The retry then re-executes.
func TestAnOversizedResponseIsSentButNotStored(t *testing.T) {
	store := idempotency.NewMemoryStore()
	handler := &countingHandler{
		status: http.StatusOK,
		body:   strings.Repeat("x", maxReplayableResponse+1),
	}
	h := Idempotent(store, nil)(handler)

	first := httptest.NewRecorder()
	h.ServeHTTP(first, bidRequest("key-1", `{}`))

	if first.Code != http.StatusOK || first.Body.Len() != maxReplayableResponse+1 {
		t.Errorf("the oversized response was not delivered: status %d, %d bytes",
			first.Code, first.Body.Len())
	}
	if store.Held(idempotencyKeyPrefix + "anonymous:key-1") {
		t.Error("an oversized response was stored")
	}

	h.ServeHTTP(httptest.NewRecorder(), bidRequest("key-1", `{}`))
	if handler.callCount() != 2 {
		t.Errorf("the handler ran %d times, want the retry to execute", handler.callCount())
	}
}

// The whole point of a fingerprint is that it does not carry the request. Bodies contain
// addresses and recipients' names, and this is a shared cache.
func TestTheFingerprintDoesNotCarryTheRequest(t *testing.T) {
	const body = `{"drop_off":{"address":"12 Wattle St, Carlton VIC 3053"},"recipient":"J Smith"}`

	got := fingerprintRequest(http.MethodPost, "/jobs", []byte(body))

	for _, secret := range []string{"Wattle", "Carlton", "Smith", "3053"} {
		if strings.Contains(got, secret) {
			t.Errorf("the fingerprint contains %q: %s", secret, got)
		}
	}
	if len(got) != 64 {
		t.Errorf("fingerprint = %q, want a sha256 digest in hex", got)
	}
	if same := fingerprintRequest(http.MethodPost, "/jobs", []byte(body)); same != got {
		t.Error("the fingerprint is not stable for the same request")
	}
}
