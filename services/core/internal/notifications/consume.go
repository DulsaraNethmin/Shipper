package notifications

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
)

// The consumer half of SHIP-137: read an event, work out who has to be told, and write one row per
// person per channel.
//
// Nothing is sent here. The rows are committed first and dispatched afterwards (dispatch.go), which
// is what makes Docs/01 §4.5's "a notification failure must not lose the event" true of a channel
// that is down: the row is already durable and the next dispatch pass finds it.

// Senders is what a process can actually send on.
//
// A struct rather than three constructor parameters because two of the three are legitimately nil
// today and a positional nil is the kind of wiring mistake nobody sees in review. [Pusher] has no
// implementation anywhere (SHIP-139), and a process may reasonably run with email only.
type Senders struct {
	Email EmailSender
	SMS   SMSSender
	Push  Pusher
}

// Service holds this domain's rules.
//
// It owns no connection, like every other domain service here: every method takes a db.Runner,
// because the transaction belongs to whoever owns the invariant being protected (Docs/10 §3.2) —
// and for a consumer pass that is this package, since "every row for one event, or none" is what
// makes a redelivery meet a complete set.
type Service struct {
	parties  Parties
	sessions Sessions
	clock    clock.Clock
	senders  Senders
	store    postgresStore
}

// Option adjusts what a service can do beyond consuming and sending.
//
// Variadic rather than a fourth parameter to [NewService], and the reason is the one [Senders] was
// made a struct for: a positional nil is the wiring mistake nobody sees in review. [Sessions] is
// legitimately absent from any process that does not dispatch — cmd/api registers device tokens and
// sends nothing — and a fourth parameter would have put a nil at fifteen call sites with no
// interest in push, which is exactly the shape that eventually gets passed in the wrong order.
//
// An option is only acceptable here because the default is the safe answer in every case. See
// [WithSessions].
type Option func(*Service)

// WithSessions supplies the device-session liveness lookup (SHIP-140).
//
// Without it **no push notification is addressed at all**, deliberately. A process that cannot tell
// a live device session from a signed-out one cannot tell which handsets it may address, and
// addressing all of them would push to phones whose owner has signed out — the one failure this
// port exists to prevent. Quietly sending to everybody would be the dangerous default; quietly
// sending to nobody is visible in a single query over `notifications`.
func WithSessions(sessions Sessions) Option {
	return func(s *Service) { s.sessions = sessions }
}

// NewService builds the domain service.
//
// The parties lookup and the clock are required and it panics without them, in the same spirit as
// jobs.NewService: this is called once from a composition root, a missing collaborator is a
// programming mistake rather than a runtime condition, and the alternative is a consumer that runs
// and quietly resolves nobody.
//
// The senders are not required, and that is a different kind of absence. A process with no email
// sender still consumes correctly — the rows are written and stay pending — and dispatch.go reports
// [ErrNoSender] per row rather than at start-up, because the row is the thing that must not be
// lost. cmd/notifier does supply one; a test that only exercises consumption does not have to.
func NewService(p Parties, c clock.Clock, s Senders, opts ...Option) *Service {
	if p == nil {
		panic("notifications: NewService needs a Parties lookup; two thirds of the catalogue " +
			"names only a job, and a consumer that cannot resolve a job's parties resolves " +
			"nobody at all")
	}
	if c == nil {
		panic("notifications: NewService needs a clock (Docs/10 §6.3)")
	}
	svc := &Service{parties: p, clock: c, senders: s}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// facts is what this domain reads out of an event payload.
//
// Deliberately a narrow struct rather than a map, and deliberately these fields and no others.
// Everything a rule needs to route is here, and nothing a body could leak is: no amount, no
// address, no goods description, no name. That is the structural half of SHIP-141 and of Docs/01
// §4.3 — the renderer cannot put a budget in a body, because the budget is not in any event and
// this struct could not carry it if it were.
//
// Unknown fields are ignored rather than refused. A payload gaining an optional field is explicitly
// not a version change (internal/events.Schema), so a consumer that rejected one would break on the
// next additive release.
type facts struct {
	JobID      string `json:"job_id"`
	CustomerID string `json:"customer_id"`
	ProviderID string `json:"provider_id"`

	// To is the status a job.status_changed moved to, and is what StatusRules keys on.
	To string `json:"to"`

	// ActorID is who caused this, where the event names them. Used only to suppress telling
	// somebody what they have just done.
	ActorID string `json:"actor_id"`

	// OfferedBy is `customer` or `provider` on a bid event: which side made the offer. The
	// bidding aggregate has no actor id, so this is the same suppression by role rather than by
	// identifier.
	OfferedBy string `json:"offered_by"`
}

// Consume turns one event into notification rows, inside the caller's transaction.
//
// It returns how many rows were written, which is not how many recipients were resolved: a
// redelivery resolves the same people and writes nothing, because
// uq_notifications_event_recipient_channel refuses the second insert. That difference is the whole
// of the idempotence, and it is a property of the database rather than of anything this function
// remembers.
//
// # Every outcome that is not a failure returns nil
//
// A rule that tells nobody, a job that no longer exists, a recipient with no address on a channel:
// all of them are ordinary and all of them return zero and nil, so the message is acknowledged and
// the consumer moves on. An event type with no rule at all is the exception — see [ErrNoRule].
func (s *Service) Consume(ctx context.Context, r db.Runner, env events.Envelope) (int, error) {
	// pgx.Tx and *pgxpool.Pool both satisfy db.Runner and only one of them is a transaction.
	// Checked before anything is written, for the reason jobs.Transition checks: half a set of
	// rows committed on its own is a set a redelivery would complete with a duplicate.
	if _, inTx := r.(pgx.Tx); !inTx {
		return 0, fmt.Errorf("notifications: %s (%s): %w", env.Type, env.ID, ErrNotInTransaction)
	}

	var f facts
	if err := json.Unmarshal(env.Payload, &f); err != nil {
		return 0, fmt.Errorf("notifications: reading the payload of %s (%s): %w",
			env.Type, env.ID, err)
	}

	rule, known := RuleFor(env.Type, f.To)
	if !known {
		return 0, fmt.Errorf("notifications: %s (%s): %w", env.Type, env.ID, ErrNoRule)
	}
	if len(rule.To) == 0 {
		// A decision, not a gap: Rule.Why says which. Nothing is written and the message is
		// acknowledged.
		return 0, nil
	}

	jobID, err := uuid.Parse(f.JobID)
	if err != nil {
		return 0, fmt.Errorf("notifications: %s (%s) names job %q, which is not an "+
			"identifier: %w", env.Type, env.ID, f.JobID, err)
	}

	recipients, err := s.resolve(ctx, r, rule, f, jobID)
	if err != nil {
		return 0, err
	}

	// SHIP-142's filter, and the condition in front of it is the guarantee rather than an
	// optimisation.
	//
	// `Essential()` is false for exactly one category today, so twelve of the thirteen events in
	// the catalogue never reach this at all — which means there is **no branch in which a muted
	// row for an award could be read and honoured**, whatever is in the table. That is the same
	// guarantee ck_notification_preferences_category gives from the other end: the database
	// cannot hold the row, and the consumer would not look for it.
	//
	// It also saves a query inside a transaction holding a Kafka partition's progress, which is
	// the reason `ruleNeedsPush` guards the device lookup the same way.
	if !rule.Category.Essential() {
		recipients, err = s.filterMuted(ctx, r, rule.Category, recipients)
		if err != nil {
			return 0, err
		}
	}

	if len(recipients) == 0 {
		return 0, nil
	}

	rows := make([]Notification, 0, len(recipients)*len(rule.Channels))
	for _, recipient := range recipients {
		for _, channel := range rule.Channels {
			// Rendered per channel, because an email and a locked screen are not the same
			// surface — templates.go carries the argument. Rendered here rather than per
			// address, because two handsets belonging to one person get one message.
			subject, text, err := Render(channel, rule, jobID)
			if err != nil {
				return 0, err
			}

			// Nought, one or several. Push is the channel that can be several — one row
			// per signed-in handset — and the channel that is routinely nought, because a
			// customer who has never opened the app is reachable by email and by nothing
			// else. Both are ordinary and neither is an error.
			for _, address := range recipient.AddressesOn(channel) {
				id, err := uuid.NewV7()
				if err != nil {
					return 0, fmt.Errorf("notifications: generating an id: %w", err)
				}

				rows = append(rows, Notification{
					ID:        id,
					EventID:   env.ID,
					EventType: env.Type,
					JobID:     jobID,
					Recipient: recipient.UserID,
					Channel:   channel,
					Category:  rule.Category,
					Essential: rule.Category.Essential(),
					Address:   address,
					Subject:   subject,
					Body:      text,
				})
			}
		}
	}

	return s.store.insert(ctx, r, rows)
}

// The rendering used to live here, as a single concatenation. **SHIP-138 moved it to
// templates.go** and made it per channel, and it inherited this function's whole argument rather
// than replacing it: the inputs are a headline the routing table declares as a literal and a job
// identifier, and nothing else is available. So SHIP-141's rule — no address, no goods description,
// no full customer name in a notification body — still holds because there is nowhere for any of
// those to come from, rather than because a redaction pass removes them.

// resolve turns a rule's audiences into people with addresses.
//
// Two lookups at most: one [Parties] call, and one read of `users` for whichever identifiers came
// out of it. Not one query per audience — an award notifies two people and would otherwise be two
// round trips inside a transaction that is holding a Kafka partition's progress.
//
// # Nobody is told what they have just done
//
// Docs/01 §4.5 says which events generate a notification, not who they generate one for, and the
// obvious reading — everyone party to the job — sends a provider an email about their own
// withdrawal. So the actor is suppressed, by identifier where the event names one (job and delivery
// events carry actor_id) and by role where it names only a side (bid events carry offered_by).
//
// This is suppression rather than an absence in the routing table because the same rule serves both
// directions: bid.countered goes to whichever of the two did not counter, and one rule expresses
// that where two would have to agree.
func (s *Service) resolve(
	ctx context.Context, r db.Runner, rule Rule, f facts, jobID uuid.UUID,
) ([]Recipient, error) {
	var customer, provider uuid.UUID

	// Only when a rule actually asks for one of them. A bid rejection names its provider in the
	// payload and needs no job read at all.
	if rule.needs(ToJobCustomer) || rule.needs(ToAwardedProvider) {
		var found bool
		var err error
		customer, provider, found, err = s.parties.PartiesOn(ctx, r, jobID)
		if err != nil {
			return nil, fmt.Errorf("notifications: resolving the parties on %s: %w", jobID, err)
		}
		if !found {
			// The job is gone — pruned, or pseudonymised under Docs/05 §3.1. Nobody to
			// tell, and not a failure: stopping the topic over it would hold up every
			// later event for a job that no longer exists.
			return nil, nil
		}
	}

	wanted := make([]uuid.UUID, 0, len(rule.To))
	roles := make(map[uuid.UUID]Role, len(rule.To))

	add := func(id uuid.UUID, role Role) {
		if id == uuid.Nil {
			return
		}
		if _, already := roles[id]; already {
			// One person on both sides of a rule is told once. ck_users_role makes that
			// impossible today (SHIP-45 fixes the role at registration), so this is a
			// decision recorded rather than one relied on — and it is also what stops
			// bid.countered writing two rows when the payload provider is the awarded
			// one.
			return
		}
		roles[id] = role
		wanted = append(wanted, id)
	}

	for _, audience := range rule.To {
		switch audience {
		case ToJobCustomer:
			if f.OfferedBy == string(RoleCustomer) {
				continue
			}
			add(customer, RoleCustomer)
		case ToAwardedProvider:
			if f.OfferedBy == string(RoleProvider) {
				continue
			}
			add(provider, RoleProvider)
		case ToBidProvider:
			if f.OfferedBy == string(RoleProvider) {
				continue
			}
			id, err := uuid.Parse(f.ProviderID)
			if err != nil {
				// A bid rule whose event names no provider is a schema change
				// nobody told this table about, and writing nothing would hide it.
				return nil, fmt.Errorf("notifications: %s names provider %q, which "+
					"is not an identifier: %w", rule.Headline, f.ProviderID, err)
			}
			add(id, RoleProvider)
		default:
			return nil, fmt.Errorf("notifications: %q is not an audience any rule may "+
				"name", audience)
		}
	}

	// The actor, where the event named one. Parsed leniently: an event with no actor_id — every
	// bid event, and a system transition — leaves it zero and suppresses nobody.
	if actor, err := uuid.Parse(f.ActorID); err == nil && actor != uuid.Nil {
		delete(roles, actor)
		wanted = withoutID(wanted, actor)
	}

	if len(wanted) == 0 {
		return nil, nil
	}

	recipients, err := s.store.contacts(ctx, r, wanted, roles)
	if err != nil {
		return nil, err
	}
	if !ruleNeedsPush(rule) {
		// A rule that does not push needs no device tokens, and the two extra queries below
		// are two round trips inside a transaction holding a Kafka partition's progress.
		return recipients, nil
	}
	return s.withDevices(ctx, r, recipients)
}

// ruleNeedsPush reports whether a rule sends on [ChannelPush].
func ruleNeedsPush(rule Rule) bool {
	for _, channel := range rule.Channels {
		if channel == ChannelPush {
			return true
		}
	}
	return false
}

// withDevices fills in each recipient's live push addresses (SHIP-140).
//
// # Two queries rather than one join, and the second one is a port
//
// The first reads this domain's own `device_tokens`. The second asks [Sessions] which of those
// devices are still signed in, and it is a port because `device_sessions` is identity's table —
// ports.go carries the argument, and it is the same one that makes `Parties` a port rather than a
// join onto `jobs` and `bids`.
//
// **The filter is what makes a token clear on sign-out.** 000104 revokes a session rather than
// deleting it, so nothing cascades and nothing writes here; what happens instead is that the
// revoked session stops appearing in this answer, from the instant the revocation commits.
//
// A service with no [Sessions] resolves no push address at all, which is [WithSessions]'s doing and
// is the safe direction: it under-notifies visibly rather than pushing to a handset whose owner has
// signed out.
func (s *Service) withDevices(
	ctx context.Context, r db.Runner, recipients []Recipient,
) ([]Recipient, error) {
	if s.sessions == nil || len(recipients) == 0 {
		return recipients, nil
	}

	ids := make([]uuid.UUID, 0, len(recipients))
	for _, recipient := range recipients {
		ids = append(ids, recipient.UserID)
	}

	devices, err := s.store.liveDevicesOf(ctx, r, ids)
	if err != nil {
		return nil, err
	}
	if len(devices) == 0 {
		return recipients, nil
	}

	sessionIDs := make([]uuid.UUID, 0, len(devices))
	for _, registered := range devices {
		for _, device := range registered {
			sessionIDs = append(sessionIDs, device.SessionID)
		}
	}
	live, err := s.sessions.LiveSessions(ctx, r, sessionIDs)
	if err != nil {
		return nil, fmt.Errorf("notifications: resolving which device sessions are live: %w", err)
	}

	for i, recipient := range recipients {
		for _, device := range devices[recipient.UserID] {
			if live[device.SessionID] {
				recipients[i].PushTokens = append(recipients[i].PushTokens, device.Token)
			}
		}
	}
	return recipients, nil
}

// needs reports whether a rule names an audience.
func (r Rule) needs(a Audience) bool {
	for _, want := range r.To {
		if want == a {
			return true
		}
	}
	return false
}

func withoutID(ids []uuid.UUID, drop uuid.UUID) []uuid.UUID {
	out := ids[:0]
	for _, id := range ids {
		if id != drop {
			out = append(out, id)
		}
	}
	return out
}
