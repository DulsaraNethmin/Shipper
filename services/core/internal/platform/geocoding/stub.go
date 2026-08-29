package geocoding

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
)

// Stub resolves an address without leaving the process.
//
// It is what runs in tests and anywhere a build must not make a network call. Every job
// published in a test would otherwise be a network test — slow, rate-limited, and failing
// for reasons unrelated to the code under test.
//
// The same address always resolves to the same coordinate, in this process and in the next
// one, so a golden file or an assertion on a distance stays valid. Nothing here is a real
// place: it is a stable fiction, which is all a test needs and rather less than a
// production caller would like to believe.
type Stub struct {
	// Unknown lists addresses the stub reports as not found, compared after the same
	// tidying it applies everywhere. It is how a test exercises the path where a
	// legitimate rural address does not resolve (SHIP-60, SHIP-63).
	Unknown []string

	// Err, when non-nil, is returned by every lookup instead of a result. It exists so a
	// test can exercise the difference between "there is no such place" and "we could not
	// ask", which are separate outcomes and must stay that way.
	Err error
}

// NewStub returns a stub that reports the given addresses as not found and resolves
// everything else.
func NewStub(unknown ...string) *Stub { return &Stub{Unknown: unknown} }

// Australia's bounding box, roughly: Cape York to southern Tasmania, and the Western
// Australian coast to the Queensland one.
//
// Coordinates are placed inside it rather than at (0, 0) because every distance in the
// product is in kilometres from somewhere real (Docs/10 §3.3, SHIP-81's eligibility radius).
// A stub answering the Gulf of Guinea produces test distances nobody can sanity-check, and
// a map that renders in the wrong ocean when somebody finally looks at one.
const (
	latSouth = -43.6
	latNorth = -10.7
	lngWest  = 113.3
	lngEast  = 153.6
)

// Lookup resolves address deterministically, with the same signature as [Provider.Lookup].
//
// The two are interchangeable because the domain declares the interface and Go satisfies it
// structurally (Docs/10 §2.3) — neither this file nor provider.go names the other.
func (s *Stub) Lookup(_ context.Context, address string) (lat, lng float64, formatted string, found bool, err error) {
	if s.Err != nil {
		return 0, 0, "", false, s.Err
	}

	tidy := tidyAddress(address)
	if tidy == "" {
		return 0, 0, "", false, nil
	}
	for _, u := range s.Unknown {
		if tidyAddress(u) == tidy {
			return 0, 0, "", false, nil
		}
	}

	h := fnv.New64a()
	_, _ = h.Write([]byte(strings.ToLower(tidy)))
	sum := h.Sum64()

	// Two independent fractions out of one hash: the low six digits and the next six.
	lat = round6(latSouth + fraction(sum%1_000_000)*(latNorth-latSouth))
	lng = round6(lngWest + fraction(sum/1_000_000%1_000_000)*(lngEast-lngWest))

	return lat, lng, tidy, true, nil
}

// tidyAddress collapses whitespace and trims, which is the whole of the stub's
// normalisation.
//
// It deliberately does not pretend to know Australian address formats — no state
// abbreviation, no postcode placement, no unit-number convention. That work belongs to
// SHIP-60 and to whichever provider is chosen, and a stub that guessed at it would be
// asserting a format the real one might not produce.
func tidyAddress(address string) string {
	return strings.Join(strings.Fields(address), " ")
}

func fraction(n uint64) float64 { return float64(n) / 1_000_000 }

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }
