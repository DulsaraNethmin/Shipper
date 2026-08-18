package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// The published contract, checked against the running service (SHIP-17a).
//
// Docs/10 §4.1 names three tests that guard the route surface. Two of them already exist in
// routes_test.go and manifest_test.go; TestEveryRouteIsInTheContract is the third, and it was
// the only one still unenforceable, because there was no contract to check against.
//
// # Why a contract needs a test at all
//
// A contract nobody verifies drifts from the service within a wave, and then it is worse than
// having none — three client codebases believe it. Docs/10 §8.1 has the Flutter client
// generated from or validated against this document, so a field renamed in Go and not in YAML
// produces a client that compiles, ships, and fails on a device.
//
// So the checks run in both directions: the manifest against the contract, and the contract
// against what the handlers actually put on the wire.

// contractPath is the published document, relative to this package.
const contractPath = "../../../../contracts/openapi.yaml"

// serverBase is one of the servers the contract declares. Requests are built against it so
// that kin-openapi's router can strip the server prefix and match the path.
const serverBase = "http://localhost:8080"

// loadContract parses and validates the document, following the $refs into paths/ and
// components/.
//
// Validate is the half that catches a broken fragment: a $ref that does not resolve, a schema
// that names a type it does not have, a response with no description. Those are mistakes a
// domain makes while adding its own fragment, and this is where they should surface.
func loadContract(t *testing.T) *openapi3.T {
	t.Helper()

	loader := &openapi3.Loader{
		Context: context.Background(),
		// The document is assembled from per-domain fragments (Docs/10 §8.1), so external
		// references are the mechanism rather than an exception.
		IsExternalRefsAllowed: true,
	}

	abs, err := filepath.Abs(contractPath)
	if err != nil {
		t.Fatalf("resolving %s: %v", contractPath, err)
	}

	doc, err := loader.LoadFromFile(abs)
	if err != nil {
		t.Fatalf("loading the contract: %v", err)
	}
	if err := doc.Validate(loader.Context); err != nil {
		t.Fatalf("the contract is not a valid OpenAPI document: %v", err)
	}
	return doc
}

// contractPathFor translates a Go route pattern into the path the contract states.
//
// net/http's `{$}` is an exact-match marker, not a path segment: the route registered as
// `/v1/{$}` is served at `/v1/`. Path parameters need no translation, because Go 1.22's
// `{id}` and OpenAPI's `{id}` happen to be written the same way — which is convenient but not
// guaranteed, so the translation lives in one function rather than being assumed at each call
// site.
func contractPathFor(r Route) string {
	return strings.TrimSuffix(r.fullPath(), "{$}")
}

// TestEveryRouteIsInTheContract cross-checks the manifest against the contract, both ways.
//
// Docs/10 §4.1. A served route with no contract entry is an endpoint three clients cannot
// discover; a contract entry with no route is a promise the service does not keep, which is
// worse, because a client written against it fails at runtime rather than at compile time.
func TestEveryRouteIsInTheContract(t *testing.T) {
	doc := loadContract(t)

	// What the contract describes: path -> set of methods.
	documented := map[string]map[string]bool{}
	for path, item := range doc.Paths.Map() {
		methods := map[string]bool{}
		for method := range item.Operations() {
			methods[method] = true
		}
		documented[path] = methods
	}

	// Every served route must be documented.
	served := map[string]map[string]bool{}
	for _, r := range routes() {
		path := contractPathFor(r)
		if served[path] == nil {
			served[path] = map[string]bool{}
		}
		served[path][r.Method] = true

		methods, ok := documented[path]
		if !ok {
			t.Errorf("route %s %s is served but the contract does not describe %s\n"+
				"  add it to a fragment under contracts/paths/ and $ref it from contracts/openapi.yaml",
				r.Method, r.fullPath(), path)
			continue
		}
		if !methods[r.Method] {
			t.Errorf("route %s %s is served but the contract describes %s without %s",
				r.Method, r.fullPath(), path, r.Method)
		}
	}

	// Every documented operation must be served.
	for path, methods := range documented {
		for method := range methods {
			if !served[path][method] {
				t.Errorf("the contract describes %s %s but no route serves it\n"+
					"  either the route was dropped in a merge, or the contract entry is ahead of the code",
					method, path)
			}
		}
	}
}

// contractRoot is the directory holding the assembled contract, relative to this package.
const contractRoot = "../../../../contracts"

// pathsBlockEntry matches one key of the root document's `paths:` block, in file order:
//
//	/v1/app/minimum-version:
var pathsBlockEntry = regexp.MustCompile(`(?m)^  (/[^\s:]*):\s*$`)

// fragmentRef matches a reference into a fragment: $ref: './paths/identity.yaml#/Register'
var fragmentRef = regexp.MustCompile(`\$ref:\s*'\./paths/([^#']+)#`)

// TestPathsBlockIsSortedAndComplete guards the one thing the both-directions check cannot see
// (SHIP-15c).
//
// TestEveryRouteIsInTheContract compares the manifest with the contract, which catches a route
// dropped from one of them. It cannot catch a merge that drops **both** a route and its `$ref`,
// because it then compares two things that were truncated together and finds them in perfect
// agreement. That is not a hypothetical shape: a badly resolved conflict in wave 2 takes one
// side of a hunk that spans `routes_<domain>.go` and this file's `paths:` block at once.
//
// So this test checks the contract against the filesystem instead, which no merge resolution
// touches: every fragment under contracts/paths/ must be referenced from the root. A fragment
// that has become unreachable is a domain's whole surface silently gone.
//
// The sortedness half is what makes the merge recipe above the block workable. "Take both sides
// and re-sort" is only checkable if sorted is the normal state.
func TestPathsBlockIsSortedAndComplete(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(contractRoot, "openapi.yaml"))
	if err != nil {
		t.Fatalf("reading the contract: %v", err)
	}

	// Only the paths: block — components: has two-space keys of its own.
	body := string(source)
	start := strings.Index(body, "\npaths:\n")
	if start < 0 {
		t.Fatal("the contract has no paths: block")
	}
	block := body[start:]
	if end := strings.Index(block, "\ncomponents:\n"); end >= 0 {
		block = block[:end]
	}

	var declared []string
	for _, m := range pathsBlockEntry.FindAllStringSubmatch(block, -1) {
		declared = append(declared, m[1])
	}
	if len(declared) == 0 {
		t.Fatal("found no entries in the paths: block; the pattern has stopped matching")
	}

	if !sort.StringsAreSorted(declared) {
		sorted := append([]string(nil), declared...)
		sort.Strings(sorted)
		t.Errorf("the paths: block is not sorted by path.\n  got:  %v\n  want: %v\n"+
			"Sorted order is what makes the merge recipe above the block work: a conflict is\n"+
			"resolved by taking both sides and re-sorting, never by choosing one.", declared, sorted)
	}

	seen := map[string]bool{}
	for _, p := range declared {
		if seen[p] {
			t.Errorf("the path %q appears twice in the paths: block — a conflict resolved by "+
				"taking both sides without re-sorting", p)
		}
		seen[p] = true
	}

	// Every fragment on disk must be reachable from the root.
	referenced := map[string]bool{}
	for _, m := range fragmentRef.FindAllStringSubmatch(body, -1) {
		referenced[m[1]] = true
	}

	entries, err := os.ReadDir(filepath.Join(contractRoot, "paths"))
	if err != nil {
		t.Fatalf("reading contracts/paths: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		if !referenced[e.Name()] {
			t.Errorf("contracts/paths/%s is not referenced from contracts/openapi.yaml.\n"+
				"  Either its $ref was lost in a merge — which removes a domain's whole surface\n"+
				"  from the contract with no compile error — or the fragment is unused and should go.",
				e.Name())
		}
	}
}

// TestErrorResponsesMatchTheContract checks the failures, which are the responses a client is
// most likely to mishandle and the least likely to be exercised by a happy-path test.
//
// The 404 and 405 here are written by net/http's own ServeMux, normalised into the error
// contract by httpx.StandardErrors (SHIP-12). That normalisation is the thing being checked:
// it is easy to believe it covers every failure and to find later that ServeMux's plain-text
// 404 reached a client that was parsing JSON.
func TestErrorResponsesMatchTheContract(t *testing.T) {
	doc := loadContract(t)

	errorSchema, ok := doc.Components.Schemas["Error"]
	if !ok {
		t.Fatal("the contract has no Error schema")
	}

	handler := testRouter()

	cases := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCode   string
	}{
		{
			name:       "unknown path under the version group",
			method:     http.MethodGet,
			path:       "/v1/no-such-endpoint",
			wantStatus: http.StatusNotFound,
			wantCode:   "not_found",
		},
		{
			// OPTIONS rather than DELETE, and the reason is worth recording. The
			// idempotency middleware wraps the whole v1 group, so a *mutating* method on
			// a path that does not serve it is refused for the missing key before
			// ServeMux ever gets to say the method is wrong — see the case below.
			// OPTIONS is read-only by both definitions, so it passes through untouched
			// and produces the genuine 405.
			name:       "known path, wrong method",
			method:     http.MethodOptions,
			path:       "/v1/app/minimum-version",
			wantStatus: http.StatusMethodNotAllowed,
			wantCode:   "method_not_allowed",
		},
		{
			// Client-visible and surprising enough to pin down: within /v1, the
			// idempotency requirement is checked before routing. A client that sends
			// DELETE to a read-only endpoint is told it is missing a key, not that the
			// method is wrong. That is the middleware ordering in newRouter working as
			// designed (Docs/10 §4.2), and it is exactly the sort of behaviour that gets
			// "tidied" into a 405 by someone who has not read why.
			name:       "mutating method is refused for the key before it is routed",
			method:     http.MethodDelete,
			path:       "/v1/app/minimum-version",
			wantStatus: http.StatusBadRequest,
			wantCode:   "idempotency_key_required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(tc.method, serverBase+tc.path, nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body)
			}
			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
				t.Fatalf("Content-Type = %q, want JSON — ServeMux's plain-text failure reached the client", ct)
			}

			var body any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("response is not JSON: %v (%s)", err, rec.Body)
			}

			if err := errorSchema.Value.VisitJSON(body); err != nil {
				t.Errorf("the error body does not match the published Error schema: %v\n  body: %s", err, rec.Body)
			}

			if got := errorCodeOf(body); got != tc.wantCode {
				t.Errorf("error.code = %q, want %q — clients branch on this", got, tc.wantCode)
			}
		})
	}
}

// errorCodeOf digs error.code out of a decoded body, returning "" when the shape is wrong —
// the schema check above is what reports that properly.
func errorCodeOf(body any) string {
	envelope, ok := body.(map[string]any)
	if !ok {
		return ""
	}
	inner, ok := envelope["error"].(map[string]any)
	if !ok {
		return ""
	}
	code, _ := inner["code"].(string)
	return code
}

// validateAgainstContract checks one recorded response against the operation the contract
// declares for it.
func validateAgainstContract(t *testing.T, router routers.Router, req *http.Request, rec *httptest.ResponseRecorder) {
	t.Helper()

	route, pathParams, err := router.FindRoute(req)
	if err != nil {
		t.Fatalf("the contract has no operation for %s %s: %v", req.Method, req.URL.Path, err)
	}

	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    req,
			PathParams: pathParams,
			Route:      route,
		},
		Status: rec.Code,
		Header: rec.Header(),
		Body:   io.NopCloser(bytes.NewReader(rec.Body.Bytes())),
		Options: &openapi3filter.Options{
			// The response body is the point of this test; an operation that declares a
			// status with no schema should fail rather than pass vacuously.
			IncludeResponseStatus: true,
		},
	}

	if err := openapi3filter.ValidateResponse(req.Context(), input); err != nil {
		t.Errorf("%s %s (status %d) does not match the contract: %v\n  body: %s",
			req.Method, req.URL.Path, rec.Code, err, rec.Body)
	}
}

// TestDocumentExpiryIsInTheContract pins the one pair that has already drifted (SHIP-159).
//
// # Why this is written by hand when TestResponsesMatchTheContract exists
//
// That test is SHIP-17a's criterion and it does the real thing — it drives the router and validates
// the bytes. **When this was written it reached 4 of the 86 routes**, because its exercisable()
// filter skipped every non-GET, every authenticated route and every parameterised path. It named
// what it skipped in its own log output every run rather than narrowing silently, which was the
// right behaviour and is also why nobody noticed that SHIP-159 added expires_at to the Go structs
// and to admin.yaml and not to this fragment. The service accepted a field the contract forbade,
// and answered with one, for a wave.
//
// **SHIP-17b closed that hole**: exercisable() is gone, contract_surface_test.go drives the routes
// it used to skip from a seeded world, and TestEveryRouteIsDrivenOrNamed fails the build on a route
// that is neither driven nor written down. This test is kept rather than folded into that one
// because it is the **regression pin** for the pair that actually drifted, and because it checks
// something the surface sweep does not: a body the contract must *refuse*.
//
// **The bodies are literals rather than marshalled structs, and that is deliberate rather than a
// shortcut.** profiles.documentSubmission and profiles.documentResponse are unexported, so no test
// in this package can reflect over them — and reflecting over them would be the wrong instrument
// anyway, because the contract is a statement about bytes rather than about Go types.
//
// # The rejection cases are the part that matters
//
// A schema check that only feeds it valid bodies passes just as well when validation is switched off
// entirely. Each half therefore asserts a body the contract must **refuse**, and an undeclared field
// is the one that proves additionalProperties: false is being enforced rather than merely written.
func TestDocumentExpiryIsInTheContract(t *testing.T) {
	doc := loadContract(t)

	router, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("building a router from the contract: %v", err)
	}

	const path = "/v1/provider/verification/documents"

	newRequest := func(t *testing.T, body string) (*http.Request, *routers.Route, map[string]string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, serverBase+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		// No credential: NoopAuthenticationFunc below means the security requirement is not
		// evaluated here, and a header nothing reads would only suggest it was.
		req.Header.Set(httpx.HeaderIdempotencyKey, "0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e1")

		route, pathParams, err := router.FindRoute(req)
		if err != nil {
			t.Fatalf("the contract has no operation for POST %s: %v", path, err)
		}
		return req, route, pathParams
	}

	validateRequest := func(t *testing.T, body string) error {
		t.Helper()
		req, route, pathParams := newRequest(t, body)
		return openapi3filter.ValidateRequest(req.Context(), &openapi3filter.RequestValidationInput{
			Request:    req,
			PathParams: pathParams,
			Route:      route,
			Options: &openapi3filter.Options{
				// The credential is SHIP-44's business and is checked by the service. What is
				// under test here is the shape of the body.
				AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
			},
		})
	}

	validateResponse := func(t *testing.T, body string) error {
		t.Helper()
		req, route, pathParams := newRequest(t, `{"kind":"licence","object_key":"verification/a/b"}`)
		header := http.Header{}
		header.Set("Content-Type", "application/json")
		return openapi3filter.ValidateResponse(req.Context(), &openapi3filter.ResponseValidationInput{
			RequestValidationInput: &openapi3filter.RequestValidationInput{
				Request:    req,
				PathParams: pathParams,
				Route:      route,
				Options: &openapi3filter.Options{
					AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
				},
			},
			Status: http.StatusCreated,
			Header: header,
			Body:   io.NopCloser(strings.NewReader(body)),
			Options: &openapi3filter.Options{
				IncludeResponseStatus: true,
			},
		})
	}

	// A recorded submission, as documentResponse writes it: no download URL on the 201, and
	// expires_at present because the Go field carries no omitempty (internal/profiles/http.go).
	const recorded = `{
		"id": "0198f2c1-8b10-7c33-9e52-4f9c6d73ba05",
		"kind": "insurance",
		"content_type": "image/jpeg",
		"content_length": 1874233,
		"submitted_at": "2026-08-18T04:15:00.000Z",
		"expires_at": %s
	}`

	t.Run("request", func(t *testing.T) {
		for _, c := range []struct {
			name   string
			body   string
			accept bool
		}{
			{
				name:   "carrying an expiry, which is what SHIP-159 added and the contract forbade",
				body:   `{"kind":"insurance","object_key":"verification/a/b","expires_at":"2027-07-01T00:00:00Z"}`,
				accept: true,
			},
			{
				// Optional because X-4 has not settled which documents must be renewed. An ABN
				// extract does not lapse at all.
				name:   "omitting the expiry, which stays legal",
				body:   `{"kind":"abn_evidence","object_key":"verification/a/b"}`,
				accept: true,
			},
			{
				name:   "naming a field the contract does not declare",
				body:   `{"kind":"licence","object_key":"verification/a/b","issued_at":"2020-01-01T00:00:00Z"}`,
				accept: false,
			},
		} {
			t.Run(c.name, func(t *testing.T) {
				err := validateRequest(t, c.body)
				switch {
				case c.accept && err != nil:
					t.Errorf("the contract refuses a body the service accepts: %v\n  body: %s", err, c.body)
				case !c.accept && err == nil:
					t.Errorf("the contract accepts a body it should refuse — additionalProperties: "+
						"false is not being enforced, so every case above passes vacuously.\n  body: %s", c.body)
				}
			})
		}
	})

	t.Run("response", func(t *testing.T) {
		for _, c := range []struct {
			name   string
			body   string
			accept bool
		}{
			{
				name:   "stating an expiry",
				body:   fmt.Sprintf(recorded, `"2027-07-01T00:00:00Z"`),
				accept: true,
			},
			{
				// null is a distinct answer: the platform was never told. It is not the same as
				// the field being absent, which is why the schema is nullable and required.
				name:   "stating that it was never told",
				body:   fmt.Sprintf(recorded, `null`),
				accept: true,
			},
			{
				name: "omitting the field the handler always writes",
				body: `{"id":"0198f2c1-8b10-7c33-9e52-4f9c6d73ba05","kind":"insurance",` +
					`"content_type":"image/jpeg","content_length":1874233,` +
					`"submitted_at":"2026-08-18T04:15:00.000Z"}`,
				accept: false,
			},
		} {
			t.Run(c.name, func(t *testing.T) {
				err := validateResponse(t, c.body)
				switch {
				case c.accept && err != nil:
					t.Errorf("the contract refuses a response the service sends: %v\n  body: %s", err, c.body)
				case !c.accept && err == nil:
					t.Errorf("the contract accepts a response it should refuse.\n  body: %s", c.body)
				}
			})
		}
	})
}
