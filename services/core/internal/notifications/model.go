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

	// ChannelPush is Docs/01 §4.5's primary channel and cannot be sent today.
	//
	// SHIP-139 is the Firebase adapter and SHIP-140 is the device token registry, and neither
	// exists — so there is no address a push notification could be written with, which is why
	// [Rules] produces none rather than producing rows nothing can complete. [Pusher] is the
	// port SHIP-139 fills; declaring it here is the whole of what this ticket can honestly do
	// about push.
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
}

// AddressOn is where this recipient is reached on a channel, and whether they can be.
//
// Push has no address and never will have one here: SHIP-140's device tokens are rows in a table
// this ticket does not create, and inventing an address for them would be worse than having none.
func (r Recipient) AddressOn(c Channel) (string, bool) {
	switch c {
	case ChannelEmail:
		return r.Email, r.Email != ""
	case ChannelSMS:
		return r.Phone, r.Phone != ""
	default:
		return "", false
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
