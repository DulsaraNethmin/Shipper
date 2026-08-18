package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/passwords"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-30 against a real PostgreSQL, per Docs/06 §4.1: "a mock happily accepts a write that the
// actual constraint would reject". Duplicate rejection *is* a constraint here — the service
// does not check first, it inserts and reads the refusal — so a mocked store would report every
// one of these tests as passing while proving nothing at all.

// recordingSender stands in for the email adapter, and keeps what it was asked to send.
//
// It is a test double for a *port*, not for persistence — which is the distinction Docs/06 §4.1
// draws. Mocking the database would hide the constraints that make these rules true; mocking the
// mail provider is how the token that only exists in the message becomes readable by the test
// that has to present it back.
type recordingSender struct {
	sent []sentMessage
	err  error
}

type sentMessage struct{ to, subject, body string }

func (s *recordingSender) Send(_ context.Context, to, subject, body string) error {
	if s.err != nil {
		return s.err
	}
	s.sent = append(s.sent, sentMessage{to, subject, body})
	return nil
}

func (s *recordingSender) last(t *testing.T) sentMessage {
	t.Helper()
	if len(s.sent) == 0 {
		t.Fatal("nothing was sent")
	}
	return s.sent[len(s.sent)-1]
}

// newTestService builds a service against a fresh database, at a cost profile a laptop can run
// hundreds of times (Docs/10 §5).
func newTestService(t *testing.T) (*Service, *pgxpool.Pool, *recordingSender) {
	t.Helper()

	pool := pgtest.DB(t)

	hasher, err := passwords.NewHasher(testProfile)
	if err != nil {
		t.Fatalf("building the hasher: %v", err)
	}

	mail := &recordingSender{}
	svc, err := NewService(pool, hasher, testServiceIssuer(t, clock.System{}), testLimiter(t),
		mail, &recordingTexter{}, noDelivery, clock.System{})
	if err != nil {
		t.Fatalf("building the service: %v", err)
	}
	return svc, pool, mail
}

// newTestServiceWithSMS is newTestService for the tickets that need to read the code out of the
// message, and a clock they can move.
func newTestServiceWithSMS(t *testing.T, clk clock.Clock) (*Service, *pgxpool.Pool, *recordingTexter) {
	t.Helper()

	pool := pgtest.DB(t)

	hasher, err := passwords.NewHasher(testProfile)
	if err != nil {
		t.Fatalf("building the hasher: %v", err)
	}

	texter := &recordingTexter{}
	svc, err := NewService(pool, hasher, testServiceIssuer(t, clk), testLimiter(t),
		&recordingSender{}, texter, noDelivery, clk)
	if err != nil {
		t.Fatalf("building the service: %v", err)
	}
	return svc, pool, texter
}

// recordingTexter is the SMS port's test double, and the only place a one-time code can be read
// — the table holds an argon2id hash of it and nothing else.
type recordingTexter struct {
	sent []sentText
	err  error
}

type sentText struct{ to, body string }

func (s *recordingTexter) Send(_ context.Context, to, body string) error {
	if s.err != nil {
		return s.err
	}
	s.sent = append(s.sent, sentText{to, body})
	return nil
}

func (s *recordingTexter) last(t *testing.T) sentText {
	t.Helper()
	if len(s.sent) == 0 {
		t.Fatal("no message was sent")
	}
	return s.sent[len(s.sent)-1]
}

func validRegistration() RegisterCommand {
	return RegisterCommand{
		Name:     "Alice Nguyen",
		Email:    "alice@example.com",
		Phone:    "0412 345 678",
		Password: "correct-horse-battery-staple",
		Role:     RoleCustomer,
	}
}

// TestRegisterCreatesAnUnverifiedAccount is SHIP-30's acceptance criterion, first half.
func TestRegisterCreatesAnUnverifiedAccount(t *testing.T) {
	svc, pool, _ := newTestService(t)

	user, err := svc.Register(t.Context(), validRegistration())
	if err != nil {
		t.Fatalf("registering: %v", err)
	}

	if user.ID.String() == "" || user.ID.Version() != 7 {
		t.Errorf("id = %s, want a UUIDv7 (Docs/10 §3.3)", user.ID)
	}
	if user.Email != "alice@example.com" {
		t.Errorf("email = %q, want it stored as submitted", user.Email)
	}
	if user.Phone != "+61412345678" {
		t.Errorf("phone = %q, want E.164 — the uniqueness index compares strings", user.Phone)
	}
	if user.Role != RoleCustomer {
		t.Errorf("role = %q, want customer", user.Role)
	}
	if user.Status != StatusActive {
		t.Errorf("status = %q, want active", user.Status)
	}

	// The half the ticket names: unverified. Docs/04 §2 requires both channels verified
	// before a customer may publish, and Docs/01 §4.1 allows drafts before then.
	if user.EmailVerified() {
		t.Error("a new account reports its email as verified, and nothing has verified it")
	}
	if user.PhoneVerified() {
		t.Error("a new account reports its phone as verified, and nothing has verified it")
	}
	if user.CanPublish() {
		t.Error("a brand new account may already publish, which skips Docs/04 §2 entirely")
	}

	// Read it back through a second connection rather than trusting RETURNING: the point is
	// that the row is there, not that the statement answered.
	var storedRole, storedStatus string
	var emailVerified, phoneVerified *time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT role, status, email_verified_at, phone_verified_at FROM users WHERE id = $1`,
		user.ID).Scan(&storedRole, &storedStatus, &emailVerified, &phoneVerified); err != nil {
		t.Fatalf("reading the account back: %v", err)
	}
	if storedRole != "customer" || storedStatus != "active" {
		t.Errorf("stored role/status = %q/%q, want customer/active", storedRole, storedStatus)
	}
	if emailVerified != nil || phoneVerified != nil {
		t.Errorf("stored verification = %v/%v, want both null", emailVerified, phoneVerified)
	}
}

// TestRegisterStoresTheNameInAColumnOfItsOwn is SHIP-30a's first clause.
//
// "Registration requires a name and `users` holds it in a column of its own." The requirement is
// [TestRegisterValidation]'s four name cases; this is the column, read back through a second
// connection rather than from RETURNING, because the claim is that the row holds it.
//
// The name is asserted **as trimmed**, which is the half worth checking: Normalise runs before
// Validate, so what reaches the column is what a search will later be matched against. A stored
// " Alice Nguyen " would be found by nobody typing the name.
func TestRegisterStoresTheNameInAColumnOfItsOwn(t *testing.T) {
	svc, pool, _ := newTestService(t)

	cmd := validRegistration()
	cmd.Name = "  Alice  Nguyen  "

	user, err := svc.Register(t.Context(), cmd)
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	if user.Name != "Alice  Nguyen" {
		t.Errorf("returned name = %q, want it trimmed but otherwise untouched", user.Name)
	}

	var stored string
	if err := pool.QueryRow(t.Context(),
		`SELECT name FROM users WHERE id = $1`, user.ID).Scan(&stored); err != nil {
		t.Fatalf("reading the name back: %v", err)
	}
	if stored != "Alice  Nguyen" {
		t.Errorf("stored name = %q, want %q", stored, "Alice  Nguyen")
	}
}

// TestTheDatabaseRefusesABlankName is the Docs/10 §3.4 pairing for the name.
//
// The service refuses a blank name and so does `ck_users_name`, and **two checks believed to agree
// are two checks until something compares them** — SHIP-162's `ck_admin_notes_body` disagreed with
// `strings.TrimSpace` about a newline for a whole wave. So this drives the constraint directly,
// past the service, with the same character set the service trims.
//
// It is here rather than in `migrations` because the pairing is between *this* validator and that
// constraint; `migrations/users_name_test.go` asserts the constraint's own behaviour.
func TestTheDatabaseRefusesABlankName(t *testing.T) {
	_, pool, _ := newTestService(t)

	for i, blank := range []string{"", " ", "\t", "\n", " \t\r\n "} {
		id, err := uuid.NewV7()
		if err != nil {
			t.Fatalf("generating an id: %v", err)
		}

		// Address and number are unique per case rather than derived from the identifier: a
		// UUIDv7 leads with a timestamp, so two generated in one millisecond share their
		// leading characters and uq_users_phone would refuse the row before ck_users_name saw
		// it — which is a green test of a different constraint.
		_, err = pool.Exec(t.Context(), `
			INSERT INTO users (id, name, email, phone, password_hash, role, status)
			VALUES ($1, $2, $3, $4, 'not-a-hash', 'customer', 'active')`,
			id, blank, fmt.Sprintf("blank%d@example.com", i), fmt.Sprintf("+6149010%04d", i))
		if err == nil {
			t.Errorf("a name of %q was accepted; the service refuses the same value", blank)
			continue
		}
		if !strings.Contains(err.Error(), "ck_users_name") {
			t.Errorf("a name of %q was refused by something other than ck_users_name: %v",
				blank, err)
		}
	}
}

// TestRegisterStoresOnlyADerivedPassword: nothing reversible anywhere (SHIP-29's rule, at the
// endpoint that first writes one).
func TestRegisterStoresOnlyADerivedPassword(t *testing.T) {
	svc, pool, _ := newTestService(t)

	cmd := validRegistration()
	user, err := svc.Register(t.Context(), cmd)
	if err != nil {
		t.Fatalf("registering: %v", err)
	}

	var stored string
	if err := pool.QueryRow(t.Context(),
		`SELECT password_hash FROM users WHERE id = $1`, user.ID).Scan(&stored); err != nil {
		t.Fatalf("reading the hash: %v", err)
	}

	if strings.Contains(stored, cmd.Password) {
		t.Fatal("the stored value contains the password")
	}
	if !strings.HasPrefix(stored, "$argon2id$") {
		t.Errorf("stored = %q, want a PHC string", stored)
	}

	hasher, _ := passwords.NewHasher(testProfile)
	ok, err := hasher.Verify(stored, cmd.Password)
	if err != nil || !ok {
		t.Errorf("the stored hash does not verify the password that made it (ok=%v, err=%v)", ok, err)
	}
}

// TestRegisterRejectsADuplicateEmail is SHIP-30's acceptance criterion, second half.
//
// The second address differs only in case. citext plus uq_users_email is what makes that one
// account rather than two indistinguishable ones (000002_users), and the service reads the
// index's refusal rather than checking first — so this also proves the error mapping reaches
// the right constraint.
func TestRegisterRejectsADuplicateEmail(t *testing.T) {
	svc, _, _ := newTestService(t)

	if _, err := svc.Register(t.Context(), validRegistration()); err != nil {
		t.Fatalf("the first registration failed: %v", err)
	}

	second := validRegistration()
	second.Email = "ALICE@Example.COM"
	second.Phone = "0412 345 679"

	_, err := svc.Register(t.Context(), second)
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("second registration returned %v, want ErrEmailTaken", err)
	}
}

// TestRegisterRejectsADuplicatePhone uses a different written form of the same number, which is
// the case normalisation exists for: without it the two are different strings and uq_users_phone
// never sees a collision.
func TestRegisterRejectsADuplicatePhone(t *testing.T) {
	svc, _, _ := newTestService(t)

	if _, err := svc.Register(t.Context(), validRegistration()); err != nil {
		t.Fatalf("the first registration failed: %v", err)
	}

	second := validRegistration()
	second.Email = "bob@example.com"
	second.Phone = "+61 412 345 678"

	_, err := svc.Register(t.Context(), second)
	if !errors.Is(err, ErrPhoneTaken) {
		t.Fatalf("second registration returned %v, want ErrPhoneTaken — two accounts now share "+
			"a number, so an OTP cannot say which it verifies", err)
	}
}

// TestRegisterAssignsTheChosenRole is SHIP-45's first half; the immutability half is in
// migrations/schema_test.go, where the trigger that enforces it lives.
func TestRegisterAssignsTheChosenRole(t *testing.T) {
	svc, _, _ := newTestService(t)

	for _, role := range []Role{RoleCustomer, RoleProvider} {
		t.Run(role.String(), func(t *testing.T) {
			cmd := validRegistration()
			cmd.Role = role
			cmd.Email = role.String() + "@example.com"
			cmd.Phone = "+6141200000" + map[Role]string{RoleCustomer: "1", RoleProvider: "2"}[role]

			user, err := svc.Register(t.Context(), cmd)
			if err != nil {
				t.Fatalf("registering: %v", err)
			}
			if user.Role != role {
				t.Errorf("role = %q, want %q", user.Role, role)
			}
		})
	}
}

// TestRegisterRefusesAdminAsARole. There is no third role: administrators sign in through a
// separate system (SHIP-147), and ck_users_role refuses the value at the database as well.
func TestRegisterRefusesAdminAsARole(t *testing.T) {
	svc, _, _ := newTestService(t)

	cmd := validRegistration()
	cmd.Role = Role("admin")

	_, err := svc.Register(t.Context(), cmd)
	if err == nil {
		t.Fatal("an account was created with the admin role")
	}
	assertFieldRejected(t, err, "role")
}

// TestRegisterValidationReportsEveryProblemAtOnce. A six-field form answered one error at a
// time takes six round trips to fill in, on a phone, in a truck yard (Docs/10 §4.6).
//
// No database: validation runs before anything is written, which is also what makes the
// equivalent test in cmd/api reachable with a nil pool.
func TestRegisterValidationReportsEveryProblemAtOnce(t *testing.T) {
	err := RegisterCommand{}.Validate()

	fields := rejectedFields(t, err)
	for _, want := range []string{"name", "email", "phone", "password", "role"} {
		if !fields[want] {
			t.Errorf("an empty registration did not report %q; got %v", want, fields)
		}
	}
}

func TestRegisterValidation(t *testing.T) {
	cases := map[string]struct {
		mutate func(*RegisterCommand)
		field  string
	}{
		"no @ in the address":      {func(c *RegisterCommand) { c.Email = "alice.example.com" }, "email"},
		"no domain dot":            {func(c *RegisterCommand) { c.Email = "alice@example" }, "email"},
		"a display name pasted":    {func(c *RegisterCommand) { c.Email = "Alice <alice@example.com>" }, "email"},
		"a space in the address":   {func(c *RegisterCommand) { c.Email = "ali ce@example.com" }, "email"},
		"landline-length nonsense": {func(c *RegisterCommand) { c.Phone = "123" }, "phone"},
		"letters in the number":    {func(c *RegisterCommand) { c.Phone = "0412 34a 678" }, "phone"},
		"a password of nine":       {func(c *RegisterCommand) { c.Password = strings.Repeat("a", 9) }, "password"},
		"a password of 129":        {func(c *RegisterCommand) { c.Password = strings.Repeat("a", 129) }, "password"},
		"a role nobody has":        {func(c *RegisterCommand) { c.Role = "driver" }, "role"},

		// SHIP-30a. Whitespace is the case worth having: Normalise trims before Validate
		// runs, so a name of three spaces is a *missing* name rather than a three-character
		// one — which is the same disagreement `ck_users_name` refuses in the database.
		"no name at all":      {func(c *RegisterCommand) { c.Name = "" }, "name"},
		"a name of spaces":    {func(c *RegisterCommand) { c.Name = "   " }, "name"},
		"a name of a tab":     {func(c *RegisterCommand) { c.Name = "\t" }, "name"},
		"a name of a newline": {func(c *RegisterCommand) { c.Name = "\n" }, "name"},
		"a name of 121 runes": {func(c *RegisterCommand) { c.Name = strings.Repeat("a", 121) }, "name"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cmd := validRegistration()
			tc.mutate(&cmd)
			cmd.Normalise()

			err := cmd.Validate()
			if err == nil {
				t.Fatalf("accepted: %+v", cmd)
			}
			assertFieldRejected(t, err, tc.field)
		})
	}
}

// TestPasswordsAreNotTrimmed. Silently removing a leading space means the password accepted at
// registration is not the one that will be accepted at sign-in.
func TestPasswordsAreNotTrimmed(t *testing.T) {
	cmd := validRegistration()
	cmd.Password = "  " + cmd.Password + "  "
	original := cmd.Password

	cmd.Normalise()
	if cmd.Password != original {
		t.Errorf("password = %q, want it untouched", cmd.Password)
	}
}

func TestNormalisePhone(t *testing.T) {
	cases := map[string]string{
		"0412 345 678":    "+61412345678",
		"0412345678":      "+61412345678",
		"(04) 1234 5678":  "+61412345678",
		"04-1234-5678":    "+61412345678",
		"+61 412 345 678": "+61412345678",
		"+61412345678":    "+61412345678",
		"61412345678":     "+61412345678",
		"+64 21 123 4567": "+64211234567",
		"":                "",
	}

	for input, want := range cases {
		t.Run(input, func(t *testing.T) {
			if got := normalisePhone(input); got != want {
				t.Errorf("normalisePhone(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

// TestValidE164 covers the shapes normalisePhone cannot repair, which have to be refused rather
// than guessed at.
func TestValidE164(t *testing.T) {
	valid := []string{"+61412345678", "+6421123456", "+14155552671"}
	invalid := []string{
		"",
		"0412345678",        // no country code — normalisePhone would have fixed this one
		"+0412345678",       // a country code never starts with zero
		"+6141",             // too short to be anybody
		"+6141234567890123", // more than fifteen digits
		"61412345678",       // no plus
	}

	for _, phone := range valid {
		if !validE164(phone) {
			t.Errorf("validE164(%q) = false, want true", phone)
		}
	}
	for _, phone := range invalid {
		if validE164(phone) {
			t.Errorf("validE164(%q) = true, want false", phone)
		}
	}
}

// TestRegisterWithoutADatabaseIsUnavailableNotAPanic.
//
// Deps documents the pool as nil-able, because the service starts with an unreachable database
// on purpose (a rolling deployment during a failover must not take the fleet down). A handler
// that dereferenced it would answer 500, which tells a mobile client to give up rather than to
// retry.
func TestRegisterWithoutADatabaseIsUnavailableNotAPanic(t *testing.T) {
	hasher, err := passwords.NewHasher(testProfile)
	if err != nil {
		t.Fatalf("building the hasher: %v", err)
	}
	svc, err := NewService(nil, hasher, testServiceIssuer(t, clock.System{}), testLimiter(t),
		&recordingSender{}, &recordingTexter{}, noDelivery, clock.System{})
	if err != nil {
		t.Fatalf("building the service: %v", err)
	}

	if _, err := svc.Register(t.Context(), validRegistration()); !errors.Is(err, errUnavailable) {
		t.Fatalf("register with no pool returned %v, want errUnavailable", err)
	}
}

// rejectedFields pulls the field paths out of a validation failure, which is the part a client
// puts beside each input.
func rejectedFields(t *testing.T, err error) map[string]bool {
	t.Helper()

	var apiErr *httpx.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v is not a *httpx.Error, so it carries no field detail", err)
	}
	if apiErr.Code != httpx.CodeValidationFailed {
		t.Fatalf("code = %q, want %q", apiErr.Code, httpx.CodeValidationFailed)
	}

	fields := map[string]bool{}
	for _, d := range apiErr.Details {
		fields[d.Field] = true
	}
	return fields
}

func assertFieldRejected(t *testing.T, err error, field string) {
	t.Helper()
	if fields := rejectedFields(t, err); !fields[field] {
		t.Errorf("the failure does not name %q; it names %v", field, fields)
	}
}
