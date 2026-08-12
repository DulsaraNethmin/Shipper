package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
)

// The transactional outbox publisher (SHIP-134) — the reading half of internal/events.
//
// Docs/06 §4.0 fixes the shape and the reason: publishing to Kafka from inside a database
// transaction cannot be made atomic, so the domain writes the event to the outbox table in the
// same transaction as the state change, and a separate process drains it. The transaction is
// then the only thing that has to be atomic, which it already is.
//
// # Why the drain lives in cmd/worker rather than in internal/events
//
// The writing half is internal/events, and putting the reader beside it was the obvious first
// answer. It is the wrong one, and the reason is the transitivity argument that already gave
// this service its fourth boundary rule (SHIP-15c): **every domain imports internal/events**,
// because every domain emits. A Kafka client imported from there is linked into cmd/api and into
// every domain's test binary, to serve one task in one binary that none of them run.
//
// internal/events/publish would avoid that — a subpackage of registered infrastructure needs no
// edit to internal/boundaries. It was rejected for a smaller reason: the drain is a claim loop,
// the claim loop lives here, and checkClaim in claim.go is the check that a claim query actually
// says FOR UPDATE SKIP LOCKED. Reaching that from another package means exporting it or writing
// the query without it, and the second is exactly the mistake claim.go exists to catch.
//
// A platform adapter under internal/platform was never in the running. CLAUDE.md admits an
// adapter only where a second implementation exists today, and there is one Kafka.
//
// # What this is not
//
// It does not create topics. **SHIP-135 owns the topic set and applies it from cmd/topics**, the
// way a migration is applied, and a topic created here with whatever partition count seemed
// reasonable would be worse than no topic at all: partitions can be added later but never removed,
// and adding one moves every key to a different partition, which silently ends the per-aggregate
// ordering promised below. A publish to a topic nobody applied fails the pass and the rows stay
// claimable — see the failure note on run — which is the correct direction to fail, and now says
// the deployment step was skipped rather than that a row is stuck.

// EventPublisher is what a pass needs from a message broker.
//
// Declared here, by the code that consumes it, in the same way a domain declares its ports
// (Docs/10 §2.3). The Kafka implementation is in tasks_outbox.go and is the only file in this
// service that imports a Kafka client.
//
// Publish returns nil only when the broker has acknowledged every event in the batch. Anything
// weaker would let the pass mark rows the broker never received, which is the one failure the
// outbox exists to prevent.
type EventPublisher interface {
	Publish(ctx context.Context, batch []events.Event) error
}

// outboxDrain is one pass over the outbox: claim, publish, mark.
type outboxDrain struct {
	to    EventPublisher
	batch int
}

// lockDueAggregates takes the aggregates behind the oldest unpublished events, for this pass.
//
// # The ordering promise, and the part of it SKIP LOCKED alone does not keep
//
// 000004_outbox.up.sql promises ordering per aggregate and not globally: events for one job
// publish in the order they were written. A claim that only said
//
//	WHERE published_at IS NULL ORDER BY occurred_at, id FOR UPDATE SKIP LOCKED LIMIT n
//
// does not keep that promise once two workers run, which a rolling deployment guarantees. Worker
// A claims the oldest hundred rows, which happen to include job X's first two events; worker B
// skips A's locked rows and claims the next hundred, which include job X's third. If B reaches
// the broker first — and it will whenever A's batch is larger or its connection slower — job X's
// events arrive out of order, and nothing anywhere reports it.
//
// So the unit of division is the aggregate rather than the row. A transaction-scoped advisory
// lock per aggregate is atomic, so an aggregate belongs to exactly one worker until that worker's
// transaction ends; B is refused job X outright and moves on to an aggregate nobody holds. The
// work is still divided rather than duplicated, which is what SKIP LOCKED was for.
//
// # Why the lock is taken in its own statement
//
// Putting pg_try_advisory_xact_lock in the claim's WHERE clause is shorter, and it was the first
// version of this. It is wrong for a reason worth keeping: **how many rows a qual is evaluated
// against is the planner's decision, not the query's.** With the partial index driving an ordered
// index scan, the qual runs on roughly LIMIT rows. On a small table PostgreSQL prefers a bitmap
// scan and a Sort, which evaluates the qual on the *entire* backlog before the LIMIT applies —
// so one worker takes an advisory lock on every aggregate in the outbox and a second worker gets
// nothing at all. Correct, and a throughput collapse that appears only at a certain table size.
// TestOneAggregateBelongsToOneWorker found it on five rows.
//
// Two statements make the count independent of the plan: MATERIALIZED fixes the LIMIT before the
// lock is attempted, so exactly the distinct aggregates among the oldest batch rows are tried and
// no others.
//
// 134 is the lock class, chosen to be this ticket's number so that a second use of advisory locks
// in this database has to pick a different one deliberately. The key is the aggregate id alone
// rather than the type and the id: ids are UUIDs, so two aggregates sharing one is not a thing
// that happens, and were it to, the cost is one worker draining both in order rather than two.
const lockDueAggregates = `
	WITH oldest AS MATERIALIZED (
		SELECT aggregate_id
		FROM outbox
		WHERE published_at IS NULL
		ORDER BY occurred_at, id
		LIMIT $1
	), due AS MATERIALIZED (
		SELECT DISTINCT aggregate_id FROM oldest
	)
	SELECT aggregate_id
	FROM due
	WHERE pg_try_advisory_xact_lock(134, hashtext(aggregate_id::text))`

// claimUnpublished reads the events of the aggregates this pass holds, oldest first.
//
// The row lock is not ceremony on top of the advisory lock. It is what makes a crashed worker
// harmless — PostgreSQL releases the claim when the connection dies and the next pass finds the
// rows — and SKIP LOCKED is what stops this pass waiting behind a row another worker took in the
// window before it held the aggregate.
//
// The LIMIT can cut an aggregate in half, and that is fine: the remainder is the oldest thing in
// the outbox next pass, and it publishes after the half that has already gone.
const claimUnpublished = `
	SELECT id, aggregate_type, aggregate_id, event_type, payload, occurred_at
	FROM outbox
	WHERE published_at IS NULL
	  AND aggregate_id = ANY($1)
	ORDER BY occurred_at, id
	FOR UPDATE SKIP LOCKED
	LIMIT $2`

// markPublished records that the broker acknowledged these events.
//
// published_at is the publisher's only state — 000004 says so, and a second state column or a
// progress table would be a second thing to keep in step with the first. now() is the platform's
// clock rather than an actor's, which is the distinction SHIP-110 made structural for milestones
// and which needs nothing structural here: no client supplies this value.
const markPublished = `
	UPDATE outbox SET published_at = now() WHERE id = ANY($1)`

// run is one pass. It is a Work, and everything it does happens inside the caller's transaction.
//
// # Publish, then mark, then commit — and the commit is the ordering that matters
//
// The row is marked only after the broker has acknowledged it, and the mark becomes durable only
// when the transaction commits. That leaves exactly three outcomes:
//
//   - The publish fails. The pass returns an error, db.InTx rolls back, published_at is still
//     NULL and the advisory locks are released. Nothing is lost and the next pass finds the same
//     rows. This is the broker-unreachable case, and it is why the mark cannot come first.
//   - The publish succeeds and the commit fails, or the process dies between them. The events
//     are on the broker and the rows still look unpublished, so the next pass publishes them
//     again. This is the at-least-once window Docs/06 §4.0 names, and it is why every consumer
//     must deduplicate on events.Event.ID.
//   - Both succeed, which is the ordinary case.
//
// There is no fourth outcome in which an event is marked but never published. That asymmetry is
// the whole design: duplicates are a consumer's problem and a solved one; a lost award
// notification is neither.
//
// # A whole batch fails together
//
// A partial failure inside Publish fails the pass, so events the broker did accept are published
// again next pass rather than marked. Salvaging the acknowledged half would mean returning nil
// from a pass that failed, and the scheduler would log "claimed n" through a broker outage.
// Louder and slightly wasteful beats quiet and slightly efficient.
//
// The cost would be head-of-line blocking: one event the broker will never accept — over the
// message size limit, say — stopping the aggregates in its batch until somebody looked. SHIP-134
// recorded that in Docs/11 §9 rather than solving it, because the obvious fix is a dead-letter
// path.
//
// **SHIP-135 closed it from the other end and there is no dead-letter path.** Every condition that
// can permanently reject an event — an unregistered type, an aggregate with no topic, a payload
// over the broker's limit — is now checked by internal/events when the row is written, inside the
// transaction making the state change, where a failure rolls the change back and names the line
// that caused it. So a row that reaches this table is one the broker will accept, and every
// failure left here is transient: the broker is unreachable, or the topic set was never applied.
// Failing the whole batch and leaving every row claimable is exactly right for both.
// internal/events/catalogue.go carries the argument and the trigger that would reopen it.
func (d outboxDrain) run(ctx context.Context, r db.Runner) (int, error) {
	if err := checkClaim(claimUnpublished); err != nil {
		return 0, err
	}

	locked, err := r.Query(ctx, lockDueAggregates, d.batch)
	if err != nil {
		return 0, fmt.Errorf("worker: outbox: taking the due aggregates: %w", err)
	}
	aggregates, err := pgx.CollectRows(locked, pgx.RowTo[uuid.UUID])
	if err != nil {
		return 0, fmt.Errorf("worker: outbox: reading the due aggregates: %w", err)
	}
	if len(aggregates) == 0 {
		// Either the outbox is empty or every aggregate in the oldest batch belongs to
		// another worker. Both are the ordinary case and neither is a failure.
		return 0, nil
	}

	rows, err := r.Query(ctx, claimUnpublished, aggregates, d.batch)
	if err != nil {
		return 0, fmt.Errorf("worker: outbox: claiming unpublished events: %w", err)
	}

	batch, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (events.Event, error) {
		var e events.Event
		var payload []byte
		if err := row.Scan(&e.ID, &e.AggregateType, &e.AggregateID, &e.Type,
			&payload, &e.OccurredAt); err != nil {
			return e, err
		}
		e.Payload = json.RawMessage(payload)
		return e, nil
	})
	if err != nil {
		return 0, fmt.Errorf("worker: outbox: reading claimed events: %w", err)
	}
	if len(batch) == 0 {
		return 0, nil
	}

	if err := d.to.Publish(ctx, batch); err != nil {
		return 0, fmt.Errorf("worker: outbox: publishing %d event(s), oldest %s (%s): %w",
			len(batch), batch[0].Type, batch[0].ID, err)
	}

	ids := make([]uuid.UUID, len(batch))
	for i, e := range batch {
		ids[i] = e.ID
	}
	tag, err := r.Exec(ctx, markPublished, ids)
	if err != nil {
		return 0, fmt.Errorf("worker: outbox: marking %d event(s) published: %w", len(batch), err)
	}
	if tag.RowsAffected() != int64(len(batch)) {
		// The rows were claimed FOR UPDATE in this transaction, so nothing else can have
		// changed them. Reaching here means the mark and the claim disagree about which
		// rows they are talking about, and committing on that would leave events on the
		// broker that the database still believes are unpublished.
		return 0, fmt.Errorf("worker: outbox: marked %d event(s) published but claimed %d",
			tag.RowsAffected(), len(batch))
	}

	return len(batch), nil
}
