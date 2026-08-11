package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// H and DecodeJSON were specified in Docs/10 §4.3 from SHIP-15a and written for the first time
// at SHIP-15e. These are the tests neither had while they lived unexported in internal/identity.

func TestHWritesAReturnedErrorThroughTheContract(t *testing.T) {
	h := H(func(http.ResponseWriter, *http.Request) error {
		return NewError(http.StatusConflict, CodeConflict, "That has already happened.")
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/jobs", nil))

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if got := decodeError(t, rec.Body.Bytes()); got.Code != CodeConflict {
		t.Errorf("code = %q, want %q", got.Code, CodeConflict)
	}
}

// An error that is not an *Error is still the contract, because WriteError decides that. A
// handler returning a bare sentinel must not leak it to the client.
func TestHDoesNotLeakAnUnwrappedError(t *testing.T) {
	h := H(func(http.ResponseWriter, *http.Request) error {
		return errors.New("connection refused to 10.0.0.4:5432")
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/jobs", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if strings.Contains(rec.Body.String(), "10.0.0.4") {
		t.Errorf("the internal error reached the client: %s", rec.Body)
	}
}

// The defect the signature exists to remove: a handler that writes and then returns nil must
// produce exactly one body. If H wrote unconditionally, this would produce two.
func TestHWritesNothingWhenTheHandlerSucceeds(t *testing.T) {
	h := H(func(w http.ResponseWriter, _ *http.Request) error {
		WriteJSON(w, http.StatusCreated, map[string]string{"id": "job-1"})
		return nil
	})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/jobs", nil))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}
	if body := rec.Body.String(); body != `{"id":"job-1"}` {
		t.Errorf("body = %s, want exactly one document", body)
	}
}

func TestDecodeJSONAcceptsAWellFormedBody(t *testing.T) {
	var got struct {
		Email string `json:"email"`
	}

	r := jsonRequest(`{"email":"someone@example.com"}`)
	if err := DecodeJSON(r, &got); err != nil {
		t.Fatalf("DecodeJSON: %v", err)
	}
	if got.Email != "someone@example.com" {
		t.Errorf("email = %q", got.Email)
	}
}

func TestDecodeJSONRefusesWhatItDoesNotUnderstand(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		body        string
		status      int
		code        Code
		// mentions is a fragment the message must carry, where the point of the
		// refusal is telling the client which field it got wrong.
		mentions string
	}{
		{
			// The typo case Docs/10 §4.3 names: a client that sends `pasword`
			// must be told, not silently registered with an empty password.
			name:     "an unknown field",
			body:     `{"email":"a@example.com","pasword":"x"}`,
			status:   http.StatusBadRequest,
			code:     CodeBadRequest,
			mentions: "pasword",
		},
		{
			name:     "a field of the wrong type",
			body:     `{"email":42}`,
			status:   http.StatusBadRequest,
			code:     CodeBadRequest,
			mentions: "email",
		},
		{
			name:   "an empty body",
			body:   "",
			status: http.StatusBadRequest,
			code:   CodeBadRequest,
		},
		{
			name:   "malformed JSON",
			body:   `{"email":`,
			status: http.StatusBadRequest,
			code:   CodeBadRequest,
		},
		{
			// Two documents in one body: the second is silently ignored by a
			// plain Decode, so a client sending both gets a success for the one
			// it did not mean.
			name:   "a second document",
			body:   `{"email":"a@example.com"}{"email":"b@example.com"}`,
			status: http.StatusBadRequest,
			code:   CodeBadRequest,
		},
		{
			name:        "a body that is not JSON at all",
			contentType: "application/x-www-form-urlencoded",
			body:        "email=a@example.com",
			status:      http.StatusUnsupportedMediaType,
			code:        CodeUnsupportedMediaType,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var into struct {
				Email string `json:"email"`
			}

			r := jsonRequest(tc.body)
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}

			err := DecodeJSON(r, &into)
			if err == nil {
				t.Fatal("DecodeJSON accepted it")
			}

			var apiErr *Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("error is not in the contract: %v", err)
			}
			if apiErr.Status != tc.status {
				t.Errorf("status = %d, want %d", apiErr.Status, tc.status)
			}
			if apiErr.Code != tc.code {
				t.Errorf("code = %q, want %q", apiErr.Code, tc.code)
			}
			if tc.mentions != "" && !strings.Contains(apiErr.Message, tc.mentions) {
				t.Errorf("message %q does not name %q", apiErr.Message, tc.mentions)
			}
		})
	}
}

// A charset parameter is part of the media type and must not make the body unreadable.
func TestDecodeJSONAcceptsAContentTypeWithParameters(t *testing.T) {
	var into struct {
		Email string `json:"email"`
	}

	r := jsonRequest(`{"email":"a@example.com"}`)
	r.Header.Set("Content-Type", "application/JSON; charset=utf-8")

	if err := DecodeJSON(r, &into); err != nil {
		t.Fatalf("DecodeJSON: %v", err)
	}
}

// The limit is shared with the idempotency middleware, which reads and fingerprints the body
// first. They are one constant so they cannot drift; this checks the shared value is actually
// enforced on the handler's side rather than only on the fingerprint's (Docs/10 §4.3).
func TestDecodeJSONRefusesABodyOverTheSharedLimit(t *testing.T) {
	var into struct {
		Note string `json:"note"`
	}

	oversized := `{"note":"` + strings.Repeat("x", maxRequestBody) + `"}`
	if err := DecodeJSON(jsonRequest(oversized), &into); err == nil {
		t.Fatalf("a body of %d bytes was accepted, over the %d limit", len(oversized), maxRequestBody)
	}

	// And the limit is not so tight that an ordinary body trips it.
	ordinary := `{"note":"` + strings.Repeat("x", 4096) + `"}`
	if err := DecodeJSON(jsonRequest(ordinary), &into); err != nil {
		t.Fatalf("an ordinary %d-byte body was refused: %v", len(ordinary), err)
	}
}

func jsonRequest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/register", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}
