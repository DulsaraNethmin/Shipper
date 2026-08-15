package push

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

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// Options is everything the Firebase implementation needs to reach a project.
//
// The adapter's own struct rather than a slice of internal/config, for the reason
// email.Options gives: a package that reads configuration decides for itself where it runs,
// which makes it untestable without the environment. The composition root reads
// configuration; this takes arguments.
type Options struct {
	// ProjectID is the Firebase project. It is part of the URL rather than a header —
	// FCM's HTTP v1 API addresses a project in its path — so an empty one is refused at
	// construction rather than producing a 404 per message.
	ProjectID string

	// BaseURL is the root of the FCM API, without a trailing path. Empty means
	// [DefaultBaseURL]; a test points it at an httptest server.
	BaseURL string

	// Credential answers the bearer token presented with each send.
	//
	// A function rather than a string because Google's is short-lived: a service account
	// exchanges its JSON key for an access token that expires in an hour, so a value read
	// once at startup stops working during the first afternoon. Taking a function means
	// this file never sees the key material and never has to know how the exchange is done
	// — the composition root supplies a closure, and a test supplies one that returns a
	// constant.
	//
	// **The exchange itself is not implemented anywhere in this repository yet**, and the
	// reason is recorded rather than hidden: it needs golang.org/x/oauth2/google, and
	// adding a module is a go.mod change. See doc.go.
	Credential func(ctx context.Context) (string, error)

	// HTTPClient replaces the client this package would otherwise build. Tests point it at
	// an httptest server; a deployment might supply one with its own transport.
	HTTPClient *http.Client
}

// DefaultBaseURL is Google's, and is what a deployment that sets nothing uses.
const DefaultBaseURL = "https://fcm.googleapis.com"

// defaultTimeout bounds a single send.
//
// A push is dispatched from cmd/notifier inside a transaction holding a batch of rows
// (notifications.DispatchBatch), so an unbounded call holds a database transaction as well as
// a goroutine. Ten seconds matches the email adapter, for the same reason: far longer than a
// healthy provider takes, far shorter than anything upstream is waiting on.
const defaultTimeout = 10 * time.Second

// FCM dispatches through Firebase Cloud Messaging, which fronts APNs for iOS as well as
// delivering to Android (Docs/06 §2).
//
// # No SDK
//
// firebase.google.com/go is not imported, and that is the same decision email.Provider made
// about its vendor: the wire contract is a JSON POST with a bearer credential, the seam is
// where Docs/06 §4.1 wants it, and a module dependency for one endpoint buys nothing this
// file does not already have. It also keeps SHIP-139 inside a branch that may not edit go.mod.
type FCM struct {
	sendURL    string
	credential func(ctx context.Context) (string, error)
	client     *http.Client
}

// NewFCM validates opts and returns the dispatching implementation.
//
// It fails at construction rather than at first send, exactly as email.NewProvider does: a
// missing project discovered when the first notification is dispatched is a channel that has
// been silently dead since the deploy, and every row it should have sent is marked `failed`
// with a message nobody reads until somebody complains.
func NewFCM(opts Options) (*FCM, error) {
	if opts.ProjectID == "" {
		return nil, fmt.Errorf("push: fcm needs a project id")
	}
	if strings.ContainsAny(opts.ProjectID, "/?#") {
		return nil, fmt.Errorf("push: project id %q is not a single path segment", opts.ProjectID)
	}
	if opts.Credential == nil {
		return nil, fmt.Errorf("push: fcm needs a credential source")
	}

	base := opts.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("push: base URL %q is not a URL: %w", base, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("push: base URL %q must be http or https", base)
	}

	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout:   defaultTimeout,
			Transport: httpx.PropagateRequestID(nil),
		}
	}

	return &FCM{
		sendURL: fmt.Sprintf("%s/v1/projects/%s/messages:send",
			strings.TrimRight(base, "/"), url.PathEscape(opts.ProjectID)),
		credential: opts.Credential,
		client:     client,
	}, nil
}

// message is FCM's HTTP v1 request body, narrowed to what this platform sends.
//
// The shape is closed on purpose. Everything variable in a Shipper push is the title, the body
// and one job identifier — there is no field here an address, a goods description or a customer
// name could travel in (SHIP-141), because the caller has nothing else to give it.
type message struct {
	Message struct {
		Token        string            `json:"token"`
		Notification notificationBody  `json:"notification"`
		Data         map[string]string `json:"data,omitempty"`
		Android      *androidConfig    `json:"android,omitempty"`
	} `json:"message"`
}

type notificationBody struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// androidConfig raises the priority so a delivery notification is not held until the handset
// next wakes. iOS takes its equivalent from the presence of a `notification` block, so there is
// no apns section to match this one.
type androidConfig struct {
	Priority string `json:"priority"`
}

// errorEnvelope is the shape FCM answers a failure with.
//
// Only the fields this adapter decides on. `status` is the canonical Google API code
// (`NOT_FOUND`, `INVALID_ARGUMENT`); `details[].errorCode` is FCM's own, and is the one that
// distinguishes a dead token from a malformed request.
type errorEnvelope struct {
	Error struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Details []struct {
			Type      string `json:"@type"`
			ErrorCode string `json:"errorCode"`
		} `json:"details"`
	} `json:"error"`
}

// rejectionCodes are the FCM error codes that mean "this token will never deliver again".
//
// Three, and each is a different way for the same thing to be true:
//
//	UNREGISTERED         the app was uninstalled, or its data cleared
//	INVALID_ARGUMENT     the token is not a token this project can address
//	SENDER_ID_MISMATCH   the token belongs to a different Firebase project
//
// All three are permanent for this token and none of them is a reason to retry, which is what
// makes them a deregistration rather than a failure — see the argument in doc.go.
var rejectionCodes = map[string]bool{
	"UNREGISTERED":       true,
	"INVALID_ARGUMENT":   true,
	"SENDER_ID_MISMATCH": true,
}

// Push delivers one notification to one device.
//
// # rejected is not an error, and the signature is where that is enforced
//
// A caller cannot ignore the first return value the way it can ignore a sentinel it forgot to
// check with errors.Is. That is deliberate and it is the whole shape of this adapter:
// doc.go's central claim is that a rejected token is normal traffic, and a claim carried by a
// sentinel error is one every caller is free to collapse back into "it failed". A boolean the
// compiler makes you name is not.
//
// rejected true, err nil means the far end says this token is dead: the app was uninstalled,
// the token belongs to another project, or it was never a token at all. The notification was
// not delivered and never will be to this device, and the right response is to deregister it
// (SHIP-140) rather than to retry, alert, or count it as a dispatch failure.
//
// err non-nil means the send did not complete: the network, the credential, FCM being down, a
// quota. Those are worth retrying and worth alerting on (SHIP-176).
func (f *FCM) Push(
	ctx context.Context, deviceToken, title, body string, jobID uuid.UUID,
) (rejected bool, err error) {
	if deviceToken == "" {
		// Not a rejection: there is no token for the far end to have rejected. A row with
		// an empty address should have been impossible (ck_notifications_address), so this
		// is a defect worth surfacing rather than a device worth deregistering.
		return false, ErrNoDeviceToken
	}

	credential, err := f.credential(ctx)
	if err != nil {
		return false, fmt.Errorf("push: obtaining a credential for fcm: %w", err)
	}
	if credential == "" {
		return false, fmt.Errorf("push: the credential source returned nothing")
	}

	var m message
	m.Message.Token = deviceToken
	m.Message.Notification = notificationBody{Title: title, Body: body}
	m.Message.Android = &androidConfig{Priority: "high"}
	if jobID != uuid.Nil {
		// Docs/01 §4.5 requires a push to open the job it concerns. The identifier travels
		// in `data` rather than in the visible body, so the client can route on it
		// (SHIP-145) and nothing renders on a locked screen that was not written as copy.
		m.Message.Data = map[string]string{"job_id": jobID.String()}
	}

	payload, err := json.Marshal(m)
	if err != nil {
		return false, fmt.Errorf("push: encoding the message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.sendURL, bytes.NewReader(payload))
	if err != nil {
		return false, fmt.Errorf("push: building the request: %w", err)
	}
	req.Header.Set(authHeader, "Bearer "+credential)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		// net/http's error carries the URL and never the headers, so the credential cannot
		// travel with it.
		return false, fmt.Errorf("push: dispatching to fcm: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		return false, nil
	}

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if code, ok := rejectionIn(resp.StatusCode, raw); ok {
		// Normal traffic. Reported, not raised — the notification is complete as far as
		// this channel is concerned, and the device is one the platform should stop
		// addressing.
		_ = code
		return true, nil
	}

	return false, fmt.Errorf("push: fcm rejected the message: %s: %s",
		resp.Status, snippet(raw))
}

// rejectionIn reports whether a failure response means the token is dead, and which code said so.
//
// Two signals, and both are needed. A 404 from this endpoint can mean nothing else — the only
// resource being addressed is the token — so it is conclusive on its own even when the body is
// unparseable, which is the case a proxy in front of FCM produces. Otherwise the answer is in
// FCM's own `errorCode`, which is the field that distinguishes "this token is not addressable"
// from "the request was malformed"; both arrive as HTTP 400.
//
// Anything else is a dispatch failure. In particular a 401 or 403 with no FCM error code is the
// credential being wrong, which is a deployment fault and must not quietly deregister the whole
// install base one device at a time.
func rejectionIn(status int, body []byte) (string, bool) {
	var env errorEnvelope
	if err := json.Unmarshal(body, &env); err == nil {
		for _, detail := range env.Error.Details {
			if rejectionCodes[detail.ErrorCode] {
				return detail.ErrorCode, true
			}
		}
		if env.Error.Status == "NOT_FOUND" {
			return "NOT_FOUND", true
		}
	}
	if status == http.StatusNotFound {
		return "NOT_FOUND", true
	}
	return "", false
}

// authHeader is spelled by RFC 9110, not by us.
const authHeader = "Authorization" // spelling:ok — HTTP header name, RFC 9110

// maxErrorBody bounds how much of a failure response is read and quoted back.
const maxErrorBody = 1024

// snippet reads a bounded, single-line excerpt of a response body for an error message.
func snippet(b []byte) string {
	if len(b) == 0 {
		return "(no body)"
	}
	return strings.Join(strings.Fields(string(b)), " ")
}

// UseNoop reports whether env logs its push notifications rather than dispatching them.
//
// The rule is stated once here rather than in each composition root, and it leans towards the
// no-op for anything it does not recognise — the same direction email.UseConsole leans, and for
// a sharper version of the same reason. A development machine that dispatched for real would
// wake a handset belonging to whoever last used that token, and unlike an email there is no
// address to inspect afterwards to work out who.
func UseNoop(env config.Environment) bool {
	switch env {
	case config.Staging, config.Production:
		return false
	default:
		return true
	}
}
