-- SHIP-63: the declaration a customer makes when they publish.
--
-- Docs/04 §2's third row: "Terms and goods declaration | Accept at job publication | **Required
-- for every job**". Every job, which is what decides the shape of this column.
--
-- # Why it is on the job and not on the account
--
-- An acceptance recorded against the account would be answered once at registration and remembered
-- for ever, and Docs/04 §2 asks for the opposite: the declaration is about *these goods*, made by a
-- customer who has just described them, and it is what the platform relies on if what turns up at
-- the pickup is not what the job said. A per-account flag could not support that — it would prove
-- the customer once agreed to something, on a date unrelated to this job.
--
-- # A timestamp, not a boolean
--
-- The same reasoning 000002 gives for users.email_verified_at: "a boolean answers 'is it
-- verified'; a timestamp answers that and also 'since when', which is what a support conversation
-- and an audit trail both actually ask". Which terms were in force when the customer accepted is a
-- question about a date.
--
-- # Nullable, because a draft has not published
--
-- NULL means "not yet published", and it stays NULL for every job that never is. It is set in the
-- same transaction as the Draft to Open transition and is not cleared afterwards: a job that
-- returns to Open under Docs/02 §6.2 was published once and the acceptance that published it
-- stands.
--
-- There is deliberately no constraint tying it to the status. The tempting one — "Open implies
-- this is set" — would be false for every job that reached Open before this migration, and there
-- are none today only because nothing has ever published. It would also put a second copy of the
-- publication rule in the schema, and 000402's header gives the argument against exactly that:
-- the transition guard is one place, and a rule restated in SQL is a rule that drifts.

ALTER TABLE jobs
    ADD COLUMN terms_accepted_at timestamptz;

COMMENT ON COLUMN jobs.terms_accepted_at IS
    'When the customer accepted the terms and the goods declaration, for this job (Docs/04 §2, SHIP-63). Per job rather than per account: the declaration is about these goods. NULL means the job has never been published.';
