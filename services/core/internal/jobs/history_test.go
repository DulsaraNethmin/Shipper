package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-65a — a job's status history, served to its parties.
//
// # What is proved where, said plainly so the split is not mistaken for an omission
//
// These tests drive the domain and its handler against a real PostgreSQL, with a [Bidders] that
// answers from a set this file controls. That proves **the rule**: who may read, in what order,
// in what shape, and that a stranger and a missing job are the same bytes.
//
// It does not prove the *query* behind the port, because the port is implemented in cmd/api where
// `bids` may be named. TestTheBidderLookupAnswersForEveryBidStatus in cmd/api/routes_jobs_test.go
// runs that statement against real rows, and scripts/verify/50-jobs.sh drives the wired route end
// to end. Three layers, each proving the thing only it can reach — which is the arrangement
// `delivery`'s `Awards` port already sits in.

// setBidders is a [Bidders] answering from a set of (job, provider) pairs, and recording what it
// was asked.
//
// The recording half is not decoration. "The owning customer reads their own history" would pass
// just as well if the customer's read went through the bid lookup by mistake, and a lookup asked
// about the wrong job would pass every assertion in this file if the fixture had only one job.
// [setBidders.asked] is what makes those two failures visible.
type setBidders struct {
	held  map[string]bool
	asked []string
}

func bidKey(jobID, providerID uuid.UUID) string { return jobID.String() + "/" + providerID.String() }

func (b *setBidders) HasBidOn(_ context.Context, _ db.Runner, jobID, providerID uuid.UUID) (bool, error) {
	b.asked = append(b.asked, bidKey(jobID, providerID))
	return b.held[bidKey(jobID, providerID)], nil
}

// bidderFor is a lookup that says yes to exactly these pairs and no to everything else.
func bidderFor(pairs ...[2]uuid.UUID) *setBidders {
	held := make(map[string]bool, len(pairs))
	for _, pair := range pairs {
		held[bidKey(pair[0], pair[1])] = true
	}
	return &setBidders{held: held}
}

// failingBidders is the lookup reporting that it could not answer.
type failingBidders struct{ err error }

func (b failingBidders) HasBidOn(context.Context, db.Runner, uuid.UUID, uuid.UUID) (bool, error) {
	return false, b.err
}

// historyResponse is the envelope this endpoint answers with, read back as a client would.
//
// Declared here rather than reusing [statusChangeResponse] through [pagination.Page], deliberately:
// a test that unmarshalled into the very struct the handler marshalled from would agree with a
// renamed json tag by construction. This names the wire keys a second time, so a rename fails here.
type historyResponse struct {
	Data []struct {
		ID    string `json:"id"`
		JobID string `json:"job_id"`

		FromStatus string `json:"from_status"`
		ToStatus   string `json:"to_status"`

		ActorType string `json:"actor_type"`
		Reason    string `json:"reason"`

		RecordedAt string `json:"recorded_at"`
		AcceptedAt string `json:"accepted_at"`
	} `json:"data"`
	NextCursor string `json:"next_cursor"`
	HasMore    bool   `json:"has_more"`
}

// placeBid writes a real `bids` row, which the fixtures need whether or not the lookup reads one.
//
// The domain tests here answer through [setBidders], so this exists for the ones that need the row
// to exist as well — and because a fixture that only lives in a Go map cannot notice that `bids`
// has stopped accepting the shape the platform writes.
func placeBid(t *testing.T, pool *pgxpool.Pool, job, provider uuid.UUID, status string) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	// 'Draft' is the one status ck_bids_offer_has_an_amount lets carry no amount.
	amount := "185.00"
	if status == "Draft" {
		amount = ""
	}

	if amount == "" {
		_, err = pool.Exec(t.Context(),
			`INSERT INTO bids (id, job_id, provider_id, status) VALUES ($1, $2, $3, $4)`,
			id, job, provider, status)
	} else {
		_, err = pool.Exec(t.Context(),
			`INSERT INTO bids (id, job_id, provider_id, status, amount, pickup_at, deliver_by)
			 VALUES ($1, $2, $3, $4, $5, now() + interval '2 days', now() + interval '3 days')`,
			id, job, provider, status, amount)
	}
	if err != nil {
		t.Fatalf("placing a %s bid on %s: %v", status, job, err)
	}
	return id
}

// TestTheOwningCustomerReadsTheirJobsHistoryOldestFirst is the first half of SHIP-65a's *Done
// when*: every row, oldest first, with its actor, its reason and both clocks, in the envelope.
//
// The job is walked through four statuses so that "oldest first" is a claim with something to be
// wrong about — a single row is ordered correctly by every ordering there is.
func TestTheOwningCustomerReadsTheirJobsHistoryOldestFirst(t *testing.T) {
	pool := pgtest.DB(t)
	bidders := bidderFor()
	router := newTestRouter(t, pool, WithBidders(bidders))

	customer := newCustomer(t, pool, "history-owner@example.com", "+61400000651")
	job := newDraft(t, pool, customer)

	publish(t, pool, job, customer)
	move(t, pool, job, StatusNegotiating, User(ActorCustomer, customer))
	move(t, pool, job, StatusAwarded, User(ActorCustomer, customer))

	rec := as(t, router, customer, http.MethodGet, "/v1/jobs/"+job.String()+"/history", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	body := decode[historyResponse](t, rec)
	if len(body.Data) != 3 {
		t.Fatalf("the response carries %d entries, want 3 (%s)", len(body.Data), rec.Body)
	}

	// Oldest first. The statuses are named here rather than read out of the fixture, so a
	// reversed ORDER BY fails rather than being agreed with.
	wantOrder := [][2]string{
		{"draft", "open"},
		{"open", "negotiating"},
		{"negotiating", "awarded"},
	}
	for i, want := range wantOrder {
		got := body.Data[i]
		if got.FromStatus != want[0] || got.ToStatus != want[1] {
			t.Errorf("entry %d is %s → %s, want %s → %s",
				i, got.FromStatus, got.ToStatus, want[0], want[1])
		}
	}

	first := body.Data[0]
	switch {
	case first.ID == "":
		t.Error("an entry carries no id")
	case first.JobID != job.String():
		t.Errorf("job_id = %q, want %s", first.JobID, job)
	case first.ActorType != "customer":
		t.Errorf("actor_type = %q, want customer", first.ActorType)
	case first.RecordedAt == "":
		t.Error("recorded_at is absent — the actor's clock is half of Docs/02 §3.1")
	case first.AcceptedAt == "":
		t.Error("accepted_at is absent — the platform's clock is the other half")
	}

	// The envelope of Docs/10 §4.5, and the position this endpoint takes inside it: bounded by
	// the lifecycle, so no cursor and no second page.
	if body.HasMore || body.NextCursor != "" {
		t.Errorf("has_more = %v, next_cursor = %q — this collection does not page",
			body.HasMore, body.NextCursor)
	}

	// The owner's read never reached the bid lookup. A customer whose own history went through
	// it would pass every assertion above and be one wrong answer away from a 404 on their own
	// job.
	if len(bidders.asked) != 0 {
		t.Errorf("the owner's read asked the bid lookup %v — ownership is answered by the job "+
			"row, and asking is a statement that it was not", bidders.asked)
	}
}

// TestTheReasonAndBothClocksSurviveTheWire is the rest of that clause, and it is separate because
// it needs a transition with an actor clock that differs from the platform's.
//
// The forty-minute lag is Docs/02 §3.1's ordinary case — a driver records something with no signal
// and the request arrives later — and it is the only fixture in which "both clocks" can be seen to
// be two.
func TestTheReasonAndBothClocksSurviveTheWire(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool, WithBidders(bidderFor()))

	customer := newCustomer(t, pool, "history-clocks@example.com", "+61400000652")
	job := newDraft(t, pool, customer)
	publish(t, pool, job, customer)

	const reason = "goods collected by another carrier"
	actorAt := testInstant.Add(-40 * time.Minute)

	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := newTestService(&recordingSink{}).Transition(ctx, r, Move{
			JobID:      job,
			To:         StatusCancelled,
			Actor:      User(ActorCustomer, customer),
			Reason:     reason,
			RecordedAt: actorAt,
		})
		return err
	}); err != nil {
		t.Fatalf("cancelling the job: %v", err)
	}

	rec := as(t, router, customer, http.MethodGet, "/v1/jobs/"+job.String()+"/history", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	body := decode[historyResponse](t, rec)
	if len(body.Data) != 2 {
		t.Fatalf("the response carries %d entries, want 2 (%s)", len(body.Data), rec.Body)
	}

	last := body.Data[1]
	if last.Reason != reason {
		t.Errorf("reason = %q, want the words the customer typed (%q)", last.Reason, reason)
	}
	if last.RecordedAt == last.AcceptedAt {
		t.Errorf("recorded_at and accepted_at are both %q — a transition recorded forty minutes "+
			"before it arrived must carry two different instants (Docs/02 §3.1)", last.RecordedAt)
	}
	if want := timestamp(actorAt); last.RecordedAt != want {
		t.Errorf("recorded_at = %q, want the actor's own clock %q", last.RecordedAt, want)
	}

	// The first entry has no reason, and the key is omitted rather than sent empty so a client
	// can tell "gave no reason" from "gave an empty one". Read as an untyped document, because
	// [historyResponse] cannot express the difference — an absent string and an empty one both
	// unmarshal to "".
	var document struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &document); err != nil {
		t.Fatalf("the response is not JSON: %v (%s)", err, rec.Body)
	}
	if _, present := document.Data[0]["reason"]; present {
		t.Errorf("the entry with no reason carries a reason key: %s", rec.Body)
	}
	if _, present := document.Data[1]["reason"]; !present {
		t.Errorf("the entry with a reason does not carry one: %s", rec.Body)
	}
}

// TestAProviderHoldingABidReadsTheHistory is the second reader, and the clause that makes this
// endpoint more than SHIP-65 with an extra field.
//
// Two providers, one of whom bid. The one who did not is the control, and without it "a provider
// reads it" would be satisfied by an endpoint that served every provider on the platform.
func TestAProviderHoldingABidReadsTheHistory(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newCustomer(t, pool, "history-bid-customer@example.com", "+61400000653")
	bidder := newProvider(t, pool, "history-bidder@example.com", "+61400000654")
	onlooker := newProvider(t, pool, "history-onlooker@example.com", "+61400000655")

	job := newDraft(t, pool, customer)
	publish(t, pool, job, customer)
	placeBid(t, pool, job, bidder, "Submitted")

	router := newTestRouter(t, pool, WithBidders(bidderFor([2]uuid.UUID{job, bidder})))

	rec := as(t, router, bidder, http.MethodGet, "/v1/jobs/"+job.String()+"/history", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("the bidding provider got %d, want 200 (%s)", rec.Code, rec.Body)
	}
	if body := decode[historyResponse](t, rec); len(body.Data) != 1 {
		t.Fatalf("the provider sees %d entries, want the 1 the job has", len(body.Data))
	}

	if rec := as(t, router, onlooker, http.MethodGet, "/v1/jobs/"+job.String()+"/history", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("a provider who has not bid got %d, want 404 (%s)", rec.Code, rec.Body)
	}
}

// TestAStrangerAndAMissingJobAreTheSameBytes is the disclosure clause, asserted as the *Done when*
// words it rather than as "both are 404".
//
// **A 403 tells a stranger the job exists**, and so does a 404 with a different message, a
// different code, or a different body length. The only assertion that catches all three is byte
// equality, and it is the only one made here.
//
// Three callers, because the ways of not being a party are not one thing: a customer who owns a
// different job, a provider who has bid on a different job, and an account with nothing at all.
func TestAStrangerAndAMissingJobAreTheSameBytes(t *testing.T) {
	pool := pgtest.DB(t)

	owner := newCustomer(t, pool, "history-owner-2@example.com", "+61400000656")
	stranger := newCustomer(t, pool, "history-stranger@example.com", "+61400000657")
	elsewhere := newProvider(t, pool, "history-elsewhere@example.com", "+61400000658")
	nobody := newProvider(t, pool, "history-nobody@example.com", "+61400000659")

	job := newDraft(t, pool, owner)
	publish(t, pool, job, owner)

	// A different job the provider *has* bid on, so "has bid on something" is not what opens the
	// door — being a party to *this* job is.
	other := newDraft(t, pool, owner)
	placeBid(t, pool, other, elsewhere, "Submitted")

	router := newTestRouter(t, pool, WithBidders(bidderFor([2]uuid.UUID{other, elsewhere})))

	missing := uuid.Must(uuid.NewV7())
	absent := as(t, router, stranger, http.MethodGet, "/v1/jobs/"+missing.String()+"/history", "")
	if absent.Code != http.StatusNotFound {
		t.Fatalf("a job that does not exist answered %d, want 404 (%s)", absent.Code, absent.Body)
	}

	for who, caller := range map[string]uuid.UUID{
		"another customer":                      stranger,
		"a provider who bid on a different job": elsewhere,
		"a provider who has bid on nothing":     nobody,
	} {
		t.Run(who, func(t *testing.T) {
			rec := as(t, router, caller, http.MethodGet, "/v1/jobs/"+job.String()+"/history", "")

			if rec.Code != absent.Code {
				t.Errorf("status = %d, want %d — the same as a job that does not exist",
					rec.Code, absent.Code)
			}
			if got, want := rec.Body.String(), absent.Body.String(); got != want {
				t.Errorf("the refusal is distinguishable from a missing job.\n  got  %s\n  want %s\n"+
					"  A body that differs at all tells a stranger holding an identifier that "+
					"somebody else's job exists.", got, want)
			}
			if got, want := rec.Header().Get("Content-Type"), absent.Header().Get("Content-Type"); got != want {
				t.Errorf("content-type = %q, want %q", got, want)
			}
		})
	}
}

// TestNothingButTheOwnersOwnJobIsReachable is the ordering guard on the two checks.
//
// A reader who is neither party must be refused *before* the history is read, not after it is read
// and filtered — because a refusal computed after the read is one `if` away from being a response.
// [setBidders.asked] is what makes the ordering observable: the lookup is asked exactly once, about
// this job and this reader, and nothing else was.
func TestNothingButTheOwnersOwnJobIsReachable(t *testing.T) {
	pool := pgtest.DB(t)

	owner := newCustomer(t, pool, "history-order-owner@example.com", "+61400000660")
	provider := newProvider(t, pool, "history-order-provider@example.com", "+61400000661")

	job := newDraft(t, pool, owner)
	publish(t, pool, job, owner)

	bidders := bidderFor()
	router := newTestRouter(t, pool, WithBidders(bidders))

	if rec := as(t, router, provider, http.MethodGet, "/v1/jobs/"+job.String()+"/history", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%s)", rec.Code, rec.Body)
	}

	if want := []string{bidKey(job, provider)}; !slicesEqual(bidders.asked, want) {
		t.Errorf("the lookup was asked %v, want exactly %v — once, about this job and this "+
			"reader", bidders.asked, want)
	}
}

// TestAFailingBidderLookupIsNotAnEmptyAnswer is the failure direction of the same seam.
//
// A lookup that cannot answer must not be read as "no bid". The distinction is invisible on the
// wire — both are refusals — so it is asserted in Go, where a 500 and a 404 are different things:
// a 404 would tell a provider their job has vanished because the database blinked.
func TestAFailingBidderLookupIsNotAnEmptyAnswer(t *testing.T) {
	pool := pgtest.DB(t)

	owner := newCustomer(t, pool, "history-fail-owner@example.com", "+61400000662")
	provider := newProvider(t, pool, "history-fail-provider@example.com", "+61400000663")
	job := newDraft(t, pool, owner)

	boom := errors.New("the bid lookup is unavailable")
	svc := NewService(&recordingSink{}, clock.NewFixed(testInstant), nil,
		WithBidders(failingBidders{err: boom}))

	_, err := svc.HistoryFor(t.Context(), pool, provider, job)
	switch {
	case err == nil:
		t.Fatal("a failing bid lookup answered with a history")
	case errors.Is(err, ErrJobNotFound), errors.Is(err, ErrNotJobOwner):
		t.Fatalf("a failing bid lookup was reported as a refusal (%v) — a provider would be "+
			"told their job does not exist because a query failed", err)
	case !errors.Is(err, boom):
		t.Fatalf("err = %v, want the lookup's own failure", err)
	}
}

// TestAServiceWithNoBidderLookupRefusesLoudly is the other half of [WithBidders]'s argument.
//
// The tempting default — no lookup means no bid — would refuse every provider while being
// indistinguishable from a job nobody bid on. This asserts the loud answer instead, which is what
// makes the option safe to be an option.
func TestAServiceWithNoBidderLookupRefusesLoudly(t *testing.T) {
	pool := pgtest.DB(t)

	owner := newCustomer(t, pool, "history-nolookup@example.com", "+61400000664")
	job := newDraft(t, pool, owner)
	provider := newProvider(t, pool, "history-nolookup-p@example.com", "+61400000665")

	svc := NewService(&recordingSink{}, clock.NewFixed(testInstant), nil)

	if _, err := svc.HistoryFor(t.Context(), pool, provider, job); !errors.Is(err, ErrNoBidderLookup) {
		t.Fatalf("err = %v, want ErrNoBidderLookup — a service wired without the lookup must "+
			"say so rather than answer \"nobody has bid\"", err)
	}

	// And the owner is refused too rather than being quietly served, because a deployment that
	// half works is the one nobody notices.
	if _, err := svc.HistoryFor(t.Context(), pool, owner, job); !errors.Is(err, ErrNoBidderLookup) {
		t.Fatalf("the owner's read = %v, want ErrNoBidderLookup", err)
	}
}

// --- the budget, which is the invariant this endpoint sits closest to -----------------------------

// historyProse is the vocabulary a "budget supplied" signal would arrive in if it arrived as words.
//
// **Deliberately not a list of field names**, and this endpoint is why the distinction is not
// academic. `job_status_history.reason` is free text an actor wrote, a provider reads it, and a
// sentence defeats every structural guard at once: it adds no key to a closed set, contains no
// value a search could find, and does not have to say "budget" to say what Docs/01 §4.3 forbids.
// Waves 10 and 11 each isolated *"The customer has set a maximum."* and watched a full suite pass
// with it live, in two different domains. This list is what that finding costs.
//
// Copied in substance from `fleet`'s equivalent rather than shared, because a test file in another
// package is not importable and a shared guard in non-test code would be a production dependency
// on a list of English phrases. The two are allowed to drift; both are checked for vacuity.
//
// **Every entry has to be absent from the fixtures as well as from the platform**, or the guard is
// a guard against the fixture. TestTheHistoryProseGuardIsNotVacuous holds that.
var historyProse = []string{
	"budget",
	"maximum",
	"max ",
	"ceiling",
	"cap ",
	"willing to pay",
	"price range",
	"up to $",
	"has set a",
	"has a limit",
	"limit of",
}

// historyProseIn reports the first phrase of [historyProse] a response carries, or "".
//
// Split from the assertion so the guard can be exercised without a fabricated *testing.T, which is
// the only way TestTheHistoryProseGuardIsNotVacuous can assert that it *fires*.
func historyProseIn(body []byte) string {
	lowered := strings.ToLower(string(body))
	for _, phrase := range historyProse {
		if strings.Contains(lowered, phrase) {
			return phrase
		}
	}
	return ""
}

// historyKeys is every key this endpoint's response may carry, at any depth.
//
// A closed list, in the direction that can express Docs/01 §4.3: a deny-list of names cannot, since
// `max_price` is a budget and does not contain the word. Anything not promised is refused, so a
// field added to this shape has to be added here as well — a decision somebody records rather than
// one that arrives with a struct change.
var historyKeys = map[string]bool{
	"data":        true,
	"next_cursor": true,
	"has_more":    true,

	"id":          true,
	"job_id":      true,
	"from_status": true,
	"to_status":   true,
	"actor_type":  true,
	"reason":      true,
	"recorded_at": true,
	"accepted_at": true,
}

// historyIdentifier matches a UUID as it appears in a JSON document, so a run of digits inside one
// cannot be mistaken for an amount.
var historyIdentifier = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// walkHistoryKeys visits every object key in a decoded JSON document, with the path that reached it.
func walkHistoryKeys(node any, visit func(path, key string)) {
	var walk func(node any, path string)
	walk = func(node any, path string) {
		switch value := node.(type) {
		case map[string]any:
			for key, child := range value {
				visit(path, key)
				walk(child, path+"."+key)
			}
		case []any:
			for i, child := range value {
				walk(child, fmt.Sprintf("%s[%d]", path, i))
			}
		}
	}
	walk(node, "$")
}

// TestTheHistoryResponseCarriesNoBudgetInAnyForm is SHIP-65a's last clause, on the endpoint where
// it is hardest to keep.
//
// Four checks over the bytes a **provider** was served, on a job that has a budget to leak:
//
//  1. every key in the document is one this API promised;
//  2. the word "budget" appears nowhere;
//  3. neither does the stored number, in any rendering an encoder could produce;
//  4. and neither does a *sentence* — the one form the first three all miss.
//
// It refuses to run against a job with no budget. A privacy test whose fixture has nothing to leak
// passes forever and proves nothing, and that is the specific way this could rot unnoticed.
func TestTheHistoryResponseCarriesNoBudgetInAnyForm(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newCustomer(t, pool, "history-budget@example.com", "+61400000666")
	provider := newProvider(t, pool, "history-budget-p@example.com", "+61400000667")

	job := newDraft(t, pool, customer)

	// A number appearing nowhere else in the fixture: not in a postcode, a dimension or a
	// timestamp. 4321.99 renders as 4321.99, 4321 and 432199 in cents, and all are searched for.
	if _, err := pool.Exec(t.Context(),
		`UPDATE jobs SET budget = $2 WHERE id = $1`, job, "4321.99"); err != nil {
		t.Fatalf("setting the budget: %v", err)
	}

	// The fixture, verified rather than assumed.
	var stored *float64
	if err := pool.QueryRow(t.Context(),
		`SELECT budget FROM jobs WHERE id = $1`, job).Scan(&stored); err != nil {
		t.Fatalf("reading the stored budget: %v", err)
	}
	if stored == nil || *stored != 4321.99 {
		t.Fatalf("the job's budget is %v, want 4321.99 — this test asserts nothing against a "+
			"job with no budget to leak", stored)
	}

	publish(t, pool, job, customer)
	placeBid(t, pool, job, provider, "Submitted")

	// A cancellation with a reason, because `reason` is the field the sentence would arrive in
	// and a response with no free text in it is not the response worth checking.
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := newTestService(&recordingSink{}).Transition(ctx, r, Move{
			JobID:  job,
			To:     StatusCancelled,
			Actor:  User(ActorCustomer, customer),
			Reason: "the goods went with another carrier on Tuesday",
		})
		return err
	}); err != nil {
		t.Fatalf("cancelling the job: %v", err)
	}

	router := newTestRouter(t, pool, WithBidders(bidderFor([2]uuid.UUID{job, provider})))

	rec := as(t, router, provider, http.MethodGet, "/v1/jobs/"+job.String()+"/history", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("the provider's read = %d, want 200 (%s)", rec.Code, rec.Body)
	}
	body := rec.Body.Bytes()

	// Not vacuous: the response has to be carrying the history before "it carries no budget"
	// means anything at all.
	if !strings.Contains(string(body), `"another carrier"`) &&
		!strings.Contains(string(body), "another carrier") {
		t.Fatalf("this response carries no reason text, so it proves nothing: %s", body)
	}

	// 1. Every key, at every depth, is one this API promised.
	var document any
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatalf("the response is not JSON: %v (%s)", err, body)
	}
	walkHistoryKeys(document, func(path, key string) {
		if !historyKeys[key] {
			t.Errorf("the history response carries %q (at %s).\n"+
				"  Every key a party may see is in historyKeys, and this is not one of them. "+
				"If it is the customer's budget under another name it is a defect: Docs/01 "+
				"§4.3 forbids it as an amount, a band, or a \"budget supplied\" flag.", key, path)
		}
	})

	// 2 and 3. Not the word, and not the value in any rendering.
	searchable := historyIdentifier.ReplaceAllString(string(body), "<id>")
	for _, rendering := range []string{"4321.99", "432199", "4321,99", "4,321.99", "4321"} {
		if strings.Contains(searchable, rendering) {
			t.Errorf("the customer's budget appears in a provider's response as %q: %s",
				rendering, body)
		}
	}

	// 4. And not as a sentence, which is the check the other three cannot make.
	if phrase := historyProseIn(body); phrase != "" {
		t.Errorf("a provider's status-history response contains %q.\n"+
			"  Docs/01 §4.3 forbids the customer's maximum reaching a provider as an amount, a "+
			"band, or a \"budget supplied\" flag — and a sentence is that flag in the one form no "+
			"key list and no value search can see. If the platform wrote this, it is a defect. "+
			"If the fixture typed it, change the fixture rather than this list.\n  %s",
			phrase, body)
	}
}

// TestTheHistoryProseGuardIsNotVacuous is the check on the check.
//
// [historyProseIn] passes trivially if every phrase it looks for is one no response could contain,
// which is what a later tidy-up of [historyProse] would produce. This asserts that it fires on the
// exact sentence waves 10 and 11 isolated, and that it does not fire on the fixtures' own words —
// the two ways a guard like this stops meaning anything.
func TestTheHistoryProseGuardIsNotVacuous(t *testing.T) {
	const smuggled = `{"data":[{"reason":"the goods went with another carrier on Tuesday. ` +
		`The customer has set a maximum."}],"has_more":false}`

	if historyProseIn([]byte(smuggled)) == "" {
		t.Error("the sentence waves 10 and 11 isolated — \"The customer has set a maximum.\" — " +
			"passed the prose guard. It carries no field, no value and no digit, so the closed " +
			"key set, the word search and the value search all miss it; this guard is the only " +
			"one that can catch it, and it is not catching it.")
	}

	// And the other way: every piece of free text these tests put into a `reason` is clean, so a
	// failure above is about the platform rather than about a fixture.
	for _, text := range []string{
		"the goods went with another carrier on Tuesday",
		"goods collected by another carrier",
		"published from the app",
		"changed my mind",
	} {
		if phrase := historyProseIn([]byte(text)); phrase != "" {
			t.Errorf("the fixture text %q contains %q, so the prose guard is testing the "+
				"fixture rather than the service", text, phrase)
		}
	}
}

// slicesEqual is a local comparison so this file does not reach for a helper another test owns.
func slicesEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
