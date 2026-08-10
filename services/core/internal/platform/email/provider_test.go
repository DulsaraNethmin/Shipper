package email

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

const testAPIKey = "test-provider-key"

// SHIP-32's acceptance criterion, second half: it goes to the provider outside development.
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
			t.Errorf("provider received an unreadable body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	p := newTestProvider(t, Options{BaseURL: srv.URL, APIKey: testAPIKey, Sender: "no-reply@shipper.example"})

	if err := p.Send(context.Background(), "customer@example.com", "Your job was awarded", "Details inside."); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if gotMethod != http.MethodPost || gotPath != "/messages" {
		t.Errorf("provider called %s %s, want POST /messages", gotMethod, gotPath)
	}
	if want := "Bearer " + testAPIKey; gotAuth != want {
		t.Errorf("credential = %q, want %q", gotAuth, want)
	}
	want := message{
		From:    "no-reply@shipper.example",
		To:      "customer@example.com",
		Subject: "Your job was awarded",
		Text:    "Details inside.",
	}
	if gotBody != want {
		t.Errorf("body = %+v, want %+v", gotBody, want)
	}
}

// A trailing slash on a configured URL is somebody's typo, not a different endpoint.
func TestProviderToleratesATrailingSlashOnTheBaseURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := newTestProvider(t, Options{BaseURL: srv.URL + "/", APIKey: testAPIKey, Sender: "no-reply@shipper.example"})
	if err := p.Send(context.Background(), "a@example.com", "s", "b"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotPath != "/messages" {
		t.Errorf("path = %q, want /messages", gotPath)
	}
}

// A provider that refuses the message has not sent it, and the caller has to be able to
// tell. Silently succeeding here is how a customer never receives a verification email and
// nothing anywhere reports a problem.
func TestProviderSurfacesANonSuccessResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":"rate limited"}`)
	}))
	defer srv.Close()

	p := newTestProvider(t, Options{BaseURL: srv.URL, APIKey: testAPIKey, Sender: "no-reply@shipper.example"})

	err := p.Send(context.Background(), "customer@example.com", "Subject", "Body")
	if err == nil {
		t.Fatal("Send returned nil for a 429")
	}
	if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error = %q, want the status and the provider's reason", err)
	}
	// The error travels into a log record, and the credential must not travel with it.
	if strings.Contains(err.Error(), testAPIKey) {
		t.Errorf("error leaks the API key: %q", err)
	}
}

// A provider that cannot be reached is a different failure from one that answered, and a
// caller that retries needs both to be errors rather than one being a silent success.
func TestProviderSurfacesATransportFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close() // nothing is listening

	p := newTestProvider(t, Options{BaseURL: srv.URL, APIKey: testAPIKey, Sender: "no-reply@shipper.example"})

	if err := p.Send(context.Background(), "customer@example.com", "Subject", "Body"); err == nil {
		t.Fatal("Send returned nil with no server listening")
	}
}

// The default client wraps httpx.PropagateRequestID, which is what makes a provider call
// traceable back to the request that caused it (SHIP-14). Supplying HTTPClient replaces
// that, so this is the one test that must not.
func TestProviderDefaultClientPropagatesTheRequestID(t *testing.T) {
	var gotID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = r.Header.Get(httpx.HeaderRequestID)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	p, err := NewProvider(Options{BaseURL: srv.URL, APIKey: testAPIKey, Sender: "no-reply@shipper.example"})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	ctx := httpx.ContextWithRequestID(context.Background(), "req-99")
	if err := p.Send(ctx, "customer@example.com", "Subject", "Body"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotID != "req-99" {
		t.Errorf("outbound %s = %q, want req-99", httpx.HeaderRequestID, gotID)
	}
}

// Configuration is refused at construction, not at the first send. A missing credential
// found when the first customer registers is an outage; found at startup it is a failed
// deploy.
func TestNewProviderRefusesIncompleteOptions(t *testing.T) {
	complete := Options{BaseURL: "https://provider.example", APIKey: "k", Sender: "no-reply@shipper.example"}

	cases := map[string]Options{
		"no base URL":     {APIKey: "k", Sender: "s@example.com"},
		"no API key":      {BaseURL: "https://provider.example", Sender: "s@example.com"},
		"no sender":       {BaseURL: "https://provider.example", APIKey: "k"},
		"not a URL":       {BaseURL: "://", APIKey: "k", Sender: "s@example.com"},
		"not http(s)":     {BaseURL: "ftp://provider.example", APIKey: "k", Sender: "s@example.com"},
		"bare host, no s": {BaseURL: "provider.example", APIKey: "k", Sender: "s@example.com"},
	}
	for name, opts := range cases {
		if _, err := NewProvider(opts); err == nil {
			t.Errorf("NewProvider(%s) returned no error", name)
		}
	}

	if _, err := NewProvider(complete); err != nil {
		t.Errorf("NewProvider(complete options): %v", err)
	}
}

// newTestProvider points the adapter at an httptest server through that server's own
// client, so no test in this package can reach a real network.
func newTestProvider(t *testing.T, opts Options) *Provider {
	t.Helper()
	opts.HTTPClient = &http.Client{}
	p, err := NewProvider(opts)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	return p
}
