package fleet

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-78a — every fleet endpoint refuses a caller who is not a provider.
//
// # What this file is guarding against, and it is not a disclosure
//
// Docs/11 §9 carried this from wave 5: `isProvider` was called in `Add` and `Declare` and in no
// other method. Every one of the other six scopes to the caller's own identifier, so a customer
// reaching one got an empty list or a 404 and never another provider's vehicle. **The defect was
// the absence of a refusal**, which is Docs/07 §3's "the platform decides" not being exercised.
//
// # Why the completeness half matters more than the eight cases
//
// A table of eight is a snapshot: the ninth method added to this service is exactly the one that
// would not be in it, and a table-driven test says nothing about the case somebody forgot to add.
// [TestEveryServiceMethodIsClassified] reflects over the type and fails on a method that is neither
// covered below nor exempt for a stated reason — so adding a method to this service is a decision
// somebody records rather than a gap that appears.

// providerOnlyCase is one service method driven with whatever subject the test supplies.
//
// inTx says whether the method requires a transaction. The four writing methods refuse a pool with
// [ErrNotInTransaction] before they reach anything this file is about, which would make a customer's
// refusal indistinguishable from a runner of the wrong kind.
type providerOnlyCase struct {
	method string
	inTx   bool
	call   func(ctx context.Context, s *Service, r db.Runner, subject uuid.UUID) error
}

// providerOnlyCases is the eight methods SHIP-78a names, in the order Docs/09's prose lists them.
//
// Every call passes arguments that are otherwise valid, so a refusal can only be about the subject.
// A vehicle identifier that names nothing is deliberate: the eventual answer for a *provider* is
// [ErrVehicleNotFound], which is a different sentinel, so a case that stopped refusing would fail
// rather than pass on the wrong error.
func providerOnlyCases() []providerOnlyCase {
	vehicle := uuid.MustParse("00000000-0000-7000-8000-0000000078aa")

	return []providerOnlyCase{
		{"Add", false, func(ctx context.Context, s *Service, r db.Runner, subject uuid.UUID) error {
			_, err := s.Add(ctx, r, subject, VehicleFields{
				Registration: ptr("XX78AA"), Type: ptr(TypeVan)})
			return err
		}},
		{"Update", true, func(ctx context.Context, s *Service, r db.Runner, subject uuid.UUID) error {
			_, err := s.Update(ctx, r, subject, vehicle, VehicleFields{Make: ptr("Toyota")})
			return err
		}},
		{"Deactivate", true, func(ctx context.Context, s *Service, r db.Runner, subject uuid.UUID) error {
			_, err := s.Deactivate(ctx, r, subject, vehicle)
			return err
		}},
		{"Reactivate", true, func(ctx context.Context, s *Service, r db.Runner, subject uuid.UUID) error {
			_, err := s.Reactivate(ctx, r, subject, vehicle)
			return err
		}},
		{"Vehicle", false, func(ctx context.Context, s *Service, r db.Runner, subject uuid.UUID) error {
			_, err := s.Vehicle(ctx, r, subject, vehicle)
			return err
		}},
		{"Vehicles", false, func(ctx context.Context, s *Service, r db.Runner, subject uuid.UUID) error {
			_, err := s.Vehicles(ctx, r, subject, VehicleQuery{})
			return err
		}},
		{"Profile", false, func(ctx context.Context, s *Service, r db.Runner, subject uuid.UUID) error {
			_, err := s.Profile(ctx, r, subject)
			return err
		}},
		{"Declare", true, func(ctx context.Context, s *Service, r db.Runner, subject uuid.UUID) error {
			_, err := s.Declare(ctx, r, subject, ProfileFields{States: ptr([]string{"NSW"})})
			return err
		}},
	}
}

// exemptFromProviderOnly is every other exported method on [Service], with the reason it takes no
// provider-account check.
//
// **Each of these is a decision rather than an omission**, which is the whole point of writing them
// down: the eligibility three are governed by one SQL predicate that already reads `users.role`, and
// adding a Go check beside it would be the second answer to who may bid that eligibility.go exists
// not to have.
var exemptFromProviderOnly = map[string]string{
	"PublicProfiles": "SHIP-79a's customer-facing read: a customer comparing offers is the caller it " +
		"was written for, so refusing one would empty the comparison screen",

	"EligibleJobs": "the feed's own predicate carries `u.role = 'provider'` in SQL, so a customer " +
		"gets an empty page — a second check here would be a second answer to who may bid",
	"EligibleFor": "same predicate, asked about one job: a customer is answered false by the SQL",
	"ProviderJobFor": "reads `readable`, which is `eligible` plus the caller's own bids — a customer " +
		"matches neither branch and is answered ErrJobNotOffered by the query",

	"PseudonymiseProfile": "SHIP-171's sweep, and the one method here with no caller at all: it is " +
		"reached from cmd/worker through a port internal/identity declares, on a schedule, with no " +
		"credential and no subject. A role check would be asking whether a background task is a " +
		"provider. What it may write is bounded instead — the same display-name rule Declare " +
		"applies — and it changes nothing for an account that never declared a name",
}

// TestEveryFleetMethodRefusesACustomer is SHIP-78a's acceptance criterion.
//
// It drives all eight with a *customer* subject and fails when any one of them answers instead of
// refusing. Before SHIP-78a six of them answered: an empty page, an empty profile, or a 404 about a
// vehicle that was never theirs to ask about.
func TestEveryFleetMethodRefusesACustomer(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "fleet-78a-customer@example.com", "+61400078101", "customer")

	for _, c := range providerOnlyCases() {
		t.Run(c.method, func(t *testing.T) {
			err := runProviderOnly(t, pool, c, customer)
			if err == nil {
				t.Fatalf("%s answered a customer; SHIP-78a requires it to refuse", c.method)
			}
			if !errors.Is(err, ErrNotProvider) {
				t.Fatalf("%s refused a customer with %v, want ErrNotProvider — the same sentinel "+
					"Add and Declare already returned", c.method, err)
			}

			// The same sentinel has to reach the wire the same way, or "one typed error" is true
			// in Go and false to a client.
			var api *httpx.Error
			if !errors.As(apiError(err), &api) {
				t.Fatalf("%s: apiError(%v) is not an httpx.Error", c.method, err)
			}
			if api.Status != http.StatusForbidden || api.Code != CodeProviderOnly {
				t.Errorf("%s answered %d/%s, want %d/%s",
					c.method, api.Status, api.Code, http.StatusForbidden, CodeProviderOnly)
			}
		})
	}
}

// TestEveryFleetMethodStillAnswersAProvider is the other half, and without it the test above is
// satisfied by a service that refuses everybody.
//
// A provider gets past the check, so the error — where there is one — is about what was asked for
// rather than about who asked. None of these may be [ErrNotProvider].
func TestEveryFleetMethodStillAnswersAProvider(t *testing.T) {
	pool := pgtest.DB(t)
	provider := newProvider(t, pool, "fleet-78a-provider@example.com", "+61400078102")

	for _, c := range providerOnlyCases() {
		t.Run(c.method, func(t *testing.T) {
			if err := runProviderOnly(t, pool, c, provider); errors.Is(err, ErrNotProvider) {
				t.Fatalf("%s refused a provider account with ErrNotProvider", c.method)
			}
		})
	}
}

// TestTheNilSubjectIsRefusedEverywhere.
//
// A request with no subject cannot reach a handler — RequireUser is what puts one in the context —
// so this is the guard against a caller inside the service passing the zero value. It is the same
// refusal as a customer's, deliberately: both are "not a provider", and two sentinels for one
// answer would be two branches for a client to write.
func TestTheNilSubjectIsRefusedEverywhere(t *testing.T) {
	pool := pgtest.DB(t)

	for _, c := range providerOnlyCases() {
		t.Run(c.method, func(t *testing.T) {
			if err := runProviderOnly(t, pool, c, uuid.Nil); !errors.Is(err, ErrNotProvider) {
				t.Fatalf("%s answered the nil subject with %v, want ErrNotProvider", c.method, err)
			}
		})
	}
}

// TestEveryServiceMethodIsClassified fails when a method is added to [Service] and is neither driven
// by [providerOnlyCases] nor exempt for a stated reason.
//
// **This is what stops the table above becoming a snapshot.** It asserts nothing about behaviour; it
// asserts that somebody looked. A ninth method is a decision, and the failure this produces names it.
func TestEveryServiceMethodIsClassified(t *testing.T) {
	covered := make(map[string]bool)
	for _, c := range providerOnlyCases() {
		covered[c.method] = true
	}

	var unclassified []string
	typ := reflect.TypeOf(&Service{})

	for i := range typ.NumMethod() {
		name := typ.Method(i).Name
		if covered[name] || exemptFromProviderOnly[name] != "" {
			continue
		}
		unclassified = append(unclassified, name)
	}

	sort.Strings(unclassified)
	if len(unclassified) > 0 {
		t.Fatalf("these fleet.Service methods are neither driven by TestEveryFleetMethodRefusesACustomer "+
			"nor listed in exemptFromProviderOnly with a reason: %v — SHIP-78a's rule is that a caller "+
			"who is not a provider is refused, and a method nobody classified is the one that forgot",
			unclassified)
	}

	// And the exemptions have to name methods that exist, or the list decays into a set of names
	// that excuse nothing while reading as though they do.
	for name := range exemptFromProviderOnly {
		if _, found := typ.MethodByName(name); !found {
			t.Errorf("exemptFromProviderOnly names %s, which fleet.Service does not have", name)
		}
	}
}

// runProviderOnly drives one case, opening a transaction where the method requires one.
func runProviderOnly(t *testing.T, pool *pgxpool.Pool, c providerOnlyCase, subject uuid.UUID) error {
	t.Helper()

	s := newTestService()
	if !c.inTx {
		return c.call(t.Context(), s, pool, subject)
	}
	return inTx(t, pool, func(ctx context.Context, r db.Runner) error {
		return c.call(ctx, s, r, subject)
	})
}
