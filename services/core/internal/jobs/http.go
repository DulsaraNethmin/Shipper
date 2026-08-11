// The HTTP surface of the jobs domain.
//
// Handlers live in the domain rather than in cmd/api (Docs/10 §2.1). The reason is a merge hazard
// rather than taste: it keeps cmd/api/routes.go free of per-domain edits, which is what lets two
// domains be built at once without touching a shared file. A domain importing internal/httpx is
// sitting on infrastructure, not crossing a boundary, and the import lint permits it.
//
// # There is no budget field in any type below, and that is not because it has not been added yet
//
// Docs/01 §4.3 keeps the customer's maximum private from providers — not as an amount, not as a
// band, not as a "budget supplied" flag. SHIP-67 adds the column and the serialisation test that
// proves it cannot leak. Every endpoint here is owner-only, so a budget field would be safe on
// them specifically; it is absent anyway, because the field arrives with the ticket that owns the
// proof rather than with the first response that could carry it.
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

	VehicleRequirement *string `json:"vehicle_requirement"`
	HandlingNotes      *string `json:"handling_notes"`

	PickupWindow  *windowBody `json:"pickup_window"`
	DropoffWindow *windowBody `json:"dropoff_window"`
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

// fields turns the request into the domain's command, reporting anything it could not parse.
//
// Only parsing failures are reported here. Whether a value is *acceptable* is the domain's
// judgement and is made in DraftFields.validate, so that the same rules apply to every caller
// rather than to whoever came in through HTTP.
func (b draftRequest) fields() (DraftFields, error) {
	var e validate.Errors

	f := DraftFields{
		GoodsDescription:   b.GoodsDescription,
		LengthCm:           b.LengthCm,
		WidthCm:            b.WidthCm,
		HeightCm:           b.HeightCm,
		WeightKg:           b.WeightKg,
		VehicleRequirement: b.VehicleRequirement,
		HandlingNotes:      b.HandlingNotes,
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
// (SHIP-82's feed, SHIP-100's detail), it is a different type in this package rather than this one
// with fields hidden, because Docs/01 §4.3's budget rule is much easier to keep with two types
// than with one and a redaction step somebody has to remember.
type jobResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`

	Pickup  *locationResponse `json:"pickup,omitempty"`
	Dropoff *locationResponse `json:"dropoff,omitempty"`

	GoodsDescription string `json:"goods_description,omitempty"`

	LengthCm int     `json:"length_cm,omitempty"`
	WidthCm  int     `json:"width_cm,omitempty"`
	HeightCm int     `json:"height_cm,omitempty"`
	WeightKg float64 `json:"weight_kg,omitempty"`

	VehicleRequirement string `json:"vehicle_requirement,omitempty"`
	HandlingNotes      string `json:"handling_notes,omitempty"`

	PickupWindow  *windowResponse `json:"pickup_window,omitempty"`
	DropoffWindow *windowResponse `json:"dropoff_window,omitempty"`

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

		LengthCm: j.Dimensions.LengthCm,
		WidthCm:  j.Dimensions.WidthCm,
		HeightCm: j.Dimensions.HeightCm,
		WeightKg: j.WeightKg,

		VehicleRequirement: j.VehicleRequirement,
		HandlingNotes:      j.HandlingNotes,

		PickupWindow:  windowFrom(j.PickupWindow),
		DropoffWindow: windowFrom(j.DropoffWindow),

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

	case errors.Is(err, ErrNothingToUpdate):
		return httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"The request changes nothing. Send at least one field to update.").WithCause(err)

	default:
		return err
	}
}
