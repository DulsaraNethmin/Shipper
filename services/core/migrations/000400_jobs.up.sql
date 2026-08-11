-- SHIP-56: the job, and the twelve statuses of Docs/02 §1.
--
-- This table is the centre of the product, and it starts deliberately narrow. It carries who
-- owns the job and what state it is in, and nothing else — the same discipline 000100 applied
-- to device_sessions, and for the same reason: every later ticket in M2 brings behaviour and
-- the columns that behaviour needs, and guessing at those now would be guessing at designs
-- nobody has written.
--
--   SHIP-58  goods category, from reference data rather than a constant
--   SHIP-60  pickup and drop-off addresses with resolved coordinates
--   SHIP-62  the draft's own fields — dimensions, weight, vehicle requirement, notes
--   SHIP-67  the customer's maximum budget, numeric(12,2) and never serialised to a provider
--   SHIP-68  expiry: the earlier of fourteen days or the pickup date passing
--
-- What SHIP-56 does have to get right is the status column, because everything else in the
-- lifecycle is defined in terms of it.

CREATE TABLE jobs (
    id          uuid        PRIMARY KEY,

    -- The customer who owns the job. There is no CHECK that this account's role is 'customer',
    -- because a foreign key cannot see another table's column — SHIP-61 enforces it where the
    -- draft is created, and the audit trail is what catches it if that is ever wrong.
    customer_id uuid        NOT NULL,

    -- One of the twelve in Docs/02 §1, stored exactly as that document writes it: spaces and
    -- sentence case included (Docs/10 §3.4). Storing the document's own strings is what lets the
    -- Go, Dart and TypeScript copies be diffed against the document rather than against each
    -- other.
    --
    -- text with a CHECK rather than a PostgreSQL ENUM type, per Docs/10 §3.4: ALTER TYPE … ADD
    -- VALUE cannot run in a transaction block alongside its use, and removing or reordering a
    -- value means recreating the type and every column referencing it. A CHECK constraint is an
    -- ordinary migration.
    --
    -- DEFAULT 'Draft' because that is where Docs/02 §2 starts every job, and because it means an
    -- INSERT never has a reason to name the column. 000402 refuses any INSERT that arrives at
    -- another status, so the default is the only value creation can produce.
    status      text        NOT NULL DEFAULT 'Draft',

    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT ck_jobs_status CHECK (status IN (
        'Draft',
        'Open',
        'Negotiating',
        'Awarded',
        'Driver assigned',
        'En route to pickup',
        'Picked up',
        'In transit',
        'Delivered',
        'Completed',
        'Cancelled',
        'Disputed'
    )),

    -- ON DELETE RESTRICT per Docs/10 §3.3. SHIP-171 pseudonymises an account rather than
    -- removing it, so nothing should be deleting a user row at all — and a cascade here would
    -- destroy the commercial record Docs/05 §3.1 requires retaining.
    CONSTRAINT fk_jobs_customer
        FOREIGN KEY (customer_id) REFERENCES users (id) ON DELETE RESTRICT
);

-- Every foreign key is indexed (Docs/10 §3.3), and this one is also the read path for SHIP-66's
-- customer job list, which is ordered newest first. One index serves both.
CREATE INDEX idx_jobs_customer ON jobs (customer_id, created_at DESC);

CREATE TRIGGER jobs_set_updated_at
    BEFORE UPDATE ON jobs
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

COMMENT ON TABLE jobs IS
    'One delivery job. Status is never a settable field: 000402 refuses any change that is not made through the guard (Docs/02 §2, SHIP-57).';
COMMENT ON COLUMN jobs.status IS
    'One of the twelve statuses in Docs/02 §1, stored exactly as that document writes them.';
