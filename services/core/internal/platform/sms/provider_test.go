package sms

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

const testAPIKey = "test-gateway-key"

// SHIP-35's acceptance criterion, first half: the OTP goes via the gateway in staging.
func TestProviderPostsTheMessage(t *testing.T) {
	var (
		gotMethod string
		gotPath   string
		gotAuth   string
		gotBody   message
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get(authHeader)
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("gateway received an unreadable body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	p := newTestProvider(t, Options{BaseURL: srv.URL, APIKey: testAPIKey, Sender: "SHIPPER"})

	if err := p.Send(context.Background(), "+61400000000", "Your Shipper code is 481920"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if gotMethod != http.MethodPost || gotPath != "/messages" {
		t.Errorf("gateway called %s %s, want POST /messages", gotMethod, gotPath)
	}
	if want := "Bearer " + testAPIKey; gotAuth != want {
		t.Errorf("credential = %q, want %q", gotAuth, want)
	}
	want := message{From: "SHIPPER", To: "+61400000000", Text: "Your Shipper code is 481920"}
	if gotBody != want {
		t.Errorf("body = %+v, want %+v", gotBody, want)
	}
}

// A gateway that refuses the message has not sent it. Silently succeeding is how somebody
// waits for a code that was never dispatched, with nothing anywhere reporting a problem.
func TestProviderSurfacesANonSuccessResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"unroutable destination"}`)
	}))
	defer srv.Close()

	p := newTestProvider(t, Options{BaseURL: srv.URL, APIKey: testAPIKey, Sender: "SHIPPER"})

	err := p.Send(context.Background(), "+61400000000", "Your Shipper code is 481920")
	if err == nil {
		t.Fatal("Send returned nil for a 400")
	}
	if !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "unroutable") {
		t.Errorf("error = %q, want the status and the gateway's reason", err)
	}
	if strings.Contains(err.Error(), testAPIKey) {
		t.Errorf("error leaks the API key: %q", err)
	}
	// An error string reaches a log aggregator (SHIP-174). A one-time code must not.
	if strings.Contains(err.Error(), "481920") {
		t.Errorf("error leaks the message body: %q", err)
	}
}

// A gateway that cannot be reached is a different failure from one that answered, and both
// have to be errors rather than one being a silent success.
func TestProviderSurfacesATransportFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close() // nothing is listening

	p := newTestProvider(t, Options{BaseURL: srv.URL, APIKey: testAPIKey, Sender: "SHIPPER"})

	if err := p.Send(context.Background(), "+61400000000", "code"); err == nil {
		t.Fatal("Send returned nil with no server listening")
	}
}

// The default client wraps httpx.PropagateRequestID, which is what makes a gateway call
// traceable back to the signup that caused it (SHIP-14).
func TestProviderDefaultClientPropagatesTheRequestID(t *testing.T) {
	var gotID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = r.Header.Get(httpx.HeaderRequestID)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	p, err := NewProvider(Options{BaseURL: srv.URL, APIKey: testAPIKey, Sender: "SHIPPER"})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	ctx := httpx.ContextWithRequestID(context.Background(), "req-99")
	if err := p.Send(ctx, "+61400000000", "code"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotID != "req-99" {
		t.Errorf("outbound %s = %q, want req-99", httpx.HeaderRequestID, gotID)
	}
}

// Configuration is refused at construction, not when the first person tries to verify a
// phone number.
func TestNewProviderRefusesIncompleteOptions(t *testing.T) {
	cases := map[string]Options{
		"no base URL": {APIKey: "k", Sender: "SHIPPER"},
		"no API key":  {BaseURL: "https://gateway.example", Sender: "SHIPPER"},
		"no sender":   {BaseURL: "https://gateway.example", APIKey: "k"},
		"not a URL":   {BaseURL: "://", APIKey: "k", Sender: "SHIPPER"},
		"not http(s)": {BaseURL: "ftp://gateway.example", APIKey: "k", Sender: "SHIPPER"},
		"no scheme":   {BaseURL: "gateway.example", APIKey: "k", Sender: "SHIPPER"},
	}
	for name, opts := range cases {
		if _, err := NewProvider(opts); err == nil {
			t.Errorf("NewProvider(%s) returned no error", name)
		}
	}

	complete := Options{BaseURL: "https://gateway.example", APIKey: "k", Sender: "SHIPPER"}
	if _, err := NewProvider(complete); err != nil {
		t.Errorf("NewProvider(complete options): %v", err)
	}
}

// newTestProvider points the adapter at an httptest server, so no test in this package can
// reach a real network.
func newTestProvider(t *testing.T, opts Options) *Provider {
	t.Helper()
	opts.HTTPClient = &http.Client{}
	p, err := NewProvider(opts)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return p
}
