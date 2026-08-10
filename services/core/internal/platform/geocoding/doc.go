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
// So an unrecognised address is an ordinary outcome, not an error:
//
//	Lookup(ctx context.Context, address string) (lat, lng float64, formatted string, found bool, err error)
//
// found is false with err nil when the provider answered and did not know the address.
// err is non-nil only when the lookup did not complete — an unreachable provider, a refused
// credential, a response that is not the agreed shape. The caller can tell the two apart
// without reading an error string, which is what lets SHIP-60 publish the job in the first
// case and retry in the second. Both implementations obey it.
//
// # No vendor, deliberately
//
// No document names a maps vendor, and Docs/06 §4.1's stated pattern is that the seam
// exists before the vendor does. [Provider] therefore speaks a generic HTTP contract — a
// GET under a configured base URL, with a bearer credential — taking base URL and key as
// its own [Options] rather than reading internal/config.
//
// The vendor is named at SHIP-60, the address value object, which is the first ticket that
// resolves anything real. Docs/11 §7 records that as decided before wave 1 rather than
// deferred by accident. Naming it should change provider.go and nothing else.
//
// # Why the method returns five values rather than a result struct
//
// A result struct declared here would have to be named in the signature, so jobs would have
// to import this package to declare its port — which is precisely the edge SHIP-11's lint
// fails the build over (Docs/06 §4.1, Docs/10 §2.3). The same argument rules out a sentinel
// error for not-found: errors.Is against geocoding.ErrNotFound is an import too.
//
// A wide signature of standard-library types is the cost of a seam with no dependency edge
// across it, and it buys something in return — comma-ok is checked by the compiler, whereas
// a sentinel error is only checked by whoever remembered to compare against it.
//
// The interface this package satisfies is declared by jobs, which is the domain that needs
// an address resolved — never here.
package geocoding
