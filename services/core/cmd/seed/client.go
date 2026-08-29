package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// The seed's view of the platform: an ordinary HTTP client holding one credential (SHIP-186).
//
// # Why the seed is a client rather than a second composition root
//
// The alternative was a command that imports the domain services and calls them directly, the way
// cmd/api does. It was rejected, and the reason is worth recording because it is not obvious from
// the outside: `bidding` and `delivery` are wired through eight adapter types that live in
// cmd/api — `negotiatedJobs`, `awardableJobs`, `presentedJobs`, `offerableVehicles`,
// `offerorDirectory`, `jobLifecycle`, `acceptedBids`, `jobCustomers` — each of which translates one
// domain's sentinels into another's vocabulary and each of which carries an argument in its header
// for why it translates the way it does. A second composition root would be a second copy of all
// eight, free to drift from the first with nothing to report that it had.
//
// Writing the rows with SQL instead was rejected harder, and by the schema rather than by taste.
// SHIP-57's trigger refuses a `jobs` row whose status moved without a `job_status_history` row
// written in the same transaction describing exactly that move, so a job cannot be inserted at
// `Delivered` at all — and reproducing the history, the audit entries and the outbox events by hand
// would be a second implementation of the transitions, which is the thing CLAUDE.md's invariant
// exists to prevent.
//
// So the seed drives the public API. Every job it creates was published by a customer, every bid
// was placed by a provider the platform decided was eligible, and every milestone passed the guard.
// What the demonstration holds is therefore data the product made, which is the only kind worth
// showing somebody.

// client is one authenticated caller. A seed run holds several — the customer, each provider, the
// administrator, and one per driver link — because the platform's answer to any request depends on
// who is asking, and that is most of what the demonstration is for.
type client struct {
	base  string
	http  *http.Client
	token string

	// header names the credential's scheme. Marketplace and administrator sessions are both
	// bearer tokens and a driver link is one too, but they are three separate systems: a driver
	// token reaches exactly one job and cannot be exchanged for either of the others.
	header string
}

// newClient builds an unauthenticated caller against base.
func newClient(base string) *client {
	return &client{
		base:   strings.TrimRight(base, "/"),
		http:   &http.Client{Timeout: 30 * time.Second},
		header: "Bearer",
	}
}

// as returns a copy of c holding token. The receiver is left alone so one anonymous client can
// mint as many authenticated ones as the run needs.
func (c *client) as(token string) *client {
	dup := *c
	dup.token = token
	return &dup
}

// apiError is a refusal from the platform, carrying the code clients are meant to branch on.
//
// The message is kept for the operator reading the seed's output and the code is kept for the seed
// itself: [errorCode] is how idempotency distinguishes "this already exists" from "this failed".
type apiError struct {
	Status    int
	Code      string
	Message   string
	RequestID string
	Method    string
	Path      string

	// Details is the per-field list a validation_failed carries.
	//
	// **Reported rather than dropped, and that is not a nicety.** The contract's top-level
	// message for a field error is deliberately generic — "Some of the details you entered need
	// attention" — because it is written for a form that puts each message beside its input. A
	// seed has no form, so without this an operator debugging a rejected job is told only that
	// something about it was wrong.
	Details []struct {
		Field   string `json:"field"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
}

func (e *apiError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("%s %s: %d", e.Method, e.Path, e.Status)
	}

	described := fmt.Sprintf("%s %s: %d %s: %s (request %s)",
		e.Method, e.Path, e.Status, e.Code, e.Message, e.RequestID)
	for _, detail := range e.Details {
		described += fmt.Sprintf("\n    %s: %s", detail.Field, detail.Message)
	}
	return described
}

// errorCode returns the platform's machine-readable code for err, or "" if err is not a refusal.
//
// Branching on the code rather than on the message or the status is the API's own rule
// (`services/core/README.md`), and the seed keeps it: "a bid already exists on this job" and "the
// job is not open" are both 409s and mean opposite things for a re-run.
func errorCode(err error) string {
	var refusal *apiError
	if !errors.As(err, &refusal) {
		return ""
	}
	return refusal.Code
}

// errorResponse is the standard error contract (SHIP-12).
type errorResponse struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
		Details   []struct {
			Field   string `json:"field"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"details"`
	} `json:"error"`
}

// get issues a read.
func (c *client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

// post issues a write, minting a fresh idempotency key for it.
//
// # The key is random rather than derived, and that is deliberate
//
// Every state-changing endpoint refuses a request without an `Idempotency-Key` (SHIP-15), and the
// obvious move is to derive one from the row being written so that a re-run replays rather than
// repeats. It would not work, and it would hide its not working: the middleware's keys live in
// Redis under a TTL, so a key derived today is a *fresh* key next week and the second run would
// write a second row while appearing to be protected against exactly that.
//
// The seed's idempotency is therefore in the data rather than in the key — every creation is
// preceded by a read that asks whether the thing already exists, on a natural key the product
// itself carries. See marketplace.go. The middleware still absorbs the retry it is for: a phone,
// or a seed, that lost its connection and sent the same request twice.
func (c *client) post(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPost, path, body, out)
}

// patch issues a partial update.
func (c *client) patch(ctx context.Context, path string, body, out any) error {
	return c.do(ctx, http.MethodPatch, path, body, out)
}

// do performs one request and decodes its answer.
func (c *client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("%s %s: encoding the request: %w", method, path, err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", c.header+" "+c.token)
	}
	if method != http.MethodGet {
		req.Header.Set("Idempotency-Key", uuid.NewString())
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("%s %s: reading the response: %w", method, path, err)
	}

	if resp.StatusCode >= 400 {
		failure := &apiError{Status: resp.StatusCode, Method: method, Path: path}
		var contract errorResponse
		if json.Unmarshal(payload, &contract) == nil {
			failure.Code = contract.Error.Code
			failure.Message = contract.Error.Message
			failure.RequestID = contract.Error.RequestID
			failure.Details = contract.Error.Details
		}
		if failure.Message == "" {
			failure.Message = strings.TrimSpace(string(payload))
		}
		return failure
	}

	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("%s %s: decoding the response: %w", method, path, err)
	}
	return nil
}

// putObject uploads bytes to a pre-signed URL.
//
// **This is the one request in the seed that does not go to the platform.** The platform mints
// permission to write one object, for one content type, at one length, for a few minutes; the bytes
// then travel straight to the object store, which is the whole point of a pre-signed URL — a
// photograph never passes through the API. The signature covers the host, the method and the
// content type, so all three have to be exactly what was asked for.
func (c *client) putObject(ctx context.Context, url, contentType string, payload []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("uploading the object: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.ContentLength = int64(len(payload))

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("uploading the object: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("uploading the object: %d: %s",
			resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	return nil
}

// signIn exchanges a password for a marketplace session.
//
// The device label is required by the endpoint rather than optional, because a device list of rows
// all reading "Unknown device" cannot be acted on. The seed names itself, which is also what an
// operator wants to see in the demonstration account's device list.
func (c *client) signIn(ctx context.Context, email, password string) (*client, error) {
	var answer struct {
		AccessToken string `json:"access_token"`
	}
	body := map[string]string{
		"email":        email,
		"password":     password,
		"device_label": "Demonstration seed",
	}
	if err := c.post(ctx, "/v1/auth/login", body, &answer); err != nil {
		return nil, fmt.Errorf("signing in as %s: %w", email, err)
	}
	if answer.AccessToken == "" {
		return nil, fmt.Errorf("signing in as %s: the platform returned no access token", email)
	}
	return c.as(answer.AccessToken), nil
}

// signInAdministrator opens an administrator session.
//
// A separate endpoint and a separate credential system from [client.signIn], and neither token is
// accepted where the other belongs — which is what SHIP-147 exists to establish and what this call
// depends on rather than merely respects.
func (c *client) signInAdministrator(ctx context.Context, email, password string) (*client, error) {
	var answer struct {
		Token string `json:"token"`
	}
	body := map[string]string{"email": email, "password": password}
	if err := c.post(ctx, "/v1/admin/sessions", body, &answer); err != nil {
		return nil, fmt.Errorf("signing in as administrator %s: %w", email, err)
	}
	if answer.Token == "" {
		return nil, fmt.Errorf("signing in as administrator %s: the platform returned no token", email)
	}
	return c.as(answer.Token), nil
}
