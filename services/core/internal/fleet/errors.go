package fleet

import (
	"errors"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// The sentinel errors this domain raises, and the error codes its endpoints answer with.
//
// Docs/10 §2.1 puts the sentinels here rather than beside the code that returns them, so a caller
// deciding what to do about a failure has one file to read. They are for Go callers, and SHIP-81's
// eligibility filter and SHIP-89's bid — both of which read a provider's fleet — are the ones this
// list is written for.
//
// The codes below are what reaches a client, and they are a separate list on purpose: a sentinel
// says what happened, a code says what the client should do about it. [ErrNotVehicleOwner] and
// [ErrVehicleNotFound] deliberately share `not_found`, because telling a caller which of the two it
// was would confirm the existence of somebody else's vehicle.
var (
	// ErrVehicleNotFound means no vehicle with that identifier exists.
	//
	// Deliberately not distinguished from "exists and is none of your business" — that decision
	// belongs to the endpoint, which is the layer that knows who is asking.
	ErrVehicleNotFound = errors.New("fleet: no such vehicle")

	// ErrNotVehicleOwner means the vehicle exists and belongs to another provider.
	//
	// Distinct from ErrVehicleNotFound in Go and indistinguishable from it on the wire. The
	// distinction is worth keeping here because a test asserting "the stranger was refused"
	// should fail if the vehicle silently stopped existing instead — the same 404 to a client
	// and very different defects.
	ErrNotVehicleOwner = errors.New("fleet: that vehicle belongs to another provider")

	// ErrNotProvider means the account is not a provider account.
	//
	// 000300 has no CHECK for it — a foreign key cannot see another table's column — so the rule
	// is enforced where a vehicle is added, and it reads users.role rather than trusting the role
	// claim in the token: the claim is evidence about the token and the column is the fact. The
	// same arrangement 000400 uses for jobs.customer_id.
	ErrNotProvider = errors.New("fleet: only a provider account can keep a fleet")

	// ErrDuplicateRegistration means the provider already has a vehicle in service on that plate.
	//
	// It is the domain's reading of uq_vehicles_provider_registration, which is partial on
	// deactivated_at being NULL — so a plate that was retired can be added again, and only a
	// second *live* row for one truck is refused. The index is what makes this true; this
	// sentinel is what makes the refusal legible, because a caller handed the constraint name
	// learns nothing they can act on.
	ErrDuplicateRegistration = errors.New("fleet: that registration is already in service")

	// ErrNothingToUpdate means an edit named no field at all.
	ErrNothingToUpdate = errors.New("fleet: the request changes nothing")

	// ErrNotInTransaction means a method that reads, decides and writes was handed a connection
	// pool rather than a transaction.
	//
	// Unlike jobs' sentinel of the same name, no trigger enforces this — which is exactly why it
	// is checked. An edit outside a transaction releases lockVehicle's row lock the instant the
	// SELECT returns, and two concurrent edits then interleave into a vehicle carrying half of
	// each, with nothing to report afterwards.
	ErrNotInTransaction = errors.New("fleet: this must run inside a transaction")
)

// The error codes this domain's endpoints answer with (Docs/10 §4.4).
//
// Registered rather than declared as constants, so that the generated Docs/10-api-error-codes.md
// describes them and cmd/api's uniqueness test can see them. Named <domain>_<condition>, which is
// what stops two domains meaning different things by one string.
//
// There are deliberately two. Everything else here is already covered by the protocol codes: a
// registration that is too long is `validation_failed` with details, a vehicle that is not yours is
// `not_found`, and a missing idempotency key is the middleware's business. A domain code earns its
// place only where a client would otherwise have to parse a message to know what to do.
var (
	// CodeProviderOnly is returned when a customer account reaches a fleet endpoint.
	//
	// A distinct code rather than a bare 403 because the client can act on it: the app shows the
	// customer surface, and a customer reaching this has followed a link or a deep route meant
	// for the other role (Docs/07 §3 — the app may hide, the platform decides).
	CodeProviderOnly = httpx.RegisterCode("fleet_provider_only",
		"Only a provider account can keep a fleet. Customers publish jobs; they do not run vehicles.")

	// CodeDuplicateRegistration is returned when a provider already has that plate in service.
	//
	// 409 rather than 422: the value is well formed and the request contradicts the state the
	// fleet is in. The client's correct response is to show the provider the vehicle they already
	// have rather than to mark the field as invalid, which is a different action and a different
	// screen.
	CodeDuplicateRegistration = httpx.RegisterCode("fleet_duplicate_registration",
		"A vehicle with that registration is already in service in this fleet. Edit the existing "+
			"one, or deactivate it first.")
)
