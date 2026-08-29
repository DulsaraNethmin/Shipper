package main

import (
	"bytes"
	"image/png"
	"regexp"
	"strings"
	"testing"
)

// What these tests are for.
//
// The seed's own correctness is demonstrated by running it — `make seed` against a migrated
// database either produces the five states Docs/09 asks for or fails saying which endpoint refused.
// What running it does **not** catch is a dataset that is quietly wrong in a way the platform
// accepts: a provider who cannot see the job they are declared to bid on, a goods category the
// catalogue will not carry, a telephone number that belongs to somebody. Those produce a
// demonstration that looks fine and is not, which is exactly the failure a seed is dangerous for.
//
// So these tests are about the declaration in dataset.go rather than about the HTTP. They need no
// database, no stack and no network, and they run in milliseconds.

// fictitiousMobile is ACMA's range for drama and training: 0491 570 156 to 0491 570 165, in E.164.
//
// Ten numbers, which is the ceiling on how many accounts this dataset may ever have. That is a
// deliberate constraint rather than an accident of the range: an eleventh account would have to take
// a number outside it, this test would fail, and whoever added it would have to decide what to do
// about a real number rather than discovering later that the demonstration had been texting one.
var fictitiousMobile = regexp.MustCompile(`^\+6149157015[6-9]$|^\+614915701(6[0-5])$`)

// TestNoRealPersonalInformation holds the *Done when*'s last clause structurally.
//
// dataset.go promises this and it is the test the promise is worth anything because of. Names are
// not checked — an invented name cannot be distinguished from a real one by a regular expression,
// which is precisely why the two fields that *can* be constrained are.
func TestNoRealPersonalInformation(t *testing.T) {
	for _, account := range allAccounts() {
		if !strings.HasSuffix(account.Email, "@example.com") {
			t.Errorf("%s is not at example.com; RFC 2606 reserves that domain so that a "+
				"demonstration cannot mail a real address", account.Email)
		}
		if !fictitiousMobile.MatchString(account.Phone) {
			t.Errorf("%s has phone %s, which is outside ACMA's fictitious range "+
				"+61491570156–165; every number here must be one nobody can be called on",
				account.Email, account.Phone)
		}
	}

	for _, job := range demoJobs {
		if job.Driver == nil {
			continue
		}
		if !fictitiousMobile.MatchString(job.Driver.Mobile) {
			t.Errorf("driver %s has phone %s, which is outside ACMA's fictitious range",
				job.Driver.Name, job.Driver.Mobile)
		}
	}
}

// TestEveryNaturalKeyIsUnique guards the mechanism idempotency is built on.
//
// **This is the test that matters most in the file.** marketplace.go recognises an existing job by
// its goods description and an existing vehicle by its registration, with no marker column
// anywhere — so two jobs sharing a description are one job to a re-run, and the second would be
// created again on every run for ever. Nothing else in the repository would report that: the
// platform is perfectly happy to hold two jobs that read the same.
func TestEveryNaturalKeyIsUnique(t *testing.T) {
	seen := map[string]string{}
	claim := func(kind, key string) {
		t.Helper()
		if held, taken := seen[kind+"\x00"+key]; taken {
			t.Errorf("two %ss share the key %q (%s); the seed matches on it, so a re-run "+
				"cannot tell them apart", kind, key, held)
			return
		}
		seen[kind+"\x00"+key] = key
	}

	for _, account := range allAccounts() {
		claim("email", strings.ToLower(account.Email))
		claim("phone", account.Phone)
	}
	for _, provider := range demoProviders {
		for _, vehicle := range provider.Vehicles {
			claim("registration", strings.ToUpper(vehicle.Registration))
		}
	}
	for _, job := range demoJobs {
		claim("goods description", job.GoodsDescription)
	}
}

// TestTheDatasetCoversEveryClauseOfTheDoneWhen reads Docs/09's acceptance criterion as a checklist.
//
// A dataset that lost its completed delivery in an edit would still seed, still be idempotent and
// still contain no personal information — and would no longer satisfy the ticket. The count of open
// jobs is checked as "more than one, at least one of them with nothing bid on it" rather than as an
// exact number, because that is what the clause actually requires and pinning the number would make
// every future addition a test edit.
func TestTheDatasetCoversEveryClauseOfTheDoneWhen(t *testing.T) {
	byStage := map[stage]int{}
	for _, job := range demoJobs {
		byStage[job.Stage]++
	}

	if byStage[stageOpen] == 0 {
		t.Error("no job is left open with nothing bid on it; a provider signing in to the " +
			"demonstration would have nothing to bid on")
	}
	if byStage[stageBidsIn] == 0 {
		t.Error("no job has bids in flight; the customer's award screen is the one screen " +
			"the whole product exists for")
	}
	if byStage[stageInProgress] == 0 {
		t.Error("no delivery is in progress")
	}
	if byStage[stageDelivered] != 1 {
		t.Errorf("%d jobs are delivered; Docs/09 asks for one completed delivery with "+
			"photograph proof", byStage[stageDelivered])
	}

	if len(demoProviders) < 2 {
		t.Error("fewer than two providers; nothing here would be a competition")
	}

	// The budget-privacy invariant is enforced and tested in `internal/jobs` and by the
	// acceptance harness, not here. What this file is responsible for is that the demonstration
	// gives those guards something to be true about: a dataset in which no job carried a budget
	// would exercise the invariant nowhere while appearing to.
	budgeted := 0
	for _, job := range demoJobs {
		if job.BudgetCents > 0 {
			budgeted++
		}
	}
	if budgeted == 0 {
		t.Error("no job carries a budget, so nothing in the demonstration exercises the rule " +
			"that a provider never sees one")
	}
}

// TestEveryDeclaredBidderCouldActuallyBid reproduces `fleet.eligible` against the declaration.
//
// # Why this test exists at all
//
// The eligibility filter is one SQL predicate with four `EXISTS` clauses, and a provider who fails
// any of them **sees an empty board rather than an error**. So a dataset naming a bidder whose
// service area does not reach the pickup, or whose only truck is too small for the load, fails at
// run time with `404 No such job` — a message about the job that is really about the provider, and
// one that took a while to read correctly the first time it happened.
//
// Two of the four clauses are checked here: service area and vehicle capability. The other two —
// the Verified decision and the account's own state — are things the seed *creates* rather than
// declares, so there is nothing in dataset.go for them to disagree with.
func TestEveryDeclaredBidderCouldActuallyBid(t *testing.T) {
	byEmail := map[string]demoProvider{}
	for _, provider := range demoProviders {
		byEmail[provider.Account.Email] = provider
	}

	for _, job := range demoJobs {
		for _, bid := range job.Bids {
			provider, known := byEmail[bid.ProviderEmail]
			if !known {
				t.Errorf("%s bids on %q and is not a seeded provider",
					bid.ProviderEmail, short(job.GoodsDescription))
				continue
			}

			if !serves(provider, job.Pickup) {
				t.Errorf("%s bids on %q, picked up in %s %s, and serves neither that "+
					"state nor that postcode — the job would not appear in their feed",
					provider.DisplayName, short(job.GoodsDescription),
					job.Pickup.State, job.Pickup.Postcode)
			}

			if !canCarry(provider, job) {
				t.Errorf("%s bids on %q and has no vehicle that fits %.0fkg at "+
					"%d×%d×%dcm", provider.DisplayName, short(job.GoodsDescription),
					job.WeightKg, job.LengthCm, job.WidthCm, job.HeightCm)
			}
		}

		if job.Stage >= stageInProgress {
			if job.AwardTo == "" {
				t.Errorf("%q is carried but names no winning bid", short(job.GoodsDescription))
				continue
			}
			if !bidsOn(job, job.AwardTo) {
				t.Errorf("%q is awarded to %s, who does not bid on it",
					short(job.GoodsDescription), job.AwardTo)
			}
			if job.Driver == nil {
				t.Errorf("%q is carried and names no driver", short(job.GoodsDescription))
			}
			if job.CarriedFrom >= 0 {
				t.Errorf("%q is carried from an offset of %s; milestones are recorded in "+
					"the past, so this must be negative", short(job.GoodsDescription), job.CarriedFrom)
			}
		}
	}
}

// TestEveryGoodsCategoryIsOneShipperCarries keeps the dataset inside the catalogue.
//
// The six carried codes are written out rather than read from `internal/config`, and that is the
// one place in this file where a second copy of a list is the right answer: the catalogue is
// *configuration*, a deployment may narrow it, and a test that read the running configuration would
// pass against a catalogue this dataset cannot actually be published under. The failure message
// names the mismatch either way.
func TestEveryGoodsCategoryIsOneShipperCarries(t *testing.T) {
	carried := map[string]bool{
		"general_freight": true, "furniture": true, "household_removals": true,
		"building_materials": true, "machinery": true, "retail_stock": true,
	}

	for _, job := range demoJobs {
		if !carried[job.GoodsCategory] {
			t.Errorf("%q is in category %q, which Shipper does not carry — publication "+
				"would be refused", short(job.GoodsDescription), job.GoodsCategory)
		}
	}
}

// TestTheProofPhotographIsADeterministicPNG checks the two properties the upload depends on.
//
// The content length is declared to the platform *before* the bytes are sent and is covered by the
// signature, so an image that encoded differently on the second call would be refused by the object
// store with a message about a signature rather than about a size.
func TestTheProofPhotographIsADeterministicPNG(t *testing.T) {
	first, err := proofPhotograph("a job")
	if err != nil {
		t.Fatalf("drawing: %v", err)
	}
	again, err := proofPhotograph("a job")
	if err != nil {
		t.Fatalf("drawing again: %v", err)
	}
	if !bytes.Equal(first, again) {
		t.Error("the same input drew two different images; the upload declares a length " +
			"before it sends the bytes, and a signature covers it")
	}

	different, err := proofPhotograph("another job")
	if err != nil {
		t.Fatalf("drawing another: %v", err)
	}
	if bytes.Equal(first, different) {
		t.Error("two jobs drew the same image; a reader would take one photograph for two")
	}

	decoded, err := png.Decode(bytes.NewReader(first))
	if err != nil {
		t.Fatalf("the image is not a PNG the standard library can read: %v", err)
	}
	if bounds := decoded.Bounds(); bounds.Dx() != proofWidth || bounds.Dy() != proofHeight {
		t.Errorf("the image is %dx%d, want %dx%d",
			bounds.Dx(), bounds.Dy(), proofWidth, proofHeight)
	}
}

// TestCredentialsAreRefusedWhenTheyWouldBeWeak covers the decision recorded in accounts.go.
func TestCredentialsAreRefusedWhenTheyWouldBeWeak(t *testing.T) {
	for _, held := range []struct {
		name  string
		creds credentials
	}{
		{"no marketplace password", credentials{Administrator: "administrator-password"}},
		{"no administrator password", credentials{User: "marketplace-password"}},
		{"short marketplace password", credentials{User: "short", Administrator: "administrator-password"}},
		{"short administrator password", credentials{User: "marketplace-password", Administrator: "short"}},
		{"one password for both", credentials{User: "the-same-password", Administrator: "the-same-password"}},
	} {
		if err := credentialsOf(held.creds).validate(); err == nil {
			t.Errorf("%s was accepted", held.name)
		}
	}

	usable := credentials{User: "marketplace-password", Administrator: "administrator-password"}
	if err := usable.validate(); err != nil {
		t.Errorf("a usable pair was refused: %v", err)
	}
}

// credentialsOf exists so the table above reads as data rather than as construction.
func credentialsOf(c credentials) credentials { return c }

// allAccounts is every marketplace account the dataset declares.
func allAccounts() []demoAccount {
	accounts := []demoAccount{demoCustomer}
	for _, provider := range demoProviders {
		accounts = append(accounts, provider.Account)
	}
	return accounts
}

// serves reports whether the provider's declared area reaches this pickup.
func serves(provider demoProvider, pickup demoAddress) bool {
	for _, state := range provider.States {
		if state == pickup.State {
			return true
		}
	}
	for _, postcode := range provider.Postcodes {
		if postcode == pickup.Postcode {
			return true
		}
	}
	return false
}

// canCarry reports whether any of the provider's vehicles fits the load.
//
// The comparison mirrors the SQL: a dimension the job does not state, or the vehicle does not
// state, does not exclude anything. Both sides are optional in the schema and the filter treats an
// absent value as "no objection" rather than as zero.
func canCarry(provider demoProvider, job demoJob) bool {
	for _, vehicle := range provider.Vehicles {
		switch {
		case vehicle.MaxWeightKg > 0 && job.WeightKg > vehicle.MaxWeightKg:
		case vehicle.LoadLengthCm > 0 && job.LengthCm > vehicle.LoadLengthCm:
		case vehicle.LoadWidthCm > 0 && job.WidthCm > vehicle.LoadWidthCm:
		case vehicle.LoadHeightCm > 0 && job.HeightCm > vehicle.LoadHeightCm:
		default:
			return true
		}
	}
	return false
}

// bidsOn reports whether this provider is among the job's declared bidders.
func bidsOn(job demoJob, email string) bool {
	for _, bid := range job.Bids {
		if bid.ProviderEmail == email {
			return true
		}
	}
	return false
}
