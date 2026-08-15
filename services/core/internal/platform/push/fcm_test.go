package push

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
)

// SHIP-139. No Firebase project exists and none may be created from this repository, so every
// test here stands an httptest server in for one and asserts the two things that survive the
// substitution: the request FCM would receive, and what each of its answers does to a device
// token.
//
// The second is the half the whole package is shaped around — doc.go's claim that a rejected
// token is normal traffic rather than an error — and TestARejectedTokenIsReportedAndNotRaised
// is the test the mutation in Docs/11 §3 breaks.

const testCredential = "ya29.test-access-token"

// fcmAgainst returns an adapter pointed at h, and the requests it receives.
func fcmAgainst(t *testing.T, h http.HandlerFunc) (*FCM, *[]*http.Request, *[]string) {
	t.Helper()

	var requests []*http.Request
	var bodies []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests = append(requests, r)
		bodies = append(bodies, string(body))
		h(w, r)
	}))
	t.Cleanup(server.Close)

	adapter, err := NewFCM(Options{
		ProjectID:  "shipper-test",
		BaseURL:    server.URL,
		Credential: func(context.Context) (string, error) { return testCredential, nil },
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("building the adapter: %v", err)
	}
	return adapter, &requests, &bodies
}

// rejection writes the failure shape FCM answers with, for one of its error codes.
func rejection(status int, errorCode string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":{"code":`+strconv.Itoa(status)+`,"status":"`+errorCode+`",
		 "message":"Requested entity was not found.","details":[
		   {"@type":"type.googleapis.com/google.firebase.fcm.v1.FcmError","errorCode":"`+errorCode+`"}]}}`)
	}
}

// TestARejectedTokenIsReportedAndNotRaised is the assertion doc.go's central argument rests on.
//
// **This is the test the Docs/11 §3 mutation breaks.** Making fcm.go return an error for a
// rejection instead of `rejected = true` fails every case below, which is what establishes that
// "a rejected token is not a dispatch failure" is demonstrated rather than merely stated.
func TestARejectedTokenIsReportedAndNotRaised(t *testing.T) {
	t.Parallel()

	// Every code FCM uses to say a token will never deliver again, at the status it uses.
	// UNREGISTERED is the common one — an uninstall — and the other two are a token from
	// another project and a token that was never one.
	cases := []struct {
		name   string
		status int
		code   string
	}{
		{"an uninstalled app", http.StatusNotFound, "UNREGISTERED"},
		{"a token from another project", http.StatusForbidden, "SENDER_ID_MISMATCH"},
		{"a token that is not one", http.StatusBadRequest, "INVALID_ARGUMENT"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			adapter, _, _ := fcmAgainst(t, rejection(c.status, c.code))

			rejected, err := adapter.Push(context.Background(), "dead-token",
				"A job has been awarded.", "Job 1", uuid.New())

			if err != nil {
				t.Fatalf("a %s answer raised an error: %v\n\n"+
					"doc.go: a rejected token means deregister this device, and treating "+
					"it as a dispatch failure produces an alert that fires forever",
					c.code, err)
			}
			if !rejected {
				t.Fatalf("a %s answer was not reported as a rejection, so nothing would "+
					"ever deregister the device", c.code)
			}
		})
	}
}

// TestA404WithNoBodyIsStillARejection covers the proxy case: something in front of FCM answers
// 404 with HTML or with nothing, and the only resource this endpoint addresses is the token.
func TestA404WithNoBodyIsStillARejection(t *testing.T) {
	t.Parallel()

	adapter, _, _ := fcmAgainst(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	rejected, err := adapter.Push(context.Background(), "dead-token", "t", "b", uuid.New())
	if err != nil {
		t.Fatalf("a bare 404 raised an error: %v", err)
	}
	if !rejected {
		t.Fatal("a bare 404 from the send endpoint was not read as a rejection")
	}
}

// TestATransportFailureIsAnErrorAndNotARejection is the other side of the same rule, and it is
// the one that stops the rejection path becoming a way to lose an install base.
//
// A credential that has expired, a quota, FCM being down: none of those say anything about the
// token, and treating them as rejections would deregister every device the platform has, one
// notification at a time, with nothing raised to anybody.
func TestATransportFailureIsAnErrorAndNotARejection(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"the credential is refused", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"status":"UNAUTHENTICATED","message":"invalid credential"}}`)
		}},
		{"the project is over quota", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"status":"RESOURCE_EXHAUSTED",
			  "details":[{"errorCode":"QUOTA_EXCEEDED"}]}}`)
		}},
		{"fcm is unwell", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"status":"UNAVAILABLE",
			  "details":[{"errorCode":"UNAVAILABLE"}]}}`)
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			adapter, _, _ := fcmAgainst(t, c.handler)

			rejected, err := adapter.Push(context.Background(), "live-token", "t", "b", uuid.New())
			if err == nil {
				t.Fatal("the send reported success")
			}
			if rejected {
				t.Fatalf("%s was read as the token being dead, which would deregister "+
					"a live device for a fault that has nothing to do with it", c.name)
			}
		})
	}
}

// TestTheCredentialIsNeverInAnError guards the one value in this file that must not be logged.
func TestTheCredentialIsNeverInAnError(t *testing.T) {
	t.Parallel()

	adapter, _, _ := fcmAgainst(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "the server is unwell")
	})

	_, err := adapter.Push(context.Background(), "live-token", "t", "b", uuid.New())
	if err == nil {
		t.Fatal("want a failure")
	}
	if strings.Contains(err.Error(), testCredential) {
		t.Fatalf("the credential is in the error: %v", err)
	}
}

// TestTheMessageIsWhatFCMExpects reads the request off the wire.
//
// The URL is the half nothing else can catch: the project is in the path, so a wrong one is a
// 404 per message, which — by the rule this package is built on — deregisters every device
// rather than raising anything.
func TestTheMessageIsWhatFCMExpects(t *testing.T) {
	t.Parallel()

	adapter, requests, bodies := fcmAgainst(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"name":"projects/shipper-test/messages/1"}`)
	})

	jobID := uuid.New()
	rejected, err := adapter.Push(context.Background(), "device-token-abcdef",
		"A job has been awarded.", "Both parties can see it in the app.", jobID)
	if err != nil || rejected {
		t.Fatalf("Push(…) = %v, %v; want false, nil", rejected, err)
	}

	if len(*requests) != 1 {
		t.Fatalf("FCM received %d requests, want 1", len(*requests))
	}
	req := (*requests)[0]

	if req.Method != http.MethodPost {
		t.Errorf("method %s, want POST", req.Method)
	}
	if want := "/v1/projects/shipper-test/messages:send"; req.URL.Path != want {
		t.Errorf("path %s, want %s", req.URL.Path, want)
	}
	if got := req.Header.Get(authHeader); got != "Bearer "+testCredential {
		t.Errorf("credential header %q", got)
	}

	var sent message
	if err := json.Unmarshal([]byte((*bodies)[0]), &sent); err != nil {
		t.Fatalf("the body is not the shape this adapter writes: %v", err)
	}
	if sent.Message.Token != "device-token-abcdef" {
		t.Errorf("token %q", sent.Message.Token)
	}
	if sent.Message.Notification.Title != "A job has been awarded." {
		t.Errorf("title %q", sent.Message.Notification.Title)
	}
	if sent.Message.Data["job_id"] != jobID.String() {
		t.Errorf("job_id %q, want %s — SHIP-145 routes on it", sent.Message.Data["job_id"], jobID)
	}
	if sent.Message.Android == nil || sent.Message.Android.Priority != "high" {
		t.Error("android priority is not high, so a delivery notification waits for the handset to wake")
	}
}

// TestNoDeviceTokenIsAFaultAndNotARejection: a row with an empty address should have been
// impossible, so it is raised rather than quietly deregistering a device that does not exist.
func TestNoDeviceTokenIsAFaultAndNotARejection(t *testing.T) {
	t.Parallel()

	adapter, requests, _ := fcmAgainst(t, func(http.ResponseWriter, *http.Request) {
		t.Error("a message with no token reached the network")
	})

	rejected, err := adapter.Push(context.Background(), "", "t", "b", uuid.New())
	if err == nil {
		t.Fatal("an empty device token was accepted")
	}
	if rejected {
		t.Fatal("an empty device token was reported as a rejection")
	}
	if len(*requests) != 0 {
		t.Fatal("it went to the network anyway")
	}
}

// TestConstructionRefusesWhatWouldFailPerMessage: every one of these is a deploy-time fault, and
// discovering it at the first dispatch means a channel that has been silently dead since.
func TestConstructionRefusesWhatWouldFailPerMessage(t *testing.T) {
	t.Parallel()

	credential := func(context.Context) (string, error) { return "t", nil }

	cases := []struct {
		name string
		opts Options
	}{
		{"no project", Options{Credential: credential}},
		{"no credential source", Options{ProjectID: "p"}},
		{"a project that is a path", Options{ProjectID: "a/b", Credential: credential}},
		{"a base URL that is not http", Options{ProjectID: "p", BaseURL: "ftp://x", Credential: credential}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewFCM(c.opts); err == nil {
				t.Fatal("accepted")
			}
		})
	}
}

// TestTheDefaultBaseURLIsGoogles: a deployment that sets nothing still reaches Firebase.
func TestTheDefaultBaseURLIsGoogles(t *testing.T) {
	t.Parallel()

	adapter, err := NewFCM(Options{
		ProjectID:  "shipper",
		Credential: func(context.Context) (string, error) { return "t", nil },
	})
	if err != nil {
		t.Fatalf("building: %v", err)
	}
	if want := DefaultBaseURL + "/v1/projects/shipper/messages:send"; adapter.sendURL != want {
		t.Errorf("send URL %s, want %s", adapter.sendURL, want)
	}
}

// TestOnlyStagingAndProductionDispatch holds the fallback direction. Choosing wrongly towards
// the no-op costs a developer a puzzled minute; choosing wrongly the other way wakes a handset
// belonging to somebody who has no idea why.
func TestOnlyStagingAndProductionDispatch(t *testing.T) {
	t.Parallel()

	for _, env := range []config.Environment{config.Staging, config.Production} {
		if UseNoop(env) {
			t.Errorf("%s uses the no-op", env)
		}
	}
	for _, env := range []config.Environment{config.Development, "something-else", ""} {
		if !UseNoop(env) {
			t.Errorf("%s dispatches for real", env)
		}
	}
}
