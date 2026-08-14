-- SHIP-123: a delivered milestone carries who received it and what was left where.
--
-- **This closes the half of Docs/01 §4.4 that SHIP-118 could not.** That ticket's own migration
-- (000605) named this one while doing it: "A delivered job also requires a recipient name and a
-- delivery note, and no column holds either yet — SHIP-123 is the ticket whose *Done when* names
-- them, and it depends on SHIP-118." Docs/11 §4 has carried the gap since, and Docs/02 §3 repeats
-- the requirement while naming Docs/01 §4.4 as authoritative for the field set.
--
-- Docs/01 §4.4 lists four things a delivered job requires: recipient name, delivery timestamp,
-- delivery note, and photo proof. Two of the four were already here — the timestamp is
-- `actor_recorded_at` and the proof is 000605's deferred trigger over `proofs`. These are the
-- other two.
--
-- # Why they are columns on `milestones` rather than a table of their own
--
-- A delivery is not a row somewhere that a milestone points at; it is **what an actor claimed**, and
-- `milestones` is the table of claims. Recipient and note are two more facts about one claim, they
-- are written in the same statement, they are append-only for the same reason the rest of the row
-- is, and they are read by every caller that already reads the milestone. A `deliveries` table
-- beside it would need its own key, its own append-only triggers, its own join on every read, and a
-- rule keeping it in step with a milestone that can legitimately be recorded twice.
--
-- **They are not `reason`.** Docs/11 §4 already ruled that out and it is worth keeping here: `reason`
-- is the actor's optional note on *any* milestone — Docs/02 §5's "the gate was locked and I am
-- returning at four" — and making it mean a second, specific, mandatory thing on one milestone value
-- is the one-column-two-meanings Docs/10 §3.3 refuses. A delivery note and a milestone reason can
-- both be present on the same row and mean different things.
--
-- # Required on 'Delivered' and refused on everything else, in one CHECK
--
-- Docs/11 §4 sets the instruction — "adds both columns and makes them required for Delivered" — and
-- the constraint is written in both directions because half of it would be the weaker half. Required
-- on 'Delivered' is what the document asks for. **Absent everywhere else is what stops the columns
-- becoming general-purpose**: a caller that started attaching a recipient name to a `picked_up` would
-- be recording a handover that did not happen, and nothing else in the schema would notice.
--
-- A `CASE` rather than a pair of implications, because three-valued logic is where a constraint like
-- this goes quietly wrong: `milestone <> 'Delivered' OR recipient_name IS NOT NULL` is true when the
-- milestone is NULL, and `milestone` is NOT NULL here only because 000601 said so. The `CASE` states
-- both branches and has no third one.
--
-- # This binds two existing writers as well as the new one, and that is the point
--
-- `POST /v1/jobs/{id}/milestones` and `POST /v1/driver/jobs/{id}/milestones` both record 'Delivered'
-- today. Docs/01 §4.4 says "delivered jobs require", not "jobs delivered through the driver portal
-- require", so both paths supply the fields from this migration onwards. That is a wider change than
-- the driver portal's form and it is the coherent one: a rule that held for one of two entry points
-- would be a rule the other one is one ticket away from breaking, which is the argument 000605 makes
-- about itself.
--
-- # What this deliberately does not do
--
-- **It does not make either field an exception path.** Docs/01 §4.4 gives three reasons a
-- *photograph* can be impossible and gives none for a name or a note, so there is no
-- `recipient_name_exception` here and there should not be: a driver can always type what they see,
-- and "left at the front door" is a delivery note. What Docs/01 §4.4 does not contemplate is an
-- unattended delivery with no recipient to name at all, and Docs/11 §9 records that as a question
-- for operations rather than resolving it in a constraint.
--
-- **It backfills nothing and can refuse nothing already written.** A CHECK constraint added with
-- ALTER TABLE validates existing rows, so this migration fails on any database that already holds a
-- 'Delivered' milestone — which is every database `make verify` has ever run against. It is
-- therefore added NOT VALID and validated immediately afterwards on a table that has been
-- backfilled first. See below.

ALTER TABLE milestones
    ADD COLUMN recipient_name text,
    ADD COLUMN delivery_note  text;

-- Backfill, and it is a deliberate lie of the least harmful available kind.
--
-- Every 'Delivered' milestone recorded before this migration was recorded by a platform that never
-- asked for these fields, so there is no true value to write and no way to obtain one — the driver
-- has gone home and the table is append-only. The choice is between refusing to migrate any database
-- that has ever recorded a delivery, and writing a marker that says plainly that the fact was not
-- captured.
--
-- The marker is the second, and it is phrased so that nobody reading a support screen mistakes it
-- for something a driver typed. It is not NULL, because NULL is what the constraint below uses to
-- mean "this is not a delivered milestone" and a delivered row carrying it would be a row the
-- constraint cannot describe.
UPDATE milestones
   SET recipient_name = '(not captured — recorded before SHIP-123)',
       delivery_note  = '(not captured — recorded before SHIP-123)'
 WHERE milestone = 'Delivered'
   AND recipient_name IS NULL;

ALTER TABLE milestones
    ADD CONSTRAINT ck_milestones_delivery_details CHECK (
        CASE WHEN milestone = 'Delivered'
             THEN recipient_name IS NOT NULL AND length(btrim(recipient_name)) BETWEEN 1 AND 120
              AND delivery_note  IS NOT NULL AND length(btrim(delivery_note))  BETWEEN 1 AND 500
             ELSE recipient_name IS NULL AND delivery_note IS NULL
        END
    );

COMMENT ON COLUMN milestones.recipient_name IS
    'Who took the goods, as the actor typed it. Required on Delivered and refused on every other milestone (Docs/01 §4.4, SHIP-123).';
COMMENT ON COLUMN milestones.delivery_note IS
    'What was left where, as the actor typed it. Required on Delivered and refused on every other milestone. Distinct from milestones.reason, which is the optional note any milestone may carry.';
COMMENT ON CONSTRAINT ck_milestones_delivery_details ON milestones IS
    'Docs/01 §4.4''s field set for a delivered job, in both directions: required on Delivered, and absent elsewhere so the columns cannot become a general-purpose pair somebody attaches to a pickup.';
