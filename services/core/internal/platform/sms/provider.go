package sms

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"text/template"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// Options is everything the provider implementation needs to reach its gateway.
//
// It is the adapter's own struct rather than a slice of internal/config: a package that
// reads configuration decides for itself where it runs, which makes it untestable without
// the environment. The composition root reads configuration; this takes arguments.
type Options struct {
	// BaseURL is the root of the gateway's API, without a trailing path.
	BaseURL string

	// APIKey is presented as a bearer credential. It is never logged.
	APIKey string

	// Sender is what the message appears to come from — an alphanumeric sender ID or an
	// originating number, depending on what the gateway and the destination country
	// permit. Australia allows both; the choice is made with the vendor at SHIP-36.
	Sender string

	// HTTPClient replaces the client this package would otherwise build. Tests point it
	// at an httptest server.
	//
	// The default wraps httpx.PropagateRequestID, so a gateway call is traceable back to
	// the request that caused it (SHIP-14).
	HTTPClient *http.Client

	// Path, AuthHeader, AuthScheme, ContentType and BodyTemplate make one gateway
	// reachable rather than another, and every one is optional — left empty they reproduce
	// what this adapter sent before they existed. internal/platform/email/provider.go
	// carries the full argument for why the vendor is configuration; it applies here
	// unchanged, and more sharply, because SMS gateways vary by country as well as by
	// vendor and a buyer in one is not served by the gateway that suits another.

	// Path is appended to BaseURL. Defaults to "/messages". Leading slash optional.
	Path string

	// AuthHeader is the header the credential is presented in. Defaults to
	// "Authorization".
	AuthHeader string

	// AuthScheme prefixes the credential, separated by a space. Defaults to "Bearer"; set
	// it to AuthSchemeNone for a gateway wanting the bare token. Empty means "unset, use
	// the default", which is why the sentinel exists.
	AuthScheme string

	// ContentType is the request's Content-Type. Defaults to "application/json".
	ContentType string

	// BodyTemplate renders the request body: a text/template over .From, .To and .Text,
	// with one function, `json`, which marshals a value including its surrounding quotes.
	//
	// Interpolate every value with it. `{"text":"{{.Text}}"}` produces a malformed body
	// the first time a message contains a quotation mark, and a body concatenated from
	// unescaped strings is an injection into the gateway's API.
	BodyTemplate string
}

// defaultTimeout bounds a single dispatch.
//
// Shorter than the email adapter's, because an OTP is on a user-facing critical path: a
// person is holding a phone waiting for it. A gateway that has not answered in five seconds
// has not sent it in time to be useful, and failing lets SHIP-36 tell them so.
const defaultTimeout = 5 * time.Second

// Provider hands a message to the SMS gateway over HTTP.
//
// # No vendor is named, and that is the decision rather than an omission
//
// Docs/06 §4.1's stated pattern is that the seam exists before the vendor does, and
// Docs/11 §7 records the choice as deferred to SHIP-36 — the phone-verification endpoint,
// which is the first ticket that sends anything real. Until then this speaks a generic
// contract: a JSON POST to a path under a configured base URL, with a bearer credential.
type Provider struct {
	baseURL     string
	apiKey      string
	sender      string
	path        string
	authHeader  string
	authScheme  string
	contentType string
	body        *template.Template
	client      *http.Client
}

// NewProvider validates opts and returns the dispatching implementation.
//
// It fails at construction rather than at first send: a missing credential discovered when
// the first customer verifies a phone number is an outage, and discovered at startup it is
// a failed deploy.
func NewProvider(opts Options) (*Provider, error) {
	if opts.BaseURL == "" {
		return nil, fmt.Errorf("sms: provider needs a base URL")
	}
	u, err := url.Parse(opts.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("sms: base URL %q is not a URL: %w", opts.BaseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("sms: base URL %q must be http or https", opts.BaseURL)
	}
	if opts.APIKey == "" {
		return nil, fmt.Errorf("sms: provider needs an API key")
	}
	if opts.Sender == "" {
		return nil, fmt.Errorf("sms: provider needs a sender")
	}

	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout:   defaultTimeout,
			Transport: httpx.PropagateRequestID(nil),
		}
	}

	path := firstNonEmpty(opts.Path, defaultPath)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	contentType := firstNonEmpty(opts.ContentType, defaultContentType)

	body, err := template.New("body").Funcs(templateFuncs).
		Parse(firstNonEmpty(opts.BodyTemplate, defaultBodyTemplate))
	if err != nil {
		return nil, fmt.Errorf("sms: body template: %w", err)
	}

	p := &Provider{
		baseURL:     strings.TrimRight(opts.BaseURL, "/"),
		apiKey:      opts.APIKey,
		sender:      opts.Sender,
		path:        path,
		authHeader:  firstNonEmpty(opts.AuthHeader, authHeader),
		authScheme:  resolveAuthScheme(opts.AuthScheme),
		contentType: contentType,
		body:        body,
		client:      client,
	}

	// Rendered once against a value chosen to break naive quoting, and required to be
	// valid JSON whenever the content type claims JSON — a template that parses can still
	// emit nonsense, and that failure arrives as a 400 from the gateway rather than as
	// anything naming the template.
	if strings.Contains(contentType, "json") {
		probe, err := p.render(message{From: opts.Sender, To: "+61400000000", Text: `a "quoted" message`})
		if err != nil {
			return nil, fmt.Errorf("sms: body template: %w", err)
		}
		if !json.Valid(probe) {
			return nil, fmt.Errorf("sms: body template does not render valid JSON — "+
				"interpolate every value with {{json .Field}}, which supplies its own "+
				"quotes, rather than writing \"{{.Field}}\": rendered %s", snippet(bytes.NewReader(probe)))
		}
	}

	return p, nil
}

// firstNonEmpty returns the first argument that is not the empty string.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// resolveAuthScheme maps the configured scheme to the prefix actually sent.
func resolveAuthScheme(configured string) string {
	if strings.EqualFold(strings.TrimSpace(configured), AuthSchemeNone) {
		return ""
	}
	return strings.TrimSpace(firstNonEmpty(configured, defaultAuthScheme))
}

// render executes the body template for one message.
func (p *Provider) render(m message) ([]byte, error) {
	var buf bytes.Buffer
	if err := p.body.Execute(&buf, m); err != nil {
		return nil, fmt.Errorf("rendering the body: %w", err)
	}
	return buf.Bytes(), nil
}

// message is the request body. Field names are the generic form; the vendor named at
// SHIP-36 confirms or replaces them, and nothing outside this file has to know.
type message struct {
	From string `json:"from"`
	To   string `json:"to"`
	Text string `json:"text"`
}

// authHeader is spelled by RFC 9110, not by us. It is the default; Options.AuthHeader
// replaces it for a gateway carrying its credential somewhere else.
const authHeader = "Authorization" // spelling:ok — HTTP header name, RFC 9110

// The defaults every configurable field falls back to. Together they reproduce exactly
// what this adapter sent before any of it was configurable.
const (
	defaultPath        = "/messages"
	defaultAuthScheme  = "Bearer"
	defaultContentType = "application/json"

	// AuthSchemeNone, given as Options.AuthScheme, presents the credential on its own with
	// no scheme and no separating space.
	AuthSchemeNone = "none"

	defaultBodyTemplate = `{"from":{{json .From}},"to":{{json .To}},"text":{{json .Text}}}`
)

// templateFuncs is the one function a body template gets. `json` marshals a value to its
// JSON literal, quotes included, so a template writes {{json .Text}} rather than
// "{{.Text}}".
var templateFuncs = template.FuncMap{
	"json": func(v any) (string, error) {
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(b), nil
	},
}

// Send dispatches one message and reports whether the gateway accepted it.
//
// to is expected in E.164 form. Normalising what a customer typed into that is the
// identity domain's job at SHIP-34, not this package's — an adapter that quietly rewrites
// its input hides a validation gap rather than closing it.
//
// There is no retry here. Retrying an OTP send is not obviously right even when the failure
// looks transient: a second message arriving late, with a different code, is worse for the
// person holding the phone than one that never arrives. SHIP-36 owns that decision.
func (p *Provider) Send(ctx context.Context, to, body string) error {
	if to == "" {
		return ErrNoRecipient
	}

	payload, err := p.render(message{From: p.sender, To: to, Text: body})
	if err != nil {
		return fmt.Errorf("sms: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+p.path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("sms: building the request: %w", err)
	}
	req.Header.Set(p.authHeader, strings.TrimSpace(p.authScheme+" "+p.apiKey))
	req.Header.Set("Content-Type", p.contentType)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		// The error from net/http carries the URL but never the headers, so the
		// credential cannot travel with it.
		return fmt.Errorf("sms: dispatching to the gateway: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// The gateway's reason is quoted, the message body is not: an OTP must not reach
		// a log record by way of an error string.
		return fmt.Errorf("sms: gateway rejected the message: %s: %s",
			resp.Status, snippet(resp.Body))
	}
	return nil
}

// maxErrorBody bounds how much of a failure response is quoted back, because this string
// ends up in a log record and a gateway having a bad day can answer with anything.
const maxErrorBody = 512

// snippet reads a bounded, single-line excerpt of a response body for an error message.
func snippet(r io.Reader) string {
	b, err := io.ReadAll(io.LimitReader(r, maxErrorBody))
	if err != nil || len(b) == 0 {
		return "(no body)"
	}
	return strings.Join(strings.Fields(string(b)), " ")
}
