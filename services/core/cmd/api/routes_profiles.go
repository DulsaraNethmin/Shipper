package main

import (
	"net/http"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/platform/storage"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/profiles"
)

// The profiles domain's routes (SHIP-81a onwards).
//
// This file exists so that adding a domain adds a file and edits none. `cmd/api/routes.go`,
// `manifest.go` and `main.go` are shared surfaces (Docs/10 §9.2); a route registration that had to go
// into one of them is a line every concurrent branch also touches, and a badly resolved conflict
// there drops an endpoint with no compile error and no failing test.
//
// # Why the prefix is `/provider` rather than `/profiles` or `/fleet`
//
// `/fleet` established the pattern that a first segment can name the *caller's role* rather than a
// resource, and `/driver` (SHIP-108) is the second instance of it. This is the third. A verification
// record is not a collection anybody browses — there is exactly one per caller and no identifier for
// it — so `/v1/provider/verification` reads as what it is, and a `/profiles` prefix would name the
// package rather than anything a client can see.
//
// It is deliberately not under `/fleet`. That prefix is the provider's *fleet* — vehicles, service
// area, the jobs they may bid on — and `internal/fleet` answers all of it. Verification is a
// different domain's record, and putting it there would suggest the eligibility answer lives in the
// same place as the record it reads, which is precisely the confusion SHIP-81a exists to remove.
//
// # There is no route to the decision, and that is not an omission
//
// `profiles.Service.Decide` is exported and has no endpoint here. Deciding somebody's verification
// standing is an administrator's act on the administrator credential (SHIP-147), so SHIP-153's queue
// and SHIP-154's decision are `/v1/admin` routes served by `internal/admin` — which will call this
// transition through a port it declares for itself. A route here would be a second way to reach the
// same act, on the wrong credential, and Docs/04 §9's least-privilege requirement is the reason not
// to build one before the reviewer exists.
func init() {
	register(
		Route{
			Method:  http.MethodGet,
			Pattern: "/provider/verification",
			Group:   GroupV1,
			Auth:    RequireUser,
			Limit:   LimitRead,
			Handler: func(d Deps) http.Handler { return profilesHandler(d).Verification() },
		},

		// SHIP-81b's three, and they are one path pair rather than three.
		//
		// `/documents` is the collection: a provider submits to it and reads it back. `/uploads`
		// under it is the credential-issuing step, and it is a sub-resource rather than a query
		// parameter or a second verb on the collection because what it answers with is not a
		// document — it is permission to create one, and nothing is written when it is called.
		// `delivery`'s `POST /v1/jobs/{id}/proof-uploads` is the same arrangement one level up.
		//
		// Four segments under `/v1` with no wildcard anywhere, so none of the `ServeMux`
		// registration hazards Docs/11 §3 records apply: there is no `{id}` slot for a literal to
		// collide with, and `/provider/verification` and `/provider/verification/documents` are
		// two distinct literal patterns.
		Route{
			Method:  http.MethodPost,
			Pattern: "/provider/verification/documents/uploads",
			Group:   GroupV1,
			Auth:    RequireUser,
			Limit:   LimitUpload,
			Handler: func(d Deps) http.Handler { return profilesHandler(d).PresignDocumentUpload() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/provider/verification/documents",
			Group:   GroupV1,
			Auth:    RequireUser,
			Limit:   LimitWrite,
			Handler: func(d Deps) http.Handler { return profilesHandler(d).SubmitDocument() },
		},
		Route{
			Method:  http.MethodGet,
			Pattern: "/provider/verification/documents",
			Group:   GroupV1,
			Auth:    RequireUser,
			Limit:   LimitRead,
			Handler: func(d Deps) http.Handler { return profilesHandler(d).ProviderDocuments() },
		},
	)
}

// profilesHandler builds the domain's handler from what Deps already carries.
//
// Nothing this domain needs is missing from Deps: the clock and the pool, which is the same pair
// `fleet` needs and the test Docs/10 §9.2 sets for whether a domain has been written the way the
// shared-surface rules ask — no field had to be added to a shared struct.
//
// It panics for the reason `fleetHandler` does: it runs during attach, at startup, and every failure
// it can report is a wiring mistake that will still be there after a restart. The pool is deliberately
// not checked — it may be nil because the database was unreachable at startup, which is a transient
// condition the service is built to survive.
func profilesHandler(d Deps) *profiles.Handler {
	handler, err := profiles.NewHandler(
		profiles.NewService(d.Clock), verificationDocuments(d), d.Pool, d.Logger)
	if err != nil {
		panic("cmd/api: profiles handler: " + err.Error())
	}
	return handler
}

// verificationDocuments builds the evidence service over the signer below (SHIP-81b, SHIP-155).
//
// # It is a function rather than a line inside profilesHandler, because there are two callers
//
// SHIP-155's administrator viewer constructs one of these from `cmd/api/routes_admin.go` — which is
// what `internal/profiles`' documents.go asked for in advance, so that `profiles.NewService` never
// widens to carry a signer the queue and the decision endpoints have no business holding. Two call
// sites building the policy out of `d.Config` separately is the drift Docs/06 §5.3 is written about:
// the accepted media types and the two lifetimes would be one configuration read twice, and the copy
// nobody updated would refuse a photograph the other half had just accepted.
//
// **Each caller gets its own value, and that is deliberate rather than wasteful.** It holds
// configuration and a signer and no connection, so a second is a struct rather than a resource —
// `profileDocuments` records the same argument for the `*storage.S3` underneath it. Sharing one
// would mean a field on `Deps` that two route files then both have to agree about.
//
// The same `*storage.S3` satisfies both ports, which is the pair the compile-time assertions at the
// foot of this file establish: `profiles` declares upload signing and object reading separately so
// that a caller reading somebody's licence cannot thereby mint permission to write one, and this is
// where one value is shown to satisfy both.
func verificationDocuments(d Deps) *profiles.Documents {
	store := profileDocuments(d)

	return profiles.NewDocuments(d.Clock, store, store, profiles.DocumentPolicy{
		MaxBytes:             d.Config.Storage.MaxUploadBytes,
		AcceptedContentTypes: d.Config.Storage.AcceptedContentTypes,
		UploadTTL:            d.Config.Storage.PresignTTL,
		DownloadTTL:          d.Config.Storage.DownloadTTL,
	})
}

// profileDocuments builds the signer behind the verification-document endpoints (SHIP-81b).
//
// # This is the only place the domain and the adapter meet, and neither names the other
//
// `profiles` declares [profiles.DocumentUploads] and [profiles.DocumentObjects] in its own ports.go
// and imports nothing from `internal/platform`; `storage` knows what a bucket is and has never heard
// of a verification record — its doc.go named this consumer in advance: "The interface this package
// satisfies is declared by the domain that needs a file stored — delivery for proof, **profiles for
// verification documents** — never here." Go satisfies the interfaces structurally, so the two are
// joined by the assignment above and by the compile-time assertion at the foot of this file, which
// is the only place in the build where that can be established at all.
//
// # It is a second signer over the same bucket, not a second bucket
//
// The same six configured values `proofUploads` reads, so a deployment has one object store, one
// credential and one endpoint. The two kinds of object are kept apart by their key prefix —
// `proof/…` and `verification/…` — which is what `internal/config`'s Storage section describes and
// what `delivery`'s `proofKeyPrefix` comment predicted. **Two `storage.S3` values rather than one
// shared between the domains** because `Deps` carries neither today and threading one through would
// be a field on a shared struct that two domains would then both have to agree about; the value
// holds configuration and no connection, so a second is a struct, not a resource.
//
// It panics for the reason `proofUploads` does: it runs during attach, from a Handler closure with
// nowhere to put an error, and every failure it can report is a configuration fault that will still
// be there after a restart.
func profileDocuments(d Deps) *storage.S3 {
	signer, err := storage.NewS3(storage.Options{
		Endpoint:        d.Config.Storage.Endpoint,
		Bucket:          d.Config.Storage.Bucket,
		Region:          d.Config.Storage.Region,
		AccessKeyID:     d.Config.Storage.AccessKeyID,
		SecretAccessKey: d.Config.Storage.SecretAccessKey,
		UsePathStyle:    d.Config.Storage.UsePathStyle,
		Clock:           d.Clock,
	})
	if err != nil {
		panic("cmd/api: verification document signer: " + err.Error())
	}
	return signer
}

// Compile-time proof that the adapter satisfies the ports `profiles` declared, which is the only
// place in the build where that can be established — `profiles` names none of these types and none
// of them names `profiles`, so nothing else links them.
//
// *storage.S3 appears twice on purpose. `profiles` declares upload signing and object reading as two
// ports — so that a caller reading somebody's licence cannot thereby mint permission to write one —
// and this is where one value is shown to satisfy both.
var (
	_ profiles.DocumentUploads = (*storage.S3)(nil)
	_ profiles.DocumentObjects = (*storage.S3)(nil)
)
