package jobs

import "github.com/DulsaraNethmin/Shipper/services/core/internal/events"

// The jobs domain's entry in the event catalogue (SHIP-135).
//
// One file, listing every event this package emits, its version and the payload struct behind it.
// It is here rather than in internal/events for two reasons.
//
// **A domain declares its own events, in the same way it declares its own routes and its own
// ports.** CLAUDE.md's rule is that domain events are emitted by the domain rather than by the API
// layer; the schema of an event is the same kind of thing as the emission of it, and putting it
// beside the payload struct is what stops the two drifting. internal/events knows nothing about
// jobs, which is the boundary rule — the catalogue is a table it holds, not a list it writes.
//
// **And no domain track ever edits internal/events to add an event.** SHIP-136 adds bidding's and
// delivery's from files exactly like this one, in their own packages, editing nothing shared. That
// is the same mechanism cmd/api/routes_<domain>.go uses and for the same reason (Docs/10 §9.2).
//
// # Versions, and when one goes up
//
// All three are at version 1, which is what a first release looks like. A version goes up when the
// payload changes in a way that would make a consumer written against the old one misread it —
// a field renamed, removed, or given a different meaning. Adding an optional field is not that.
//
// The mechanism that makes this hard to forget is cmd/api/events_golden.txt: the field set below
// is derived from the struct by reflection and recorded there, so changing a payload moves a line
// in a committed file, and a line that moved without its `v` moving is visible in the diff beside
// the change that caused it. See internal/events/catalogue.go for the whole argument, including
// why the version is in the payload rather than in the topic name.
//
// # None of these carries a budget, and none of them ever may
//
// Docs/01 §4.3 keeps the customer's maximum private from providers. An event is a copy of a job
// that travels further than any endpoint — through the outbox, onto a topic, into every consumer
// there will ever be, and past every place a response body could have redacted it. The three
// payload structs below have no budget field; expiry_test.go reads the rows back out of the outbox
// and fails if one appears; and a test over the whole catalogue in cmd/api refuses any registered
// field whose name mentions a budget, in this domain or any later one.
func init() {
	events.Register(events.Schema{
		Type:      EventStatusChanged,
		Aggregate: events.AggregateJob,
		Version:   1,
		Payload:   statusChanged{},
	})

	events.Register(events.Schema{
		Type:      EventExpiryWarned,
		Aggregate: events.AggregateJob,
		Version:   1,
		Payload:   expiryWarned{},
	})

	events.Register(events.Schema{
		Type:      EventExpiryExtended,
		Aggregate: events.AggregateJob,
		Version:   1,
		Payload:   expiryExtended{},
	})
}
