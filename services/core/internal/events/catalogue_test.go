package events

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The catalogue's own tests. Nothing here touches PostgreSQL or a broker: what is being tested is
// the declaration and the refusals, and the acceptance harness demonstrates the topics and the
// wire form against the real Kafka (scripts/verify/80-notifications.sh).
//
// Registration is exercised against a fresh registry rather than the package one. A test that
// registered into the catalogue the process runs with would leave its fixtures in cmd/api's golden
// comparison, which is the one place the catalogue is supposed to be exactly what the domains
// declared.

type samplePayload struct {
	JobID    string    `json:"job_id"`
	Reason   string    `json:"reason,omitempty"`
	Recorded time.Time `json:"recorded_at"`

	Ignored  string `json:"-"`
	Untagged int
	hidden   string //nolint:unused // present to prove an unexported field is not a wire field
}

func sample() Schema {
	return Schema{
		Type:      "job.sampled",
		Aggregate: AggregateJob,
		Version:   1,
		Payload:   samplePayload{},
	}
}

func TestRegisterDerivesTheFieldSetFromTheStruct(t *testing.T) {
	r := newRegistry()
	if err := r.register(sample()); err != nil {
		t.Fatalf("registering: %v", err)
	}

	got, ok := r.lookup("job.sampled")
	if !ok {
		t.Fatal("the schema was registered and cannot be looked up")
	}

	// Sorted, `name:type`, the json tag's name where there is one and the Go name where there
	// is not, `-` dropped and an unexported field absent. The type is carried because a field
	// that changed type is the change a consumer misreads without noticing.
	want := "Untagged:int job_id:string reason:string recorded_at:time.Time"
	if strings.Join(got.Fields(), " ") != want {
		t.Errorf("fields are %v, want [%s]", got.Fields(), want)
	}
}

func TestRegisterRefusesWhatWouldPublishToNowhere(t *testing.T) {
	cases := []struct {
		name   string
		schema Schema
		want   string
	}{
		{
			name:   "an aggregate with no topic",
			schema: Schema{Type: "invoice.raised", Aggregate: "invoice", Version: 1, Payload: samplePayload{}},
			want:   "not one of",
		},
		{
			name:   "a type that disagrees with its aggregate",
			schema: Schema{Type: "bid.accepted", Aggregate: AggregateJob, Version: 1, Payload: samplePayload{}},
			want:   "must be named job.",
		},
		{
			name:   "no version",
			schema: Schema{Type: "job.sampled", Aggregate: AggregateJob, Payload: samplePayload{}},
			want:   "versions start at 1",
		},
		{
			name:   "a payload that is not a struct",
			schema: Schema{Type: "job.sampled", Aggregate: AggregateJob, Version: 1, Payload: []string{}},
			want:   "an event payload is a JSON object",
		},
		{
			name:   "no payload type at all",
			schema: Schema{Type: "job.sampled", Aggregate: AggregateJob, Version: 1},
			want:   "derived rather than retyped",
		},
		{
			name: "a payload declaring the version itself",
			schema: Schema{Type: "job.sampled", Aggregate: AggregateJob, Version: 1,
				Payload: struct {
					Version int `json:"schema_version"`
				}{}},
			want: "which [New] writes from this catalogue",
		},
		{
			name: "a bound the broker would refuse anyway",
			schema: Schema{Type: "job.sampled", Aggregate: AggregateJob, Version: 1,
				Payload: samplePayload{}, MaxPayloadBytes: BrokerMessageLimitBytes + 1},
			want: "belongs in object storage",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := newRegistry().register(c.schema)
			if err == nil {
				t.Fatalf("registered %+v, want a refusal", c.schema)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("refused with %q, want it to mention %q", err, c.want)
			}
		})
	}
}

func TestRegisterRefusesTheSameTypeTwice(t *testing.T) {
	r := newRegistry()
	if err := r.register(sample()); err != nil {
		t.Fatalf("first registration: %v", err)
	}
	if err := r.register(sample()); err == nil {
		t.Fatal("registered job.sampled twice; one event type has one schema, and two would " +
			"mean the version on the wire depended on which init ran last")
	}
}

// The topic set is what cmd/topics creates, so a change to it is a change to the cluster. Held to
// a literal rather than derived, which is the point: adding an aggregate has to be typed twice.
func TestTheTopicSetIsThreeTopicsOnePerAggregate(t *testing.T) {
	want := []string{"shipper.job", "shipper.bid", "shipper.delivery"}

	got := Topics()
	if len(got) != len(want) {
		t.Fatalf("the topic set is %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("topic %d is %q, want %q", i, got[i], want[i])
		}
	}

	if TopicFor("job") != "shipper.job" {
		t.Errorf("TopicFor(job) is %q, want shipper.job", TopicFor("job"))
	}
}

// New's refusals are the dead-letter decision (catalogue.go): every permanent condition is checked
// where the transaction can still roll back, so nothing permanently unpublishable reaches the
// outbox and the publisher needs no dead-letter path.
func TestNewRefusesAnEventTheBrokerCouldNeverUsefullyCarry(t *testing.T) {
	id := uuid.Must(uuid.NewV7())

	t.Run("an unregistered type", func(t *testing.T) {
		_, err := New("job.invented_here", id, time.Now(), map[string]string{"a": "b"})
		if err == nil {
			t.Fatal("built an event for a type in no catalogue, which would publish to a " +
				"topic nobody consumes and could never be recognised")
		}
		if !strings.Contains(err.Error(), "not in the catalogue") {
			t.Errorf("refused with %q", err)
		}
	})

	t.Run("a payload that is not an object", func(t *testing.T) {
		withSample(t)
		if _, err := New("job.sampled", id, time.Now(), []string{"a"}); err == nil {
			t.Fatal("built an event with an array payload")
		}
	})

	t.Run("a payload over the schema's bound", func(t *testing.T) {
		withSample(t)
		_, err := New("job.sampled", id, time.Now(),
			samplePayload{Reason: strings.Repeat("x", DefaultMaxPayloadBytes)})
		if err == nil {
			t.Fatal("built an event larger than its schema permits; one of those in the " +
				"outbox blocks its batch for as long as nobody looks")
		}
		if !strings.Contains(err.Error(), "object storage") {
			t.Errorf("refused with %q, want it to say where a large payload belongs", err)
		}
	})
}

func TestNewStampsTheVersionAndTheAggregateFromTheCatalogue(t *testing.T) {
	withSample(t)
	id := uuid.Must(uuid.NewV7())

	e, err := New("job.sampled", id, time.Date(2026, 8, 12, 1, 2, 3, 0, time.UTC),
		samplePayload{JobID: id.String()})
	if err != nil {
		t.Fatalf("building: %v", err)
	}

	// The aggregate is the catalogue's rather than the caller's, so an event cannot be emitted
	// onto the wrong topic.
	if e.AggregateType != string(AggregateJob) {
		t.Errorf("aggregate type is %q, want %q", e.AggregateType, AggregateJob)
	}

	if v := SchemaVersionOf(e.Payload); v != 1 {
		t.Errorf("the stored payload reports version %d, want 1: %s", v, e.Payload)
	}

	// Spliced onto the front rather than round-tripped, so the domain's field order survives
	// and the version is the first thing a person reading the row sees.
	if !strings.HasPrefix(string(e.Payload), `{"schema_version":1,`) {
		t.Errorf("the payload is %s, want the version first", e.Payload)
	}

	var back map[string]json.RawMessage
	if err := json.Unmarshal(e.Payload, &back); err != nil {
		t.Fatalf("the stamped payload is not valid JSON: %v", err)
	}
	if _, ok := back["job_id"]; !ok {
		t.Errorf("stamping the version lost the payload: %s", e.Payload)
	}
}

// An empty object is the one shape the splice cannot treat as "{" plus a remainder, and a payload
// with every field omitempty marshals to exactly that.
func TestTheVersionIsStampedOntoAnEmptyPayloadToo(t *testing.T) {
	got, err := withSchemaVersion([]byte("{}"), 2)
	if err != nil {
		t.Fatalf("stamping an empty object: %v", err)
	}
	if string(got) != `{"schema_version":2}` {
		t.Errorf("stamped to %s", got)
	}
}

// SchemaVersionOf answers zero for a row written by hand, which is what the acceptance harness
// does to arrange a transaction that rolls back. Zero is a true statement about that row; the
// catalogue's current answer would not be.
func TestSchemaVersionOfARowThatCarriesNoneIsZero(t *testing.T) {
	if v := SchemaVersionOf([]byte(`{"sequence": 1}`)); v != 0 {
		t.Errorf("version is %d, want 0", v)
	}
	if v := SchemaVersionOf([]byte(`not json`)); v != 0 {
		t.Errorf("version is %d, want 0", v)
	}
}

// Emit's half of the same guard, for an Event assembled by hand rather than through New. This is
// the check that reaches the database, so it is the one that decides what can be in the table.
func TestEmitRefusesAnEventTheCatalogueDoesNotRecognise(t *testing.T) {
	withSample(t)
	id := uuid.Must(uuid.NewV7())

	unknown := Event{ID: id, AggregateType: "job", AggregateID: id,
		Type: "job.hand_written", Payload: json.RawMessage(`{}`), OccurredAt: time.Now()}
	if err := checkAgainstCatalogue(unknown); err == nil {
		t.Error("a type in no catalogue was accepted into the outbox")
	}

	misrouted := Event{ID: id, AggregateType: "bid", AggregateID: id,
		Type: "job.sampled", Payload: json.RawMessage(`{}`), OccurredAt: time.Now()}
	err := checkAgainstCatalogue(misrouted)
	if err == nil {
		t.Fatal("a job event emitted on the bid aggregate was accepted; it would publish to " +
			"shipper.bid and be read by nobody")
	}
	if !strings.Contains(err.Error(), "shipper.bid") {
		t.Errorf("refused with %q, want it to name the topic it would have gone to", err)
	}
}

// withSample registers the fixture schema in the process catalogue and removes it afterwards.
//
// New and Emit read the package registry, so testing them needs an entry in it. Removing it again
// is what keeps cmd/api's golden comparison a statement about what the domains declared — these
// packages' tests run in different binaries, but the discipline is worth keeping local anyway.
func withSample(t *testing.T) {
	t.Helper()

	if _, already := Lookup("job.sampled"); already {
		return
	}
	if err := catalogue.register(sample()); err != nil {
		t.Fatalf("registering the fixture: %v", err)
	}
	t.Cleanup(func() { delete(catalogue.byType, "job.sampled") })
}
