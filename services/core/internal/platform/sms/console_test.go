package sms

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

// SHIP-35's acceptance criterion, second half: it logs to console in dev.
func TestConsoleRendersTheMessage(t *testing.T) {
	buf := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := httpx.ContextWithLogger(context.Background(), log)

	if err := NewConsole().Send(ctx, "+61400000000", "Your Shipper code is 481920"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var rec map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &rec); err != nil {
		t.Fatalf("console wrote no readable record: %v (%q)", err, buf.String())
	}
	if got, _ := rec["to"].(string); got != "+61400000000" {
		t.Errorf("record to = %q, want +61400000000", got)
	}

	// The code is legible on purpose: reading it out of the log is how a developer
	// completes phone verification without a handset.
	if body, _ := rec["body"].(string); !strings.Contains(body, "481920") {
		t.Errorf("record body = %q, want the code intact", body)
	}
}

// Every line an adapter writes is attributable to the request that caused it (SHIP-14).
func TestConsoleLogsThroughTheRequestScopedLogger(t *testing.T) {
	buf := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(buf, nil)).With(slog.String("request_id", "req-42"))
	ctx := httpx.ContextWithLogger(context.Background(), log)

	if err := NewConsole().Send(ctx, "+61400000000", "code"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !strings.Contains(buf.String(), `"request_id":"req-42"`) {
		t.Errorf("console record is not attributable to its request: %s", buf.String())
	}
}

// "Sends nothing" is a structural property — there is no call to assert the absence of.
// Holding no HTTP client is what makes it true, and a bill is what a regression costs.
func TestConsoleHoldsNoTransport(t *testing.T) {
	typ := reflect.TypeOf(Console{})
	for i := range typ.NumField() {
		if strings.Contains(typ.Field(i).Type.String(), "http.") {
			t.Errorf("Console.%s is %s — the console implementation must not be able to "+
				"reach the network", typ.Field(i).Name, typ.Field(i).Type)
		}
	}
}

func TestConsoleRefusesAMessageWithNoRecipient(t *testing.T) {
	if err := NewConsole().Send(context.Background(), "", "code"); !errors.Is(err, ErrNoRecipient) {
		t.Errorf("Send with no recipient: err = %v, want ErrNoRecipient", err)
	}
}
