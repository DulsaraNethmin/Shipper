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
// # What may be in the body
//
// Nothing this package sends may contain an address, a goods description, or a full
// customer name (SHIP-141). A push notification renders on a locked screen, in front of
// whoever is holding the phone.
//
// The interface this package satisfies is declared by notifications — never here.
package push
