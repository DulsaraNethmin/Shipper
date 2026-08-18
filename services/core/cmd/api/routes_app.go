package main

import (
	"net/http"
	"time"

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
		Limit:   LimitPublicRead,
		Handler: minimumVersionHandler,
	})

	// SHIP-167a. Beside the floor rather than under a domain, because it is the same kind of
	// thing: something the app is told about itself, changed by configuration rather than by a
	// release. There is no domain behind it and no table it reads.
	register(Route{
		Method:  http.MethodGet,
		Pattern: "/app/policy",
		Group:   GroupV1,
		Auth:    Public,
		Limit:   LimitPublicRead,
		Handler: appPolicyHandler,
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

// appPolicyResponse is the body of GET /v1/app/policy (SHIP-167a).
//
// # Why these two numbers are served at all
//
// SHIP-127's four-hour unsynced nudge and SHIP-130's compression budget are both operational
// numbers — the kind CLAUDE.md's "anything expected to change under operational pressure lives
// server-side" is written about — and both were compiled into the client because **both fire on
// a handset that by assumption has no connection.** That is the premise of the features rather
// than an oversight in them, so an endpoint fetched at the moment of use could never have worked.
//
// What does work is an endpoint the app reads *while it still has signal* and keeps. The client
// caches the last response and applies it offline; the compiled default is the floor for an
// install that has never once been online, and is the only case it is used for.
//
// # Flat rather than grouped, unlike minimumVersionResponse
//
// The floor is keyed by platform because platforms multiply — adding one should be an added key
// rather than a reshaped response. Nothing here multiplies: there is one nudge threshold and one
// budget, the same on every handset. A `proof: {…}` wrapper around a single field would be a
// group invented for a second member that does not exist.
//
// # What is deliberately not here
//
// **The longest edge of a compressed photograph is not served**, though it sits in the same client
// policy object. It is a legibility judgement rather than an operational one — Docs/01 §4.4 makes
// the photograph the evidence, and 1600 pixels is what keeps a licence plate readable when the
// moderator zooms into a corner — so moving it under pressure would trade evidence for bytes
// silently. The backlog row asks for the threshold and the budget, and this serves those.
type appPolicyResponse struct {
	// UnsyncedNudgeAfterSeconds is Docs/02 §3.1's second rung, in seconds.
	//
	// Seconds rather than a Go duration string, matching `retry_after_seconds` in
	// contracts/paths/identity.yaml: a client parsing "4h0m0s" would be parsing a Go
	// implementation detail, and every client language can divide an integer.
	UnsyncedNudgeAfterSeconds int `json:"unsynced_nudge_after_seconds"`

	// ProofCompressionBudgetBytes is the size a proof photograph is compressed towards.
	//
	// A budget, not the platform's bound. See config.App.ProofCompressionBudgetBytes: the bound
	// is STORAGE_MAX_UPLOAD_BYTES and is fifteen times larger, and startup refuses a deployment
	// that inverts them.
	ProofCompressionBudgetBytes int `json:"proof_compression_budget_bytes"`
}

// appPolicyHandler answers with the operational numbers the client applies locally.
//
// Public and unauthenticated, for the same reason the floor is and one more of its own. The floor's
// reason is that a build in trouble must still be able to ask; this one's is that **nothing here is
// about the caller.** There is no user in the response, no per-account value, and no way to make
// one — two integers, identical for every handset. An endpoint that required a session to learn how
// long to wait before prompting about unsynced work would be asking a device with no connection for
// a credential it refreshes over the network.
//
// It reads configuration on each request rather than capturing it once, so moving either number is
// changing the environment and restarting — the same operational shape as raising the build floor.
func appPolicyHandler(d Deps) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, appPolicyResponse{
			// Truncating rather than rounding, and it costs nothing: the value is a duration
			// operations type, and a sub-second component in a four-hour threshold is not a
			// setting anybody meant.
			UnsyncedNudgeAfterSeconds:   int(d.Config.App.UnsyncedNudgeAfter / time.Second),
			ProofCompressionBudgetBytes: d.Config.App.ProofCompressionBudgetBytes,
		})
	})
}
