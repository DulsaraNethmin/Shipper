package bidding

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// Who may read a negotiation, and what each of them sees (SHIP-96).
//
// Docs/02 §4's last line is the whole rule: "**Bid history remains visible to the customer, bidding
// provider, and administrators.**" SHIP-88 built the read and served the first two, naming this
// ticket for the third and for the rules in full. This file is that — one place where the three
// audiences are enumerated, so that adding a fourth or widening one is an edit somebody makes here
// rather than a branch somebody adds to a lookup.
//
// # What "only what Docs/02 §4 permits" turns out to mean, stated as four rules
//
//  1. **The customer who owns the job** reads every negotiation on it. Docs/01 §4.3 wants exactly
//     that — "allow a customer to compare price, timing, provider profile" — and the customer is the
//     one audience for whom the whole point is comparison.
//  2. **The provider a negotiation is with** reads that negotiation and no other. Their own offers,
//     and the customer's counters addressed to them.
//  3. **An administrator** reads any negotiation, and needs to be neither party. Docs/04 §5 has them
//     handling delivery exceptions and disputes, which means reading a record they are outside of.
//  4. **Nobody else, and a competing provider is the one that matters.** Docs/01 §4.3's second line
//     makes another provider's price private, and this endpoint is the one place a competitor could
//     otherwise read a whole negotiation at once. It answers the 404 a bid that does not exist gets,
//     byte-identically, because a refusal that confirmed the bid exists would be the disclosure.
//
// **There is no field-level half to any of this.** No shape in this package carries anything of the
// job beyond its identifier ([Bid]), so the customer's budget is not in a chain to be redacted from
// one audience and shown to another, and the amounts that *are* there are what each party
// deliberately offered the other. What the three audiences differ in is which negotiations they can
// reach, not what a row shows them — which is why this file decides reachability and nothing else.
//
// # The administrator has no route yet, and that is recorded rather than worked around
//
// `authctx.Subject` cannot carry an administrator and says so: Docs/06 §5.2 and SHIP-147 make admin
// sign-in a separate system that a user token cannot reach, so `authctx.Role` has two values and
// neither is one. `cmd/api` therefore builds every [Viewer] with `Administrator` false, and that is
// a **fact about the auth class rather than a stub** — there is no credential in this platform that
// could make it true.
//
// So [AudienceAdministrator] is reachable from the domain and not from the wire. SHIP-147 supplies
// the session and SHIP-152 the endpoint; what each of them then needs from this package is one
// field on a struct rather than a rule to re-derive. The tests exercise all three audiences.

// Audience is which of Docs/02 §4's three readers a caller turns out to be.
//
// An enumeration rather than a bool per rule, because the three are exclusive and the *reason* a
// caller may read matters to more than the yes-or-no: SHIP-102's customer comparison and SHIP-101's
// provider list are different screens over the same rows, and a later ticket asking "which of them
// is this" wants an answer rather than three predicates.
type Audience int

const (
	// AudienceNone is the zero value: a caller Docs/02 §4 names none of.
	//
	// First on purpose, as every outcome enumeration in this package is: a resolver with a missing
	// case answers "nobody" rather than "everybody".
	AudienceNone Audience = iota

	// AudienceProvider is the provider the negotiation is with.
	AudienceProvider

	// AudienceCustomer is the customer who owns the job the negotiation is on.
	AudienceCustomer

	// AudienceAdministrator is a platform administrator, who is neither party.
	AudienceAdministrator
)

func (a Audience) String() string {
	switch a {
	case AudienceProvider:
		return "provider"
	case AudienceCustomer:
		return "customer"
	case AudienceAdministrator:
		return "administrator"
	default:
		return "nobody"
	}
}

// permitted reports whether this audience may read a negotiation at all.
//
// One function rather than three comparisons at the call site, so that Docs/02 §4's list is in one
// place. It is deliberately not a lookup keyed on a role: whether a caller is a *party* is a fact
// about rows rather than about their account, which is what [Service.reachableBid] establishes.
func (a Audience) permitted() bool { return a != AudienceNone }

// Viewer is a caller asking to read a negotiation, and the one thing about them the domain cannot
// work out for itself.
//
// **The identifier is compared against rows and the flag is not derivable from anything here.**
// Whether an account is the bidding provider is `bids.provider_id`; whether it is the customer is
// `jobs`' fact, asked through [Negotiation.CustomerOf]. Whether it is an administrator is neither —
// it is a property of the credential the request arrived with, and Docs/07 §3 puts that decision on
// the platform's edge rather than in a domain.
//
// So `cmd/api` says which auth class served the request and this package decides everything else.
// Today every route in this domain is `RequireUser`, which cannot be an administrator at all (see
// the file header), so the flag is false on every request the service actually receives.
type Viewer struct {
	// ID is the account asking. Required: a viewer with no identifier is nobody, including when
	// Administrator is set, because an audit trail names the administrator who read.
	ID uuid.UUID

	// Administrator is true when the request arrived through an administrator's session.
	Administrator bool
}

// audienceFor works out which of Docs/02 §4's readers this viewer is for this negotiation.
//
// # The administrator is decided first, and that is the one ordering that matters
//
// An administrator is not a party and asking `jobs` whether they own the job would answer "no" —
// so a resolver that checked the parties first would refuse them before reaching the branch that
// permits them. It is also the cheapest answer: it costs no query.
//
// The provider is checked before the customer for the reason [Service.reachableBid] gives: it is a
// comparison against a column already in hand, the customer's is a question for another domain, and
// the two cannot both be true — `users.role` is immutable (000005) and SHIP-81's filter excludes a
// job's own customer from bidding on it.
func (s *Service) audienceFor(ctx context.Context, r db.Runner, v Viewer, bid Bid) (Audience, error) {
	if v.ID == uuid.Nil {
		return AudienceNone, nil
	}
	if v.Administrator {
		return AudienceAdministrator, nil
	}
	if bid.ProviderID == v.ID {
		return AudienceProvider, nil
	}

	owns, err := s.negotiation.CustomerOf(ctx, r, v.ID, bid.JobID)
	if err != nil {
		return AudienceNone, fmt.Errorf(
			"bidding: deciding whether %s owns %s: %w", v.ID, bid.JobID, err)
	}
	if owns {
		return AudienceCustomer, nil
	}
	return AudienceNone, nil
}
