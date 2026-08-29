// The HTTP surface of the jobs domain.
//
// Handlers live in the domain rather than in cmd/api (Docs/10 §2.1). The reason is a merge hazard
// rather than taste: it keeps cmd/api/routes.go free of per-domain edits, which is what lets two
// domains be built at once without touching a shared file. A domain importing internal/httpx is
// sitting on infrastructure, not crossing a boundary, and the import lint permits it.
//
// # The budget is on exactly one type below, and a test is what keeps it there
//
// Docs/01 §4.3 keeps the customer's maximum private from providers — not as an amount, not as a
// band, not as a "budget supplied" flag. Every endpoint in this file is owner-only, so
// [jobResponse] may carry it and does (SHIP-67).
//
// What makes that safe as the file grows is not this paragraph. SHIP-82's provider feed and
// SHIP-83's provider job detail get response types of their own rather than this one with fields
// hidden, and TestOnlyTheOwnersResponseCarriesTheBudget parses this package's source and fails
// when any struct but [jobResponse] declares a `budget` json tag. A provider shape written later
// with the field copied across does not compile past the test suite.
//
// # Status appears in responses and in no request
//
// Job status is never a settable field (Docs/02 §2, CLAUDE.md). The request types do not carry it
// and httpx.DecodeJSON refuses unknown fields, so a client that sends `"status": "Open"` is told
// the field does not exist rather than having it silently ignored — which is the failure worth
// preventing, because a client that believes it published a job and did not will retry forever.
//
// The blank line below keeps this a file note rather than a second package comment.

package jobs

import (
	"context"
	"errors"
	"fmt"
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
// It is built in cmd/api/routes_jobs.go from Deps. The pool is held here rather than inside the
// service because Docs/10 §3.2 puts the transaction boundary with whoever owns the invariant, and
// for an edit that is this layer: the read, the ownership check and the write are one decision.
type Handler struct {
	svc  *Service
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewHandler wires the handlers to the service.
//
// The pool may be nil and that is not an error. The service starts with an unreachable database
// on purpose — a rolling deployment during a failover would otherwise take every instance down at
// once — so a nil pool is a condition the handlers answer 503 to for as long as it lasts, not a
// reason to refuse to start.
func NewHandler(svc *Service, pool *pgxpool.Pool, log *slog.Logger) (*Handler, error) {
	if svc == nil {
		return nil, errors.New("jobs: a handler needs a service")
	}
	if log == nil {
		return nil, errors.New("jobs: a handler needs a logger")
	}
	return &Handler{svc: svc, pool: pool, log: log}, nil
}

// addressBody is one address on the wire.
//
// Replaced as a whole rather than merged field by field, on both verbs. A client sending
// `{"pickup": {"suburb": "Newtown"}}` is saying the pickup address is now that and nothing else,
// which is refused as an incomplete address rather than quietly merged with the three parts of
// the previous one — merging would let an edit produce an address made of two different places,
// and no error would be reported for it.
type addressBody struct {
	Line     string `json:"line"`
	Suburb   string `json:"suburb"`
	State    string `json:"state"`
	Postcode string `json:"postcode"`
}

// windowBody is a date window on the wire, as two RFC 3339 timestamps.
//
// Strings rather than time.Time, and parsed by hand below. encoding/json reports a bad timestamp
// as an ordinary error with no type of its own, so httpx.DecodeJSON can only answer "the request
// body is not valid JSON" — which is untrue and unactionable when the body is perfectly good JSON
// containing `"next tuesday"`. Parsing here produces a field error naming the field
// (Docs/10 §4.6).
type windowBody struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// draftRequest is the body of both POST /v1/jobs and PATCH /v1/jobs/{id}.
//
// One type for both, because a field a customer can set at creation is a field they can change
// afterwards, and two types would be two places for that list to drift. Every field is a pointer,
// so absent, null and an explicit empty value are three distinct things:
//
//	omitted or null   leave it alone — on POST that means empty, on PATCH unchanged
//	an empty value    clear it
//
// The second is not decoration: without it a customer could add a handling note and never remove
// it, because "" would be indistinguishable from having said nothing.
//
// There is no `status` field. See the note at the top of this file.
type draftRequest struct {
	Pickup  *addressBody `json:"pickup"`
	Dropoff *addressBody `json:"dropoff"`

	GoodsDescription *string  `json:"goods_description"`
	LengthCm         *int     `json:"length_cm"`
	WidthCm          *int     `json:"width_cm"`
	HeightCm         *int     `json:"height_cm"`
	WeightKg         *float64 `json:"weight_kg"`

	// GoodsCategory is a code from GET /v1/goods-categories (SHIP-58).
	//
	// The code, never the label. A client that sent "General freight" would be sending
	// wording that changes; the code is the half that does not. An unknown code is
	// `validation_failed` on `goods_category`, and the client's move is to re-fetch the
	// catalogue — see [Service.checkDraftCategory].
	GoodsCategory *string `json:"goods_category"`

	VehicleRequirement *string `json:"vehicle_requirement"`
	HandlingNotes      *string `json:"handling_notes"`

	PickupWindow  *windowBody `json:"pickup_window"`
	DropoffWindow *windowBody `json:"dropoff_window"`

	// BudgetCents is the customer's maximum, in cents (SHIP-67).
	//
	// Minor units as a whole number rather than dollars as a decimal, for the reason
	// Docs/10 §3.3 gives for the Go and PostgreSQL sides: money is never a float, and a JSON
	// number written as 45.50 is one. The name says the unit, because a field called
	// `budget` holding 150000 is a field somebody eventually reads as dollars.
	//
	// It is accepted here and returned only to the owner. No provider-facing shape carries
	// it in any form (Docs/01 §4.3) — see the note at the top of this file.
	BudgetCents *int64 `json:"budget_cents"`
}

// cancelRequest is the body of POST /v1/jobs/{id}/cancel.
//
// A plain string rather than a pointer, unlike every field of [draftRequest]. The three-way
// distinction those need — absent, null, explicitly empty — has no meaning here: there is no
// stored reason to leave alone or to clear, only one being supplied now or not at all.
//
// A body is still required, even though every field in it is optional. httpx.DecodeJSON refuses
// an empty body, and making this endpoint the one exception would be a second answer to what a
// request looks like for the sake of saving a client two characters.
type cancelRequest struct {
	Reason string `json:"reason"`
}

// publishRequest is the body of POST /v1/jobs/{id}/publish (SHIP-63).
//
// One field, and it is not optional. Docs/04 §2 requires the terms and goods declaration "for
// every job", so a publication that does not carry it is refused rather than defaulted — which is
// the whole reason this is a field the client must send rather than a header or an assumption.
//
// A plain bool rather than a pointer: absent and false mean the same thing here, because both mean
// the customer did not declare. There is no stored value to leave alone, which is the distinction
// [draftRequest]'s pointers exist for.
//
// **`false` is refused rather than ignored.** A client that sends it has told the platform the
// customer declined, and answering 200 to that would record an acceptance nobody made.
type publishRequest struct {
	// AcceptsTerms is the customer's declaration, made now, about these goods.
	//
	// Named for what it is rather than `confirmed` or `agreed`: the field name is what a client
	// developer reads when they decide what to bind it to, and a vague one invites binding it to
	// a constant.
	AcceptsTerms bool `json:"accepts_terms"`
}

// extendRequest is the body of POST /v1/jobs/{id}/extend, and it has no fields at all (SHIP-70).
//
// **The client does not say how long.** Docs/02 §6.3 asks for an extension "in one action", and a
// period in the body would be a client choosing how long the platform's own listing rule applies to
// it — the same shape as a client naming a status, and refused for the same reason. The platform
// decides, and [ExtensionPeriod] is where.
//
// A body is still required, on exactly the reasoning [cancelRequest] gives: httpx.DecodeJSON
// refuses an empty body, and a second endpoint excepted from that would be a second answer to what
// a request looks like. `{}` is the whole request. Because the decoder refuses unknown fields, a
// client that sends `{"days": 30}` is told the field does not exist rather than having it ignored —
// which matters more here than usual, since a client that believes it bought thirty days and got
// fourteen has no way to tell from a successful response until the job disappears.
type extendRequest struct{}

// fields turns the request into the domain's command, reporting anything it could not parse.
//
// Only parsing failures are reported here. Whether a value is *acceptable* is the domain's
// judgement and is made in DraftFields.validate, so that the same rules apply to every caller
// rather than to whoever came in through HTTP.
func (b draftRequest) fields() (DraftFields, error) {
	var e validate.Errors

	f := DraftFields{
		GoodsDescription:   b.GoodsDescription,
		GoodsCategory:      b.GoodsCategory,
		LengthCm:           b.LengthCm,
		WidthCm:            b.WidthCm,
		HeightCm:           b.HeightCm,
		WeightKg:           b.WeightKg,
		VehicleRequirement: b.VehicleRequirement,
		HandlingNotes:      b.HandlingNotes,
		BudgetCents:        b.BudgetCents,
	}

	if b.Pickup != nil {
		f.Pickup = &Address{
			Line: b.Pickup.Line, Suburb: b.Pickup.Suburb,
			State: State(b.Pickup.State), Postcode: b.Pickup.Postcode,
		}
	}
	if b.Dropoff != nil {
		f.Dropoff = &Address{
			Line: b.Dropoff.Line, Suburb: b.Dropoff.Suburb,
			State: State(b.Dropoff.State), Postcode: b.Dropoff.Postcode,
		}
	}

	f.PickupWindow = b.PickupWindow.parse("pickup_window", &e)
	f.DropoffWindow = b.DropoffWindow.parse("dropoff_window", &e)

	if !e.Any() {
		return f, nil
	}

	// Something could not be parsed, so the service will never see these fields and will never
	// have the chance to complain about them. Its complaints are collected here instead and
	// answered together — a customer who mistypes a date and a postcode should be told about
	// both at once rather than fixing one and being sent back for the other.
	problems := f.normalise().problems()
	for _, problem := range problems.Fields() {
		e.Add(problem.Field, problem.Code, "%s", problem.Message)
	}
	return DraftFields{}, e.Err()
}

// parse reads the two ends of a window, adding a field error for anything it cannot.
func (w *windowBody) parse(field string, e *validate.Errors) *TimeWindow {
	if w == nil {
		return nil
	}

	var out TimeWindow
	out.Start = instant(field+".start", w.Start, e)
	out.End = instant(field+".end", w.End, e)
	return &out
}

// instant parses one RFC 3339 timestamp, treating empty as absent.
func instant(field, value string, e *validate.Errors) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}

	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		e.Add(field, validate.CodeInvalid, "Enter a date and time in RFC 3339 form, such as 2026-08-14T09:00:00+10:00.")
		return time.Time{}
	}
	return t.UTC()
}

// locationResponse is one address and what the platform resolved it to.
//
// The coordinate is a nested object that is absent rather than zeroed when the address was not
// resolved. A client cannot then mistake (0, 0) for a place, and does not have to carry a
// `resolved` flag alongside two numbers it must remember to ignore.
type locationResponse struct {
	Line     string `json:"line"`
	Suburb   string `json:"suburb"`
	State    string `json:"state"`
	Postcode string `json:"postcode"`

	Coordinate *coordinateResponse `json:"coordinate,omitempty"`
}

type coordinateResponse struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`

	// Formatted is the geocoder's own rendering of the place it matched, which is not always
	// what the customer typed. Shown where a client wants to confirm the platform understood
	// the address, and omitted when the provider did not supply one.
	Formatted string `json:"formatted,omitempty"`
}

type windowResponse struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

// jobResponse is a job as its owner sees it.
//
// Everything optional is omitted rather than sent empty, because a draft is mostly empty for most
// of its life and a response full of `""` and `0` tells a client nothing about which fields the
// customer has actually filled in. Responses stay additive (Docs/07 §6): fields are added here,
// never repurposed or removed.
//
// **This is the customer's view and there is no provider view of a job yet.** When one arrives
// (SHIP-82's feed, SHIP-83's detail), it is a different type in this package rather than this one
// with fields hidden, because Docs/01 §4.3's budget rule is much easier to keep with two types
// than with one and a redaction step somebody has to remember. This is the type that carries
// `budget_cents`, and TestOnlyTheOwnersResponseCarriesTheBudget is what stops it being the second.
type jobResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`

	Pickup  *locationResponse `json:"pickup,omitempty"`
	Dropoff *locationResponse `json:"dropoff,omitempty"`

	GoodsDescription string `json:"goods_description,omitempty"`

	// GoodsCategory is the catalogue code, not the label (SHIP-58).
	//
	// The code alone, and the client resolves it against GET /v1/goods-categories. Returning
	// the label here would put a second copy of the wording on every job response, which is
	// the copy that is stale the day somebody corrects a typo in the catalogue.
	GoodsCategory string `json:"goods_category,omitempty"`

	LengthCm int     `json:"length_cm,omitempty"`
	WidthCm  int     `json:"width_cm,omitempty"`
	HeightCm int     `json:"height_cm,omitempty"`
	WeightKg float64 `json:"weight_kg,omitempty"`

	VehicleRequirement string `json:"vehicle_requirement,omitempty"`
	HandlingNotes      string `json:"handling_notes,omitempty"`

	PickupWindow  *windowResponse `json:"pickup_window,omitempty"`
	DropoffWindow *windowResponse `json:"dropoff_window,omitempty"`

	// BudgetCents is the customer's own maximum, returned to the customer (SHIP-67).
	//
	// Omitted when it was not supplied, like everything else optional here, so a client can
	// tell "no budget" from "a budget of nothing" without a second flag.
	BudgetCents int64 `json:"budget_cents,omitempty"`

	// TermsAcceptedAt is when the customer made the declaration that published this job
	// (SHIP-63).
	//
	// Omitted while the job is a Draft, because a draft has never been published. Returned to
	// the owner because it is a record *of their own act* and Docs/04 §2 makes it the thing the
	// platform relies on if the goods are not what the job said — a customer is entitled to see
	// what they are on record as having declared, and when.
	TermsAcceptedAt string `json:"terms_accepted_at,omitempty"`

	// ExpiresAt is when this job stops being offered, once it is Open (SHIP-68).
	//
	// Omitted while the job is a Draft, because a Draft has no deadline — the clock starts at
	// publication (Docs/02 §6.3). The client needs it to show "expires in three days" and to
	// offer the extension SHIP-70 builds, and it is the same instant SHIP-69 warns against.
	ExpiresAt string `json:"expires_at,omitempty"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func jobFrom(j Job) jobResponse {
	return jobResponse{
		ID:     j.ID.String(),
		Status: j.Status.Wire(),

		Pickup:  locationFrom(j.Pickup),
		Dropoff: locationFrom(j.Dropoff),

		GoodsDescription: j.GoodsDescription,
		GoodsCategory:    j.GoodsCategory,

		LengthCm: j.Dimensions.LengthCm,
		WidthCm:  j.Dimensions.WidthCm,
		HeightCm: j.Dimensions.HeightCm,
		WeightKg: j.WeightKg,

		VehicleRequirement: j.VehicleRequirement,
		HandlingNotes:      j.HandlingNotes,

		PickupWindow:  windowFrom(j.PickupWindow),
		DropoffWindow: windowFrom(j.DropoffWindow),

		BudgetCents:     j.BudgetCents,
		TermsAcceptedAt: timestamp(j.TermsAcceptedAt),
		ExpiresAt:       timestamp(j.ExpiresAt),

		CreatedAt: timestamp(j.CreatedAt),
		UpdatedAt: timestamp(j.UpdatedAt),
	}
}

func locationFrom(l Location) *locationResponse {
	if l.Address.IsZero() {
		return nil
	}

	out := &locationResponse{
		Line: l.Line, Suburb: l.Suburb,
		// The abbreviation, upper case, which is the canonical form of an Australian state
		// and what a client renders verbatim. Docs/10 §4.7 puts enum values on the wire in
		// lower snake case, and this is a deliberate exception rather than an oversight:
		// a client does not branch on the state, it prints it, and `nsw` would have every
		// client upper-casing it back. Input is accepted in any case, including the
		// spelled-out name.
		State:    string(l.State),
		Postcode: l.Postcode,
	}

	if lat, lng, resolved := l.Coordinate(); resolved {
		out.Coordinate = &coordinateResponse{Latitude: lat, Longitude: lng, Formatted: l.Formatted}
	}
	return out
}

func windowFrom(w TimeWindow) *windowResponse {
	if w.IsZero() {
		return nil
	}
	return &windowResponse{Start: timestamp(w.Start), End: timestamp(w.End)}
}

// timestamp renders an instant the way every other endpoint does, in UTC with milliseconds.
// The zero time renders as empty, so an absent window end is omitted rather than sent as
// year one.
func timestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// categoriesResponse is the body of GET /v1/goods-categories (SHIP-58).
//
// An object with one key rather than a bare JSON array, and that is Docs/07 §6 rather than taste:
// responses stay additive — fields are added, never repurposed or removed — and a top-level array
// is the one shape that cannot gain a field. The first thing this will want is the catalogue's
// own revision or a `provisional` summary, and neither is expressible without breaking every
// client at once.
type categoriesResponse struct {
	Categories []categoryResponse `json:"categories"`
}

// categoryResponse is one served category.
//
// Every field of [Category] appears, including the refused ones' — see [Category.Carried] for why
// a category the platform will not take is served at all.
type categoryResponse struct {
	Code        string `json:"code"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`

	// Carried says whether a job in this category may be published.
	//
	// Always present, never omitted — `omitempty` would drop it from exactly the entries where
	// it is false, which is to say from every entry whose answer matters. A client reading a
	// missing field as "unknown, assume yes" would offer the customer dangerous goods.
	Carried bool `json:"carried"`

	// Provisional says the entry has not been through X-4's legal review (Docs/11 §4).
	//
	// Served rather than kept internal because Docs/11 §5's reduced-form acceptance turns on
	// it: the list may ship before a legal adviser has seen it precisely because it says so,
	// and a flag nobody can read is not a disclosure. Never omitted, for [categoryResponse]'s
	// reason above and one more — the interesting value here is `true`, and the day it is
	// false for everything is the day X-4 closed.
	Provisional bool `json:"provisional"`
}

// Categories handles GET /v1/goods-categories (SHIP-58).
//
// # Public, and reading configuration on every request
//
// The same two properties GET /v1/app/policy has, for the same two reasons. Nothing in the answer
// is about the caller — it is one list, identical for everybody — and the app needs it to render
// the job form, which it may do before anyone has signed in. And it is read per request rather
// than captured at startup so that changing the catalogue is changing the environment and
// restarting, which is what CLAUDE.md means by a category list living server-side.
//
// # It carries no cache header, deliberately
//
// The obvious optimisation is a long max-age on a list that changes twice a year. It is left out
// because the one time the list changes in a hurry is the time it matters — a category withdrawn
// on legal advice — and a cache is exactly what would keep serving it. The client caches this for
// offline use anyway (Docs/07 §4), which is the copy that should be stale, because it is the copy
// that knows it might be.
func (h *Handler) Categories() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		catalogue, err := h.svc.Categories()
		if err != nil {
			return apiError(err)
		}

		out := categoriesResponse{Categories: make([]categoryResponse, 0, len(catalogue))}
		for _, c := range catalogue {
			out.Categories = append(out.Categories, categoryResponse{
				Code:        c.Code,
				Label:       c.Label,
				Description: c.Description,
				Carried:     c.Carried,
				Provisional: c.Provisional,
			})
		}

		httpx.WriteJSON(w, http.StatusOK, out)
		return nil
	})
}

// Publish handles POST /v1/jobs/{id}/publish (SHIP-63).
//
// A verb under the resource, for the reason [Handler.Cancel] is one: job status is not a settable
// field, so there is no `PATCH` that could carry `"status": "open"` and no request schema anywhere
// in this domain that has a status in it. A client that sends one is told the field does not exist.
//
// State-changing, so it carries an `Idempotency-Key` like every other mutating route (SHIP-15),
// and the middleware absorbs a retry that reuses its key. A retry with a *fresh* key is absorbed
// too, by [Service.Publish] itself — unlike an extension, publishing twice is not two of anything.
//
// A job belonging to somebody else answers 404, byte-identically to a job that does not exist. See
// [apiError].
func (h *Handler) Publish() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		customerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, err := jobIDFrom(r)
		if err != nil {
			return err
		}

		var req publishRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		var published Job
		err = db.InTx(r.Context(), pool, func(ctx context.Context, runner db.Runner) error {
			var err error
			published, err = h.svc.Publish(ctx, runner, customerID, jobID, req.AcceptsTerms)
			return err
		})
		if err != nil {
			return h.publishError(r.Context(), err, jobID)
		}

		httpx.WriteJSON(w, http.StatusOK, jobFrom(published))
		return nil
	})
}

// publishError maps [Service.Publish]'s refusals, naming the category when that is what happened.
//
// A method rather than a function, and the only error mapping in this file that is: answering
// SHIP-59's "explains why" needs the catalogue, and the catalogue is on the service. Everything it
// does not handle falls through to [apiError], which is where the rest of this domain's mapping
// lives and stays.
//
// # Why the message is built here rather than registered with the code
//
// The registered description is what the generated document says the code means, and it has to be
// true of every use. The *message* is about this request — these goods, this catalogue's wording
// for them — and the wording moves when operations change it. Building it here is what lets a
// category withdrawn on legal advice explain itself in the customer's own refusal on the same
// afternoon, with no release.
func (h *Handler) publishError(ctx context.Context, err error, jobID uuid.UUID) error {
	switch {
	case errors.Is(err, ErrProhibitedCategory):
		return httpx.NewError(http.StatusUnprocessableEntity, CodeProhibitedCategory,
			"%s", h.prohibitedMessage(ctx, jobID)).WithCause(err)

	case errors.Is(err, ErrUnknownCategory):
		// Reachable here only for a job whose stored category has since left the
		// catalogue — a draft saved before operations withdrew it. The client's move is
		// the same as for any stale reference: re-fetch the list and choose again.
		return httpx.NewError(http.StatusUnprocessableEntity, httpx.CodeValidationFailed,
			"This job's goods category is no longer one this platform serves. "+
				"Choose another from the catalogue.").WithCause(err)

	case errors.Is(err, ErrJobNotPublishable):
		return httpx.NewError(http.StatusConflict, CodeNotPublishable,
			"This job cannot be published. Reload it to see its current status.").WithCause(err)

	case errors.Is(err, ErrCustomerNotVerified):
		return httpx.NewError(http.StatusForbidden, CodeCustomerNotVerified,
			"Verify your email address and phone number before publishing a job. "+
				"You can keep editing the draft in the meantime.").WithCause(err)

	case errors.Is(err, ErrTermsNotAccepted):
		return httpx.NewError(http.StatusUnprocessableEntity, CodeTermsNotAccepted,
			"Accept the terms and the goods declaration to publish this job.").WithCause(err)

	default:
		return apiError(err)
	}
}

// prohibitedMessage is the sentence a refused customer reads.
//
// It quotes the catalogue's own label and description, so the explanation is the platform's
// current policy wording rather than a copy of it compiled into this file. If the category cannot
// be resolved — which would mean the catalogue changed between the refusal and this call — it
// falls back to a sentence that is still true and still actionable.
func (h *Handler) prohibitedMessage(ctx context.Context, jobID uuid.UUID) string {
	category, refused := h.categoryOf(ctx, jobID)
	if !refused {
		return "Shipper does not carry goods in this category. " +
			"See GET /v1/goods-categories for what can be published."
	}

	message := fmt.Sprintf("Shipper does not carry %s.", strings.ToLower(category.Label))
	if category.Description != "" {
		message += " " + category.Description
	}
	return message + " See GET /v1/goods-categories for what can be published."
}

// categoryOf reads the job's stored category and asks whether it is a refused one.
//
// A second read, on a path that is already refusing the request, and outside the transaction that
// has just rolled back. That is deliberate: the alternative is threading the category out through
// the error, which would make one refusal in this domain a different shape from every other. See
// [Service.prohibitedCategory].
//
// It takes the request's context, so a client that has gone away stops this query too — the answer
// is only ever used to word a refusal nobody is waiting for any more. Every failure reports "no
// category to explain" and the caller falls back to a sentence that is still true: a refusal whose
// wording could not be looked up is still a refusal, and turning it into a 500 would replace a
// correct answer with an incorrect one.
func (h *Handler) categoryOf(ctx context.Context, jobID uuid.UUID) (Category, bool) {
	if h.pool == nil {
		return Category{}, false
	}

	var code string
	const q = `SELECT COALESCE(goods_category, '') FROM jobs WHERE id = $1`
	if err := h.pool.QueryRow(ctx, q, jobID).Scan(&code); err != nil {
		return Category{}, false
	}
	return h.svc.prohibitedCategory(code)
}

// Create handles POST /v1/jobs (SHIP-61).
//
// Protected: the owner of the job is whoever the token says is calling, and there is no field in
// the request for naming somebody else. That is not merely convenient — a customer id in the body
// would be an authorisation decision made from client input, which Docs/07 §3 puts on the
// platform.
//
// The job is created as a Draft and there is no way to ask for anything else. Every field is
// optional, so an empty body creates an empty draft, which is what the first step of the app's
// job wizard needs (SHIP-71, SHIP-75).
func (h *Handler) Create() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		customerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		var req draftRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		fields, err := req.fields()
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		job, err := h.svc.CreateDraft(r.Context(), pool, customerID, fields)
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusCreated, jobFrom(job))
		return nil
	})
}

// Update handles PATCH /v1/jobs/{id} (SHIP-62).
//
// PATCH rather than PUT, because the app edits one step of the job wizard at a time and a PUT
// would require it to send every field it is not changing — which is how a client that has not
// been updated for a new field silently clears it.
//
// A job belonging to somebody else answers 404, not 403. See [apiError].
func (h *Handler) Update() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		customerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, err := jobIDFrom(r)
		if err != nil {
			return err
		}

		var req draftRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		fields, err := req.fields()
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		var updated Job
		err = db.InTx(r.Context(), pool, func(ctx context.Context, runner db.Runner) error {
			var err error
			updated, err = h.svc.UpdateDraft(ctx, runner, customerID, jobID, fields)
			return err
		})
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, jobFrom(updated))
		return nil
	})
}

// List handles GET /v1/jobs (SHIP-66).
//
// The customer's own jobs, newest first, filterable by status and paginated by cursor
// (Docs/10 §4.5). **There is no parameter for whose jobs to list** — the owner is whoever the token
// says is calling, so there is no filter here to forget and no way to widen the query by asking.
//
// Each entry is the same [jobResponse] the detail endpoint returns, whole rather than summarised.
// A list of summaries would be a second shape to keep in step with the first, and the saving is
// not real: the fields a summary would drop are the ones a draft usually has none of, and they are
// already omitted when empty.
//
// No idempotency key: a GET changes nothing.
func (h *Handler) List() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		customerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		query, err := jobQueryFrom(r)
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		page, err := h.svc.Jobs(r.Context(), pool, customerID, query)
		if err != nil {
			return apiError(err)
		}

		jobs := make([]jobResponse, 0, len(page.Jobs))
		for _, job := range page.Jobs {
			jobs = append(jobs, jobFrom(job))
		}

		httpx.WriteJSON(w, http.StatusOK, pagination.NewPage(jobs, encodeJobCursor(page.Next)))
		return nil
	})
}

// jobQueryFrom reads `?status=`, `?limit=` and `?cursor=`.
//
// Every failure it returns is already in the error contract. They are bad_request rather than
// validation_failed throughout, which is httpx's own division: a query parameter is part of how the
// request was addressed rather than data a person typed into a form, and `?cursor=` in particular
// is a token the client was handed rather than a value it composed.
func jobQueryFrom(r *http.Request) (JobQuery, error) {
	values := r.URL.Query()

	var query JobQuery

	if wanted := strings.TrimSpace(values.Get("status")); wanted != "" {
		status, known := StatusFromWire(wanted)
		if !known {
			// The valid values are not listed in the message. There are twelve, the
			// contract publishes them, and a message that enumerates them is a
			// thirteenth copy of the list to keep in step (Docs/10 §3.4).
			return JobQuery{}, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
				"%q is not a job status.", wanted)
		}
		query.Status = status
	}

	limit, err := pagination.Limit(values.Get("limit"))
	if err != nil {
		return JobQuery{}, err
	}
	query.Limit = limit

	if query.After, err = decodeJobCursor(values.Get("cursor")); err != nil {
		return JobQuery{}, err
	}
	return query, nil
}

// jobCursorFields is how many parts a job cursor has: the created_at it is positioned at, and the
// id that breaks ties on it.
const jobCursorFields = 2

// encodeJobCursor renders a position for a client to hand back. The zero cursor is the empty
// string, which is also what "no cursor" looks like on the way in.
func encodeJobCursor(c JobCursor) string {
	if c.IsZero() {
		return ""
	}
	return pagination.Cursor{
		c.CreatedAt.UTC().Format(time.RFC3339Nano),
		c.ID.String(),
	}.Encode()
}

// decodeJobCursor reads one back.
//
// pagination.Decode establishes the shape — this version, this many fields — and this establishes
// the meaning. The split matters: only the domain knows that its ordering key is a timestamp and a
// UUID, and a cursor whose fields decode but do not parse must be refused here rather than reaching
// the query as a zero time, which would silently answer with the first page.
//
// **Nanoseconds, not the millisecond precision responses use.** A cursor is compared against
// created_at rather than displayed, and a rendering that rounded would put the boundary in the
// wrong place — repeating a job at every page edge, or dropping one.
func decodeJobCursor(raw string) (JobCursor, error) {
	fields, err := pagination.Decode(raw, jobCursorFields)
	if err != nil || fields == nil {
		return JobCursor{}, err
	}

	at, err := time.Parse(time.RFC3339Nano, fields[0])
	if err != nil {
		return JobCursor{}, invalidJobCursor(err)
	}
	id, err := uuid.Parse(fields[1])
	if err != nil {
		return JobCursor{}, invalidJobCursor(err)
	}
	return JobCursor{CreatedAt: at, ID: id}, nil
}

func invalidJobCursor(cause error) error {
	return httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
		"The cursor is not one this endpoint issued. Ask for the first page without one.").
		WithCause(cause)
}

// Detail handles GET /v1/jobs/{id} (SHIP-65).
//
// The same [jobResponse] the two writing endpoints answer with, so a client parses one type
// whatever it did to get the job. That is worth stating because the alternative is tempting and
// wrong: a "detail" shape with a field or two more would make the create response a subset a
// client has to special-case, and a subset is where a field quietly goes missing.
//
// **Owner-only, and a stranger gets the same 404 a missing job gets.** A draft is visible to
// nobody but its owner, and a published job is visible to providers only through a shape that does
// not exist yet (SHIP-82, SHIP-83); until it does, "not yours" and "no such job" are one answer
// here as they are on every other route in this domain.
//
// No idempotency key: a GET changes nothing and the middleware lets read-only methods through
// untouched.
func (h *Handler) Detail() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		customerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, err := jobIDFrom(r)
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		// The pool rather than a transaction. One statement is atomic on its own, and a
		// transaction around a single SELECT buys nothing but a round trip either side.
		job, err := h.svc.Job(r.Context(), pool, customerID, jobID)
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, jobFrom(job))
		return nil
	})
}

// Cancel handles POST /v1/jobs/{id}/cancel (SHIP-64).
//
// A verb under the resource rather than a `PATCH` setting a field, because job status is not a
// settable field (Docs/02 §2, CLAUDE.md). `{"status": "cancelled"}` would be a client naming a
// state; this is a client naming an intent, and the platform decides what the state becomes —
// which is the whole distinction the guard exists to enforce. It is also why an already-cancelled
// job answers 200 rather than 409: the caller asked for an outcome that holds.
//
// State-changing, so it carries an Idempotency-Key like every other mutating route (SHIP-15). The
// middleware absorbs a retry that reuses its key; [Service.Cancel] absorbs the one that does not.
//
// A job belonging to somebody else answers 404, byte-identically to a job that does not exist.
// See [apiError].
func (h *Handler) Cancel() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		customerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, err := jobIDFrom(r)
		if err != nil {
			return err
		}

		var req cancelRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		var cancelled Job
		err = db.InTx(r.Context(), pool, func(ctx context.Context, runner db.Runner) error {
			var err error
			cancelled, err = h.svc.Cancel(ctx, runner, customerID, jobID, req.Reason)
			return err
		})
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, jobFrom(cancelled))
		return nil
	})
}

// Extend handles POST /v1/jobs/{id}/extend (SHIP-70).
//
// Docs/02 §6.3's "can extend in one action", and it is one call with an empty body: the platform
// decides the new deadline, so there is nothing for a client to send and nothing for it to get
// wrong. See [extendRequest] and [Service.Extend].
//
// A verb under the resource rather than a `PATCH` writing `expires_at`, for a reason that is worth
// separating from the one behind [Handler.Cancel]. Cancel is a verb because status is not settable;
// the deadline *is* an ordinary column, so a PATCH would work — and would be wrong anyway, because
// the value is the platform's to compute. A client that could write the field could keep a listing
// alive for a decade.
//
// State-changing, so it carries an Idempotency-Key like every other mutating route (SHIP-15). The
// middleware absorbs a retry that reuses its key. A retry with a *fresh* key is not absorbed here,
// and that is deliberate rather than an oversight: unlike a cancellation, an extension is not
// idempotent by nature — asking twice is asking for two extensions, and the second is a request the
// customer is entitled to make. What bounds it is the pickup window, not the endpoint.
//
// A job belonging to somebody else answers 404, byte-identically to a job that does not exist.
// See [apiError].
func (h *Handler) Extend() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		customerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, err := jobIDFrom(r)
		if err != nil {
			return err
		}

		var req extendRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		var extended Job
		err = db.InTx(r.Context(), pool, func(ctx context.Context, runner db.Runner) error {
			var err error
			extended, err = h.svc.Extend(ctx, runner, customerID, jobID)
			return err
		})
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, jobFrom(extended))
		return nil
	})
}

// statusChangeResponse is one recorded transition — one row of `job_status_history` (SHIP-65a).
//
// # The actor is served as its kind and never as an identifier
//
// `actor_id` is in the table and is not on this shape. That is the same call
// `delivery.milestoneResponse.RecordedBy` already makes — "the kind of actor rather than who they
// are" — and it is the right one here for four separate readings of one column:
//
//   - For `customer` there is exactly one, and both readers already know who: it is the reader, or
//     it is the counterparty on their own bid. The identifier adds nothing and hands a provider an
//     account id they had no other way to obtain.
//   - For `admin` it names a member of staff to a commercial party. `Docs/01` §3 requires an
//     administrator's action to be *auditable*, and `audit_log` is where it is auditable; naming
//     them to the customer whose job they unpublished is a different thing entirely.
//   - For `driver` it is a `driver_assignments` row rather than an account, because a driver has no
//     account (000401). It would read as a user id and be one only by coincidence of type.
//   - For `system` it is absent, and a field that is present for four kinds and absent for the
//     fifth is one a client has to branch on to render a timeline that never needed it.
//
// **What a timeline renders is the kind**: "Cancelled by you", "Expired by Shipper", "Picked up by
// the driver". SHIP-77 is the screen and none of its rows wants a UUID.
//
// # Two clocks, named for what they mean rather than for their columns
//
// `recorded_at` is the actor's — when the person or process says they acted — and is what a
// customer is shown. `accepted_at` is the platform's, and is what support reasons about. `Docs/02`
// §3.1 requires them carried separately because a driver records a milestone with no signal and the
// request arrives much later, and `delivery.milestoneResponse` names the same pair the same way so
// that a client reading both timelines learns one convention.
//
// # `reason` is free text and may be absent
//
// Optional for everyone except an administrator, for whom `Docs/01` §3 makes it the condition of
// changing a commercial record at all (`ck_job_status_history_admin_reason`). `omitempty`, because
// an absent field says nothing while an empty string would say somebody supplied no words when they
// were asked for some.
//
// **This is the field the budget-privacy guard exists for.** See history.go's file note: a provider
// reads it, and a sentence is the one form of disclosure no key list, word search or value search
// can see.
type statusChangeResponse struct {
	ID    string `json:"id"`
	JobID string `json:"job_id"`

	// From and To are the wire form of Docs/02 §1's statuses, lower snake case per Docs/10 §4.7 —
	// the same spelling `status` takes on [jobResponse], through the same [Status.Wire].
	From string `json:"from_status"`
	To   string `json:"to_status"`

	// ActorType is one of the five in [ActorTypes]. See the type note for why there is no id
	// beside it.
	ActorType string `json:"actor_type"`

	Reason string `json:"reason,omitempty"`

	RecordedAt string `json:"recorded_at"`
	AcceptedAt string `json:"accepted_at"`
}

func statusChangeFrom(c StatusChange) statusChangeResponse {
	return statusChangeResponse{
		ID:    c.ID.String(),
		JobID: c.JobID.String(),

		From: c.From.Wire(),
		To:   c.To.Wire(),

		ActorType: c.Actor.Type.String(),

		Reason: c.Reason,

		RecordedAt: timestamp(c.ActorRecordedAt),
		AcceptedAt: timestamp(c.ServerRecordedAt),
	}
}

// History handles GET /v1/jobs/{id}/history (SHIP-65a).
//
// # The first four-segment `GET /v1/jobs/{id}/<literal>` in the service
//
// Until SHIP-83a this path could not be registered at all. `GET /v1/jobs/open/{id}` (SHIP-83) put a
// literal where the identifier goes, so it and any four-segment `GET /v1/jobs/{id}/<literal>` both
// matched `/v1/jobs/open/history` with neither more specific — and Go's `ServeMux` **panics at
// registration**, so the process did not start rather than an endpoint answering oddly. Four
// tickets took workarounds for it (SHIP-115, SHIP-115a, SHIP-101a, SHIP-102a) and this row declared
// the blocker instead. SHIP-83a moved the feed to `GET /v1/fleet/jobs/{id}`; this is the first
// endpoint to spend what that bought.
//
// # The collection envelope, with `has_more` always false
//
// `Docs/10` §4.5's shape, so a client tells a collection from a single resource without knowing the
// endpoint — and it does not page, which is a fact about the table rather than an omission.
// `Docs/02` §2's transition graph is acyclic and twelve statuses wide, [Service.Transition] refuses
// a move to the status a job already stands in ([ErrAlreadyInStatus]), and 000402's trigger refuses
// a status write with no history row describing it. So the collection is bounded by the lifecycle
// at somewhere under a dozen rows, the way `/v1/jobs/{id}/delivery/proof` is bounded by there being
// at most one photograph per milestone. `delivery`'s milestone list is the contrast and the reason
// this paragraph is worth writing: 000601 deliberately has *no* uniqueness on `(job_id, milestone)`,
// so a repeat is a legitimate outcome there and that collection has no bound to stand on.
//
// A `cursor` parameter would therefore be one no caller could usefully pass, and `next_cursor` is
// omitted rather than sent empty ([pagination.NewPage]).
//
// No idempotency key: a GET changes nothing.
func (h *Handler) History() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		readerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		jobID, err := jobIDFrom(r)
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		changes, err := h.svc.HistoryFor(r.Context(), pool, readerID, jobID)
		if err != nil {
			return apiError(err)
		}

		out := make([]statusChangeResponse, 0, len(changes))
		for _, change := range changes {
			out = append(out, statusChangeFrom(change))
		}

		httpx.WriteJSON(w, http.StatusOK, pagination.NewPage(out, ""))
		return nil
	})
}

// jobIDFrom reads and parses the {id} path parameter.
//
// A path parameter of the wrong shape is bad_request rather than not_found, which is the httpx
// registry's own description of that code. It also discloses nothing: the answer is the same
// whether or not any job exists.
//
// One function rather than one copy per handler, because the alternative is three answers to what
// a malformed id produces and a client that has to handle all three.
func jobIDFrom(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return uuid.Nil, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"The job id in the path is not a valid identifier.").WithCause(err)
	}
	return id, nil
}

// callerID is the authenticated customer's account id.
//
// Both routes declare RequireUser, so a subject is guaranteed by the time a handler runs — which
// is why the absence of one is reported as an internal failure rather than as a 401. Reaching
// here without a subject means a route was declared public and written as though it were
// protected, and that is a wiring defect the caller can do nothing about.
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
// 503 rather than 500, because the two say different things to a mobile client: retry, or surface
// a failure to the person holding the phone. Docs/10 §9.2 is explicit that the pool may be nil —
// the service starts with an unreachable database on purpose — so this is an expected condition
// rather than a defect, and it is logged at warning level for exactly that reason.
func (h *Handler) database(r *http.Request) (*pgxpool.Pool, error) {
	if h.pool != nil {
		return h.pool, nil
	}

	httpx.LoggerFrom(r.Context()).Warn("a job request arrived with no database connection")
	return nil, httpx.NewError(http.StatusServiceUnavailable, httpx.CodeUnavailable,
		"This cannot be completed right now. Try again shortly.")
}

// apiError turns this domain's errors into the API's error contract.
//
// The mapping lives at the transport edge on purpose: the service answers in domain terms, and
// which HTTP status a non-owner deserves is not a question the domain has an opinion about. A
// *httpx.Error passes straight through, because validation already produced one in the right
// shape.
//
// # Why a job belonging to somebody else is 404 and not 403
//
// 403 would confirm that the job exists. For a Draft that is a real disclosure — a draft is
// visible to nobody but its owner, so being told "that is not yours" tells a stranger that a job
// with that id has been created, which is information they had no way to obtain. httpx's own
// description of `not_found` says the two cases are deliberately indistinguishable, and this is
// one of the places that matters.
//
// The domain still distinguishes them ([ErrNotJobOwner] against [ErrJobNotFound]) so that a test
// can tell "the non-owner was refused" from "the job silently stopped existing" — the same 404 to
// a client and very different defects.
//
// Anything unrecognised is returned as-is and becomes an opaque 500 in httpx.WriteError. That is
// the correct default: an error nobody has given a status and a code has not been considered.
func apiError(err error) error {
	var apiErr *httpx.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}

	switch {
	case errors.Is(err, ErrJobNotFound), errors.Is(err, ErrNotJobOwner):
		return httpx.NewError(http.StatusNotFound, httpx.CodeNotFound,
			"No such job.").WithCause(err)

	case errors.Is(err, ErrNotCustomer):
		return httpx.NewError(http.StatusForbidden, CodeCustomerOnly,
			"Only a customer account can create or edit a job.").WithCause(err)

	case errors.Is(err, ErrJobNotDraft):
		return httpx.NewError(http.StatusConflict, CodeNotADraft,
			"This job has been published and can no longer be edited as a draft.").WithCause(err)

	case errors.Is(err, ErrJobNotCancellable):
		return httpx.NewError(http.StatusConflict, CodeNotCancellable,
			"This job can no longer be cancelled. Reload it to see its current status.").WithCause(err)

	// Two sentinels, one code, two messages. The client's action is the same for both — reload
	// and offer what is actually available — so a second code would be one more branch for no
	// decision; the sentence differs because the customer's next move does.
	case errors.Is(err, ErrJobNotExtendable):
		return httpx.NewError(http.StatusConflict, CodeNotExtendable,
			"This job is not being offered to providers, so there is no expiry to extend. "+
				"Reload it to see its current status.").WithCause(err)

	case errors.Is(err, ErrExpiryBoundByPickup):
		return httpx.NewError(http.StatusConflict, CodeNotExtendable,
			"This job is ending because its pickup date is passing, not because the listing "+
				"has aged. More listing time would not keep it open.").WithCause(err)

	case errors.Is(err, ErrNothingToUpdate):
		return httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"The request changes nothing. Send at least one field to update.").WithCause(err)

	default:
		return err
	}
}
