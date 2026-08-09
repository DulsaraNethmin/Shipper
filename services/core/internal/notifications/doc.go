// Package notifications turns domain events into messages that reach a person — the
// seventh of the eight platform domains in Docs/06 §3.
//
// # What lives here
//
// The transactional outbox and its publisher, the event consumer, recipient resolution,
// per-channel dispatch, the device token registry, redaction rules, and user
// preferences. SHIP-134…SHIP-142.
//
// # Rules this domain is responsible for
//
//   - An event is written in the same transaction as the state change it describes, and
//     published from the outbox afterwards (SHIP-134). Publishing inside the transaction
//     means a rollback that has already told the world; publishing after it means a crash
//     that loses the event. The outbox is what removes the choice.
//   - Delivery is at least once. Every consumer is idempotent, because the alternative to
//     a duplicate notification is a missing one.
//   - A notification failure must not lose the event (Docs/01 §4.5). The event survives in
//     the outbox and in Kafka regardless of what any channel does with it.
//   - No address, goods description, or full customer name appears in a notification body
//     (SHIP-141). A push notification renders on a locked screen, which is a display
//     nobody consented to.
//   - Essential events cannot be muted (SHIP-142). Preferences govern the rest.
//
// # Where the events come from
//
// The domain that changes state emits the event, not the API layer (Docs/08 slice 5). This
// package consumes them; it never reaches back into jobs, bidding, or delivery to ask what
// happened.
//
// The package is empty at SHIP-10 by design. The skeleton exists so the boundaries are
// enforced before there is code to bend them — see the file layout and the boundary rules
// in services/core/README.md.
package notifications
