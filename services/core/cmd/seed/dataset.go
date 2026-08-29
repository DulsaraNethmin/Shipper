package main

import "time"

// The demonstration dataset, declared once (SHIP-186).
//
// # Everything here is invented, and the guarantee is structural rather than editorial
//
// Docs/09's *Done when* requires that the seed "contains no real personal information". A promise
// to have chosen carefully is not worth much six months from now, when somebody adds a provider to
// make a screenshot look fuller. So the two fields that could identify a living person are drawn
// from ranges that cannot:
//
//   - **Every address is at example.com**, which RFC 2606 reserves for exactly this and which no
//     mail system will deliver to.
//   - **Every telephone number is from 0491 570 156–165**, the ten mobile numbers the ACMA reserves
//     for use in drama and training. They are not allocated to anybody and never will be.
//
// [TestNoRealPersonalInformation] holds both, so a thirteenth account added in a hurry fails a test
// rather than reaching a hosted instance. Names are invented and cannot be constrained that way,
// which is the reason the two fields that *can* be are.
//
// # The addresses are Sydney because X-5 named Sydney
//
// The pilot metropolitan area was closed on 29 August 2026 (`Docs/01` §8, `Docs/11` §3), and this
// is the first code to depend on it. Suburbs and postcodes are real — they are places rather than
// people — and the street numbers are generic. A provider's service area is expressed in the same
// vocabulary the platform uses, states and postcodes, because SHIP-79 settled that a service area
// has no radius in it.
//
// # Why the dataset is a declaration rather than a script
//
// Idempotency (below) needs a stable identity for every row, and the cheapest honest one is the
// content itself: no row here carries a marker, a prefix or a seeded UUID. [demoJob.GoodsDescription]
// is unique across the set and is what a re-run matches on, so what makes the data recognisable to
// the seed is the same thing that makes it readable to a buyer. A marker column would have been a
// field in the product that exists only for the seed's benefit.

// demoAccount is one marketplace account, created as a fixture rather than through registration.
//
// See accounts.go for why these are written with SQL when everything downstream of them is not.
type demoAccount struct {
	Email string
	Name  string
	Phone string
	Role  string // customer or provider; ck_users_role admits no third value
}

// demoProvider is an account plus everything that makes it able to bid.
//
// The four things `fleet.eligible` requires are all here — a Verified decision, a verified and
// active account, a service area covering the pickup, and a vehicle big enough for the load. A
// provider missing any one of them is invisible in the feed, and the failure looks identical to
// there being no work.
type demoProvider struct {
	Account     demoAccount
	DisplayName string

	// OperatesAs is `individual` or `business` — the operating form, not a legal name. The
	// field is narrow on purpose: SHIP-79a made it the one thing a customer learns about who
	// they are dealing with beyond the display name, and a free-text trading name would have
	// been an unverified claim rendered next to a verification badge.
	OperatesAs string
	States     []string
	Postcodes  []string
	Vehicles   []demoVehicle
}

// demoVehicle is one fleet entry. The capacities matter: they are half of the eligibility filter,
// and a provider whose truck is smaller than the load will not see the job it is meant to bid on.
type demoVehicle struct {
	Registration string
	Type         string
	Make         string
	Model        string
	MaxWeightKg  float64
	LoadLengthCm int
	LoadWidthCm  int
	LoadHeightCm int
}

// demoAddress is the four-part address SHIP-60 settled on.
type demoAddress struct {
	Line     string
	Suburb   string
	State    string
	Postcode string
}

// stage is how far through the journey a job is driven.
//
// **Ordered, and each stage includes every stage before it.** The seed walks a job forward one
// transition at a time through the same endpoints a customer and a driver use, so a job at
// [stageDelivered] has been published, bid on, awarded, assigned and carried — with the history,
// the audit entries and the domain events that produces. Nothing here writes a status.
type stage int

const (
	// stageOpen is published and waiting. A buyer needs at least one job with no bids on it,
	// or the marketplace looks like a finished ledger rather than a live board.
	stageOpen stage = iota

	// stageBidsIn is published with competing offers against it — the screen the whole product
	// exists for, and the one a customer makes a decision on.
	stageBidsIn

	// stageInProgress is awarded, with a driver assigned and milestones recorded up to
	// In transit. Docs/09's "a delivery in progress".
	stageInProgress

	// stageDelivered is complete, with a photograph behind it. Docs/01 §4.4 admits proof or a
	// recorded exception and never neither; this takes the photograph, because a demonstration
	// of the exception path would be a demonstration of the unhappy case.
	stageDelivered
)

// demoBid is one provider's offer, named by the provider's address rather than by an identifier
// nothing has yet assigned.
type demoBid struct {
	ProviderEmail string
	AmountCents   int64
	Message       string

	// PickupAt and DeliverBy are offsets from the seed's own start, for the reason
	// [demoJob.PickupWindowStart] is.
	PickupAt  time.Duration
	DeliverBy time.Duration
}

// demoJob is one job and everything that happens to it.
type demoJob struct {
	// GoodsDescription is the natural key. It is unique across the dataset and is what a
	// re-run matches on to decide whether this job already exists — see marketplace.go.
	GoodsDescription string

	GoodsCategory string
	Pickup        demoAddress
	Dropoff       demoAddress

	WeightKg float64
	LengthCm int
	WidthCm  int
	HeightCm int

	VehicleRequirement string
	HandlingNotes      string

	// BudgetCents is the customer's ceiling, and it is here to be *invisible*. CLAUDE.md's
	// first invariant is that no provider ever sees it, in any form, through any endpoint —
	// so a demonstration in which no job has a budget would quietly stop demonstrating that.
	// The invariant itself is guarded in `internal/jobs` and by the acceptance harness;
	// [TestTheDatasetCoversEveryClauseOfTheDoneWhen] only checks that this dataset gives those
	// guards something to be true about.
	BudgetCents int64

	// Windows are offsets from the seed's start rather than instants, so the dataset is
	// plausible whenever it is run rather than only in the week it was written.
	PickupWindowStart  time.Duration
	PickupWindowEnd    time.Duration
	DropoffWindowStart time.Duration
	DropoffWindowEnd   time.Duration

	Stage stage
	Bids  []demoBid

	// CarriedFrom is when the driver recorded the first milestone, as an offset from the run.
	// Milestones step [milestoneSpacing] apart from there.
	//
	// # Why this is declared rather than derived from the pickup window
	//
	// It was derived, and the derivation could not be right. **A bid must promise a pickup in the
	// future** — `POST /v1/jobs/{id}/bids` refuses "Enter a collection time in the future" — while
	// a delivery that has already happened needs milestones in the past. Both are true at once for
	// every completed job the seed builds, because the seed places the bid and records the
	// milestones minutes apart on the same afternoon.
	//
	// So the promised time and the recorded time cannot be made to agree, and the honest thing is
	// to keep them close and say so: the demonstration's carried jobs read as work a driver got to
	// ahead of the slot they quoted for. `RecordedAt` is the actor's clock and 000601 is explicit
	// that it is not bounded against now(), which is what makes the past half of this legal at all.
	CarriedFrom time.Duration

	// AwardTo names the winning bid's provider, and is read only at [stageInProgress] and
	// beyond. Exactly one bid per job is ever awarded — the database enforces that with a
	// partial unique index, and this is the seed's half of the same fact.
	AwardTo string

	Driver *demoDriver
}

// demoDriver is who carries the load. A driver has no account, by design: they hold a job-scoped
// token that reaches exactly one job and cannot be exchanged for anything else.
type demoDriver struct {
	Name   string
	Mobile string
}

const (
	// The demonstration's customer. One is enough: a buyer signs in as this account to see the
	// customer's half of the marketplace, and a second would only halve what that account owns.
	customerEmail = "naomi.fletcher@example.com"

	harbourEmail    = "harbour.haulage@example.com"
	parramattaEmail = "parramatta.freight@example.com"
	illawarraEmail  = "illawarra.transport@example.com"
)

// demoCustomer is the account every seeded job belongs to.
var demoCustomer = demoAccount{
	Email: customerEmail,
	Name:  "Naomi Fletcher",
	Phone: "+61491570156",
	Role:  "customer",
}

// demoAdministratorEmail is the bootstrap administrator's address.
//
// One administrator, at the `owner` role, because the demonstration's admin half is Docs/01 §8's
// "an administrator finds the job and reads its audit trail" and a second account would prove
// nothing further. Permissions are demonstrated by the panel refusing what this account may not
// do, which needs a *narrower* account rather than another wide one — and creating that is
// `POST /v1/admin/administrators`, which is itself part of the demonstration.
const demoAdministratorEmail = "admin@example.com"

// demoAdministratorName is who the panel greets.
const demoAdministratorName = "Robyn Ellis"

// demoProviders are the three providers who compete for the seeded work.
//
// **Three rather than two.** Two providers make a comparison; three make a market — and the
// customer's award screen only looks like a decision when the offers differ in price and in
// promised date, which needs a third to be visibly not the cheapest and not the fastest.
var demoProviders = []demoProvider{
	{
		Account: demoAccount{
			Email: harbourEmail,
			Name:  "Sione Tuilagi",
			Phone: "+61491570157",
			Role:  "provider",
		},
		DisplayName: "Harbour City Haulage",
		OperatesAs:  "business",
		States:      []string{"NSW"},
		Vehicles: []demoVehicle{
			{
				Registration: "DEM01A", Type: "box_truck",
				Make: "Isuzu", Model: "NPR 65-190",
				MaxWeightKg: 4500, LoadLengthCm: 620, LoadWidthCm: 230, LoadHeightCm: 240,
			},
			{
				Registration: "DEM01B", Type: "van",
				Make: "Ford", Model: "Transit 350L",
				MaxWeightKg: 1400, LoadLengthCm: 340, LoadWidthCm: 175, LoadHeightCm: 190,
			},
		},
	},
	{
		Account: demoAccount{
			Email: parramattaEmail,
			Name:  "Anjali Devi",
			Phone: "+61491570158",
			Role:  "provider",
		},
		DisplayName: "Parramatta Freight Co",
		OperatesAs:  "business",
		States:      []string{"NSW"},
		Vehicles: []demoVehicle{
			{
				Registration: "DEM02A", Type: "tray_truck",
				Make: "Hino", Model: "300 Series",
				MaxWeightKg: 3800, LoadLengthCm: 500, LoadWidthCm: 220, LoadHeightCm: 220,
			},
		},
	},
	{
		Account: demoAccount{
			Email: illawarraEmail,
			Name:  "Marco Bellini",
			Phone: "+61491570159",
			Role:  "provider",
		},
		DisplayName: "Illawarra Transport",

		// The one sole operator in the set. Two forms exist and a demonstration showing
		// only one of them would leave a customer unable to see that the distinction is
		// drawn at all.
		OperatesAs: "individual",

		// Postcodes rather than the whole state, so the feed visibly filters. This provider
		// serves the seeded pickups and would not see work in Newcastle, which is the
		// difference between a service area that is configured and one that is decorative.
		Postcodes: []string{"2015", "2010", "2019", "2204"},
		Vehicles: []demoVehicle{
			{
				Registration: "DEM03A", Type: "box_truck",
				Make: "Fuso", Model: "Canter 815",
				MaxWeightKg: 4200, LoadLengthCm: 570, LoadWidthCm: 210, LoadHeightCm: 230,
			},
		},
	},
}

const (
	day    = 24 * time.Hour
	hour   = time.Hour
	minute = time.Minute
)

// milestoneSpacing is how far apart a carried job's milestones are recorded.
//
// Twenty minutes reads as a run across Sydney rather than as four events at one instant, which is
// what omitting the time entirely would have produced — the platform stamps its own clock when the
// actor does not say, and four milestones posted in one second is not a journey anybody can read
// off a tracking screen.
const milestoneSpacing = 20 * minute

// demoJobs is the board a buyer sees, in the order it is seeded.
//
// **Five jobs, and each one is here for a clause of the *Done when*.** Two open (one with offers
// against it and one without), one in progress, one delivered with a photograph, and one more open
// job carrying a budget so that the budget-privacy invariant has something to be true about.
var demoJobs = []demoJob{
	{
		GoodsDescription: "Commercial kitchen equipment on two pallets — fryer, cold bench and shelving",
		GoodsCategory:    "machinery",
		Pickup:           demoAddress{Line: "12 Bourke Road", Suburb: "Alexandria", State: "NSW", Postcode: "2015"},
		Dropoff:          demoAddress{Line: "45 Macquarie Street", Suburb: "Parramatta", State: "NSW", Postcode: "2150"},

		WeightKg: 820, LengthCm: 240, WidthCm: 120, HeightCm: 180,
		VehicleRequirement: "Tail lift required — no dock at either end",
		HandlingNotes:      "Fryer must stay upright. Ask for Dan at the loading bay.",
		BudgetCents:        95000,

		PickupWindowStart: 2 * day, PickupWindowEnd: 3 * day,
		DropoffWindowStart: 2 * day, DropoffWindowEnd: 4 * day,

		Stage: stageBidsIn,
		Bids: []demoBid{
			{
				ProviderEmail: harbourEmail,
				AmountCents:   78000,
				Message:       "Can do this Thursday morning with the box truck. Tail lift fitted.",
				PickupAt:      2*day + 9*hour, DeliverBy: 2*day + 14*hour,
			},
			{
				ProviderEmail: illawarraEmail,
				AmountCents:   71500,
				Message:       "Available Friday. Happy to help with the bench at the far end.",
				PickupAt:      3*day + 8*hour, DeliverBy: 3*day + 13*hour,
			},
			{
				ProviderEmail: parramattaEmail,
				AmountCents:   88000,
				Message:       "Thursday, and I can be there first thing if that suits better.",
				PickupAt:      2*day + 7*hour, DeliverBy: 2*day + 11*hour,
			},
		},
	},
	{
		GoodsDescription: "Twelve office desks and chairs, flat-packed",
		GoodsCategory:    "furniture",
		Pickup:           demoAddress{Line: "3 Reservoir Street", Suburb: "Surry Hills", State: "NSW", Postcode: "2010"},
		Dropoff:          demoAddress{Line: "88 Archer Street", Suburb: "Chatswood", State: "NSW", Postcode: "2067"},

		WeightKg: 540, LengthCm: 180, WidthCm: 90, HeightCm: 160,
		HandlingNotes: "Building access is through the rear lane before 10am.",

		PickupWindowStart: 4 * day, PickupWindowEnd: 6 * day,
		DropoffWindowStart: 4 * day, DropoffWindowEnd: 7 * day,

		// No bids. A board on which every job already has offers reads as a closed archive,
		// and a provider signing in to the demonstration needs something to bid on.
		Stage: stageOpen,
	},
	{
		GoodsDescription: "Retail fit-out stock — eight cartons of shelving and signage",
		GoodsCategory:    "retail_stock",
		Pickup:           demoAddress{Line: "150 Victoria Road", Suburb: "Marrickville", State: "NSW", Postcode: "2204"},
		Dropoff:          demoAddress{Line: "9 Spring Street", Suburb: "Bondi Junction", State: "NSW", Postcode: "2022"},

		WeightKg: 260, LengthCm: 150, WidthCm: 80, HeightCm: 120,
		HandlingNotes: "Signage is glass-fronted. Deliver to the centre's dock, level B2.",
		BudgetCents:   42000,

		PickupWindowStart: 5 * day, PickupWindowEnd: 8 * day,
		DropoffWindowStart: 5 * day, DropoffWindowEnd: 9 * day,

		Stage: stageOpen,
	},
	{
		GoodsDescription: "Two engine crates for a workshop rebuild",
		GoodsCategory:    "machinery",
		Pickup:           demoAddress{Line: "7 Baker Street", Suburb: "Botany", State: "NSW", Postcode: "2019"},
		Dropoff:          demoAddress{Line: "22 Batt Street", Suburb: "Penrith", State: "NSW", Postcode: "2750"},

		WeightKg: 1150, LengthCm: 200, WidthCm: 140, HeightCm: 130,
		VehicleRequirement: "Forklift at pickup; crane truck not required",
		HandlingNotes:      "Crates are strapped to pallets. Do not lay flat.",
		BudgetCents:        130000,

		PickupWindowStart: -1 * hour, PickupWindowEnd: 6 * hour,
		DropoffWindowStart: -1 * hour, DropoffWindowEnd: 10 * hour,

		Stage:       stageInProgress,
		AwardTo:     harbourEmail,
		CarriedFrom: -50 * minute,
		Bids: []demoBid{
			{
				ProviderEmail: harbourEmail,
				AmountCents:   112000,
				Message:       "Have done this run before. Forklift is no problem.",
				PickupAt:      30 * minute, DeliverBy: 6 * hour,
			},
			{
				ProviderEmail: parramattaEmail,
				AmountCents:   124000,
				Message:       "Can fit it in on the western run.",
				PickupAt:      2 * hour, DeliverBy: 9 * hour,
			},
		},
		Driver: &demoDriver{Name: "Errol Nash", Mobile: "+61491570160"},
	},
	{
		GoodsDescription: "Plasterboard and cornice for a first-floor fit-out",
		GoodsCategory:    "building_materials",
		Pickup:           demoAddress{Line: "18 Euston Road", Suburb: "Alexandria", State: "NSW", Postcode: "2015"},
		Dropoff:          demoAddress{Line: "60 Church Street", Suburb: "Parramatta", State: "NSW", Postcode: "2150"},

		WeightKg: 900, LengthCm: 300, WidthCm: 120, HeightCm: 60,
		HandlingNotes: "Sheets are 3.6m. Lay flat, do not stand on end.",
		BudgetCents:   68000,

		PickupWindowStart: -3 * hour, PickupWindowEnd: 3 * hour,
		DropoffWindowStart: -3 * hour, DropoffWindowEnd: 6 * hour,

		Stage:       stageDelivered,
		AwardTo:     parramattaEmail,
		CarriedFrom: -110 * minute,
		Bids: []demoBid{
			{
				ProviderEmail: parramattaEmail,
				AmountCents:   59000,
				Message:       "Tray truck suits this. Can load this morning.",
				PickupAt:      20 * minute, DeliverBy: 3 * hour,
			},
			{
				ProviderEmail: illawarraEmail,
				AmountCents:   64000,
				Message:       "Available this afternoon.",
				PickupAt:      3 * hour, DeliverBy: 6 * hour,
			},
		},
		Driver: &demoDriver{Name: "Wendy Okafor", Mobile: "+61491570161"},
	},
}
