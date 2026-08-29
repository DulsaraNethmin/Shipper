package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/png"

	// Aliased to the Australian spelling so that the twelve uses below read the way the rest of
	// this repository writes, and the American form appears exactly once — here, on an import
	// path nobody chose the spelling of.
	colour "image/color" // spelling:ok — standard library import path
)

// The proof photograph, drawn rather than photographed (SHIP-186).
//
// # Why the seed draws its own image
//
// `Docs/01` §4.4 admits a delivery as complete on a photograph or a recorded exception and never on
// neither, so a dataset with a completed delivery in it needs a real object in the store — one the
// platform signed permission for, with the content type and the length it authorised, that a
// customer can open through `GET /v1/jobs/{id}/delivery/proof` and see.
//
// A photograph committed to the repository would be the obvious way to get one, and it is the wrong
// one for this ticket. A real photograph of a real doorway is exactly the kind of thing the *Done
// when*'s "no real personal information" clause is about: a street number, a vehicle registration,
// a face at a window, none of which anybody would notice until the demonstration was in front of
// somebody. A drawing has nothing in it that was not put there.
//
// It is also smaller than a photograph, needs no binary in git, and differs per job — so the
// completed delivery and any later one are visibly different images rather than the same file
// twice, which is what a reader would otherwise assume they were looking at.
//
// # No text, and that is a dependency decision rather than an aesthetic one
//
// Drawing a legible word needs a font, and the standard library has none — `golang.org/x/image` is
// not in `go.mod` and adding it, to a shared file, so that a demonstration image could say "DEMO"
// on it, is not a trade worth taking. The image is recognisably a diagram instead: flat colour,
// hard edges, a drawn crate. Nobody will mistake it for a camera's output, which is the property
// that actually matters.

const (
	proofWidth       = 1024
	proofHeight      = 768
	proofContentType = "image/png"
)

// proofPhotograph draws the image proving one delivery, deterministically from seedFrom.
//
// The same input always produces the same bytes, so a re-run that did upload again would upload an
// identical object rather than a second, subtly different one.
func proofPhotograph(seedFrom string) ([]byte, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, proofWidth, proofHeight))

	// The palette is derived from the caller's string so that two jobs' photographs are
	// distinguishable at a glance. Only the hue moves; the composition does not.
	digest := sha256.Sum256([]byte(seedFrom))
	accent := colour.RGBA{R: 60 + digest[0]/2, G: 70 + digest[1]/2, B: 90 + digest[2]/2, A: 255}

	horizon := proofHeight * 58 / 100

	// Background: a vertical gradient down to the horizon, then a flat floor. Two flat regions
	// would read as a flag; the gradient is what makes it read as a space.
	for y := range proofHeight {
		var row colour.RGBA
		switch {
		case y < horizon:
			shade := uint8(200 - (y * 45 / horizon))
			row = colour.RGBA{R: shade - 20, G: shade - 8, B: shade, A: 255}
		default:
			row = colour.RGBA{R: 96, G: 98, B: 104, A: 255}
		}
		for x := range proofWidth {
			canvas.SetRGBA(x, y, row)
		}
	}

	// The crate, centred on the floor line.
	crate := image.Rect(proofWidth/2-230, horizon-210, proofWidth/2+230, horizon+150)
	fill(canvas, crate, colour.RGBA{R: 168, G: 130, B: 84, A: 255})

	// Its lid and its shadow, which is all it takes to stop the rectangle reading as a hole.
	fill(canvas, image.Rect(crate.Min.X, crate.Min.Y, crate.Max.X, crate.Min.Y+46),
		colour.RGBA{R: 190, G: 150, B: 98, A: 255})
	fill(canvas, image.Rect(crate.Min.X-40, crate.Max.Y, crate.Max.X+40, crate.Max.Y+26),
		colour.RGBA{R: 74, G: 76, B: 82, A: 255})

	// Two straps in the derived accent, so the per-job colour lands somewhere structural.
	fill(canvas, image.Rect(crate.Min.X+96, crate.Min.Y, crate.Min.X+140, crate.Max.Y), accent)
	fill(canvas, image.Rect(crate.Max.X-140, crate.Min.Y, crate.Max.X-96, crate.Max.Y), accent)

	// A label plate on the crate's face. Blank on purpose: a drawn rectangle where a consignment
	// note would be, carrying nothing that could identify anybody.
	plate := image.Rect(crate.Min.X+176, crate.Min.Y+150, crate.Max.X-176, crate.Min.Y+264)
	fill(canvas, plate, colour.RGBA{R: 246, G: 244, B: 238, A: 255})
	outline(canvas, plate, 4, colour.RGBA{R: 120, G: 96, B: 62, A: 255})

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, canvas); err != nil {
		return nil, fmt.Errorf("encoding the proof photograph: %w", err)
	}
	return encoded.Bytes(), nil
}

// fill paints one rectangle, clipped to the canvas.
func fill(canvas *image.RGBA, area image.Rectangle, shade colour.RGBA) {
	area = area.Intersect(canvas.Bounds())
	for y := area.Min.Y; y < area.Max.Y; y++ {
		for x := area.Min.X; x < area.Max.X; x++ {
			canvas.SetRGBA(x, y, shade)
		}
	}
}

// outline draws a border of the given thickness inside area.
func outline(canvas *image.RGBA, area image.Rectangle, thickness int, shade colour.RGBA) {
	fill(canvas, image.Rect(area.Min.X, area.Min.Y, area.Max.X, area.Min.Y+thickness), shade)
	fill(canvas, image.Rect(area.Min.X, area.Max.Y-thickness, area.Max.X, area.Max.Y), shade)
	fill(canvas, image.Rect(area.Min.X, area.Min.Y, area.Min.X+thickness, area.Max.Y), shade)
	fill(canvas, image.Rect(area.Max.X-thickness, area.Min.Y, area.Max.X, area.Max.Y), shade)
}
