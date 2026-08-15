package notifications

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-142's two clauses, against a real PostgreSQL.
//
// The second clause — "essential events cannot be muted" — is held in three places and each is
// tested where it lives: ck_notification_preferences_category in migrations/notifications_test.go,
// [Service.SetMuted] here, and the handler's per-index field error in http.go. This file is the
// middle one plus the thing all three exist for, which is that a mute actually stops a message.

// preferenceService is a service with nothing wired but a clock and a parties stub, which is all a
// preference test needs.
func preferenceService(parties Parties) *Service {
	return NewService(parties, clock.NewFixed(testInstant), Senders{Email: &recordingSender{}})
}

// setMuted runs SetMuted in a transaction, which is what it requires.
func setMuted(
	t *testing.T, pool *pgxpool.Pool, s *Service, userID uuid.UUID, muted ...Category,
) ([]Preference, error) {
	t.Helper()

	var out []Preference
	err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var txErr error
		out, txErr = s.SetMuted(ctx, r, userID, muted)
		return txErr
	})
	return out, err
}

// mutedIn is the muted set as the screen would show it.
func mutedIn(preferences []Preference) map[Category]bool {
	muted := map[Category]bool{}
	for _, p := range preferences {
		if p.Muted {
			muted[p.Category] = true
		}
	}
	return muted
}

// TestThePreferenceScreenIsEveryCategoryWhetherMutedOrNot.
//
// The client renders a switch per row and does not hold the category list, so the answer has to be
// complete rather than a list of what is off. It also has to carry `essential`, because that is the
// only way a build shipped today learns that a second category became mutable.
func TestThePreferenceScreenIsEveryCategoryWhetherMutedOrNot(t *testing.T) {
	pool := pgtest.DB(t)
	svc := preferenceService(&stubParties{})
	user := newUser(t, pool, "prefs-screen@example.com", "0418100001", "customer")

	preferences, err := svc.Preferences(t.Context(), pool, user)
	if err != nil {
		t.Fatalf("reading preferences: %v", err)
	}

	if len(preferences) != len(Categories) {
		t.Fatalf("the screen has %d rows and there are %d categories", len(preferences), len(Categories))
	}
	for i, category := range Categories {
		switch {
		case preferences[i].Category != category:
			t.Errorf("row %d is %q, want %q — the order is 000700's and a screen that "+
				"reshuffles between requests is one nobody trusts",
				i, preferences[i].Category, category)
		case preferences[i].Essential != category.Essential():
			t.Errorf("%s is served as essential=%v and Category.Essential() says %v",
				category, preferences[i].Essential, category.Essential())
		case preferences[i].Muted:
			t.Errorf("%s is muted for an account that has never set a preference; absence "+
				"is the default and the default is that everything is sent", category)
		}
	}
}

// TestAMutableCategoryCanBeMutedAndUnmuted is the first clause of the *Done when*.
//
// The unmute matters as much as the mute: `[]` is "send me everything" and has to be accepted, which
// an implementation treating an empty list as "change nothing" would get wrong — and would get
// wrong silently, because the screen would keep showing what it sent.
func TestAMutableCategoryCanBeMutedAndUnmuted(t *testing.T) {
	pool := pgtest.DB(t)
	svc := preferenceService(&stubParties{})
	user := newUser(t, pool, "prefs-toggle@example.com", "0418100002", "customer")

	after, err := setMuted(t, pool, svc, user, CategoryJobExpiry)
	if err != nil {
		t.Fatalf("muting job_expiry: %v", err)
	}
	if muted := mutedIn(after); !muted[CategoryJobExpiry] || len(muted) != 1 {
		t.Fatalf("after muting job_expiry the muted set is %v", muted)
	}

	after, err = setMuted(t, pool, svc, user)
	if err != nil {
		t.Fatalf("unmuting everything: %v", err)
	}
	if muted := mutedIn(after); len(muted) != 0 {
		t.Errorf("an empty request left %v muted; `[]` is unmute everything, not change nothing",
			muted)
	}
}

// TestSettingTheSamePreferenceTwiceIsOneRow.
//
// A preference screen saves on every visit, so this is the ordinary case rather than the retry. The
// row survives rather than being deleted and rewritten, which is what keeps `muted_at` an answer to
// "when did they turn this off" instead of "when did they last open settings".
func TestSettingTheSamePreferenceTwiceIsOneRow(t *testing.T) {
	pool := pgtest.DB(t)
	svc := preferenceService(&stubParties{})
	user := newUser(t, pool, "prefs-twice@example.com", "0418100003", "customer")

	for range 2 {
		if _, err := setMuted(t, pool, svc, user, CategoryJobExpiry, CategoryJobExpiry); err != nil {
			t.Fatalf("muting job_expiry: %v", err)
		}
	}

	var rows int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM notification_preferences WHERE user_id = $1`, user).
		Scan(&rows); err != nil {
		t.Fatalf("counting preferences: %v", err)
	}
	if rows != 1 {
		t.Errorf("%d preference rows after muting the same category twice in two requests", rows)
	}
}

// TestAnEssentialCategoryIsRefused is the second clause of the *Done when*, in the service.
//
// Every essential category, not one of them: a check written against `bidding` alone would pass with
// `award` mutable, and the failure is invisible until somebody switches off the message telling them
// their job was taken.
func TestAnEssentialCategoryIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	svc := preferenceService(&stubParties{})
	user := newUser(t, pool, "prefs-essential@example.com", "0418100004", "customer")

	for _, category := range Categories {
		if !category.Essential() {
			continue
		}
		if _, err := setMuted(t, pool, svc, user, category); !errors.Is(err, ErrEssentialCategory) {
			t.Errorf("muting %s returned %v, want ErrEssentialCategory", category, err)
		}
	}

	if _, err := setMuted(t, pool, svc, user, Category("nonsense")); !errors.Is(err, ErrUnknownCategory) {
		t.Errorf("muting a category that does not exist returned %v, want ErrUnknownCategory", err)
	}
}

// TestARefusedPreferenceWritesNothingAtAll.
//
// A request naming one mutable category and one essential one must leave the account exactly as it
// was. The refusal happens before any statement runs, which is what makes this true — and it is
// worth a test rather than a comment, because the obvious implementation validates as it inserts and
// leaves the valid half behind.
func TestARefusedPreferenceWritesNothingAtAll(t *testing.T) {
	pool := pgtest.DB(t)
	svc := preferenceService(&stubParties{})
	user := newUser(t, pool, "prefs-partial@example.com", "0418100005", "customer")

	if _, err := setMuted(t, pool, svc, user, CategoryJobExpiry, CategoryAward); err == nil {
		t.Fatal("a request carrying an essential category was accepted")
	}

	preferences, err := svc.Preferences(t.Context(), pool, user)
	if err != nil {
		t.Fatalf("reading preferences: %v", err)
	}
	if muted := mutedIn(preferences); len(muted) != 0 {
		t.Errorf("a refused request left %v muted; nothing in it should have been applied", muted)
	}
}

// TestSettingAPreferenceOutsideATransactionIsRefused.
//
// Clearing what was muted and writing what now is are one change, and committed apart a crash
// between them leaves an account subscribed to everything with no record that it asked not to be.
// Checked rather than documented, for the reason Consume and RegisterDevice check.
func TestSettingAPreferenceOutsideATransactionIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	svc := preferenceService(&stubParties{})
	user := newUser(t, pool, "prefs-notx@example.com", "0418100006", "customer")

	_, err := svc.SetMuted(t.Context(), pool, user, []Category{CategoryJobExpiry})
	if !errors.Is(err, ErrNotInTransaction) {
		t.Errorf("SetMuted on a pool returned %v, want ErrNotInTransaction", err)
	}
}

// --- what the preference is for -----------------------------------------------------------------

// TestAMutedCategoryWritesNoNotification is the whole point, end to end through the consumer.
//
// job.expiry_warned is the one routed event in the mutable category. Muting it has to stop **every**
// channel, not only push: somebody who switched off expiry reminders has not asked to receive them
// by email instead.
func TestAMutedCategoryWritesNoNotification(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newUser(t, pool, "muted-customer@example.com", "0418100007", "customer")
	svc := preferenceService(&stubParties{customer: customer})

	job := uuid.Must(uuid.NewV7())

	before := envelopeOf(t, "job.expiry_warned", map[string]any{
		"schema_version": 1, "job_id": job.String(), "customer_id": customer.String(),
	})
	written, err := consume(t, pool, svc, before)
	if err != nil {
		t.Fatalf("consuming before the mute: %v", err)
	}
	if written == 0 {
		t.Fatal("the event wrote nothing before the mute, so this proves nothing about the mute")
	}

	if _, err := setMuted(t, pool, svc, customer, CategoryJobExpiry); err != nil {
		t.Fatalf("muting job_expiry: %v", err)
	}

	after := envelopeOf(t, "job.expiry_warned", map[string]any{
		"schema_version": 1, "job_id": job.String(), "customer_id": customer.String(),
	})
	written, err = consume(t, pool, svc, after)
	if err != nil {
		t.Fatalf("consuming after the mute: %v", err)
	}
	if written != 0 {
		t.Errorf("a muted category wrote %d notification(s); the rows are %v",
			written, rowsFor(t, pool, after.ID))
	}
}

// TestAMuteDoesNotSilenceAnEssentialEvent is the invariant from the consumer's side.
//
// It is not a restatement of the refusal above. The row cannot be written today, so this puts one
// there **behind the constraint's back** — by muting the mutable category and then consuming an
// event in an essential one — and checks the consumer never even looks. That is the guarantee
// `Consume` gets from calling the filter only when `Essential()` is false: there is no branch in
// which a muted row for an award could be read and honoured, whatever the table holds.
func TestAMuteDoesNotSilenceAnEssentialEvent(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newUser(t, pool, "muted-essential@example.com", "0418100008", "customer")
	provider := newUser(t, pool, "muted-provider@example.com", "0418100009", "provider")
	svc := preferenceService(&stubParties{customer: customer, provider: provider})

	if _, err := setMuted(t, pool, svc, customer, CategoryJobExpiry); err != nil {
		t.Fatalf("muting job_expiry: %v", err)
	}

	// bid.accepted is CategoryAward, which Docs/01 §4.5 puts on its essential list.
	env := envelopeOf(t, "bid.accepted", map[string]any{
		"schema_version": 1,
		"job_id":         uuid.Must(uuid.NewV7()).String(),
		"provider_id":    provider.String(),
	})
	if _, err := consume(t, pool, svc, env); err != nil {
		t.Fatalf("consuming an award: %v", err)
	}

	told := map[uuid.UUID]bool{}
	for _, row := range rowsFor(t, pool, env.ID) {
		told[row.Recipient] = true
	}
	if !told[customer] {
		t.Error("the customer was not told their job had been awarded, and they had only " +
			"muted job expiry — an essential category must be unmutable in effect as well " +
			"as in the constraint")
	}
}

// TestAnUnmutedAccountIsUnaffectedByAnotherAccountsMute.
//
// The filter reads a batch of recipients at once, and the shape of that query is where "who muted
// this" turns into "somebody muted this". A two-recipient event with one mute is the smallest case
// that tells the two apart, and it is exactly the fixture wave 10 warned about — one that sorts
// identically under a right and a wrong implementation would prove nothing.
func TestAnUnmutedAccountIsUnaffectedByAnotherAccountsMute(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newUser(t, pool, "unmuted-customer@example.com", "0418100010", "customer")
	other := newUser(t, pool, "muted-other@example.com", "0418100011", "customer")
	svc := preferenceService(&stubParties{customer: customer})

	if _, err := setMuted(t, pool, svc, other, CategoryJobExpiry); err != nil {
		t.Fatalf("muting job_expiry for the other account: %v", err)
	}

	env := envelopeOf(t, "job.expiry_warned", map[string]any{
		"schema_version": 1,
		"job_id":         uuid.Must(uuid.NewV7()).String(),
		"customer_id":    customer.String(),
	})
	written, err := consume(t, pool, svc, env)
	if err != nil {
		t.Fatalf("consuming: %v", err)
	}
	if written == 0 {
		t.Error("an account with no preference of its own was silenced by somebody else's mute")
	}
}

// TestAPreferenceIsScopedToOneAccount is the read half of the same worry, without the consumer.
func TestAPreferenceIsScopedToOneAccount(t *testing.T) {
	pool := pgtest.DB(t)
	svc := preferenceService(&stubParties{})
	mine := newUser(t, pool, "prefs-mine@example.com", "0418100012", "customer")
	theirs := newUser(t, pool, "prefs-theirs@example.com", "0418100013", "customer")

	if _, err := setMuted(t, pool, svc, theirs, CategoryJobExpiry); err != nil {
		t.Fatalf("muting for the other account: %v", err)
	}

	preferences, err := svc.Preferences(t.Context(), pool, mine)
	if err != nil {
		t.Fatalf("reading preferences: %v", err)
	}
	if muted := mutedIn(preferences); len(muted) != 0 {
		t.Errorf("this account's screen shows %v muted, and the mute belongs to another account",
			muted)
	}
}
