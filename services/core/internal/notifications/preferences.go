package notifications

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// Notification preferences (SHIP-142): which categories an account has switched off.
//
// 000704 carries the schema argument — why presence is the mute, why there is no `muted boolean`,
// and why the CHECK constraint rather than this file is what makes "essential events cannot be
// muted" true. This file is the three operations over it and the one filter that reads them.
//
// # The category is the unit, and four is the number
//
// [Categories] has four entries drawn from Docs/01 §4.5's own grouping, rather than one switch per
// event type. Thirteen switches is a preference screen nobody reads, and a screen nobody reads is a
// screen where the one switch that mattered was never found.
//
// # Only one of the four can be muted, and that is a product decision rather than an oversight
//
// [Category.Essential] says which, and it has said so since SHIP-137 — the constant carries the
// argument. Docs/01 §4.5's list of essential events covers bidding, awards and delivery; the job
// expiry warning and the extension are reminders about a decision the customer can already see in
// the app, and neither is on it.
//
// So today this ticket buys exactly one switch. That is the honest size of it, and it is the right
// size: the alternative reading — that a preference screen should be able to switch off the message
// telling somebody their job has been awarded — is one Docs/01 §4.5 forecloses.

// Preference is one category as a preference screen shows it.
//
// All three fields are served, including [Preference.Essential], because **the client must not hold
// the list of what may be muted**. CLAUDE.md puts anything that changes under operational pressure
// server-side and Flutter has no over-the-air path for Dart code, so a build that hard-coded "only
// job expiry is mutable" would be wrong the day a second category became mutable and could not be
// corrected. The client renders a switch per row and disables the ones marked essential.
//
// It is also not an authorisation decision on the device (Docs/07 §3). The app disables the switch;
// [Service.SetMuted] and ck_notification_preferences_category are what refuse it.
type Preference struct {
	Category  Category
	Essential bool
	Muted     bool
}

// Preferences is every category and whether this account has muted it.
//
// The complete list rather than only the muted ones, because the caller is rendering a screen: a
// response carrying `["job_expiry"]` would leave the client to know what the other three are, which
// is the knowledge this method exists to keep on the platform.
//
// In [Categories] order, which is 000700's CHECK order, so the screen does not reshuffle between
// requests.
func (s *Service) Preferences(ctx context.Context, r db.Runner, userID uuid.UUID) ([]Preference, error) {
	muted, err := s.store.mutedOf(ctx, r, userID)
	if err != nil {
		return nil, err
	}

	out := make([]Preference, 0, len(Categories))
	for _, category := range Categories {
		out = append(out, Preference{
			Category:  category,
			Essential: category.Essential(),
			Muted:     muted[category],
		})
	}
	return out, nil
}

// SetMuted replaces this account's muted set with exactly the categories given.
//
// # A replacement rather than a toggle, and that is what makes it idempotent
//
// A preference screen sends the state it is in. Sending the whole set means the same request twice
// leaves the same rows, a retry after a dropped connection is free, and there is no ordering between
// two switches flipped in one session — which a pair of add and remove calls would have.
//
// It also means the empty list is meaningful and has to be accepted: it is "unmute everything".
//
// # Essential categories are refused here and refused again by the database
//
// [ErrEssentialCategory] for a category Docs/01 §4.5 puts on its list. The check is here so the
// client is told which value was wrong; ck_notification_preferences_category is what makes it true
// of a connection that did not come through this service. 000704 has the argument for why both.
//
// # It must run in a transaction
//
// Clearing what was muted and writing what now is are one change. Committed apart, a crash between
// them leaves an account subscribed to everything with no record that it asked not to be — and the
// screen it would next load would agree, so nobody would ever find out. Checked rather than
// documented, for the reason [Consume] and [Service.RegisterDevice] check.
func (s *Service) SetMuted(
	ctx context.Context, r db.Runner, userID uuid.UUID, muted []Category,
) ([]Preference, error) {
	if _, inTx := r.(pgx.Tx); !inTx {
		return nil, fmt.Errorf("notifications: setting preferences: %w", ErrNotInTransaction)
	}

	// Deduplicated on the way in, because a client sending the same category twice is asking for
	// one thing and the insert below would otherwise conflict with itself.
	wanted := make([]Category, 0, len(muted))
	seen := make(map[Category]bool, len(muted))
	for _, category := range muted {
		if !category.Valid() {
			return nil, fmt.Errorf("%w: %q", ErrUnknownCategory, category)
		}
		if category.Essential() {
			return nil, fmt.Errorf("%w: %q", ErrEssentialCategory, category)
		}
		if seen[category] {
			continue
		}
		seen[category] = true
		wanted = append(wanted, category)
	}

	if err := s.store.replaceMuted(ctx, r, userID, wanted, s.clock.Now().UTC()); err != nil {
		return nil, err
	}
	return s.Preferences(ctx, r, userID)
}

// filterMuted drops the recipients who have switched this category off.
//
// # It is only ever called for a category that may be muted, and that is structural rather than tidy
//
// [Consume] calls this when `rule.Category.Essential()` is false and not otherwise, so an essential
// category is never filtered because **no lookup is made** — there is no branch in which a muted row
// for an award could be read and honoured. That is a second reading of the same guarantee
// ck_notification_preferences_category gives, from the other end: the database cannot hold the row
// and the consumer would not read it.
//
// It is also two fewer round trips inside a transaction holding a Kafka partition's progress, for
// twelve of the thirteen events in the catalogue.
//
// # A muted recipient is dropped on every channel, not only on push
//
// The unit of the preference is the notification rather than the handset. Somebody who has switched
// off job-expiry reminders has not asked to receive them by email instead, and a mute that silenced
// the push and sent the email would read as the platform ignoring them.
func (s *Service) filterMuted(
	ctx context.Context, r db.Runner, category Category, recipients []Recipient,
) ([]Recipient, error) {
	if len(recipients) == 0 {
		return recipients, nil
	}

	ids := make([]uuid.UUID, 0, len(recipients))
	for _, recipient := range recipients {
		ids = append(ids, recipient.UserID)
	}

	muted, err := s.store.mutedBy(ctx, r, ids, category)
	if err != nil {
		return nil, err
	}
	if len(muted) == 0 {
		return recipients, nil
	}

	kept := recipients[:0]
	for _, recipient := range recipients {
		if !muted[recipient.UserID] {
			kept = append(kept, recipient)
		}
	}
	return kept, nil
}
