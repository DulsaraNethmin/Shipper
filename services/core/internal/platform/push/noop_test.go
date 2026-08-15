package push

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestTheNoopNeverRejects. The deregistration path is the same code in every environment
// (notifications.Service.Dispatch), so a development implementation that reported a rejection
// would delete real device tokens on the strength of nothing having happened.
func TestTheNoopNeverRejects(t *testing.T) {
	t.Parallel()

	noop := NewNoop()
	jobID := uuid.New()

	rejected, err := noop.Push(context.Background(), "a-token", "A job has been awarded.",
		"Both parties can see it in the app.", jobID)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if rejected {
		t.Fatal("the no-op reported a rejection, which would deregister a real device")
	}

	sent := noop.Sent()
	if len(sent) != 1 {
		t.Fatalf("recorded %d pushes, want 1", len(sent))
	}
	if sent[0].DeviceToken != "a-token" || sent[0].JobID != jobID {
		t.Errorf("recorded %+v", sent[0])
	}
}

// TestTheNoopRefusesAnEmptyToken, so development behaves the way staging will.
func TestTheNoopRefusesAnEmptyToken(t *testing.T) {
	t.Parallel()

	if _, err := NewNoop().Push(context.Background(), "", "t", "b", uuid.New()); err == nil {
		t.Fatal("accepted a message with nowhere to go")
	}
}

// TestAFingerprintIsNotTheToken. The value identifies somebody's handset and a development log
// is the least protected place in this system.
func TestAFingerprintIsNotTheToken(t *testing.T) {
	t.Parallel()

	const token = "fMEr9Xk2Q3-long-registration-token-from-firebase"
	if got := Fingerprint(token); got == token || len(got) > fingerprintLength+3 {
		t.Fatalf("Fingerprint(%q) = %q", token, got)
	}
	if got := Fingerprint("short"); got != "short" {
		t.Fatalf("a token shorter than the fingerprint became %q", got)
	}
}
