package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// transitionSetting is the session variable 000402's trigger reads.
//
// It carries the id of the job_status_history row describing the change that is about to be
// made. The trigger refuses the update unless that row exists, belongs to the job being updated,
// and names the same two statuses — so the variable is not a password that unlocks the column,
// it is a pointer to the record that justifies the write.
//
// A custom setting name has to contain a dot; PostgreSQL treats the part before it as an
// extension namespace, which is why every one of ours starts with "shipper.".
const transitionSetting = "shipper.job_status_transition"

// postgresStore is this domain's persistence, concrete and unexported.
//
// There is no repository interface, per Docs/10 §2.2 and Docs/06 §4.1: the guarantees this
// domain rests on are PostgreSQL's — row locking here, a trigger in 000402 — and an interface
// designed to keep the database swappable would hide exactly the mechanisms that make the
// lifecycle correct. A mock would accept every write these methods exist to have refused.
//
// Every method takes a db.Runner as its first argument after ctx, so the caller decides whether
// the work stands alone or joins a transaction it already opened (Docs/10 §3.2). For a
// transition that decision is made for them: 000402 requires a transaction.
type postgresStore struct{}

// jobColumns is every column of a job, in the order [scanJob] reads them.
//
// The nullable text, integer and numeric columns are coalesced in SQL rather than scanned into
// pointers, and that is not laziness: the domain's representation of "not supplied" for those
// fields is already the zero value, and the constraints in 000404 make the zero value one the
// column cannot hold — ck_jobs_length_cm refuses a dimension that is not positive — so 0 and NULL
// cannot be confused in either direction.
//
// The coordinates and the four window ends are the exceptions, for opposite reasons. (0, 0) is a
// real point in the Gulf of Guinea, so a coalesced coordinate would be indistinguishable from a
// resolved one; and PostgreSQL's NULL has no representation in time.Time at all.
const jobColumns = `
	id, customer_id, status,
	COALESCE(pickup_line, ''), COALESCE(pickup_suburb, ''),
	COALESCE(pickup_state, ''), COALESCE(pickup_postcode, ''),
	pickup_latitude, pickup_longitude, COALESCE(pickup_formatted, ''),
	COALESCE(dropoff_line, ''), COALESCE(dropoff_suburb, ''),
	COALESCE(dropoff_state, ''), COALESCE(dropoff_postcode, ''),
	dropoff_latitude, dropoff_longitude, COALESCE(dropoff_formatted, ''),
	COALESCE(goods_description, ''),
	COALESCE(length_cm, 0), COALESCE(width_cm, 0), COALESCE(height_cm, 0),
	COALESCE(weight_kg, 0),
	COALESCE(vehicle_requirement, ''), COALESCE(handling_notes, ''),
	pickup_window_start, pickup_window_end,
	dropoff_window_start, dropoff_window_end,
	created_at, updated_at`

// scanJob reads one row of [jobColumns].
//
// One function rather than four copies of a thirty-argument Scan call. A column added to
// jobColumns and not here is a scan mismatch at the first call, which is the failure worth
// having: the alternative is four call sites that have to be found and edited together.
func scanJob(row pgx.Row) (Job, error) {
	var (
		j Job

		pickupLat, pickupLng   *float64
		dropoffLat, dropoffLng *float64

		pickupStart, pickupEnd   *time.Time
		dropoffStart, dropoffEnd *time.Time
	)

	if err := row.Scan(
		&j.ID, &j.CustomerID, &j.Status,
		&j.Pickup.Line, &j.Pickup.Suburb, &j.Pickup.State, &j.Pickup.Postcode,
		&pickupLat, &pickupLng, &j.Pickup.Formatted,
		&j.Dropoff.Line, &j.Dropoff.Suburb, &j.Dropoff.State, &j.Dropoff.Postcode,
		&dropoffLat, &dropoffLng, &j.Dropoff.Formatted,
		&j.GoodsDescription,
		&j.Dimensions.LengthCm, &j.Dimensions.WidthCm, &j.Dimensions.HeightCm,
		&j.WeightKg,
		&j.VehicleRequirement, &j.HandlingNotes,
		&pickupStart, &pickupEnd,
		&dropoffStart, &dropoffEnd,
		&j.CreatedAt, &j.UpdatedAt,
	); err != nil {
		return Job{}, err
	}

	// ck_jobs_pickup_coordinate_is_a_pair means one of these being present implies the other,
	// so testing either would test both. Written as an explicit pair anyway: a scan that
	// relied on a constraint holding would be silently wrong if the constraint ever went.
	if pickupLat != nil && pickupLng != nil {
		j.Pickup.Latitude, j.Pickup.Longitude, j.Pickup.Resolved = *pickupLat, *pickupLng, true
	}
	if dropoffLat != nil && dropoffLng != nil {
		j.Dropoff.Latitude, j.Dropoff.Longitude, j.Dropoff.Resolved = *dropoffLat, *dropoffLng, true
	}

	j.PickupWindow = window(pickupStart, pickupEnd)
	j.DropoffWindow = window(dropoffStart, dropoffEnd)

	return j, nil
}

func window(start, end *time.Time) TimeWindow {
	var w TimeWindow
	if start != nil {
		w.Start = *start
	}
	if end != nil {
		w.End = *end
	}
	return w
}

// The NULL conversions.
//
// The database distinguishes "no value" from "the zero value" and Go does not, so the conversion
// happens at the boundary in both directions rather than leaving columns that are never NULL and
// constraints that never fire. recordTransition already does the same for actor_id and reason.
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

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

// locationArgs renders a location as the seven values its columns take.
//
// The coordinate and the formatted address go NULL together with Resolved, so a location edited
// since it was looked up cannot keep the previous answer. That is the invariant [Location] exists
// to hold, expressed once here rather than at each call site.
func locationArgs(l Location) []any {
	args := []any{
		nullText(l.Line), nullText(l.Suburb), nullText(string(l.State)), nullText(l.Postcode),
	}
	if l.Resolved {
		return append(args, l.Latitude, l.Longitude, nullText(l.Formatted))
	}
	return append(args, nil, nil, nil)
}

// draftArgs is every value a draft's columns take, in the order insertDraft and updateDraft write
// them. One list, so the two statements cannot drift into disagreeing about a column.
func draftArgs(j Job) []any {
	args := locationArgs(j.Pickup)
	args = append(args, locationArgs(j.Dropoff)...)
	return append(args,
		nullText(j.GoodsDescription),
		nullInt(j.Dimensions.LengthCm), nullInt(j.Dimensions.WidthCm), nullInt(j.Dimensions.HeightCm),
		nullFloat(j.WeightKg),
		nullText(j.VehicleRequirement), nullText(j.HandlingNotes),
		nullTime(j.PickupWindow.Start), nullTime(j.PickupWindow.End),
		nullTime(j.DropoffWindow.Start), nullTime(j.DropoffWindow.End),
	)
}

// draftColumns names the columns draftArgs supplies, in the same order.
const draftColumns = `
	pickup_line, pickup_suburb, pickup_state, pickup_postcode,
	pickup_latitude, pickup_longitude, pickup_formatted,
	dropoff_line, dropoff_suburb, dropoff_state, dropoff_postcode,
	dropoff_latitude, dropoff_longitude, dropoff_formatted,
	goods_description, length_cm, width_cm, height_cm, weight_kg,
	vehicle_requirement, handling_notes,
	pickup_window_start, pickup_window_end,
	dropoff_window_start, dropoff_window_end`

// isCustomer reports whether the account exists and is a customer account.
//
// This domain reading `users` is sanctioned rather than a boundary crossed: the table is in the
// shared migration block precisely because it is read across the whole service (Docs/10 §9.2),
// and `jobs.customer_id` already references it. What is not sanctioned — and is not done — is
// importing internal/identity to ask.
//
// A missing account reports false rather than an error. The only caller has a token naming that
// account, so the row missing means it has been removed underneath a live session, and "you may
// not create a job" is a truthful answer to that.
func (postgresStore) isCustomer(ctx context.Context, r db.Runner, id uuid.UUID) (bool, error) {
	const q = `SELECT role = 'customer' FROM users WHERE id = $1`

	var customer bool
	err := r.QueryRow(ctx, q, id).Scan(&customer)
	switch {
	case errors.Is(err, db.ErrNoRows):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("jobs: read the role of %s: %w", id, err)
	}
	return customer, nil
}

// insertDraft creates a job.
//
// status is deliberately not named. 000400 defaults it to 'Draft' and 000402's insert trigger
// refuses any other value, so the default is the only status creation can produce and there is
// nothing here for a caller to get wrong — which is what "status is never a settable field" means
// at the moment a job comes into existence (Docs/02 §2).
func (postgresStore) insertDraft(ctx context.Context, r db.Runner, j Job) (Job, error) {
	const q = `
		INSERT INTO jobs (id, customer_id, ` + draftColumns + `)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
		        $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27)
		RETURNING ` + jobColumns

	args := append([]any{j.ID, j.CustomerID}, draftArgs(j)...)

	created, err := scanJob(r.QueryRow(ctx, q, args...))
	if err != nil {
		return Job{}, fmt.Errorf("jobs: create a draft for %s: %w", j.CustomerID, err)
	}
	return created, nil
}

// updateDraft writes every draft column at once, from a job the caller has already locked.
//
// Not a dynamically built statement naming only the changed columns, and that is deliberate. The
// caller holds the row under FOR UPDATE and has applied the patch to the value it read, so
// writing all of them is writing what is already there — and one fixed statement cannot suffer
// the defect a built one invites, where a column is omitted from the SET list and its parameter
// is not, silently shifting every value after it by one.
//
// status is not in draftColumns, so this statement cannot move a job even by accident. 000402's
// trigger returns early when the status is unchanged, which is what lets an ordinary edit pass
// through a guard that exists for a different column entirely.
func (postgresStore) updateDraft(ctx context.Context, r db.Runner, j Job) (Job, error) {
	const q = `
		UPDATE jobs SET
			pickup_line = $2, pickup_suburb = $3, pickup_state = $4, pickup_postcode = $5,
			pickup_latitude = $6, pickup_longitude = $7, pickup_formatted = $8,
			dropoff_line = $9, dropoff_suburb = $10, dropoff_state = $11, dropoff_postcode = $12,
			dropoff_latitude = $13, dropoff_longitude = $14, dropoff_formatted = $15,
			goods_description = $16, length_cm = $17, width_cm = $18, height_cm = $19,
			weight_kg = $20, vehicle_requirement = $21, handling_notes = $22,
			pickup_window_start = $23, pickup_window_end = $24,
			dropoff_window_start = $25, dropoff_window_end = $26
		WHERE id = $1
		RETURNING ` + jobColumns

	args := append([]any{j.ID}, draftArgs(j)...)

	updated, err := scanJob(r.QueryRow(ctx, q, args...))
	switch {
	case errors.Is(err, db.ErrNoRows):
		// The row was locked a few statements ago. Nothing in this platform deletes a job
		// (Docs/10 §3.3 has no soft deletes and SHIP-171 pseudonymises), so this is
		// reported rather than assumed away.
		return Job{}, fmt.Errorf("jobs: %s vanished mid-edit: %w", j.ID, ErrJobNotFound)
	case err != nil:
		return Job{}, fmt.Errorf("jobs: update %s: %w", j.ID, err)
	}
	return updated, nil
}

// lockJob reads a job and holds the row for the rest of the transaction.
//
// FOR UPDATE rather than a plain read, because a transition is a read-modify-write on the status
// column and two of them arriving at once is not hypothetical: a customer cancelling while an
// administrator disputes, a provider's queued milestone landing beside a live one. In READ
// COMMITTED, the second transaction blocks here and then re-reads the row the first one
// committed, so its own check of Docs/02 §2 runs against the status the job actually has rather
// than the one it had when the request arrived.
func (postgresStore) lockJob(ctx context.Context, r db.Runner, id uuid.UUID) (Job, error) {
	const q = `SELECT ` + jobColumns + ` FROM jobs WHERE id = $1 FOR UPDATE`

	j, err := scanJob(r.QueryRow(ctx, q, id))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Job{}, fmt.Errorf("jobs: %s: %w", id, ErrJobNotFound)
	case err != nil:
		return Job{}, fmt.Errorf("jobs: lock %s: %w", id, err)
	}
	return j, nil
}

// job reads one job and holds nothing.
//
// The counterpart of [postgresStore.lockJob], and a separate method rather than a flag on it,
// because the difference is not a detail of the query. FOR UPDATE inside a read-only request
// serialises every reader of a job behind whatever is writing it, and — worse — a GET that took
// a row lock and then returned would hold it until the enclosing transaction ended, which for a
// handler using the pool directly is unbounded. A read that is only ever a read takes no lock.
func (postgresStore) job(ctx context.Context, r db.Runner, id uuid.UUID) (Job, error) {
	const q = `SELECT ` + jobColumns + ` FROM jobs WHERE id = $1`

	j, err := scanJob(r.QueryRow(ctx, q, id))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Job{}, fmt.Errorf("jobs: %s: %w", id, ErrJobNotFound)
	case err != nil:
		return Job{}, fmt.Errorf("jobs: read %s: %w", id, err)
	}
	return j, nil
}

// jobsFor reads one customer's jobs, newest first, from a keyset position (SHIP-66).
//
// # Why the statement is one shape rather than built from the query
//
// Both optional conditions are written as `$n IS NULL OR …` rather than appended when they apply.
// A built statement renumbers its parameters as clauses come and go, and the defect that invites is
// the one draftColumns already avoids: a clause dropped from the SQL and not from the argument
// list, silently shifting every value after it. PostgreSQL folds a comparison against a NULL
// parameter at plan time, so the fixed statement costs nothing at the two call shapes that exist.
//
// # The ordering and the index
//
// ORDER BY created_at DESC, id DESC matches idx_jobs_customer (customer_id, created_at DESC),
// which 000400 created for exactly this read. The id is the tie-break: created_at is not unique,
// and an ordering that is not total makes a keyset cursor repeat or skip the rows that share a
// timestamp.
//
// The row comparison `(created_at, id) < ($3, $4)` is the keyset itself, and it must be a row
// comparison rather than `created_at <= $3 AND id < $4` — the second is wrong for every row whose
// timestamp is strictly older, and it is wrong quietly, by dropping them.
func (postgresStore) jobsFor(ctx context.Context, r db.Runner, customerID uuid.UUID,
	status Status, after JobCursor, limit int) ([]Job, error) {
	const q = `
		SELECT ` + jobColumns + `
		FROM jobs
		WHERE customer_id = $1
		  AND ($2::text IS NULL OR status = $2)
		  AND ($3::timestamptz IS NULL OR (created_at, id) < ($3, $4))
		ORDER BY created_at DESC, id DESC
		LIMIT $5`

	var wanted any
	if status != "" {
		wanted = string(status)
	}

	// The two cursor parameters go NULL together: a position is both fields or neither, which
	// is what JobCursor.IsZero says and what the row comparison above needs to be true of.
	var since, sinceID any
	if !after.IsZero() {
		since, sinceID = after.CreatedAt.UTC(), after.ID
	}

	rows, err := r.Query(ctx, q, customerID, wanted, since, sinceID, limit)
	if err != nil {
		return nil, fmt.Errorf("jobs: list the jobs of %s: %w", customerID, err)
	}
	defer rows.Close()

	var out []Job
	for rows.Next() {
		// pgx.Rows satisfies pgx.Row, so the one scanner serves the single-row reads and
		// this one alike — which is what stops a column being added to jobColumns and to
		// three of the four places that read it.
		job, err := scanJob(rows)
		if err != nil {
			return nil, fmt.Errorf("jobs: scanning the jobs of %s: %w", customerID, err)
		}
		out = append(out, job)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("jobs: reading the jobs of %s: %w", customerID, err)
	}
	return out, nil
}

// recordTransition writes the history row and returns the platform's clock reading.
//
// server_recorded_at is not supplied. It defaults from now(), which is transaction start time,
// so it is the same instant as the job row's updated_at — and, more to the point, it is a
// timestamp no caller can choose. Docs/02 §3.1 makes the second of the two clocks the one audit
// and support rely on, and a value the application could pass would be one it could backdate.
func (postgresStore) recordTransition(ctx context.Context, r db.Runner, c StatusChange) (time.Time, error) {
	const q = `
		INSERT INTO job_status_history
			(id, job_id, from_status, to_status, actor_type, actor_id, reason, actor_recorded_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING server_recorded_at`

	// The database distinguishes "no account" from "the zero account", and "no reason given"
	// from "the empty reason". Go's zero values do not, so the conversion happens here rather
	// than leaving two columns that are never NULL and two constraints that never fire.
	var actorID any
	if c.Actor.ID != uuid.Nil {
		actorID = c.Actor.ID
	}
	var reason any
	if c.Reason != "" {
		reason = c.Reason
	}

	var serverRecordedAt time.Time
	if err := r.QueryRow(ctx, q,
		c.ID, c.JobID, string(c.From), string(c.To),
		string(c.Actor.Type), actorID, reason, c.ActorRecordedAt,
	).Scan(&serverRecordedAt); err != nil {
		return time.Time{}, fmt.Errorf("jobs: record %s moving %s to %s: %w", c.JobID, c.From, c.To, err)
	}
	return serverRecordedAt, nil
}

// claimTransition tells 000402's trigger which history row authorises the update that follows.
//
// The third argument to set_config makes the setting local to the current transaction. Outside
// one it lasts only for this statement, which is why a caller holding a pool rather than a
// transaction finds the update refused — and why Transition checks for a transaction before it
// writes anything at all.
func (postgresStore) claimTransition(ctx context.Context, r db.Runner, historyID uuid.UUID) error {
	const q = `SELECT set_config($1, $2, true)`

	if _, err := r.Exec(ctx, q, transitionSetting, historyID.String()); err != nil {
		return fmt.Errorf("jobs: claim transition %s: %w", historyID, err)
	}
	return nil
}

// setStatus is the one place in this service that writes jobs.status.
//
// It is unexported and takes no route to the outside: the only caller is Transition, which has
// already validated the move and written the record 000402 will look for. A second caller would
// be the defect the guard exists to prevent.
func (postgresStore) setStatus(ctx context.Context, r db.Runner, id uuid.UUID, to Status) (Job, error) {
	const q = `UPDATE jobs SET status = $2 WHERE id = $1 RETURNING ` + jobColumns

	j, err := scanJob(r.QueryRow(ctx, q, id, string(to)))
	switch {
	case errors.Is(err, db.ErrNoRows):
		// The row was locked a few statements ago, so this means it has been deleted
		// underneath us — which nothing in this platform does (Docs/10 §3.3 has no soft
		// deletes and SHIP-171 pseudonymises). Reported rather than assumed away.
		return Job{}, fmt.Errorf("jobs: %s vanished mid-transition: %w", id, ErrJobNotFound)
	case err != nil:
		return Job{}, fmt.Errorf("jobs: move %s to %s: %w", id, to, err)
	}
	return j, nil
}

// history reads a job's transitions, oldest first.
//
// Written now because a status timeline is what SHIP-77 shows a customer and what support reads
// during a dispute, and because a test that only ever writes history cannot tell whether the
// columns hold what they were given.
func (postgresStore) history(ctx context.Context, r db.Runner, jobID uuid.UUID) ([]StatusChange, error) {
	const q = `
		SELECT id, job_id, from_status, to_status, actor_type, actor_id, reason,
		       actor_recorded_at, server_recorded_at
		FROM job_status_history
		WHERE job_id = $1
		ORDER BY server_recorded_at, id`

	rows, err := r.Query(ctx, q, jobID)
	if err != nil {
		return nil, fmt.Errorf("jobs: read the history of %s: %w", jobID, err)
	}
	defer rows.Close()

	var out []StatusChange
	for rows.Next() {
		var (
			c        StatusChange
			actorID  *uuid.UUID
			reason   *string
			actorKnd string
		)
		if err := rows.Scan(&c.ID, &c.JobID, &c.From, &c.To, &actorKnd, &actorID, &reason,
			&c.ActorRecordedAt, &c.ServerRecordedAt); err != nil {
			return nil, fmt.Errorf("jobs: scanning the history of %s: %w", jobID, err)
		}
		c.Actor.Type = ActorType(actorKnd)
		if actorID != nil {
			c.Actor.ID = *actorID
		}
		if reason != nil {
			c.Reason = *reason
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("jobs: reading the history of %s: %w", jobID, err)
	}
	return out, nil
}
