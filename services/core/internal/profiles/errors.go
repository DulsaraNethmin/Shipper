package profiles

import (
	"errors"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// The sentinel errors this domain raises, and the error codes its endpoints answer with.
//
// Docs/10 §2.1 puts the sentinels here rather than beside the code that returns them, so a caller
// deciding what to do about a failure has one file to read. The callers written for are SHIP-153 and
// SHIP-154, which review and decide, and `cmd/api`, which maps these onto the wire.
var (
	// ErrNotProvider means the account is not a provider account.
	//
	// The same reading `fleet.ErrNotProvider` takes, and read from `users.role` rather than from the
	// role claim in the token: the claim is evidence about the token and the column is the fact.
	// SHIP-78a is where that stopped being true of only two methods.
	ErrNotProvider = errors.New("profiles: only a provider account has a verification record")

	// ErrNoSuchProvider means there is no verification record for that identifier.
	//
	// Distinct from [ErrNotProvider] in Go and one 404 on the wire. `000200` gives every provider a
	// record at registration, so reaching this means the account never existed or has been removed
	// underneath a live decision — and a reviewer being told "no such provider" rather than "not a
	// provider" is the difference between a stale queue and a wrong permission.
	ErrNoSuchProvider = errors.New("profiles: no such provider")

	// ErrAlreadyInState means a decision would not change anything.
	//
	// Refused rather than absorbed. A decision is an act by a person and Docs/04 §6.6 requires it be
	// recorded with a reason; a second one that changed nothing would be a row asserting a review
	// took place with no change to show for it. See [Service.Decide].
	ErrAlreadyInState = errors.New("profiles: the provider is already in that state")

	// ErrActorNotRecorded means the decision named no usable actor.
	//
	// Not a field error, because no client supplies the actor: it comes from the deciding
	// administrator's own credential in the composition root, so a bad one is a wiring fault rather
	// than a request somebody can correct. `ck_provider_verification_decisions_actor_id` is the
	// layer underneath.
	ErrActorNotRecorded = errors.New("profiles: a decision must record who took it")

	// ErrNotInTransaction means a decision was attempted outside a transaction.
	//
	// The database refuses it too — `provider_verification_decide` sets a transaction-local variable
	// the trigger reads, and outside a transaction it lasts only for the statement that set it — so
	// this is the legible half of a refusal that holds either way.
	ErrNotInTransaction = errors.New("profiles: a verification decision needs a transaction")
)

// The error codes this domain's endpoints answer with.
//
// Registered rather than declared as constants, so that the generated Docs/10-api-error-codes.md
// describes them and `cmd/api`'s uniqueness test can see them. Named `<domain>_<condition>`, which is
// what stops two domains meaning different things by one string.
//
// **There is deliberately one.** Everything else here is covered by the protocol codes: a reason that
// is too long is `validation_failed` with details, a provider who does not exist is `not_found`, and
// a decision that changes nothing is `conflict`. A domain code earns its place only where a client
// would otherwise have to parse a message to know what to do.
var (
	// CodeProviderOnly is returned when an account that is not a provider reaches a verification
	// endpoint.
	//
	// A distinct code rather than a bare 403 for `fleet.CodeProviderOnly`'s reason: the app can act
	// on it by showing the customer surface. It is a *separate* registration from fleet's and not a
	// shared one, because two domains sharing a code is two domains that can no longer change their
	// own message — and `httpx.RegisterCode` refuses a duplicate string, which is what makes that a
	// decision rather than an accident.
	CodeProviderOnly = httpx.RegisterCode("profiles_provider_only",
		"Only a provider account has a verification record. Customers publish jobs; they are not "+
			"verified to bid.")
)
