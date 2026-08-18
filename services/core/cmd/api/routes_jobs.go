package main

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/platform/geocoding"
)

// The jobs domain's routes (SHIP-61 onwards).
//
// This file exists so that adding a domain adds a file and edits none. cmd/api/routes.go,
// manifest.go and main.go are shared surfaces (Docs/10 §9.2); a route registration that had to go
// into one of them is a line every concurrent branch also touches, and a badly resolved conflict
// there drops an endpoint with no compile error and no failing test.
//
// # Why every route requires a user
//
// A job belongs to a customer. There is no field in any request for naming which one — the
// owner is whoever the token says is calling — so no endpoint here has a meaning without a
// credential. That is also what makes them safe under SHIP-44's scoped idempotency: keys land
// in idem:v1:<subject>:<key> rather than the shared anonymous namespace, which is the gate
// CLAUDE.md holds authenticated state-changing endpoints behind and which SHIP-44 closed.
//
// The role is not enforced here. A provider presenting a valid token passes RequireUser and is
// refused by the domain, which reads users.role rather than trusting the token's claim — 000400
// says the customer-account rule is enforced where the draft is created, and that is one place
// rather than two that can disagree.
func init() {
	register(
		Route{
			Method:  http.MethodGet,
			Pattern: "/jobs",
			Group:   GroupV1,
			Auth:    RequireUser,
			Limit:   LimitRead,
			Handler: func(d Deps) http.Handler { return jobsHandler(d).List() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/jobs",
			Group:   GroupV1,
			Auth:    RequireUser,
			Limit:   LimitWrite,
			Handler: func(d Deps) http.Handler { return jobsHandler(d).Create() },
		},
		Route{
			Method:  http.MethodGet,
			Pattern: "/jobs/{id}",
			Group:   GroupV1,
			Auth:    RequireUser,
			Limit:   LimitRead,
			Handler: func(d Deps) http.Handler { return jobsHandler(d).Detail() },
		},
		Route{
			Method:  http.MethodPatch,
			Pattern: "/jobs/{id}",
			Group:   GroupV1,
			Auth:    RequireUser,
			Limit:   LimitWrite,
			Handler: func(d Deps) http.Handler { return jobsHandler(d).Update() },
		},
		Route{
			// A verb under the resource, because job status is not a settable field
			// (Docs/02 §2). A PATCH carrying `{"status": "cancelled"}` would be a client
			// naming a state; this is a client naming an intent.
			Method:  http.MethodPost,
			Pattern: "/jobs/{id}/cancel",
			Group:   GroupV1,
			Auth:    RequireUser,
			Limit:   LimitWrite,
			Handler: func(d Deps) http.Handler { return jobsHandler(d).Cancel() },
		},
		Route{
			// **The first four-segment `GET /v1/jobs/{id}/<literal>` in the service**
			// (SHIP-65a), and the endpoint SHIP-83a existed to make registrable.
			//
			// While `GET /v1/jobs/open/{id}` existed, this pattern and that one both
			// matched `/v1/jobs/open/history` with neither more specific, and Go's
			// ServeMux panics at registration rather than answering a 404 somebody
			// debugs — `make run` died at startup. Registering the intersection did not
			// help. Four tickets took workarounds for it (SHIP-115, SHIP-115a,
			// SHIP-101a, SHIP-102a) and `Docs/09`'s row for this one declared the
			// blocker instead. SHIP-83a moved the feed to `GET /v1/fleet/jobs/{id}` and
			// proved the slot free by registering a probe; this is the first ticket to
			// spend what that bought.
			//
			// **`GET /v1/jobs/open` now answers 400 rather than 404**, because "open"
			// lands in the identifier slot of the route above and fails UUID parsing.
			// That is the proof the slot is an identifier again rather than a defect.
			//
			// # RequireUser is not the access control here, and this is the first route
			// in this file where that distinction is live
			//
			// Every other route in this file is owner-only. This one serves two parties
			// — the customer who owns the job, and a provider holding a bid on it — and
			// which of the two a caller is comes from rows rather than from the role
			// claim on their token. Being neither answers exactly what a job that does
			// not exist answers, byte for byte.
			Method:  http.MethodGet,
			Pattern: "/jobs/{id}/history",
			Group:   GroupV1,
			Auth:    RequireUser,
			Limit:   LimitRead,
			Handler: func(d Deps) http.Handler { return jobsHandler(d).History() },
		},
		Route{
			// Also a verb under the resource, but for a different reason from cancel's.
			// The deadline is an ordinary column, so a PATCH would work — and would be
			// wrong, because the new deadline is the platform's to compute (Docs/02 §6.3).
			// A client that could write the field could keep a listing alive for a decade.
			Method:  http.MethodPost,
			Pattern: "/jobs/{id}/extend",
			Group:   GroupV1,
			Auth:    RequireUser,
			Limit:   LimitWrite,
			Handler: func(d Deps) http.Handler { return jobsHandler(d).Extend() },
		},
	)
}

// jobsHandler builds the domain's handler from what Deps already carries.
//
// Nothing jobs needs is missing from Deps, which is the test of whether a domain has been written
// the way Docs/10 §9.2 asks: the outbox writer, the clock and the geocoder are all pure functions
// of the pool, the clock and the configuration, so no field had to be added to a shared struct.
//
// It panics for the same reason identityHandler does: it runs during attach, at startup, and
// every failure it can report is a wiring mistake that will still be there after a restart. The
// pool is deliberately not checked — it may be nil because the database was unreachable at
// startup, which is a transient condition the service is built to survive, and the handlers
// answer 503 for as long as it lasts.
func jobsHandler(d Deps) *jobs.Handler {
	svc := jobs.NewService(events.NewOutbox(), d.Clock, newGeocoder(d),
		jobs.WithBidders(jobBidders{}))

	handler, err := jobs.NewHandler(svc, d.Pool, d.Logger)
	if err != nil {
		panic("cmd/api: jobs handler: " + err.Error())
	}
	return handler
}

// newGeocoder picks the geocoding implementation for this environment (SHIP-59a, SHIP-60).
//
// The composition root doing the one thing only it can: jobs declares what it needs of a geocoder
// in its own ports.go and imports nothing from internal/platform, and the adapter knows nothing
// about jobs. Go satisfies the interface structurally, and the two meet here (Docs/06 §4.1).
//
// # Development uses the stub; everything else uses what is configured
//
// SHIP-15g added GEOCODING_BASE_URL and GEOCODING_API_KEY, which SHIP-60 needed and could not add
// from a domain branch. So this now builds the real provider when one is configured, and the
// unconfigured case is the only one that still ends in nil.
//
// Falling back to geocoding.Stub outside development remains refused, and the reason has not
// changed: the stub writes coordinates that are stable, plausible, inside Australia, and entirely
// fictional, and a fictional coordinate on a real job is far harder to notice than a missing one.
//
// A nil geocoder stores the address exactly as the customer typed it, with no coordinate. Every
// path through the domain copes, because SHIP-59a requires an unrecognised address not to fail the
// job — which is also why a provider that fails to build is a warning rather than a panic: an
// unbootable API in staging would be a regression in a deployment that works today, over a feature
// it does not yet have.
//
// The log line is deliberately at startup rather than per request: it is a fact about the
// deployment, and one line in the boot log is findable where one line per created job is noise.
func newGeocoder(d Deps) jobs.Geocoder {
	if geocoding.UseStub(d.Config.Env) {
		return geocoding.NewStub()
	}

	if d.Config.Geocoding.ProviderBaseURL == "" {
		d.Logger.Warn("no geocoding provider is configured; job addresses will be stored unresolved",
			"env", string(d.Config.Env),
			"needs", "GEOCODING_BASE_URL in deploy/.env")
		return nil
	}

	provider, err := geocoding.NewProvider(geocoding.Options{
		BaseURL: d.Config.Geocoding.ProviderBaseURL,
		APIKey:  d.Config.Geocoding.ProviderAPIKey,
	})
	if err != nil {
		d.Logger.Warn("the geocoding provider could not be built; job addresses will be stored unresolved",
			"env", string(d.Config.Env), "error", err.Error())
		return nil
	}

	d.Logger.Info("geocoding provider configured", "base_url", d.Config.Geocoding.ProviderBaseURL)
	return provider
}

// jobBidders implements jobs.Bidders by asking whether this provider has ever offered on this job
// (SHIP-65a).
//
// # Why this query is here and not in a domain
//
// It belongs to `bidding`, and `jobs` may not import it. The composition root is where a dependency
// between two domains is visible to anybody reading how the service is wired, rather than buried in
// `jobs/postgres.go` where a `SELECT … FROM bids` would read as a table `jobs` owns. `acceptedBids`
// in routes_delivery.go is the same arrangement for the same reason and carries the fuller argument;
// this is the second instance of it and the first outside `delivery`.
//
// **When `bidding` grows a reader for this, the type is deleted and a method on its store passed
// instead.** Nothing in `jobs` changes, which is the point of the port.
//
// # Any bid, at any status, and the absence of a status filter is the decision
//
// `EXISTS` over `(job_id, provider_id)` with no predicate on `bids.status`. jobs.Bidders argues why
// at length: a losing or withdrawn bidder has a legitimate interest in what became of the job they
// priced, and filtering would put `bidding`'s eight-value enumeration into a statement neither
// domain owns — a second copy of another domain's vocabulary, in a file that cannot see it change
// (Docs/10 §3.4).
//
// idx_bids_job leads on job_id, so this is an index scan over the bids on one job with a filter,
// and the planner stops at the first match.
type jobBidders struct{}

// HasBidOn reports whether providerID has any bid on jobID.
//
// No lock and no transaction: one read, and it is a question about the past. A bid placed in the
// instant after this returns would make the answer "yes" a moment later, which is a provider
// gaining access to a history they are about to be entitled to rather than a race worth serialising.
//
// A job that does not exist answers false, exactly as a job nobody bid on does. The caller has
// already established that the job exists — jobs.Service.HistoryFor reads the row before it asks
// this — so the two cases cannot be confused here.
func (jobBidders) HasBidOn(
	ctx context.Context,
	r db.Runner,
	jobID, providerID uuid.UUID,
) (bool, error) {
	const q = `SELECT EXISTS (SELECT 1 FROM bids WHERE job_id = $1 AND provider_id = $2)`

	var held bool
	if err := r.QueryRow(ctx, q, jobID, providerID).Scan(&held); err != nil {
		return false, fmt.Errorf("cmd/api: reading whether %s has bid on %s: %w",
			providerID, jobID, err)
	}
	return held, nil
}

// Compile-time proof that the implementations of these ports satisfy them, which is the only place
// in the build where that can be established — the domain does not name the adapter and the
// adapter does not name the domain, so nothing else links them.
var (
	_ jobs.Geocoder = (*geocoding.Stub)(nil)
	_ jobs.Geocoder = (*geocoding.Provider)(nil)
	_ jobs.Bidders  = jobBidders{}
)
