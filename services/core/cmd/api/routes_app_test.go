package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

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
		testAuthenticator(), testDriverGuard())
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
