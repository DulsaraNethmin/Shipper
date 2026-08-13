package delivery

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
// There is no repository interface, per Docs/10 §2.2 and Docs/06 §4.1. The guarantee this file
// rests on hardest is PostgreSQL's — uq_driver_assignments_active is a *partial* unique index, so a
// job accumulates assignments over its life while never having two live at once — and an interface
// designed to keep the database swappable would hide exactly the mechanism that makes the rule
// correct. A mock would accept the second live driver this store exists to have refused.
//
// Every method takes a db.Runner as its first argument after ctx, so the caller decides whether the
// work stands alone or joins a transaction it already opened (Docs/10 §3.2).
//
// # What this file does not query
//
// `jobs` and `bids`. Both are other domains' tables and both are reached through the ports in
// ports.go instead, wired in cmd/api — the boundary lint reads imports and cannot see a SELECT, so
// this is a rule that holds by being written down and followed. `users` is different and is read
// below: it lives in the shared migration block precisely because the whole service reads it
// (Docs/10 §9.2), which is the same sanction `jobs` and `fleet` rely on for their role checks.
type postgresStore struct{}

// assignmentColumns is every column of an assignment, in the order [scanAssignment] reads them.
//
// unassigned_at has no COALESCE because PostgreSQL's NULL has no representation in time.Time, and
// because the distinction it carries is the whole of [Assignment.Live].
const assignmentColumns = `
	id, job_id, driver_name, driver_mobile, unassigned_at, created_at, updated_at`

// scanAssignment reads one row of [assignmentColumns].
//
// One function rather than a copy of the Scan call at each call site: a column added to
// assignmentColumns and not here is a scan mismatch at the first call, which is the failure worth
// having.
func scanAssignment(row pgx.Row) (Assignment, error) {
	var (
		a            Assignment
		unassignedAt *time.Time
	)

	if err := row.Scan(
		&a.ID, &a.JobID, &a.DriverName, &a.DriverMobile,
		&unassignedAt, &a.CreatedAt, &a.UpdatedAt,
	); err != nil {
		return Assignment{}, err
	}

	if unassignedAt != nil {
		a.UnassignedAt = *unassignedAt
	}
	return a, nil
}

// insert creates an assignment, live.
//
// unassigned_at is not named, so a driver is always put on the job rather than off it. There is no
// request shape that could ask for anything else, which is what "an assignment ends by being ended,
// not by being created dead" means at the moment the row comes into existence.
//
// A unique violation on uq_driver_assignments_active is translated here rather than reported as a
// database error: it is the index doing the work [Service.AssignDriver] describes, and a caller
// handed a constraint name learns nothing they can act on.
func (postgresStore) insert(ctx context.Context, r db.Runner, a Assignment) (Assignment, error) {
	const q = `
		INSERT INTO driver_assignments (id, job_id, driver_name, driver_mobile)
		VALUES ($1, $2, $3, $4)
		RETURNING ` + assignmentColumns

	created, err := scanAssignment(r.QueryRow(ctx, q, a.ID, a.JobID, a.DriverName, a.DriverMobile))
	if err != nil {
		if db.IsUniqueViolation(err, "uq_driver_assignments_active") {
			return Assignment{}, fmt.Errorf("delivery: %s already has a live driver: %w",
				a.JobID, ErrDriverAlreadyAssigned)
		}
		return Assignment{}, fmt.Errorf("delivery: assigning a driver to %s: %w", a.JobID, err)
	}
	return created, nil
}

// liveAssignment reads the driver currently on a job, and reports whether there is one.
//
// Comma-ok rather than a sentinel error, because "this job has no driver yet" is the ordinary state
// of every awarded job and reporting it as an error would make the normal path the exceptional one.
//
// **No FOR UPDATE.** Locking the rows that exist cannot stop the row that does not yet exist from
// being inserted, so a lock here would buy nothing that uq_driver_assignments_active does not
// already guarantee — and it would suggest to the next reader that the check is what enforces the
// rule.
func (postgresStore) liveAssignment(ctx context.Context, r db.Runner, jobID uuid.UUID) (Assignment, bool, error) {
	const q = `
		SELECT ` + assignmentColumns + `
		FROM driver_assignments
		WHERE job_id = $1 AND unassigned_at IS NULL`

	a, err := scanAssignment(r.QueryRow(ctx, q, jobID))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Assignment{}, false, nil
	case err != nil:
		return Assignment{}, false, fmt.Errorf("delivery: reading the driver on %s: %w", jobID, err)
	}
	return a, true, nil
}

// milestoneColumns is every column of a milestone, in the order [scanMilestone] reads them.
//
// server_recorded_at is selected and never written. 000601's trigger raises on an INSERT that names
// it, which is not a rule this file has to remember: there is no statement below that could.
const milestoneColumns = `
	id, job_id, milestone, actor_type, actor_id, reason, idempotency_key,
	actor_recorded_at, server_recorded_at`

// scanMilestone reads one row of [milestoneColumns].
func scanMilestone(row pgx.Row) (Record, error) {
	var (
		rec     Record
		actorID *uuid.UUID
		reason  *string
		key     *string
	)

	if err := row.Scan(
		&rec.ID, &rec.JobID, &rec.Milestone, &rec.Actor, &actorID, &reason, &key,
		&rec.ActorRecordedAt, &rec.ServerRecordedAt,
	); err != nil {
		return Record{}, err
	}

	// Three nullable columns, and each null means something the zero value says just as well:
	// nobody (the platform acting alone), no reason given, and no request behind the row.
	if actorID != nil {
		rec.ActorID = *actorID
	}
	if reason != nil {
		rec.Reason = *reason
	}
	if key != nil {
		rec.Key = *key
	}
	return rec, nil
}

// insertMilestone records a milestone, or reports that this key already recorded one.
//
// # ON CONFLICT DO NOTHING, and every word of that is load-bearing
//
// **ON CONFLICT** rather than an insert whose unique violation is caught: a violation aborts the
// surrounding transaction, and this insert runs inside one that still has a status transition to
// make. Catching the error would mean unwinding to a savepoint to ask a question the conflict has
// already answered.
//
// **DO NOTHING** rather than DO UPDATE: the table is append-only and 000601's trigger would refuse
// the update. That is the correct refusal — a retry must return what was recorded, not overwrite it
// with a second attempt's timestamp.
//
// **The index is inferred by its columns and its predicate**, because uq_milestones_idempotency is
// a partial index and therefore has no constraint name to name. The WHERE clause here is not a
// filter on rows; it is how PostgreSQL is told which index this statement expects.
//
// Under a concurrent duplicate the second statement waits on the first's speculative insertion,
// then finds the committed row and returns none — which is why the caller's follow-up read is what
// answers, rather than this returning a partially written row.
//
// recorded is false when the key had already recorded something on this job. The existing row is
// not read here: what to do about it is [Service.RecordMilestone]'s decision.
func (postgresStore) insertMilestone(ctx context.Context, r db.Runner, rec Record) (Record, bool, error) {
	const q = `
		INSERT INTO milestones
			(id, job_id, milestone, actor_type, actor_id, reason, idempotency_key, actor_recorded_at)
		VALUES ($1, $2, $3, $4, $5, nullif($6, ''), nullif($7, ''), $8)
		ON CONFLICT (job_id, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
		RETURNING ` + milestoneColumns

	created, err := scanMilestone(r.QueryRow(ctx, q,
		rec.ID, rec.JobID, string(rec.Milestone), string(rec.Actor), rec.ActorID,
		rec.Reason, rec.Key, rec.ActorRecordedAt))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Record{}, false, nil
	case err != nil:
		return Record{}, false, fmt.Errorf("delivery: recording %s on %s: %w",
			rec.Milestone, rec.JobID, err)
	}
	return created, true, nil
}

// milestoneRecordedBy is what a key already recorded on a job, if anything.
//
// Read after [postgresStore.insertMilestone] declines, which is its only caller: at READ COMMITTED
// each statement takes a fresh snapshot, so a row committed by the request that won a race is
// visible to this one even though the transaction around it began earlier.
func (postgresStore) milestoneRecordedBy(
	ctx context.Context,
	r db.Runner,
	jobID uuid.UUID,
	key string,
) (Record, bool, error) {
	const q = `
		SELECT ` + milestoneColumns + `
		FROM milestones
		WHERE job_id = $1 AND idempotency_key = $2`

	rec, err := scanMilestone(r.QueryRow(ctx, q, jobID, key))
	switch {
	case errors.Is(err, db.ErrNoRows):
		return Record{}, false, nil
	case err != nil:
		return Record{}, false, fmt.Errorf("delivery: reading what %s recorded on %s: %w", key, jobID, err)
	}
	return rec, true, nil
}

// proofColumns is every column of a proof, in the order [scanProof] reads them.
//
// The milestone's own two facts are joined in rather than copied into `proofs`: which milestone was
// recorded, and when the actor says they recorded it. 000603 keeps neither, because both are already
// one row away and a second copy is a second thing that can be wrong.
const proofColumns = `
	p.id, p.job_id, p.milestone_id, m.milestone, p.object_key,
	p.content_type, p.content_length, p.etag, p.exception_reason,
	m.actor_recorded_at, p.created_at`

// scanProof reads one row of [proofColumns].
//
// Five nullable columns since SHIP-116, and they are nullable in one group or the other: a row is a
// photograph the store confirmed or a reasoned exception, and ck_proofs_photograph_or_exception
// refuses every mixture. The zero value says the same thing in Go — an empty object key is what
// [Proof.IsException] reads — so no pointer survives past this function.
func scanProof(row pgx.Row) (Proof, error) {
	var (
		p             Proof
		objectKey     *string
		contentType   *string
		contentLength *int64
		etag          *string
		exception     *string
	)

	if err := row.Scan(
		&p.ID, &p.JobID, &p.MilestoneID, &p.Milestone, &objectKey,
		&contentType, &contentLength, &etag, &exception, &p.RecordedAt, &p.AcceptedAt,
	); err != nil {
		return Proof{}, err
	}

	if objectKey != nil {
		p.ObjectKey = *objectKey
	}
	if contentType != nil {
		p.ContentType = *contentType
	}
	if contentLength != nil {
		p.ContentLength = *contentLength
	}
	if etag != nil {
		p.ETag = *etag
	}
	if exception != nil {
		p.ExceptionReason = ProofExceptionReason(*exception)
	}
	return p, nil
}

// insertProof records the evidence for one milestone — an object, or the reason there is none
// (SHIP-115, SHIP-116).
//
// # `nullif` on all five, because the domain speaks zero values and the table speaks NULL
//
// A [Proof] built for an exception carries an empty object key and a zero length, and 000604's
// ck_proofs_photograph_or_exception counts NULLs rather than reading empty strings — which is the
// right way round: `''` is a perfectly good object key as far as `text` is concerned, and a CHECK
// written against it would be a second spelling of "absent" for the database to disagree with the
// domain about. The cast on the length is what stops PostgreSQL inferring `nullif($6, 0)` as
// something other than bigint.
//
// # ON CONFLICT on the object key, for the reason [postgresStore.insertMilestone] gives
//
// A bare unique violation aborts the surrounding transaction, and this insert runs inside one that
// still has a status transition to make. `DO NOTHING` turns the conflict into a row count, which the
// caller reads as [ErrProofAlreadyRecorded] — the same shape, and the same reason, as the milestone
// insert one function up.
//
// **Only uq_proofs_object_key is named.** uq_proofs_milestone is unreachable from here: the milestone
// this row points at was inserted moments ago in this transaction, and a request whose key had
// already recorded one never reaches this statement ([Service.RecordMilestone] returns first). A
// conflict on it would be a defect rather than a race, and a defect should abort loudly rather than
// be absorbed into "already recorded".
//
// # It writes what the store reported
//
// content_type, content_length and etag come from [Service.VerifyProof], which read them off the
// object. They are not the client's stated values and the difference is the point — see proof.go.
func (postgresStore) insertProof(ctx context.Context, r db.Runner, p Proof) (Proof, error) {
	const q = `
		WITH inserted AS (
			INSERT INTO proofs
				(id, job_id, milestone_id, object_key, content_type, content_length, etag,
				 exception_reason)
			VALUES ($1, $2, $3, nullif($4, ''), nullif($5, ''), nullif($6::bigint, 0),
			        nullif($7, ''), nullif($8, ''))
			ON CONFLICT (object_key) DO NOTHING
			RETURNING id, job_id, milestone_id, object_key, content_type, content_length, etag,
			          exception_reason, created_at
		)
		SELECT ` + proofColumns + `
		FROM inserted p
		JOIN milestones m ON m.id = p.milestone_id`

	stored, err := scanProof(r.QueryRow(ctx, q,
		p.ID, p.JobID, p.MilestoneID, p.ObjectKey, p.ContentType, p.ContentLength, p.ETag,
		string(p.ExceptionReason)))
	switch {
	case errors.Is(err, db.ErrNoRows):
		// Only a photograph can reach this. A NULL object key conflicts with nothing —
		// uq_proofs_object_key is a btree and NULLs are distinct — so an exception either
		// inserts or fails loudly on uq_proofs_milestone, which would be a defect rather than
		// a race (see above) and must not be absorbed into "already recorded".
		return Proof{}, fmt.Errorf("delivery: %s is already proof of something: %w",
			p.ObjectKey, ErrProofAlreadyRecorded)
	case err != nil:
		return Proof{}, fmt.Errorf("delivery: recording the evidence for %s on %s: %w",
			p.MilestoneID, p.JobID, err)
	}
	return stored, nil
}

// proofOn is every piece of evidence recorded against one job — photographs and reasoned
// exceptions alike — most recently acted first.
//
// # Ordered by the actor's clock rather than by arrival
//
// Docs/02 §3.1 shows a customer what the driver recorded, not when the phone synced, and SHIP-112's
// absorption makes the two orders genuinely differ: a queued milestone photographed at dawn arrives
// after ones recorded later in the day. `created_at` and then `id` break the tie, because
// actor_recorded_at is not unique — an offline batch synced together carries whatever times the
// device stamped — and an ordering that is not total returns rows in whatever order the plan
// happened to produce.
//
// No pagination and no limit. A delivery has at most five recordable milestones (Docs/01 §4.4) and
// at most one proof each, so the collection is bounded by the domain rather than by a page size —
// the same reading `bidding`'s negotiation chain takes of its own bound.
//
// **This does not check who is asking.** [Service.ProofFor] does, before calling it, which is the
// division doc.go states and the reason this method is unexported.
func (postgresStore) proofOn(ctx context.Context, r db.Runner, jobID uuid.UUID) ([]Proof, error) {
	const q = `
		SELECT ` + proofColumns + `
		FROM proofs p
		JOIN milestones m ON m.id = p.milestone_id
		WHERE p.job_id = $1
		ORDER BY m.actor_recorded_at DESC, p.created_at DESC, p.id DESC`

	rows, err := r.Query(ctx, q, jobID)
	if err != nil {
		return nil, fmt.Errorf("delivery: reading the proof on %s: %w", jobID, err)
	}
	defer rows.Close()

	var proof []Proof
	for rows.Next() {
		p, err := scanProof(rows)
		if err != nil {
			return nil, fmt.Errorf("delivery: reading a proof row on %s: %w", jobID, err)
		}
		proof = append(proof, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("delivery: reading the proof on %s: %w", jobID, err)
	}
	return proof, nil
}

// accountPhone is the caller's own verified mobile, for a self-assignment.
//
// Reading `users` from here is the sanctioned kind of cross-table read: the table is in the shared
// migration block because the whole service reads it, and `jobs` and `fleet` both read it for the
// account's role. What this domain must not do — and does not — is read `jobs` or `bids`.
//
// A missing row is reported rather than absorbed. The caller is authenticated, so their account
// exists; if it does not, something is wrong that a blank driver mobile would only hide.
func (postgresStore) accountPhone(ctx context.Context, r db.Runner, userID uuid.UUID) (string, error) {
	const q = `SELECT phone FROM users WHERE id = $1`

	var phone string
	err := r.QueryRow(ctx, q, userID).Scan(&phone)
	switch {
	case errors.Is(err, db.ErrNoRows):
		return "", fmt.Errorf("delivery: no account %s to self-assign from: %w", userID, err)
	case err != nil:
		return "", fmt.Errorf("delivery: reading the mobile on %s: %w", userID, err)
	}
	return phone, nil
}
