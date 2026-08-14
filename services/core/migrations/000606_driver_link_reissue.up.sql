-- SHIP-109: which link is the live one, so reissuing it invalidates the previous one.
--
-- # Revocation as a read, not a denylist
--
-- A driver token is stateless, signed, and cannot be recalled — 000600 said so and SHIP-108 built
-- the check that follows from it: the *row* is what knows whether a link still opens anything, and
-- Service.AssignmentFor asks it on every driver request. That check answered one question — is this
-- assignment still the live one on the job — and it is the wrong question for a reissue, because a
-- reissue deliberately leaves the assignment live. The driver has not changed; the link has.
--
-- So this column is the second half of the same mechanism. The assignment names the one link that
-- currently opens it, every driver request compares the `jti` inside the presented token with this
-- value, and a reissue is an UPDATE. There is no denylist to grow without bound, nothing to expire,
-- and no cache whose flush would restore a revoked link — which is the position Docs/10 §5 takes
-- about every control this platform has over a credential.
--
-- # Why not end the assignment and create another
--
-- That is what replacing a *driver* does, and it is the wrong shape for replacing a link. 000600
-- makes an assignment's identity immutable because `job_status_history.actor_id` and
-- `milestones.actor_id` name it: a second row would re-attribute nothing already recorded, but it
-- would split one driver's delivery across two identities for no reason anybody reading the history
-- could reconstruct. A driver who loses the link keeps the job (000600 says so in as many words).
--
-- # Why every existing link is invalidated by this migration, and why the default is a random value
--
-- The `jti` of a token already issued is recorded nowhere — it exists only inside the signed value
-- the provider forwarded — so there is nothing to backfill from. A NULL-means-accept-anything column
-- would have been the cheaper migration and it is the wrong one: it leaves every pre-existing link
-- permanently unrevokable, which is exactly the state this ticket exists to end. Links issued before
-- revocation existed cannot be revoked, so they are revoked here, once, and the provider reissues.
--
-- The default does the same work for a row this platform writes and then fails to finish. It is a
-- value no token can carry, so an assignment whose link was never recorded opens nothing — the safe
-- failure direction, and the reason the column is NOT NULL with a default rather than nullable.

ALTER TABLE driver_assignments
    ADD COLUMN link_token_id text NOT NULL DEFAULT gen_random_uuid()::text;

-- The bound the `jti` claim is written with: a UUID, rendered. Not a uuid column, because what is
-- compared is the string inside a JWT and a type conversion on every driver request would be a
-- second place for the two forms to disagree.
ALTER TABLE driver_assignments
    ADD CONSTRAINT ck_driver_assignments_link_token_id CHECK (
        link_token_id ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
    );

-- How many links this assignment has been given, and when the current one was issued.
--
-- Not decoration: Docs/04 §5 puts repeated reissues in front of a moderator eventually, and "this
-- link has been reissued nine times" is the kind of fact a support conversation starts from. The
-- count is also the only trace a revocation leaves — the previous `jti` is deliberately overwritten
-- rather than kept, because a list of dead link identifiers is a denylist by another name and this
-- migration's header explains why there is not one.
ALTER TABLE driver_assignments
    ADD COLUMN link_issued_at timestamptz NOT NULL DEFAULT now();

-- Zero, and the *assignment* is what takes it to one. Every path that hands somebody a link goes
-- through one statement (Service.granted), including the one that creates the assignment, so the
-- count is incremented in one place rather than initialised in one and incremented in another —
-- which is the arrangement in which the two drift.
ALTER TABLE driver_assignments
    ADD COLUMN link_issue_count integer NOT NULL DEFAULT 0;

ALTER TABLE driver_assignments
    ADD CONSTRAINT ck_driver_assignments_link_issue_count CHECK (link_issue_count >= 0);

COMMENT ON COLUMN driver_assignments.link_token_id IS
    'The jti of the one driver token that currently opens this assignment (SHIP-109). A reissue overwrites it, which is what invalidates the previous link — revocation is a read against this column, never a denylist.';
COMMENT ON COLUMN driver_assignments.link_issued_at IS
    'When the current link was issued. Not the assignment''s created_at: a reissue moves this and leaves that alone.';
COMMENT ON COLUMN driver_assignments.link_issue_count IS
    'How many links this assignment has been given; one after the assignment itself, and one more per reissue. The only trace a revocation leaves, because the previous jti is overwritten rather than kept.';
