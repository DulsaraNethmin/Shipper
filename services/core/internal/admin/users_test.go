package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// SHIP-151 against a real PostgreSQL.
//
// Docs/06 §4.1s argument for not mocking applies with unusual force here: what this endpoint does is
// a `WHERE` clause, and a mocked store would be a test of a slice filter written twice. `citext` on
// the address, the LIKE escaping and the two-column cursor are all PostgreSQL behaviours.
//
// # The *Done when* is four terms and one of them has nothing behind it
//
// "Search users by email, phone, name, and status." Email, phone and status are exercised below.
// **Name is not, because no column holds one** — see users.go and Docs/11 §4. A test that searched
// for a name would have to assert that nothing comes back, which reads as a passing test of a
// working feature and is the opposite of recording the gap.

// aUser inserts an account directly.
//
// A fixture rather than a call into `identity`, which this domain may not import and which the
// boundary lint refuses. `users` is a shared table and the columns are `000002`s; what is being
// tested is the search, and a registration flow in the middle of it would be a second thing that
// could fail.
func aUser(
	t *testing.T,
	pool *pgxpool.Pool,
	email, phone, role string,
	standing UserStanding,
	createdAt time.Time,
) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating a user id: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `
		INSERT INTO users (id, email, phone, password_hash, role, status, created_at, updated_at)
		VALUES ($1, $2, $3, 'not-a-real-hash', $4, $5, $6, $6)`,
		id, email, phone, role, standing.String(), createdAt,
	); err != nil {
		t.Fatalf("inserting %s: %v", email, err)
	}
	return id
}

// searchFixture is a handler with a live account search and an owner holding a session.
type searchFixture struct {
	pool    *pgxpool.Pool
	handler *Handler
	auth    *Authenticator
	token   string
}

func newSearchFixture(t *testing.T) searchFixture {
	t.Helper()

	f := newAuditFixture(t)
	_, token := f.signedIn(t, "searcher@example.com", RoleSupport, "10.0.60.1")

	return searchFixture{pool: f.pool, handler: f.handler, auth: f.auth, token: token}
}

// search runs one request through the guard and the handler and returns the decoded page.
func (f searchFixture) search(t *testing.T, query url.Values) (int, pageEnvelope[userResponse], string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/users?"+query.Encode(), nil)
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+f.token)

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.SearchUsers()).ServeHTTP(rec, req)

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		return rec.Code, pageEnvelope[userResponse]{}, body
	}

	var page pageEnvelope[userResponse]
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decoding the page: %v (%s)", err, body)
	}
	return rec.Code, page, body
}

// pageEnvelope mirrors internal/pagination's envelope for decoding.
//
// A local type rather than the real one because [pagination.Page] is what the handler *writes*, and
// a test that decoded into it would agree with the handler about the field names by construction.
// This one is written from Docs/10 §4.5, so a rename of `data` fails here.
type pageEnvelope[T any] struct {
	Data       []T    `json:"data"`
	NextCursor string `json:"next_cursor"`
	HasMore    bool   `json:"has_more"`
}

func emailsOf(page pageEnvelope[userResponse]) []string {
	out := make([]string, 0, len(page.Data))
	for _, u := range page.Data {
		out = append(out, u.Email)
	}
	return out
}

// TestTheAccountSearchFindsByEmailAndByPhone is two of the *Done when*s four terms.
//
// One field matching both is the decision users.go records: a support engineer has a string off a
// ticket and does not always know which it is. So the same parameter is checked against both
// columns, and a partial match on either finds the account.
func TestTheAccountSearchFindsByEmailAndByPhone(t *testing.T) {
	f := newSearchFixture(t)

	base := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	aUser(t, f.pool, "hana@acme.example", "+61400111222", "customer", StandingActive, base)
	aUser(t, f.pool, "raj@acme.example", "+61400333444", "provider", StandingActive, base.Add(time.Hour))
	aUser(t, f.pool, "wei@other.example", "+61455000111", "customer", StandingActive, base.Add(2*time.Hour))

	for name, tc := range map[string]struct {
		term string
		want []string
	}{
		"a whole address":     {"hana@acme.example", []string{"hana@acme.example"}},
		"a domain":            {"@acme.example", []string{"raj@acme.example", "hana@acme.example"}},
		"a local part":        {"wei", []string{"wei@other.example"}},
		"the end of a number": {"333444", []string{"raj@acme.example"}},
		"a whole number":      {"+61455000111", []string{"wei@other.example"}},

		// The three below are one number written the ways a person writes it, against a
		// column that holds E.164. `make verify` found this: the section searched for the
		// local form a customer had registered with and got nothing, because registration
		// had normalised `0419…` to `+61419…` and the leading zero is simply not in the row.
		// A substring match of what somebody types against what is stored is the shape that
		// silently finds nothing — and at the endpoint that is indistinguishable from there
		// being no such account. See phonePattern.
		"the local form":        {"0400333444", []string{"raj@acme.example"}},
		"spaced and bracketed":  {"(04) 0033 3444", []string{"raj@acme.example"}},
		"the number as stored":  {"+61400333444", []string{"raj@acme.example"}},
		"a case-varied address": {"HANA@ACME.EXAMPLE", []string{"hana@acme.example"}},
		"nothing at all":        {"nobody@nowhere.example", nil},
	} {
		t.Run(name, func(t *testing.T) {
			status, page, body := f.search(t, url.Values{"q": {tc.term}})
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200 (%s)", status, body)
			}

			got := emailsOf(page)
			if len(got) != len(tc.want) {
				t.Fatalf("searching %q found %v, want %v", tc.term, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("searching %q found %v, want %v (newest first)",
						tc.term, got, tc.want)
					break
				}
			}
		})
	}

	// The administrator's own account is not a user, which is the structural half of "admin
	// authentication is a separate system": `admin_users` and `users` are different tables and
	// nothing joins them.
	_, page, _ := f.search(t, url.Values{"q": {"searcher@example.com"}})
	if len(page.Data) != 0 {
		t.Errorf("an administrator account came back from the user search: %v", emailsOf(page))
	}
}

// TestTheAccountSearchNarrowsByStanding is the *Done when*s fourth term.
//
// Both directions are checked: the filter finds the standing asked for, and an unfiltered search
// still returns every standing. A filter that quietly excluded suspended accounts would pass the
// first check alone, and support looking for a suspended account is most of why this endpoint exists.
func TestTheAccountSearchNarrowsByStanding(t *testing.T) {
	f := newSearchFixture(t)

	base := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	aUser(t, f.pool, "fine@example.com", "+61400000001", "customer", StandingActive, base)
	aUser(t, f.pool, "limited@example.com", "+61400000002", "customer", StandingRestricted, base.Add(time.Hour))
	aUser(t, f.pool, "gone@example.com", "+61400000003", "provider", StandingSuspended, base.Add(2*time.Hour))

	for _, standing := range UserStandings {
		t.Run(standing.String(), func(t *testing.T) {
			status, page, body := f.search(t, url.Values{"status": {standing.String()}})
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200 (%s)", status, body)
			}
			if len(page.Data) != 1 {
				t.Fatalf("filtering on %q found %v, want exactly one", standing, emailsOf(page))
			}
			if page.Data[0].Status != standing.String() {
				t.Errorf("status = %q, want %q", page.Data[0].Status, standing)
			}
		})
	}

	_, all, _ := f.search(t, url.Values{})
	if len(all.Data) != 3 {
		t.Errorf("an unfiltered search found %v, want all three standings", emailsOf(all))
	}

	// And the two filters compose, which is the search support actually runs: this address,
	// among the suspended.
	_, both, _ := f.search(t, url.Values{"q": {"example.com"}, "status": {"suspended"}})
	if len(both.Data) != 1 || both.Data[0].Email != "gone@example.com" {
		t.Errorf("term and standing together found %v, want gone@example.com", emailsOf(both))
	}
}

// TestAnUnrecognisedStandingIsRefusedRatherThanIgnored.
//
// The mistake this catches is a support engineer mistyping. An ignored filter answers with **every**
// account, which is the same answer a filter matching everything gives — so `suspeneded` would read
// as "every account is suspended" rather than as a typo. The refusal has to name the field so they
// can see which one.
func TestAnUnrecognisedStandingIsRefusedRatherThanIgnored(t *testing.T) {
	f := newSearchFixture(t)
	aUser(t, f.pool, "present@example.com", "+61400000009", "customer", StandingActive,
		time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC))

	// 422 rather than 400, because it is the platform's validation shape: the request was
	// well-formed and one field's value is not one the platform has. `validate.Errors` produces
	// that everywhere else and a query parameter is no reason to answer differently — the console
	// branches on `code` and on the named `field`, never on which of the two statuses it was.
	status, _, body := f.search(t, url.Values{"status": {"suspeneded"}})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", status, body)
	}
	if !strings.Contains(body, "status") {
		t.Errorf("the refusal does not name the field: %s", body)
	}
	if strings.Contains(body, "present@example.com") {
		t.Errorf("a refused search returned accounts anyway: %s", body)
	}
}

// TestASearchTermIsNotAPattern is the LIKE escaping, and it is the one defect here with a security
// shape.
//
// `%` is LIKE's "anything", so an unescaped term of `%` matches every account in the table — a full
// read triggered by one character in a search box, from the least-privileged role. `_` matches any
// single character, which is quieter and worse: it returns more than was asked for while looking as
// though it worked.
func TestASearchTermIsNotAPattern(t *testing.T) {
	f := newSearchFixture(t)

	base := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	aUser(t, f.pool, "aa@example.com", "+61400000011", "customer", StandingActive, base)
	aUser(t, f.pool, "ab@example.com", "+61400000012", "customer", StandingActive, base.Add(time.Hour))
	// The one account that genuinely contains the metacharacters, so the escaped pattern has
	// something to match rather than only something to refuse.
	aUser(t, f.pool, "a%b_c@example.com", "+61400000013", "customer", StandingActive, base.Add(2*time.Hour))

	for name, tc := range map[string]struct {
		term string
		want int
	}{
		"a bare wildcard matches nothing": {"%", 1},

		// Not a pattern question, but the same failure: a term too short to be a search
		// must not turn the phone branch on. Every account has a number, so `%000%` would
		// return all three here — which is what a caller who typed three digits into an
		// address search would get. See minPhoneDigits.
		"three digits is not a phone search": {"000", 0},
		"an underscore is not any character": {"a_@example.com", 0},
		"the literal metacharacters match":   {"a%b_c", 1},
	} {
		t.Run(name, func(t *testing.T) {
			status, page, body := f.search(t, url.Values{"q": {tc.term}})
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200 (%s)", status, body)
			}
			if len(page.Data) != tc.want {
				t.Errorf("searching %q found %d accounts (%v), want %d.\n"+
					"A search term is a string somebody typed, not a LIKE pattern. "+
					"See likeContains.", tc.term, len(page.Data), emailsOf(page), tc.want)
			}
		})
	}
}

// TestTheAccountSearchPagesWithoutSkippingOrRepeating.
//
// The cursor is two columns because `created_at` is not unique, and the failure a single-column
// cursor produces is not a crash — it is an account silently missing from a search, which is the one
// outcome this endpoint must not have. So the fixture puts several accounts on the **same instant**,
// which is the case a `created_at`-only cursor gets wrong.
func TestTheAccountSearchPagesWithoutSkippingOrRepeating(t *testing.T) {
	f := newSearchFixture(t)

	same := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	want := map[string]bool{}
	for i := range 7 {
		email := fmt.Sprintf("page%d@example.com", i)
		aUser(t, f.pool, email, fmt.Sprintf("+6140000%04d", i), "customer", StandingActive, same)
		want[email] = true
	}

	seen := map[string]int{}
	cursor := ""
	for pages := 0; ; pages++ {
		if pages > 10 {
			t.Fatal("the search never reached its last page")
		}

		values := url.Values{"limit": {"3"}}
		if cursor != "" {
			values.Set("cursor", cursor)
		}

		status, page, body := f.search(t, values)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", status, body)
		}
		for _, u := range page.Data {
			seen[u.Email]++
		}
		if !page.HasMore {
			break
		}
		cursor = page.NextCursor
	}

	var missing, repeated []string
	for email := range want {
		switch seen[email] {
		case 0:
			missing = append(missing, email)
		case 1:
		default:
			repeated = append(repeated, email)
		}
	}
	sort.Strings(missing)
	sort.Strings(repeated)

	if len(missing) != 0 {
		t.Errorf("paging skipped %v.\n"+
			"Accounts created in the same instant need a cursor on (created_at, id); on "+
			"created_at alone the page boundary either loses a row or repeats one.", missing)
	}
	if len(repeated) != 0 {
		t.Errorf("paging repeated %v", repeated)
	}
}

// TestTheUserSearchResponseCarriesNothingCommercial is the budget invariant reaching this endpoint.
//
// # Why it is a closed key set rather than a search for the word
//
// SHIP-83 established the axis: a response searched for "budget" passes while carrying `max_price`,
// and the two leak the same fact. So this asserts what the shape **has** rather than what it lacks —
// a field added to [userResponse] fails here until somebody says what it is, which is the review
// this invariant deserves.
//
// It also asserts there is no `password_hash` and no hash under any name, which [UserRecord] makes
// structural: the column is not selected and there is nowhere to put it.
func TestTheUserSearchResponseCarriesNothingCommercial(t *testing.T) {
	f := newSearchFixture(t)
	aUser(t, f.pool, "shape@example.com", "+61400000021", "customer", StandingActive,
		time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC))

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/users?q=shape", nil)
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+f.token)
	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.SearchUsers()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	var envelope struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decoding: %v (%s)", err, rec.Body)
	}
	if len(envelope.Data) != 1 {
		t.Fatalf("found %d accounts, want 1", len(envelope.Data))
	}

	permitted := map[string]bool{
		"id": true, "email": true, "phone": true, "role": true, "status": true,
		"email_verified_at": true, "phone_verified_at": true, "created_at": true,
	}

	var unexpected []string
	for key := range envelope.Data[0] {
		if !permitted[key] {
			unexpected = append(unexpected, key)
		}
	}
	sort.Strings(unexpected)

	if len(unexpected) != 0 {
		t.Errorf("the administrator's view of an account carries %v.\n"+
			"This shape is a closed set of account facts. A customer's budget is never "+
			"exposed in any form (Docs/01 §4.3) and this endpoint reads no commercial data at "+
			"all — if a field belongs here, add it to the permitted set deliberately.",
			unexpected)
	}
	for key := range permitted {
		if _, ok := envelope.Data[0][key]; !ok {
			t.Errorf("the response is missing %q", key)
		}
	}
}

// TestTheAccountSearchNeedsAnAdministratorCredential.
//
// No authorisation decision is made on the device (CLAUDE.md), and the console hiding the screen is
// not what stops somebody reaching it. A request with no credential, and one with something that is
// not an administrator session, both answer 401 — and neither reveals whether any account matched.
func TestTheAccountSearchNeedsAnAdministratorCredential(t *testing.T) {
	f := newSearchFixture(t)
	aUser(t, f.pool, "private@example.com", "+61400000031", "customer", StandingActive,
		time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC))

	for name, header := range map[string]string{
		"nothing presented":  "",
		"an invented token":  "Bearer not-a-session",
		"an empty bearer":    "Bearer ",
		"the wrong scheme":   "Basic " + f.token,
		"the raw credential": f.token,
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/admin/users?q=private", nil)
			if header != "" {
				req.Header.Set(httpx.HeaderAuthorization, header)
			}

			rec := httptest.NewRecorder()
			RequireAdmin(f.auth)(f.handler.SearchUsers()).ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401 (%s)", rec.Code, rec.Body)
			}
			if strings.Contains(rec.Body.String(), "private@example.com") {
				t.Errorf("a refused search returned accounts: %s", rec.Body)
			}
		})
	}
}

// TestSearchingWithNoDatabaseAnswersUnavailable.
//
// The service starts with an unreachable database on purpose, so a failover does not take the fleet
// down. 503 rather than 500 is the difference between a console that retries and one that shows a
// failure to the person at the screen.
func TestSearchingWithNoDatabaseAnswersUnavailable(t *testing.T) {
	users, err := NewUsers(nil)
	if err != nil {
		t.Fatalf("building the account search: %v", err)
	}
	if _, err := users.Search(t.Context(), UserQuery{Limit: 10}); err == nil {
		t.Error("a search with no database answered without an error")
	}
}
