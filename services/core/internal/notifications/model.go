package notifications

import (
	"time"

	"github.com/google/uuid"
)

// The vocabulary this domain owns (SHIP-137).
//
// Two enumerations — the channel and the category — and both are paired against their CHECK
// constraint in 000700 by a test in migrations/, in both directions, per Docs/10 §3.4. A value the
// database accepts and Go has no rule for is a notification nothing can send; a value Go knows and
// the database refuses is one nothing can write.
//
// The status is deliberately *not* a Go enumeration with a list. It has three values, they are
// written in exactly three places in postgres.go, and a fourth copy of them here would be a list to
// keep in step for no reader's benefit.

// Channel is how a notification reaches a person.
type Channel string

const (
	// ChannelEmail is the one channel with a working sender in this ticket.
	//
	// Docs/01 §4.5: "email retained for records and for anything the user may need to retrieve
	// later". internal/platform/email has both a console implementation and a provider one, so
	// this dispatches for real in every environment.
	ChannelEmail Channel = "email"

	// ChannelSMS has a working sender and no rule that routes to it today.
	//
	// internal/platform/sms exists because identity sends one-time codes through it (SHIP-35).
	// Docs/01 §4.5 names push and email as the notification channels and says explicitly that
	// Shipper does not SMS the driver in the MVP, so routing a notification here would be
	// product design rather than implementation. The channel is supported end to end — the
	// dispatcher picks a sender by this value and there is a test that sends through it — so
	// that a ticket which decides otherwise adds a line to [Rules] and nothing else.
	ChannelSMS Channel = "sms"

	// ChannelPush is Docs/01 §4.5's primary channel, and since SHIP-139 and SHIP-140 it is one
	// this platform can actually send on.
	//
	// It is the one channel whose address is not a property of the account. An email address and
	// a phone number are columns on `users`; a push address is a device token bound to a signed-in
	// device (000701), so one person is nought, one or several addresses depending on how many
	// handsets they are signed in on — and none at all once they sign out.
	//
	// That is why [Recipient.AddressesOn] returns a list and why a recipient with nothing
	// registered is skipped rather than failing: a customer who has never opened the app is
	// reachable by email and by nothing else, which is a fact about them and not an error.
	ChannelPush Channel = "push"
)

// Channels is the closed set, in the order 000700 lists them.
var Channels = []Channel{ChannelEmail, ChannelSMS, ChannelPush}

// Valid reports whether c is one of [Channels].
func (c Channel) Valid() bool {
	for _, known := range Channels {
		if c == known {
			return true
		}
	}
	return false
}

func (c Channel) String() string { return string(c) }

// Category is what kind of notification this is, and it is the unit SHIP-142 lets a user mute.
//
// Four rather than one per event type, because a preference screen with thirteen switches on it is
// a screen nobody reads. They are drawn from Docs/01 §4.5's own grouping of its bullets.
type Category string

const (
	// CategoryBidding is offers arriving, being revised, countered, withdrawn, rejected or
	// expiring. Docs/01 §4.5: "new bid, counter-offer, withdrawal, or bid expiry".
	CategoryBidding Category = "bidding"

	// CategoryAward is a job being awarded or ending: Docs/01 §4.5's "bid accepted or job
	// cancelled", plus the job closing.
	CategoryAward Category = "award"

	// CategoryDelivery is Docs/01 §4.5's "delivery status changes" — a driver assigned, a
	// milestone recorded, proof captured.
	CategoryDelivery Category = "delivery"

	// CategoryJobExpiry is the one category that is not essential, and it is the reason
	// SHIP-142 has anything to switch off.
	//
	// A job approaching its deadline (SHIP-69) and a deadline that moved (SHIP-70) are both
	// reminders about a decision the customer can already see in the app. Docs/01 §4.5's list
	// of essential events does not include either. Everything else here is on that list.
	CategoryJobExpiry Category = "job_expiry"
)

// Categories is the closed set, in the order 000700's CHECK lists them.
var Categories = []Category{CategoryBidding, CategoryAward, CategoryDelivery, CategoryJobExpiry}

// Essential reports whether a category may be muted (SHIP-142).
//
// A method on the category rather than a field on the rule, so that "which categories are
// essential" has one answer rather than one per event type. The rule carries the answer onto the
// row, because 000700's essential column is a property of the notification as it was decided.
func (c Category) Essential() bool { return c != CategoryJobExpiry }

// Valid reports whether c is one of [Categories].
func (c Category) Valid() bool {
	for _, known := range Categories {
		if c == known {
			return true
		}
	}
	return false
}

func (c Category) String() string { return string(c) }

// Role is which side of a job a recipient is on.
//
// It is not stored. The routing rules are written in terms of it — "tell the customer", "tell the
// awarded provider" — and users.role is the authority on what any account actually is. A copy of
// the role on the notification row would be a third statement of a fact ck_users_role already
// fixes at registration.
type Role string

const (
	RoleCustomer Role = "customer"
	RoleProvider Role = "provider"
)

// Recipient is one person to tell, with the address to tell them at.
//
// Resolved once, when the notification row is written, and copied onto the row — see 000700 on why
// a dispatch retry must reach the address the platform had when the event happened.
type Recipient struct {
	UserID  uuid.UUID
	Role    Role
	Email   string
	Phone   string
	Deleted bool

	// PushTokens is every handset this person is addressable on, and it is the one field here
	// that is a list.
	//
	// A person has one email address and one number, and any number of signed-in devices. Each
	// is its own notification row: the phone and the tablet are two messages about one event,
	// and uq_notifications_event_recipient_device (000701) is what keeps them one apiece.
	//
	// Only the live ones. A token whose device session has been revoked is not here, which is
	// what "clears on sign-out" means in practice — see [Sessions].
	PushTokens []string
}

// AddressesOn is every address this recipient is reached at on a channel.
//
// Zero, one or many, and the many is push. SHIP-137's version of this returned one address and a
// boolean, which was exactly right while every channel had one address per person; a device token
// registry (SHIP-140) makes push the exception, and a signature returning one address would have
// forced the second handset to be dropped somewhere no caller could see it happen.
//
// An empty result is not a failure. It is a customer with no phone number, or one who has never
// opened the app on a handset — both ordinary, and both meaning "not reachable that way" rather
// than "something went wrong".
func (r Recipient) AddressesOn(c Channel) []string {
	switch {
	case c == ChannelEmail && r.Email != "":
		return []string{r.Email}
	case c == ChannelSMS && r.Phone != "":
		return []string{r.Phone}
	case c == ChannelPush:
		return r.PushTokens
	default:
		return nil
	}
}

// Notification is one row of the notifications table.
//
// There is no budget field and there never will be. Docs/01 §4.3 keeps the customer maximum
// private from providers, and a notification is read on a locked screen by whoever is holding the
// handset — which is further even than an event travels. TestNoNotificationFieldCanCarryABudget
// holds the shape rather than any particular instance of it.
type Notification struct {
	ID        uuid.UUID
	EventID   uuid.UUID
	EventType string
	JobID     uuid.UUID
	Recipient uuid.UUID
	Channel   Channel
	Category  Category
	Essential bool
	Address   string
	Subject   string
	Body      string
	Status    string
	Attempts  int
	LastError string
	SentAt    *time.Time
	CreatedAt time.Time
}
