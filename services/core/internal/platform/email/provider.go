package email

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

// Options is everything the provider implementation needs to reach its vendor.
//
// It is the adapter's own struct rather than a slice of internal/config on purpose. A
// package that reads configuration decides for itself where it runs, which makes it
// untestable without the environment and couples every adapter to the shape of one
// settings file. The composition root reads configuration; this takes arguments.
type Options struct {
	// BaseURL is the root of the provider's API, without a trailing path.
	BaseURL string

	// APIKey is presented as a bearer credential. It is never logged.
	APIKey string

	// Sender is the From address every message is dispatched with. One address for the
	// whole service in the MVP; per-domain senders are a deliverability decision nobody
	// has needed to make yet.
	Sender string

	// Path, AuthHeader, AuthScheme, ContentType and BodyTemplate are what make one vendor
	// reachable rather than another, and every one of them is optional — left empty they
	// reproduce byte for byte what this adapter sent before they existed.
	//
	// # Why these are configuration and not five more Go files
	//
	// The vendor is not this project's to choose. A deployment is sold to a buyer who
	// already has a mail provider, a contract with it and an address their customers
	// recognise, and the adapter that cannot follow them there is one they have to pay
	// somebody to rewrite. So the three things vendors actually differ on — where the
	// request goes, how the credential is presented, and what the body looks like — are
	// read from the environment, and switching from one JSON email API to another is an
	// edit to a .env file.
	//
	// This is the same argument CLAUDE.md makes for category lists and validation limits:
	// anything expected to change under operational pressure lives outside the binary.

	// Path is appended to BaseURL. Defaults to "/messages". Leading slash optional.
	Path string

	// AuthHeader is the header the credential is presented in. Defaults to
	// "Authorization" (spelling:ok — HTTP header name, RFC 9110). Vendors that use their
	// own — Postmark's X-Postmark-Server-Token,
	// for one — set it here.
	AuthHeader string

	// AuthScheme prefixes the credential, separated by a space. Defaults to "Bearer".
	//
	// Set it to AuthSchemeNone for a vendor that wants the bare token with no scheme —
	// Postmark is the common one. The sentinel exists because an optional string field
	// cannot otherwise express "deliberately empty": empty means "unset, use the default"
	// for every other field here, and one field quietly meaning the opposite is the kind
	// of inconsistency that is discovered by a credential arriving in the wrong shape.
	AuthScheme string

	// ContentType is the request's Content-Type. Defaults to "application/json".
	ContentType string

	// BodyTemplate renders the request body. It is a text/template over four fields —
	// .From, .To, .Subject and .Text — with one function, `json`, which marshals a value
	// **including its surrounding quotes**.
	//
	// Use it for every interpolated value. `{"subject":"{{.Subject}}"}` produces invalid
	// JSON the first time somebody is sent a subject containing a quotation mark, and a
	// body assembled by string concatenation is an injection into the vendor's API;
	// `{"subject":{{json .Subject}}}` is correct for every input. The default is written
	// that way and is worth copying.
	//
	// Resend, for example, differs from the default only in taking an array:
	//
	//	{"from":{{json .From}},"to":[{{json .To}}],"subject":{{json .Subject}},"text":{{json .Text}}}
	BodyTemplate string

	// HTTPClient replaces the client this package would otherwise build. Tests point it
	// at an httptest server; a deployment might supply one with its own transport.
	//
	// The default wraps httpx.PropagateRequestID, so a provider call is traceable back to
	// the request that caused it (SHIP-14).
	HTTPClient *http.Client
}

// defaultTimeout bounds a single dispatch.
//
// Sending email is not on a user-facing critical path — a verification message is queued
// behind a response that has already been written — but an unbounded call still holds a
// goroutine and, at SHIP-138, a worker slot. Ten seconds is far longer than a healthy
// provider takes and far shorter than a request timeout.
const defaultTimeout = 10 * time.Second

// Provider hands a message to the email vendor over HTTP.
//
// # No vendor is named, and that is the decision rather than an omission
//
// Docs/06 §4.1's stated pattern is that the seam exists before the vendor does, and
// Docs/11 §7 records the choice as deferred to SHIP-33 — the verification-email endpoint,
// which is the first ticket that sends anything real. Until then this speaks a generic
// contract: a JSON POST to a path under a configured base URL, with a bearer credential.
//
// Substituting a real vendor is expected to change this file and nothing else. If it ever
// needs to change a caller, the seam was drawn in the wrong place.
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
// It fails at construction rather than at first send. A missing credential discovered when
// the first customer registers is an outage; discovered at startup it is a failed deploy,
// which is the cheaper of the two by a wide margin.
func NewProvider(opts Options) (*Provider, error) {
	if opts.BaseURL == "" {
		return nil, fmt.Errorf("email: provider needs a base URL")
	}
	u, err := url.Parse(opts.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("email: base URL %q is not a URL: %w", opts.BaseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("email: base URL %q must be http or https", opts.BaseURL)
	}
	if opts.APIKey == "" {
		return nil, fmt.Errorf("email: provider needs an API key")
	}
	if opts.Sender == "" {
		return nil, fmt.Errorf("email: provider needs a sender address")
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

	// Parsed once here rather than on every send, and the error is returned rather than
	// logged: a template that does not compile is a deployment that cannot send email, and
	// the cheapest moment to find that out is the one before any customer registers.
	body, err := template.New("body").Funcs(templateFuncs).
		Parse(firstNonEmpty(opts.BodyTemplate, defaultBodyTemplate))
	if err != nil {
		return nil, fmt.Errorf("email: body template: %w", err)
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

	// A template that parses can still render nonsense, and the two failures look nothing
	// alike from a log: a parse error names a line, while a template that quietly emits
	// `{"subject":"he said "hi""` is reported by the vendor as a 400 with a body nobody
	// reads. So it is rendered once against a value chosen to break naive quoting, and the
	// result is required to be valid JSON whenever the content type claims JSON.
	//
	// Only then — a deployment posting form-encoded or XML to some other vendor is not
	// wrong, it is simply not checkable this way, and refusing it would make the
	// configurability this file exists for conditional on one content type.
	if strings.Contains(contentType, "json") {
		probe, err := p.render(message{
			From:    opts.Sender,
			To:      `probe@example.com`,
			Subject: `a "quoted" subject`,
			Text:    "line\nbreak",
		})
		if err != nil {
			return nil, fmt.Errorf("email: body template: %w", err)
		}
		if !json.Valid(probe) {
			return nil, fmt.Errorf("email: body template does not render valid JSON — "+
				"interpolate every value with {{json .Field}}, which supplies its own "+
				"quotes, rather than writing \"{{.Field}}\": rendered %s", snippet(bytes.NewReader(probe)))
		}
	}

	return p, nil
}

// firstNonEmpty returns the first argument that is not the empty string.
//
// Every configurable field on Options is optional and falls back to the value this adapter
// used before it was configurable, so this shape appears once per field.
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

// message is what a body template is rendered against, and the four fields are the whole
// vocabulary a template has.
//
// The json tags are not used by the default template, which interpolates each field
// individually. They are kept because they make `{{json .}}` render the entire object in
// the shape below, which is a convenience for a vendor that happens to accept it.
type message struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
}

// authHeader is spelled by RFC 9110, not by us. It is the default; Options.AuthHeader
// replaces it for a vendor that carries its credential somewhere else.
const authHeader = "Authorization" // spelling:ok — HTTP header name, RFC 9110

// The defaults every configurable field falls back to.
//
// Together they reproduce exactly what this adapter sent before any of it was
// configurable, which is the property that let the change land without rewriting a test:
// a POST of {"from":…,"to":…,"subject":…,"text":…} to {base}/messages, bearing the
// credential as `Authorization: Bearer` (spelling:ok — HTTP header name, RFC 9110).
const (
	defaultPath        = "/messages"
	defaultAuthScheme  = "Bearer"
	defaultContentType = "application/json"

	// AuthSchemeNone, given as Options.AuthScheme, presents the credential on its own with
	// no scheme and no separating space.
	AuthSchemeNone = "none"

	// Every value is interpolated with `json`, which supplies its own quotes. Written
	// with bare "{{.Subject}}" instead, the first subject containing a quotation mark
	// would produce a malformed body — and a body built by concatenating unescaped
	// strings into JSON is an injection into the vendor's API, not merely a bug.
	defaultBodyTemplate = `{"from":{{json .From}},"to":{{json .To}},` +
		`"subject":{{json .Subject}},"text":{{json .Text}}}`
)

// templateFuncs is the one function a body template gets.
//
// `json` marshals a value to its JSON literal, quotes included, so a template writes
// {{json .Subject}} rather than "{{.Subject}}". That is the difference between a body that
// survives an apostrophe in somebody's name and one that does not.
var templateFuncs = template.FuncMap{
	"json": func(v any) (string, error) {
		b, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(b), nil
	},
}

// Send dispatches one message and reports whether the provider accepted it.
//
// Accepted means accepted for delivery, which is the only thing any provider promises
// synchronously. Whether it reached the inbox is a bounce webhook's answer and is out of
// scope for the MVP (Docs/01 §8).
//
// There is no retry here. A retry policy needs to know which failures are worth repeating,
// which differs by vendor, and a blind loop against a provider that is rejecting the
// credential is a way to get rate-limited. Dispatch reliability is SHIP-138, and it owns
// that decision once the vendor is known.
func (p *Provider) Send(ctx context.Context, to, subject, body string) error {
	if to == "" {
		return ErrNoRecipient
	}

	payload, err := p.render(message{From: p.sender, To: to, Subject: subject, Text: body})
	if err != nil {
		return fmt.Errorf("email: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+p.path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("email: building the request: %w", err)
	}
	req.Header.Set(p.authHeader, strings.TrimSpace(p.authScheme+" "+p.apiKey))
	req.Header.Set("Content-Type", p.contentType)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		// The error from net/http carries the URL but never the headers, so the
		// credential cannot travel with it.
		return fmt.Errorf("email: dispatching to the provider: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("email: provider rejected the message: %s: %s",
			resp.Status, snippet(resp.Body))
	}
	return nil
}

// maxErrorBody bounds how much of a failure response is quoted back.
//
// A provider having a bad day can answer an HTML error page of any size, and this string
// ends up in a log record. Enough to identify the failure, not enough to be a problem.
const maxErrorBody = 512

// snippet reads a bounded, single-line excerpt of a response body for an error message.
func snippet(r io.Reader) string {
	b, err := io.ReadAll(io.LimitReader(r, maxErrorBody))
	if err != nil || len(b) == 0 {
		return "(no body)"
	}
	return strings.Join(strings.Fields(string(b)), " ")
}
