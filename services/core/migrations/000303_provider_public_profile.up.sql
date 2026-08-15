-- SHIP-79a: who a provider is, as a customer comparing offers may be told. Docs/01 §4.2's
-- "Register as an individual or business, complete profile details", which nothing until now stored.
--
-- # The gap this closes, measured rather than assumed
--
-- Docs/01 §4.3 has a customer compare "price, timing, provider profile, vehicle, and declared
-- capability", and SHIP-102a served the profile clause in reduced form because there was nothing to
-- serve. Measured before this migration: `internal/profiles` holds `doc.go` and nothing else; the
-- only provider profile in the service is `fleet.Profile`, whose two fields are the service area and
-- the specialties — **precisely what SHIP-102a's *Done when* forbids showing a customer**, because
-- they are a competitor's map of the market; and there is no trading name, no rating and no
-- completed-job count anywhere in the schema. A customer choosing between two offers could be told
-- a UUID, whether the account had confirmed an email address, and the date it was created.
--
-- # Two columns, and the two that were considered and declined
--
-- **`display_name`** is the whole point: something to put on the screen instead of an identifier.
-- **`operates_as`** is Docs/01 §4.2's own distinction — an individual and a business are different
-- propositions to a customer with a piano, and the provider is asked which they are at registration
-- in that document's words.
--
-- Declined, both named in Docs/09's SHIP-79a note:
--
--   * **A rating.** "A rating implies a review mechanism nobody has specified" — there is no review,
--     no moderation of one, and no policy position on disputes affecting it. A column would be an
--     invitation to fill it from something.
--   * **A completed-job count.** "A figure the platform can derive", and deriving it is the problem:
--     the definition is `Docs/02`'s (Completed? Delivered? auto-completed after 72 hours?), it lives
--     in another domain's tables, and `fleet` reaching into `jobs` for a *decoration* is a much
--     weaker case than reaching into it for the eligibility filter. It is additive whenever somebody
--     owns the definition.
--
-- # A row per provider, unlike 000301's two tables
--
-- 000301 stores sets and deliberately has no parent row — "this provider has declared nothing" is
-- the absence of rows. These are not sets: a provider has one name and one form. A row here means
-- "this provider has said who they are", and its absence means they have not, which is the same
-- one-representation rule pointed the other way.
--
-- The primary key **is** the provider, so there is no separate id and no way to hold two profiles
-- for one account. `provider_service_areas` needs a surrogate key because it holds many rows per
-- provider; this holds one, and a surrogate here would be a second thing to join on.
--
-- No CHECK that the account's role is 'provider', for the reason 000300, 000301 and 000400 all give:
-- a foreign key cannot see another table's column, and the domain enforces it where the declaration
-- is made. ON DELETE RESTRICT per Docs/10 §3.3 — SHIP-171 pseudonymises rather than deletes.

CREATE TABLE provider_profiles (
    -- The provider, and the primary key. One profile per account by construction.
    provider_id  uuid        PRIMARY KEY,

    -- What a customer is shown instead of an identifier: a trading name, or the name an individual
    -- operates under.
    --
    -- **Not a person's legal name and not verified to be anything.** Docs/04 §3's document review is
    -- what establishes who somebody actually is; this is what they trade as, which is a declaration
    -- exactly like the specialties in 000301 — it claims something and grants nothing.
    --
    -- The length ceiling is here rather than in the validator alone because a text column with no
    -- bound is a column somebody stores a paragraph in. Docs/10 §3.1's split puts coherence in the
    -- database and policy in the domain: "not empty, and not a paragraph" is coherence; whether 80
    -- is the right number for a screen is the validator's, and it agrees with this one.
    display_name text        NOT NULL,

    -- 'individual' or 'business', in Docs/01 §4.2's own words.
    --
    -- text with a CHECK rather than a PostgreSQL ENUM, per Docs/10 §3.4: ALTER TYPE … ADD VALUE
    -- cannot run in a transaction block alongside its use.
    operates_as  text        NOT NULL,

    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    -- **`btrim` with both arguments, deliberately.** PostgreSQL's one-argument form strips spaces
    -- and not tabs or newlines, which `ck_admin_notes_body` was written with and a test caught: a
    -- body of a single newline satisfied the constraint while the Go service refused the same value,
    -- and the two disagreed about what "empty" means. The character set is spelled out here so the
    -- constraint and `strings.TrimSpace` cannot part company.
    CONSTRAINT ck_provider_profiles_display_name CHECK (
        btrim(display_name, E' \t\r\n') <> '' AND length(display_name) <= 80
    ),

    CONSTRAINT ck_provider_profiles_operates_as CHECK (
        operates_as IN ('individual', 'business')
    ),

    CONSTRAINT fk_provider_profiles_provider
        FOREIGN KEY (provider_id) REFERENCES users (id) ON DELETE RESTRICT
);

-- The `updated_at` trigger 000001 defines and every mutable table wears.
--
-- **Not optional and not the writer's job.** A table with the column and no trigger looks entirely
-- healthy — the column exists, is NOT NULL, and defaults correctly on insert — and simply never
-- records a change, which nobody notices until somebody asks when a profile was last touched and is
-- told the creation time. `TestEveryMutableTableHasItsUpdatedAtTrigger` sweeps for exactly this and
-- caught this table without it, which is the sweep working rather than a near miss.
CREATE TRIGGER provider_profiles_set_updated_at
    BEFORE UPDATE ON provider_profiles
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- No index beyond the primary key, and that is a decision rather than an omission.
--
-- Every read of this table is by provider identifier — one for the provider's own profile screen,
-- and one `= ANY($1)` over the providers a page of offers named — and the primary key serves both.
-- Docs/10 §3.3's "every foreign key is indexed" is satisfied by the primary key being the foreign
-- key. Searching profiles by name is an administrator's question (SHIP-151's shape) and belongs with
-- whoever builds that read, against the access pattern they actually have.

COMMENT ON TABLE provider_profiles IS
    'Who a provider trades as, as a customer comparing their offer is shown it (SHIP-79a). '
    'A declaration, not a verified identity — Docs/04 §3 owns that.';

COMMENT ON COLUMN provider_profiles.display_name IS
    'The trading name a customer sees instead of an identifier. Declared, never verified.';

COMMENT ON COLUMN provider_profiles.operates_as IS
    'individual or business, in Docs/01 §4.2''s words.';
