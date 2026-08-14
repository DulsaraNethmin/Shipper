// SHIP-148: what an administrator may do, and the reason the answer is "as little as possible".
//
// The *Done when* is "permissions are granular and default to the minimum", and those are two
// separate claims that fail in two separate ways.
//
// **Granular** means the check at each endpoint names the *action*, not the role. A handler that
// asked `if role == owner` would make every future permission a change to that condition, and the
// condition would drift towards the role that already worked. So a role is a named bundle and the
// check is per permission — [Grant.Permits] takes a [Permission] and there is no exported way to
// ask what role somebody has for the purpose of deciding anything.
//
// **Default to the minimum** means three things, in three places, and no one of them is enough:
//
//   - a command with no role stated becomes [RoleSupport] ([Credentials.Create]);
//   - a row written with no role stated becomes 'support' (`000801`'s column default), which covers
//     the migrations and support scripts that never come through Go;
//   - and a role with no entry in [rolePermissions] grants **nothing**, which is what makes adding
//     a fourth role a deliberate act rather than an accident that inherits somebody else's bundle.
//
// # Why this is a Go table and not a grant table
//
// `Docs/06` §5.3 puts "anything expected to change under operational pressure" server-side and
// changeable — category lists, validation limits, policy copy. **A permission model is the
// opposite of that.** What a role may do is a security control, and a control an operator can widen
// with an `UPDATE` is one that gets widened at 2am by somebody who needs a queue to load. In a Go
// table it changes by review, in a diff, beside the reasoning.
//
// It also makes the audit invariant expressible. There is no permission below that authorises
// deleting an audit entry, and there is no row anybody could insert to create one.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"slices"
	"sort"
)

// Permission is one thing an administrator may do.
//
// Named `<subject>.<verb>`, which is not the error codes' convention and is deliberately different
// from it: an error code is a string a client branches on and these are never published to one.
// What they are is a list a person reads when deciding what a role should hold, and grouping by
// subject is what makes "everything about users" answerable by looking.
type Permission string

// The catalogue. Every permission the platform has, grouped by subject, and closed.
//
// Each names a ticket, because a permission with no endpoint behind it is a decision nobody has had
// to make yet — and the alternative, adding them one at a time as the endpoints land, is how a
// permission model ends up shaped by the order the work happened in rather than by what an
// administrator does.
//
// **There is deliberately no permission to delete an audit entry, and there will not be one.**
// CLAUDE.md's invariant is that audit entries are append-only and ordinary administrators cannot
// delete them; `000003` enforces it with a trigger that refuses `UPDATE` and `DELETE` from any
// connection, which is what makes it true of a `psql` prompt as well as of this service.
// [TestNoPermissionAuthorisesDeletingAnAuditEntry] holds both halves. A permission here would be the
// first step towards an endpoint, and an endpoint would be an application-level rule standing where
// a database-level one already stands.
const (
	// PermissionUsersRead is searching and opening user accounts (SHIP-151).
	PermissionUsersRead Permission = "users.read"

	// PermissionUsersRestrict is restricting or suspending one, with a recorded reason
	// (SHIP-161). A *permanent* suspension additionally needs a second administrator's approval
	// (SHIP-166), which is a rule about the action rather than about who holds this.
	PermissionUsersRestrict Permission = "users.restrict"

	// PermissionJobsRead is searching and opening any job with its bids and status history
	// (SHIP-152).
	PermissionJobsRead Permission = "jobs.read"

	// PermissionJobsUnpublish is removing a policy-breaching job (SHIP-160).
	PermissionJobsUnpublish Permission = "jobs.unpublish"

	// PermissionVerificationsRead is the pending-verification queue (SHIP-153).
	PermissionVerificationsRead Permission = "verifications.read"

	// PermissionVerificationsDecide is recording Verified, Restricted, Rejected or Suspended
	// against a provider (SHIP-154).
	PermissionVerificationsDecide Permission = "verifications.decide"

	// PermissionModerationRead is the moderation queues of Docs/04 §5 (SHIP-117, SHIP-156…159).
	PermissionModerationRead Permission = "moderation.read"

	// PermissionDisputesRead is reading a dispute and its evidence (SHIP-164).
	PermissionDisputesRead Permission = "disputes.read"

	// PermissionDisputesResolve is recording an outcome, which unfreezes the job (SHIP-164).
	PermissionDisputesResolve Permission = "disputes.resolve"

	// PermissionNotesWrite is attaching an internal support note to a user or a job (SHIP-162).
	// Notes are never user-visible, so there is no corresponding read permission for anyone
	// outside the console.
	PermissionNotesWrite Permission = "notes.write"

	// PermissionAuditRead is reading the immutable history (SHIP-165).
	//
	// **The only audit permission there is.** See the note above the block.
	PermissionAuditRead Permission = "audit.read"

	// PermissionAdminsManage is creating administrator accounts and setting their roles
	// (SHIP-147, SHIP-148).
	//
	// The permission that grants permissions, and therefore the one that makes every other
	// boundary in this file real or advisory. Only [RoleOwner] holds it.
	PermissionAdminsManage Permission = "admins.manage"
)

// Permissions is the whole catalogue, in the order the constants declare it.
//
// Ordered by subject rather than alphabetically, so that the list reads as the console's surface.
// It is what [TestEveryPermissionConstantIsInTheCatalogue] compares against — a constant declared
// and left out of this slice would be a permission no role could ever hold and which
// [Permission.Valid] would refuse, while every other test passed.
var Permissions = []Permission{
	PermissionUsersRead,
	PermissionUsersRestrict,
	PermissionJobsRead,
	PermissionJobsUnpublish,
	PermissionVerificationsRead,
	PermissionVerificationsDecide,
	PermissionModerationRead,
	PermissionDisputesRead,
	PermissionDisputesResolve,
	PermissionNotesWrite,
	PermissionAuditRead,
	PermissionAdminsManage,
}

// Valid reports whether p is in the catalogue.
func (p Permission) Valid() bool { return slices.Contains(Permissions, p) }

// String is the name, which is what a log record and a refusal's cause carry.
func (p Permission) String() string { return string(p) }

// rolePermissions is what each role holds.
//
// # It is written out per role rather than composed
//
// `moderator` is not "support plus five", and `owner` is not "moderator plus one", even though both
// sentences are true today. Composition would mean a permission added to `support` silently reaching
// `owner`, which is the direction that matters — a widening nobody reviewed, arriving through a list
// they were not looking at. Three explicit lists cost a dozen lines and make every grant visible
// beside the role that holds it.
//
// # A role with no entry holds nothing
//
// The zero [Role], a role read out of a database that has drifted from `ck_admin_users_role`, and a
// fourth role somebody adds to the vocabulary and forgets here all reach the same place: a nil slice
// and a [Grant.Permits] that answers false to everything. That is what "default to the minimum"
// means at its limit — the minimum is nothing, and the pairing test in `migrations` is what stops
// the third case being silent.
var rolePermissions = map[Role][]Permission{
	// Read-only across the console. Docs/01 §4.6's first two capabilities — search, and review
	// verification status — plus reading the queues and the trail. Everything here is a
	// permission to *look*.
	RoleSupport: {
		PermissionUsersRead,
		PermissionJobsRead,
		PermissionVerificationsRead,
		PermissionModerationRead,
		PermissionDisputesRead,
		PermissionAuditRead,
	},

	// Support, plus acting on what it can see: Docs/04 §6's step 4 outcomes, and Docs/01 §4.6's
	// remaining capabilities other than managing administrators.
	RoleModerator: {
		PermissionUsersRead,
		PermissionUsersRestrict,
		PermissionJobsRead,
		PermissionJobsUnpublish,
		PermissionVerificationsRead,
		PermissionVerificationsDecide,
		PermissionModerationRead,
		PermissionDisputesRead,
		PermissionDisputesResolve,
		PermissionNotesWrite,
		PermissionAuditRead,
	},

	// Moderator, plus managing administrator accounts.
	//
	// **Not "everything".** It is a listed set like the other two, so a permission added to the
	// catalogue later reaches this role by somebody writing it here — which is a line in a diff
	// rather than a consequence of a wildcard nobody re-read.
	RoleOwner: {
		PermissionUsersRead,
		PermissionUsersRestrict,
		PermissionJobsRead,
		PermissionJobsUnpublish,
		PermissionVerificationsRead,
		PermissionVerificationsDecide,
		PermissionModerationRead,
		PermissionDisputesRead,
		PermissionDisputesResolve,
		PermissionNotesWrite,
		PermissionAuditRead,
		PermissionAdminsManage,
	},
}

// Permissions is what this role holds, sorted, as a fresh slice.
//
// A copy rather than the stored slice, because the stored one is a package-level variable and a
// caller that appended to what it was handed would be widening a role for the whole process. That is
// not a hypothetical shape — it is exactly how a response builder that adds "and also this one for
// the current user" goes wrong.
func (r Role) Permissions() []Permission {
	held := rolePermissions[r]
	out := make([]Permission, len(held))
	copy(out, held)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Holds reports whether this role holds p.
//
// **Default deny, and the whole function is that sentence.** An unrecognised role holds nothing
// because the map has no entry for it; an unrecognised permission is held by nobody because no
// entry lists it. There is no branch that answers true for a reason other than "this exact
// permission is in this exact role's list".
func (r Role) Holds(p Permission) bool { return slices.Contains(rolePermissions[r], p) }

// Permits reports whether the administrator this request is being served for may do p.
//
// The only authorisation question anything outside this file asks. **It reads the role from the
// grant, which the guard read from the database on this request** — so a demotion takes effect on
// the next call rather than at the next expiry, which is the whole reason the credential is a row
// (000801, SHIP-147).
//
// A disabled account cannot reach here: [Authenticator.Resolve] refuses one before a grant exists.
// It is still checked, because a Grant is an ordinary struct and a future caller could construct
// one — and "this administrator is out of service" must not depend on where the value came from.
func (g Grant) Permits(p Permission) bool {
	return g.Administrator.CanSignIn() && g.Administrator.Role.Holds(p)
}
