// SHIP-151: the statement behind the administrator's account search.
//
// A method on the same unexported [postgresStore] as the rest of this domain's SQL (Docs/10 §2.2).
//
// # This file selects from `users`, which postgres.go said this domain had no reason to
//
// It said `users` is "shared and readable, but this domain has no reason to" — true when it was
// written and no longer. `users` is in the shared migration block precisely because most of the
// service reads it (`migrations/blocks.go`), and `internal/jobs`, `internal/fleet` and
// `internal/delivery` each select from it in their own stores. What belongs in cmd/api is a query
// spanning **two other domains'** tables — [JobParties] joins `jobs` and `bids`, the exception queue
// joins `proofs`, `milestones` and `jobs` — and this one spans none: it is a single table, read by
// the domain whose console needs it.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// userColumns is every column of an account the console sees, in the order [scanUser] reads them.
//
// **Deliberately without `password_hash`.** The same arrangement [administratorColumns] describes: a
// credential is selected only by the one statement that verifies it, so a new caller reaching for
// "the user columns" cannot pull a hash into a struct that is logged or serialised. There is no such
// statement in this package at all — `admin` never verifies a user's password.
//
// `coalesce(name, ”)` is the only expression in the list (SHIP-30a). `users.name` is nullable
// because accounts predating `000006` have none and a name cannot be invented for them, while
// [UserRecord.Name] is a plain string; the coalesce turns "no name" into an empty field rather than a
// NULL [scanUser] would have to take through a pointer. `internal/identity`s projection does the
// same, and the two agreeing is what makes the console show what registration stored.
const userColumns = `id, coalesce(name, '') AS name, email, phone, role, status, ` +
	`email_verified_at, phone_verified_at, created_at`

// searchUsers returns one page of accounts matching the query, newest first.
//
// # The predicate, and why each half is written the way it is
//
// `$1 = ” OR (email LIKE … OR phone LIKE …)` rather than two statements or a string built by
// concatenation. One statement means one plan and one place for the disclosure rules to be read;
// building SQL from the query would be the injection this parameterisation exists to make
// impossible.
//
// **The term is escaped before it becomes a LIKE pattern.** A caller sending `%` would otherwise
// match every account, and one sending `_` would match any single character — neither is what
// somebody typing into a search box means, and the first is a full table read on demand. See
// [likeContains].
//
// **The address, the name and the number are matched differently, and that is not tidiness.**
// `email` is citext and `name` is text; both take the term as typed, case-insensitively, through the
// same `ILIKE` pattern — a person searching for "alice" means the address and the name equally, and
// two patterns for one typed term would be two things to keep in step. A phone number is not a
// string somebody types the way it is stored: registration normalises to E.164, so the account
// whose owner writes `0419 312 345` on a support ticket is stored as `+61419312345` — and a
// substring match of the first against the second finds nothing. [phonePattern] is what closes
// that, and the empty pattern is what stops a term with no digits in it matching every number in
// the table.
//
// **`name` is nullable and the predicate needs nothing extra for that.** `NULL ILIKE '%x%'` is NULL
// rather than false, and an OR with a NULL branch is decided by its other branches — so an account
// registered before `000006` is matched by its address and never by a name it does not have, which
// is what should happen. A `coalesce` in the predicate would say the same thing and would cost the
// column its ability to use an index if one is ever added.
//
// # The ordering is total, and the cursor is why
//
// `(created_at DESC, id DESC)` rather than `created_at` alone: two accounts registered in the same
// millisecond would make a single-column cursor either skip one or repeat it, and a search that can
// hide an account is worse than one that shows it twice. The comparison is `<` because the order is
// descending — the next page is what was created *before* the last row of this one.
//
// # What this deliberately does not do
//
// It does not join `jobs` and it never will. A customer's budget is never exposed in any form
// (Docs/01 §4.3), and an administrative search that reached into the commercial side of the platform
// would be a new place for it to escape; opening a user's jobs is SHIP-152's endpoint with its own
// disclosure decisions.
//
// # Scale
//
// A leading-wildcard LIKE cannot use `uq_users_email`, so this is a sequential scan bounded by the
// LIMIT and by [maxSearchTermLength]. That is the right trade at pilot volume and is not a permanent
// answer: the fix is a trigram index, which is a migration against `users` in the **shared** block
// and therefore a request rather than something this ticket takes (Docs/11 §6 strikes SHIP-169 for
// the same reason). Whoever needs it should have a row count to point at.
func (postgresStore) searchUsers(ctx context.Context, r db.Runner, q UserQuery) ([]UserRecord, error) {
	const query = `
		SELECT ` + userColumns + `
		FROM users
		WHERE ($1 = '' OR email ILIKE $2 OR name ILIKE $2 OR ($3 <> '' AND phone LIKE $3))
		  AND ($4 = '' OR status = $4)
		  AND ($5::timestamptz IS NULL OR (created_at, id) < ($5, $6))
		ORDER BY created_at DESC, id DESC
		LIMIT $7`

	// A nil rather than a zero time for the first page. `< (NULL, …)` is NULL rather than true,
	// so the predicate has to be skipped rather than satisfied, and the `$4 IS NULL` guard is
	// what does it. Passing the zero time would work today and would stop working the first time
	// somebody backdated a fixture — the exception queue records the same trap.
	var (
		after   any
		afterID any
	)
	if !q.After.Zero() {
		after, afterID = q.After.CreatedAt, q.After.UserID
	}

	rows, err := r.Query(ctx, query,
		q.Term, likeContains(q.Term), phonePattern(q.Term), q.Standing.String(),
		after, afterID, q.Limit)
	if err != nil {
		return nil, fmt.Errorf("admin: reading the account search: %w", err)
	}
	defer rows.Close()

	var out []UserRecord
	for rows.Next() {
		record, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("admin: reading the account search: %w", err)
	}
	return out, nil
}

// scanUser reads one row of [userColumns].
//
// One function rather than a copy of the Scan call at each call site, matching [scanDispute]: a
// column added to the list and not here is a scan mismatch at the first call, which is the failure
// worth having. SHIP-152 will be the second caller.
func scanUser(row interface{ Scan(...any) error }) (UserRecord, error) {
	var (
		u                                UserRecord
		emailVerifiedAt, phoneVerifiedAt *time.Time
	)

	if err := row.Scan(&u.ID, &u.Name, &u.Email, &u.Phone, &u.Role, &u.Standing,
		&emailVerifiedAt, &phoneVerifiedAt, &u.CreatedAt); err != nil {
		return UserRecord{}, fmt.Errorf("admin: reading an account: %w", err)
	}

	// NULL means not yet, which the zero time says just as well — so there is one representation
	// of "unverified" rather than a pointer every reader has to check. `000002` records why the
	// column is a timestamp rather than a boolean: it answers "since when" as well.
	if emailVerifiedAt != nil {
		u.EmailVerifiedAt = *emailVerifiedAt
	}
	if phoneVerifiedAt != nil {
		u.PhoneVerifiedAt = *phoneVerifiedAt
	}
	return u, nil
}

// likeContains turns a search term into a pattern matching it anywhere.
//
// # The escaping is the whole function, and leaving it out is not a subtle bug
//
// `%` and `_` are LIKE's wildcards and `\` escapes them. A term containing `%` would match every
// account in the table — a full read triggered by one character in a search box — and one containing
// `_` would match more than the caller asked for while looking as though it had worked. Both are
// escaped, and the backslash is escaped first so that escaping does not undo itself.
//
// An empty term returns `%`, which matches everything. It is never reached: the statement's `$1 = ”`
// branch short-circuits before the pattern is compared. Returned honestly anyway rather than as
// something that would match nothing, so that a future caller which drops the guard gets "everybody"
// rather than "nobody" — a search that silently returns nothing is the failure this domain has the
// least chance of noticing.
func likeContains(term string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(term)
	return "%" + escaped + "%"
}

// minPhoneDigits is the shortest run of digits treated as a phone number.
//
// Four. Three or fewer is inside most numbers in any table, so a term that short would return
// something close to everything through the phone branch while the caller believed they had
// searched for an address. It is a floor on what counts as a search rather than a fact about
// numbering plans.
const minPhoneDigits = 4

// phonePattern turns a search term into a pattern matching a stored phone number, or "" for a term
// that is not one.
//
// # Why a term cannot simply be matched against the column
//
// Registration normalises to E.164 and `000002` stores that form, so the number a support engineer
// has in front of them is almost never the string in the row. `0419 312 345`, `(04) 1931 2345` and
// `+61 419 312 345` are one number written three ways, and the row holds `+61419312345`. A
// substring match of any of the first three against it finds nothing at all — which is
// indistinguishable, at the endpoint, from there being no such account.
//
// So the term is reduced to its digits and matched against the stored digits. The separators a
// person types — spaces, brackets, dashes, the leading plus — all disappear on both sides.
//
// # The leading zero, and why dropping it is not an assumption about Australia
//
// E.164 has no leading zero: a national trunk prefix is dropped when the country code is added, so
// `0419…` becomes `+61419…` and the `0` is simply not in the row. Dropping one leading zero from the
// term is therefore a fact about the *stored form* rather than a guess about which country the
// number is from — the same reduction works for any national number written in its local form.
// **Only one is dropped**, because the digits after it are the number.
//
// # An empty answer means "not a phone search", and the statement relies on it
//
// A term with no digits, or with fewer than [minPhoneDigits], returns the empty string — and the
// statement's guard on `$3` then turns the phone branch off entirely, rather than comparing the
// column against a pattern of two bare wildcards. Every account has a number (`phone` is NOT NULL),
// so getting this wrong would make every email search return the whole table.
//
// The guard is written in SQL as a comparison against an empty literal, which is deliberately not
// quoted here: gofmt rewrites a pair of adjacent single quotes inside a doc comment into a typographic
// one, which puts the file on `gofmt -l` permanently. Docs/11 §3 records the same trap.
func phonePattern(term string) string {
	var digits strings.Builder
	for _, r := range term {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}

	national := strings.TrimPrefix(digits.String(), "0")
	if len(national) < minPhoneDigits {
		return ""
	}

	// No escaping: the value is digits by construction, and `%` and `_` cannot survive the loop
	// above. Written as a plain concatenation rather than through likeContains so that a reader
	// can see there is nothing here to escape.
	return "%" + national + "%"
}
