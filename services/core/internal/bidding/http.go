// The HTTP surface of the bidding domain.
//
// Handlers live in the domain rather than in cmd/api (Docs/10 §2.1). The reason is a merge hazard
// rather than taste: it keeps cmd/api/routes.go free of per-domain edits, which is what lets two
// domains be built at once without touching a shared file. A domain importing internal/httpx is
// sitting on infrastructure, not crossing a boundary, and the import lint permits it.
//
// # This is a provider-facing file, so two privacy rules apply at once and they are not the same rule
//
// **The customer's budget never reaches a provider** in any form — not an amount, not a band, not a
// "budget supplied" flag (Docs/01 §4.3, CLAUDE.md). Nothing in this file reads a job at all, which is
// the strongest available form of that: there is no field to redact because there is no job here to
// redact it from.
//
// **And one provider never sees another's offer.** Docs/01 §4.3's second line — "treat provider bid
// price as private from competing providers" — is a separate rule with a separate failure mode, and
// the place it would break is not a response field but a *lookup*: a query that found a bid by job
// and idempotency key alone would hand one provider another's price to anybody who reused a key.
// The store's read is scoped by provider for exactly that reason, and the provider comes from the
// authenticated subject rather than from anything a client can send.
//
// TestTheBidResponseCarriesNothingOfTheCustomers holds the serialised response to a closed set of
// keys at every depth, and TestOneProvidersKeyCannotReachAnothersBid drives the second rule over the
// wire. A closed set rather than a search for the word "budget", because a field named `max_price`
// is a budget and does not contain the word — the axis SHIP-83 found a source-parsing guard cannot
// have.
//
// # Status appears in the response and in no request
//
// A bid's status is the platform's, exactly as a job's is (Docs/02 §2, CLAUDE.md). The request type
// carries no `status` field and httpx.DecodeJSON refuses unknown fields, so a client sending
// `"status": "accepted"` is told the field does not exist rather than having it silently ignored —
// which is the failure worth preventing, because awarding is the customer's act and a provider that
// believed it had self-accepted would show a won job that nobody awarded.
//
// The blank line below keeps this a file note rather than a second package comment.

package bidding

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/pagination"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// Handler serves this domain's routes.
//
// It is built in cmd/api/routes_bidding.go from Deps. The pool is held here rather than inside the
// service because Docs/10 §3.2 puts the transaction boundary with whoever owns the invariant, and
// for a placement that is this layer: the eligibility read and the insert it authorises are one
// decision.
type Handler struct {
	svc  *Service
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewHandler wires the handlers to the service.
//
// The pool may be nil and that is not an error. The service starts with an unreachable database on
// purpose — a rolling deployment during a failover would otherwise take every instance down at once
// — so a nil pool is a condition the handlers answer 503 to for as long as it lasts, not a reason to
// refuse to start.
func NewHandler(svc *Service, pool *pgxpool.Pool, log *slog.Logger) (*Handler, error) {
	if svc == nil {
		return nil, errors.New("bidding: a handler needs a service")
	}
	if log == nil {
		return nil, errors.New("bidding: a handler needs a logger")
	}
	return &Handler{svc: svc, pool: pool, log: log}, nil
}

// bidRequest is the body of POST /v1/jobs/{id}/bids.
//
// Every field is a plain value rather than a pointer, unlike jobs' draft request. The three-way
// distinction a draft needs — absent, null, explicitly empty — has no meaning here: an offer is
// placed whole, there is nothing stored to leave alone or to clear, and every field but the message
// is required. SHIP-85's revision is a different request with a different shape and will need the
// pointers this one does not.
//
// There is no `job_id`, no `provider_id` and no `status`. The job is in the path, the provider is
// whoever the token says is calling, and the status is the platform's. A provider id in a body would
// be an authorisation decision made from client input, which Docs/07 §3 puts on the platform.
type bidRequest struct {
	// AmountCents is what the provider is asking, in cents.
	//
	// Minor units as a whole number rather than dollars as a decimal, for the reason Docs/10 §3.3
	// gives for the Go and PostgreSQL sides: money is never a float, and a JSON number written as
	// 450.50 is one. The name says the unit, because a field called `amount` holding 45000 is a
	// field somebody eventually reads as dollars.
	AmountCents int64 `json:"amount_cents"`

	// PickupAt and DeliverBy are RFC 3339 strings, parsed by hand below.
	//
	// Strings rather than time.Time for the reason jobs' windowBody takes: encoding/json reports a
	// bad timestamp as an ordinary error with no type of its own, so httpx.DecodeJSON could only
	// answer "the request body is not valid JSON" — which is untrue and unactionable when the body
	// is perfectly good JSON containing "next tuesday". Parsing here produces a field error naming
	// the field (Docs/10 §4.6).
	PickupAt  string `json:"pickup_at"`
	DeliverBy string `json:"deliver_by"`

	// Message is the conditions accompanying the offer, and one of two optional fields.
	Message string `json:"message"`

	// VehicleID is the vehicle the offer is made with, as a string parsed by hand below (SHIP-102a).
	//
	// A string rather than a uuid.UUID for the reason the two timestamps are strings: encoding/json
	// reports an unparseable UUID as an ordinary error with no type of its own, so httpx.DecodeJSON
	// could only answer "the request body is not valid JSON" — which is untrue and unactionable when
	// the body is good JSON containing "my-van". Parsing here produces a field error naming the
	// field (Docs/10 §4.6).
	//
	// **Optional, which is Docs/10 §4.2 rather than a soft rule**: this field arrived at SHIP-102a,
	// long after `POST /v1/jobs/{id}/bids` was being served, and a field added to a request is
	// optional or it is a new endpoint.
	VehicleID string `json:"vehicle_id"`
}

// offer turns the request into the domain's command, reporting anything it could not parse.
//
// Only parsing failures are reported here. Whether a value is *acceptable* is the domain's judgement
// and is made in [Offer.validate], so that the same rules apply to every caller rather than to
// whoever came in through HTTP.
func (b bidRequest) offer(key string, now time.Time) (Offer, error) {
	var e validate.Errors

	o := Offer{
		AmountCents: b.AmountCents,
		PickupAt:    instant("pickup_at", b.PickupAt, &e),
		DeliverBy:   instant("deliver_by", b.DeliverBy, &e),
		Message:     b.Message,
		VehicleID:   reference("vehicle_id", b.VehicleID, &e),
		Key:         key,
	}

	if !e.Any() {
		return o, nil
	}

	// A timestamp could not be parsed, so the service will never see this offer and will never have
	// the chance to complain about the rest of it. Its complaints are collected here instead and
	// answered together — a provider who mistypes a date and an amount should be told about both at
	// once rather than fixing one and being sent back for the other. The same arrangement jobs'
	// draftRequest.fields makes, and for the same reason.
	//
	// **A field that could not be read is reported once, not twice.** The domain sees an unparseable
	// timestamp as an absent one, and would add "say when you can collect" beside "that is not a
	// date" — a second and misleading complaint about an error the provider already has. So the
	// fields that failed to parse are excluded from the domain's half.
	unreadable := map[string]bool{}
	for _, problem := range e.Fields() {
		unreadable[problem.Field] = true
	}

	var apiErr *httpx.Error
	if err := o.normalise().validate(now); errors.As(err, &apiErr) {
		for _, problem := range apiErr.Details {
			if !unreadable[problem.Field] {
				e.Add(problem.Field, problem.Code, "%s", problem.Message)
			}
		}
	}
	return Offer{}, e.Err()
}

// instant parses one RFC 3339 timestamp, treating empty as absent.
//
// Absent is left to [Offer.validate] to complain about rather than reported here, so that "you did
// not say when" and "that is not a date" are one rule expressed once — in the domain, where every
// caller meets it.
func instant(field, value string, e *validate.Errors) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}

	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		e.Add(field, validate.CodeInvalid,
			"Enter a date and time in RFC 3339 form, such as 2026-08-14T09:00:00+10:00.")
		return time.Time{}
	}
	return t.UTC()
}

// reference parses a UUID from a request body, reporting a field error rather than a decode error.
//
// Named for what it is rather than for its type: every UUID in a request body here refers to a row
// in another table, and `identifier` is already the name http_test.go gives the regular expression
// that finds one in a response.
//
// The [instant] of identifiers, and for the same reason: encoding/json cannot say which field a bad
// value was in, and "the request body is not valid JSON" is untrue of a body that is.
//
// An empty or absent value is [uuid.Nil], which every caller reads as "not given". That is the
// ordinary case for `vehicle_id`, so it must not be a complaint.
func reference(field, value string, e *validate.Errors) uuid.UUID {
	if strings.TrimSpace(value) == "" {
		return uuid.Nil
	}

	id, err := uuid.Parse(value)
	if err != nil {
		e.Add(field, validate.CodeInvalid, "Enter an identifier from your own fleet.")
		return uuid.Nil
	}
	return id
}

// bidResponse is an offer as the provider who made it sees it.
//
// **This is the only shape this package serialises, and its key set is closed by test.** Every key
// below is one this API promises a provider; a field arriving under any other name fails
// TestTheBidResponseCarriesNothingOfTheCustomers whatever it is called. The `Bid` schema in
// contracts/paths/bidding.yaml is `additionalProperties: false` for the same reason.
//
// There is no provider id: the only caller who can obtain a bid today is the one who made it, so the
// field would be the client's own identifier read back to it. There is no idempotency key either —
// a client that sent one already has it. And there is nothing at all about the job beyond its
// identifier, which is what makes Docs/01 §4.3 structurally true here rather than remembered.
//
// Responses stay additive (Docs/07 §6): fields are added here, never repurposed or removed. Adding
// one means adding it to the test's key set and to the contract schema, deliberately, which is the
// point of both.
type bidResponse struct {
	ID     string `json:"id"`
	JobID  string `json:"job_id"`
	Status string `json:"status"`

	// OfferedBy is which party made this offer — `provider` or `customer` (SHIP-87).
	//
	// **A key added deliberately, in the three places the closed set lives**: here, `providerBidKeys`
	// in http_test.go, and the `Bid` schema in contracts/paths/bidding.yaml. That is the cost the
	// closed set is meant to impose, and it is paid rather than worked around.
	//
	// It is not derivable by the client. A chain alternates, so a client could in principle count
	// from the end — except that it may join a negotiation halfway (a provider opening an old bid, a
	// customer returning to a job), and a negotiation may hold more than one chain, since a withdrawn
	// offer can be replaced. Rendering "you" against "them" is the whole of what a negotiation screen
	// does, and inferring it is not acceptable there.
	//
	// [Bid.ProviderID] is still absent, and now for a better reason than SHIP-84's: both callers who
	// can obtain a bid already know which provider the negotiation is with.
	OfferedBy string `json:"offered_by"`

	AmountCents int64 `json:"amount_cents"`

	PickupAt  string `json:"pickup_at"`
	DeliverBy string `json:"deliver_by"`

	// Message is omitted when none was given, so a client can tell "no conditions" from "an empty
	// note" without a second flag.
	Message string `json:"message,omitempty"`

	// SupersededBy is the counter-offer that displaced this one, omitted while this is the live head
	// of its chain (SHIP-88).
	//
	// **This is what makes a history readable rather than merely ordered.** The chain endpoint returns
	// a negotiation's offers oldest first, which is enough to *show*; this is what lets a client say
	// which offer answered which. Its absence is the single most useful thing in the shape — it names
	// the one offer still standing, which is the only one anybody can act on, and it is the same fact
	// `ck_bids_superseded_is_not_live` holds the award to.
	SupersededBy string `json:"superseded_by,omitempty"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func bidFrom(b Bid) bidResponse {
	response := bidResponse{
		ID:        b.ID.String(),
		JobID:     b.JobID.String(),
		Status:    b.Status.Wire(),
		OfferedBy: b.OfferedBy.Wire(),

		AmountCents: b.AmountCents,

		PickupAt:  timestamp(b.PickupAt),
		DeliverBy: timestamp(b.DeliverBy),
		Message:   b.Message,

		CreatedAt: timestamp(b.CreatedAt),
		UpdatedAt: timestamp(b.UpdatedAt),
	}
	if b.SupersededBy != uuid.Nil {
		response.SupersededBy = b.SupersededBy.String()
	}
	return response
}

// chainResponse is one negotiation's offers, oldest first (SHIP-88).
//
// The collection envelope of Docs/10 §4.5, so a client tells a collection from a single resource
// without knowing the endpoint. `next_cursor` is always null: this does not page, for the reason
// [maxChainLength] gives, and `has_more` is a truncation report rather than an invitation to ask for
// the rest — the reading `identity`'s device list takes and states.
//
// **The element type is the same [bidResponse] every other endpoint here answers with**, which is
// what keeps the closed key set one list rather than two. A separate "offer in a history" shape would
// be a second place for a field to be added without anybody noticing it had reached a provider — and
// this is the response where the *customer's* numbers reach one.
type chainResponse struct {
	Data       []bidResponse `json:"data"`
	NextCursor *string       `json:"next_cursor"`
	HasMore    bool          `json:"has_more"`
}

func chainFrom(offers []Bid, truncated bool) chainResponse {
	// A non-nil empty slice, so the field is `[]` rather than `null`. Unreachable through the
	// endpoint — a caller reaches a chain by naming an offer in it — and written anyway, because the
	// shape must not depend on that staying true.
	data := make([]bidResponse, 0, len(offers))
	for _, offer := range offers {
		data = append(data, bidFrom(offer))
	}
	return chainResponse{Data: data, HasMore: truncated}
}

// timestamp renders an instant the way every other endpoint does, in UTC with milliseconds.
func timestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// Place handles POST /v1/jobs/{id}/bids (SHIP-84).
//
// Protected: the bidder is whoever the token says is calling, and there is no field in the request
// for naming somebody else.
//
// **201 when the offer was placed, 200 when this key had already placed it.** The same shape either
// way, so a client that does not care which happened parses one type — the arrangement
// `POST /v1/jobs/{id}/driver` and `POST /v1/jobs/{id}/milestones` already use. A client that *does*
// care reads the status code, and one recovering from a dropped connection generally does not.
//
// State-changing, so it carries an Idempotency-Key like every other mutating route (SHIP-15), and
// the key is read here as well as by the middleware. That is not redundancy: the middleware replays
// a *response* while the entry lives, and the column replays the *row* forever. A phone that retries
// after a day outlives any TTL worth setting, and the second mechanism is what stands between it and
// a 409 for a bid that actually succeeded. See migration 000501 and [Service.PlaceBid].
//
// A job this provider may not bid on answers 404, byte-identically to a job that does not exist. See
// [apiError].
func (h *Handler) Place() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, err := jobIDFrom(r)
		if err != nil {
			return err
		}

		var req bidRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		// The service's clock, not the wall clock. Docs/10 §6.3 injects it so a test can place an
		// offer against a fixed instant, and reading time.Now() here would put half the timing rules
		// on one clock and half on another.
		offer, err := req.offer(r.Header.Get(httpx.HeaderIdempotencyKey), h.svc.clock.Now())
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		var (
			bid     Bid
			created bool
		)
		err = db.InTx(r.Context(), pool, func(ctx context.Context, runner db.Runner) error {
			var err error
			bid, created, err = h.svc.PlaceBid(ctx, runner, providerID, jobID, offer)
			return err
		})
		if err != nil {
			return apiError(err)
		}

		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		httpx.WriteJSON(w, status, bidFrom(bid))
		return nil
	})
}

// reviseRequest is the body of PATCH /v1/jobs/{id}/bids/{bid_id} (SHIP-85).
//
// **Every field is a pointer, which is the shape [bidRequest] said this one would need.** A placement
// is made whole and a revision is not: a provider dropping their price by fifty dollars names the
// price and nothing else, and a client made to restate two timestamps it is not changing is a client
// that can get them wrong. Absent means "leave this alone".
//
// There is still no `job_id`, no `provider_id` and no `status`, for the reasons [bidRequest] gives.
// httpx.DecodeJSON refuses unknown fields, so a client sending one of them is told it does not exist.
type reviseRequest struct {
	AmountCents *int64 `json:"amount_cents"`

	// PickupAt and DeliverBy are RFC 3339 strings, parsed by hand for the reason [bidRequest]'s are.
	PickupAt  *string `json:"pickup_at"`
	DeliverBy *string `json:"deliver_by"`

	// Message is the conditions accompanying the offer. Sending it blank clears it, which is how a
	// provider takes a condition back — the alternative would be a second field meaning "and now
	// remove the note", for a value the column already expresses as NULL.
	Message *string `json:"message"`

	// VehicleID is the vehicle the offer is made with (SHIP-102a). **Present and blank clears it**,
	// which is [reviseRequest.Message]'s treatment applied to an identifier — a provider who named
	// a truck and then decided not to commit to one has to be able to say so.
	//
	// Blank rather than `null`, and that is a constraint of the decoder rather than a preference.
	// `encoding/json` sets a pointer to nil for the JSON literal `null`, so a `*string` cannot tell
	// "absent" from "null" and a `**string` cannot either — the outer pointer is what null nils.
	// `fleet.VehicleFields` reached the same place and states the same rule: omitted or null leaves
	// it alone, an empty value clears it.
	VehicleID *string `json:"vehicle_id"`
}

// revision turns the request into the domain's command, reporting anything it could not parse.
//
// **Unlike [bidRequest.offer], a parse failure is answered on its own rather than merged with the
// domain's complaints**, and that is a consequence of what a revision is judged against. A placement
// is validated against nothing but itself, so the handler can run the domain's validator early and
// report both halves at once. A revision is validated against the *stored* offer — the fields it does
// not name come from the row — and reading that row means finding the bid, which is behind the
// ownership check. Reaching for the value rules here would mean judging a bid before establishing it
// is the caller's, which is the wrong order for a reason that has nothing to do with error messages.
//
// Both timestamps are still reported together, which is the part a provider notices.
func (b reviseRequest) revision() (Revision, error) {
	var e validate.Errors

	rev := Revision{AmountCents: b.AmountCents, Message: b.Message}
	if b.VehicleID != nil {
		// Blank is [uuid.Nil], which the store writes as NULL — [reference] returns it for an empty
		// value rather than complaining, because "no vehicle" is the ordinary answer here.
		id := reference("vehicle_id", *b.VehicleID, &e)
		rev.VehicleID = &id
	}
	if b.PickupAt != nil {
		at := instant("pickup_at", *b.PickupAt, &e)
		rev.PickupAt = &at
	}
	if b.DeliverBy != nil {
		by := instant("deliver_by", *b.DeliverBy, &e)
		rev.DeliverBy = &by
	}

	if err := e.Err(); err != nil {
		return Revision{}, err
	}
	return rev, nil
}

// withdrawRequest is the body of POST /v1/jobs/{id}/bids/{bid_id}/withdraw, and it has no fields at
// all (SHIP-86).
//
// **The client names an intent and the platform decides what the state becomes**, which is the same
// shape `POST /v1/jobs/{id}/cancel` and `POST /v1/fleet/vehicles/{id}/deactivate` take. A `PATCH`
// writing `"status": "withdrawn"` would make a bid's status a settable field, which it is not.
//
// A body is still required, on the reasoning jobs' extendRequest gives: httpx.DecodeJSON refuses an
// empty body, and an endpoint excepted from that would be a second answer to what a request looks
// like. `{}` is the whole request, and because the decoder refuses unknown fields a client that sends
// `{"status": "withdrawn"}` is told the field does not exist rather than having it ignored.
type withdrawRequest struct{}

// Revise handles PATCH /v1/jobs/{id}/bids/{bid_id} (SHIP-85).
//
// Protected, and the reviser is whoever the token says is calling. **"Their own" is the whole of the
// authorisation**: another provider's bid is a 404, byte-identical to a bid that does not exist, and
// so is a bid paired with the wrong job.
//
// A `PATCH` on the bid rather than a second `POST` under the job, because a revision is a change to
// the offer that is already there: it keeps the bid's identifier, its status, and the key it was
// placed under, and the customer sees one offer at a new number rather than two. **A counter-offer is
// the thing that makes a new row** — Docs/02 §4, SHIP-87 — and giving a revision the same shape here
// would settle that ticket's design by accident.
//
// 200 always. There is no 201 to answer with: nothing is created, and a revision that changes a field
// to the value it already held is still the state the caller asked for.
//
// State-changing, so it carries an Idempotency-Key like every other mutating route (SHIP-15) and is
// refused without one by the middleware. **Unlike [Handler.Place] the key is not read here**, because
// there is no column for it and no failure for one to prevent: applying one `UPDATE` twice reaches the
// state applying it once reaches. See [Revision] for the trap that would be introduced by storing it.
func (h *Handler) Revise() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, bidID, err := pathIDs(r)
		if err != nil {
			return err
		}

		var req reviseRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		rev, err := req.revision()
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		var bid Bid
		err = db.InTx(r.Context(), pool, func(ctx context.Context, runner db.Runner) error {
			var err error
			bid, err = h.svc.ReviseBid(ctx, runner, providerID, jobID, bidID, rev)
			return err
		})
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, bidFrom(bid))
		return nil
	})
}

// Withdraw handles POST /v1/jobs/{id}/bids/{bid_id}/withdraw (SHIP-86).
//
// Protected, and the same two rules as [Handler.Revise] decide who may reach a bid: it has to be the
// caller's own, and it has to be on the job in the path.
//
// A verb under the bid rather than a `DELETE`, because nothing is deleted. Docs/01 §4.3 requires the
// platform to record every withdrawal and Docs/02 §4 keeps the history readable, so the row survives
// at [StatusWithdrawn] — the same reading behind `POST /v1/fleet/vehicles/{id}/deactivate`, where a
// vehicle named by a bid and a delivery is likewise never removed.
//
// **200, including when the offer was already withdrawn.** A withdrawal is idempotent by state rather
// than by key: the caller asked for an outcome, and answering "you already did that" with a 409 would
// mean a phone that retried with a fresh key after a restart is told its withdrawal failed when it
// succeeded. See [Service.WithdrawBid].
func (h *Handler) Withdraw() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, bidID, err := pathIDs(r)
		if err != nil {
			return err
		}

		var req withdrawRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		var bid Bid
		err = db.InTx(r.Context(), pool, func(ctx context.Context, runner db.Runner) error {
			var err error
			bid, err = h.svc.WithdrawBid(ctx, runner, providerID, jobID, bidID)
			return err
		})
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, bidFrom(bid))
		return nil
	})
}

// counterRequest is the body of POST /v1/jobs/{id}/bids/{bid_id}/counter (SHIP-87).
//
// **The same four pointers [reviseRequest] carries, and that is 000501's deferred decision cashed.**
// That migration removed `ck_bids_offer_has_timing` because it would have bound this shape: "whether
// a customer countering on *price alone* restates the timing or inherits it from the offer it
// supersedes is that ticket's decision." It inherits — see [Counter] for the two reasons, one shared
// with a revision and one specific to a negotiation.
//
// There is no `offered_by`, no `job_id`, no `provider_id` and no `status`. **Which party is
// countering is derived from the caller and the offer being answered**, never sent: a field naming
// the author would be an authorisation decision made from client input, which Docs/07 §3 puts on the
// platform. httpx.DecodeJSON refuses unknown fields, so a client sending one is told it does not
// exist rather than having it ignored.
type counterRequest struct {
	AmountCents *int64 `json:"amount_cents"`

	// PickupAt and DeliverBy are RFC 3339 strings, parsed by hand for the reason [bidRequest]'s are.
	PickupAt  *string `json:"pickup_at"`
	DeliverBy *string `json:"deliver_by"`

	// Message is the conditions accompanying the counter. Sending it blank clears whatever the offer
	// being answered carried.
	Message *string `json:"message"`
}

// counter turns the request into the domain's command, reporting anything it could not parse.
//
// A parse failure is answered on its own rather than merged with the domain's complaints, for the
// reason [reviseRequest.revision] gives: a counter is validated against the *stored* offer, and
// reading that row means finding the bid, which is behind the authorisation check. Reaching for the
// value rules here would mean judging an offer before establishing the caller may answer it.
//
// The key is read from the header here and not by the middleware alone, exactly as a placement's is
// (SHIP-84). A counter writes a row, so the key is a *column* rather than only a cache entry: the
// middleware replays a response while its entry lives, and the column answers a retry forever. See
// [Counter] and migration 000501.
func (b counterRequest) counter(key string) (Counter, error) {
	var e validate.Errors

	c := Counter{AmountCents: b.AmountCents, Message: b.Message, Key: key}
	if b.PickupAt != nil {
		at := instant("pickup_at", *b.PickupAt, &e)
		c.PickupAt = &at
	}
	if b.DeliverBy != nil {
		by := instant("deliver_by", *b.DeliverBy, &e)
		c.DeliverBy = &by
	}

	if err := e.Err(); err != nil {
		return Counter{}, err
	}
	return c, nil
}

// Counter handles POST /v1/jobs/{id}/bids/{bid_id}/counter (SHIP-87).
//
// **The first endpoint in this domain a customer may call**, and the reason the domain now holds two
// ports. Docs/02 §4 lets either party counter, so one route serves both and the platform works out
// which side the caller is on: the provider is a comparison against `bids.provider_id`, and the
// customer is `jobs`' fact, asked through [Negotiation.CustomerOf].
//
// **The route requires a user and not a role**, exactly as the three before it, and here that matters
// more than it did. A role claim in a token would be the wrong thing to branch on for an endpoint
// whose two callers are told apart by the *database* — and a customer presenting a token claiming
// `provider` is still recognised as the customer of their own job, which is what CLAUDE.md means by
// no authorisation decision on the device.
//
// A verb under the bid rather than a second `POST` on the collection, because a counter *answers a
// particular offer*: the offer being superseded is the resource this acts on, and naming it in the
// path is what makes two counters against one offer a race the platform can see rather than two
// independent creations. `PATCH` would have been wrong for the opposite reason — a counter creates a
// row and leaves the one it answers behind, which is precisely what SHIP-85's revision does not do.
//
// **201, or 200 when this key had already made this counter.** The same pair [Handler.Place] answers
// with, and the same shape either way, so a client that does not care which happened parses one type.
//
// A caller who is neither party answers 404, byte-identically to a bid that does not exist. See
// [apiError].
func (h *Handler) Counter() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		callerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, bidID, err := pathIDs(r)
		if err != nil {
			return err
		}

		var req counterRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		counter, err := req.counter(r.Header.Get(httpx.HeaderIdempotencyKey))
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		var (
			bid     Bid
			created bool
		)
		err = db.InTx(r.Context(), pool, func(ctx context.Context, runner db.Runner) error {
			var err error
			bid, created, err = h.svc.CounterOffer(ctx, runner, callerID, jobID, bidID, counter)
			return err
		})
		if err != nil {
			return apiError(err)
		}

		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		httpx.WriteJSON(w, status, bidFrom(bid))
		return nil
	})
}

// awardRequest is the body of POST /v1/jobs/{id}/award (SHIP-92).
//
// **The one request in this domain that names a bid in the body rather than in the path**, and that
// follows from what the endpoint is rather than from taste. Docs/09's *Done when* is
// `POST /v1/jobs/{id}/award`, and it is right: awarding is an act on the **job** — the job is what
// moves, exactly once, and the bid is what the act selects. `POST …/bids/{bid_id}/award` would have
// read as one more verb on an offer, beside `withdraw` and `counter`, which are acts on the offer and
// leave the job where it is.
//
// A string rather than a `uuid.UUID`, parsed by hand below, for the reason [bidRequest]'s timestamps
// are strings: encoding/json reports a malformed value as an ordinary error with no type of its own,
// so httpx.DecodeJSON could only answer "the request body is not valid JSON" — which is untrue and
// unactionable when the body is perfectly good JSON containing a truncated identifier.
//
// There is no `status` and no `customer_id`. The status is the platform's, and the customer is
// whoever the token says is calling — a customer id in a body would be an authorisation decision
// made from client input, which Docs/07 §3 puts on the platform.
type awardRequest struct {
	BidID string `json:"bid_id"`
}

// bid parses the identifier, reporting anything it could not read as a field error.
//
// Empty and unparseable are one answer with two messages, both naming `bid_id`. A client that sent
// nothing and a client that sent a truncated value have made the same kind of mistake and fix it in
// the same place, and Docs/10 §4.6 wants the field named either way.
func (a awardRequest) bid() (uuid.UUID, error) {
	var e validate.Errors

	switch trimmed := strings.TrimSpace(a.BidID); {
	case trimmed == "":
		e.Add("bid_id", validate.CodeRequired, "Name the offer you are awarding.")
	default:
		id, err := uuid.Parse(trimmed)
		if err == nil {
			return id, nil
		}
		e.Add("bid_id", validate.CodeInvalid, "That is not a valid offer identifier.")
	}
	return uuid.Nil, e.Err()
}

// Award handles POST /v1/jobs/{id}/award (SHIP-92).
//
// SHIP-92's *Done when*: "accepts one bid and moves the job to Awarded in one transaction". Both
// halves happen inside one `db.InTx` here, which is Docs/10 §3.2's rule about where a transaction
// boundary goes — "the award is one transaction and `bidding` owns it, even though it also moves the
// job".
//
// Protected, and **only the customer may award**: Docs/02 §3's first transition control. The route
// declares `RequireUser` rather than a role, exactly as the four before it, and for the sharper
// reason SHIP-87's counter has — whether this account is the customer of *this* job is a fact in the
// database, and a role claim in a token is evidence about the token. A provider presenting a valid
// token is refused with the 404 a job that does not exist gets.
//
// **200, and there is no 201 to answer with.** Nothing is created: one row moves to 'Accepted' and
// the job moves to 'Awarded'. The same 200 answers an award that had already been made, which is the
// idempotency-by-state [Service.AwardBid] describes — a phone that reconnected and generated a fresh
// key is told what it did, not that it failed.
//
// State-changing, so it carries an `Idempotency-Key` and is refused without one by the middleware
// (SHIP-15). **The key is not read here and there is no column for it**, unlike a placement or a
// counter: those are inserts, where a retry that outlives the cache could otherwise add a second row.
// An award is an update of a row that already exists, so the record itself answers a retry. See
// [Service.WithdrawBid], which made the same call for the same reason.
//
// **SHIP-94 is that division stated rather than assumed** (Docs/11 §3, and SHIP-111's before it). The
// middleware replays the stored response while its entry lives and the handler is never reached; past
// that — a TTL, an eviction, a failover, or a phone that restarted and generated a fresh key — this
// handler runs a second time and [Service.AwardBid]'s already-accepted branch answers from the row.
// The one case where the two disagree is a **key reused for a different bid**, which the middleware
// refuses with `idempotency_key_reused` before this function is called, because it fingerprints the
// body and that is a different request rather than a retry of this one.
//
// The response is the accepted bid, in the same [bidResponse] every other endpoint here answers with
// — the closed key set stays one list rather than two, and nothing of the job travels in it beyond
// its identifier. A client that wants the job's new status reads the job.
//
// **Nothing of the offers the award closed travels in it either** (SHIP-93). One request accepted one
// offer, and that is what it answers with; the competing offers now at `Rejected` are read where the
// job's bids have always been read. Listing them here would put another provider's identifiers, and
// with a shape this domain has been careful about, into a response that had no reason to carry them —
// and it would make the response depend on how many people happened to bid.
func (h *Handler) Award() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		customerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, err := jobIDFrom(r)
		if err != nil {
			return err
		}

		var req awardRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		bidID, err := req.bid()
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		var bid Bid
		err = db.InTx(r.Context(), pool, func(ctx context.Context, runner db.Runner) error {
			var err error
			bid, err = h.svc.AwardBid(ctx, runner, customerID, jobID, bidID)
			return err
		})
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, bidFrom(bid))
		return nil
	})
}

// History handles GET /v1/jobs/{id}/bids/{bid_id}/history (SHIP-88).
//
// SHIP-88's *Done when* is "only the latest valid offer is acceptable; **full chain remains
// readable**". The first half is a database constraint (000502's `ck_bids_superseded_is_not_live`);
// this is the second.
//
// # What it is and, as importantly, what it is not
//
// **One negotiation's offers, addressed through any offer in it.** Docs/02 §4 keeps bid history
// visible to the customer, the bidding provider and administrators, and this serves the first two —
// the administrator's view arrives with **SHIP-96**, which owns the visibility rules in full and is
// where an administrator's wider reach belongs.
//
// It is deliberately **not** `GET /v1/jobs/{id}/bids`. That path is the customer's comparison of
// *every* provider's offer side by side, which routes_bidding.go reserved for SHIP-102 by way of
// SHIP-96 at SHIP-84 — a different resource with a different privacy rule, since one provider may
// never see another's price. Serving it here would have taken that ticket's design.
//
// Read-only, so no `Idempotency-Key`: the middleware lets safe methods through untouched, and a key
// on a request that changes nothing would be a key stored for no reason.
//
// # Two callers here, three in the domain, and the one this file has to be most careful about
//
// SHIP-96 enumerates Docs/02 §4's three readers in `visibility.go` — the bidding provider, the
// customer, and an administrator. **Two of the three can reach this handler**, because the route is
// `RequireUser` and no administrator session exists until SHIP-147; the third is reachable from the
// domain and is exercised by test. That is recorded rather than papered over, and the one line a
// later administrator endpoint changes is the [Viewer] built below.
//
// The customer sees the provider's prices, which Docs/01 §4.3 explicitly wants — "allow a customer to
// compare price, timing, provider profile". The provider sees the customer's *counters*, which is new
// and is the shape worth stating: an amount the customer chose, in a response that reaches a
// provider. **It is not the budget.** Docs/01 §4.3 makes the customer's private maximum unreachable
// in any form, and nothing in this package reads a job at all; a counter-offer is a number the
// customer deliberately offered to this provider, and withholding it would leave a counter-offer
// endpoint whose counters nobody can read. Docs/11 §3 states the distinction and reports the one
// consequence a customer should be told about.
func (h *Handler) History() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		callerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, bidID, err := pathIDs(r)
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		// Administrator is false and cannot be anything else on this route: it declares
		// RequireUser, and `authctx.Subject` cannot carry an administrator at all — Docs/06 §5.2
		// and SHIP-147 make admin sign-in a separate system that a user token cannot reach.
		// **The line SHIP-152's administrator endpoint changes is this one**, and nothing in the
		// domain moves with it (SHIP-96).
		offers, _, truncated, err := h.svc.Chain(
			r.Context(), pool, Viewer{ID: callerID}, jobID, bidID)
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, chainFrom(offers, truncated))
		return nil
	})
}

// Mine handles GET /v1/fleet/bids (SHIP-101a).
//
// SHIP-101a's *Done when*: "a provider lists every bid they have placed, grouped by status,
// paginated, and sees no other provider's; the response carries no customer budget in any form."
//
// # Why it is under /v1/fleet and not under /v1/jobs
//
// **The resource is the caller's own bids across every job**, which is not a collection under any
// one job. routes_bidding.go reserved `/v1/jobs/{id}/bids` for SHIP-102's customer comparison at
// SHIP-84 and has said since then that a provider's list "is a different resource — the caller's own
// bids across every job — rather than a filter on this one".
//
// `/v1/fleet` is where a provider's own things already live: their vehicles, their declared service
// area, their profile. A provider asking "what have I bid on" is asking about their operation rather
// than about a job, and the URL says so. It also sidestepped `net/http`'s routing constraint
// entirely rather than taking another shelf under `/v1/jobs/` — `GET /v1/jobs/open/{id}` existed at
// the time, so a four-segment `GET /v1/jobs/{id}/<literal>` panicked the mux at registration.
// SHIP-83a has since lifted that constraint; the ownership argument above is why this path stays
// where it is regardless.
//
// # Every response key is one this API already promises a provider
//
// The element type is the same [bidResponse] the four write endpoints and the history answer with,
// which is what keeps the closed key set one list rather than two. Nothing of the job travels in it
// beyond the identifier, so Docs/01 §4.3 is structurally true here rather than remembered — there is
// no budget field to omit because there is no job in the shape.
//
// Read-only, so no `Idempotency-Key`: the middleware lets safe methods through untouched.
//
// **A customer calling it gets an empty page rather than a refusal**, and that is deliberate. There
// is no role check here for the reason `fleet`'s own list endpoints have none: the query is scoped to
// the caller's own id, so a customer's answer is empty by construction rather than by permission, and
// a 403 would make the client special-case a screen it never shows. Docs/11 §9 carries the wider
// question about `fleet`'s missing role checks; this endpoint discloses nothing either way.
func (h *Handler) Mine() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		query, err := bidQueryFrom(r)
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		page, err := h.svc.Bids(r.Context(), pool, providerID, query)
		if err != nil {
			return apiError(err)
		}

		bids := make([]bidResponse, 0, len(page.Bids))
		for _, bid := range page.Bids {
			bids = append(bids, bidFrom(bid))
		}

		httpx.WriteJSON(w, http.StatusOK, pagination.NewPage(bids, encodeBidCursor(page.Next)))
		return nil
	})
}

// bidQueryFrom reads `?status=`, `?limit=` and `?cursor=`, which is every parameter this list has.
//
// Every failure is bad_request rather than validation_failed, which is httpx's own division: a query
// parameter is part of how the request was addressed rather than data a person typed into a form, and
// `?cursor=` in particular is a token the client was handed rather than a value it composed.
//
// An unknown parameter is passed over rather than refused, unlike an unknown *body* field. Docs/07
// §6's strictness is about bodies: a URL picks up parameters from link trackers and proxies that no
// client typed. What matters is the other direction — neither parameter this endpoint reads can
// widen the scope, and the provider is not one of them.
func bidQueryFrom(r *http.Request) (BidQuery, error) {
	values := r.URL.Query()

	var query BidQuery

	if wanted := strings.TrimSpace(values.Get("status")); wanted != "" {
		status, known := StatusFromWire(wanted)
		if !known {
			// The valid values are not listed in the message. There are eight, the contract
			// publishes them, and a message that enumerated them would be a fourth copy of the
			// list to keep in step (Docs/10 §3.4).
			return BidQuery{}, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
				"%q is not a bid status.", wanted)
		}
		query.Status = status
	}

	limit, err := pagination.Limit(values.Get("limit"))
	if err != nil {
		return BidQuery{}, err
	}
	query.Limit = limit

	if query.After, err = decodeBidCursor(values.Get("cursor")); err != nil {
		return BidQuery{}, err
	}
	return query, nil
}

// bidCursorFields is how many parts a bid cursor has: the created_at it is positioned at, and the id
// that breaks ties on it.
const bidCursorFields = 2

// encodeBidCursor renders a position for a client to hand back, in the encoding
// `internal/pagination` owns. The zero cursor is the empty string, which is also what "no cursor"
// looks like on the way in.
func encodeBidCursor(c BidCursor) string {
	if c.IsZero() {
		return ""
	}
	return pagination.Cursor{
		c.CreatedAt.UTC().Format(time.RFC3339Nano),
		c.ID.String(),
	}.Encode()
}

// decodeBidCursor reads one back.
//
// pagination.Decode establishes the shape — this version, this many fields — and this establishes the
// meaning. Only the domain knows that its ordering key is a timestamp and a UUID, and a cursor whose
// fields decode but do not parse has to be refused here rather than reaching the query as a zero
// time, which would silently answer with the first page.
//
// **Nanoseconds, not the millisecond precision responses render.** A cursor is compared against
// created_at rather than displayed, and a rendering that rounded would put the boundary in the wrong
// place — repeating an offer at every page edge, or dropping one.
func decodeBidCursor(raw string) (BidCursor, error) {
	fields, err := pagination.Decode(raw, bidCursorFields)
	if err != nil || fields == nil {
		return BidCursor{}, err
	}

	at, err := time.Parse(time.RFC3339Nano, fields[0])
	if err != nil {
		return BidCursor{}, invalidBidCursor(err)
	}
	id, err := uuid.Parse(fields[1])
	if err != nil {
		return BidCursor{}, invalidBidCursor(err)
	}
	return BidCursor{CreatedAt: at, ID: id}, nil
}

func invalidBidCursor(cause error) error {
	return httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
		"The cursor is not one this endpoint issued. Ask for the first page without one.").
		WithCause(cause)
}

// pathIDs reads both identifiers a bid is addressed by.
//
// One function rather than two calls in each handler, so that a malformed job id and a malformed bid
// id produce one answer apiece and a client has two cases rather than four. The job is reported first
// because it is the first segment a reader of the URL meets.
func pathIDs(r *http.Request) (jobID, bidID uuid.UUID, err error) {
	if jobID, err = jobIDFrom(r); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if bidID, err = bidIDFrom(r); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return jobID, bidID, nil
}

// bidIDFrom reads and parses the {bid_id} path parameter.
//
// bad_request rather than not_found, exactly as [jobIDFrom] has it: a value that is not an identifier
// is not a bid that is missing, and the answer is the same whether or not any bid exists.
func bidIDFrom(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("bid_id"))
	if err != nil {
		return uuid.Nil, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"The bid id in the path is not a valid identifier.").WithCause(err)
	}
	return id, nil
}

// jobIDFrom reads and parses the {id} path parameter.
//
// A path parameter of the wrong shape is bad_request rather than not_found, which is the httpx
// registry's own description of that code. It also discloses nothing: the answer is the same whether
// or not any job exists.
func jobIDFrom(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return uuid.Nil, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"The job id in the path is not a valid identifier.").WithCause(err)
	}
	return id, nil
}

// callerID is the authenticated provider's account id.
//
// The route declares RequireUser, so a subject is guaranteed by the time a handler runs — which is
// why the absence of one is reported as an internal failure rather than as a 401. Reaching here
// without a subject means a route was declared public and written as though it were protected, and
// that is a wiring defect the caller can do nothing about.
//
// **The role on the subject is not read.** It is a claim in a token; whether this account may bid is
// decided by the eligibility filter, which reads `users.role` and four other facts from the
// database. The claim is evidence about the token and the column is the fact.
func callerID(ctx context.Context) (uuid.UUID, error) {
	subject := authctx.MustSubject(ctx)

	id, err := uuid.Parse(subject.UserID)
	if err != nil {
		return uuid.Nil, httpx.NewError(http.StatusInternalServerError, httpx.CodeInternal,
			"Something went wrong at our end.").WithCause(err)
	}
	return id, nil
}

// database returns the pool, or the answer to give while there is not one.
//
// 503 rather than 500, because the two say different things to a mobile client: retry, or surface a
// failure to the person holding the phone. Docs/10 §9.2 is explicit that the pool may be nil — the
// service starts with an unreachable database on purpose — so this is an expected condition rather
// than a defect, and it is logged at warning level for exactly that reason.
func (h *Handler) database(r *http.Request) (*pgxpool.Pool, error) {
	if h.pool != nil {
		return h.pool, nil
	}

	httpx.LoggerFrom(r.Context()).Warn("a bid arrived with no database connection")
	return nil, httpx.NewError(http.StatusServiceUnavailable, httpx.CodeUnavailable,
		"This cannot be completed right now. Try again shortly.")
}

// apiError turns this domain's errors into the API's error contract.
//
// The mapping lives at the transport edge on purpose: the service answers in domain terms, and which
// HTTP status an ineligible provider deserves is not a question the domain has an opinion about. A
// *httpx.Error passes straight through, because validation already produced one in the right shape.
//
// # Why an ineligible provider is 404 and not 403
//
// 403 would confirm the job exists, and which jobs a competitor may bid on is commercial information
// nobody published. It is also the answer a job that has been cancelled, awarded or expired
// deserves, and telling those apart would tell a provider what happened to work they were not given
// — including that somebody else won it. httpx's own description of `not_found` says the two cases
// are deliberately indistinguishable, and `GET /v1/fleet/jobs/{id}` already answers the same way for
// the same reasons (SHIP-83), so a provider gets one consistent answer whichever endpoint they
// reach.
//
// Anything unrecognised is returned as-is and becomes an opaque 500 in httpx.WriteError. That is the
// correct default: an error nobody has given a status and a code has not been considered.
func apiError(err error) error {
	var apiErr *httpx.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}

	switch {
	case errors.Is(err, ErrJobNotOffered):
		// The message says nothing about eligibility. A provider refused with "you are not eligible
		// for this job" has been told the job exists, which is what the indistinguishable answer is
		// for. The client already has the feed to show what this provider may bid on.
		return httpx.NewError(http.StatusNotFound, httpx.CodeNotFound,
			"No such job.").WithCause(err)

	case errors.Is(err, ErrNotJobCustomer):
		// The award's 404, byte-identical to the one above. A job that is not the caller's and a job
		// that does not exist are one answer: a provider who could tell them apart would learn that
		// somebody else's job exists, which is the disclosure Docs/01 §4.3 and this domain's 404s are
		// for. It is answered before any bid is read, so this endpoint cannot be used to probe for
		// bid identifiers either.
		return httpx.NewError(http.StatusNotFound, httpx.CodeNotFound,
			"No such job.").WithCause(err)

	case errors.Is(err, ErrBidNotFound), errors.Is(err, ErrNotBidOwner):
		// One answer for both, and the domain keeps them apart behind it. A provider told "that bid
		// is not yours" has been told the bid exists, which is a competitor's offer confirmed by
		// anybody willing to try identifiers — and the two rows in Docs/01 §4.3 are the budget and
		// exactly this. The domain distinguishes them so a test can tell "the stranger was refused"
		// from "the row vanished"; the wire does not.
		return httpx.NewError(http.StatusNotFound, httpx.CodeNotFound,
			"No such bid.").WithCause(err)

	case errors.Is(err, ErrBidAccepted):
		// 409 rather than 422: the request is well formed and contradicts the state the bid is in.
		// Its own code, because the app's next screen is the job this provider has just won.
		return httpx.NewError(http.StatusConflict, CodeBidAccepted,
			"That offer has been accepted. An accepted bid can be neither revised nor "+
				"withdrawn.").WithCause(err)

	case errors.Is(err, ErrBidClosed):
		return httpx.NewError(http.StatusConflict, CodeBidClosed,
			"That offer is no longer live, so it can be neither changed nor answered.").WithCause(err)

	case errors.Is(err, ErrNegotiationOver):
		// Deliberately CodeBidClosed rather than a code of its own. The client does the same thing
		// with it as with a superseded or rejected offer — this negotiation is finished — and the
		// message is what says which. **SHIP-93 has narrowed what reaches it**: a counter on a job
		// that was *awarded* now meets a `Rejected` offer and answers through the case above, so what
		// is left here is a job cancelled or expired without ever being awarded — where the offers
		// really are still live and only the job has moved.
		return httpx.NewError(http.StatusConflict, CodeBidClosed,
			"That job can no longer be awarded, so there is nothing a counter-offer could "+
				"lead to.").WithCause(err)

	case errors.Is(err, ErrWrongParty):
		// 409 rather than 404: the caller is a party to this negotiation and will see the offer in
		// its history, so hiding it here would be an inconsistency rather than a disclosure control.
		// Its own code, because the client's correct response is a *different request* — a `PATCH`
		// where it sent a counter, or a counter where it sent a `PATCH`.
		return httpx.NewError(http.StatusConflict, CodeWrongParty,
			"That offer belongs to the other party. Counter an offer they made; revise one you "+
				"made yourself.").WithCause(err)

	case errors.Is(err, ErrNotAProvidersOffer):
		// [CodeWrongParty] again, with the message the award needs (SHIP-92). One code for the third
		// instance of one rule — you counter the other party's offer, you revise your own, and you
		// award theirs — because the client's correct response is a different request in all three,
		// which is Docs/10 §4.4's test. A second code would have said the same thing to a client
		// that would branch to the same place.
		return httpx.NewError(http.StatusConflict, CodeWrongParty,
			"That is your own counter-offer, and awarding it would commit a provider to terms they "+
				"have not agreed to. Award an offer they made.").WithCause(err)

	case errors.Is(err, ErrJobNotAwardable):
		// `conflict` rather than a domain code, and this is the protocol code doing exactly what it
		// was registered for: its description has read "a second award on one job" since SHIP-12,
		// before this endpoint existed. The request is well formed and contradicts the state the job
		// is in, and the client's next screen is the job — not the offer, which is what separates
		// this from [CodeBidClosed].
		return httpx.NewError(http.StatusConflict, httpx.CodeConflict,
			"That job can no longer be awarded. Reload it to see its current status — it may "+
				"already have been awarded, cancelled, or have expired.").WithCause(err)

	case errors.Is(err, ErrNothingToRevise):
		return httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"The request changes nothing. Send at least one field to revise.").WithCause(err)

	case errors.Is(err, ErrNothingToCounter):
		// Kept apart from the revision's answer even though both are `bad_request`. A counter that
		// changes nothing is agreement rather than a client defect, and the client's next screen is
		// the award rather than the form it just submitted.
		return httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"A counter-offer has to change something. To accept these terms, award the job "+
				"rather than countering.").WithCause(err)

	case errors.Is(err, ErrAlreadyBid):
		// 409 rather than 422: the values are well formed and the request contradicts the state the
		// job is in. The client's correct response is to show the provider the offer they already
		// have rather than to mark a field as invalid, which is a different action and a different
		// screen.
		return httpx.NewError(http.StatusConflict, CodeAlreadyBid,
			"You already have a live offer on this job. Revise or withdraw it rather than "+
				"placing a second.").WithCause(err)

	case errors.Is(err, ErrNoIdempotencyKey):
		// Unreachable through the served router — SHIP-15's middleware refuses the request first —
		// and answered here anyway with the same code and the same status, so that the two paths
		// cannot tell a client two different things about one condition.
		return httpx.NewError(http.StatusBadRequest, httpx.CodeIdempotencyKeyRequired,
			"This request needs an %s header. Generate one value per action and reuse it for "+
				"every retry of that action.", httpx.HeaderIdempotencyKey).WithCause(err)

	default:
		return err
	}
}

// offerResponse is one offer as the customer who owns the job sees it (SHIP-102a).
//
// # It embeds [bidResponse] rather than restating it, and that is what keeps the closed set one list
//
// Go flattens an embedded struct into the enclosing JSON object, so every key the four write
// endpoints and the history already promise appears here under the same name, produced by the same
// [bidFrom]. The alternative — a second flat struct with eleven copied tags — is a second place for
// the offer's shape to be corrected, and the correction that reached only one of them would give a
// customer and a provider two different words for one field.
//
// **The closed key set is therefore [bidResponse]'s keys plus exactly two.** Both additions are
// objects rather than flattened fields, so `provider` and `vehicle` each have their own closed set
// declared in ports.go, and TestTheOfferResponseCarriesNothingItMayNot walks all three depths.
//
// # What is not here, stated as the four things the *Done when* forbids
//
// **No budget in any form** — no amount, no band, no flag, and no sentence. There is no field for one
// because [bidResponse] carries nothing of the job beyond its identifier and neither summary below
// carries a job at all. **No service area and no specialties**, which is the whole of `fleet.Profile`
// and therefore the whole of what "provider profile" can mean today. **No other jobs**, which no
// query in this domain could reach: [Service.Offers] selects one `job_id`.
//
// The fourth is not a field at all and is the one wave 9 found surviving in a form nothing caught: a
// *sentence* that says a budget exists — "the customer has set a maximum" — passes a closed key set
// and a source-parsing scan alike, because it is neither a field nor the word "budget". Nothing on
// this side of the wire can produce one, and the guard for it is on the screen: SHIP-102's
// customer_offers_test.dart asserts on words as well as on values.
type offerResponse struct {
	bidResponse

	// Provider is the closed customer-facing summary of who made this offer.
	//
	// Always present, and never a provider's declaration. `fleet.Profile` has exactly two fields —
	// the service area and the specialties — and SHIP-102a's *Done when* forbids both, so what a
	// customer may know about a provider today is what `users` can answer. Docs/11 §3 records the
	// gap and names SHIP-153…SHIP-159 as what closes it.
	Provider providerSummaryResponse `json:"provider"`

	// Vehicle is the vehicle the offer is made with, omitted when it names none.
	//
	// Omitted rather than null, which is the treatment [bidResponse.Message] and
	// [bidResponse.SupersededBy] both get: a client can tell "no vehicle stated" from a vehicle
	// whose fields happen to be empty without a second flag. Every offer placed before 000504 is in
	// this case, and so is every offer from a client that does not yet send the field.
	Vehicle *vehicleSummaryResponse `json:"vehicle,omitempty"`
}

// providerSummaryResponse is [ProviderSummary] on the wire.
type providerSummaryResponse struct {
	ID string `json:"id"`

	// DisplayName is who the provider trades as, and OperatesAs is `individual` or `business`
	// (SHIP-79a). Omitted when the provider has not declared them — an ordinary state the client
	// renders as "not stated", never as an error and never filled in from a contact detail.
	DisplayName string `json:"display_name,omitempty"`
	OperatesAs  string `json:"operates_as,omitempty"`

	// Verified is the platform's automated verification: email and phone confirmed, on an account in
	// good standing. The same predicate `fleet`'s eligibility filter reads, rather than a second one.
	Verified bool `json:"verified"`

	// MemberSince is when the provider's account was created, rendered day-first by the client
	// rather than here — every instant in this API is RFC 3339 in UTC (Docs/10 §4.1).
	MemberSince string `json:"member_since"`
}

// vehicleSummaryResponse is [VehicleSummary] on the wire.
//
// **The registration is absent and its absence is the point.** See [VehicleSummary].
type vehicleSummaryResponse struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Make  string `json:"make,omitempty"`
	Model string `json:"model,omitempty"`

	// Capacity is Docs/01 §4.3's "declared capability", nested rather than flattened so that the
	// four numbers read as one statement about the vehicle — which is `fleet.Capacity`'s own reason
	// for being a struct.
	Capacity capacityResponse `json:"capacity"`
}

// capacityResponse is what the vehicle can carry, as declared.
//
// Zeroes are sent rather than omitted. A provider who stated no maximum weight has a vehicle whose
// capacity is unstated, and a client showing "not stated" needs the key to be there to say so —
// `omitempty` would make an unstated capacity indistinguishable from a field this API had dropped.
type capacityResponse struct {
	MaxWeightKg float64 `json:"max_weight_kg"`
	LengthCm    int     `json:"length_cm"`
	WidthCm     int     `json:"width_cm"`
	HeightCm    int     `json:"height_cm"`
}

// offerFrom renders one described offer.
func offerFrom(o ReceivedOffer) offerResponse {
	response := offerResponse{
		bidResponse: bidFrom(o.Bid),
		Provider: providerSummaryResponse{
			ID:          o.Provider.ID.String(),
			DisplayName: o.Provider.DisplayName,
			OperatesAs:  o.Provider.OperatesAs,
			Verified:    o.Provider.Verified,
			MemberSince: timestamp(o.Provider.MemberSince),
		},
	}
	if o.HasVehicle {
		response.Vehicle = &vehicleSummaryResponse{
			ID:    o.Vehicle.ID.String(),
			Type:  o.Vehicle.Type,
			Make:  o.Vehicle.Make,
			Model: o.Vehicle.Model,
			Capacity: capacityResponse{
				MaxWeightKg: o.Vehicle.MaxWeightKg,
				LengthCm:    o.Vehicle.LengthCm,
				WidthCm:     o.Vehicle.WidthCm,
				HeightCm:    o.Vehicle.HeightCm,
			},
		}
	}
	return response
}

// Received handles GET /v1/jobs/{id}/bids/received (SHIP-102a).
//
// SHIP-102a's *Done when*: "the owning customer lists every live offer on one of their jobs in the
// Docs 10 §4.5 collection envelope with cursor pagination, each element carrying the offer's price
// and timing, a closed customer-facing provider summary and the vehicle it is offered with; a
// provider gets what a stranger gets; the response carries no budget in any form, and no provider's
// service area, specialties or other jobs."
//
// # The path says `/bids/received` and the *Done when* says `/bids`, and that is a finding rather
// than a slip
//
// `GET /v1/jobs/{id}/bids` **could not be registered when this endpoint was written**.
// `GET /v1/jobs/open/{id}` (SHIP-83) put a literal where the `{id}` wildcard goes, so it and any
// four-segment `GET /v1/jobs/{id}/<literal>` both matched `/v1/jobs/open/bids` with neither more
// specific, and Go's `ServeMux` panics at registration — the process does not start. Renaming the
// literal did not help; `/offers` collided identically. `POST /v1/jobs/{id}/bids` was unaffected
// only because the other route was a `GET`.
//
// SHIP-115 met this first and resolved it by taking a shelf under the job, which is why
// `GET /jobs/{id}/delivery/detail`, `/delivery/milestones` and `/delivery/proof` are shaped the way
// they are; that note ended by recording the collision "for whoever owns `/jobs/open/{id}`".
// **SHIP-83a is that owner and moved the feed to `/v1/fleet/jobs/{id}`, so the four-segment space is
// now free** — cmd/api/routes_jobsegment_test.go demonstrates it by registering one.
//
// **This path did not move with it, and that is a decision rather than an omission.** It is a
// published endpoint the Flutter client already calls, and `Docs/06` §5.3 settles it: old builds
// persist on devices indefinitely and Dart has no over-the-air update path, so a URL that has
// shipped cannot be withdrawn on the strength of it now being avoidable. `received` also says which
// side of the negotiation is asking, which `/bids` alone did not.
//
// # Read-only, so no `Idempotency-Key`
//
// The middleware lets safe methods through untouched, exactly as it does for
// `GET /v1/jobs/{id}/bids/{bid_id}/history` and `GET /v1/fleet/bids`.
//
// # A provider calling it gets a 404 and so does a stranger
//
// A clause of the *Done when* rather than a consequence, and the ownership check in
// [Service.Offers] is the whole of it — there is no branch anywhere that asks whether the caller is
// a provider, which is what makes "not the customer", "no such job" and "somebody else's job" one
// answer by construction rather than by three code paths agreeing. This differs from
// `GET /v1/fleet/bids`, where a customer gets an *empty page* rather than a refusal: that endpoint
// is scoped by the caller's own id and discloses nothing either way, and this one takes a job
// identifier from the client and would otherwise disclose that it exists.
func (h *Handler) Received() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		customerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, err := jobIDFrom(r)
		if err != nil {
			return err
		}

		query, err := bidQueryFrom(r)
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		page, err := h.svc.Offers(r.Context(), pool, customerID, jobID, OfferQuery{
			Status: query.Status,
			Limit:  query.Limit,
			After:  query.After,
		})
		if err != nil {
			return apiError(err)
		}

		offers := make([]offerResponse, 0, len(page.Offers))
		for _, offer := range page.Offers {
			offers = append(offers, offerFrom(offer))
		}

		httpx.WriteJSON(w, http.StatusOK, pagination.NewPage(offers, encodeBidCursor(page.Next)))
		return nil
	})
}

// --- job-scoped messaging (SHIP-97) ---------------------------------------------------------------

// messageRequest is the body of POST /v1/jobs/{id}/bids/{bid_id}/messages.
//
// **One field, and that is the whole request.** There is no `job_id`, no `provider_id`, no `sent_by`
// and no `created_at`: the conversation comes from the path, the party is whichever side the platform
// works out the caller is on, and the instant is the platform's. A `sent_by` in a body would be an
// authorisation decision made from client input, which Docs/07 §3 puts on the platform — and it is
// the one field somebody would reach for, because a client rendering "you" against "them" has the
// value in hand and could send it back.
//
// httpx.DecodeJSON refuses unknown fields, so a client sending any of them is told it does not exist
// rather than having it silently ignored.
type messageRequest struct {
	Body string `json:"body"`
}

// messageResponse is one message on the wire (SHIP-97).
//
// # A closed key set, and this is the response where that matters most in the domain
//
// Every other shape here carries numbers and instants. This one carries **free text**, and wave 10
// and wave 11 both established that a disclosure with no field, no value and no digit defeats every
// structural guard there is. So the set is closed *and* the platform contributes no prose to it:
// every string a client receives is a key name, an identifier, an instant, a party name, or the body
// a party typed. `TestNothingTheCustomerTypedIsAddedToByThePlatform` holds that word by word over the
// rendered bytes rather than over the struct.
//
// **The idempotency key is not here.** It is on the row (000506) and it is the *sender's* client's
// token — rendering it would hand one party a value the other party's device generated, which is
// nothing they need and is exactly the sort of field that ends up in a log. [bidResponse] leaves
// `Bid.Key` off for the same reason.
//
// There is no `job_id` either, unlike [bidResponse]. A message is only ever reached through its own
// job's path, and the collection is the conversation rather than a mixed list — so the identifier
// would be the same value on every element of every page.
type messageResponse struct {
	ID string `json:"id"`

	// SentBy is which party wrote it — `provider` or `customer` — and it is the field the screen is
	// built out of.
	//
	// [bidResponse.OfferedBy]'s argument applies unchanged and more strongly: a client cannot infer
	// it. A conversation does not alternate, both parties may write twice in a row, and a reader may
	// join it halfway. Rendering "you" against "them" is the whole of what a message list does.
	SentBy string `json:"sent_by"`

	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
}

func messageFrom(m Message) messageResponse {
	return messageResponse{
		ID:        m.ID.String(),
		SentBy:    m.SentBy.Wire(),
		Body:      m.Body,
		CreatedAt: timestamp(m.CreatedAt),
	}
}

// SendMessage handles POST /v1/jobs/{id}/bids/{bid_id}/messages (SHIP-97).
//
// Protected: the sender is whoever the token says is calling, and the side they are on is worked out
// from the database — `bids.provider_id` for the provider, `jobs.customer_id` for the customer. That
// is `RequireUser` doing what routes_bidding.go says it is for: a role claim in a token is evidence
// about the token rather than the fact.
//
// **201 when the message was sent, 200 when this key had already sent it.** The same shape either
// way, which is the arrangement `POST /v1/jobs/{id}/bids` and `POST /v1/jobs/{id}/milestones` already
// use — a client that does not care parses one type, and one recovering from a dropped connection
// generally does not.
//
// State-changing, so it carries an `Idempotency-Key` like every other mutating route (SHIP-15), and
// the key is read here as well as by the middleware. See [Service.SendMessage]: the middleware
// replays a response while its entry lives and the column replays the row forever, and a duplicated
// message is one the other party has already read.
//
// A caller who is neither party answers 404, byte-identically to a bid that does not exist.
func (h *Handler) SendMessage() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		caller, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, bidID, err := pathIDs(r)
		if err != nil {
			return err
		}

		var req messageRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		note := Note{Body: req.Body, Key: r.Header.Get(httpx.HeaderIdempotencyKey)}

		message, created, err := h.svc.SendMessage(r.Context(), pool, caller, jobID, bidID, note)
		if err != nil {
			return apiError(err)
		}

		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		httpx.WriteJSON(w, status, messageFrom(message))
		return nil
	})
}

// Messages handles GET /v1/jobs/{id}/bids/{bid_id}/messages (SHIP-97).
//
// The collection envelope of Docs/10 §4.5 with cursor pagination, **oldest first** — a conversation
// is read forward, which is the opposite of `GET /v1/fleet/bids` and the same as the history.
//
// Read-only, so the idempotency middleware lets it through untouched and it carries no key.
//
// # The administrative reader is one field, and the field cannot be set from here
//
// `Viewer{ID: caller}` with `Administrator` left false, exactly as [Handler.History] builds it and
// for exactly the same reason: this route declares `RequireUser`, and `authctx.Subject` cannot carry
// an administrator at all — Docs/06 §5.2 and SHIP-147 make admin sign-in a separate system that a
// user token cannot reach. **The line an administrative endpoint changes is this one**, and nothing
// in the domain moves with it. SHIP-97's "and admins" clause is therefore met in `internal/bidding`
// and unreachable from the wire; Docs/11 §3 records it as a declared reduced clause rather than a
// clause quietly met.
func (h *Handler) Messages() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		caller, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, bidID, err := pathIDs(r)
		if err != nil {
			return err
		}

		query, err := messageQueryFrom(r)
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		page, _, err := h.svc.Messages(r.Context(), pool, Viewer{ID: caller}, jobID, bidID, query)
		if err != nil {
			return apiError(err)
		}

		messages := make([]messageResponse, 0, len(page.Messages))
		for _, message := range page.Messages {
			messages = append(messages, messageFrom(message))
		}

		httpx.WriteJSON(w, http.StatusOK,
			pagination.NewPage(messages, encodeMessageCursor(page.Next)))
		return nil
	})
}

// messageQueryFrom reads `?limit=` and `?cursor=`, which is every parameter this list has.
//
// No `?status=` and no `?sent_by=`, unlike [bidQueryFrom]: see [MessageQuery]. Every failure is
// `bad_request` rather than `validation_failed`, which is httpx's own division and the reading
// [bidQueryFrom] states — a query parameter is part of how the request was addressed rather than data
// a person typed into a form.
//
// An unknown parameter is passed over rather than refused, for [bidQueryFrom]'s reason. What matters
// is the other direction: neither parameter here can widen the scope, and the conversation is not one
// of them.
func messageQueryFrom(r *http.Request) (MessageQuery, error) {
	values := r.URL.Query()

	var query MessageQuery

	limit, err := pagination.Limit(values.Get("limit"))
	if err != nil {
		return MessageQuery{}, err
	}
	query.Limit = limit

	if query.After, err = decodeMessageCursor(values.Get("cursor")); err != nil {
		return MessageQuery{}, err
	}
	return query, nil
}

// encodeMessageCursor renders a position for a client to hand back, in the encoding
// `internal/pagination` owns.
//
// The same two fields [encodeBidCursor] writes and deliberately not the same function: the two
// cursors are positions in differently ordered lists, and one encoder shared between them would be
// an invitation to hand a bid cursor to a conversation. They decode to different types, so the
// compiler refuses that.
func encodeMessageCursor(c MessageCursor) string {
	if c.IsZero() {
		return ""
	}
	return pagination.Cursor{
		c.CreatedAt.UTC().Format(time.RFC3339Nano),
		c.ID.String(),
	}.Encode()
}

// decodeMessageCursor reads one back.
//
// Nanoseconds rather than the millisecond precision responses render, for [decodeBidCursor]'s reason
// and with more riding on it here: two parties answering each other inside one millisecond is the
// ordinary case, so a rounded boundary would repeat or drop a message rather than an offer.
func decodeMessageCursor(raw string) (MessageCursor, error) {
	fields, err := pagination.Decode(raw, bidCursorFields)
	if err != nil || fields == nil {
		return MessageCursor{}, err
	}

	at, err := time.Parse(time.RFC3339Nano, fields[0])
	if err != nil {
		return MessageCursor{}, invalidBidCursor(err)
	}
	id, err := uuid.Parse(fields[1])
	if err != nil {
		return MessageCursor{}, invalidBidCursor(err)
	}
	return MessageCursor{CreatedAt: at, ID: id}, nil
}
