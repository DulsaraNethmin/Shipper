package events

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"
)

// The event catalogue and the topic set it implies (SHIP-135).
//
// SHIP-134 left two things open and named this ticket for both: nothing created the Kafka topics,
// and there was no schema for what travels on them. They are one decision, because a topic is
// only meaningful to the degree that something is promised about the messages on it.
//
// # One topic per aggregate, and the topic name cannot carry the version
//
// shipper.job, shipper.bid, shipper.delivery, from [Aggregates] — rather than one topic for
// everything, so a consumer subscribes to what it cares about and retention can differ per
// aggregate. That much SHIP-134 already argued in topicFor.
//
// The obvious way to version a message is to put the version in the topic name — shipper.job.v2 —
// and it is wrong here for a structural reason rather than an aesthetic one. **Ordering is
// promised per aggregate** (000004_outbox.up.sql), and Kafka orders within one partition of one
// topic. One topic per aggregate is what makes that promise keepable, and it is what the
// publisher's advisory-lock design exists to feed. A version in the topic name is therefore a
// version per *aggregate*: `job.expiry_warned` gaining a field would move `job.status_changed`
// to a new topic too, and a job's events would end up split across two topics with no order
// between them. Going the other way — one topic per event type, versioned — loses the ordering
// outright, because two events about one job would be on two topics.
//
// So the topic set is per aggregate and closed, and the version travels with the message.
//
// # What "versioned" means here
//
// A [Schema] per event type: its aggregate, its version, the exact JSON field set of its payload
// at that version, and a bound on how large the payload may be.
//
// The failure worth preventing is the one internal/pagination's cursor prefix was built against
// (SHIP-66, Docs/11 §3): not a rejected event but a **stale one accepted as meaning something
// else** after a deployment. Two things make that hard to do by accident.
//
//   - **The version is written into the payload by [New]**, at the moment the row is written,
//     from this catalogue. It is a property of the row rather than of whatever the catalogue
//     happens to say when the publisher reaches it — so an outbox row written before a
//     deployment and drained after it carries the version it was actually written under, which
//     is the only version it is true of. It is also what makes a row self-describing at three in
//     the morning: `select payload->>'schema_version'` answers "what shape is this".
//   - **The field set is recorded in cmd/api/events_golden.txt**, derived from the payload struct
//     by reflection rather than retyped. Changing a payload moves a line in that file; a line
//     that moved without its `v` moving is the defect this catalogue exists to make visible, and
//     it is visible in the diff beside the change rather than in a consumer's logs a week later.
//     cmd/api is where the golden lives for the same reason routes_golden.txt does: it is the one
//     binary that links every domain, so it is the only place the whole surface exists at once.
//
// # Everything that can permanently reject an event happens before the row is written
//
// This is the answer to Docs/11 §9's dead-letter question, and it is why the outbox still has no
// dead-letter path. SHIP-134 fails a whole pass when any event in it fails to publish, which is
// right for the failure that actually happens — the broker is unreachable and every row must stay
// claimable — and wrong for an event the broker will never accept, which would block its batch
// until somebody looked.
//
// Rather than build a recovery path for that event, this makes it unwritable. Enumerate what
// "permanently unacceptable" can mean and the list is short: the event type is one nothing
// consumes, its aggregate has no topic, or the payload is over the broker's message limit. All
// three are decided here, and [New] and [Outbox.Emit] check all three **inside the transaction
// making the state change** — where a failure rolls the change back, the caller is told, and the
// stack trace names the line that built the event. What remains at publish time is only the
// transient class, and for that class failing the batch and leaving every row claimable is
// exactly right.
//
// The revisit trigger is named rather than left to judgement: **the first event whose payload is
// legitimately unbounded — a document, a photograph, a manifest — must not travel in the outbox
// at all.** It goes to object storage and the event carries the key. If that is ever refused, the
// dead-letter question reopens and §9's other two answers are still there.

// Aggregate is what an event is about, and therefore which topic it publishes to.
//
// The set is closed, in the same way internal/boundaries.Domains and migrations.Blocks are
// closed: a fourth aggregate is a decision somebody records here, not something that happens.
// A topic's partition count cannot be reduced once chosen, so deciding all three at once is
// cheaper than deciding each one under whatever deadline first needs it.
type Aggregate string

const (
	AggregateJob      Aggregate = "job"
	AggregateBid      Aggregate = "bid"
	AggregateDelivery Aggregate = "delivery"
)

// Aggregates is the closed set, in the order Docs/06 §3 lists the domains behind them.
var Aggregates = []Aggregate{AggregateJob, AggregateBid, AggregateDelivery}

// TopicPrefix namespaces every topic this service owns, so that a broker shared with anything
// else stays legible.
const TopicPrefix = "shipper."

// TopicFor is where an aggregate type becomes a topic name.
//
// **This is the line Docs/11 §9 said SHIP-135 would change**, moved here from cmd/worker because
// the topic set and the schema are one decision and this is where the schema lives.
//
// Deliberately total: it maps any string rather than only the three in [Aggregates], and returns
// no error. That is not laxity, it is the dead-letter position above — the publisher must never
// be the thing that permanently rejects a row, so it does not get a way to. An aggregate outside
// the set cannot reach the outbox in the first place, because [Outbox.Emit] refuses it.
func TopicFor(aggregateType string) string { return TopicPrefix + aggregateType }

// Topic is the topic this aggregate's events publish to.
func (a Aggregate) Topic() string { return TopicFor(string(a)) }

// Topics is the whole topic set, which is what cmd/topics creates.
func Topics() []string {
	out := make([]string, 0, len(Aggregates))
	for _, a := range Aggregates {
		out = append(out, a.Topic())
	}
	return out
}

// The topic parameters. These are the part of the topic set that cannot be changed afterwards,
// which is why they are constants here rather than flags on the command that applies them.
const (
	// TopicPartitions is three, and three is a decision about ordering rather than about
	// throughput.
	//
	// The key is the aggregate id, so one job's events hash to one partition and Kafka keeps
	// their order within it — which is the promise 000004_outbox.up.sql makes and the whole
	// reason the publisher takes an advisory lock per aggregate. **Partitions can be added and
	// never removed, and adding one rehashes every key onto a different partition**, which
	// silently ends that promise for events already on the topic. So the number has to be
	// large enough now: one would serialise the entire system through a single consumer, and a
	// pilot that outgrows three has other things to celebrate.
	TopicPartitions = 3

	// TopicRetention is how long a message stays readable, set explicitly rather than
	// inherited.
	//
	// Seven days matches Kafka's own default, and stating it means the topic set is
	// reproducible on a broker whose default is something else. The outbox is the record of
	// truth (Docs/06 §4), so retention bounds how far a consumer may fall behind and still
	// catch up from the topic — not how long the event is knowable.
	TopicRetention = 7 * 24 * time.Hour
)

// DefaultReplicationFactor is one, which is correct locally and wrong everywhere else.
//
// A single-broker compose stack cannot replicate, and RequiredAcks: RequireAll on the producer
// means "every in-sync replica" — which is one replica here and is why the local stack
// acknowledges anything at all.
//
// **A deployed cluster needs three, and this is the one number in the topic set that is genuinely
// environment-dependent.** internal/config is a shared surface a domain branch may not edit
// (Docs/10 §9.2), so rather than add KAFKA_REPLICATION_FACTOR here, cmd/topics takes it as a
// flag defaulting to this — a deployment passes -replication 3 and needs no configuration change.
// Whoever next owns internal/config should fold it in.
const DefaultReplicationFactor = 1

// BrokerMessageLimitBytes is Kafka's default max.message.bytes, and the ceiling every payload
// bound below is chosen against.
//
// It is here as a documented number rather than as something enforced: the broker enforces it,
// and a test asserts that no schema in the catalogue permits a payload anywhere near it.
const BrokerMessageLimitBytes = 1_048_588

// DefaultMaxPayloadBytes bounds a payload when its schema names no bound of its own.
//
// Sixteen kibibytes is generous by any measure that matters — the largest field in any payload
// this catalogue holds today is a cancellation reason somebody typed on a phone — and it is two
// orders of magnitude below [BrokerMessageLimitBytes], so the envelope, the headers and the
// batching between them cannot close the gap.
//
// The number is not tuned and does not need to be. Its job is to make "a payload the broker will
// never accept" impossible to write, and any bound comfortably below the broker's does that.
const DefaultMaxPayloadBytes = 16 << 10

// SchemaVersionField is the payload key [New] writes the schema version into.
//
// A payload struct may not declare it; [Register] refuses one that does, so the field always
// means the catalogue's answer rather than a domain's.
const SchemaVersionField = "schema_version"

// Schema is one domain event's contract: what it is about, which version of it this is, what its
// payload contains, and how large that payload may be.
//
// A domain declares its own, from an init in its own package — internal/jobs/events.go — which
// keeps "domain events are emitted by the domain" true of the schema as well as of the emission,
// and means no domain track ever edits this package to add an event.
type Schema struct {
	// Type is the event name, `<aggregate>.<past tense>`: "job.status_changed".
	Type string

	// Aggregate is what the event is about, and so which topic it lands on. It must be one
	// of [Aggregates].
	Aggregate Aggregate

	// Version starts at 1 and goes up whenever the payload changes shape in a way that would
	// make an older consumer misread it. Adding an optional field is not that; renaming one,
	// removing one, or changing what one means is.
	Version int

	// Payload is the zero value of the struct the domain marshals into the payload. The field
	// set is derived from it by reflection rather than retyped, so the catalogue cannot
	// describe a shape the code does not have.
	Payload any

	// MaxPayloadBytes bounds the marshalled payload. Zero means [DefaultMaxPayloadBytes].
	MaxPayloadBytes int

	// fields is the derived JSON field set, `name:type`, sorted. Unexported because it is
	// computed rather than declared.
	fields []string
}

// Fields is the payload's JSON field set at this version, as `name:type`, sorted.
func (s Schema) Fields() []string { return slices.Clone(s.fields) }

// PayloadLimit is the bound this schema's payload is held to.
func (s Schema) PayloadLimit() int {
	if s.MaxPayloadBytes > 0 {
		return s.MaxPayloadBytes
	}
	return DefaultMaxPayloadBytes
}

// registry holds the catalogue. It is a type rather than a set of package variables so that a
// test can exercise registration without writing into the catalogue the process will run with.
type registry struct {
	byType map[string]Schema
}

func newRegistry() *registry { return &registry{byType: map[string]Schema{}} }

// catalogue is the process-wide registry, written by init and read afterwards.
//
// No mutex, for the same reason internal/pagination's bounds has none: every write happens in an
// init, which runs before main and before any test, and everything after is a read. A lock here
// would suggest a concurrency this value does not have.
var catalogue = newRegistry()

// Register adds a schema to the catalogue. It panics rather than returning an error, because it
// is called from init and a mis-declared event should not start the process.
//
// Call it from a domain's own package, from an init, and nowhere else.
func Register(s Schema) {
	if err := catalogue.register(s); err != nil {
		panic("events: " + err.Error())
	}
}

// Lookup returns the schema for an event type.
func Lookup(eventType string) (Schema, bool) { return catalogue.lookup(eventType) }

// Catalogue is every registered schema, ordered by topic then by type.
//
// It holds what the calling binary links. That is complete wherever it matters — a process can
// only emit events from a domain it links, and the publisher deliberately does not consult the
// catalogue at all — and cmd/api, which links every domain, is where the golden file lives.
func Catalogue() []Schema { return catalogue.all() }

func (r *registry) register(s Schema) error {
	if s.Type == "" {
		return fmt.Errorf("a schema with no type cannot be registered")
	}
	if _, dup := r.byType[s.Type]; dup {
		return fmt.Errorf("%s is registered twice; one event type has one schema", s.Type)
	}
	if !slices.Contains(Aggregates, s.Aggregate) {
		return fmt.Errorf("%s names aggregate %q, which is not one of %v; a fourth aggregate "+
			"is a new topic and is decided in internal/events, not in a domain",
			s.Type, s.Aggregate, Aggregates)
	}
	if want := string(s.Aggregate) + "."; !strings.HasPrefix(s.Type, want) {
		return fmt.Errorf("%s is on the %s aggregate, so it must be named %s<past tense>",
			s.Type, s.Aggregate, want)
	}
	if s.Version < 1 {
		return fmt.Errorf("%s has version %d; versions start at 1", s.Type, s.Version)
	}
	if s.MaxPayloadBytes < 0 || s.MaxPayloadBytes > BrokerMessageLimitBytes {
		return fmt.Errorf("%s permits a payload of %d bytes, which the broker's %d byte "+
			"limit would refuse; an event this large belongs in object storage with the "+
			"event carrying its key", s.Type, s.MaxPayloadBytes, BrokerMessageLimitBytes)
	}

	fields, err := fieldsOf(s.Payload)
	if err != nil {
		return fmt.Errorf("%s: %w", s.Type, err)
	}
	s.fields = fields

	r.byType[s.Type] = s
	return nil
}

func (r *registry) lookup(eventType string) (Schema, bool) {
	s, ok := r.byType[eventType]
	return s, ok
}

func (r *registry) all() []Schema {
	out := make([]Schema, 0, len(r.byType))
	for _, s := range r.byType {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Aggregate != out[j].Aggregate {
			return out[i].Aggregate < out[j].Aggregate
		}
		return out[i].Type < out[j].Type
	})
	return out
}

// fieldsOf derives a payload's JSON field set from its struct tags.
//
// `name:type` rather than the name alone, because a field that changed type is exactly the
// change a consumer misreads without noticing — a string that became a number deserialises into
// a different thing and nothing says so.
//
// Deliberately one level deep and deliberately strict about what it will describe. An embedded
// struct flattens into the JSON at a distance from the declaration, and a payload whose shape
// depends on reading two structs is a payload whose golden line is not a fingerprint. Refusing
// it is cheaper than describing it, and no event needs one.
func fieldsOf(payload any) ([]string, error) {
	if payload == nil {
		return nil, fmt.Errorf("has no payload type; register the zero value of the struct " +
			"the domain marshals, so the field set is derived rather than retyped")
	}

	t := reflect.TypeOf(payload)
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("has a %s payload; an event payload is a JSON object, so the "+
			"schema names a struct", t.Kind())
	}

	out := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Anonymous {
			return nil, fmt.Errorf("embeds %s; a payload's fields are declared where the "+
				"payload is, so that this list is the whole of it", f.Type)
		}
		if !f.IsExported() {
			continue
		}

		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		if name == SchemaVersionField {
			return nil, fmt.Errorf("declares %s, which [New] writes from this catalogue; "+
				"remove it rather than have two answers", SchemaVersionField)
		}
		out = append(out, name+":"+f.Type.String())
	}

	sort.Strings(out)
	return out, nil
}
