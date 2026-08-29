package main

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/delivery"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/profiles"
)

// Driving the marketplace through its own API (SHIP-186).
//
// # Idempotency lives here, and it is a read before every write
//
// Docs/09 requires the seed to be idempotent. The mechanism is the same at every step: ask the
// platform what already exists, on a key the product itself carries, and create only what is
// missing. A job is found by its goods description, a vehicle by its registration, a bid by the
// provider who offered it, an assignment by `driver_assigned`, a milestone by its name.
//
// **No marker columns, no seeded identifiers on the wire, and no reliance on the idempotency
// middleware.** The first two would put a field in the product that exists for the seed's benefit;
// the third does not survive its own Redis TTL, and client.go's [client.post] says why at length.
//
// What this buys is more than "running it twice is safe". A run that dies halfway — a container
// killed, a token expired, an object store that was not up yet — is resumed by running it again,
// and it picks up exactly where it stopped. That is the property an operator actually needs, and it
// is why the checks are per-step rather than one "has this instance been seeded" flag at the top.
//
// # Each stage is reached by the transition that reaches it in the product
//
// A job at [stageDelivered] here was drafted, published, bid on, awarded, assigned and carried,
// through eight endpoints and three separate credentials. Nothing sets a status, because nothing
// can: `Docs/02` §2's guarded function is the only way a job moves, and a database trigger refuses
// a move that arrives without the history row describing it.

// page is the envelope every list in this API uses.
type page[T any] struct {
	Data       []T    `json:"data"`
	NextCursor string `json:"next_cursor"`
	HasMore    bool   `json:"has_more"`
}

type jobResponse struct {
	ID               string `json:"id"`
	Status           string `json:"status"`
	GoodsDescription string `json:"goods_description"`
}

type bidResponse struct {
	ID     string `json:"id"`
	JobID  string `json:"job_id"`
	Status string `json:"status"`

	// OfferedBy is which *side* the standing offer came from — `provider` or `customer` — and
	// not who the provider is. A counter-offer is the same negotiation with the other party
	// speaking, so the identity of the provider stays in [Provider] throughout.
	OfferedBy string `json:"offered_by"`

	Provider struct {
		ID string `json:"id"`
	} `json:"provider"`
}

type vehicleResponse struct {
	ID           string `json:"id"`
	Registration string `json:"registration"`
	Active       bool   `json:"active"`
}

type documentResponse struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

type verificationResponse struct {
	State string `json:"state"`
}

type assignmentResponse struct {
	ID          string `json:"id"`
	DriverToken string `json:"driver_token"`
}

type deliveryDetailResponse struct {
	DriverAssigned bool   `json:"driver_assigned"`
	AssignmentID   string `json:"assignment_id"`
}

type milestoneResponse struct {
	ID        string `json:"id"`
	Milestone string `json:"milestone"`
}

type uploadResponse struct {
	ObjectKey     string `json:"object_key"`
	UploadURL     string `json:"upload_url"`
	Method        string `json:"method"`
	ContentType   string `json:"content_type"`
	ContentLength int64  `json:"content_length"`
}

// seeder holds the credentials one run works through.
//
// **Several clients rather than one**, because the platform's answer depends on who is asking and
// that is most of what a demonstration shows: the customer sees a budget the providers cannot, a
// provider sees only the jobs they are eligible for, and the driver sees one job and nothing else.
type seeder struct {
	anonymous *client
	customer  *client
	admin     *client

	// providers is keyed by address. The order they are worked in is [demoProviders]', not a
	// map's, so the output of two runs reads the same.
	providers map[string]*client

	out io.Writer
	now time.Time
}

// step reports progress. A seed against a hosted instance is somebody watching a terminal, and a
// command that prints nothing for ninety seconds looks hung rather than busy.
func (s *seeder) step(format string, args ...any) {
	fmt.Fprintf(s.out, "  %s\n", fmt.Sprintf(format, args...))
}

// signIn opens every session the run needs.
//
// Done in one place and up front, so that a wrong password fails before anything has been written
// rather than after two providers have been verified.
func (s *seeder) signIn(ctx context.Context, creds credentials) error {
	var err error
	if s.customer, err = s.anonymous.signIn(ctx, demoCustomer.Email, creds.User); err != nil {
		return err
	}
	if s.admin, err = s.anonymous.signInAdministrator(ctx, demoAdministratorEmail, creds.Administrator); err != nil {
		return err
	}

	s.providers = make(map[string]*client, len(demoProviders))
	for _, provider := range demoProviders {
		signedIn, err := s.anonymous.signIn(ctx, provider.Account.Email, creds.User)
		if err != nil {
			return err
		}
		s.providers[provider.Account.Email] = signedIn
	}
	return nil
}

// prepareProviders makes every provider able to bid.
//
// The order matters and is the order a real provider goes through: describe the business, put a
// vehicle in the fleet, submit the documents, and be decided on by an administrator. A provider
// missing any one of the four is invisible in the feed, and — this is the part worth knowing —
// **the failure is silent**. `fleet.eligible` is one predicate with four `EXISTS` clauses in it, so
// a provider with no declared service area does not see an error; they see an empty board, which is
// indistinguishable from there being no work.
func (s *seeder) prepareProviders(ctx context.Context) error {
	for _, provider := range demoProviders {
		caller := s.providers[provider.Account.Email]

		if err := s.describeProvider(ctx, caller, provider); err != nil {
			return err
		}
		if err := s.stockFleet(ctx, caller, provider); err != nil {
			return err
		}
		if err := s.submitDocuments(ctx, caller, provider); err != nil {
			return err
		}
		if err := s.verifyProvider(ctx, caller, provider); err != nil {
			return err
		}
	}
	return nil
}

// describeProvider writes the public profile and the service area.
//
// A `PATCH` is idempotent by construction — it states the whole of what the seed wants to be true
// and does not care what was there — so this one step needs no read before it.
func (s *seeder) describeProvider(ctx context.Context, caller *client, provider demoProvider) error {
	area := map[string]any{}
	if len(provider.States) > 0 {
		area["states"] = provider.States
	}
	if len(provider.Postcodes) > 0 {
		area["postcodes"] = provider.Postcodes
	}

	body := map[string]any{
		"display_name": provider.DisplayName,
		"operates_as":  provider.OperatesAs,
		"service_area": area,
	}
	if err := caller.patch(ctx, "/v1/fleet/profile", body, nil); err != nil {
		return fmt.Errorf("describing %s: %w", provider.DisplayName, err)
	}
	s.step("%s: profile and service area", provider.DisplayName)
	return nil
}

// stockFleet adds the vehicles that are not there yet.
//
// The registration is the natural key, and the platform agrees: `uq_vehicles_provider_registration`
// refuses a plate a provider already has in service. The read here is what turns that refusal into
// a skip rather than an error the run has to interpret.
func (s *seeder) stockFleet(ctx context.Context, caller *client, provider demoProvider) error {
	var held page[vehicleResponse]
	if err := caller.get(ctx, "/v1/fleet/vehicles?limit=100", &held); err != nil {
		return fmt.Errorf("reading %s's fleet: %w", provider.DisplayName, err)
	}

	existing := make(map[string]bool, len(held.Data))
	for _, vehicle := range held.Data {
		existing[strings.ToUpper(vehicle.Registration)] = true
	}

	for _, vehicle := range provider.Vehicles {
		if existing[strings.ToUpper(vehicle.Registration)] {
			continue
		}
		body := map[string]any{
			"registration":   vehicle.Registration,
			"vehicle_type":   vehicle.Type,
			"make":           vehicle.Make,
			"model":          vehicle.Model,
			"max_weight_kg":  vehicle.MaxWeightKg,
			"load_length_cm": vehicle.LoadLengthCm,
			"load_width_cm":  vehicle.LoadWidthCm,
			"load_height_cm": vehicle.LoadHeightCm,
		}
		if err := caller.post(ctx, "/v1/fleet/vehicles", body, nil); err != nil {
			return fmt.Errorf("adding %s to %s's fleet: %w",
				vehicle.Registration, provider.DisplayName, err)
		}
		s.step("%s: vehicle %s", provider.DisplayName, vehicle.Registration)
	}
	return nil
}

// verificationDocuments are the kinds every seeded provider submits.
//
// Three of the four `profiles` knows about. `abn_evidence` is left off deliberately: `Docs/04` §3
// records that *which* documents are legally required is still an open question owned by an
// adviser, so a seed that submitted the complete set would be asserting an answer the project has
// not got. Three is enough to demonstrate a queue, a reviewer opening evidence, and a decision.
var verificationDocuments = []string{"licence", "registration", "insurance"}

// submitDocuments uploads the evidence a reviewer opens.
//
// Each document is a drawn image, not a photograph — photo.go carries the reasoning, and it applies
// twice as strongly here: a real driver's licence is the single most identifying document in the
// product.
//
// **The upload is two requests and one of them does not come here.** The platform authorises one
// object of one type and one length for a few minutes, and the bytes then go straight to the store.
// A seed that could not do that would be a seed that had to be trusted with the bucket's
// credentials, which is the arrangement the pre-signed URL exists to avoid.
func (s *seeder) submitDocuments(ctx context.Context, caller *client, provider demoProvider) error {
	var held page[documentResponse]
	if err := caller.get(ctx, "/v1/provider/verification/documents?limit=100", &held); err != nil {
		return fmt.Errorf("reading %s's documents: %w", provider.DisplayName, err)
	}

	submitted := make(map[string]bool, len(held.Data))
	for _, document := range held.Data {
		submitted[document.Kind] = true
	}

	for _, kind := range verificationDocuments {
		if submitted[kind] {
			continue
		}

		image, err := proofPhotograph(provider.Account.Email + ":" + kind)
		if err != nil {
			return err
		}

		var upload uploadResponse
		request := map[string]any{
			"content_type":   proofContentType,
			"content_length": len(image),
		}
		if err := caller.post(ctx, "/v1/provider/verification/documents/uploads", request, &upload); err != nil {
			return fmt.Errorf("asking to upload %s's %s: %w", provider.DisplayName, kind, err)
		}
		if err := caller.putObject(ctx, upload.UploadURL, proofContentType, image); err != nil {
			return fmt.Errorf("uploading %s's %s: %w", provider.DisplayName, kind, err)
		}

		record := map[string]any{"kind": kind, "object_key": upload.ObjectKey}
		if err := caller.post(ctx, "/v1/provider/verification/documents", record, nil); err != nil {
			return fmt.Errorf("recording %s's %s: %w", provider.DisplayName, kind, err)
		}
		s.step("%s: %s submitted", provider.DisplayName, kind)
	}
	return nil
}

// verifyProvider has an administrator decide, through the endpoint an administrator uses.
//
// # The state is read from the provider's own view rather than from the queue
//
// `GET /v1/admin/verifications` would need a page walk and a filter; `GET /v1/provider/verification`
// answers in one request and answers the question actually being asked — is *this* provider
// Verified. It is also the reading the demonstration depends on: SHIP-188d's *Done when* ends with
// "the provider's own app then reports the new state", and this is that same read.
func (s *seeder) verifyProvider(ctx context.Context, caller *client, provider demoProvider) error {
	var state verificationResponse
	if err := caller.get(ctx, "/v1/provider/verification", &state); err != nil {
		return fmt.Errorf("reading %s's verification: %w", provider.DisplayName, err)
	}
	if state.State == string(profiles.StateVerified) {
		return nil
	}

	decision := map[string]any{
		"state": string(profiles.StateVerified),
		"reason": "Licence, registration and insurance sighted and current. " +
			"Seeded for the demonstration instance.",
	}
	path := fmt.Sprintf("/v1/admin/verifications/%s/decision", accountID(provider.Account.Email))
	if err := s.admin.post(ctx, path, decision, nil); err != nil {
		return fmt.Errorf("verifying %s: %w", provider.DisplayName, err)
	}
	s.step("%s: verified by %s", provider.DisplayName, demoAdministratorName)
	return nil
}

// seedJobs walks every job to the stage it is declared at.
func (s *seeder) seedJobs(ctx context.Context) error {
	existing, err := s.customerJobs(ctx)
	if err != nil {
		return err
	}

	for _, job := range demoJobs {
		if err := s.seedJob(ctx, job, existing); err != nil {
			return fmt.Errorf("%s: %w", short(job.GoodsDescription), err)
		}
	}
	return nil
}

// customerJobs reads every job the demonstration customer owns, keyed by goods description.
//
// The whole list is walked rather than filtered, because the seed needs to recognise a job at any
// status — a delivered one is not in `?status=Open`, and a seed that only looked at open jobs would
// cheerfully create a second copy of the completed delivery on every run.
func (s *seeder) customerJobs(ctx context.Context) (map[string]jobResponse, error) {
	found := make(map[string]jobResponse)
	cursor := ""

	for {
		path := "/v1/jobs?limit=100"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}

		var listed page[jobResponse]
		if err := s.customer.get(ctx, path, &listed); err != nil {
			return nil, fmt.Errorf("reading the customer's jobs: %w", err)
		}
		for _, job := range listed.Data {
			found[job.GoodsDescription] = job
		}
		if !listed.HasMore || listed.NextCursor == "" {
			return found, nil
		}
		cursor = listed.NextCursor
	}
}

// seedJob drives one job as far as its declared stage.
//
// # The live status is read rather than assumed, and that is what makes a re-run resumable
//
// Two things move a job between the list read at the top of the run and this point: publishing it,
// two lines above, and a previous run that took it further. So the status is fetched once here and
// every later step is guarded on it — a job already Awarded is not bid on again, and a job already
// carried is not awarded again. Without the read, a second run reaches `POST .../bids` on an awarded
// job and is told 404 by the eligibility filter, which is true and unhelpful.
func (s *seeder) seedJob(ctx context.Context, job demoJob, existing map[string]jobResponse) error {
	held, known := existing[job.GoodsDescription]

	jobID := held.ID
	if !known {
		created, err := s.createJob(ctx, job)
		if err != nil {
			return err
		}
		jobID = created.ID
		held = created
		s.step("job created: %s", short(job.GoodsDescription))
	}

	if held.Status == jobs.StatusDraft.Wire() {
		if err := s.customer.post(ctx, "/v1/jobs/"+jobID+"/publish",
			map[string]any{"accepts_terms": true}, nil); err != nil {
			return fmt.Errorf("publishing: %w", err)
		}
		s.step("published: %s", short(job.GoodsDescription))
	}

	if job.Stage == stageOpen {
		return nil
	}

	var current jobResponse
	if err := s.customer.get(ctx, "/v1/jobs/"+jobID, &current); err != nil {
		return fmt.Errorf("reading the job: %w", err)
	}
	biddable := current.Status == jobs.StatusOpen.Wire() ||
		current.Status == jobs.StatusNegotiating.Wire()

	var placed map[string]bidResponse
	if biddable {
		var err error
		if placed, err = s.placeBids(ctx, job, jobID); err != nil {
			return err
		}
	}

	if job.Stage == stageBidsIn {
		return nil
	}

	if biddable {
		if err := s.awardJob(ctx, job, jobID, placed); err != nil {
			return err
		}
	}
	return s.deliverJob(ctx, job, jobID)
}

// createJob drafts the job as the customer.
//
// Windows are absolute instants computed from the run's own start, so a dataset seeded today reads
// as today's work. RFC 3339 throughout, which is what the platform parses.
func (s *seeder) createJob(ctx context.Context, job demoJob) (jobResponse, error) {
	body := map[string]any{
		"pickup":            address(job.Pickup),
		"dropoff":           address(job.Dropoff),
		"goods_description": job.GoodsDescription,
		"goods_category":    job.GoodsCategory,
		"weight_kg":         job.WeightKg,
		"length_cm":         job.LengthCm,
		"width_cm":          job.WidthCm,
		"height_cm":         job.HeightCm,
		"pickup_window":     s.window(job.PickupWindowStart, job.PickupWindowEnd),
		"dropoff_window":    s.window(job.DropoffWindowStart, job.DropoffWindowEnd),
	}
	if job.VehicleRequirement != "" {
		body["vehicle_requirement"] = job.VehicleRequirement
	}
	if job.HandlingNotes != "" {
		body["handling_notes"] = job.HandlingNotes
	}
	if job.BudgetCents > 0 {
		body["budget_cents"] = job.BudgetCents
	}

	var created jobResponse
	if err := s.customer.post(ctx, "/v1/jobs", body, &created); err != nil {
		return jobResponse{}, fmt.Errorf("creating the draft: %w", err)
	}
	return created, nil
}

// placeBids has each declared provider offer, and returns the bids on the job by provider.
//
// The read is the customer's `bids/received` rather than each provider's own list: one request
// instead of three, and it is the same view the award below is made from.
func (s *seeder) placeBids(ctx context.Context, job demoJob, jobID string) (map[string]bidResponse, error) {
	var received page[bidResponse]
	if err := s.customer.get(ctx, "/v1/jobs/"+jobID+"/bids/received?limit=100", &received); err != nil {
		return nil, fmt.Errorf("reading the bids received: %w", err)
	}

	byProvider := make(map[string]bidResponse, len(received.Data))
	for _, bid := range received.Data {
		byProvider[bid.Provider.ID] = bid
	}

	for _, bid := range job.Bids {
		providerID := accountID(bid.ProviderEmail).String()
		if _, already := byProvider[providerID]; already {
			continue
		}

		caller, known := s.providers[bid.ProviderEmail]
		if !known {
			return nil, fmt.Errorf("no seeded provider at %s", bid.ProviderEmail)
		}

		vehicleID, err := s.firstVehicle(ctx, caller)
		if err != nil {
			return nil, err
		}

		body := map[string]any{
			"amount_cents": bid.AmountCents,
			"pickup_at":    s.at(bid.PickupAt),
			"deliver_by":   s.at(bid.DeliverBy),
			"message":      bid.Message,
			"vehicle_id":   vehicleID,
		}
		var placed bidResponse
		if err := caller.post(ctx, "/v1/jobs/"+jobID+"/bids", body, &placed); err != nil {
			return nil, fmt.Errorf("bidding as %s: %w", bid.ProviderEmail, err)
		}
		byProvider[providerID] = placed
		s.step("bid: %s offered %s", bid.ProviderEmail, money(bid.AmountCents))
	}
	return byProvider, nil
}

// firstVehicle is the vehicle a provider offers with.
//
// The dataset does not say which vehicle bids, because it does not matter to anything being
// demonstrated and naming one would be a second place the registrations are written down.
func (s *seeder) firstVehicle(ctx context.Context, caller *client) (string, error) {
	var held page[vehicleResponse]
	if err := caller.get(ctx, "/v1/fleet/vehicles?limit=100", &held); err != nil {
		return "", fmt.Errorf("reading the fleet: %w", err)
	}
	for _, vehicle := range held.Data {
		if vehicle.Active {
			return vehicle.ID, nil
		}
	}
	return "", fmt.Errorf("the provider has no active vehicle to bid with")
}

// awardJob accepts the declared bid.
//
// **Exactly one bid per job is ever accepted**, and that is a partial unique index rather than a
// line of Go — so a seed that awarded twice would be refused by PostgreSQL, not by a check here.
// The guard below is about not making a pointless request, not about correctness.
func (s *seeder) awardJob(ctx context.Context, job demoJob, jobID string, placed map[string]bidResponse) error {
	winner, known := placed[accountID(job.AwardTo).String()]
	if !known {
		return fmt.Errorf("no bid from %s to award", job.AwardTo)
	}

	if err := s.customer.post(ctx, "/v1/jobs/"+jobID+"/award",
		map[string]any{"bid_id": winner.ID}, nil); err != nil {
		return fmt.Errorf("awarding to %s: %w", job.AwardTo, err)
	}
	s.step("awarded to %s", job.AwardTo)
	return nil
}

// deliveryMilestones is how far each stage is carried.
//
// "Driver assigned" is not in either list: the platform records it itself when the assignment is
// made, and sending it would be the seed claiming a milestone the product already owns.
var deliveryMilestones = map[stage][]delivery.Milestone{
	stageInProgress: {
		delivery.MilestoneEnRouteToPickup,
		delivery.MilestonePickedUp,
		delivery.MilestoneInTransit,
	},
	stageDelivered: {
		delivery.MilestoneEnRouteToPickup,
		delivery.MilestonePickedUp,
		delivery.MilestoneInTransit,
		delivery.MilestoneDelivered,
	},
}

// deliverJob assigns a driver and records the milestones, as the driver.
//
// # The driver is a third credential, and using it is the point
//
// A job-scoped token reaches exactly one job, is not a mobile session and cannot be exchanged for
// one. The seed could record these milestones as the customer — `POST /v1/jobs/{id}/milestones`
// exists and is served — and it does not, because a demonstration in which the driver's link was
// never used would not show that the driver portal works at all.
func (s *seeder) deliverJob(ctx context.Context, job demoJob, jobID string) error {
	if job.Driver == nil {
		return fmt.Errorf("stage needs a driver and the dataset names none")
	}

	// **The provider assigns, not the customer**, and the endpoint answers a customer with a
	// 404 rather than a 403 — so a seed that had this the wrong way round was told the job did
	// not exist. Who drives is the winning provider's decision: they hold the contract, they
	// know which of their drivers is free, and the customer never chose a person.
	carrier, known := s.providers[job.AwardTo]
	if !known {
		return fmt.Errorf("no seeded provider at %s to carry the job", job.AwardTo)
	}

	token, err := s.assignDriver(ctx, carrier, job, jobID)
	if err != nil {
		return err
	}
	driver := s.anonymous.as(token)

	var recorded page[milestoneResponse]
	if err := driver.get(ctx, "/v1/driver/jobs/"+jobID+"/milestones?limit=100", &recorded); err != nil {
		return fmt.Errorf("reading the milestones: %w", err)
	}
	already := make(map[string]bool, len(recorded.Data))
	for _, milestone := range recorded.Data {
		already[milestone.Milestone] = true
	}

	wanted := deliveryMilestones[job.Stage]
	for index, milestone := range wanted {
		if already[milestone.Wire()] {
			continue
		}

		body := map[string]any{
			"milestone": milestone.Wire(),
			// Spread apart rather than posted at one instant, so the customer's tracking
			// screen reads as a journey. [demoJob.CarriedFrom] says why these are in the
			// past while the bid that won the job promises a pickup in the future.
			"recorded_at": s.at(job.CarriedFrom + time.Duration(index)*milestoneSpacing),
		}

		if milestone == delivery.MilestoneDelivered {
			proof, err := s.uploadProof(ctx, driver, jobID, job)
			if err != nil {
				return err
			}
			body["proof"] = map[string]any{"object_key": proof}
			body["recipient_name"] = "Site supervisor"
			body["delivery_note"] = "Left inside the loading dock, signed for on site."
		}

		if err := driver.post(ctx, "/v1/driver/jobs/"+jobID+"/milestones", body, nil); err != nil {
			return fmt.Errorf("recording %q: %w", milestone, err)
		}
		s.step("milestone: %s", milestone)
	}
	return nil
}

// assignDriver names the driver if nobody is named yet, and returns a live job-scoped token.
//
// Assignment answers with a token; a job that already has a driver needs `driver/link` to mint a
// fresh one, because the token from the first run expired long ago and was never stored anywhere.
func (s *seeder) assignDriver(ctx context.Context, carrier *client, job demoJob, jobID string) (string, error) {
	var detail deliveryDetailResponse
	if err := carrier.get(ctx, "/v1/jobs/"+jobID+"/delivery/detail", &detail); err != nil {
		return "", fmt.Errorf("reading the delivery: %w", err)
	}

	var assignment assignmentResponse
	if detail.DriverAssigned {
		if err := carrier.post(ctx, "/v1/jobs/"+jobID+"/driver/link", map[string]any{}, &assignment); err != nil {
			return "", fmt.Errorf("re-issuing the driver link: %w", err)
		}
		return assignment.DriverToken, nil
	}

	body := map[string]any{
		"driver_name":   job.Driver.Name,
		"driver_mobile": job.Driver.Mobile,
	}
	if err := carrier.post(ctx, "/v1/jobs/"+jobID+"/driver", body, &assignment); err != nil {
		return "", fmt.Errorf("assigning %s: %w", job.Driver.Name, err)
	}
	s.step("driver assigned: %s", job.Driver.Name)
	return assignment.DriverToken, nil
}

// uploadProof puts the photograph in the store and returns the key the milestone will carry.
func (s *seeder) uploadProof(ctx context.Context, driver *client, jobID string, job demoJob) (string, error) {
	image, err := proofPhotograph(job.GoodsDescription)
	if err != nil {
		return "", err
	}

	var upload uploadResponse
	request := map[string]any{
		"content_type":   proofContentType,
		"content_length": len(image),
	}
	if err := driver.post(ctx, "/v1/driver/jobs/"+jobID+"/proof-uploads", request, &upload); err != nil {
		return "", fmt.Errorf("asking to upload the proof: %w", err)
	}
	if err := driver.putObject(ctx, upload.UploadURL, proofContentType, image); err != nil {
		return "", fmt.Errorf("uploading the proof: %w", err)
	}
	s.step("proof photograph uploaded (%d bytes)", len(image))
	return upload.ObjectKey, nil
}

// window is a pickup or drop-off window as the API wants it.
func (s *seeder) window(start, end time.Duration) map[string]string {
	return map[string]string{"start": s.at(start), "end": s.at(end)}
}

// at turns an offset from the run's start into an RFC 3339 instant.
func (s *seeder) at(offset time.Duration) string {
	return s.now.Add(offset).UTC().Format(time.RFC3339)
}

// address is the four-part address on the wire.
func address(a demoAddress) map[string]string {
	return map[string]string{
		"line": a.Line, "suburb": a.Suburb, "state": a.State, "postcode": a.Postcode,
	}
}

// money renders cents as dollars, for the progress output only.
func money(cents int64) string {
	return fmt.Sprintf("$%d.%02d", cents/100, cents%100)
}

// short trims a goods description down to something that fits a progress line.
func short(description string) string {
	const width = 52
	if len(description) <= width {
		return description
	}
	return description[:width-1] + "…"
}
