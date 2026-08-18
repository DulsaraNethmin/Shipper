// SHIP-155: the document viewer for private evidence, and the record that somebody looked.
//
// Docs/04 §3 decides that a provider's licence, registration, insurance certificate and ABN evidence
// are "collected and reviewed by an administrator" who checks each "by eye for obvious validity".
// SHIP-153 built the queue of people waiting and SHIP-154 built the decision taken about them, and
// between the two there was nothing to look at: `provider_verification_documents` existed from
// SHIP-81b and the only endpoint over it was the provider's own.
//
// # The two clauses of the *Done when* are two different guarantees and only one of them is new
//
// "Verification images render through short-lived signed URLs" is `internal/profiles`' property and
// this file inherits it rather than reimplementing it. Nothing is stored that holds a URL — the
// schema has no such column and `scripts/verify/62-profiles.sh` asserts its absence — so every
// reader pays for its own signature with its own expiry, and an administrator's link stops working
// on the same clock a provider's does. What this file adds is a *second reader* on a different
// credential, not a second mechanism.
//
// "…and are access-logged" is the new one, and it is the reason this is a service rather than a
// handler calling a port. A signed URL cannot be revoked: once it is issued, the holder can fetch a
// photograph of somebody's driver licence until it expires, and the platform will never hear about
// it. **So the entry written here is the only durable record that a particular administrator was
// handed that credential.** Docs/04 §9 requires "private storage of verification evidence" as an
// internal control, and a store that is private to everyone except an unlogged console is not one.
//
// # The entry is written in the transaction that performs the read
//
// This is verifications.go's rule applied to a read, and it means what it says: a viewer whose
// access entry cannot be written renders nothing at all. The instinct to run the other way — show
// the images, log best-effort — is the one to resist, because it makes the failure invisible in
// exactly the circumstances that produce it. [Auditor.Record] takes the transaction, so a refusal
// aborts the read.
//
// [TestTheDocumentViewerRendersNothingWhenItsAccessEntryCannotBeWritten] takes that branch. It is
// there because this is the clause of this ticket that can be met in name only: an implementation
// that returns the documents and then writes an entry it ignores the error of passes every other
// test in this file.
//
// # Holding a transaction across the signing is safe, and it is not obvious
//
// [ProviderEvidence.EvidenceFor] mints a URL per document, and `profiles.Documents.Submit`
// deliberately refuses to hold a transaction across a call to the object store — "a pool connection
// hostage to that service's worst day". This is not that call. Presigning is an HMAC over a string
// the platform already holds: `internal/platform/storage`'s PresignDownload makes no request, takes
// no context deadline that matters and cannot block on the store being slow. The call that does
// reach the network is `Stored`, and nothing here makes one.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// Evidence serves Docs/04 §3's document images to a reviewer, and records that they were served.
type Evidence struct {
	documents ProviderEvidence
	auditor   *Auditor

	pool *pgxpool.Pool
}

// NewEvidence builds the viewer.
//
// The pool may be nil, which every constructor in this package accepts: the process starts with an
// unreachable database on purpose, and the endpoints answer [ErrAdminUnavailable] until it returns.
//
// The port and the auditor may not, and the second refusal is the one that matters. A nil auditor
// would be caught by [Auditor.Record] at the moment somebody opened a provider's licence, which is
// precisely when this service must not be discovering its own wiring — and the failure mode a
// missing auditor produces is an unlogged read, which is the thing this ticket exists to prevent.
// [NewVerifications] refuses one for the same reason and records it.
func NewEvidence(documents ProviderEvidence, auditor *Auditor, pool *pgxpool.Pool) (*Evidence, error) {
	if documents == nil {
		return nil, errors.New("admin: the document viewer needs a source of verification " +
			"evidence; without one every provider's file reads as empty, which is what a " +
			"provider who has submitted nothing looks like")
	}
	if auditor == nil {
		return nil, errors.New("admin: the document viewer needs the audit writer; a signed URL " +
			"to somebody's identity documents cannot be revoked, so the entry recording who " +
			"was given one is the only durable record there is")
	}

	return &Evidence{documents: documents, auditor: auditor, pool: pool}, nil
}

// For is one provider's submitted evidence, each image with a freshly signed URL (SHIP-155).
//
// # The refusals, in this order
//
//  1. a request naming nobody is [ErrVerificationNotFound] — there is no identifier to look up and
//     the nil UUID is a value rather than an absence;
//  2. a read that cannot name the administrator taking it is a wiring fault, not a refusal a client
//     can act on: the entry would have nobody to attribute the access to, and an unattributed
//     access record is the failure this ticket exists to prevent;
//  3. no database is [ErrAdminUnavailable];
//  4. a provider with no verification record is [ErrVerificationNotFound].
//
// # A provider who has submitted nothing is an answer, and it still writes an entry
//
// An empty file is the ordinary state of every provider on the day they register, and it is exactly
// what a reviewer about to reject somebody for supplying no evidence needs to be able to see. It is
// therefore not collapsed into (4), and the access is recorded the same way: "an administrator
// opened this person's file" is the fact, and whether there was anything in it is metadata.
//
// **A refused read writes nothing**, which is the same asymmetry [Verifications.Decide] takes about
// a refused decision: an entry for a read that did not happen would record a review of a file
// nobody was shown, in a table with no way to take it back.
func (e *Evidence) For(ctx context.Context, providerID, actorID uuid.UUID) ([]EvidenceDocument, error) {
	if providerID == uuid.Nil {
		return nil, ErrVerificationNotFound
	}
	if actorID == uuid.Nil {
		return nil, errors.New(
			"admin: reading a provider's verification evidence must name the administrator reading it")
	}
	if e.pool == nil {
		return nil, ErrAdminUnavailable
	}

	var documents []EvidenceDocument
	err := db.InTx(ctx, e.pool, func(ctx context.Context, tx db.Runner) error {
		found, exists, err := e.documents.EvidenceFor(ctx, tx, providerID)
		if err != nil {
			return fmt.Errorf("admin: reading the verification evidence of %s: %w", providerID, err)
		}
		if !exists {
			return ErrVerificationNotFound
		}

		// The error is returned, never logged and swallowed. See the file header — this is the
		// statement that makes "access-logged" a guarantee rather than an intention.
		if _, err := e.auditor.Record(ctx, tx, AuditEntry{
			Actor:  AdminActor(actorID),
			Action: AuditActionVerificationEvidenceViewed,

			// The **provider**, as a `users` row, so that a support query asking
			// "everything that happened to this account" returns who looked at their
			// documents beside the decisions taken about them.
			TargetType: AuditTargetUser,
			TargetID:   providerID,

			// Docs/04 §6.6 asks a moderation record to carry its "evidence reference", and
			// this is the entry that can carry one: the identifiers of the images the
			// administrator was actually handed links to, on the read that handed them
			// over. A later question — "was the insurance certificate current when this was
			// reviewed?" — is answerable from this and from nothing else, because the
			// newest row of a kind moves when the provider retakes it.
			//
			// The count is beside the list rather than derived from it, so a reader
			// filtering the trail for "opened a file that was empty" does not have to
			// measure an array.
			Metadata: map[string]any{
				"document_count": len(found),
				"document_ids":   evidenceIDs(found),
			},
		}); err != nil {
			return err
		}

		documents = found
		return nil
	})
	if err != nil {
		return nil, err
	}
	return documents, nil
}

// evidenceIDs is the identifiers of the images this read handed over, as strings.
//
// Strings rather than uuid.UUID values, because this goes into `audit_log.metadata` as jsonb and a
// UUID has no JSON form of its own — marshalling one produces the string anyway, and doing it here
// means the shape a reader gets back is decided in the file that decided to record them.
//
// A non-nil empty slice, so an entry for a provider who has submitted nothing carries `[]` rather
// than `null`: a reader indexing into the metadata should never have to check which of the two the
// writer meant, which is [AuditEntry.Metadata]'s own argument one level down.
func evidenceIDs(documents []EvidenceDocument) []string {
	out := make([]string, 0, len(documents))
	for _, d := range documents {
		out = append(out, d.ID.String())
	}
	return out
}
