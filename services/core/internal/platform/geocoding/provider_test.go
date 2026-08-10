package geocoding

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

const testAPIKey = "test-maps-key"

// SHIP-59a's acceptance criterion, first half: provider-backed in staging.
func TestProviderResolvesAnAddress(t *testing.T) {
	var gotMethod, gotPath, gotQuery, gotAuth string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotQuery, gotAuth = r.URL.Query().Get("address"), r.Header.Get(authHeader)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[
			{"latitude":-33.867487,"longitude":151.20699,"formatted_address":"1 Martin Pl, Sydney NSW 2000, Australia"},
			{"latitude":-37.813629,"longitude":144.963058,"formatted_address":"Somewhere else entirely"}
		]}`)
	}))
	defer srv.Close()

	p := newTestProvider(t, Options{BaseURL: srv.URL, APIKey: testAPIKey})

	lat, lng, formatted, found, err := p.Lookup(context.Background(), sydney)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !found {
		t.Fatal("found = false for an address the provider resolved")
	}

	if gotMethod != http.MethodGet || gotPath != "/geocode" {
		t.Errorf("provider called %s %s, want GET /geocode", gotMethod, gotPath)
	}
	if gotQuery != sydney {
		t.Errorf("address query = %q, want %q", gotQuery, sydney)
	}
	if want := "Bearer " + testAPIKey; gotAuth != want {
		t.Errorf("credential = %q, want %q", gotAuth, want)
	}

	// The first candidate, and only the first: ranking alternatives is SHIP-60's decision.
	if lat != -33.867487 || lng != 151.20699 {
		t.Errorf("coordinates = (%v, %v), want the first candidate", lat, lng)
	}
	if formatted != "1 Martin Pl, Sydney NSW 2000, Australia" {
		t.Errorf("formatted = %q, want the provider's normalised form", formatted)
	}
}

// SHIP-59a's acceptance criterion: a failed lookup does not fail the job. An address the
// provider does not recognise comes back as an outcome the caller can act on, not an error
// it has to parse.
func TestProviderReportsAnUnrecognisedAddressAsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"results":[]}`)
	}))
	defer srv.Close()

	p := newTestProvider(t, Options{BaseURL: srv.URL, APIKey: testAPIKey})

	lat, lng, formatted, found, err := p.Lookup(context.Background(), "Lot 7, Unnamed Road, Innamincka SA 5731")
	if err != nil {
		t.Fatalf("an unrecognised address must not be an error: %v", err)
	}
	if found {
		t.Fatal("found = true with no results")
	}
	if lat != 0 || lng != 0 || formatted != "" {
		t.Errorf("a not-found lookup returned data: (%v, %v, %q)", lat, lng, formatted)
	}
}

// The distinction the domain depends on: not-found is nil, a lookup that never happened is
// an error. If these collapsed into one, SHIP-60 would either publish jobs with silently
// missing coordinates or refuse legitimate rural addresses.
func TestProviderDistinguishesNotFoundFromFailure(t *testing.T) {
	t.Run("provider refuses the lookup", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"error":"key not authorised"}`)
		}))
		defer srv.Close()

		p := newTestProvider(t, Options{BaseURL: srv.URL, APIKey: testAPIKey})
		_, _, _, found, err := p.Lookup(context.Background(), sydney)
		if err == nil {
			t.Fatal("a 403 came back as a successful lookup")
		}
		if found {
			t.Error("found = true alongside an error")
		}
		if !strings.Contains(err.Error(), "403") {
			t.Errorf("error = %q, want the status in it", err)
		}
		if strings.Contains(err.Error(), testAPIKey) {
			t.Errorf("error leaks the API key: %q", err)
		}
	})

	// A 404 is the endpoint being wrong, not the address being unknown. Reading it as
	// not-found would turn a misconfigured base URL into every address in the country
	// quietly failing to resolve.
	t.Run("a 404 is a failure, not a not-found", func(t *testing.T) {
		srv := httptest.NewServer(http.NotFoundHandler())
		defer srv.Close()

		p := newTestProvider(t, Options{BaseURL: srv.URL, APIKey: testAPIKey})
		if _, _, _, _, err := p.Lookup(context.Background(), sydney); err == nil {
			t.Fatal("a 404 came back as a successful lookup")
		}
	})

	t.Run("provider cannot be reached", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		srv.Close() // nothing is listening

		p := newTestProvider(t, Options{BaseURL: srv.URL, APIKey: testAPIKey})
		if _, _, _, _, err := p.Lookup(context.Background(), sydney); err == nil {
			t.Fatal("Lookup returned nil with no server listening")
		}
	})

	t.Run("provider answers something else entirely", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `<html>maintenance</html>`)
		}))
		defer srv.Close()

		p := newTestProvider(t, Options{BaseURL: srv.URL, APIKey: testAPIKey})
		if _, _, _, _, err := p.Lookup(context.Background(), sydney); err == nil {
			t.Fatal("an unparseable body came back as a successful lookup")
		}
	})
}

// Returning a plausible coordinate somewhere else is the one outcome this package must
// never produce, and two fields in the wrong order is how it would happen.
func TestProviderRefusesAnImpossibleCoordinate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Latitude and longitude swapped: 151 is not a latitude.
		_, _ = io.WriteString(w, `{"results":[{"latitude":151.20699,"longitude":-33.867487}]}`)
	}))
	defer srv.Close()

	p := newTestProvider(t, Options{BaseURL: srv.URL, APIKey: testAPIKey})

	_, _, _, found, err := p.Lookup(context.Background(), sydney)
	if err == nil {
		t.Fatal("an impossible coordinate was accepted")
	}
	if found {
		t.Error("found = true alongside an error")
	}
}

// A provider that resolves the address but does not echo a normalised form leaves the
// caller with what the customer typed, which is better than nothing at all.
func TestProviderFallsBackToTheGivenAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"results":[{"latitude":-33.867487,"longitude":151.20699}]}`)
	}))
	defer srv.Close()

	p := newTestProvider(t, Options{BaseURL: srv.URL, APIKey: testAPIKey})

	_, _, formatted, found, err := p.Lookup(context.Background(), sydney)
	if err != nil || !found {
		t.Fatalf("Lookup: found = %v, err = %v", found, err)
	}
	if formatted != sydney {
		t.Errorf("formatted = %q, want the address as given", formatted)
	}
}

// Nothing to resolve is answered without a round trip: a blank address is the validator's
// business (SHIP-60), and spending a metered lookup on it would be nobody's.
func TestProviderDoesNotCallOutForABlankAddress(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	p := newTestProvider(t, Options{BaseURL: srv.URL, APIKey: testAPIKey})

	_, _, _, found, err := p.Lookup(context.Background(), "   ")
	if found || err != nil {
		t.Errorf("found = %v, err = %v, want false and nil", found, err)
	}
	if called {
		t.Error("the provider was called for a blank address")
	}
}

// The default client wraps httpx.PropagateRequestID, so a lookup is traceable back to the
// job creation that caused it (SHIP-14).
func TestProviderDefaultClientPropagatesTheRequestID(t *testing.T) {
	var gotID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = r.Header.Get(httpx.HeaderRequestID)
		_, _ = io.WriteString(w, `{"results":[]}`)
	}))
	defer srv.Close()

	p, err := NewProvider(Options{BaseURL: srv.URL, APIKey: testAPIKey})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	ctx := httpx.ContextWithRequestID(context.Background(), "req-99")
	if _, _, _, _, err := p.Lookup(ctx, sydney); err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if gotID != "req-99" {
		t.Errorf("outbound %s = %q, want req-99", httpx.HeaderRequestID, gotID)
	}
}

func TestNewProviderRefusesIncompleteOptions(t *testing.T) {
	cases := map[string]Options{
		"no base URL": {APIKey: "k"},
		"no API key":  {BaseURL: "https://maps.example"},
		"not a URL":   {BaseURL: "://", APIKey: "k"},
		"not http(s)": {BaseURL: "ftp://maps.example", APIKey: "k"},
		"no scheme":   {BaseURL: "maps.example", APIKey: "k"},
	}
	for name, opts := range cases {
		if _, err := NewProvider(opts); err == nil {
			t.Errorf("NewProvider(%s) returned no error", name)
		}
	}

	if _, err := NewProvider(Options{BaseURL: "https://maps.example", APIKey: "k"}); err != nil {
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
