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

	AmountCents int64 `json:"amount_cents"`

	PickupAt  string `json:"pickup_at"`
	DeliverBy string `json:"deliver_by"`

	// Message is omitted when none was given, so a client can tell "no conditions" from "an empty
	// note" without a second flag.
	Message string `json:"message,omitempty"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func bidFrom(b Bid) bidResponse {
	return bidResponse{
		ID:     b.ID.String(),
		JobID:  b.JobID.String(),
		Status: b.Status.Wire(),

		AmountCents: b.AmountCents,

		PickupAt:  timestamp(b.PickupAt),
		DeliverBy: timestamp(b.DeliverBy),
		Message:   b.Message,

		CreatedAt: timestamp(b.CreatedAt),
		UpdatedAt: timestamp(b.UpdatedAt),
	}
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
