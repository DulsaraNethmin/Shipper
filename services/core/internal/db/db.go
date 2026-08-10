// Package db holds the two things every domain needs from PostgreSQL and nothing else.
//
// It is not an abstraction over the database. Docs/06 §4.1 is explicit that PostgreSQL is not
// abstracted here: the partial unique index enforcing one accepted bid per job and the row
// locking in the award transaction are load-bearing and PostgreSQL-specific, and a repository
// interface designed to keep the database swappable would hide exactly the mechanisms that
// make the marketplace correct. Persistence lives in each domain's own postgres.go, concrete
// and unwrapped.
//
// What this package provides is narrower: a way to write a query that does not care whether
// it is inside a transaction, and a helper that owns the transaction lifecycle.
//
// # Why Runner exists
//
// The award (SHIP-92) accepts one bid and moves a job in a single transaction. Those are two
// domains, and domains may not import each other. If bidding's persistence took a *pgxpool.Pool
// it could not join a transaction the caller had already opened, and the award would become
// two transactions with a window between them — which is the defect the ticket exists to
// prevent.
//
// So every persistence method takes a Runner. The pool satisfies it and so does a transaction,
// which means the same method works standalone or enlisted, decided by the caller rather than
// by the method.
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Runner is what a query needs: somewhere to send it.
//
// *pgxpool.Pool and pgx.Tx both satisfy it structurally, which is the whole point — neither
// pgx nor this package had to be told about the other.
//
// Persistence methods take a Runner as their first argument after ctx. A method that opens its
// own transaction instead has decided, on its caller's behalf, that its work cannot be part of
// something larger, and that decision belongs to the caller.
type Runner interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Compile-time proof that the two implementations that matter satisfy Runner. Without these
// the mismatch would surface at the first call site rather than here.
var (
	_ Runner = (*pgxpool.Pool)(nil)
	_ Runner = (pgx.Tx)(nil)
)

// InTx runs fn inside a transaction, committing when it returns nil and rolling back otherwise.
//
// It takes a *pgxpool.Pool rather than an interface deliberately. pgx.Tx also has a Begin
// method — it opens a savepoint — so an interface here would let InTx be handed a transaction
// and quietly nest. Docs/10 §3.2 forbids nesting: a method that needs a transaction takes a
// Runner and trusts its caller. Requiring the concrete pool makes that rule a compile error
// rather than a review comment.
//
// Rollback is attempted on a context that cannot be cancelled. The case this guards is a client
// disconnecting mid-request: the request context is already done, and a rollback issued on it
// would not reach the server, leaving the transaction to be cleaned up on connection close.
//
// A panic is rolled back and then re-panicked, so httpx.Recover still sees it and the request
// still becomes a 500 with its request ID attached.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(context.Context, Runner) error) (err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("db: begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			panic(p)
		}
		if err != nil {
			// The rollback error is deliberately not surfaced. Whatever fn returned is
			// what the caller needs to see, and replacing it with a rollback failure
			// would hide the cause behind its consequence.
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	if err = fn(ctx, tx); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("db: commit transaction: %w", err)
	}
	return nil
}

// PoolOptions are the connection-pool settings, taken as primitives rather than as the config
// struct.
//
// internal/logging takes the same approach and says why: a dependency from here into
// configuration would put config underneath every domain in the import graph, which is the
// kind of edge SHIP-11's lint exists to prevent.
type PoolOptions struct {
	// MaxConns caps open connections. From DATABASE_MAX_OPEN_CONNS.
	MaxConns int

	// MinConns is the number kept warm. From DATABASE_MAX_IDLE_CONNS.
	//
	// The mapping is approximate, and worth being honest about: database/sql keeps at most
	// that many idle connections, whereas pgxpool keeps at least this many open. The
	// intent — a warm pool that does not reconnect on every request — is the same, and the
	// numbers are of the same order, so the configuration key keeps its meaning even
	// though the mechanism differs.
	MinConns int

	// MaxConnLifetime retires a connection after this long, so a rolling database
	// failover is not defeated by connections that never close.
	MaxConnLifetime time.Duration
}

// Open builds a connection pool and verifies it can reach the database.
//
// It pings before returning. A pool constructs lazily, so without this the first sign of a bad
// DATABASE_URL would be a failing request rather than a failing startup — and a service that
// starts healthy and then serves errors is much harder to diagnose than one that refuses to
// start. Redis is treated differently on purpose (see cmd/api): the idempotency middleware
// fails closed and the service is still useful for reads without it, whereas nothing works
// without PostgreSQL.
func Open(ctx context.Context, url string, opts PoolOptions) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_URL: %w", err)
	}

	if opts.MaxConns > 0 {
		cfg.MaxConns = int32(opts.MaxConns)
	}
	if opts.MinConns > 0 {
		cfg.MinConns = int32(opts.MinConns)
	}
	if opts.MaxConnLifetime > 0 {
		cfg.MaxConnLifetime = opts.MaxConnLifetime
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: create pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}
	return pool, nil
}

// IsUniqueViolation reports whether err is PostgreSQL's unique-constraint violation, optionally
// narrowed to one named constraint.
//
// This is how a database-enforced invariant is turned into a domain error. The one that matters
// most is SHIP-91's partial unique index: a second accepted bid on a job is refused by the
// database, and the award endpoint has to recognise that refusal and answer with a conflict
// rather than a 500. Naming the constraint keeps that check specific — a unique violation on
// some other column is a different bug and should not be reported as "already awarded".
func IsUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	if pgErr.Code != uniqueViolationCode {
		return false
	}
	return constraint == "" || pgErr.ConstraintName == constraint
}

// uniqueViolationCode is SQLSTATE 23505.
const uniqueViolationCode = "23505"

// ErrNoRows is pgx.ErrNoRows, re-exported so a domain's postgres.go can report "not found"
// without importing pgx purely for the sentinel.
var ErrNoRows = pgx.ErrNoRows
