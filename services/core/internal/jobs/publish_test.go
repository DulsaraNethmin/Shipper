package jobs

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-63 against a real PostgreSQL.
//
// The transition, the trigger that refuses a status write without a history row, and the
// expiry trigger that fires as the job becomes Open are all the database's, and Docs/06 §4.1 is
// the argument for not mocking any of them: "a mock happily accepts a write that the actual
// constraint would reject."
//
// TestPublishingProhibitedGoodsIsRefusedAndSaysWhy is the test SHIP-59's commit names as the
// end-to-end demonstration of its own *Done when*. Until this file existed nothing in the platform
// could publish, so the prohibition could only be shown at the rule.

// verifiedCustomer is an account that may publish — Docs/04 §2's two checks both satisfied.
//
// [newCustomer] deliberately leaves both timestamps NULL, because every test written before this
// ticket wanted an account that could create drafts and nothing was checking verification. That is
// the right default and this is the exception, so the verification is written here rather than
// added there.
func verifiedCustomer(t *testing.T, pool *pgxpool.Pool, email, phone string) uuid.UUID {
	t.Helper()

	id := newCustomer(t, pool, email, phone)
	if _, err := pool.Exec(t.Context(),
		`UPDATE users SET email_verified_at = now(), phone_verified_at = now() WHERE id = $1`,
		id); err != nil {
		t.Fatalf("verifying %s: %v", email, err)
	}
	return id
}

// publishableFields is a draft with everything [publishable] requires and nothing more.
//
// Nothing more is the point: it is the minimum that publishes, so a field added to the required
// set without a reason breaks this and has to be argued for. Size is deliberately absent — see
// [publishable] on why it is not required.
func publishableFields(category string) DraftFields {
	return DraftFields{
		Pickup:           sydney(),
		Dropoff:          melbourne(),
		GoodsDescription: text("Two-seater sofa, wrapped"),
		GoodsCategory:    text(category),
		PickupWindow:     &TimeWindow{Start: testInstant.AddDate(0, 0, 3)},
	}
}

func newPublishService(t *testing.T) *Service {
	t.Helper()
	return NewService(&recordingSink{}, clock.NewFixed(testInstant), &fakeGeocoder{},
		WithCatalogue(testCatalogue(t)))
}

// publishJob runs a publication in a transaction, which is what Publish requires.
func publishJob(t *testing.T, pool *pgxpool.Pool, svc *Service, customer, job uuid.UUID, accepted bool) (Job, error) {
	t.Helper()

	var published Job
	err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var err error
		published, err = svc.Publish(ctx, r, customer, job, accepted)
		return err
	})
	return published, err
}

// draftFor creates a draft owned by customer with the given fields.
func draftFor(t *testing.T, pool *pgxpool.Pool, svc *Service, customer uuid.UUID, f DraftFields) uuid.UUID {
	t.Helper()

	job, err := svc.CreateDraft(t.Context(), pool, customer, f)
	if err != nil {
		t.Fatalf("creating the draft: %v", err)
	}
	return job.ID
}

// TestPublishingMovesADraftToOpen is SHIP-63's acceptance criterion.
//
// It checks the transition and the two things publication is responsible for starting: the
// declaration Docs/04 §2 requires, and the expiry deadline Docs/02 §6.3 derives as the job becomes
// Open. The deadline is 000406's trigger rather than anything in Go, which is exactly why it is
// asserted here — nothing in this package would fail if the trigger were dropped.
func TestPublishingMovesADraftToOpen(t *testing.T) {
	pool := pgtest.DB(t)
	sink := &recordingSink{}
	svc := NewService(sink, clock.NewFixed(testInstant), &fakeGeocoder{},
		WithCatalogue(testCatalogue(t)))

	customer := verifiedCustomer(t, pool, "publishes@example.com", "+61400000801")
	job := draftFor(t, pool, svc, customer, publishableFields("widgets"))

	published, err := publishJob(t, pool, svc, customer, job, true)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if published.Status != StatusOpen {
		t.Errorf("status = %s, want Open", published.Status)
	}
	if published.TermsAcceptedAt.IsZero() {
		t.Error("terms_accepted_at is unset; Docs/04 §2 requires the declaration per job")
	}
	if published.ExpiresAt.IsZero() {
		t.Error("expires_at is unset; 000406's trigger sets it as the job becomes Open")
	}

	// The transition went through the guard, so there is a history row and an event. Status is
	// never a settable field, and this is what proves the route to Open was not a shortcut.
	history, err := svc.History(t.Context(), pool, job)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("%d history rows, want 1", len(history))
	}
	if history[0].From != StatusDraft || history[0].To != StatusOpen {
		t.Errorf("history records %s to %s, want Draft to Open", history[0].From, history[0].To)
	}
	if history[0].Actor.Type != ActorCustomer || history[0].Actor.ID != customer {
		t.Errorf("history attributes the move to %s/%s, want the customer %s",
			history[0].Actor.Type, history[0].Actor.ID, customer)
	}

	if len(sink.emitted) != 1 {
		t.Fatalf("%d events emitted, want 1", len(sink.emitted))
	}
	if sink.emitted[0].Type != EventStatusChanged {
		t.Errorf("emitted %s, want %s", sink.emitted[0].Type, EventStatusChanged)
	}
}

// TestPublishingProhibitedGoodsIsRefusedAndSaysWhy is SHIP-59's *Done when*, end to end.
//
// SHIP-59's commit names this test: the rule landed before the endpoint that applies it, so this
// is the first point at which "cannot be published" can be shown rather than argued.
func TestPublishingProhibitedGoodsIsRefusedAndSaysWhy(t *testing.T) {
	pool := pgtest.DB(t)
	svc := newPublishService(t)

	customer := verifiedCustomer(t, pool, "anvils@example.com", "+61400000802")
	job := draftFor(t, pool, svc, customer, publishableFields("anvils"))

	_, err := publishJob(t, pool, svc, customer, job, true)
	if !errors.Is(err, ErrProhibitedCategory) {
		t.Fatalf("Publish with a refused category = %v, want ErrProhibitedCategory", err)
	}

	// It stayed a draft. The refusal is not a rollback of a partial publication — nothing was
	// written at all, so there is no history row claiming a transition that did not happen.
	after, err := svc.Job(t.Context(), pool, customer, job)
	if err != nil {
		t.Fatalf("reading the job back: %v", err)
	}
	if after.Status != StatusDraft {
		t.Errorf("status = %s after a refusal, want Draft", after.Status)
	}
	if !after.TermsAcceptedAt.IsZero() {
		t.Error("terms_accepted_at was written on a publication that was refused")
	}
}

// TestTheRefusalOnTheWireNamesTheCategory is the "explains why" half at the wire.
//
// The message is the catalogue's own wording rather than a sentence compiled into the handler,
// which is what makes a category withdrawn on legal advice explain itself the same afternoon.
func TestTheRefusalOnTheWireNamesTheCategory(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool, WithCatalogue(testCatalogue(t)))
	svc := newPublishService(t)

	customer := verifiedCustomer(t, pool, "anvils-wire@example.com", "+61400000803")
	job := draftFor(t, pool, svc, customer, publishableFields("anvils"))

	rec := as(t, router, customer, http.MethodPost,
		"/v1/jobs/"+job.String()+"/publish", `{"accepts_terms":true}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", rec.Code, rec.Body)
	}

	body := decode[errorEnvelope](t, rec)
	if body.Error.Code != string(CodeProhibitedCategory) {
		t.Errorf("code = %q, want %q", body.Error.Code, CodeProhibitedCategory)
	}

	// The catalogue's label and its description, both. A client shows this to the customer,
	// and "we do not carry this" without saying what "this" is helps nobody.
	if !strings.Contains(body.Error.Message, "anvils") {
		t.Errorf("message %q does not name the category", body.Error.Message)
	}
	if !strings.Contains(body.Error.Message, "Far too heavy.") {
		t.Errorf("message %q does not carry the catalogue's description", body.Error.Message)
	}
}

// TestPublishingRequiresBothContactDetailsVerified is Docs/04 §2's first two rows.
//
// Each case says which detail is outstanding, because a customer who has verified their email and
// not their phone is one tap away and telling them "verify your contact details" sends them to
// look at the one that is already done.
func TestPublishingRequiresBothContactDetailsVerified(t *testing.T) {
	cases := []struct {
		name  string
		set   string
		email string
		phone string
	}{
		{"neither", "", "none@example.com", "+61400000810"},
		{"email only", "email_verified_at = now()", "email-only@example.com", "+61400000811"},
		{"phone only", "phone_verified_at = now()", "phone-only@example.com", "+61400000812"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := pgtest.DB(t)
			svc := newPublishService(t)

			customer := newCustomer(t, pool, tc.email, tc.phone)
			if tc.set != "" {
				if _, err := pool.Exec(t.Context(),
					`UPDATE users SET `+tc.set+` WHERE id = $1`, customer); err != nil {
					t.Fatalf("setting %s: %v", tc.set, err)
				}
			}

			job := draftFor(t, pool, svc, customer, publishableFields("widgets"))

			if _, err := publishJob(t, pool, svc, customer, job, true); !errors.Is(err, ErrCustomerNotVerified) {
				t.Fatalf("Publish with %s verified = %v, want ErrCustomerNotVerified", tc.name, err)
			}
		})
	}
}

// TestAnUnverifiedCustomerCanStillDraft is the other half of Docs/04 §2's table.
//
// "Account may create drafts before verification" — so the check belongs at publication and
// nowhere else. Worth its own test because the tempting simplification is to check verification
// once, at the top of the domain, which would break the flow the document describes.
func TestAnUnverifiedCustomerCanStillDraft(t *testing.T) {
	pool := pgtest.DB(t)
	svc := newPublishService(t)

	customer := newCustomer(t, pool, "unverified-draft@example.com", "+61400000813")

	// Creating one. draftFor fails the test if this is refused.
	job := draftFor(t, pool, svc, customer, publishableFields("widgets"))

	// And editing it.
	if _, err := edit(t, pool, svc, customer, job,
		DraftFields{GoodsDescription: text("A different sofa")}); err != nil {
		t.Fatalf("an unverified customer could not edit their own draft: %v", err)
	}

	// Only publishing is refused.
	if _, err := publishJob(t, pool, svc, customer, job, true); !errors.Is(err, ErrCustomerNotVerified) {
		t.Fatalf("Publish = %v, want ErrCustomerNotVerified", err)
	}
}

// TestPublishingRequiresTheDeclaration is Docs/04 §2's third row.
func TestPublishingRequiresTheDeclaration(t *testing.T) {
	pool := pgtest.DB(t)
	svc := newPublishService(t)

	customer := verifiedCustomer(t, pool, "no-terms@example.com", "+61400000814")
	job := draftFor(t, pool, svc, customer, publishableFields("widgets"))

	if _, err := publishJob(t, pool, svc, customer, job, false); !errors.Is(err, ErrTermsNotAccepted) {
		t.Fatalf("Publish without the declaration = %v, want ErrTermsNotAccepted", err)
	}

	// And it is asked every time rather than remembered: the same job, accepted, publishes.
	if _, err := publishJob(t, pool, svc, customer, job, true); err != nil {
		t.Fatalf("Publish with the declaration: %v", err)
	}
}

// TestAnIncompleteJobReportsEveryMissingFieldAtOnce is the "validates required fields" clause.
//
// All of them in one answer, per Docs/10 §4.6: a customer who fixes the address and is then told
// about the pickup window has been made to fill the form in twice.
func TestAnIncompleteJobReportsEveryMissingFieldAtOnce(t *testing.T) {
	pool := pgtest.DB(t)
	svc := newPublishService(t)

	customer := verifiedCustomer(t, pool, "incomplete@example.com", "+61400000815")

	// A draft with nothing on it at all, which POST /v1/jobs permits by design.
	job := draftFor(t, pool, svc, customer, DraftFields{GoodsDescription: text("Something")})

	_, err := publishJob(t, pool, svc, customer, job, true)
	if err == nil {
		t.Fatal("Publish accepted a job with no pickup, no drop-off and no window")
	}

	var apiErr *httpx.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("Publish returned %v, want the validation error contract", err)
	}
	if apiErr.Code != httpx.CodeValidationFailed {
		t.Errorf("code = %q, want %q", apiErr.Code, httpx.CodeValidationFailed)
	}

	reported := map[string]bool{}
	for _, f := range apiErr.Details {
		reported[f.Field] = true
	}

	// All four in one answer, not one request at a time.
	for _, want := range []string{"pickup", "dropoff", "goods_category", "pickup_window.start"} {
		if !reported[want] {
			t.Errorf("%q is missing from the refusal; it reported %v", want, apiErr.Details)
		}
	}

	// goods_description was supplied, so it must not be reported.
	if reported["goods_description"] {
		t.Error("goods_description was reported missing and it was supplied")
	}
}

// TestPublishingSomebodyElsesJobIsANotFound holds the disclosure rule.
//
// Indistinguishable from a job that does not exist, exactly as editing and cancelling are: a
// stranger who could tell the two apart would learn that the job exists.
func TestPublishingSomebodyElsesJobIsANotFound(t *testing.T) {
	pool := pgtest.DB(t)
	svc := newPublishService(t)

	owner := verifiedCustomer(t, pool, "owner-pub@example.com", "+61400000816")
	stranger := verifiedCustomer(t, pool, "stranger-pub@example.com", "+61400000817")
	job := draftFor(t, pool, svc, owner, publishableFields("widgets"))

	if _, err := publishJob(t, pool, svc, stranger, job, true); !errors.Is(err, ErrNotJobOwner) {
		t.Fatalf("a stranger publishing = %v, want ErrNotJobOwner", err)
	}

	// And an id that names nothing is the same 404 on the wire.
	if _, err := publishJob(t, pool, svc, stranger, uuid.Must(uuid.NewV7()), true); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("publishing a job that does not exist = %v, want ErrJobNotFound", err)
	}
}

// TestPublishingAnAlreadyOpenJobIsAbsorbed is the retry a fresh idempotency key produces.
//
// It writes nothing further: no second history row, no second event, and the acceptance keeps the
// instant it already had. [Service.Cancel] makes the same choice, and Docs/02 §3.1 asks for it.
func TestPublishingAnAlreadyOpenJobIsAbsorbed(t *testing.T) {
	pool := pgtest.DB(t)
	sink := &recordingSink{}
	svc := NewService(sink, clock.NewFixed(testInstant), &fakeGeocoder{},
		WithCatalogue(testCatalogue(t)))

	customer := verifiedCustomer(t, pool, "twice@example.com", "+61400000818")
	job := draftFor(t, pool, svc, customer, publishableFields("widgets"))

	first, err := publishJob(t, pool, svc, customer, job, true)
	if err != nil {
		t.Fatalf("the first publication: %v", err)
	}

	second, err := publishJob(t, pool, svc, customer, job, true)
	if err != nil {
		t.Fatalf("the second publication: %v", err)
	}
	if second.Status != StatusOpen {
		t.Errorf("status = %s, want Open", second.Status)
	}
	if !second.TermsAcceptedAt.Equal(first.TermsAcceptedAt) {
		t.Errorf("terms_accepted_at moved from %s to %s on a retry",
			first.TermsAcceptedAt, second.TermsAcceptedAt)
	}

	history, err := svc.History(t.Context(), pool, job)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(history) != 1 {
		t.Errorf("%d history rows after publishing twice, want 1", len(history))
	}
	if len(sink.emitted) != 1 {
		t.Errorf("%d events after publishing twice, want 1", len(sink.emitted))
	}
}

// TestOnlyADraftCanBePublished covers the statuses that are neither Draft nor Open.
func TestOnlyADraftCanBePublished(t *testing.T) {
	pool := pgtest.DB(t)
	svc := newPublishService(t)

	customer := verifiedCustomer(t, pool, "cancelled-pub@example.com", "+61400000819")
	job := draftFor(t, pool, svc, customer, publishableFields("widgets"))

	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := svc.Cancel(ctx, r, customer, job, "changed my mind")
		return err
	}); err != nil {
		t.Fatalf("cancelling first: %v", err)
	}

	if _, err := publishJob(t, pool, svc, customer, job, true); !errors.Is(err, ErrJobNotPublishable) {
		t.Fatalf("publishing a cancelled job = %v, want ErrJobNotPublishable", err)
	}
}
