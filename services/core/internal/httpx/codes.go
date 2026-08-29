package httpx

import (
	"fmt"
	"sort"
	"sync"
)

// The error code registry (SHIP-15c).
//
// Docs/10 §4.4 has described this mechanism since SHIP-15a and Docs/11 §3 listed it as built.
// Neither was true: internal/httpx had the protocol codes as plain constants, there was no
// registration function, and the generated document §4.4 says clients branch on had never been
// written. This is that mechanism, made real before the first domain needs it.
//
// # Why a registry rather than one file of constants
//
// A single shared list of every code in the platform is a file that every domain edits, which is
// the collision Docs/10 §9.2 exists to prevent — the same argument as the route manifest and the
// per-domain contract fragments. So a domain declares its own codes in its own package:
//
//	var CodeProhibitedCategory = httpx.RegisterCode(
//	    "jobs_prohibited_category", "The goods category may not be published.")
//
// and edits nothing shared. What stops two domains choosing the same string is a test in cmd/api,
// which is the one package that links every domain together and is therefore the only place the
// whole registry exists at once.
//
// # Why the description is required
//
// Docs/10-api-error-codes.md is generated from this registry, and it is what three client
// codebases branch on. A code with no description is a string a Flutter developer has to guess
// the meaning of from its name, which is how two clients end up handling the same code
// differently.

// CodeInfo is one registered code and what it means to a client.
type CodeInfo struct {
	// Code is the wire value. Clients branch on this and on nothing else in the body.
	Code Code

	// Description says when the code is returned, in the voice of the API documentation
	// rather than of the handler that raises it.
	Description string

	// Protocol marks the codes owned by internal/httpx — the ones any endpoint can return
	// because they come from the transport, the middleware, or net/http itself, rather than
	// from a domain rule. A client handles all of them once, centrally; the rest are handled
	// where the call is made.
	Protocol bool
}

var (
	codesMu sync.Mutex
	codes   = map[Code]CodeInfo{}
)

// RegisterCode declares a domain-specific error code and returns it.
//
// It is meant to be called from a package-level var, so it panics rather than returning an
// error: a duplicate or malformed code is a programming mistake, and the alternative to stopping
// the process at startup is a service where two domains quietly mean different things by one
// string that clients cannot tell apart.
//
// Codes are lower_snake_case and named <domain>_<condition> — `jobs_prohibited_category`,
// `bidding_bid_withdrawn`. They are part of the API contract in the same way a field name is: stable once
// published, because a store build already on a device is branching on them and Flutter has no
// over-the-air path to fix it (Docs/06 §5.3).
func RegisterCode(code Code, description string) Code {
	return registerCode(code, description, false)
}

// registerProtocolCode declares one of the codes internal/httpx owns. Unexported, because a
// domain adding to the protocol set has misread what the set is.
func registerProtocolCode(code Code, description string) Code {
	return registerCode(code, description, true)
}

func registerCode(code Code, description string, protocol bool) Code {
	if err := validCode(code); err != nil {
		panic(fmt.Sprintf("httpx: %v", err))
	}
	if description == "" {
		panic(fmt.Sprintf("httpx: error code %q has no description, and the generated "+
			"Docs/10-api-error-codes.md is what three clients read to find out what it means", code))
	}

	codesMu.Lock()
	defer codesMu.Unlock()

	if existing, taken := codes[code]; taken {
		panic(fmt.Sprintf("httpx: the error code %q is already registered (%q). "+
			"Codes are named <domain>_<condition> precisely so two domains cannot collide; "+
			"pick one that names the domain raising it", code, existing.Description))
	}

	codes[code] = CodeInfo{Code: code, Description: description, Protocol: protocol}
	return code
}

// RegisteredCodes returns every code registered in this binary, sorted by code.
//
// What it returns depends on which packages are linked in, which is the point: cmd/api imports
// every domain, so the list there is the whole platform's surface, and that is where both the
// uniqueness test and the generated document live.
func RegisteredCodes() []CodeInfo {
	codesMu.Lock()
	defer codesMu.Unlock()

	out := make([]CodeInfo, 0, len(codes))
	for _, info := range codes {
		out = append(out, info)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// validCode enforces the shape Docs/10 §4.4 specifies.
//
// Checked rather than trusted because the cost of publishing a badly shaped code is that it can
// never be tidied: a client is branching on the exact string, so `validationFailed` stays
// `validationFailed` for as long as that build exists.
func validCode(code Code) error {
	if code == "" {
		return fmt.Errorf("an error code cannot be empty")
	}
	if code[0] == '_' || code[len(code)-1] == '_' {
		return fmt.Errorf("the error code %q starts or ends with an underscore", code)
	}

	for i := 0; i < len(code); i++ {
		c := code[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '_':
			if i > 0 && code[i-1] == '_' {
				return fmt.Errorf("the error code %q has a doubled underscore", code)
			}
		default:
			return fmt.Errorf("the error code %q is not lower_snake_case; codes are named "+
				"<domain>_<condition> in lower snake case (Docs/10 §4.4)", code)
		}
	}
	return nil
}

// The fifteen protocol-level codes, registered here so that the generated document describes the
// whole surface rather than only the domain half.
//
// A client handles these once, centrally: any endpoint can return any of them, because they come
// from the transport, the middleware, or net/http's own ServeMux rather than from a business
// rule. The constants themselves stay in errors.go and idempotency.go beside the code that
// raises them — TestEveryProtocolCodeIsRegistered keeps the two from drifting apart.
func init() {
	registerProtocolCode(CodeBadRequest,
		"The request could not be understood — unparseable JSON, a missing header, or a path parameter of the wrong shape.")
	registerProtocolCode(CodeValidationFailed,
		"The request was well formed and the platform rejected it. Carries `details`, one entry per offending field.")
	registerProtocolCode(CodeUnauthenticated,
		"No usable credential was presented. Distinct from `forbidden` so a client knows to refresh a token rather than give up.")
	registerProtocolCode(CodeForbidden,
		"The caller is known and is not permitted. Used where the caller may already know the resource exists.")
	registerProtocolCode(CodeNotFound,
		"No such resource — or none the caller has any business knowing about. The two are deliberately indistinguishable.")
	registerProtocolCode(CodeMethodNotAllowed,
		"The path exists but does not serve this method.")
	registerProtocolCode(CodeConflict,
		"Valid, but it contradicts the current state — a bid on a job that has just been awarded, a second award on one job.")
	registerProtocolCode(CodeUnsupportedMediaType,
		"This endpoint accepts application/json.")
	registerProtocolCode(CodePayloadTooLarge,
		"The request body exceeds the limit. Images go directly to object storage by pre-signed URL and never through the API.")
	registerProtocolCode(CodeRateLimited,
		"Too many requests. Carries a Retry-After header wherever one can be given honestly.")
	registerProtocolCode(CodeInternal,
		"A failure the caller cannot act on. Carries nothing beyond the request ID, which is what makes it diagnosable.")
	registerProtocolCode(CodeUnavailable,
		"A dependency this request needs is not answering. Unlike `internal_error` it means try again.")

	registerProtocolCode(CodeIdempotencyKeyRequired,
		"A state-changing request arrived with no Idempotency-Key header. Generate one value per action and reuse it for every retry of that action.")
	registerProtocolCode(CodeIdempotencyKeyReused,
		"The key has already been used for a different request. Replaying the first response would report success for something the client never sent.")
	registerProtocolCode(CodeIdempotencyInProgress,
		"The original request with this key is still being handled. Retrying is correct, and is already what the client was doing.")
}
