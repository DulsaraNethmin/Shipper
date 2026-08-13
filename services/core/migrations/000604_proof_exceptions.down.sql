-- Reverses SHIP-116.
--
-- `IF EXISTS` throughout, for the reason 000603's down migration gives at length: SHIP-15g's guard
-- refuses a migration numbered below the database's current version, so a delivery branch applies
-- its work with `make migrate-down n=all` followed by `make migrate-up` — which runs this file
-- before its own up migration has ever run.
--
-- # It is one DO block, and that is not decoration
--
-- Restoring 000603's four NOT NULLs is the whole difficulty. A row recorded through the exception
-- path has no representation in the reversed schema at all — it holds a reason and no object — so
-- `SET NOT NULL` fails against it, and the down migration would stop halfway with the constraint
-- dropped and the column still there. **The rows are deleted**, which is the honest reversal: this
-- migration is what made them writable, and reversing it makes them unwritable again.
--
-- `proofs` is append-only (000603), so the delete has to disable that trigger and put it back. That
-- is the one place in this repository where the append-only rule is deliberately stepped around,
-- and it is a migration reversing the feature that created the rows rather than anything reachable
-- from a request. The photographs are untouched.
--
-- `to_regclass` guards the whole block because a `DROP TRIGGER … IF EXISTS` still fails when the
-- *table* is absent, which is exactly the down-before-up case above.
DO $$
BEGIN
    IF to_regclass('public.proofs') IS NULL THEN
        RETURN;
    END IF;

    EXECUTE 'DROP INDEX IF EXISTS idx_proofs_exception';
    EXECUTE 'ALTER TABLE proofs DROP CONSTRAINT IF EXISTS ck_proofs_photograph_or_exception';
    EXECUTE 'ALTER TABLE proofs DROP CONSTRAINT IF EXISTS ck_proofs_exception_reason';

    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public' AND table_name = 'proofs' AND column_name = 'exception_reason'
    ) THEN
        EXECUTE 'ALTER TABLE proofs DISABLE TRIGGER proofs_no_delete';
        EXECUTE 'DELETE FROM proofs WHERE exception_reason IS NOT NULL';
        EXECUTE 'ALTER TABLE proofs ENABLE TRIGGER proofs_no_delete';
        EXECUTE 'ALTER TABLE proofs DROP COLUMN exception_reason';
    END IF;

    EXECUTE 'ALTER TABLE proofs
                 ALTER COLUMN object_key SET NOT NULL,
                 ALTER COLUMN content_type SET NOT NULL,
                 ALTER COLUMN content_length SET NOT NULL,
                 ALTER COLUMN etag SET NOT NULL';

    EXECUTE $c$COMMENT ON COLUMN proofs.object_key IS
        'The key in the private bucket. Unique: one object is proof of at most one milestone, so a photograph cannot become evidence for a delivery it was not taken at.'$c$;
END;
$$;
