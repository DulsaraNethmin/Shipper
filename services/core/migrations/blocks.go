package migrations

import (
	"fmt"
	"sort"
)

// Migration numbers are allocated in reserved per-domain blocks.
//
// The tool picks the next free number by taking the highest one already present and adding
// one. With a single ordered queue of work that is correct. With two branches open at once it
// is not: both read the same directory, both compute the same next number, both create a file,
// and the two files merge cleanly in git because their names differ only after the number.
// Nothing fails until someone runs `migrate up`, by which point the work has been reviewed.
//
// Reserving a block per domain removes the shared counter. Two branches working in different
// domains draw from different ranges and cannot collide, and golang-migrate is content with
// gaps — it requires versions to be unique and ascending, not contiguous.
//
// The block order is also dependency order, which is not a coincidence and is worth keeping.
// Every domain's tables reference users, users is in the shared block, and the shared block is
// first — so a migration can never be applied before the table it points at exists.
//
// A ninth domain needs a block here, which is the same deliberate act that adding it to
// internal/boundaries requires.

// Block is a reserved range of migration numbers.
type Block struct {
	// Domain is the name used on the command line: `make migrate-create domain=jobs`.
	// It matches the package name under internal/, except for "shared".
	Domain string

	// First and Last bound the range, inclusive.
	First, Last int

	// Note says what belongs here, for the error message when someone picks wrong.
	Note string
}

// SharedDomain owns the tables that more than one domain reads.
//
// It is deliberately not one of the eight. users, audit_log and outbox are read across the
// whole service, so they belong to whoever is doing shared-platform work rather than to a
// domain that happens to have needed them first — see Docs/10 §9.2.
const SharedDomain = "shared"

// Blocks is the allocation. It is closed, in the same way the domain list is closed.
//
// The order follows Docs/06 §3, so that this table and internal/boundaries.Domains read the
// same way. One block per domain, with no sharing: profiles and fleet are two domains even
// though their tables are related, and a shared block between them would put two agents back
// on one counter.
var Blocks = []Block{
	{SharedDomain, 1, 99, "extensions, helpers, and tables every domain reads: users, audit_log, outbox"},
	{"identity", 100, 199, "credentials, verification tokens, device sessions"},
	{"profiles", 200, 299, "customer and provider profiles, verification state and evidence"},
	{"fleet", 300, 399, "vehicles, capabilities, service areas"},
	{"jobs", 400, 499, "the job record, its status history, reference data"},
	{"bidding", 500, 599, "bids, offers, the one-accepted-bid index"},
	{"delivery", 600, 699, "driver assignments, milestones, proof"},
	{"notifications", 700, 799, "device tokens, preferences, delivery records"},
	{"admin", 800, 899, "moderation queues, disputes, internal notes"},
}

// BlockFor returns the block reserved for a domain.
func BlockFor(domain string) (Block, error) {
	for _, b := range Blocks {
		if b.Domain == domain {
			return b, nil
		}
	}
	return Block{}, fmt.Errorf("no migration block is reserved for %q; %s", domain, knownDomains())
}

// BlockContaining returns the block a version falls in.
func BlockContaining(version int) (Block, bool) {
	for _, b := range Blocks {
		if version >= b.First && version <= b.Last {
			return b, true
		}
	}
	return Block{}, false
}

func knownDomains() string {
	names := make([]string, 0, len(Blocks))
	for _, b := range Blocks {
		names = append(names, b.Domain)
	}
	sort.Strings(names)
	return fmt.Sprintf("reserved blocks exist for: %v", names)
}
