package bidding_test

import (
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/bidding"
)

// TestStatusesAreTheEightInTheDocument holds the Go copy to Docs/02 §4 without a database.
//
// The pairing test in migrations/bids_test.go compares this list against ck_bids_status, which
// catches drift between the two. It cannot catch the two drifting *together* — somebody
// "tidying" a spelling in both places at once — so the eight strings are written out here as
// well, against the document rather than against the schema.
func TestStatusesAreTheEightInTheDocument(t *testing.T) {
	want := []bidding.Status{
		"Draft", "Submitted", "Countered", "Accepted", "Rejected", "Withdrawn", "Expired", "Superseded",
	}

	if len(bidding.Statuses) != len(want) {
		t.Fatalf("bidding.Statuses holds %d statuses; Docs/02 §4 has %d",
			len(bidding.Statuses), len(want))
	}
	for i, status := range want {
		if bidding.Statuses[i] != status {
			t.Errorf("bidding.Statuses[%d] is %q, want %q — Docs/02 §4 is the source for "+
				"these exact strings, and Docs/10 §3.4 stores them unaltered",
				i, bidding.Statuses[i], status)
		}
	}

	seen := map[bidding.Status]bool{}
	for _, status := range bidding.Statuses {
		if seen[status] {
			t.Errorf("bidding.Statuses names %q twice", status)
		}
		seen[status] = true
	}
}

func TestStatusValid(t *testing.T) {
	for _, status := range bidding.Statuses {
		if !status.Valid() {
			t.Errorf("%q is in bidding.Statuses and Valid says otherwise", status)
		}
	}

	// Lower case is the wire form Docs/10 §4.7 will eventually put on the wire, and it is not
	// the stored form. Accepting it here would make the pairing against ck_bids_status
	// meaningless, because the database would refuse exactly the value Go had approved.
	for _, wrong := range []bidding.Status{"", "accepted", "ACCEPTED", "Pending", "Won"} {
		if wrong.Valid() {
			t.Errorf("%q was accepted as a bid status", wrong)
		}
	}
}
