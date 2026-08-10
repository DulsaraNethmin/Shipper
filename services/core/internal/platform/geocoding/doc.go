// Package geocoding resolves an address to coordinates and normalises what the customer
// typed into something a provider can navigate to.
//
// Two implementations exist today (Docs/06 §4.1):
//
//	provider.go   the maps provider
//	stub.go       used in tests, and where a build must not make a network call
//
// SHIP-60.
//
// # Why a stub is worth having
//
// Geocoding is the one external call on the job creation path. Left unstubbed, every test
// that publishes a job becomes a network test: slow, rate-limited, and failing for
// reasons that have nothing to do with the code under test.
//
// # A failed lookup is not a failed job
//
// The provider will occasionally not recognise a legitimate rural address. The domain
// decides what that means for publication (SHIP-60, SHIP-63); this package's job is to
// report the failure honestly rather than to return a plausible coordinate somewhere else.
//
// The interface this package satisfies is declared by jobs, which is the domain that needs
// an address resolved — never here.
package geocoding
