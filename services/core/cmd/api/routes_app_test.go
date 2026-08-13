package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/idempotency"
)

func routerWithApp(t *testing.T, app config.App) http.Handler {
	t.Helper()

	deps := testDeps()
	// Every domain's handler is built during attach, so this literal has to carry what all of
	// them need rather than what this test reads. Replacing Config wholesale is what makes that
	// easy to forget: the delivery keyset is here because a router built without one stops the
	// process (SHIP-107), not because a minimum-version test has any use for it.
	deps.Config = &config.Config{
		App:      app,
		Identity: testIdentityConfig(),
		Delivery: testDeliveryConfig(),
	}
	return newRouter(deps, idempotency.NewMemoryStore(), testAuthenticator(), nil)
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
