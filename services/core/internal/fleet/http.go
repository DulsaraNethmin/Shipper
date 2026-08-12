// The HTTP surface of the fleet domain.
//
// Handlers live in the domain rather than in cmd/api (Docs/10 §2.1). The reason is a merge hazard
// rather than taste: it keeps cmd/api/routes.go free of per-domain edits, which is what lets two
// domains be built at once without touching a shared file. A domain importing internal/httpx is
// sitting on infrastructure, not crossing a boundary, and the import lint permits it.
//
// # Every route here is the provider's own fleet, and there is no parameter for whose
//
// The owner is whoever the token says is calling. That is not merely convenient — a provider id in
// a request would be an authorisation decision made from client input, which Docs/07 §3 puts on the
// platform. A vehicle belonging to another provider answers 404, byte-identically to one that does
// not exist.
//
// # One route here is not under /fleet, and that is deliberate
//
// `GET /v1/jobs/open` (SHIP-82) serves jobs, and it is served from this domain because this domain
// decides which jobs a provider may bid on. Routes are declared rather than registered, so the file
// a route is declared in follows the domain that answers it rather than the first segment of its
// path. The rule that keeps it apart from `jobs`' own route is the one the budget invariant needs:
// `/v1/jobs/{id}` is the customer's job and carries their budget, and nothing served from here has
// a field it could go in.
//
// **This is not the customer's view of a vehicle.** Docs/01 §4.3 lets a customer compare "provider
// profile, vehicle, and declared capability" when they read the bids on their job, and that shape
// arrives with SHIP-96 as a schema of its own rather than as this one reached by another route —
// for the same reason `jobs` keeps the customer's and the provider's views apart: one shape with a
// redaction step somebody has to remember is the arrangement a privacy rule is hardest to keep
// with.
//
// # Active is reported and is never accepted
//
// A vehicle leaves and re-enters service through its own endpoints, not through a field on a PATCH.
// The request types below carry no `active` and no `deactivated_at`, and httpx.DecodeJSON refuses
// unknown fields — so a client that sends `"active": false` is told the field does not exist rather
// than having it silently ignored, which is the failure worth preventing: a provider who believes
// they took a truck off the road and did not will keep receiving work for it.
//
// # Two resources, and one of them is a set
//
// `/v1/fleet/vehicles` is a collection of things with identifiers. `/v1/fleet/profile` (SHIP-79) is
// one document per provider holding three sets — states, postcodes, specialties — and it is edited
// by sending a set back rather than by adding and removing entries one at a time. There is
// therefore no `/v1/fleet/profile/states/{state}`: two operations over one collection is where a
// client and a server stop agreeing about what is in it.
//
// The blank line below keeps this a file note rather than a second package comment.

package fleet

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/pagination"
)

// Handler serves this domain's routes.
//
// It is built in cmd/api/routes_fleet.go from Deps. The pool is held here rather than inside the
// service because Docs/10 §3.2 puts the transaction boundary with whoever owns the invariant, and
// for an edit that is this layer: the read, the ownership check and the write are one decision.
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
		return nil, errors.New("fleet: a handler needs a service")
	}
	if log == nil {
		return nil, errors.New("fleet: a handler needs a logger")
	}
	return &Handler{svc: svc, pool: pool, log: log}, nil
}

// vehicleRequest is the body of both POST /v1/fleet/vehicles and PATCH /v1/fleet/vehicles/{id}.
//
// One type for both, because a field a provider can set when adding a vehicle is a field they can
// change afterwards, and two types would be two places for that list to drift. Every field is a
// pointer, so absent, null and an explicit empty value are three distinct things:
//
//	omitted or null   leave it alone — on POST that means unset, on PATCH unchanged
//	an empty value    clear it
//
// The second is not decoration: without it a provider could state a load height and never correct
// it back to "unstated", because 0 would be indistinguishable from having said nothing. Registration
// and vehicle type are the exception — required on POST, and refused as empty on either verb, since
// a row with a blank plate identifies nothing.
//
// There is no `active` field. See the note at the top of this file.
type vehicleRequest struct {
	Registration *string `json:"registration"`
	VehicleType  *string `json:"vehicle_type"`
	Make         *string `json:"make"`
	Model        *string `json:"model"`

	MaxWeightKg  *float64 `json:"max_weight_kg"`
	LoadLengthCm *int     `json:"load_length_cm"`
	LoadWidthCm  *int     `json:"load_width_cm"`
	LoadHeightCm *int     `json:"load_height_cm"`
}

// fields turns the request into the domain's command.
//
// The vehicle type is converted rather than checked here: whether a value is *acceptable* is the
// domain's judgement and is made in [VehicleFields.problems], so that the same rules apply to every
// caller rather than to whoever came in through HTTP. That is the same division `jobs` makes for the
// Australian state, and it is why this function cannot fail.
func (b vehicleRequest) fields() VehicleFields {
	f := VehicleFields{
		Registration: b.Registration,
		Make:         b.Make,
		Model:        b.Model,
		MaxWeightKg:  b.MaxWeightKg,
		LengthCm:     b.LoadLengthCm,
		WidthCm:      b.LoadWidthCm,
		HeightCm:     b.LoadHeightCm,
	}
	if b.VehicleType != nil {
		t := VehicleType(*b.VehicleType)
		f.Type = &t
	}
	return f
}

// vehicleResponse is a vehicle as its owning provider sees it.
//
// Everything optional is omitted rather than sent empty, because a vehicle added in a truck yard
// carries a plate and a type and often nothing else, and a response full of `""` and `0` tells a
// client nothing about which fields the provider has actually filled in. Responses stay additive
// (Docs/07 §6): fields are added here, never repurposed or removed.
//
// `active` is the exception and is always present. It is the field every fleet screen branches on,
// and a boolean that is absent when false is a boolean every client has to remember to default.
type vehicleResponse struct {
	ID string `json:"id"`

	Registration string `json:"registration"`
	VehicleType  string `json:"vehicle_type"`
	Make         string `json:"make,omitempty"`
	Model        string `json:"model,omitempty"`

	MaxWeightKg  float64 `json:"max_weight_kg,omitempty"`
	LoadLengthCm int     `json:"load_length_cm,omitempty"`
	LoadWidthCm  int     `json:"load_width_cm,omitempty"`
	LoadHeightCm int     `json:"load_height_cm,omitempty"`

	Active bool `json:"active"`

	// DeactivatedAt is absent while the vehicle is in service, which is what makes it readable
	// beside `active` rather than a second copy of the same fact: a client showing "off the road
	// since 3 August" has the date, and one that only cares whether the vehicle can be offered
	// reads the boolean.
	DeactivatedAt string `json:"deactivated_at,omitempty"`

	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func vehicleFrom(v Vehicle) vehicleResponse {
	return vehicleResponse{
		ID: v.ID.String(),

		Registration: v.Registration,
		VehicleType:  string(v.Type),
		Make:         v.Make,
		Model:        v.Model,

		MaxWeightKg:  v.Capacity.MaxWeightKg,
		LoadLengthCm: v.Capacity.LengthCm,
		LoadWidthCm:  v.Capacity.WidthCm,
		LoadHeightCm: v.Capacity.HeightCm,

		Active:        v.Active(),
		DeactivatedAt: timestamp(v.DeactivatedAt),

		CreatedAt: timestamp(v.CreatedAt),
		UpdatedAt: timestamp(v.UpdatedAt),
	}
}

// timestamp renders an instant the way every other endpoint does, in UTC with milliseconds. The
// zero time renders as empty, so a vehicle in service omits deactivated_at rather than sending
// year one.
func timestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// Add handles POST /v1/fleet/vehicles (SHIP-78).
//
// Protected: the owner of the vehicle is whoever the token says is calling, and there is no field
// in the request for naming somebody else.
//
// Only a provider account may keep a fleet. A customer presenting a valid token passes RequireUser
// and is refused here with `fleet_provider_only` — read from users.role rather than from the role
// claim in the token, because the claim is evidence about the token and the column is the fact.
func (h *Handler) Add() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		var req vehicleRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		vehicle, err := h.svc.Add(r.Context(), pool, providerID, req.fields())
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusCreated, vehicleFrom(vehicle))
		return nil
	})
}

// Update handles PATCH /v1/fleet/vehicles/{id} (SHIP-78).
//
// PATCH rather than PUT, because a provider correcting one field should not have to send every
// field they are not changing — which is how a client that has not been updated for a new field
// silently clears it.
//
// A vehicle belonging to somebody else answers 404, not 403. See [apiError].
func (h *Handler) Update() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		vehicleID, err := vehicleIDFrom(r)
		if err != nil {
			return err
		}

		var req vehicleRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		var updated Vehicle
		err = db.InTx(r.Context(), pool, func(ctx context.Context, runner db.Runner) error {
			var err error
			updated, err = h.svc.Update(ctx, runner, providerID, vehicleID, req.fields())
			return err
		})
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, vehicleFrom(updated))
		return nil
	})
}

// Deactivate handles POST /v1/fleet/vehicles/{id}/deactivate (SHIP-78).
//
// A verb under the resource rather than a `PATCH` setting a field, and **not a DELETE**. Docs/01
// §4.2 gives the provider "add, edit, deactivate" and 000300 explains why the row survives: a
// vehicle is named by the bid that won a job and by the delivery that followed it, so removing it
// would remove part of a commercial record Docs/05 §3.1 requires retaining. `DELETE` would describe
// the wrong thing to every client that read the contract.
//
// It takes no body. There is nothing to say — unlike cancelling a job, where the customer may give
// a reason — and `POST /v1/auth/logout` already establishes that a state-changing request whose
// meaning is entirely in its URL does not invent a field to carry it.
//
// Deactivating an already-deactivated vehicle answers 200 with the vehicle: the caller asked for an
// outcome that holds. The idempotency middleware absorbs a retry that reuses its key; this absorbs
// the one that does not, which is the case where a phone lost its connection and generated a fresh
// key for the same intent.
func (h *Handler) Deactivate() http.Handler {
	return h.serviceChange(h.svc.Deactivate)
}

// Reactivate handles POST /v1/fleet/vehicles/{id}/reactivate (SHIP-78).
//
// The counterpart of [Handler.Deactivate], and the one that can be refused: the provider may have
// added a replacement on the same plate while this vehicle was out of service, and
// uq_vehicles_provider_registration refuses the second live row. That answers 409 with
// `fleet_duplicate_registration` rather than 422, because the value is well formed and it is the
// state of the fleet that contradicts the request.
//
// A vehicle that is already in service answers 200, unchanged, for the reason a repeated
// deactivation does.
func (h *Handler) Reactivate() http.Handler {
	return h.serviceChange(h.svc.Reactivate)
}

// changer is one of the two service methods that move a vehicle in or out of service.
type changer func(context.Context, db.Runner, uuid.UUID, uuid.UUID) (Vehicle, error)

// serviceChange is the body both of those handlers share.
//
// Written once because the two differ in exactly one call, and two copies of an authenticate,
// parse, open-a-transaction, map-the-error sequence is two places for the ownership check to be
// dropped from — which here would be another provider's fleet rather than a cosmetic defect.
func (h *Handler) serviceChange(change changer) http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		vehicleID, err := vehicleIDFrom(r)
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		var changed Vehicle
		err = db.InTx(r.Context(), pool, func(ctx context.Context, runner db.Runner) error {
			var err error
			changed, err = change(ctx, runner, providerID, vehicleID)
			return err
		})
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, vehicleFrom(changed))
		return nil
	})
}

// Detail handles GET /v1/fleet/vehicles/{id} (SHIP-78).
//
// The same [vehicleResponse] the writing endpoints answer with, so a client parses one type whatever
// it did to get the vehicle.
//
// Owner-only, and a stranger gets the same 404 a missing vehicle gets. No idempotency key: a GET
// changes nothing and the middleware lets read-only methods through untouched.
func (h *Handler) Detail() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		vehicleID, err := vehicleIDFrom(r)
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		// The pool rather than a transaction. One statement is atomic on its own, and a
		// transaction around a single SELECT buys nothing but a round trip either side.
		vehicle, err := h.svc.Vehicle(r.Context(), pool, providerID, vehicleID)
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, vehicleFrom(vehicle))
		return nil
	})
}

// List handles GET /v1/fleet/vehicles (SHIP-78).
//
// The provider's own fleet, newest first, filterable by whether a vehicle is in service and
// paginated by cursor (Docs/10 §4.5). **There is no parameter for whose fleet to list** — the owner
// is whoever the token says is calling, so there is no filter here to forget and no way to widen the
// query by asking.
//
// The default is every vehicle rather than the active ones, which is the less obvious of the two
// choices and the right one: a fleet screen that silently hid retired vehicles would leave a
// provider unable to find the one they need to bring back, and `?active=true` is one parameter away
// for the screen that wants only what can be offered.
func (h *Handler) List() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		query, err := vehicleQueryFrom(r)
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		page, err := h.svc.Vehicles(r.Context(), pool, providerID, query)
		if err != nil {
			return apiError(err)
		}

		vehicles := make([]vehicleResponse, 0, len(page.Vehicles))
		for _, vehicle := range page.Vehicles {
			vehicles = append(vehicles, vehicleFrom(vehicle))
		}

		httpx.WriteJSON(w, http.StatusOK, pagination.NewPage(vehicles, encodeVehicleCursor(page.Next)))
		return nil
	})
}

// --- SHIP-79: the provider's declaration ------------------------------------------------------

// profileRequest is the body of PATCH /v1/fleet/profile.
//
// Every list is a pointer, so absent, null and an explicit empty list are three distinct things —
// the same distinction [vehicleRequest] draws, applied to a set:
//
//	omitted or null   leave that list exactly as it is
//	an empty list     clear it
//
// A list that is supplied replaces its predecessor whole. There is no add-one or remove-one
// operation, deliberately: the client renders the declaration as a set of chips and sends the set
// back, and two operations over one collection is where a client and a server stop agreeing about
// what is in it.
//
// `service_area` is nested so that the two grains read as one idea rather than as two unrelated
// top-level lists — but the grains still move independently inside it, because a provider adding a
// postcode should not have to resend every state.
type profileRequest struct {
	ServiceArea *serviceAreaRequest `json:"service_area"`
	Specialties *[]string           `json:"specialties"`
}

// serviceAreaRequest is the two grains of a service area.
//
// **There is no radius and there is no coordinate**, and that is the decision SHIP-79 took rather
// than left to SHIP-81. model.go carries the reasoning; the short version is that a job's
// coordinate is best-effort and its state and postcode are not, so a radius would filter a feed on
// a field the platform may not have.
type serviceAreaRequest struct {
	States    *[]string `json:"states"`
	Postcodes *[]string `json:"postcodes"`
}

// fields turns the request into the domain's command.
//
// Values are carried across rather than checked here, for the reason [vehicleRequest.fields] gives:
// whether a state or a specialty is *acceptable* is the domain's judgement, made once in
// [ProfileFields.problems], so that the rule is the same for every caller rather than for whoever
// arrived over HTTP.
func (b profileRequest) fields() ProfileFields {
	f := ProfileFields{}

	if b.ServiceArea != nil {
		f.States = b.ServiceArea.States
		f.Postcodes = b.ServiceArea.Postcodes
	}
	if b.Specialties != nil {
		specialties := make([]Specialty, 0, len(*b.Specialties))
		for _, raw := range *b.Specialties {
			specialties = append(specialties, Specialty(raw))
		}
		f.Specialties = &specialties
	}
	return f
}

// profileResponse is a provider's declaration as they see it.
//
// **Every list is always present and never null**, which is the opposite of the rule
// [vehicleResponse] follows and is right for the opposite reason. An omitted capacity means "the
// provider has not said"; an omitted list would mean the same thing as an empty one, and here that
// distinction is exactly what the client is editing. A provider who has declared nothing gets three
// empty arrays, which a client can render and iterate without a nil check.
type profileResponse struct {
	ServiceArea serviceAreaResponse `json:"service_area"`
	Specialties []string            `json:"specialties"`
}

type serviceAreaResponse struct {
	// States is every state or territory the provider serves in full, ascending.
	States []string `json:"states"`

	// Postcodes is every single postcode they serve, ascending, and it is not narrowed by States:
	// the two are separate claims, and a postcode named inside a state already declared is
	// harmless duplication rather than a contradiction.
	Postcodes []string `json:"postcodes"`
}

// profileFrom splits the domain's flat set of areas into the two lists a client renders.
//
// The domain holds one list because that is what the table holds and what a membership test walks;
// the wire holds two because the screen has two controls. Splitting here rather than in the store
// keeps the shape of the API a decision of this file's.
func profileFrom(p Profile) profileResponse {
	out := profileResponse{
		ServiceArea: serviceAreaResponse{States: []string{}, Postcodes: []string{}},
		Specialties: []string{},
	}

	for _, area := range p.Areas {
		switch area.Scope {
		case ScopeState:
			out.ServiceArea.States = append(out.ServiceArea.States, area.Area)
		case ScopePostcode:
			out.ServiceArea.Postcodes = append(out.ServiceArea.Postcodes, area.Area)
		}
	}
	for _, specialty := range p.Specialties {
		out.Specialties = append(out.Specialties, string(specialty))
	}
	return out
}

// Profile handles GET /v1/fleet/profile (SHIP-79).
//
// The calling provider's own declaration. **There is no parameter for whose**, and no route to
// another provider's: what a competitor covers and specialises in is commercial information, and the
// customer-facing view of a provider arrives with SHIP-96 as a shape of its own.
//
// It never answers 404. An undeclared profile is an empty one, so a client opening the screen for
// the first time gets three empty arrays rather than an error it has to read as "not yet".
func (h *Handler) Profile() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		profile, err := h.svc.Profile(r.Context(), pool, providerID)
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, profileFrom(profile))
		return nil
	})
}

// Declare handles PATCH /v1/fleet/profile (SHIP-79).
//
// PATCH rather than PUT, and the reason is the one [Handler.Update] gives sharpened by a second: a
// provider changing their specialties should not have to resend four hundred postcodes, and a
// client that has not been updated for a field added later would clear it by sending everything it
// knows about. What each *named* list does is still a replacement — the three-state rule is in
// [profileRequest].
//
// Only a provider account may declare one, refused with `fleet_provider_only` and read from
// users.role rather than from the role claim in the token. The matching read is deliberately not
// refused: see [Service.Profile].
//
// A transaction, because replacing a set is a delete and an insert that must be one decision, and
// because the advisory lock that stops two devices interleaving only lasts as long as one.
func (h *Handler) Declare() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		var req profileRequest
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		var declared Profile
		err = db.InTx(r.Context(), pool, func(ctx context.Context, runner db.Runner) error {
			var err error
			declared, err = h.svc.Declare(ctx, runner, providerID, req.fields())
			return err
		})
		if err != nil {
			return apiError(err)
		}

		httpx.WriteJSON(w, http.StatusOK, profileFrom(declared))
		return nil
	})
}

// --- SHIP-82: the marketplace as a provider sees it -------------------------------------------

// openJobResponse is a job a provider may bid on.
//
// # There is no budget field, in any form, and this is the shape that had to prove it
//
// Docs/01 §4.3 keeps the customer's maximum private from providers — "not as an amount, a band, or
// a 'budget supplied' flag" — and CLAUDE.md calls a breach a defect rather than a style choice.
// Docs/11 §8 records SHIP-83 as owing the one proof SHIP-67 could not write — the provider's
// response, serialised, asserted to carry no budget — and **SHIP-83 answers it against this type**,
// because this is the shape both of its endpoints put on the wire.
//
// # The street line is not here either, and SHIP-83 confirms or reopens that
//
// SHIP-81 took the decision for the feed because no document takes a position: a provider prices a
// job on the locality, the distance and the state, and the doorstep is needed by whoever drives to
// it, which is after an award. The coordinate is absent for the same reason rather than a different
// one — `jobs` geocodes the whole address, so a pickup coordinate *is* the street line written as
// two numbers.
//
// Nor is there a customer. Docs/01 §4.3 lets a *customer* compare provider profiles once bids
// arrive; nothing gives the reverse before an award.
type openJobResponse struct {
	ID string `json:"id"`

	// Status is `open` or `negotiating` and can be nothing else. A job already being negotiated is
	// one a provider is bidding against company, which is worth their knowing before they price it.
	Status string `json:"status"`

	// Pickup is present for every job this endpoint can return: the eligibility filter matches a
	// declared region against the pickup state or postcode, so a job with neither cannot be here.
	// Dropoff can be absent, because 000403 lets a customer publish before they have both ends.
	Pickup  *regionResponse `json:"pickup,omitempty"`
	Dropoff *regionResponse `json:"dropoff,omitempty"`

	GoodsDescription string  `json:"goods_description,omitempty"`
	LengthCm         int     `json:"length_cm,omitempty"`
	WidthCm          int     `json:"width_cm,omitempty"`
	HeightCm         int     `json:"height_cm,omitempty"`
	WeightKg         float64 `json:"weight_kg,omitempty"`

	VehicleRequirement string `json:"vehicle_requirement,omitempty"`
	HandlingNotes      string `json:"handling_notes,omitempty"`

	PickupWindow  *windowResponse `json:"pickup_window,omitempty"`
	DropoffWindow *windowResponse `json:"dropoff_window,omitempty"`

	// ExpiresAt is when the job stops being offered (SHIP-68), so a provider deciding whether to
	// price it today knows it will still be there tomorrow.
	ExpiresAt string `json:"expires_at,omitempty"`

	CreatedAt string `json:"created_at"`
}

// regionResponse is one end of a job, at the grain a provider is given it.
//
// Three parts and no fourth. See [openJobResponse] for why the street line and the coordinate are
// both absent, and [Region] for the decision SHIP-81 took first.
type regionResponse struct {
	Suburb   string `json:"suburb,omitempty"`
	State    string `json:"state,omitempty"`
	Postcode string `json:"postcode,omitempty"`
}

// windowResponse is a period a job's pickup or delivery has to happen in, with either end omitted
// when the customer only cared about the other one.
type windowResponse struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

func openJobFrom(j EligibleJob) openJobResponse {
	return openJobResponse{
		ID:     j.ID.String(),
		Status: wireStatus(j.Status),

		Pickup:  regionFrom(j.Pickup),
		Dropoff: regionFrom(j.Dropoff),

		GoodsDescription: j.GoodsDescription,
		LengthCm:         j.LengthCm,
		WidthCm:          j.WidthCm,
		HeightCm:         j.HeightCm,
		WeightKg:         j.WeightKg,

		VehicleRequirement: j.VehicleRequirement,
		HandlingNotes:      j.HandlingNotes,

		PickupWindow:  windowFrom(j.PickupWindow),
		DropoffWindow: windowFrom(j.DropoffWindow),

		ExpiresAt: timestamp(j.ExpiresAt),
		CreatedAt: timestamp(j.CreatedAt),
	}
}

// regionFrom omits an end of the job the customer has not filled in, rather than sending `{}`.
func regionFrom(r Region) *regionResponse {
	if r == (Region{}) {
		return nil
	}
	return &regionResponse{Suburb: r.Suburb, State: r.State, Postcode: r.Postcode}
}

// windowFrom does the same for a window nobody stated.
func windowFrom(w Window) *windowResponse {
	if w.Start.IsZero() && w.End.IsZero() {
		return nil
	}
	return &windowResponse{Start: timestamp(w.Start), End: timestamp(w.End)}
}

// wireStatus turns a stored job status into the form Docs/10 §4.7 puts on the wire — lower snake
// case, so `Driver assigned` becomes `driver_assigned`.
//
// **A second copy of a rule `jobs` also holds**, for the reason [biddableStatuses] is a second copy
// of two strings: domains do not import each other. It is the general transformation rather than a
// two-entry lookup, so that it stays correct if this endpoint ever carries a third status, and
// TestTheBiddableStatusesReachTheWireInDocs02sNames holds its output for the two statuses that can
// reach here to the names Docs/02 §1 and the published contract both use.
func wireStatus(stored string) string {
	return strings.ToLower(strings.ReplaceAll(stored, " ", "_"))
}

// OpenJobs handles GET /v1/jobs/open (SHIP-82).
//
// The marketplace as this provider may bid on it: every open job the platform's eligibility filter
// offers them, newest first, keyset-paged (Docs/10 §4.5).
//
// **There is no parameter that widens it and none that narrows it.** Not a state, not a vehicle,
// not a goods type. Eligibility is the platform's decision (Docs/07 §3 — the app may hide, the
// platform decides), and a filter parameter here would be a second place for that answer to be
// argued with; a provider narrowing their own feed further is SHIP-99's client-side business.
//
// **A caller who is eligible for nothing gets an empty page, not a refusal**, and that includes an
// unverified provider, one who has declared no service area, one with no vehicle in service, and a
// customer who followed a link meant for the other role. None of the four is a failure of the
// request — they are the truthful answer to it — and the read discloses nothing, so refusing it
// would make the client special-case a screen it can render from an empty list and the profile it
// already holds. That is the reading [Service.Profile] takes for the same reason.
//
// The route lives in cmd/api/routes_fleet.go rather than routes_jobs.go, because routes are
// declared rather than registered and the declaring file is the one whose domain decides the
// answer. `fleet` owns eligibility; `jobs` owns the customer's view of a job.
func (h *Handler) OpenJobs() http.Handler {
	return httpx.H(func(w http.ResponseWriter, r *http.Request) error {
		providerID, err := callerID(r.Context())
		if err != nil {
			return err
		}

		query, err := eligibilityQueryFrom(r)
		if err != nil {
			return err
		}

		pool, err := h.database(r)
		if err != nil {
			return err
		}

		page, err := h.svc.EligibleJobs(r.Context(), pool, providerID, query)
		if err != nil {
			return apiError(err)
		}

		jobs := make([]openJobResponse, 0, len(page.Jobs))
		for _, job := range page.Jobs {
			jobs = append(jobs, openJobFrom(job))
		}

		httpx.WriteJSON(w, http.StatusOK, pagination.NewPage(jobs, encodeJobCursor(page.Next)))
		return nil
	})
}

// eligibilityQueryFrom reads `?limit=` and `?cursor=`, which is every parameter this feed has.
//
// An unknown query parameter is passed over rather than refused, which is the opposite of what
// httpx.DecodeJSON does to an unknown *body* field. Docs/07 §6's strictness is about bodies: a URL
// picks up parameters from link trackers and proxies that no client typed, and refusing those would
// break a request nobody composed. What matters is the other direction — neither parameter this
// endpoint does read can widen the filter, and there are only two of them.
func eligibilityQueryFrom(r *http.Request) (EligibilityQuery, error) {
	values := r.URL.Query()

	var query EligibilityQuery

	limit, err := pagination.Limit(values.Get("limit"))
	if err != nil {
		return EligibilityQuery{}, err
	}
	query.Limit = limit

	if query.After, err = decodeJobCursor(values.Get("cursor")); err != nil {
		return EligibilityQuery{}, err
	}
	return query, nil
}

// jobCursorFields is how many parts a job cursor has: the created_at it is positioned at, and the
// id that breaks ties on it. Two, for the reason [JobCursor] gives — a customer publishing several
// jobs in one sitting writes rows in the same millisecond, and a cursor that could not break that
// tie would repeat or drop a job at exactly the page boundary.
const jobCursorFields = 2

// encodeJobCursor renders a position for a client to hand back, in the encoding
// `internal/pagination` owns. The zero cursor is the empty string, which is also what "no cursor"
// looks like on the way in.
func encodeJobCursor(c JobCursor) string {
	if c.IsZero() {
		return ""
	}
	return pagination.Cursor{
		c.CreatedAt.UTC().Format(time.RFC3339Nano),
		c.ID.String(),
	}.Encode()
}

// decodeJobCursor reads one back. See [decodeVehicleCursor] for why the shape is checked by
// `pagination` and the meaning here, and why the timestamp is nanosecond rather than the
// millisecond precision responses render.
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

// vehicleQueryFrom reads `?active=`, `?limit=` and `?cursor=`.
//
// Every failure it returns is already in the error contract, and they are bad_request rather than
// validation_failed throughout — httpx's own division: a query parameter is part of how the request
// was addressed rather than data a person typed into a form.
func vehicleQueryFrom(r *http.Request) (VehicleQuery, error) {
	values := r.URL.Query()

	var query VehicleQuery

	if raw := strings.TrimSpace(values.Get("active")); raw != "" {
		wanted, err := strconv.ParseBool(raw)
		if err != nil {
			// Refused rather than read as false. Answering `?active=yes` with the retired
			// vehicles would tell a client its filter worked, which is the failure the
			// status filter in `jobs` refuses for the same reason.
			return VehicleQuery{}, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
				"The active filter must be true or false.").WithCause(err)
		}
		query.Active = &wanted
	}

	limit, err := pagination.Limit(values.Get("limit"))
	if err != nil {
		return VehicleQuery{}, err
	}
	query.Limit = limit

	if query.After, err = decodeVehicleCursor(values.Get("cursor")); err != nil {
		return VehicleQuery{}, err
	}
	return query, nil
}

// vehicleCursorFields is how many parts a vehicle cursor has: the created_at it is positioned at,
// and the id that breaks ties on it.
const vehicleCursorFields = 2

// encodeVehicleCursor renders a position for a client to hand back. The zero cursor is the empty
// string, which is also what "no cursor" looks like on the way in.
func encodeVehicleCursor(c VehicleCursor) string {
	if c.IsZero() {
		return ""
	}
	return pagination.Cursor{
		c.CreatedAt.UTC().Format(time.RFC3339Nano),
		c.ID.String(),
	}.Encode()
}

// decodeVehicleCursor reads one back.
//
// pagination.Decode establishes the shape — this version, this many fields — and this establishes
// the meaning. The split matters: only the domain knows that its ordering key is a timestamp and a
// UUID, and a cursor whose fields decode but do not parse must be refused here rather than reaching
// the query as a zero time, which would silently answer with the first page.
//
// **Nanoseconds, not the millisecond precision responses use.** A cursor is compared against
// created_at rather than displayed, and a rendering that rounded would put the boundary in the wrong
// place — repeating a vehicle at every page edge, or dropping one.
func decodeVehicleCursor(raw string) (VehicleCursor, error) {
	fields, err := pagination.Decode(raw, vehicleCursorFields)
	if err != nil || fields == nil {
		return VehicleCursor{}, err
	}

	at, err := time.Parse(time.RFC3339Nano, fields[0])
	if err != nil {
		return VehicleCursor{}, invalidVehicleCursor(err)
	}
	id, err := uuid.Parse(fields[1])
	if err != nil {
		return VehicleCursor{}, invalidVehicleCursor(err)
	}
	return VehicleCursor{CreatedAt: at, ID: id}, nil
}

func invalidVehicleCursor(cause error) error {
	return httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
		"The cursor is not one this endpoint issued. Ask for the first page without one.").
		WithCause(cause)
}

// vehicleIDFrom reads and parses the {id} path parameter.
//
// A path parameter of the wrong shape is bad_request rather than not_found, which is the httpx
// registry's own description of that code. It also discloses nothing: the answer is the same whether
// or not any vehicle exists.
func vehicleIDFrom(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return uuid.Nil, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"The vehicle id in the path is not a valid identifier.").WithCause(err)
	}
	return id, nil
}

// callerID is the authenticated provider's account id.
//
// Every route declares RequireUser, so a subject is guaranteed by the time a handler runs — which is
// why the absence of one is reported as an internal failure rather than as a 401. Reaching here
// without a subject means a route was declared public and written as though it were protected, and
// that is a wiring defect the caller can do nothing about.
//
// The *role* is deliberately not read from the subject. It is a claim in a token; the domain reads
// users.role instead, which is the fact.
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

	httpx.LoggerFrom(r.Context()).Warn("a fleet request arrived with no database connection")
	return nil, httpx.NewError(http.StatusServiceUnavailable, httpx.CodeUnavailable,
		"This cannot be completed right now. Try again shortly.")
}

// apiError turns this domain's errors into the API's error contract.
//
// The mapping lives at the transport edge on purpose: the service answers in domain terms, and which
// HTTP status a non-owner deserves is not a question the domain has an opinion about. A *httpx.Error
// passes straight through, because validation already produced one in the right shape.
//
// # Why a vehicle belonging to somebody else is 404 and not 403
//
// 403 would confirm that the vehicle exists, and a provider's fleet is nobody else's business —
// which vehicles a competitor runs is commercial information they never published. httpx's own
// description of `not_found` says the two cases are deliberately indistinguishable, and this is one
// of the places that matters.
//
// The domain still distinguishes them ([ErrNotVehicleOwner] against [ErrVehicleNotFound]) so that a
// test can tell "the non-owner was refused" from "the vehicle silently stopped existing".
//
// Anything unrecognised is returned as-is and becomes an opaque 500 in httpx.WriteError. That is the
// correct default: an error nobody has given a status and a code has not been considered.
func apiError(err error) error {
	var apiErr *httpx.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}

	switch {
	case errors.Is(err, ErrVehicleNotFound), errors.Is(err, ErrNotVehicleOwner):
		return httpx.NewError(http.StatusNotFound, httpx.CodeNotFound,
			"No such vehicle.").WithCause(err)

	case errors.Is(err, ErrNotProvider):
		return httpx.NewError(http.StatusForbidden, CodeProviderOnly,
			"Only a provider account can keep a fleet.").WithCause(err)

	case errors.Is(err, ErrDuplicateRegistration):
		return httpx.NewError(http.StatusConflict, CodeDuplicateRegistration,
			"A vehicle with that registration is already in service in this fleet.").WithCause(err)

	case errors.Is(err, ErrNothingToUpdate):
		return httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"The request changes nothing. Send at least one field to update.").WithCause(err)

	default:
		return err
	}
}
