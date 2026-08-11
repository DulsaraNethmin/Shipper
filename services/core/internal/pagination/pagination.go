// Package pagination is the cursor half of every list endpoint (Docs/10 §4.5).
//
// It was registered in internal/boundaries as infrastructure long before anything needed it,
// together with `ratelimit` and `money`, so that the track that first wanted a cursor could write
// the package and edit no shared file. SHIP-66 is that track — the customer's own job list is the
// first collection the API returns.
//
// # Keyset, never offset
//
// Docs/10 §4.5 settles this and gives the reason: offset duplicates and skips rows when the
// underlying set changes between pages. On the provider feed (SHIP-82), where jobs arrive
// continuously, that is a reportable bug rather than a theoretical one — a provider pages past a
// job they never saw. A keyset cursor names the last row of the previous page, so a page is
// "everything after that row" and stays correct whatever happened in between.
//
// # What this package does and does not decide
//
// It owns the *encoding*: what a cursor looks like on the wire, how a malformed one is refused,
// and the bounds on a page size. It does not know what is being paginated, what the sort key is,
// or how the query is written — a [Cursor] is a list of opaque strings and the domain decides what
// they mean. That is what lets `jobs` order by (created_at, id) and a later domain order by
// something else, with one implementation of the encoding between them.
//
// # The cursor is opaque, and that is a contract rather than an accident
//
// It is base64url of a versioned, separator-joined record — readable by anyone who wants to look,
// and deliberately not documented as such. Clients pass back what they were given. The version
// prefix is what makes the shape changeable later: a stored cursor from an older release decodes
// to a clear refusal rather than to plausible nonsense.
package pagination

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// The page sizes of Docs/10 §4.5.
//
// **That section says both come from configuration, and neither does yet.** internal/config has no
// fields for them, and adding two is a shared-surface change SHIP-66 could not make from a domain
// branch — the same position SHIP-60 reached over GEOCODING_*, recorded the same way in Docs/11 §3.
// They are here rather than in a domain because every list endpoint needs the same answer, and two
// domains with two defaults is exactly the divergence this package exists to prevent. Whoever next
// owns internal/config moves them; no caller changes, because callers ask [Limit].
const (
	DefaultLimit = 20
	MaxLimit     = 100
)

// cursorVersion prefixes every encoded cursor.
//
// One byte, so that the encoding can change without a client's stored cursor decoding to something
// that looks valid. A cursor is held across an app restart and across a deployment; the failure
// worth preventing is not a rejected cursor but an accepted one that means something else now.
const cursorVersion = "1"

// The field separator inside a cursor.
//
// ASCII unit separator, which cannot appear in a timestamp, a UUID or any other key field an
// ordering is built from — so a field containing the separator is a programming mistake rather
// than a value that has to be escaped, and [Cursor.Encode] says so.
const cursorSeparator = "\x1f"

// Cursor is a position in a list: the ordering key of the last row of a page.
//
// One entry per column in the ORDER BY, in the same order. Both are usually needed: a list ordered
// by a timestamp alone has ties, and a cursor that cannot break them repeats or skips a row at
// exactly the page boundary — which is the failure keyset pagination exists to avoid, arriving by
// a different route.
type Cursor []string

// Encode renders the cursor for a client to hand back.
//
// The empty cursor encodes to the empty string rather than to a valid-looking token, so "the first
// page" and "the page after nothing" are the same request.
//
// It panics if a field contains the separator. That cannot happen for a timestamp or a UUID, and a
// caller that has found a way to make it happen has built a cursor that will decode into the wrong
// number of fields — silently, and only for the rows whose key contains the byte.
func (c Cursor) Encode() string {
	if len(c) == 0 {
		return ""
	}
	for _, field := range c {
		if strings.Contains(field, cursorSeparator) {
			panic("pagination: a cursor field contains the field separator; it would decode " +
				"into the wrong number of fields")
		}
	}
	return base64.RawURLEncoding.EncodeToString(
		[]byte(cursorVersion + cursorSeparator + strings.Join(c, cursorSeparator)))
}

// Decode reads a cursor a client handed back, insisting on exactly fields entries.
//
// The count is an argument rather than something the caller checks afterwards, because the
// alternative is every list endpoint indexing into a slice whose length came from a client. A
// truncated cursor is refused here; it is not a panic three frames further in.
//
// The empty string decodes to the empty cursor with no error: a client that has no cursor sends
// none, and `?cursor=` is the same request as no parameter at all.
//
// Every refusal is already in the error contract, so a handler can return it unexamined. It is
// bad_request rather than validation_failed for the reason httpx's own registry gives: a cursor is
// not a field a person filled in, it is a token the client was given, and a client that has
// mangled one has made a protocol mistake rather than a data one.
func Decode(raw string, fields int) (Cursor, error) {
	if raw == "" {
		return nil, nil
	}

	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, invalidCursor(err)
	}

	parts := strings.Split(string(decoded), cursorSeparator)
	if len(parts) != fields+1 || parts[0] != cursorVersion {
		return nil, invalidCursor(errors.New("pagination: not a cursor of this version or shape"))
	}
	return Cursor(parts[1:]), nil
}

func invalidCursor(cause error) error {
	return httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
		"The cursor is not one this endpoint issued. Ask for the first page without one.").
		WithCause(cause)
}

// Limit reads a `?limit=` parameter, applying [DefaultLimit] and [MaxLimit].
//
// The two failures are treated differently on purpose:
//
//   - a value that is not a positive whole number is refused, because it is a client defect and
//     answering it with the default would hide one;
//   - a value above the maximum is **clamped rather than refused**, because a client asking for
//     more than the platform will give is asking for a page, not making a mistake — and refusing
//     it turns a tuning change on the server into a broken client.
//
// An absent or empty parameter is the default, which is what makes the zero-argument request the
// ordinary one.
func Limit(raw string) (int, error) {
	if strings.TrimSpace(raw) == "" {
		return DefaultLimit, nil
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, httpx.NewError(http.StatusBadRequest, httpx.CodeBadRequest,
			"The limit must be a whole number of at least 1.").WithCause(err)
	}
	if n > MaxLimit {
		return MaxLimit, nil
	}
	return n, nil
}

// Page is the collection envelope of Docs/10 §4.5.
//
// Every collection in this API is returned in this shape and a single resource is returned as a
// bare object, which is what lets a client tell the two apart without knowing the endpoint.
//
// NextCursor is omitted on the last page rather than sent empty; HasMore is always present, and is
// carried rather than inferred because "the cursor is absent" is also what the *first* request
// looks like, and a client should not have to know the difference.
type Page[T any] struct {
	Data       []T    `json:"data"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

// NewPage builds the envelope, with `data` as an empty array rather than null.
//
// That is the whole reason this is a constructor rather than a struct literal. A nil slice
// marshals to `null`, and a client that writes `for (final j in body['data'])` breaks on an empty
// list — a bug that appears the first time a new customer opens the app and never again in
// testing.
func NewPage[T any](items []T, next string) Page[T] {
	if items == nil {
		items = []T{}
	}
	return Page[T]{Data: items, NextCursor: next, HasMore: next != ""}
}
