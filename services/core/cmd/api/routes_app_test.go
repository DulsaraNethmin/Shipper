package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/idempotency"
)

// withApp copies deps' configuration and replaces the one section under test, and the shape is
// the point (SHIP-15p).
//
// routerWithApp used to substitute a whole config.Config literal, which meant every domain's
// requirements had to be repeated in it — every handler is built during attach, so a router built
// without the delivery keyset stops the process (SHIP-107) whether or not a minimum-version test
// has any use for one. **Every new configuration section therefore broke this file silently**
// until somebody noticed and added the section to the literal. Docs/11 §9 carried that as an open
// item with no owner from wave 6, and Storage was the section that would have collected on it.
//
// Overwriting one field on a copy has the property the literal did not: a section added tomorrow
// arrives here already correct, because it arrives through whatever built the configuration.
func withApp(deps Deps, app config.App) Deps {
	cfg := *deps.Config
	cfg.App = app
	deps.Config = &cfg
	return deps
}

func routerWithApp(t *testing.T, app config.App) http.Handler {
	t.Helper()
	return newRouter(withApp(testDeps(), app), idempotency.NewMemoryStore(),
		testAuthenticator(), testDriverGuard(), testAdminGuard())
}

// TestTheAppFixtureOverwritesNothingButApp is the guard on that shape rather than on any value in
// it, and it is written so that no future configuration section has to remember it exists.
//
// The marker in a section this test has no use for is what stops the assertion being vacuous: a
// fixture that rebuilds the configuration from a literal cannot carry a value it does not name,
// and comparing the whole struct with App blanked on both sides catches that without naming a
// section either. Demonstrated by mutation — restoring the literal fails this test.
func TestTheAppFixtureOverwritesNothingButApp(t *testing.T) {
	base := testDeps()
	base.Config.Storage = config.Storage{Bucket: "a-bucket-only-this-test-names"}

	got := *withApp(base, config.App{MinimumIOSBuild: 7, MinimumAndroidBuild: 7}).Config
	if got.App.MinimumIOSBuild != 7 {
		t.Fatalf("App.MinimumIOSBuild = %d, want the value the fixture was given", got.App.MinimumIOSBuild)
	}

	want := *base.Config
	got.App, want.App = config.App{}, config.App{}
	if !reflect.DeepEqual(got, want) {
		t.Error("the app fixture changed configuration other than App. Overwrite the one field " +
			"under test on a copy of the configuration it was given — a literal here silently " +
			"drops every section it does not name, and every domain's handler is built from those.")
	}
}

func getMinimumVersion(t *testing.T, router http.Handler) (int, minimumVersionResponse) {
	t.Helper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/app/minimum-version", nil))

	var body minimumVersionResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decoding the response: %v (body %s)", err, rec.Body.String())
		}
	}
	return rec.Code, body
}

// SHIP-167's acceptance criterion: GET /v1/app/minimum-version returns the floor per platform.
func TestMinimumVersionReturnsTheFloorPerPlatform(t *testing.T) {
	router := routerWithApp(t, config.App{
		MinimumIOSBuild:     42,
		MinimumAndroidBuild: 17,
		IOSStoreURL:         "https://apps.apple.com/app/id000000",
		AndroidStoreURL:     "https://play.google.com/store/apps/details?id=au.com.shipper",
	})

	status, body := getMinimumVersion(t, router)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}

	if body.IOS.MinimumBuild != 42 {
		t.Errorf("ios.minimum_build = %d, want 42", body.IOS.MinimumBuild)
	}
	if body.Android.MinimumBuild != 17 {
		t.Errorf("android.minimum_build = %d, want 17", body.Android.MinimumBuild)
	}
	if body.IOS.StoreURL == "" || body.Android.StoreURL == "" {
		t.Error("a blocked build needs somewhere to send the user")
	}
}

// The floor comes from configuration, so raising it is an operational act rather than a release
// (Docs/07 §6). This is the test that would fail if someone replaced it with a constant.
func TestTheFloorComesFromConfiguration(t *testing.T) {
	_, first := getMinimumVersion(t, routerWithApp(t, config.App{MinimumIOSBuild: 1, MinimumAndroidBuild: 1}))
	_, second := getMinimumVersion(t, routerWithApp(t, config.App{MinimumIOSBuild: 99, MinimumAndroidBuild: 99}))

	if first.IOS.MinimumBuild == second.IOS.MinimumBuild {
		t.Error("the floor did not follow the configuration; is it hard-coded?")
	}
}

// During the pilot there is no public store listing to link to (Docs/01 §8), so the endpoint
// has to answer usefully with no URL rather than refusing to start or emitting an empty string
// the client would try to open.
func TestTheStoreLinkIsOptional(t *testing.T) {
	router := routerWithApp(t, config.App{MinimumIOSBuild: 5, MinimumAndroidBuild: 5})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/app/minimum-version", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var raw map[string]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if _, present := raw["ios"]["store_url"]; present {
		t.Error("store_url is emitted when empty; omitempty should leave it out entirely " +
			"so the client cannot try to open a blank link")
	}
}

// The app calls this before it has signed in — and a build old enough to be blocked may be old
// enough that its authentication no longer works. Requiring a token here would leave exactly
// those builds unable to discover that they need updating.
func TestMinimumVersionNeedsNoAuthentication(t *testing.T) {
	status, _ := getMinimumVersion(t, routerWithApp(t, config.App{MinimumIOSBuild: 1, MinimumAndroidBuild: 1}))
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		t.Errorf("status = %d; the upgrade gate must be reachable without credentials", status)
	}
}

func getAppPolicy(t *testing.T, router http.Handler) (int, appPolicyResponse) {
	t.Helper()

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/app/policy", nil))

	var body appPolicyResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decoding the response: %v (body %s)", err, rec.Body.String())
		}
	}
	return rec.Code, body
}

// SHIP-167a's acceptance criterion, the platform half: GET /v1/app/policy serves the
// unsynced-nudge threshold and the proof compression budget. The client half — cache, apply
// offline, fall back only when it has never had one — is held in apps/mobile/test/core/policy.
func TestAppPolicyServesTheThresholdAndTheBudget(t *testing.T) {
	router := routerWithApp(t, config.App{
		MinimumIOSBuild:             1,
		MinimumAndroidBuild:         1,
		UnsyncedNudgeAfter:          4 * time.Hour,
		ProofCompressionBudgetBytes: 1 << 20,
	})

	status, body := getAppPolicy(t, router)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}

	if body.UnsyncedNudgeAfterSeconds != 14400 {
		t.Errorf("unsynced_nudge_after_seconds = %d, want 14400 (four hours, Docs/02 §3.1)",
			body.UnsyncedNudgeAfterSeconds)
	}
	if body.ProofCompressionBudgetBytes != 1<<20 {
		t.Errorf("proof_compression_budget_bytes = %d, want %d",
			body.ProofCompressionBudgetBytes, 1<<20)
	}
}

// Both numbers come from configuration, which is the whole reason the endpoint exists: they are
// operations tuning knobs, and a client cannot be asked to wait for a store release. This is the
// test that fails if either is replaced by a constant.
func TestTheClientPolicyComesFromConfiguration(t *testing.T) {
	_, first := getAppPolicy(t, routerWithApp(t, config.App{
		UnsyncedNudgeAfter: 4 * time.Hour, ProofCompressionBudgetBytes: 1 << 20,
	}))
	_, second := getAppPolicy(t, routerWithApp(t, config.App{
		UnsyncedNudgeAfter: 90 * time.Minute, ProofCompressionBudgetBytes: 512 << 10,
	}))

	if first.UnsyncedNudgeAfterSeconds == second.UnsyncedNudgeAfterSeconds {
		t.Error("the nudge threshold did not follow the configuration; is it hard-coded?")
	}
	if first.ProofCompressionBudgetBytes == second.ProofCompressionBudgetBytes {
		t.Error("the compression budget did not follow the configuration; is it hard-coded?")
	}
}

// Nothing in the response is about the caller — two integers, identical on every handset — and
// the app reads it on a device that may have no usable session. Requiring one would put the
// values behind the very connection they exist to survive without.
func TestAppPolicyNeedsNoAuthentication(t *testing.T) {
	status, _ := getAppPolicy(t, routerWithApp(t, config.App{
		UnsyncedNudgeAfter: time.Hour, ProofCompressionBudgetBytes: 1 << 20,
	}))
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		t.Errorf("status = %d; the client policy must be reachable without credentials", status)
	}
}

// The wire carries seconds, not a Go duration string.
//
// `retry_after_seconds` in contracts/paths/identity.yaml is the precedent, and the reason is that
// "1h30m0s" is a fact about Go's formatter rather than about the setting. A client parsing it
// would be parsing this service's implementation.
func TestTheThresholdTravelsAsSeconds(t *testing.T) {
	router := routerWithApp(t, config.App{
		UnsyncedNudgeAfter: 90 * time.Minute, ProofCompressionBudgetBytes: 1 << 20,
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/app/policy", nil))

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	seconds, ok := raw["unsynced_nudge_after_seconds"].(float64)
	if !ok {
		t.Fatalf("unsynced_nudge_after_seconds = %#v, want a number", raw["unsynced_nudge_after_seconds"])
	}
	if int(seconds) != 5400 {
		t.Errorf("unsynced_nudge_after_seconds = %d, want 5400", int(seconds))
	}
}
