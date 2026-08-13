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

	// Message is the conditions accompanying the offer, and the only optional field.
	Message string `json:"message"`
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
// # Two callers, and the one this file has to be most careful about
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

		offers, truncated, err := h.svc.Chain(r.Context(), pool, callerID, jobID, bidID)
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, chainFrom(offers, truncated))
		return nil
	})
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
// are deliberately indistinguishable, and `GET /v1/jobs/open/{id}` already answers the same way for
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
		// message is what says which. Once SHIP-93 closes competing bids on award, this branch is
		// reached less and less: the offer itself will be Rejected and answer through the case above.
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
