-- SHIP-111: the key that recorded a milestone, so a retry cannot record a second one.
--
-- # Redis replays a response; this refuses a row, and they are not the same guarantee
--
-- SHIP-15's middleware stores the first response against the key and returns it to every later
-- request carrying that key. That is what makes a retry cheap, and it is bounded by a TTL and by
-- Redis being a cache: the entry expires, and a cache can be flushed. A phone that has been out of
-- signal for longer than the TTL — which is the ordinary case Docs/01 §4.4 describes, "pickup bays,
-- warehouses, and rural routes" — retries into a store that has forgotten the key, and the handler
-- runs a second time.
--
-- The endpoint's *Done when* is "records a milestone once per idempotency key", and a milestone
-- recorded twice is a duplicate on the customer's delivery timeline and in every count support
-- takes off this table. So the guarantee is written down where it survives a restart, an eviction
-- and two concurrent requests: a unique index. Redis makes the retry fast; the index makes it
-- correct.
--
-- # Scoped per job, which is the tightest scope that refuses nothing legitimate
--
-- Keys are client-generated, one per action (Docs/02 §3.1). Two facts decide the scope:
--
--   * A key must not be able to refuse a *different* action. A client that reuses one value across
--     two jobs has made two requests that both deserve to succeed, so the job belongs in the key.
--   * A key must not be able to reach another caller's record. Only the awarded provider may write
--     a milestone on a job (Docs/02 §3, and SHIP-111 enforces it before this index is reached), so
--     job_id already scopes the key to one caller's work. A subject column would be a second copy
--     of that fact, and a wrong one the moment SHIP-108's driver records under the same key.
--
-- Partial on the column being present, because the column is optional and must stay so: Docs/02 §2
-- permits `Picked up → In transit` as "an automatic presentation change", which is the platform
-- recording a milestone with no request and therefore no key behind it. NULLs are distinct to a
-- btree in any case, so the predicate is documentation as much as it is mechanism — it says that a
-- keyless milestone is expected rather than an omission.
ALTER TABLE milestones ADD COLUMN idempotency_key text;

-- The same bound httpx puts on the header (maxIdempotencyKeyLen), so a value that reaches the
-- column has already been refused at the edge if it is malformed. Written out rather than trusted:
-- the middleware protects the endpoint, and this protects the table from anything that ever writes
-- to it without passing through one.
ALTER TABLE milestones ADD CONSTRAINT ck_milestones_idempotency_key CHECK (
    idempotency_key IS NULL OR
    (length(idempotency_key) BETWEEN 1 AND 255)
);

-- **This is the whole of the "once per idempotency key" guarantee.**
--
-- A unique *index* rather than a unique *constraint* because the rule is partial, and a constraint
-- cannot be. That is the same reason uq_driver_assignments_active is an index (000600), and it has
-- the same consequence: `ON CONFLICT (job_id, idempotency_key) WHERE idempotency_key IS NOT NULL`
-- is how a writer infers it, since there is no constraint name to name.
CREATE UNIQUE INDEX uq_milestones_idempotency
    ON milestones (job_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
