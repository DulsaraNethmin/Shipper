package fleet

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// postgresStore is this domain's persistence, concrete and unexported.
//
// There is no repository interface, per Docs/10 §2.2 and Docs/06 §4.1. The guarantee this domain
// rests on hardest is PostgreSQL's — uq_vehicles_provider_registration is a *partial* unique index,
// which is what lets a retired plate be added again while a second live row for one truck is
// refused — and an interface designed to keep the database swappable would hide exactly the
// mechanism that makes the rule correct. A mock would accept the duplicate this store exists to
// have refused.
//
// Every method takes a db.Runner as its first argument after ctx, so the caller decides whether the
// work stands alone or joins a transaction it already opened (Docs/10 §3.2).
type postgresStore struct{}

// vehicleColumns is every column of a vehicle, in the order [scanVehicle] reads them.
//
// The nullable text, integer and numeric columns are coalesced in SQL rather than scanned into
// pointers, and that is not laziness: the domain's representation of "not stated" for those fields
// is already the zero value, and the constraints in 000300 make the zero value one the column
// cannot hold — ck_vehicles_load_length_cm refuses a dimension that is not positive — so 0 and NULL
// cannot be confused in either direction.
//
// deactivated_at is the exception, because PostgreSQL's NULL has no representation in time.Time and
// because the distinction it carries is the whole of [Vehicle.Active].
const vehicleColumns = `
	id, provider_id, registration, vehicle_type,
	COALESCE(make, ''), COALESCE(model, ''),
	COALESCE(max_weight_kg, 0),
	COALESCE(load_length_cm, 0), COALESCE(load_width_cm, 0), COALESCE(load_height_cm, 0),
	deactivated_at, created_at, updated_at`

// scanVehicle reads one row of [vehicleColumns].
//
// One function rather than five copies of a thirteen-argument Scan call. A column added to
// vehicleColumns and not here is a scan mismatch at the first call, which is the failure worth
// having: the alternative is five call sites that have to be found and edited together.
func scanVehicle(row pgx.Row) (Vehicle, error) {
	var (
		v             Vehicle
		deactivatedAt *time.Time
	)

	if err := row.Scan(
		&v.ID, &v.ProviderID, &v.Registration, &v.Type,
		&v.Make, &v.Model,
		&v.Capacity.MaxWeightKg,
		&v.Capacity.LengthCm, &v.Capacity.WidthCm, &v.Capacity.HeightCm,
		&deactivatedAt, &v.CreatedAt, &v.UpdatedAt,
	); err != nil {
		return Vehicle{}, err
	}

	if deactivatedAt != nil {
		v.DeactivatedAt = *deactivatedAt
	}
	return v, nil
}

// The NULL conversions.
//
// The database distinguishes "no value" from "the zero value" and Go does not, so the conversion
// happens at the boundary in both directions rather than leaving columns that are never NULL and
// constraints that never fire.
func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullFloat(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}

// writableColumns names the columns [writableArgs] supplies, in the same order.
//
// deactivated_at is deliberately absent. It moves only through [postgresStore.setDeactivatedAt] and
// [postgresStore.clearDeactivatedAt], so an ordinary edit cannot take a vehicle out of service or
// bring it back by accident — the same discipline that keeps `status` out of jobs' draftColumns.
const writableColumns = `
	registration, vehicle_type, make, model,
	max_weight_kg, load_length_cm, load_width_cm, load_height_cm`

// writableArgs is every value those columns take, in the order insert and update write them. One
// list, so the two statements cannot drift into disagreeing about a column.
func writableArgs(v Vehicle) []any {
	return []any{
		v.Registration, string(v.Type), nullText(v.Make), nullText(v.Model),
		nullFloat(v.Capacity.MaxWeightKg),
		nullInt(v.Capacity.LengthCm), nullInt(v.Capacity.WidthCm), nullInt(v.Capacity.HeightCm),
	}
}

// isProvider reports whether the account exists and is a provider account.
//
// This domain reading `users` is sanctioned rather than a boundary crossed: the table is in the
// shared migration block precisely because it is read across the whole service (Docs/10 §9.2), and
// `vehicles.provider_id` already references it. What is not sanctioned — and is not done — is
// importing internal/identity to ask.
//
// A missing account reports false rather than an error. The only caller has a token naming that
// account, so the row missing means it has been removed underneath a live session, and "you may not
// add a vehicle" is a truthful answer to that.
func (postgresStore) isProvider(ctx context.Context, r db.Runner, id uuid.UUID) (bool, error) {
	const q = `SELECT role = 'provider' FROM users WHERE id = $1`

	var provider bool
	err := r.QueryRow(ctx, q, id).Scan(&provider)
	switch {
	case errors.Is(err, db.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("fleet: read the role of %s: %w", id, err)
	}
	return provider, nil
}

// insert creates a vehicle, in service.
//
// deactivated_at is not named, so a vehicle is always added active. There is no request shape that
// could ask for anything else, which is what "deactivation is an intent rather than a field" means
// at the moment a vehicle comes into existence.
func (postgresStore) insert(ctx context.Context, r db.Runner, v Vehicle) (Vehicle, error) {
	const q = `
		INSERT INTO vehicles (id, provider_id, ` + writableColumns + `)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING ` + vehicleColumns

	args := append([]any{v.ID, v.ProviderID}, writableArgs(v)...)

	created, err := scanVehicle(r.QueryRow(ctx, q, args...))
	if err != nil {
		return Vehicle{}, fmt.Errorf("fleet: add a vehicle for %s: %w", v.ProviderID, duplicate(err))
	}
	return created, nil
}

// update writes every editable column at once, from a vehicle the caller has already locked.
//
// Not a dynamically built statement naming only the changed columns, and that is deliberate. The
// caller holds the row under FOR UPDATE and has applied the patch to the value it read, so writing
// all of them is writing what is already there — and one fixed statement cannot suffer the defect a
// built one invites, where a column is omitted from the SET list and its parameter is not, silently
// shifting every value after it by one.
func (postgresStore) update(ctx context.Context, r db.Runner, v Vehicle) (Vehicle, error) {
	const q = `
		UPDATE vehicles SET
			registration = $2, vehicle_type = $3, make = $4, model = $5,
			max_weight_kg = $6, load_length_cm = $7, load_width_cm = $8, load_height_cm = $9
		WHERE id = $1
		RETURNING ` + vehicleColumns

	args := append([]any{v.ID}, writableArgs(v)...)

	updated, err := scanVehicle(r.QueryRow(ctx, q, args...))
	switch {
	case errors.Is(err, db.ErrNoRows):
		// The row was locked a few statements ago. Nothing in this platform deletes a vehicle,
		// so this is reported rather than assumed away.
		return Vehicle{}, fmt.Errorf("fleet: %s vanished mid-edit: %w", v.ID, ErrVehicleNotFound)
	case err != nil:
		return Vehicle{}, fmt.Errorf("fleet: update %s: %w", v.ID, duplicate(err))
	}
	return updated, nil
}

// lockVehicle reads a vehicle and holds the row for the rest of the transaction.
//
// FOR UPDATE rather than a plain read, because every write in this domain is a read-modify-write and
// two of them arriving at once is not hypothetical: a provider editing on the phone while the same
// account is open on a second device. In READ COMMITTED, the second transaction blocks here and
// then re-reads the row the first one committed, so its own ownership check and its own view of
// deactivated_at run against the vehicle as it actually is.
func (postgresStore) lockVehicle(ctx context.Context, r db.Runner, id uuid.UUID) (Vehicle, error) {
	const q = `SELECT ` + vehicleColumns + ` FROM vehicles WHERE id = $1 FOR UPDATE`

	v, err := scanVehicle(r.QueryRow(ctx, q, id))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Vehicle{}, fmt.Errorf("fleet: %s: %w", id, ErrVehicleNotFound)
	case err != nil:
		return Vehicle{}, fmt.Errorf("fleet: lock %s: %w", id, err)
	}
	return v, nil
}

// vehicle reads one vehicle and holds nothing.
//
// The counterpart of [postgresStore.lockVehicle], and a separate method rather than a flag on it,
// because the difference is not a detail of the query. FOR UPDATE inside a read-only request
// serialises every reader of a vehicle behind whatever is writing it, and — worse — a GET that took
// a row lock and then returned would hold it until the enclosing transaction ended, which for a
// handler using the pool directly is unbounded.
func (postgresStore) vehicle(ctx context.Context, r db.Runner, id uuid.UUID) (Vehicle, error) {
	const q = `SELECT ` + vehicleColumns + ` FROM vehicles WHERE id = $1`

	v, err := scanVehicle(r.QueryRow(ctx, q, id))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Vehicle{}, fmt.Errorf("fleet: %s: %w", id, ErrVehicleNotFound)
	case err != nil:
		return Vehicle{}, fmt.Errorf("fleet: read %s: %w", id, err)
	}
	return v, nil
}

// vehiclesFor reads one provider's fleet, newest first, from a keyset position.
//
// # Why the statement is one shape rather than built from the query
//
// Both optional conditions are written as `$n IS NULL OR …` rather than appended when they apply. A
// built statement renumbers its parameters as clauses come and go, and the defect that invites is
// the one writableColumns already avoids: a clause dropped from the SQL and not from the argument
// list, silently shifting every value after it. PostgreSQL folds a comparison against a NULL
// parameter at plan time, so the fixed statement costs nothing at the call shapes that exist.
//
// # The ordering and the index
//
// ORDER BY created_at DESC, id DESC matches idx_vehicles_provider (provider_id, created_at DESC),
// which 000300 created for exactly this read. The id is the tie-break: created_at is not unique, and
// an ordering that is not total makes a keyset cursor repeat or skip the rows that share a
// timestamp.
//
// The row comparison `(created_at, id) < ($4, $5)` is the keyset itself, and it must be a row
// comparison rather than `created_at <= $4 AND id < $5` — the second is wrong for every row whose
// timestamp is strictly older, and it is wrong quietly, by dropping them.
func (postgresStore) vehiclesFor(ctx context.Context, r db.Runner, providerID uuid.UUID,
	active *bool, after VehicleCursor, limit int) ([]Vehicle, error) {
	const q = `
		SELECT ` + vehicleColumns + `
		FROM vehicles
		WHERE provider_id = $1
		  AND ($2::boolean IS NULL OR (deactivated_at IS NULL) = $2)
		  AND ($3::timestamptz IS NULL OR (created_at, id) < ($3, $4))
		ORDER BY created_at DESC, id DESC
		LIMIT $5`

	var wanted any
	if active != nil {
		wanted = *active
	}

	// The two cursor parameters go NULL together: a position is both fields or neither, which is
	// what VehicleCursor.IsZero says and what the row comparison above needs to be true of.
	var since, sinceID any
	if !after.IsZero() {
		since, sinceID = after.CreatedAt.UTC(), after.ID
	}

	rows, err := r.Query(ctx, q, providerID, wanted, since, sinceID, limit)
	if err != nil {
		return nil, fmt.Errorf("fleet: list the fleet of %s: %w", providerID, err)
	}
	defer rows.Close()

	var out []Vehicle
	for rows.Next() {
		// pgx.Rows satisfies pgx.Row, so the one scanner serves the single-row reads and this
		// one alike — which is what stops a column being added to vehicleColumns and to four of
		// the five places that read it.
		v, err := scanVehicle(rows)
		if err != nil {
			return nil, fmt.Errorf("fleet: scanning the fleet of %s: %w", providerID, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fleet: reading the fleet of %s: %w", providerID, err)
	}
	return out, nil
}

// setDeactivatedAt takes a vehicle out of service.
//
// The timestamp is supplied rather than defaulted from now(), unlike jobs' server_recorded_at. The
// difference is what the column is for: that one is an audit clock nothing may backdate, and this
// is a fact about the fleet that the domain's injectable clock owns (Docs/10 §6.3), so a test can
// state when it happened and assert on it.
func (postgresStore) setDeactivatedAt(ctx context.Context, r db.Runner, id uuid.UUID, at time.Time) (Vehicle, error) {
	const q = `UPDATE vehicles SET deactivated_at = $2 WHERE id = $1 RETURNING ` + vehicleColumns

	v, err := scanVehicle(r.QueryRow(ctx, q, id, at))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Vehicle{}, fmt.Errorf("fleet: %s vanished mid-deactivation: %w", id, ErrVehicleNotFound)
	case err != nil:
		return Vehicle{}, fmt.Errorf("fleet: deactivate %s: %w", id, err)
	}
	return v, nil
}

// clearDeactivatedAt brings a vehicle back into service.
//
// This is the one write in the domain that can be refused by
// uq_vehicles_provider_registration without the caller having supplied a registration at all: the
// provider may have added a replacement on the same plate while this one was out of service. The
// index is partial on deactivated_at being NULL, so the collision only exists at exactly this
// moment, which is why the duplicate is reported from here rather than checked for beforehand.
func (postgresStore) clearDeactivatedAt(ctx context.Context, r db.Runner, id uuid.UUID) (Vehicle, error) {
	const q = `UPDATE vehicles SET deactivated_at = NULL WHERE id = $1 RETURNING ` + vehicleColumns

	v, err := scanVehicle(r.QueryRow(ctx, q, id))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Vehicle{}, fmt.Errorf("fleet: %s vanished mid-reactivation: %w", id, ErrVehicleNotFound)
	case err != nil:
		return Vehicle{}, fmt.Errorf("fleet: reactivate %s: %w", id, duplicate(err))
	}
	return v, nil
}

// --- SHIP-79: the provider's declaration ------------------------------------------------------

// profileLockClass is the advisory-lock class this domain takes, and it is the ticket's number.
//
// cmd/worker/outbox.go established the convention at 134 so that a second use of advisory locks
// could not silently share a key space with the first. This is that second use, and 79 keeps them
// apart by construction rather than by anyone remembering.
const profileLockClass = 79

// lockProfile serialises the whole of one provider's declaration for the rest of the transaction.
//
// **A row lock cannot express this, which is why it is an advisory one.** What has to be serialised
// is a *set* — including the case where the set is currently empty, so there are no rows to lock and
// FOR UPDATE has nothing to hold. Two providers declaring at once take different keys and neither
// waits; the same provider from two devices takes one, and the second sees what the first committed.
//
// Without it, READ COMMITTED loses the update rather than reporting a conflict: each transaction's
// DELETE cannot see the other's uncommitted INSERT, so both survive and the declaration becomes the
// union of two sets that neither client asked for. That is the defect this exists to prevent, and
// it is invisible afterwards.
//
// Blocking rather than pg_try_advisory_xact_lock: the outbox skips an aggregate another worker
// holds because there is more work to do meanwhile, and here there is nothing else to do — the
// caller asked for their declaration to be replaced and waiting a few milliseconds is the answer.
func (postgresStore) lockProfile(ctx context.Context, r db.Runner, providerID uuid.UUID) error {
	const q = `SELECT pg_advisory_xact_lock($1, hashtext($2::text))`

	if _, err := r.Exec(ctx, q, profileLockClass, providerID); err != nil {
		return fmt.Errorf("fleet: locking the declaration of %s: %w", providerID, err)
	}
	return nil
}

// replaceAreas makes one scope of a provider's service area exactly the supplied set.
//
// Two statements rather than a DELETE of everything followed by an INSERT of everything, and the
// difference is visible in the data: an entry the provider has held since January survives a
// request that merely resends it, keeping its created_at. Re-declaring an unchanged set writes
// nothing at all.
//
// The other scope is untouched. Adding a postcode does not disturb a state, which is what lets the
// two lists in [ProfileFields] move independently.
//
// ON CONFLICT DO NOTHING rather than an existence check: the caller has already deduplicated within
// the request, and the unique index is what settles the case the caller cannot see — the same row
// arriving from two directions. The advisory lock makes that unreachable today; the clause is what
// keeps this correct if it ever is not.
func (postgresStore) replaceAreas(ctx context.Context, r db.Runner, providerID uuid.UUID,
	scope AreaScope, areas []string) error {

	// An empty set deletes every row of the scope: `area = ANY('{}')` is false for every row, and
	// NOT false is true. That is the "clear it" half of ProfileFields' three states, and it must
	// not be special-cased into "leave it alone" — which is the whole distinction.
	const removed = `
		DELETE FROM provider_service_areas
		WHERE provider_id = $1 AND scope = $2 AND NOT (area = ANY($3::text[]))`

	if _, err := r.Exec(ctx, removed, providerID, string(scope), areas); err != nil {
		return fmt.Errorf("fleet: withdrawing %s areas from %s: %w", scope, providerID, err)
	}

	if len(areas) == 0 {
		return nil
	}

	ids, err := identifiers(len(areas))
	if err != nil {
		return err
	}

	const added = `
		INSERT INTO provider_service_areas (id, provider_id, scope, area)
		SELECT entry.id, $2, $3, entry.area
		FROM unnest($1::uuid[], $4::text[]) AS entry(id, area)
		ON CONFLICT (provider_id, scope, area) DO NOTHING`

	if _, err := r.Exec(ctx, added, ids, providerID, string(scope), areas); err != nil {
		return fmt.Errorf("fleet: declaring %s areas for %s: %w", scope, providerID, err)
	}
	return nil
}

// replaceSpecialties makes a provider's specialties exactly the supplied set.
//
// The counterpart of [postgresStore.replaceAreas] and the same shape, written out rather than
// generalised over the two tables: they differ in their columns, their constraint and their
// conflict target, and a version parameterised on all three would be a query builder — which
// Docs/10 §3.1 rejects for the reason the fixed statements in this file already give.
func (postgresStore) replaceSpecialties(ctx context.Context, r db.Runner, providerID uuid.UUID,
	specialties []Specialty) error {

	wanted := make([]string, 0, len(specialties))
	for _, specialty := range specialties {
		wanted = append(wanted, string(specialty))
	}

	const removed = `
		DELETE FROM provider_specialties
		WHERE provider_id = $1 AND NOT (specialty = ANY($2::text[]))`

	if _, err := r.Exec(ctx, removed, providerID, wanted); err != nil {
		return fmt.Errorf("fleet: withdrawing specialties from %s: %w", providerID, err)
	}

	if len(wanted) == 0 {
		return nil
	}

	ids, err := identifiers(len(wanted))
	if err != nil {
		return err
	}

	const added = `
		INSERT INTO provider_specialties (id, provider_id, specialty)
		SELECT entry.id, $2, entry.specialty
		FROM unnest($1::uuid[], $3::text[]) AS entry(id, specialty)
		ON CONFLICT (provider_id, specialty) DO NOTHING`

	if _, err := r.Exec(ctx, added, ids, providerID, wanted); err != nil {
		return fmt.Errorf("fleet: declaring specialties for %s: %w", providerID, err)
	}
	return nil
}

// profile reads one provider's whole declaration.
//
// Two statements rather than a join or a UNION. They are two sets with nothing in common but the
// provider, and joining them would multiply eight states by twelve specialties into ninety-six rows
// to be deduplicated back in Go.
//
// A provider who has declared nothing reads as an empty profile rather than as a missing one, which
// is why neither query treats no rows as an error.
func (postgresStore) profile(ctx context.Context, r db.Runner, providerID uuid.UUID) (Profile, error) {
	profile := Profile{ProviderID: providerID}

	// The public half first, because it is the one a customer can ever be shown and reading it
	// through the same function is what keeps `GET /v1/fleet/profile` and the comparison screen
	// from drifting about what a provider has declared (SHIP-79a).
	public, err := postgresStore{}.publicProfile(ctx, r, providerID)
	if err != nil {
		return Profile{}, err
	}
	profile.Public = public

	// States before postcodes, then ascending within each. Written as an ordering on the
	// predicate rather than `ORDER BY scope DESC`, which sorts states first only by the accident
	// that 's' follows 'p'.
	const areas = `
		SELECT scope, area
		FROM provider_service_areas
		WHERE provider_id = $1
		ORDER BY (scope = 'state') DESC, area`

	rows, err := r.Query(ctx, areas, providerID)
	if err != nil {
		return Profile{}, fmt.Errorf("fleet: read the service area of %s: %w", providerID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var area ServiceArea
		if err := rows.Scan(&area.Scope, &area.Area); err != nil {
			return Profile{}, fmt.Errorf("fleet: scanning the service area of %s: %w", providerID, err)
		}
		profile.Areas = append(profile.Areas, area)
	}
	if err := rows.Err(); err != nil {
		return Profile{}, fmt.Errorf("fleet: reading the service area of %s: %w", providerID, err)
	}

	// Unordered in SQL and sorted in Go, because the order that matters is Specialties' own —
	// general freight first, dangerous goods last — and neither the alphabet nor the insertion
	// order is that. A value the constraint accepts and Specialties does not know would sort to
	// the front; the pairing test in migrations is what makes that unreachable.
	const kinds = `SELECT specialty FROM provider_specialties WHERE provider_id = $1`

	specialtyRows, err := r.Query(ctx, kinds, providerID)
	if err != nil {
		return Profile{}, fmt.Errorf("fleet: read the specialties of %s: %w", providerID, err)
	}
	defer specialtyRows.Close()

	held := map[Specialty]bool{}
	for specialtyRows.Next() {
		var specialty Specialty
		if err := specialtyRows.Scan(&specialty); err != nil {
			return Profile{}, fmt.Errorf("fleet: scanning the specialties of %s: %w", providerID, err)
		}
		held[specialty] = true
	}
	if err := specialtyRows.Err(); err != nil {
		return Profile{}, fmt.Errorf("fleet: reading the specialties of %s: %w", providerID, err)
	}

	for _, specialty := range Specialties {
		if held[specialty] {
			profile.Specialties = append(profile.Specialties, specialty)
		}
	}
	return profile, nil
}

// insertPublicProfile writes a provider's first declaration of who they trade as (SHIP-79a).
//
// **Two statements rather than one upsert, and the reason is a constraint rather than taste.** The
// obvious shape is `INSERT … ON CONFLICT DO UPDATE` with a `COALESCE` per column, so a caller naming
// one field leaves the other alone. It does not work: PostgreSQL forms and **checks** the proposed
// row before it arbitrates the conflict, so the placeholder that stands in for "not named" has to
// satisfy `ck_provider_profiles_display_name` — and the only value that would is a made-up name.
// Found by TestAFirstDeclarationNamesBothFields, which amended one field on an existing row and got
// a 500 out of a constraint that was doing exactly its job.
//
// So the caller establishes which case it is — [Service.Declare] already reads the row under the
// lock, to refuse a half-declaration — and this is the branch where there is nothing to preserve.
// Both values are plain strings here rather than pointers, because a first declaration that could
// omit one is the case the read has already ruled out.
func (postgresStore) insertPublicProfile(ctx context.Context, r db.Runner, providerID uuid.UUID,
	displayName, operatesAs string) error {

	const q = `
		INSERT INTO provider_profiles (provider_id, display_name, operates_as)
		VALUES ($1, $2, $3)`

	if _, err := r.Exec(ctx, q, providerID, displayName, operatesAs); err != nil {
		return fmt.Errorf("fleet: declare who %s trades as: %w", providerID, err)
	}
	return nil
}

// updatePublicProfile amends an existing declaration, leaving unnamed fields alone (SHIP-79a).
//
// `COALESCE` against the column rather than against a placeholder, which is safe here for the reason
// it is not safe in an insert: every branch of it is a value already in the row, so a field nobody
// named keeps a value the constraint has already accepted.
func (postgresStore) updatePublicProfile(ctx context.Context, r db.Runner, providerID uuid.UUID,
	displayName, operatesAs *string) error {

	// `updated_at` is deliberately not set here: 000303 attaches the `set_updated_at` trigger every
	// mutable table in this schema wears, and a statement that also wrote the column would be a
	// second answer to when the row changed — one that a psql session amending a profile by hand
	// would not give.
	const q = `
		UPDATE provider_profiles
		SET display_name = COALESCE($2, display_name),
		    operates_as  = COALESCE($3, operates_as)
		WHERE provider_id = $1`

	if _, err := r.Exec(ctx, q, providerID, displayName, operatesAs); err != nil {
		return fmt.Errorf("fleet: amend who %s trades as: %w", providerID, err)
	}
	return nil
}

// publicProfile reads one provider's public half.
//
// No rows is the zero value rather than an error: a provider who has not said who they are is an
// ordinary state, and [PublicProfile.Declared] is how a caller tells the two apart.
func (postgresStore) publicProfile(ctx context.Context, r db.Runner, providerID uuid.UUID) (PublicProfile, error) {
	const q = `SELECT display_name, operates_as FROM provider_profiles WHERE provider_id = $1`

	public := PublicProfile{ProviderID: providerID}
	err := r.QueryRow(ctx, q, providerID).Scan(&public.DisplayName, &public.OperatesAs)
	switch {
	case errors.Is(err, db.ErrNoRows):
		return PublicProfile{ProviderID: providerID}, nil
	case err != nil:
		return PublicProfile{}, fmt.Errorf("fleet: read who %s trades as: %w", providerID, err)
	}
	return public, nil
}

// publicProfiles reads the public half of many providers at once (SHIP-79a).
//
// **One statement for a whole page, which is the reason this exists rather than a loop over
// [postgresStore.publicProfile].** Its caller renders a customer's comparison screen, and a page of
// a hundred offers through the single-row read is a hundred round trips — the N+1 that
// `cmd/api/routes_bidding.go` already records as the thing a batch read in this domain would fix.
//
// A provider with no row is simply absent from the map. The caller renders what it has: an offer
// must not vanish from a comparison screen because the provider has not finished their profile.
func (postgresStore) publicProfiles(ctx context.Context, r db.Runner, ids []uuid.UUID) (map[uuid.UUID]PublicProfile, error) {
	found := make(map[uuid.UUID]PublicProfile, len(ids))
	if len(ids) == 0 {
		return found, nil
	}

	const q = `
		SELECT provider_id, display_name, operates_as
		FROM provider_profiles
		WHERE provider_id = ANY($1)`

	rows, err := r.Query(ctx, q, ids)
	if err != nil {
		return nil, fmt.Errorf("fleet: read who %d providers trade as: %w", len(ids), err)
	}
	defer rows.Close()

	for rows.Next() {
		var public PublicProfile
		if err := rows.Scan(&public.ProviderID, &public.DisplayName, &public.OperatesAs); err != nil {
			return nil, fmt.Errorf("fleet: scanning one of %d public profiles: %w", len(ids), err)
		}
		found[public.ProviderID] = public
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fleet: reading %d public profiles: %w", len(ids), err)
	}
	return found, nil
}

// identifiers generates n UUIDv7s, in the application rather than in PostgreSQL.
//
// Docs/10 §3.3's rule, and the reason it is a rule holds here too: the ordering a v7 carries is the
// order the provider named their regions in, which a gen_random_uuid() default would replace with
// noise.
func identifiers(n int) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, n)
	for range n {
		id, err := uuid.NewV7()
		if err != nil {
			return nil, fmt.Errorf("fleet: generating an identifier: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// duplicate turns the index's refusal into the domain's own error.
//
// Named by constraint rather than by SQLSTATE alone, so that a unique violation from some future
// index is not silently reported as a registration clash — which would be a message telling the
// provider to change a field that had nothing to do with it.
func duplicate(err error) error {
	if db.IsUniqueViolation(err, "uq_vehicles_provider_registration") {
		return ErrDuplicateRegistration
	}
	return err
}
