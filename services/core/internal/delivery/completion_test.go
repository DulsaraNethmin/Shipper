package delivery

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-123 — the two facts a delivered job carries that nothing held until now.
//
// # What this closes, and it had been open since SHIP-118 named it
//
// `Docs/01` §4.4 lists four things a delivered job requires: recipient name, delivery timestamp,
// delivery note, and photo proof. Three of the four were reachable — the timestamp is
// `actor_recorded_at`, and SHIP-115/116/118 made the evidence recordable and mandatory. **No column
// held the recipient or the note**, `000605`'s own comment named SHIP-123 as the ticket that would
// close it, and `Docs/11` §4 has carried the gap as documentation ahead of code ever since.
//
// # The rule is in both directions, and the second half is the one worth testing
//
// Required on 'Delivered' is what the document asks for. **Refused everywhere else** is what stops
// the columns becoming a general-purpose pair somebody attaches to a pickup, which would be
// recording a handover that did not happen. Both halves live in the domain — where a client is told
// which field is wrong — and in `ck_milestones_delivery_details`, where the rule stops depending on
// which function did the writing. Every test below drives one of the two layers deliberately.

// deliverable is a job driven to 'In transit' by its provider, ready for one delivery.
//
// Built through the service rather than in SQL, so that the milestone rows before the delivery are
// real ones — a fixture that moved the job with an UPDATE would leave the transition guard
// untested and, since SHIP-123, would also miss that the three earlier milestones are the ones the
// constraint has to *refuse* the new fields on.
func deliverable(t *testing.T, pool *pgxpool.Pool, svc *Service, provider, jobID uuid.UUID) {
	t.Helper()

	for i, milestone := range []Milestone{MilestoneEnRouteToPickup, MilestonePickedUp, MilestoneInTransit} {
		if _, _, err := recordMilestone(t, pool, svc, provider, jobID,
			Recording{Milestone: milestone, Key: "deliverable-" + string(rune('a'+i))}); err != nil {
			t.Fatalf("driving the job to %s: %v", milestone, err)
		}
	}
}

// TestADeliveredMilestoneCarriesTheRecipientAndTheNote is the *Done when*'s first clause.
//
// The values are read back out of the row rather than off the response, because the response is
// rendered from what the service returned and the row is what a customer's tracking view, a
// dispute and an administrator will actually read.
func TestADeliveredMilestoneCarriesTheRecipientAndTheNote(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "comp1-c@example.com", "+61400000770", "customer")
	provider := newAccount(t, pool, "comp1-p@example.com", "+61400000771", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	svc, _, objects := driverProofService()
	deliverable(t, pool, svc, provider, jobID)

	key := objects.holding("proof/"+jobID.String()+"/019bd7a1-2c44-7f10-9a2c-3d4e5f607182", aPhotograph())
	proof, err := svc.VerifyProof(t.Context(), pool, provider, jobID, key)
	if err != nil {
		t.Fatalf("verifying the photograph: %v", err)
	}

	record, _, err := recordMilestone(t, pool, svc, provider, jobID, Recording{
		Milestone:     MilestoneDelivered,
		Key:           "comp1-delivered",
		Proof:         proof,
		RecipientName: "R. Chen",
		DeliveryNote:  "Left with reception, signed for",
	})
	if err != nil {
		t.Fatalf("recording the delivery: %v", err)
	}

	var recipient, note string
	if err := pool.QueryRow(t.Context(),
		`SELECT recipient_name, delivery_note FROM milestones WHERE id = $1`, record.ID).
		Scan(&recipient, &note); err != nil {
		t.Fatalf("reading the delivered row: %v", err)
	}
	if recipient != "R. Chen" || note != "Left with reception, signed for" {
		t.Fatalf("the row holds %q / %q", recipient, note)
	}

	// And they come back out through the domain, so a reader that never touches SQL sees them.
	if record.RecipientName != "R. Chen" || record.DeliveryNote != "Left with reception, signed for" {
		t.Fatalf("the record holds %q / %q", record.RecipientName, record.DeliveryNote)
	}
}

// TestADeliveryWithoutTheFieldSetIsRefused is `Docs/01` §4.4's requirement, one field at a time.
//
// Each case names the field, because that is the layer's whole job: `ck_milestones_delivery_details`
// would refuse the same rows with a constraint name and a 500, which `Docs/10` §4.6 says explains
// nothing.
//
// **A name that is only spaces is refused as required rather than stored**, which is
// `Recording.normalise` collapsing before `Recording.problems` judges — and is the case a length
// check alone would let through into a column the database then accepts.
func TestADeliveryWithoutTheFieldSetIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "comp2-c@example.com", "+61400000772", "customer")
	provider := newAccount(t, pool, "comp2-p@example.com", "+61400000773", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	svc, _, _ := driverProofService()
	deliverable(t, pool, svc, provider, jobID)

	for _, tc := range []struct {
		name  string
		rec   Recording
		field string
	}{
		{"no recipient", Recording{RecipientName: "", DeliveryNote: "Left at the door"}, "recipient_name"},
		{"no note", Recording{RecipientName: "R. Chen", DeliveryNote: ""}, "delivery_note"},
		{"neither", Recording{}, "recipient_name"},
		{"a recipient of only spaces", Recording{RecipientName: "   ", DeliveryNote: "Left at the door"}, "recipient_name"},
		{"a note of only spaces", Recording{RecipientName: "R. Chen", DeliveryNote: "  "}, "delivery_note"},
		{"a recipient over the limit", Recording{RecipientName: strings.Repeat("x", maxRecipientName+1), DeliveryNote: "n"}, "recipient_name"},
		{"a note over the limit", Recording{RecipientName: "R. Chen", DeliveryNote: strings.Repeat("x", maxDeliveryNote+1)}, "delivery_note"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tc.rec
			rec.Milestone = MilestoneDelivered
			rec.Key = "comp2-" + tc.name
			rec.Exception = ExceptionRecipientObjected

			_, _, err := recordMilestone(t, pool, svc, provider, jobID, rec)

			var apiErr *httpx.Error
			if !errors.As(err, &apiErr) || apiErr.Code != httpx.CodeValidationFailed {
				t.Fatalf("got %v, want a validation failure naming %s", err, tc.field)
			}
			if field := fieldOf(t, apiErr); field != tc.field {
				t.Fatalf("the refusal names %q, want %q", field, tc.field)
			}
		})
	}

	if n := milestoneCount(t, pool, jobID); n != 3 {
		t.Fatalf("%d milestones on the job, want the 3 that drove it to In transit — a refused "+
			"delivery must write nothing", n)
	}
}

// TestOnlyADeliveredMilestoneCarriesThem is the other half of the rule, and the half a first draft
// leaves out.
//
// A recipient name on a `picked_up` is a handover that did not happen, and the column would happily
// hold it. Both the domain and `ck_milestones_delivery_details` refuse it.
func TestOnlyADeliveredMilestoneCarriesThem(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "comp3-c@example.com", "+61400000774", "customer")
	provider := newAccount(t, pool, "comp3-p@example.com", "+61400000775", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	svc, _, _ := driverProofService()

	for _, tc := range []struct {
		name  string
		rec   Recording
		field string
	}{
		{"a recipient on a pickup", Recording{RecipientName: "R. Chen"}, "recipient_name"},
		{"a note on a pickup", Recording{DeliveryNote: "Left with reception"}, "delivery_note"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := tc.rec
			rec.Milestone = MilestoneEnRouteToPickup
			rec.Key = "comp3-" + tc.name

			_, _, err := recordMilestone(t, pool, svc, provider, jobID, rec)

			var apiErr *httpx.Error
			if !errors.As(err, &apiErr) || apiErr.Code != httpx.CodeValidationFailed {
				t.Fatalf("got %v, want a validation failure naming %s", err, tc.field)
			}
			if field := fieldOf(t, apiErr); field != tc.field {
				t.Fatalf("the refusal names %q, want %q", field, tc.field)
			}
		})
	}
}

// TestTheDeliveryFieldSetIsAlsoTheDatabases is the layer that does not depend on which function did
// the writing.
//
// The same argument SHIP-118 makes about `000605` and `000600` makes about its own unique index: the
// service checks so that a caller is told something useful, and the database enforces so that being
// told is not the mechanism. This writes raw SQL, which is the only way to reach the second layer.
func TestTheDeliveryFieldSetIsAlsoTheDatabases(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "comp4-c@example.com", "+61400000776", "customer")
	provider := newAccount(t, pool, "comp4-p@example.com", "+61400000777", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	for _, tc := range []struct {
		name      string
		milestone string
		recipient any
		note      any
	}{
		{"delivered with no recipient", "Delivered", nil, "Left with reception"},
		{"delivered with no note", "Delivered", "R. Chen", nil},
		{"delivered with neither", "Delivered", nil, nil},
		{"delivered with an empty recipient", "Delivered", "  ", "Left with reception"},
		{"a pickup carrying a recipient", "Picked up", "R. Chen", nil},
		{"a pickup carrying a note", "Picked up", nil, "Left with reception"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, _ := uuid.NewV7()
			_, err := pool.Exec(t.Context(), `
				INSERT INTO milestones
					(id, job_id, milestone, actor_type, actor_id, actor_recorded_at,
					 recipient_name, delivery_note)
				VALUES ($1, $2, $3, 'provider', $4, now(), $5, $6)`,
				id, jobID, tc.milestone, provider, tc.recipient, tc.note)

			if err == nil {
				t.Fatal("the database accepted a row Docs/01 §4.4 forbids, so the rule lives only " +
					"in the function that happened to be called")
			}
			if !strings.Contains(err.Error(), "ck_milestones_delivery_details") {
				t.Fatalf("refused by something other than the pairing constraint: %v", err)
			}
		})
	}
}

// TestTheDeliveryFieldLimitsAreOneLimit is Docs/10 §3.4's pairing for a bound rather than an
// enumeration.
//
// Two numbers stated in two languages is two numbers that drift, and the drift is silent in the
// worse direction: a Go bound looser than the column's turns a client's perfectly good input into a
// constraint name and a 500. The constraint definition is read out of the catalogue rather than out
// of the migration file, so a later `ALTER` that changed one and not the other is caught.
func TestTheDeliveryFieldLimitsAreOneLimit(t *testing.T) {
	pool := pgtest.DB(t)

	var definition string
	if err := pool.QueryRow(t.Context(), `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint
		 WHERE conname = 'ck_milestones_delivery_details'`).Scan(&definition); err != nil {
		t.Fatalf("reading ck_milestones_delivery_details out of pg_constraint: %v", err)
	}

	// The bound is matched against the *column* rather than merely against the constraint text,
	// so that swapping the two numbers is caught. PostgreSQL normalises `BETWEEN 1 AND 120` into a
	// pair of comparisons when it renders the definition, which is why the expected string is
	// built from the catalogue's own spelling and not from the migration's.
	for column, want := range map[string]int{
		"recipient_name": maxRecipientName,
		"delivery_note":  maxDeliveryNote,
	} {
		bound := fmt.Sprintf("length(btrim(%s)) <= %d", column, want)
		if !strings.Contains(definition, bound) {
			t.Errorf("the constraint does not bound %s at the Go limit of %d (%q): %s",
				column, want, bound, definition)
		}
		if !strings.Contains(definition, fmt.Sprintf("length(btrim(%s)) >= 1", column)) {
			t.Errorf("the constraint does not refuse an empty %s: %s", column, definition)
		}
	}
}

// TestTheDriverIsToldWhenTheDeliveryIsFinished is the second clause of the *Done when*: "portal
// becomes read-only after".
//
// # Why the platform answers it rather than the page remembering
//
// A driver reloads. Component state does not survive that, `sessionStorage` would be the page
// inventing a fact about the delivery, and there is no driver-readable milestone list — `GET
// /v1/jobs/{id}/delivery/milestones` is `RequireUser`. So `delivered_at` is served, which is also
// where `Docs/07` §3 puts every question of this kind.
//
// Asserted over HTTP because the field's absence is `omitempty` and a struct comparison would not
// distinguish "absent" from "empty string" the way a client parsing JSON does.
func TestTheDriverIsToldWhenTheDeliveryIsFinished(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "comp5-c@example.com", "+61400000778", "customer")
	provider := newAccount(t, pool, "comp5-p@example.com", "+61400000779", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	_, grant, token := driverOnJob(t, pool, provider, jobID)

	svc, _, objects := driverProofService()
	router := newDriverRouterFor(t, pool, svc)

	// Before: no key at all, which is what keeps the closed-set guard in driverauth_test.go true
	// of an undelivered job.
	if _, ok := driverJobKeys(t, router, jobID, token.Value)["delivered_at"]; ok {
		t.Fatal("an undelivered job reports a delivered_at")
	}

	deliverable(t, pool, svc, provider, jobID)

	key := objects.holding("proof/"+jobID.String()+"/019bd7a1-2c44-7f10-9a2c-3d4e5f607182", aPhotograph())
	proof, err := svc.VerifyDriverProof(t.Context(), pool, grant, key)
	if err != nil {
		t.Fatalf("verifying the driver's photograph: %v", err)
	}
	if _, _, err := recordAsDriver(t, pool, svc, grant, Recording{
		Milestone:     MilestoneDelivered,
		Key:           "comp5-delivered",
		Proof:         proof,
		RecipientName: "R. Chen",
		DeliveryNote:  "Left with reception, signed for",
	}); err != nil {
		t.Fatalf("recording the delivery: %v", err)
	}

	// After: present, in UTC, and the link still opens the page — a driver may reopen it to check
	// what they recorded, and the field is presentation rather than authorisation.
	after := driverJobKeys(t, router, jobID, token.Value)
	delivered, ok := after["delivered_at"].(string)
	if !ok || delivered == "" {
		t.Fatalf("a delivered job reports %v for delivered_at", after["delivered_at"])
	}
	if !strings.HasSuffix(delivered, "Z") {
		t.Errorf("delivered_at = %q, want UTC", delivered)
	}
}

// newDriverRouterFor is [newDriverRouter] over a service the test built, so that the stubbed store
// used to verify a photograph is the same one the route reads through.
func newDriverRouterFor(t *testing.T, pool *pgxpool.Pool, svc *Service) http.Handler {
	t.Helper()

	handler, err := NewHandler(svc, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /v1/driver/jobs/{id}",
		RequireDriverToken(testDriverVerifier(t, clock.NewFixed(testDriverIssuedAt)))(handler.DriverJob()))
	return mux
}

// driverJobKeys is what `GET /v1/driver/jobs/{id}` answered, as a map, so a test can ask whether a
// key is present rather than whether a field is empty.
func driverJobKeys(t *testing.T, h http.Handler, jobID uuid.UUID, token string) map[string]any {
	t.Helper()

	rec := openLink(h, jobID, bearer(token))
	if rec.Code != http.StatusOK {
		t.Fatalf("the driver's own delivery answered %d: %s", rec.Code, rec.Body)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the response is not JSON: %v", err)
	}
	return body
}

// fieldOf reads the first field name out of a validation failure.
func fieldOf(t *testing.T, err *httpx.Error) string {
	t.Helper()

	if len(err.Details) == 0 {
		t.Fatalf("the refusal carries no field errors: %v", err)
	}
	return err.Details[0].Field
}
