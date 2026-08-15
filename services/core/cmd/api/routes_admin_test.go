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
