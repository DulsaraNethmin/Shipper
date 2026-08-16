-- SHIP-81a: Docs/04 §4's five verification outcomes, as a record with a guarded transition.
--
-- The first migration in block 200–299 and the first table `internal/profiles` owns, eleven waves
-- after SHIP-10 created the package. `000002_users.up.sql` said where these belong in its own column
-- comment — "Provider verification state is separate and lives with profiles (Docs/04 §4)" — and
-- this is that place.
--
-- # What this is not: `users.status`
--
-- `users.status` has three values and is *account standing* — may this person sign in at all
-- (`000002`, SHIP-161, SHIP-166). This is *eligibility to bid*, which Docs/04 §1 calls "an
-- eligibility decision, not a guarantee of delivery quality". The two move independently: an active
-- account may be Pending, and a Verified provider may be restricted for something that has nothing
-- to do with their documents. Collapsing them would make one column answer two questions and give
-- the wrong answer to whichever one was asked second.
--
-- # Every provider has a row, and the database is what guarantees it
--
-- The alternative — a row appearing when somebody first decides something — was rejected on
-- SHIP-153. Its *Done when* is "pending provider verifications listed oldest first", and a queue
-- over a table that only holds *decided* providers lists nobody who is waiting. `Pending` is a state
-- a provider is *in*, not the absence of a state, so it is a row.
--
-- So the row is created by `provider_verification_on_registration`, a trigger on `users`, and the
-- providers who registered before this migration are backfilled at the foot of this file.
--
-- **A trigger on another domain's table is a deliberate crossing and is the narrower of the two
-- options available.** `internal/profiles` cannot ask `internal/identity` to write this row — domains
-- do not import each other — and `internal/identity` is not this branch's to edit in any case. What
-- is left is a trigger here or a lazy create in `profiles`, and lazy loses: a provider who has not
-- opened the app has no row, so SHIP-153's queue cannot see them and `fleet`'s predicate cannot tell
-- "not verified" from "never asked". The trigger adds no column to `users`, changes no answer
-- `identity` can observe, and is owned entirely by this block — which is the same reading
-- `internal/fleet` already takes when its eligibility filter reads `users` in SQL without importing
-- the domain that owns it.
--
-- It fires on INSERT only. `000005_users_role_is_immutable` makes the role unchangeable after
-- registration, so there is no path by which a customer becomes a provider and needs a row later.
--
-- # The transition is guarded, and the guard is a function rather than a convention
--
-- CLAUDE.md's rule for job status — "never a settable field, all transitions pass one guarded
-- function" — is the shape this follows, and `000402` is the precedent. **It differs from that
-- precedent in one way and the difference is deliberate**: there, the guarded function is in Go and
-- the trigger checks that Go wrote a history row first, which means anything that is not Go — a
-- support query at a psql prompt, a repair script, `make verify`'s own fixtures — has to reproduce
-- the protocol by hand. `Docs/11` §3 records what that cost once already: the harness's `dispute_move`
-- fixture is a hand-written copy of the jobs protocol, and it carried an ambiguity nobody saw until a
-- job reached one status twice.
--
-- So here the guarded function is `provider_verification_decide`, in the database, and Go calls it.
-- There is exactly one implementation of a transition, and every caller — the domain service, a
-- future SHIP-154 reviewer, a fixture — is the same caller. Nothing about the domain's policy moves
-- into SQL by doing this, because Docs/04 §4 defines no transition table: any outcome may follow any
-- other, since a Suspended provider can be reinstated and a Rejected one can be Restricted after
-- clarification. What the function enforces is coherence — five states, an actor, a reason, and a
-- record written before the change — which is precisely what Docs/10 §3.1 puts in the database.

CREATE TABLE provider_verifications (
    -- One row per provider, and the provider *is* the key. There is no separate identifier because
    -- there is nothing a second row could mean: a provider has one verification standing, and an
    -- `id` column would make "which of these is current" a question somebody has to answer.
    provider_id uuid PRIMARY KEY,

    -- Docs/04 §4's five outcomes, stored exactly as the document writes them.
    --
    -- **DEFAULT 'Pending', and the default is the control.** §4 gives Pending as "information is
    -- incomplete or awaiting review", which is true of every provider the moment they register, and
    -- an INSERT that forgot this column must produce the state that can do least. The trigger below
    -- refuses any other state at creation anyway; the default is what makes the trigger's refusal
    -- unreachable in ordinary use rather than a thing every writer has to remember.
    state text NOT NULL DEFAULT 'Pending',

    -- When this provider first came under review, which is what SHIP-153 orders its queue by.
    --
    -- Deliberately *not* the same as the newest decision's clock: "oldest first" in a review queue
    -- means whoever has been waiting longest, and a provider whose state was corrected twice has not
    -- gone to the back of the line by being corrected.
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ck_provider_verifications_state CHECK (
        state IN ('Pending', 'Verified', 'Restricted', 'Rejected', 'Suspended')
    ),

    -- ON DELETE RESTRICT per Docs/10 §3.3, and with more force than usual: SHIP-171 pseudonymises an
    -- account rather than removing it, and a verification record is the evidence trail Docs/04 §1
    -- requires be kept of "every review, evidence item, decision, and expiry date".
    CONSTRAINT fk_provider_verifications_provider
        FOREIGN KEY (provider_id) REFERENCES users (id) ON DELETE RESTRICT
);

CREATE TRIGGER set_provider_verifications_updated_at
    BEFORE UPDATE ON provider_verifications
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- The queue SHIP-153 reads: everybody in one state, oldest first.
--
-- Partial on nothing, because all five states get queried — §5's first queue is the Pending ones and
-- §5's seventh is the expiring Verified ones — and a five-value column with a date is a small index
-- either way.
CREATE INDEX idx_provider_verifications_state ON provider_verifications (state, created_at);

-- Every decision that has ever been made about a provider's standing.
--
-- Append-only, the control `000003` puts on `audit_log`, `000401` on `job_status_history` and
-- `000601` on `milestones`. Docs/04 §1 requires that every review and decision be recorded and
-- Docs/04 §9 forbids ordinary administrators deleting audit history; a row that can be updated is a
-- rejection that can be rewritten as an approval after somebody complains.
CREATE TABLE provider_verification_decisions (
    id uuid PRIMARY KEY,

    provider_id uuid NOT NULL,

    -- Both ends, so the record is readable without replaying every row before it — `000401`'s shape,
    -- and the same reason: a history that stores only where it arrived cannot answer "what changed"
    -- for the row in front of you.
    from_state text NOT NULL,
    to_state   text NOT NULL,

    -- Who decided. `admin` names an `admin_users` row and `system` names nobody, which is the
    -- vocabulary `000401` established and the reason it has no foreign key: this record must outlive
    -- its subject, and an administrator who has left the company must still be nameable as the person
    -- who took the decision.
    --
    -- There is deliberately no `provider` actor. A provider submits evidence (SHIP-81b) and never
    -- decides their own standing; an actor value that could appear here would be a way for the
    -- subject of a review to be recorded as its reviewer.
    actor_type text NOT NULL,
    actor_id   uuid,

    -- Why, and it is required of every decision rather than only of an administrator's.
    --
    -- `job_status_history.reason` is nullable because most transitions are self-explanatory — a
    -- customer publishing their own job owes nobody an explanation. **No verification decision is
    -- self-explanatory.** Docs/04 §6.6 requires "the decision, actor, timestamp, reason, and evidence
    -- reference" for every moderation outcome, §4 requires a rejection's reason be communicable to
    -- the provider, and the one decision that looks automatic — the record's own creation — has the
    -- plainest reason of all and can say so.
    reason text NOT NULL,

    decided_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ck_provider_verification_decisions_from CHECK (
        from_state IN ('Pending', 'Verified', 'Restricted', 'Rejected', 'Suspended')
    ),
    CONSTRAINT ck_provider_verification_decisions_to CHECK (
        to_state IN ('Pending', 'Verified', 'Restricted', 'Rejected', 'Suspended')
    ),

    -- A decision that changes nothing is not a decision. It is also what makes the guard below safe
    -- to re-run: a row can only ever authorise the one move it describes.
    CONSTRAINT ck_provider_verification_decisions_moves CHECK (from_state <> to_state),

    CONSTRAINT ck_provider_verification_decisions_actor_type CHECK (
        actor_type IN ('admin', 'system')
    ),

    -- An account-backed actor names its account; 'system' must not claim one. `000401`'s rule.
    CONSTRAINT ck_provider_verification_decisions_actor_id CHECK (
        (actor_type =  'system' AND actor_id IS NULL) OR
        (actor_type <> 'system' AND actor_id IS NOT NULL)
    ),

    CONSTRAINT ck_provider_verification_decisions_reason CHECK (
        length(btrim(reason)) BETWEEN 1 AND 500
    ),

    CONSTRAINT fk_provider_verification_decisions_provider
        FOREIGN KEY (provider_id) REFERENCES provider_verifications (provider_id) ON DELETE RESTRICT
);

-- One provider's history, newest first, which is how a reviewer reads it and how the domain finds
-- the reason behind the current state.
CREATE INDEX idx_provider_verification_decisions_provider
    ON provider_verification_decisions (provider_id, decided_at DESC);

CREATE OR REPLACE FUNCTION provider_verification_decisions_is_append_only() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'provider_verification_decisions is append-only: % is not permitted', TG_OP
        USING HINT = 'A decision that turned out to be wrong is another decision. Rewriting one in '
                     'place removes the evidence that the first was ever taken (Docs/04 §9).';
END;
$$;

CREATE TRIGGER provider_verification_decisions_no_update
    BEFORE UPDATE ON provider_verification_decisions
    FOR EACH ROW EXECUTE FUNCTION provider_verification_decisions_is_append_only();

CREATE TRIGGER provider_verification_decisions_no_delete
    BEFORE DELETE ON provider_verification_decisions
    FOR EACH ROW EXECUTE FUNCTION provider_verification_decisions_is_append_only();

-- --- the guard -------------------------------------------------------------------------------

-- A verification record is created Pending and reaches every other state by transition.
--
-- `000402`'s `jobs_starts_as_a_draft`, for the same reason: a row inserted at 'Verified' has been
-- verified by nobody, and there would be no decision recording who decided it or why.
CREATE OR REPLACE FUNCTION provider_verification_starts_as_pending() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.state IS DISTINCT FROM 'Pending' THEN
        RAISE EXCEPTION 'a verification record is created Pending, not %', NEW.state
            USING HINT = 'Insert the record, then move it with provider_verification_decide() '
                         '(Docs/04 §4, SHIP-81a).';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER provider_verification_starts_as_pending
    BEFORE INSERT ON provider_verifications
    FOR EACH ROW EXECUTE FUNCTION provider_verification_starts_as_pending();

-- No change to `state` that was not made by `provider_verification_decide` below.
--
-- The mechanism is `000402`'s: a transaction-local setting names the decision row, the trigger checks
-- that the row exists and describes exactly this move, and `set_config(…, true)` outside a
-- transaction lasts only for the statement that set it — so a caller holding a pool rather than a
-- transaction is refused here rather than committing a state change whose record might not follow.
--
-- Three guarantees fall out of one condition: the change went through the guard, because nothing else
-- writes that row; it is recorded with an actor and a reason, because those are NOT NULL columns; and
-- it happened in a transaction, because the setting is transaction-local.
CREATE OR REPLACE FUNCTION provider_verification_change_is_guarded() RETURNS trigger
    LANGUAGE plpgsql
AS $$
DECLARE
    claimed  text;
    recorded uuid;
BEGIN
    -- This trigger has an opinion about one column. `updated_at` passes straight through, as would
    -- anything a later migration adds.
    IF NEW.state IS NOT DISTINCT FROM OLD.state THEN
        RETURN NEW;
    END IF;

    claimed := current_setting('shipper.provider_verification_decision', true);

    IF claimed IS NULL OR claimed = '' THEN
        RAISE EXCEPTION
            'provider verification state is not a settable field: % may not move from % to % by direct UPDATE',
            NEW.provider_id, OLD.state, NEW.state
            USING HINT = 'Every transition passes provider_verification_decide() (Docs/04 §4, SHIP-81a).';
    END IF;

    BEGIN
        recorded := claimed::uuid;
    EXCEPTION WHEN invalid_text_representation THEN
        RAISE EXCEPTION 'shipper.provider_verification_decision is %, which is not a decision id', claimed;
    END;

    IF NOT EXISTS (
        SELECT 1
        FROM provider_verification_decisions d
        WHERE d.id          = recorded
          AND d.provider_id = NEW.provider_id
          AND d.from_state  = OLD.state
          AND d.to_state    = NEW.state
    ) THEN
        RAISE EXCEPTION
            'provider verification change is unrecorded: no decision % says % moved from % to %',
            recorded, NEW.provider_id, OLD.state, NEW.state
            USING HINT = 'Write the decision in the same transaction, before the update (SHIP-81a).';
    END IF;

    RETURN NEW;
END;
$$;

CREATE TRIGGER provider_verification_change_is_guarded
    BEFORE UPDATE ON provider_verifications
    FOR EACH ROW EXECUTE FUNCTION provider_verification_change_is_guarded();

-- **The one guarded function.** Records the decision, names it to the trigger, and moves the state,
-- in that order and in one statement's worth of atomicity.
--
-- It takes the row's lock first, so two administrators deciding at once serialise rather than
-- interleave — without it both would read the same `from_state` and the second would write a decision
-- describing a move that had already happened.
--
-- What it deliberately does *not* do is judge whether the move is sensible. Docs/04 §4 defines no
-- transition table: a Suspended provider is reinstated, a Rejected one is Restricted after
-- clarification, and a Verified one whose insurance lapses goes back to Pending. The only refusals
-- here are coherence — no such provider, and a move to the state the provider is already in.
CREATE OR REPLACE FUNCTION provider_verification_decide(
    p_provider   uuid,
    p_to         text,
    p_actor_type text,
    p_actor_id   uuid,
    p_reason     text
) RETURNS uuid
    LANGUAGE plpgsql
AS $$
DECLARE
    current_state text;
    decision      uuid;
BEGIN
    SELECT state INTO current_state
    FROM provider_verifications
    WHERE provider_id = p_provider
    FOR UPDATE;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'no verification record for provider %', p_provider
            USING ERRCODE = 'no_data_found';
    END IF;

    decision := gen_random_uuid();

    INSERT INTO provider_verification_decisions
        (id, provider_id, from_state, to_state, actor_type, actor_id, reason)
    VALUES
        (decision, p_provider, current_state, p_to, p_actor_type, p_actor_id, p_reason);

    -- Transaction-local, which is what makes it useless to anybody holding a pool: the trigger below
    -- reads it and refuses when it is unset.
    PERFORM set_config('shipper.provider_verification_decision', decision::text, true);

    UPDATE provider_verifications SET state = p_to WHERE provider_id = p_provider;

    -- Cleared immediately, so one decision authorises exactly one update. Leaving it set would let a
    -- second UPDATE in the same transaction ride on the first decision's authority — and the trigger
    -- would not catch it if the second move happened to describe the same pair.
    PERFORM set_config('shipper.provider_verification_decision', '', true);

    RETURN decision;
END;
$$;

COMMENT ON FUNCTION provider_verification_decide(uuid, text, text, uuid, text) IS
    'The one guarded transition for a provider verification state: records the decision with its actor and reason, then moves the state (Docs/04 §4, SHIP-81a).';
COMMENT ON FUNCTION provider_verification_change_is_guarded() IS
    'Refuses any change to provider_verifications.state that is not described by a decision row written in the same transaction by provider_verification_decide().';

-- --- the record every provider has -------------------------------------------------------------

-- A provider account gets its verification record at the moment it exists.
--
-- AFTER INSERT rather than BEFORE, because the row this writes has a foreign key to the row being
-- inserted. `WHEN (NEW.role = 'provider')` keeps it off every customer registration, which is most of
-- them; `000005` makes the role immutable, so a row that qualifies now qualifies for ever and one
-- that does not never will.
CREATE OR REPLACE FUNCTION provider_verification_on_registration() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO provider_verifications (provider_id) VALUES (NEW.id)
    ON CONFLICT (provider_id) DO NOTHING;
    RETURN NULL;
END;
$$;

CREATE TRIGGER provider_verification_on_registration
    AFTER INSERT ON users
    FOR EACH ROW WHEN (NEW.role = 'provider')
    EXECUTE FUNCTION provider_verification_on_registration();

COMMENT ON FUNCTION provider_verification_on_registration() IS
    'Gives every provider account a Pending verification record at registration, so that Pending is a state a provider is in rather than the absence of one (SHIP-81a).';

-- The providers who registered before this migration.
--
-- **They are backfilled Pending rather than Verified**, and the difference is the whole ticket.
-- Nobody has reviewed their licence, registration, insurance or ABN, so Verified would be a
-- statement the platform has no evidence for — recorded in the one table Docs/04 §1 requires be an
-- evidence trail. The visible consequence is that every existing provider stops being eligible to
-- bid until somebody decides otherwise, which is Docs/04 §1's first principle read literally: "do not
-- allow a provider to bid until baseline checks are complete."
INSERT INTO provider_verifications (provider_id)
SELECT id FROM users WHERE role = 'provider'
ON CONFLICT (provider_id) DO NOTHING;

COMMENT ON TABLE provider_verifications IS
    'Docs/04 §4''s five verification outcomes for one provider, and the only fact internal/fleet''s eligibility filter reads about verification (SHIP-81a). State is not a settable field.';
COMMENT ON TABLE provider_verification_decisions IS
    'Every decision ever taken about a provider''s verification standing, with its actor and its reason. Append-only (Docs/04 §6, §9).';
COMMENT ON COLUMN provider_verifications.state IS
    'Pending, Verified, Restricted, Rejected or Suspended (Docs/04 §4). Only Verified may bid. Changed only by provider_verification_decide().';
