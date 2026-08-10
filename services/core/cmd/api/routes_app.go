package main

import (
	"net/http"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// SHIP-167: the minimum supported build, per platform.
//
// # Why this is in the foundation rather than in M7 with the rest of hardening
//
// Docs/07 §6 and Docs/08 Step 3 both say the upgrade gate has to ship in the *first* build, and
// Docs/08 puts the reason plainly: "It cannot be added retroactively to builds already on
// devices, which is precisely when it is needed." A build that does not ask this question at
// launch can never be told to stop working. So the endpoint has to exist before SHIP-25 puts
// anything on a real device — which makes it foundation work that happens to carry an M7 ticket
// number.
//
// # Why it is not versioned away
//
// It sits inside /v1 because it is a product endpoint the app calls, not an operational one.
// That creates an obligation: whatever v2 does, this path has to keep answering for as long as
// any v1 build survives, or the gate stops working for exactly the builds it exists to retire.
func init() {
	register(Route{
		Method:  http.MethodGet,
		Pattern: "/app/minimum-version",
		Group:   GroupV1,
		Auth:    Public,
		Handler: minimumVersionHandler,
	})
}

// minimumVersionResponse is the body of GET /v1/app/minimum-version.
//
// Keyed by platform rather than flattened into ios_* and android_* fields, so that adding a
// platform is an added key rather than a reshaped response — and so the client reads the one
// entry it cares about.
type minimumVersionResponse struct {
	IOS     platformFloor `json:"ios"`
	Android platformFloor `json:"android"`
}

type platformFloor struct {
	// MinimumBuild is the lowest build number still permitted. A client whose own build
	// number is below this must block with an update prompt (SHIP-168).
	MinimumBuild int `json:"minimum_build"`

	// StoreURL is where to send the user. Empty during the pilot, when distribution is
	// TestFlight and Play internal testing and there is no public listing to link to.
	StoreURL string `json:"store_url,omitempty"`
}

// minimumVersionHandler answers with the floor for each platform.
//
// It is public and unauthenticated on purpose. The app calls it at launch, before it has
// decided whether it can sign in — and a build old enough to be blocked may be old enough that
// its authentication no longer works, which would otherwise leave it unable to discover that it
// needs updating.
//
// It reads configuration on each request rather than capturing the value once, so that the
// floor is raised by changing the environment and restarting rather than by a release. Docs/07
// §6 wants that to be an operational decision with an owner.
func minimumVersionHandler(d Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, minimumVersionResponse{
			IOS: platformFloor{
				MinimumBuild: d.Config.App.MinimumIOSBuild,
				StoreURL:     d.Config.App.IOSStoreURL,
			},
			Android: platformFloor{
				MinimumBuild: d.Config.App.MinimumAndroidBuild,
				StoreURL:     d.Config.App.AndroidStoreURL,
			},
		})
	})
}
