-- SHIP-138: a failed notification waits before it is tried again.
--
-- # The defect this fixes, which is a starvation rather than a retry policy
--
-- 000700 made `failed` non-terminal and the dispatcher claims `ORDER BY created_at` with
-- `LIMIT 20` (internal/notifications.DispatchBatch). Those two facts together mean that twenty
-- rows which will never succeed — a mailbox that no longer exists, an address somebody typed
-- wrongly — are claimed on every pass in perpetuity, and **no notification written after them is
-- ever sent**. The queue is not slow; it is stopped, and every counter reports a healthy platform
-- dispatching twenty messages a pass.
--
-- It is worth being precise about why the ticket found this rather than the one that wrote the
-- dispatcher. SHIP-137's *Done when* is "dispatches per channel" and its retry is correct for the
-- failure it was written against: a provider down for a minute. SHIP-138's is "sends reliably",
-- and reliability is a claim about the queue rather than about one message.
--
-- # Why a column and not a computed delay
--
-- The claim has to be able to *exclude* a waiting row in its predicate, so that a partial index
-- serves it and so that a waiting row does not consume one of the twenty slots. A delay derived in
-- Go from `attempts` and `updated_at` would have to be written into the query as arithmetic on
-- every pass, which is the same expression with none of the index.
--
-- # Why the backoff is bounded
--
-- Docs/01 §4.5 says a notification failure must not lose the event, so nothing here gives up: the
-- schedule flattens at an hour and keeps retrying forever. What bounds the retries is a person
-- reading `attempts`, which is 000700's own answer and the column SHIP-176 alerts on.

-- Nullable, and NULL is the ordinary state rather than a gap: **nothing has deferred this row**.
--
-- A NOT NULL column with `DEFAULT now()` was the first shape and it is wrong in a way worth
-- recording, because it looks tidier. The default comes from the *database* clock while every
-- comparison against it comes from the *injected* one (Docs/10 §6.3), so a service whose clock is a
-- fixture — every test in this package, and any run against a seeded database — writes rows that
-- are already deferred past the instant the claim asks about, and dispatches nothing at all. Six
-- tests failed at once, which is the cheap version of that discovery.
--
-- NULL has no clock in it, so the two never have to agree.
ALTER TABLE notifications
    ADD COLUMN next_attempt_at timestamptz;

COMMENT ON COLUMN notifications.next_attempt_at IS
    'The earliest a dispatch pass may claim this row again (SHIP-138). NULL while nothing has deferred it; written on every failure, from the injected clock.';

-- The claim's predicate and its partial index have to agree or the index stops serving it. The
-- time comparison is deliberately **not** in the index — a partial index on `now()` is not
-- immutable and PostgreSQL refuses it — so the index narrows to the undelivered rows and the
-- planner applies the instant to those. `NULLS FIRST` because NULL means "claimable now", so the
-- rows nothing has deferred are the ones a pass wants first.
DROP INDEX idx_notifications_undelivered;

CREATE INDEX idx_notifications_undelivered
    ON notifications (next_attempt_at NULLS FIRST, created_at) WHERE status NOT IN ('sent', 'undeliverable');
