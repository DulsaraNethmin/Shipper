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

// --- SHIP-187a: the vendor is configuration, not code -------------------------------------

// captureRequest runs one send against a recording server and returns what arrived.
func captureRequest(t *testing.T, opts Options, to, subject, body string) (path, auth, contentType, payload string) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		contentType = r.Header.Get("Content-Type")
		for name := range r.Header {
			if name != "Content-Type" && name != "Accept" && name != "Content-Length" &&
				name != "User-Agent" && name != "Accept-Encoding" {
				if v := r.Header.Get(name); v != "" && strings.Contains(v, testAPIKey) {
					auth = name + ": " + v
				}
			}
		}
		read, _ := io.ReadAll(r.Body)
		payload = string(read)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	opts.BaseURL = server.URL
	if opts.APIKey == "" {
		opts.APIKey = testAPIKey
	}
	if opts.Sender == "" {
		opts.Sender = "no-reply@shipper.com.au"
	}

	provider, err := NewProvider(opts)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if err := provider.Send(context.Background(), to, subject, body); err != nil {
		t.Fatalf("Send: %v", err)
	}
	return path, auth, contentType, payload
}

// The three things vendors differ on — where the request goes, how the credential is
// presented, and what the body looks like — all reachable from configuration.
func TestProviderUsesAConfiguredPathAndCredentialHeader(t *testing.T) {
	path, auth, _, _ := captureRequest(t, Options{
		Path:       "emails",
		AuthHeader: "X-Postmark-Server-Token",
		AuthScheme: AuthSchemeNone,
	}, "driver@example.com", "Verify your email", "code")

	// The leading slash is supplied: a deployment that writes "emails" and one that writes
	// "/emails" must not reach different URLs.
	if path != "/emails" {
		t.Errorf("path = %q, want /emails", path)
	}
	if want := "X-Postmark-Server-Token: " + testAPIKey; auth != want {
		t.Errorf("credential header = %q, want %q", auth, want)
	}
}

// Resend differs from the default only in taking an array, which is the shape a template
// has to be able to express for the configurability to be worth anything.
func TestProviderRendersAConfiguredBodyTemplate(t *testing.T) {
	_, _, _, payload := captureRequest(t, Options{
		Path:         "/emails",
		BodyTemplate: `{"from":{{json .From}},"to":[{{json .To}}],"subject":{{json .Subject}},"text":{{json .Text}}}`,
	}, "driver@example.com", "Verify your email", "Your code is 123456.")

	var got struct {
		From    string   `json:"from"`
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		Text    string   `json:"text"`
	}
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("payload is not the configured shape: %v\n%s", err, payload)
	}
	if len(got.To) != 1 || got.To[0] != "driver@example.com" {
		t.Errorf("to = %v, want a one-element array", got.To)
	}
	if got.Subject != "Verify your email" || got.Text != "Your code is 123456." {
		t.Errorf("payload = %s", payload)
	}
}

// The default template has to survive the values it will actually be given. A subject
// containing a quotation mark is not exotic — it is the first apostrophe in somebody's
// name — and concatenating it unescaped into JSON is an injection into the vendor's API.
func TestProviderEscapesValuesThatWouldBreakTheBody(t *testing.T) {
	_, _, _, payload := captureRequest(t, Options{},
		"driver@example.com",
		`He said "deliver it"`,
		"Line one\nLine \"two\"")

	if !json.Valid([]byte(payload)) {
		t.Fatalf("default template produced invalid JSON:\n%s", payload)
	}

	var got message
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("unmarshalling: %v", err)
	}
	if got.Subject != `He said "deliver it"` {
		t.Errorf("subject round-tripped as %q", got.Subject)
	}
	if got.Text != "Line one\nLine \"two\"" {
		t.Errorf("text round-tripped as %q", got.Text)
	}
}

// A template that parses can still be wrong, and this is the way it is wrong in practice:
// somebody writes "{{.Subject}}" with their own quotes, it works for every subject they
// try, and it breaks on the first one containing a quotation mark. Refused at startup.
func TestNewProviderRefusesABodyTemplateThatCannotRenderJSON(t *testing.T) {
	_, err := NewProvider(Options{
		BaseURL:      "https://api.example.com",
		APIKey:       testAPIKey,
		Sender:       "no-reply@shipper.com.au",
		BodyTemplate: `{"subject":"{{.Subject}}"}`,
	})
	if err == nil {
		t.Fatal("NewProvider accepted a template that renders invalid JSON")
	}
	if !strings.Contains(err.Error(), "{{json .Field}}") {
		t.Errorf("error = %v, want it to name the correct form", err)
	}
}

// A template that does not compile is a deployment that cannot send email, and the moment
// to discover that is before any customer registers.
func TestNewProviderRefusesABodyTemplateThatDoesNotParse(t *testing.T) {
	_, err := NewProvider(Options{
		BaseURL:      "https://api.example.com",
		APIKey:       testAPIKey,
		Sender:       "no-reply@shipper.com.au",
		BodyTemplate: `{"subject":{{json .Subject}`,
	})
	if err == nil {
		t.Fatal("NewProvider accepted a template that does not parse")
	}
}

// A deployment posting something other than JSON is not wrong, and refusing it would make
// the configurability conditional on one content type.
func TestNewProviderDoesNotRequireJSONOfANonJSONContentType(t *testing.T) {
	_, _, contentType, payload := captureRequest(t, Options{
		ContentType:  "application/x-www-form-urlencoded",
		BodyTemplate: `to={{.To}}&subject={{.Subject}}`,
	}, "driver@example.com", "Verify", "code")

	if contentType != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", contentType)
	}
	if payload != "to=driver@example.com&subject=Verify" {
		t.Errorf("payload = %q", payload)
	}
}
