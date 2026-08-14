// Package notifications turns domain events into messages that reach a person — the
// seventh of the eight platform domains in Docs/06 §3.
//
// # What lives here
//
// The event consumer, recipient resolution, per-channel dispatch, the notifications table, and —
// when their tickets arrive — the device token registry, redaction rules and user preferences.
// SHIP-137 built the first four; SHIP-138 to SHIP-142 are the rest.
//
// **The transactional outbox and its publisher are not here, and this file used to say they
// were.** SHIP-134 put the writer in internal/events and the drain in cmd/worker, and both
// decisions have a reason that outranks the tidy grouping this sentence originally described.
// The writer is infrastructure because every domain emits, and a domain that imported another
// domain to emit would fail the lint — internal/events/events.go carries the argument. The drain
// is in cmd/worker because it holds a Kafka producer, and a Kafka client imported from
// internal/events would be linked into cmd/api and every domain's test binary to serve one task
// in one binary; cmd/worker/outbox.go carries that one. The correction is recorded rather than
// quietly made, because "the outbox lives in notifications" is exactly the kind of statement a
// later ticket would have built on.
//
// # Rules this domain is responsible for
//
//   - An event is written in the same transaction as the state change it describes, and
//     published from the outbox afterwards (SHIP-134). Publishing inside the transaction
//     means a rollback that has already told the world; publishing after it means a crash
//     that loses the event. The outbox is what removes the choice.
//   - Delivery is at least once. Every consumer is idempotent, because the alternative to
//     a duplicate notification is a missing one. SHIP-137 makes that structural rather than
//     careful: uq_notifications_event_recipient_channel in 000700 refuses the second row, so
//     a redelivered event writes nothing and a second consumer instance racing the first
//     resolves to one row without coordinating.
//   - A notification failure must not lose the event (Docs/01 §4.5). The event survives in
//     the outbox and in Kafka regardless of what any channel does with it — and since
//     SHIP-137 the *recipient* survives too, as a pending row that no channel failure can
//     roll back, because it is committed before anything is sent.
//   - No address, goods description, or full customer name appears in a notification body
//     (SHIP-141). A push notification renders on a locked screen, which is a display
//     nobody consented to. SHIP-137 leaves that rule with nothing to enforce: the renderer
//     is handed a fixed headline and a job identifier, never the job, so there is nowhere
//     for any of the three to come from.
//   - A customer budget never reaches a provider (Docs/01 §4.3), and a notification is
//     further along that road than any endpoint — it is read on a handset by whoever is
//     holding it. No event in the catalogue carries a budget, the facts struct this package
//     decodes could not hold one, and a test refuses a Notification field named for it.
//   - Essential events cannot be muted (SHIP-142). Preferences govern the rest. The
//     essential/mutable split is decided here already, on Category, and carried onto every
//     row, so SHIP-142 is a preference table and a filter rather than a redesign.
//
// # Where the events come from
//
// The domain that changes state emits the event, not the API layer (Docs/08 slice 5). This
// package consumes them; it never reaches back into jobs, bidding, or delivery to ask what
// happened. Where a rule needs a fact no event carries — which accounts are party to a job — it
// declares a port and the composition root supplies the query, which is the arrangement
// admin.JobParties already uses (SHIP-113, SHIP-117). See ports.go.
//
// # Where it runs
//
// cmd/notifier, which is a service of its own rather than a task in cmd/worker. Docs/09 calls
// SHIP-137 a "notification consumer service" and Docs/11 §6 left the choice to the lane; the
// reason it went this way is in cmd/notifier/main.go, and the short form is that a cmd/worker pass
// *is* one transaction and a consumer must commit its topic offsets strictly after its database
// transaction commits.
package notifications
