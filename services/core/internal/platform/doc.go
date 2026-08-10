// Package platform is the root of the integration adapter tree. It holds no code itself;
// each subdirectory wraps one external service.
//
// # The test an adapter has to pass
//
// Docs/06 §4.1 applies the adapter pattern selectively, and the test is narrow on
// purpose: does a second implementation exist *today*? Not "might one exist someday" —
// that question always answers yes and justifies any abstraction. A seam built for an
// implementation that does not exist is a guess, and it tends to be wrong in exactly the
// way that matters when the real second implementation finally arrives.
//
// Five integrations pass the test today:
//
//	email/       console in development, provider in staging and production
//	sms/         console in development, provider in staging and production
//	push/        Firebase Cloud Messaging, plus a no-op used in tests
//	storage/     local filesystem in development, S3 deployed
//	geocoding/   provider-backed, with a stub for tests
//
// Payments and third-party identity verification have no implementation yet. Both are
// named commitments for later phases (Docs/06 §6), and their interfaces get written when
// the domain that needs them is built — so the seam exists before the vendor does, and
// nothing here has to guess its shape now.
//
// # PostgreSQL is not in this tree, and will not be
//
// PostgreSQL is the source of truth by decision, not by circumstance (Docs/06 §4.1). The
// two mechanisms holding award correctness together — the partial unique index enforcing
// one accepted bid per job, and the row locking in the award transaction — are both
// PostgreSQL-specific and both load-bearing. Neither survives an abstraction designed to
// keep the database swappable, and losing either converts a database-enforced guarantee
// into an application-level hope.
//
// Persistence therefore lives in each domain's own postgres.go, concrete and unwrapped.
// Where the goal is testable domain logic, a real PostgreSQL instance in tests gives more
// fidelity than a mock ever could: a mock happily accepts the write that the actual
// constraint exists to reject.
//
// # Which way the imports point
//
// Interfaces are declared by the domain that consumes them, never by the package that
// implements them. jobs/ports.go says what jobs needs from geocoding; this tree knows
// nothing about jobs. Go's structural interface satisfaction means an adapter never
// imports the domain it serves, and the two are joined in cmd/api rather than by a
// dependency edge.
//
// That is not a convention — SHIP-11's import lint fails the build in both directions,
// so the architectural rule and the automated check are the same rule expressed twice.
package platform
