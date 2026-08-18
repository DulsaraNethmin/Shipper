package main

import (
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/admin"
)

// SHIP-150 read from the side `internal/admin` cannot see.
//
// The domain's own TestEveryAdminMutationWritesAnAuditEntry proves that each mutation it knows about
// writes an entry, and it is the stronger of the two tests — it drives real handlers against a real
// database. What it cannot do is notice a *route*. A domain adds `cmd/api/routes_<domain>.go` and
// nothing in the package it serves from names the routes, so an administrative endpoint could be
// declared here, served, and never appear in that suite at all.
//
// This is the other half: the manifest is the served surface, and every state-changing thing on it
// that an administrator can reach has to be an action the audit catalogue knows about.

// auditedAdminMutations is every state-changing administrative route, with the entry it writes.
//
// **Adding a row here is not the same as auditing the route**, and nobody should read it that way.
// This list is a tripwire: it makes a new administrative mutation impossible to add *silently*, by
// failing the build until somebody writes down which audit action it produces. Whether the entry is
// actually written is [admin.AuditActions] paired with the domain's own suite, which drives the
// handler and reads the row back.
//
// The pairing is what makes the two halves add up. A route listed here whose action is not in the
// catalogue fails below; an action in the catalogue that no mutation exercises fails in the domain.
// So an endpoint cannot reach the served surface with no entry behind it, and an entry cannot be
// declared with nothing writing it.
var auditedAdminMutations = map[string]admin.AuditAction{
	// SHIP-147. Public, because an endpoint that hands out a credential cannot require one —
	// and audited for the same reason it is rate limited: it is the moment somebody becomes able
	// to act, and every later entry names an administrator whose presence only this records.
	"POST /v1/admin/sessions": admin.AuditActionAdministratorSignedIn,

	// SHIP-147.
	"DELETE /v1/admin/sessions/current": admin.AuditActionAdministratorSignedOut,

	// SHIP-147, SHIP-148. The most privileged action the console has.
	"POST /v1/admin/administrators": admin.AuditActionAdministratorCreated,

	// SHIP-160. The first audited mutation whose target is not an administrator — the entry
	// names the job, and the same reason is written into `job_status_history` as well, because
	// the two tables answer different questions to different readers.
	"POST /v1/admin/jobs/{id}/unpublish": admin.AuditActionJobUnpublished,

	// SHIP-161. One action for all three standings including reinstatement, which is what this
	// table's one-action-per-route rule and TestTheAuditedMutationsAreDistinctActions require —
	// and independently the right shape, because "what has happened to this account's standing"
	// is one question a reader should be able to ask with one filter.
	"POST /v1/admin/users/{id}/standing": admin.AuditActionUserStandingChanged,

	// SHIP-162. The entry names the **subject** of the note rather than the note, so that a
	// search for everything that happened to an account returns the notes taken about it. The
	// note's identifier is in the metadata; the body deliberately is not.
	"POST /v1/admin/notes": admin.AuditActionNoteAdded,

	// SHIP-166, and the two rows are why Docs/04 §9's control is auditable rather than merely
	// enforced. A two-person review that recorded only its approval would name one of the two
	// people, and the request is the moment the case was made — with the reason the second
	// administrator was asked to agree with.
	"POST /v1/admin/users/{id}/suspension": admin.AuditActionUserSuspensionRequested,

	// The approval carries **both** administrators: the actor is the one who agreed, and
	// `requested_by` is in the metadata. A control whose trail names one participant has not
	// recorded what happened.
	"POST /v1/admin/suspensions/{id}/approval": admin.AuditActionUserSuspensionApproved,

	// SHIP-154. One action for all five of Docs/04 §4's outcomes, which is what this table's
	// one-action-per-route rule requires and independently the right shape: "what has happened
	// to this provider's eligibility" is one question, and a reader filtering by action should
	// get the whole history rather than having to know which of five verbs to ask for.
	//
	// **The reason is written into two tables and neither is redundant.**
	// `provider_verification_decisions` is the provider's evidence trail (Docs/04 §1) and this
	// entry is the administrator's accountability record (Docs/04 §9) — the same split SHIP-160
	// makes between `job_status_history` and `audit_log`.
	//
	// `GET /v1/admin/verifications` is not here and does not need to be: it is a read, and
	// Docs/01 §5.1 asks for audit logs of **privileged actions**. An entry per queue load would
	// bury the decisions in the reads.
	//
	// `GET /v1/admin/verifications/{id}/documents` is a read that *does* write an entry
	// (SHIP-155), and it is in [auditedAdminReads] rather than here — this table is the mutating
	// half of the surface and its reverse check would report a read listed here as unserved. The
	// two tables are checked against each other for a shared action.
	"POST /v1/admin/verifications/{id}/decision": admin.AuditActionVerificationDecided,

	// SHIP-164. One action for all five of Docs/04 §7's outcomes, which is what this table's
	// one-action-per-route rule requires and independently the right shape.
	//
	// **The entry names the job rather than the dispute**, so that a search for everything that
	// happened to a job returns the resolution beside the unpublish and the notes; the dispute's
	// identifier is in the metadata, along with **both** vocabularies — Docs/04 §7's outcome and
	// Docs/02 §2's destination, which `000804` keeps orthogonal and which are two independent
	// facts about one decision.
	//
	// `GET /v1/admin/disputes` and `GET /v1/admin/disputes/{id}` are not here and do not need to
	// be: both are reads, and Docs/01 §5.1 asks for audit logs of **privileged actions**. An
	// entry per queue load would bury the resolutions in the reads.
	//
	// `POST /v1/jobs/{id}/disputes` is still deliberately outside this table — see
	// [administrativeMutation]. Intake is a party reporting a problem with their own delivery;
	// this is an administrator deciding what happens to it.
	"POST /v1/admin/disputes/{id}/resolution": admin.AuditActionDisputeResolved,
}

// auditedAdminReads is every administrative *read* that writes an audit entry, with the entry.
//
// **There is one, and the list exists so that there goes on being a reason for each.** Docs/01 §5.1
// asks for audit logs of privileged actions; reading a queue or a history is not one, and an entry
// per read would bury the actions in the reads. `admin.AuditActions`' own header states that rule
// and states the exception, and both halves matter — a reader who takes "mutations only" as the
// whole rule will delete the exception, and a reader who takes the exception as permission will
// audit every GET the console serves.
//
// It is a **separate table from [auditedAdminMutations] rather than a row in it**, and that is
// forced rather than tidy: [TestEveryMutatingAdminRouteIsAudited] checks its table in both
// directions, and its reverse direction only ever marks *mutating* routes as served — so a read
// listed there would fail as "listed as an audited administrative mutation and is not served", on a
// tree where nothing is wrong.
var auditedAdminReads = map[string]admin.AuditAction{
	// SHIP-155. Docs/04 §3's document images, rendered for the administrator who reviews them
	// "by eye for obvious validity".
	//
	// **What makes this different from every other read here is what the response contains.**
	// `GET /v1/admin/verifications` discloses that somebody is waiting; this hands the caller
	// short-lived signed URLs to photographs of that person's driver licence, vehicle
	// registration and insurance certificate. Nothing revokes a pre-signed URL, so the entry
	// written on this request is the only durable record that a particular administrator was
	// given those links — and Docs/04 §9 requires private storage of verification evidence as an
	// internal control, which a store private to everyone except an unlogged console is not.
	//
	// The entry is written in the transaction that performs the read, so a viewer whose entry
	// cannot be written renders nothing. `admin.Evidence.For` records the argument and the
	// domain's own suite takes the branch.
	"GET /v1/admin/verifications/{id}/documents": admin.AuditActionVerificationEvidenceViewed,
}

// TestEveryAuditedAdminReadIsServedAndDistinct.
//
// The same pairing [TestEveryMutatingAdminRouteIsAudited] puts on the mutating half, in the one
// direction that is checkable here: a read listed above must be on the served surface, must declare
// an action the catalogue knows, and must not share that action with anything else.
//
// **The other direction — "no unlisted administrative read writes an entry" — is not checkable from
// the route table**, and pretending otherwise would be worse than saying so: a route's declaration
// carries its method, its path and its auth class, and nothing about whether the handler behind it
// writes. What closes that side is `admin.AuditActions` paired with the domain's own
// `adminMutations` table, which drives every handler that writes an entry and reads the row back.
func TestEveryAuditedAdminReadIsServedAndDistinct(t *testing.T) {
	served := map[string]bool{}
	for _, r := range routes() {
		if r.mutating() {
			continue
		}
		served[r.Method+" "+r.fullPath()] = true
	}

	seen := map[admin.AuditAction]string{}
	for key, action := range auditedAdminReads {
		if !served[key] {
			t.Errorf("%s is listed as an audited administrative read and is not served as a "+
				"read.\nRemove the row, or restore the route.", key)
		}
		if !action.Valid() {
			t.Errorf("%s is recorded as writing %q, which is not in admin.AuditActions",
				key, action)
		}
		if first, ok := seen[action]; ok {
			t.Errorf("%s and %s both write %q; a reader cannot tell which happened", first, key, action)
		}
		seen[action] = key

		if mutation, ok := auditedAdminMutations[key]; ok {
			t.Errorf("%s is listed as both an audited read and an audited mutation (%q); it is "+
				"one or the other", key, mutation)
		}
	}

	for key, action := range auditedAdminMutations {
		if read, ok := seen[action]; ok {
			t.Errorf("%s and %s both write %q; one action per route", read, key, action)
		}
	}
}

// administrativeMutation reports whether this route is a state change an administrator makes.
//
// Two conditions rather than one, because neither alone is right. **The auth class alone misses
// sign-in**, which is `Public` — it has to be, it is where the credential comes from — and which is
// exactly the route somebody would most want in the trail. **The path prefix alone would catch a
// route in the `/v1/admin/` tree served to somebody else**, which nothing does today and which is a
// shape a future ticket could want.
//
// `POST /v1/jobs/{id}/disputes` is deliberately outside both: it is `RequireUser`, served by
// `internal/admin` because the rules are that domain's, and called by a customer or a provider
// reporting a problem with their own delivery. Docs/01 §5.1 asks for audit logs of **privileged**
// actions, and intake is not one — see the note on admin.AuditActions.
func administrativeMutation(r Route) bool {
	return r.mutating() &&
		(r.Auth == RequireAdmin || strings.HasPrefix(r.fullPath(), apiPrefix+"/admin/"))
}

// TestEveryMutatingAdminRouteIsAudited.
//
// SHIP-150's *Done when* is that **all** admin mutations write an audit entry, and "all" is a claim
// about the served surface rather than about the handlers somebody thought to test. A route declared
// in routes_admin.go with no audit behind it produces no compile error, no failing handler test and
// no wrong answer — only a privileged action that leaves no record, which is the one failure this
// table cannot recover from afterwards: `Docs/09` lists audit among the things that must not be cut
// because it is impossible to backfill.
func TestEveryMutatingAdminRouteIsAudited(t *testing.T) {
	served := map[string]bool{}

	for _, r := range routes() {
		if !administrativeMutation(r) {
			continue
		}

		key := r.Method + " " + r.fullPath()
		served[key] = true

		action, listed := auditedAdminMutations[key]
		if !listed {
			t.Errorf("%s changes state behind an administrator credential and no audit action "+
				"is recorded for it.\n"+
				"SHIP-150: every privileged action writes an entry, in the transaction that "+
				"performs it. Write the entry in the domain, add the action to "+
				"admin.AuditActions, exercise it in the domain's adminMutations table, and "+
				"list it in auditedAdminMutations in %s.", key, "routes_admin_test.go")
			continue
		}
		if !action.Valid() {
			t.Errorf("%s is recorded as writing %q, which is not in admin.AuditActions",
				key, action)
		}
	}

	// The other direction. A route removed or renamed leaves a stale row here, which would go on
	// asserting something about an endpoint nobody serves — and the next person to read this list
	// would take it as a description of the surface.
	for key := range auditedAdminMutations {
		if !served[key] {
			t.Errorf("%s is listed as an audited administrative mutation and is not served.\n"+
				"Remove the row, or restore the route.", key)
		}
	}
}

// TestTheAuditedMutationsAreDistinctActions.
//
// One action per route. Two routes sharing one would make a trail in which "an administrator was
// created" could have come from either, which is precisely the ambiguity a stable action identifier
// exists to prevent (`000003`).
func TestTheAuditedMutationsAreDistinctActions(t *testing.T) {
	seen := map[admin.AuditAction]string{}

	for key, action := range auditedAdminMutations {
		if first, ok := seen[action]; ok {
			t.Errorf("%s and %s both write %q; a reader cannot tell which happened",
				first, key, action)
		}
		seen[action] = key
	}
}
