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
	baseURL string
	apiKey  string
	sender  string
	client  *http.Client
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

	return &Provider{
		baseURL: strings.TrimRight(opts.BaseURL, "/"),
		apiKey:  opts.APIKey,
		sender:  opts.Sender,
		client:  client,
	}, nil
}

// message is the request body. Field names are the generic form; the vendor named at
// SHIP-33 confirms or replaces them, and nothing outside this file has to know.
type message struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
}

// authHeader is spelled by RFC 9110, not by us.
const authHeader = "Authorization" // spelling:ok — HTTP header name, RFC 9110

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

	payload, err := json.Marshal(message{From: p.sender, To: to, Subject: subject, Text: body})
	if err != nil {
		return fmt.Errorf("email: encoding the message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/messages", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("email: building the request: %w", err)
	}
	req.Header.Set(authHeader, "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")
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
