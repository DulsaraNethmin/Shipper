// Package push delivers notifications to devices through Firebase Cloud Messaging, which
// fronts APNs for iOS as well as delivering to Android (Docs/06 §2).
//
// Two implementations exist today (Docs/06 §4.1):
//
//	fcm.go     dispatches through Firebase, and handles token rejection
//	noop.go    used in tests, and in development where no Firebase project is configured
//
// SHIP-139.
//
// # Token rejection is normal traffic, not an error
//
// FCM rejects a token whenever an app is uninstalled or its data is cleared, which happens
// constantly across a real install base. A rejected token means "deregister this device",
// and treating it as a dispatch failure produces an alert that fires forever and is
// eventually ignored — including on the day it means something (SHIP-140, SHIP-176).
//
// **The signature is what enforces that**, rather than this paragraph. [FCM.Push] returns
// `(rejected bool, err error)`, so a rejection is a value the caller has to name and cannot
// collapse into a failure by forgetting a sentinel check. The domain's response is to
// deregister the device and mark the notification complete for that address.
//
// # What may be in the body
//
// Nothing this package sends may contain an address, a goods description, or a full
// customer name (SHIP-141). A push notification renders on a locked screen, in front of
// whoever is holding the phone.
//
// # The one thing SHIP-139 could not build, named rather than narrowed away
//
// **Nothing here mints the bearer credential FCM wants.** Google's HTTP v1 API takes a
// short-lived OAuth access token exchanged from a service-account JSON key, and that exchange
// needs golang.org/x/oauth2/google — a module, and therefore a go.mod change, which the branch
// this ticket was built on may not make. So [Options.Credential] is a function the composition
// root supplies, [FCM] never sees key material, and the exchange is one closure away whenever
// the module lands.
//
// That is also why no part of this repository dispatches a real push today: there is no
// Firebase project, and a service-account key is on CLAUDE.md's never-commit list. What is
// demonstrated instead is the whole path up to Google's door — the request FCM would receive,
// every response it can answer, and what each of them does to a device token — against an
// httptest server standing in for the project. See Docs/11 §3.
//
// The interface this package satisfies is declared by notifications — never here.
package push
