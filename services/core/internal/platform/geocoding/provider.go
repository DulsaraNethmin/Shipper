package geocoding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// Options is everything the provider implementation needs to reach its maps vendor.
//
// It is the adapter's own struct rather than a slice of internal/config: a package that
// reads configuration decides for itself where it runs, which makes it untestable without
// the environment. The composition root reads configuration; this takes arguments.
type Options struct {
	// BaseURL is the root of the maps provider's API, without a trailing path.
	BaseURL string

	// APIKey is presented as a bearer credential. It is never logged.
	APIKey string

	// HTTPClient replaces the client this package would otherwise build. Tests point it
	// at an httptest server.
	//
	// The default wraps httpx.PropagateRequestID, so a lookup is traceable back to the
	// request that caused it (SHIP-14).
	HTTPClient *http.Client
}

// defaultTimeout bounds a single lookup.
//
// Geocoding is the one external call on the job-creation path, so a customer is waiting on
// it. Five seconds is long enough for a healthy provider and short enough that a sick one
// degrades into a not-found — which SHIP-60 and SHIP-63 are already required to handle —
// rather than into a request that never answers.
const defaultTimeout = 5 * time.Second

// Provider resolves an address through the maps vendor over HTTP.
//
// # No vendor is named, and that is the decision rather than an omission
//
// Docs/06 §4.1's stated pattern is that the seam exists before the vendor does, and
// Docs/11 §7 records the choice as deferred to SHIP-60 — the address value object, which
// is the first ticket that resolves anything real. Until then this speaks a generic
// contract: a GET under a configured base URL, with a bearer credential, answering a list
// of candidates.
type Provider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewProvider validates opts and returns the provider-backed implementation.
func NewProvider(opts Options) (*Provider, error) {
	if opts.BaseURL == "" {
		return nil, fmt.Errorf("geocoding: provider needs a base URL")
	}
	u, err := url.Parse(opts.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("geocoding: base URL %q is not a URL: %w", opts.BaseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("geocoding: base URL %q must be http or https", opts.BaseURL)
	}
	if opts.APIKey == "" {
		return nil, fmt.Errorf("geocoding: provider needs an API key")
	}

	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout:   defaultTimeout,
			Transport: httpx.PropagateRequestID(nil),
		}
	}

	return &Provider{
		baseURL: strings.TrimRight(opts.BaseURL, "/"),
		apiKey:  opts.APIKey,
		client:  client,
	}, nil
}

// response is the reply shape. Field names are the generic form; the vendor named at
// SHIP-60 confirms or replaces them, and nothing outside this file has to know.
type response struct {
	Results []candidate `json:"results"`
}

type candidate struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Formatted string  `json:"formatted_address"`
}

// authHeader is spelled by RFC 9110, not by us.
const authHeader = "Authorization" // spelling:ok — HTTP header name, RFC 9110

// Lookup resolves address to coordinates and to the provider's normalised form of it.
//
// found is false when the provider answered and did not recognise the address. That is an
// ordinary outcome rather than a failure — see the package documentation — and err is nil
// with it. err is non-nil only when the lookup itself did not complete: an unreachable
// provider, a refused credential, a response that is not the shape agreed above.
//
// A caller therefore distinguishes "we asked and there is no such place" from "we could
// not ask" without inspecting an error string, which is what lets SHIP-60 publish the job
// anyway in the first case and retry in the second.
func (p *Provider) Lookup(ctx context.Context, address string) (lat, lng float64, formatted string, found bool, err error) {
	if strings.TrimSpace(address) == "" {
		// Nothing was asked, so nothing was found. Not an error: an empty address is the
		// validator's business (SHIP-60), not the transport's.
		return 0, 0, "", false, nil
	}

	endpoint := p.baseURL + "/geocode?" + url.Values{"address": {address}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, 0, "", false, fmt.Errorf("geocoding: building the request: %w", err)
	}
	req.Header.Set(authHeader, "Bearer "+p.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		// The error from net/http carries the URL but never the headers, so the
		// credential cannot travel with it.
		return 0, 0, "", false, fmt.Errorf("geocoding: reaching the provider: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	// A 404 here is the endpoint being wrong, not the address being unknown. Reading it
	// as not-found would turn a misconfigured base URL into every address in the country
	// quietly failing to resolve, with nothing in the logs to say so.
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return 0, 0, "", false, fmt.Errorf("geocoding: provider refused the lookup: %s: %s",
			resp.Status, snippet(resp.Body))
	}

	var body response
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBody)).Decode(&body); err != nil {
		return 0, 0, "", false, fmt.Errorf("geocoding: decoding the response: %w", err)
	}
	if len(body.Results) == 0 {
		return 0, 0, "", false, nil
	}

	// The first candidate, and only the first. Ranking alternatives is a product decision
	// about ambiguous addresses that SHIP-60 owns; picking one here would hide it.
	best := body.Results[0]
	if err := validCoordinates(best.Latitude, best.Longitude); err != nil {
		// A candidate outside the possible range is a mangled response — most often two
		// fields in the wrong order. Reporting it as found would put a job somewhere it
		// is not, which is the one outcome this package must never produce.
		return 0, 0, "", false, fmt.Errorf("geocoding: %w", err)
	}

	formatted = best.Formatted
	if strings.TrimSpace(formatted) == "" {
		formatted = address
	}
	return best.Latitude, best.Longitude, formatted, true, nil
}

// validCoordinates rejects a pair that cannot exist.
//
// It checks the whole globe rather than Australia. Whether a resolved address is inside
// the service area is a product rule, and it belongs with the rest of them in the jobs
// domain (SHIP-60) — an adapter enforcing it would put a policy decision in the transport,
// where nobody looking for it would find it.
func validCoordinates(lat, lng float64) error {
	if lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		return fmt.Errorf("provider returned an impossible coordinate (%.6f, %.6f)", lat, lng)
	}
	return nil
}

const (
	// maxResponseBody bounds what is decoded. A provider having a bad day can answer with
	// anything, and this runs on the job-creation path.
	maxResponseBody = 1 << 20 // 1 MiB

	// maxErrorBody bounds how much of a failure response is quoted back into a log record.
	maxErrorBody = 512
)

// snippet reads a bounded, single-line excerpt of a response body for an error message.
func snippet(r io.Reader) string {
	b, err := io.ReadAll(io.LimitReader(r, maxErrorBody))
	if err != nil || len(b) == 0 {
		return "(no body)"
	}
	return strings.Join(strings.Fields(string(b)), " ")
}
