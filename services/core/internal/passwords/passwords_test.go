package passwords

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
)

// testProfile is the reduced profile Docs/10 §5 calls for: the production one is 64 MiB per
// hash, and this package hashes dozens of times across tests that `go test ./...` runs beside
// other packages. It is deliberately still a real argon2id profile rather than a stub — the
// property being tested is that the parameters travel with the hash, which a stub would not
// exercise.
var testProfile = Argon2Profile{MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1}

func testHasher(t *testing.T) *Hasher {
	t.Helper()
	h, err := NewHasher(testProfile)
	if err != nil {
		t.Fatalf("building a hasher at the test profile: %v", err)
	}
	return h
}

func TestPasswordRoundTrip(t *testing.T) {
	h := testHasher(t)
	const password = "correct-horse-battery-staple"

	encoded, err := h.Hash(password)
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}

	// The plaintext must not survive anywhere in the stored value. This is the whole of
	// "no reversible storage anywhere" as it applies to one hash.
	if strings.Contains(encoded, password) {
		t.Fatalf("the stored hash contains the password itself: %s", encoded)
	}

	ok, err := h.Verify(encoded, password)
	if err != nil {
		t.Fatalf("verifying: %v", err)
	}
	if !ok {
		t.Error("the password did not verify against its own hash")
	}
}

func TestPasswordVerifyRejectsTheWrongPassword(t *testing.T) {
	h := testHasher(t)

	encoded, err := h.Hash("the-right-one")
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}

	for _, wrong := range []string{"the-wrong-one", "the-right-on", "the-right-one ", "", "The-Right-One"} {
		ok, err := h.Verify(encoded, wrong)
		if err != nil {
			t.Errorf("verifying %q returned an error rather than a mismatch: %v", wrong, err)
			continue
		}
		if ok {
			t.Errorf("%q was accepted", wrong)
		}
	}
}

// TestPasswordHashesDifferForTheSamePassword is the salt, observed rather than assumed.
//
// Without a fresh salt, two accounts choosing the same password are visibly the same account to
// anyone who reads the table, and one cracked hash is every account that shares it.
func TestPasswordHashesDifferForTheSamePassword(t *testing.T) {
	h := testHasher(t)
	const password = "the-same-password"

	first, err := h.Hash(password)
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	second, err := h.Hash(password)
	if err != nil {
		t.Fatalf("hashing again: %v", err)
	}

	if first == second {
		t.Fatal("two hashes of one password are identical, so the salt is not random")
	}

	for _, encoded := range []string{first, second} {
		ok, err := h.Verify(encoded, password)
		if err != nil || !ok {
			t.Errorf("%s did not verify (ok=%v, err=%v)", encoded, ok, err)
		}
	}
}

// phcShape is the PHC string this package writes, as the specification defines it: the variant,
// the version, the three costs in order, then the salt and the key in unpadded standard base64.
var phcShape = regexp.MustCompile(`^\$argon2id\$v=19\$m=\d+,t=\d+,p=\d+\$[A-Za-z0-9+/]+\$[A-Za-z0-9+/]+$`)

func TestPasswordHashIsAPHCString(t *testing.T) {
	h := testHasher(t)

	encoded, err := h.Hash("shape-check")
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}

	// Logged so scripts/verify-foundation.sh can read the stored form out of the test output
	// and check it end to end, rather than taking this assertion's word for it. A hash is not
	// a secret; the password that produced it is, and it is a literal above.
	t.Logf("stored form: %s", encoded)

	if !phcShape.MatchString(encoded) {
		t.Errorf("the stored form is not a PHC argon2id string: %s", encoded)
	}

	// The PHC specification uses unpadded base64, and a padded field would be read as
	// malformed by every other implementation of the format.
	fields := strings.Split(encoded, "$")
	for _, field := range fields[4:] {
		if strings.Contains(field, "=") {
			t.Errorf("the base64 field %q is padded; PHC uses raw base64", field)
		}
	}
}

// TestPasswordParametersComeOutOfTheHash is the property the whole format exists for.
func TestPasswordParametersComeOutOfTheHash(t *testing.T) {
	h := testHasher(t)

	encoded, err := h.Hash("parameters")
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}

	stored, err := parsePHC(encoded)
	if err != nil {
		t.Fatalf("parsing what we just wrote: %v", err)
	}

	if stored.profile != testProfile {
		t.Errorf("parsed profile %+v, wrote %+v", stored.profile, testProfile)
	}
	if len(stored.salt) != saltLength {
		t.Errorf("salt is %d bytes, want %d", len(stored.salt), saltLength)
	}
	if len(stored.key) != keyLength {
		t.Errorf("key is %d bytes, want %d", len(stored.key), keyLength)
	}
}

// TestPasswordSurvivesTheProfileBeingRaised is why the parameters are stored with the hash
// rather than read from configuration.
//
// Raising the cost has to be a configuration change and nothing else. If verification took its
// parameters from configuration, raising it would invalidate every stored password at once —
// which is a mass password reset, discovered by the users.
func TestPasswordSurvivesTheProfileBeingRaised(t *testing.T) {
	const password = "written-under-the-old-profile"

	old := testHasher(t)
	encoded, err := old.Hash(password)
	if err != nil {
		t.Fatalf("hashing at the old profile: %v", err)
	}

	raised, err := NewHasher(Argon2Profile{
		MemoryKiB:   testProfile.MemoryKiB * 2,
		Iterations:  testProfile.Iterations + 1,
		Parallelism: testProfile.Parallelism + 1,
	})
	if err != nil {
		t.Fatalf("building the raised hasher: %v", err)
	}

	ok, err := raised.Verify(encoded, password)
	if err != nil {
		t.Fatalf("verifying an old hash under the raised profile: %v", err)
	}
	if !ok {
		t.Error("a hash written at the old profile no longer verifies, so raising the cost " +
			"would lock every existing account out")
	}

	needs, err := raised.NeedsRehash(encoded)
	if err != nil {
		t.Fatalf("checking whether it needs rehashing: %v", err)
	}
	if !needs {
		t.Error("the old hash does not report needing a rehash, so no password would ever be upgraded")
	}

	// And the new one does not, or every sign-in would rewrite a hash that is already current.
	fresh, err := raised.Hash(password)
	if err != nil {
		t.Fatalf("hashing at the raised profile: %v", err)
	}
	needs, err = raised.NeedsRehash(fresh)
	if err != nil {
		t.Fatalf("checking a fresh hash: %v", err)
	}
	if needs {
		t.Error("a hash written at the current profile reports needing a rehash")
	}
}

// TestPasswordVerifyRejectsAMalformedHash covers the one function in this package that reads a
// value it did not write.
//
// Every case here is something that could genuinely be in the column: a truncated write, a hash
// from another system, a value someone edited at a psql prompt. None of them may panic, and none
// may be treated as a password mismatch — see ErrMalformedHash.
func TestPasswordVerifyRejectsAMalformedHash(t *testing.T) {
	h := testHasher(t)

	sound, err := h.Hash("sound")
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}
	fields := strings.Split(sound, "$")

	unreadable := map[string]string{
		"empty":                "",
		"not a hash at all":    "hunter2",
		"bcrypt":               "$2y$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy",
		"no leading marker":    strings.TrimPrefix(sound, "$"),
		"truncated at the key": sound[:strings.LastIndex(sound, "$")],
		"unknown variant":      "$argon2i$" + strings.Join(fields[2:], "$"),
		"unknown version":      "$argon2id$v=16$" + strings.Join(fields[3:], "$"),
		"missing a cost":       "$argon2id$v=19$m=8192,t=1$" + strings.Join(fields[4:], "$"),
		"reordered costs":      "$argon2id$v=19$t=1,m=8192,p=1$" + strings.Join(fields[4:], "$"),
		"cost is not a number": "$argon2id$v=19$m=lots,t=1,p=1$" + strings.Join(fields[4:], "$"),
		"absurd memory":        "$argon2id$v=19$m=4294967295,t=1,p=1$" + strings.Join(fields[4:], "$"),
		"zero iterations":      "$argon2id$v=19$m=8192,t=0,p=1$" + strings.Join(fields[4:], "$"),
		"zero parallelism":     "$argon2id$v=19$m=8192,t=1,p=0$" + strings.Join(fields[4:], "$"),
		"salt is not base64":   "$argon2id$v=19$m=8192,t=1,p=1$not base64!$" + fields[5],
		"empty salt":           "$argon2id$v=19$m=8192,t=1,p=1$$" + fields[5],
		"padded base64":        strings.Join(fields[:5], "$") + "$" + fields[5] + "==",
		"an extra field":       sound + "$extra",
	}

	for name, encoded := range unreadable {
		t.Run(name, func(t *testing.T) {
			ok, err := h.Verify(encoded, "sound")
			if ok {
				t.Fatal("a malformed hash was accepted as a match")
			}
			if err == nil {
				t.Fatal("a malformed hash was reported as an ordinary password mismatch")
			}
			if !errors.Is(err, ErrMalformedHash) && !errors.Is(err, ErrInvalidProfile) {
				t.Errorf("unexpected error kind: %v", err)
			}
		})
	}

	// Tampering that leaves a readable string is a different case: the format is intact, so
	// the only honest answer is that the password does not match. What must never happen is
	// that it does.
	tampered := map[string]string{
		"truncated mid-key": sound[:len(sound)-8],
		"a different salt":  strings.Join(fields[:4], "$") + "$AAAAAAAAAAAAAAAAAAAAAA$" + fields[5],
		"a different key":   strings.Join(fields[:5], "$") + "$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	}

	for name, encoded := range tampered {
		t.Run(name, func(t *testing.T) {
			ok, err := h.Verify(encoded, "sound")
			if ok {
				t.Fatalf("a tampered hash was accepted as a match: %s", encoded)
			}
			_ = err // an error is a fine answer here; accepting the password is not
		})
	}
}

func TestPasswordHasherRejectsAnImpossibleProfile(t *testing.T) {
	cases := map[string]Argon2Profile{
		"no memory":          {MemoryKiB: 0, Iterations: 3, Parallelism: 4},
		"too little memory":  {MemoryKiB: 64, Iterations: 3, Parallelism: 4},
		"absurd memory":      {MemoryKiB: 1 << 21, Iterations: 3, Parallelism: 4},
		"no iterations":      {MemoryKiB: 64 * 1024, Iterations: 0, Parallelism: 4},
		"no lanes":           {MemoryKiB: 64 * 1024, Iterations: 3, Parallelism: 0},
		"memory below lanes": {MemoryKiB: 1024, Iterations: 3, Parallelism: 200},
	}

	for name, profile := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewHasher(profile); err == nil {
				t.Fatalf("%+v was accepted", profile)
			} else if !errors.Is(err, ErrInvalidProfile) {
				t.Errorf("unexpected error kind: %v", err)
			}
		})
	}
}

func TestPasswordHasherRefusesToHashNothing(t *testing.T) {
	h := testHasher(t)

	if _, err := h.Hash(""); !errors.Is(err, ErrEmptyPassword) {
		t.Errorf("hashing the empty string returned %v, want ErrEmptyPassword", err)
	}
}

// TestSpendEquivalentWorkCostsWhatVerifyCosts is the disclosure control, checked in the package
// that now owns it (SHIP-15r).
//
// The end-to-end proof is `internal/identity`'s
// TestSignInSpendsTheSameWorkWhetherOrNotTheAccountExists, and it stays there because that is where
// the disclosure would happen. **It needs a real PostgreSQL, and this does not** — which is the gap
// worth closing on the way past: after the move, emptying this function's body would leave every
// test in this package green and be caught only by an integration test in another one.
//
// Mutation-checked: replacing the body with a return makes this fail.
func TestSpendEquivalentWorkCostsWhatVerifyCosts(t *testing.T) {
	if testing.Short() {
		t.Skip("times two argon2id derivations; -short is for the runs that skip infrastructure")
	}

	h := testHasher(t)
	stored, err := h.Hash("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("hashing: %v", err)
	}

	verify := medianDuration(t, func() {
		if _, err := h.Verify(stored, "the-wrong-password"); err != nil {
			t.Fatalf("verifying: %v", err)
		}
	})
	spend := medianDuration(t, func() { h.SpendEquivalentWork("the-wrong-password") })

	// A quarter, not a half: the true ratio is one — both are a single derivation at the same
	// profile — and the margin is there so a loaded machine cannot fail an honest build. What it
	// still catches is the defect, which is a path that does no work at all.
	if spend < verify/4 {
		t.Errorf("the decoy derivation costs %s and a real verification costs %s.\n"+
			"A caller who can time two sign-ins can then tell an unknown address from a wrong "+
			"password, which is what one error code refuses to tell them.", spend, verify)
	}
}

// medianDuration is `internal/identity`'s, copied rather than shared: it is fifteen lines of test
// helper, and the alternative to a copy across a package boundary is an exported testing utility in
// non-test code.
//
// Medians rather than single readings, because this runs beside other packages under
// `go test ./... -race` and one descheduled sample would otherwise decide the result.
func medianDuration(t *testing.T, fn func()) time.Duration {
	t.Helper()

	const samples = 5
	var taken [samples]time.Duration
	for i := range taken {
		start := time.Now()
		fn()
		taken[i] = time.Since(start)
	}

	for i := 1; i < samples; i++ {
		for j := i; j > 0 && taken[j] < taken[j-1]; j-- {
			taken[j], taken[j-1] = taken[j-1], taken[j]
		}
	}
	return taken[samples/2]
}

// TestPasswordProductionProfileMatchesTheDocument pins the numbers Docs/10 §5 fixes, so that a
// change to them is a change to the document as well.
func TestPasswordProductionProfileMatchesTheDocument(t *testing.T) {
	want := Argon2Profile{MemoryKiB: 65536, Iterations: 3, Parallelism: 4}
	if ProductionArgon2Profile != want {
		t.Errorf("production profile is %+v, but Docs/10 §5 says %+v", ProductionArgon2Profile, want)
	}
	if err := ProductionArgon2Profile.Validate(); err != nil {
		t.Errorf("the production profile does not pass its own bounds: %v", err)
	}
}
