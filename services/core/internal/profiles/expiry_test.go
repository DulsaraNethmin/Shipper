package profiles

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-159 against a real PostgreSQL, with the clock injected and the lead times driven both ways.
//
// # Every boundary here is measured against an injected instant, never against `time.Now`
//
// The whole feature is a comparison between two dates, so a test that let either side come from the
// wall clock would be a test whose result depends on when it runs. `clock.NewFixed` supplies "now"
// and every fixture's expiry is written relative to it — which is also what makes "expiring" and
// "expired" distinguishable at all, since the two differ by a horizon rather than by a value.
//
// # Both configurations are driven, because the shipping one is the empty one
//
// With no lead time configured, the "expiring" half of Docs/04 §5's queue is empty by construction —
// that is X-4 being unanswered, and it is the state this platform ships in. A suite that only ever
// configured a horizon would prove nothing about it, and a suite that never did would leave the
// clause the ticket is named for untested. So the fixture takes a lead-time map and the tests below
// pass two different ones.

// expiryFixture is a queue reader, a store, and providers with documents on them.
type expiryFixture struct {
	pool     *pgxpool.Pool
	clk      *clock.Fixed
	docs     *Documents
	store    *fakeObjects
	sequence int
}

// expiryInstant is "now" for this suite.
//
// Deliberately not [testInstant]: the documents fixture backdates nothing and this suite needs
// dates on both sides of the boundary, so an instant with room in front of it and behind it makes
// every fixture readable as a date rather than as an offset.
var expiryInstant = time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)

func newExpiryFixture(t *testing.T) *expiryFixture {
	t.Helper()

	pool := pgtest.DB(t)
	store := newFakeObjects()

	return &expiryFixture{
		pool:  pool,
		clk:   clock.NewFixed(expiryInstant),
		docs:  NewDocuments(clock.NewFixed(expiryInstant), store, store, testDocumentPolicy()),
		store: store,
	}
}

// queue builds a reader over the given horizons.
func (f *expiryFixture) queue(lead map[Kind]time.Duration) *Expiry {
	return NewExpiry(f.clk, lead)
}

// provider registers a provider account, which `000200`'s trigger gives a Pending record.
func (f *expiryFixture) provider(t *testing.T, name string) uuid.UUID {
	t.Helper()

	f.sequence++
	return newProvider(t, f.pool, name+"-expiry@example.com",
		fmt.Sprintf("04159000%02d", f.sequence))
}

// submit records one document, optionally stating when it lapses.
func (f *expiryFixture) submit(t *testing.T, provider uuid.UUID, kind Kind, expires *time.Time) Document {
	t.Helper()

	key := upload(t, f.docs, f.pool, provider, 4096)
	f.store.put(key, 4096)

	document, err := f.docs.Submit(t.Context(), f.pool, provider, kind, key, expires)
	if err != nil {
		t.Fatalf("submitting a %s: %v", kind, err)
	}
	return document
}

// due reads the whole queue, which is small in every fixture below.
func (f *expiryFixture) due(t *testing.T, q *Expiry) []ExpiringDocument {
	t.Helper()

	found, err := q.Due(t.Context(), f.pool, ExpiryQuery{Limit: 100})
	if err != nil {
		t.Fatalf("reading the expiry queue: %v", err)
	}
	return found
}

// carries reports whether the queue holds an entry for that document.
func carries(entries []ExpiringDocument, document uuid.UUID) (ExpiringDocument, bool) {
	for _, e := range entries {
		if e.DocumentID == document {
			return e, true
		}
	}
	return ExpiringDocument{}, false
}

// at returns the instant that many days from the fixture's now.
func days(n int) *time.Time {
	at := expiryInstant.AddDate(0, 0, n)
	return &at
}

// TestExpiredDocumentsSurfaceWithNoConfigurationAtAll.
//
// **This is the half of SHIP-159's *Done when* that needs no answer from X-4.** A document states
// when it lapses; when that date has passed it is expired, and the platform has invented nothing to
// say so. With an empty lead-time map every kind's horizon is *now*, which is the shipping default
// and is what this asserts.
//
// The negative half matters as much: a document that lapses tomorrow must **not** be on the queue,
// because "tomorrow is soon" is exactly the judgement Docs/04 §3 gives to legal and insurance
// advisers and nobody has made.
func TestExpiredDocumentsSurfaceWithNoConfigurationAtAll(t *testing.T) {
	f := newExpiryFixture(t)
	provider := f.provider(t, "lapsed")

	lapsed := f.submit(t, provider, KindInsurance, days(-30))
	lapsesTomorrow := f.submit(t, provider, KindRegistration, days(1))
	silent := f.submit(t, provider, KindABNEvidence, nil)

	entries := f.due(t, f.queue(nil))

	entry, found := carries(entries, lapsed.ID)
	if !found {
		t.Fatal("a document that lapsed a month ago is on no queue. Docs/04 §5's seventh queue " +
			"is expiring **or expired** records, and this half needs no configuration at all")
	}
	if !entry.Expired {
		t.Error("the entry does not read as expired, so a console cannot tell the two halves apart")
	}
	if entry.VerificationState != StatePending {
		t.Errorf("verification_state = %q, want the provider's standing", entry.VerificationState)
	}
	if entry.Kind != KindInsurance {
		t.Errorf("kind = %q, want insurance", entry.Kind)
	}

	if _, found := carries(entries, lapsesTomorrow.ID); found {
		t.Error("a document that lapses tomorrow is on the queue with no lead time configured. " +
			"How far ahead is worth chasing is Docs/04 §3's open question (Track-X row X-4), " +
			"and answering it here is the platform enforcing a number nobody decided")
	}
	if _, found := carries(entries, silent.ID); found {
		t.Error("a document that states no expiry is on the queue. NULL means the platform was " +
			"never told, which is not the same as a date that has passed")
	}
}

// TestAConfiguredLeadTimeIsWhatMakesADocumentExpiring.
//
// The other half of the *Done when* — "**expiring** … surface **ahead of time**" — and the clause
// that cannot be met without a number somebody chose. Configuring one horizon and not another is
// what shows the lead time is doing the work: the same fixture, read twice, differs only in the map.
func TestAConfiguredLeadTimeIsWhatMakesADocumentExpiring(t *testing.T) {
	f := newExpiryFixture(t)
	provider := f.provider(t, "ahead")

	inThirtyDays := f.submit(t, provider, KindInsurance, days(30))
	inNinetyDays := f.submit(t, provider, KindLicence, days(90))

	// Neither is due yet, because nothing has been configured.
	if entries := f.due(t, f.queue(nil)); len(entries) != 0 {
		t.Fatalf("%d documents are due with no horizon configured, want 0", len(entries))
	}

	// Sixty days' notice on insurance certificates, and nothing said about licences.
	entries := f.due(t, f.queue(map[Kind]time.Duration{KindInsurance: 60 * 24 * time.Hour}))

	entry, found := carries(entries, inThirtyDays.ID)
	if !found {
		t.Fatal("an insurance certificate lapsing in thirty days is not on a queue configured " +
			"with sixty days' notice, so the lead time is not reaching the query")
	}
	if entry.Expired {
		t.Error("the entry reads as expired. It has not lapsed — it is *expiring*, and a console " +
			"that cannot tell them apart shows a provider as unable to trade when they are not")
	}
	if !entry.ExpiresAt.Equal(*days(30)) {
		t.Errorf("expires_at = %s, want the date the document states", entry.ExpiresAt)
	}

	if _, found := carries(entries, inNinetyDays.ID); found {
		t.Error("a licence lapsing in ninety days is on a queue that was given no lead time for " +
			"licences. A kind with no configured horizon surfaces when it has actually lapsed")
	}
}

// TestOnlyTheCurrentDocumentOfAKindIsOnTheQueue.
//
// **The non-obvious one, and it is a property of the query rather than of the data.** `000201` is
// append-only, so re-photographing a licence leaves the old row behind with its old date and nothing
// can ever delete it. A queue that read every row would ask an administrator to chase a renewal that
// has already happened — every time it was opened, for ever, growing by one entry per retake.
func TestOnlyTheCurrentDocumentOfAKindIsOnTheQueue(t *testing.T) {
	f := newExpiryFixture(t)
	provider := f.provider(t, "retaken")

	superseded := f.submit(t, provider, KindInsurance, days(-10))
	current := f.submit(t, provider, KindInsurance, days(365))

	entries := f.due(t, f.queue(nil))

	if _, found := carries(entries, superseded.ID); found {
		t.Error("the superseded insurance certificate is on the queue. It is the image an " +
			"administrator already reviewed, it cannot be deleted, and chasing it is chasing a " +
			"renewal that has happened")
	}
	if _, found := carries(entries, current.ID); found {
		t.Error("the current certificate is on the queue and does not lapse for a year")
	}

	// And the reduction is per kind rather than per provider: a lapsed licence beside a current
	// insurance certificate must still surface.
	licence := f.submit(t, provider, KindLicence, days(-1))
	if _, found := carries(f.due(t, f.queue(nil)), licence.ID); !found {
		t.Error("a lapsed licence is hidden by a current insurance certificate, so the queue is " +
			"reducing per provider rather than per kind")
	}
}

// TestTheQueueIsSoonestFirstAndPagesTotally.
//
// Soonest first, because the entry at the top has been out of date longest — the same reasoning
// [Service.AwaitingReview] puts the longest-waiting provider first. The cursor carries the document
// identifier as well as the date, and that is not decoration: an expiry is *printed on a document*
// rather than generated, so two lapsing on the same day is the ordinary case and a single-column
// cursor would skip a row or repeat one.
func TestTheQueueIsSoonestFirstAndPagesTotally(t *testing.T) {
	f := newExpiryFixture(t)
	provider := f.provider(t, "ordered")
	other := f.provider(t, "ordered-two")

	// Two documents sharing one date, so the tie-break is exercised rather than avoided, and one
	// older than both.
	sameDay := days(-5)
	oldest := f.submit(t, provider, KindLicence, days(-40))
	tiedA := f.submit(t, provider, KindInsurance, sameDay)
	tiedB := f.submit(t, other, KindInsurance, sameDay)

	q := f.queue(nil)
	all := f.due(t, q)
	if len(all) != 3 {
		t.Fatalf("the queue holds %d documents, want 3", len(all))
	}
	if all[0].DocumentID != oldest.ID {
		t.Errorf("the first entry is not the one that has been out of date longest")
	}

	tied := map[uuid.UUID]bool{tiedA.ID: true, tiedB.ID: true}
	if !tied[all[1].DocumentID] || !tied[all[2].DocumentID] {
		t.Errorf("the two documents sharing a date are not the last two entries")
	}

	// Paged one at a time, the three come back once each and in the same order. A cursor that
	// was not total would repeat one and drop another, which is the failure that hides a
	// provider whose paperwork has lapsed.
	var seen []uuid.UUID
	cursor := ExpiryCursor{}
	for range all {
		page, err := q.Due(t.Context(), f.pool, ExpiryQuery{Limit: 1, After: cursor})
		if err != nil {
			t.Fatalf("reading a page: %v", err)
		}
		if len(page) != 1 {
			t.Fatalf("a page of one returned %d entries", len(page))
		}
		seen = append(seen, page[0].DocumentID)
		cursor = ExpiryCursor{ExpiresAt: page[0].ExpiresAt, DocumentID: page[0].DocumentID}
	}

	if len(seen) != len(all) {
		t.Fatalf("paging returned %d entries and the whole queue holds %d", len(seen), len(all))
	}
	for i := range seen {
		if seen[i] != all[i].DocumentID {
			t.Errorf("paged entry %d is %s and the whole queue has %s there — the cursor is not "+
				"total, so a row is being skipped or repeated", i, seen[i], all[i].DocumentID)
		}
	}
}

// TestAConfiguredLeadTimeForAKindThePlatformDoesNotHaveIsRefusedAtStartup.
//
// A typo in `VERIFICATION_EXPIRY_LEAD_TIMES` would otherwise be configuration that silently does
// nothing: set in staging, restarted, queue unchanged, and the obvious conclusion is that the
// feature is broken rather than that the name was wrong. It panics because this is called once from
// the composition root and a misconfiguration present at startup will still be there after a
// restart.
//
// A negative horizon is refused for a sharper reason: it is not a smaller policy, it is a horizon in
// the past, and it would hide documents that have already lapsed. A queue quietly under-reporting
// looks exactly like a supply side whose paperwork is all current.
func TestAConfiguredLeadTimeForAKindThePlatformDoesNotHaveIsRefusedAtStartup(t *testing.T) {
	for _, tc := range []struct {
		name string
		lead map[Kind]time.Duration
	}{
		{"a kind Docs/04 §3 does not have", map[Kind]time.Duration{Kind("passport"): time.Hour}},
		{"a horizon in the past", map[Kind]time.Duration{KindInsurance: -time.Hour}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("NewExpiry accepted it, so the misconfiguration would reach " +
						"production as a queue that quietly reports the wrong thing")
				}
			}()
			NewExpiry(clock.NewFixed(expiryInstant), tc.lead)
		})
	}
}

// TestTheConfiguredHorizonsCannotBeWidenedByACaller.
//
// [Expiry.LeadTimes] hands back a copy, for [Role.Permissions]' reason one package over: a caller
// that wrote into what it was handed would change the platform's horizons for the whole process —
// and the console reads this map to explain the page it is showing, so the drift would be invisible.
func TestTheConfiguredHorizonsCannotBeWidenedByACaller(t *testing.T) {
	q := NewExpiry(clock.NewFixed(expiryInstant),
		map[Kind]time.Duration{KindInsurance: 24 * time.Hour})

	held := q.LeadTimes()
	held[KindLicence] = 365 * 24 * time.Hour
	held[KindInsurance] = 0

	again := q.LeadTimes()
	if _, widened := again[KindLicence]; widened {
		t.Error("a caller added a horizon to the platform's configuration")
	}
	if again[KindInsurance] != 24*time.Hour {
		t.Errorf("the insurance horizon is now %s; a caller changed it", again[KindInsurance])
	}
}
