package email

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// SHIP-32's acceptance criterion, first half: emails log to console in dev.
func TestConsoleRendersTheMessage(t *testing.T) {
	buf := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := httpx.ContextWithLogger(context.Background(), log)

	if err := NewConsole().Send(ctx, "driver@example.com", "Verify your email",
		"Open https://shipper.example/verify?token=abc123 to confirm."); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("console wrote no readable record: %v (%q)", err, buf.String())
	}

	for field, want := range map[string]string{
		"to":      "driver@example.com",
		"subject": "Verify your email",
	} {
		if got, _ := rec[field].(string); got != want {
			t.Errorf("record %s = %q, want %q", field, got, want)
		}
	}

	// The body is logged whole and unredacted on purpose: reading the verification link
	// out of the log is how a developer completes a signup without a mail server.
	if body, _ := rec["body"].(string); !strings.Contains(body, "token=abc123") {
		t.Errorf("record body = %q, want the verification link intact", body)
	}
}

// The console implementation is bound to the request that caused it, like everything else
// that logs (SHIP-14). Without this, an email in the log cannot be tied to the signup.
func TestConsoleLogsThroughTheRequestScopedLogger(t *testing.T) {
	buf := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(buf, nil)).With(slog.String("request_id", "req-42"))
	ctx := httpx.ContextWithLogger(context.Background(), log)

	if err := NewConsole().Send(ctx, "someone@example.com", "Subject", "Body"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.Contains(buf.String(), `"request_id":"req-42"`) {
		t.Errorf("console record is not attributable to its request: %s", buf.String())
	}
}

// "Sends nothing" is a structural property rather than an observable one — there is no
// call to assert the absence of. Holding no HTTP client is what makes it true, so that is
// what this checks. A future field is fine; a transport is the regression.
func TestConsoleHoldsNoTransport(t *testing.T) {
	typ := reflect.TypeOf(Console{})
	for i := range typ.NumField() {
		if strings.Contains(typ.Field(i).Type.String(), "http.") {
			t.Errorf("Console.%s is %s — the console implementation must not be able to "+
				"reach the network", typ.Field(i).Name, typ.Field(i).Type)
		}
	}
}

// Both implementations refuse an empty recipient, so development behaves the way staging
// will rather than differing in a way only a provider error would reveal.
func TestConsoleRefusesAMessageWithNoRecipient(t *testing.T) {
	if err := NewConsole().Send(context.Background(), "", "Subject", "Body"); !errors.Is(err, ErrNoRecipient) {
		t.Errorf("Send with no recipient: err = %v, want ErrNoRecipient", err)
	}
}
