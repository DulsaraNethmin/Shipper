package jobs

import (
	"fmt"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// The goods-category catalogue (SHIP-58), and the rule that a job in a refused category cannot
// be published (SHIP-59).
//
// # The catalogue is configuration and this package does not hold a copy of it
//
// CLAUDE.md names "category lists" first among the things that live server-side because they move
// under operational pressure, and Docs/06 §5.3 requires them to move without a deploy. So the
// list arrives from internal/config through [WithCatalogue] and nothing in this file names a
// category. The one thing that is compiled in is the *shape* of the rule — that a category must
// be known, and that a refused one cannot be published — because that is a decision rather than a
// value, and it is the same distinction model.go draws when it keeps the transition table in Go
// and the twelve status names in a generator.
//
// # Why the domain re-checks what internal/config already validated
//
// [loader.goods] refuses a duplicate code, a code that is not lower snake case, and a catalogue
// with nothing carried, so a service built by cmd/api cannot reach [NewCatalogue] with any of
// them. The check is here anyway because the invariant belongs to this package rather than to
// that one: a test constructing a Service directly does not go through the loader, and a
// catalogue with two entries for one code would answer [Catalogue.Lookup] according to insertion
// order — a defect that shows up as one job in fifty being refused for no visible reason. It is
// cheap, it runs once at startup, and it fails where the cause is legible.

// Category is one entry in the catalogue served to clients.
//
// The domain's own type rather than config.GoodsCategory, because a domain may not import
// infrastructure's shapes any more than it may import an adapter's — cmd/api translates, exactly
// as it does for the geocoder and for profiles' document kinds. It carries the same five fields
// because there is nothing here for the translation to decide.
type Category struct {
	// Code is the stored form and what a client sends. Lower snake case, and stable: it is
	// written into jobs.goods_category on every job that names it.
	Code string

	// Label is the short human name; Description is the optional line beneath it. Both are
	// served and neither is ever compared against — a client that matched on the wording
	// would break the first time somebody fixed a typo.
	Label       string
	Description string

	// Carried says whether a job in this category may be published (SHIP-59).
	//
	// A refused category is still served. Docs/09's *Done when* for SHIP-59 is "a job in a
	// prohibited category cannot be published **and explains why**", and an explanation needs
	// the platform to know the name of the thing it is refusing — which it cannot do if the
	// refused entries were simply left out of the list.
	Carried bool

	// Provisional says the entry has not been through the legal review X-4 owns. See
	// config.Goods; it is per entry so that X-4's answer can land incrementally.
	Provisional bool
}

// Catalogue is the whole list, in the order it is served.
//
// A slice rather than a map, and the order is the configuration's. A client renders it in a
// picker, and sorting it here would take a decision away from whoever set it — the same argument
// config.Goods makes. [Catalogue.Lookup] is linear over at most a few dozen entries, which is not
// a cost worth a second data structure that could disagree with this one.
type Catalogue []Category

// NewCatalogue checks a catalogue and returns it.
//
// Called once, from cmd/api, on a list internal/config has already refused the obvious problems
// in. See this file's header for why it checks anyway.
func NewCatalogue(cs []Category) (Catalogue, error) {
	if len(cs) == 0 {
		return nil, fmt.Errorf("jobs: the catalogue is empty: %w", ErrCatalogueUnusable)
	}

	seen := make(map[string]bool, len(cs))
	carried := 0
	for _, c := range cs {
		if c.Code == "" {
			return nil, fmt.Errorf("jobs: a category has no code: %w", ErrCatalogueUnusable)
		}
		if seen[c.Code] {
			return nil, fmt.Errorf("jobs: %q appears twice: %w", c.Code, ErrCatalogueUnusable)
		}
		seen[c.Code] = true

		if c.Carried {
			carried++
		}
	}

	// See config.Goods: a catalogue of nothing but refusals serves perfectly well and refuses
	// every publication, which reads as a broken product rather than as a misconfiguration.
	if carried == 0 {
		return nil, fmt.Errorf("jobs: no category is carried: %w", ErrCatalogueUnusable)
	}

	return Catalogue(cs), nil
}

// Lookup finds a category by code.
//
// The second return distinguishes "no such category" from "a category that is not carried", and
// the two are answered differently on the wire: an unknown code is a field the client got wrong
// and is `validation_failed`; a known code that is refused is a policy decision and is
// [CodeProhibitedCategory]. Collapsing them would tell a customer who picked "dangerous goods"
// that the field was invalid, which is both untrue and unhelpful.
func (c Catalogue) Lookup(code string) (Category, bool) {
	for _, entry := range c {
		if entry.Code == code {
			return entry, true
		}
	}
	return Category{}, false
}

// Codes is every category code, in catalogue order.
//
// For validate.Errors.OneOf, whose own documentation names this ticket: "category lists and
// vehicle types are reference data served to the client (SHIP-58), so the client already has the
// list and a long enumeration in an error message is noise on a phone screen."
func (c Catalogue) Codes() []string {
	out := make([]string, 0, len(c))
	for _, entry := range c {
		out = append(out, entry.Code)
	}
	return out
}

// checkDraftCategory refuses a category the catalogue does not serve, in the error contract's
// shape (SHIP-58).
//
// # A refused category passes here
//
// This checks that the code is *known*, not that it is carried. Saving a draft that names
// "dangerous goods" is a legitimate thing for a customer to do — they may not know yet, and
// Docs/01 §4.1 lets them come back to it — and Docs/09 puts the prohibition "on publish". See
// [DraftFields.GoodsCategory].
//
// # An unwired catalogue is an error rather than a silent pass
//
// [Catalogue.Lookup] on a nil catalogue reports every code unknown, which would refuse every
// draft that names a category and look exactly like a client sending bad data. So the absence is
// asked about first and answered with [ErrNoCatalogue] — the failure [WithCatalogue] exists to
// make loud. The check is skipped entirely when no category was supplied, so a deployment that
// never wires one still serves every endpoint that does not involve a category.
func (s *Service) checkDraftCategory(f DraftFields) error {
	if f.GoodsCategory == nil || *f.GoodsCategory == "" {
		return nil
	}

	catalogue, err := s.Categories()
	if err != nil {
		return err
	}

	var e validate.Errors
	e.OneOf("goods_category", *f.GoodsCategory, catalogue.Codes())
	return e.Err()
}

// WithCatalogue supplies the goods-category catalogue (SHIP-58).
//
// # Absent means refuse, loudly, exactly as [WithBidders] does
//
// A Service built without one cannot validate a category and cannot publish a job. The tempting
// defaults are both wrong in the way that file describes: treating every category as unknown
// would refuse every draft that names one, and treating every category as carried would publish
// jobs in categories Docs/01 §2 puts out of scope — a policy failure that looks exactly like
// working software. So the operations that need a catalogue return [ErrNoCatalogue], which
// becomes a 500 naming itself, and a process wired without one fails on its first publication
// rather than on its hundredth.
//
// An Option rather than a fourth parameter to NewService, for the reason [Option] gives: the
// constructor has call sites across cmd/api and cmd/worker, and only some of them serve a route
// that touches a category.
func WithCatalogue(c Catalogue) Option {
	return func(s *Service) { s.catalogue = c }
}

// Categories is the catalogue this service was built with, for the endpoint that serves it.
//
// Returns [ErrNoCatalogue] rather than an empty list when there is none, on the reasoning in
// [WithCatalogue]: an empty catalogue and an unwired one are indistinguishable to a client, and
// the second is a defect in the composition root that should say so.
func (s *Service) Categories() (Catalogue, error) {
	if s.catalogue == nil {
		return nil, ErrNoCatalogue
	}
	return s.catalogue, nil
}
