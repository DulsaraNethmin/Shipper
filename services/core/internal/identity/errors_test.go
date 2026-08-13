package identity

import (
	"errors"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/passwords"
)

// The three password sentinels crossed a package boundary at SHIP-15r, and error identity is the
// part of a move like that which breaks quietly.
//
// `internal/passwords` raises them and this domain publishes them, so a caller may hold either name.
// The decision recorded in errors.go is that they are **the same values** rather than translations
// of each other — but nothing in the build enforces a `=` binding over a fresh `errors.New`, and the
// day somebody writes the second one every `errors.Is` in a caller starts answering false while
// every package still compiles and every other test still passes. `internal/identity` had no test
// over these at all after password_test.go moved out with them, which is what this closes.

// TestThePasswordSentinelsAreTheValuesInternalPasswordsRaises is the behavioural half, and it is
// the one that matters: an error that actually came out of the hasher, matched against this
// domain's name for it.
//
// Mutation-checked: replacing any of the three bindings in errors.go with an errors.New of its own
// fails this.
func TestThePasswordSentinelsAreTheValuesInternalPasswordsRaises(t *testing.T) {
	hasher, err := passwords.NewHasher(testProfile)
	if err != nil {
		t.Fatalf("building a hasher: %v", err)
	}

	t.Run("an unreadable stored hash", func(t *testing.T) {
		// A value that could genuinely be in the column — a hash written by something else.
		_, err := hasher.Verify("$2y$10$notanargon2idhash", "whatever")
		if err == nil {
			t.Fatal("a bcrypt hash verified without complaint")
		}
		if !errors.Is(err, ErrMalformedPasswordHash) {
			t.Errorf("the hasher's error does not match this domain's ErrMalformedPasswordHash: %v.\n"+
				"A caller cannot then tell a data defect from a wrong password, which is the "+
				"distinction the sentinel exists to keep.", err)
		}
	})

	t.Run("the empty password", func(t *testing.T) {
		if _, err := hasher.Hash(""); !errors.Is(err, ErrEmptyPassword) {
			t.Errorf("hashing the empty string returned %v, want ErrEmptyPassword", err)
		}
	})

	t.Run("a profile out of range", func(t *testing.T) {
		_, err := passwords.NewHasher(passwords.Argon2Profile{MemoryKiB: 64, Iterations: 3, Parallelism: 4})
		if !errors.Is(err, ErrInvalidArgon2Profile) {
			t.Errorf("an impossible profile returned %v, want ErrInvalidArgon2Profile", err)
		}
	})
}

// And the structural half, which says the same thing about the bindings themselves and fails with a
// clearer message when somebody changes one.
func TestThePasswordSentinelsAreBoundRatherThanCopied(t *testing.T) {
	for name, pair := range map[string][2]error{
		"ErrEmptyPassword":         {ErrEmptyPassword, passwords.ErrEmptyPassword},
		"ErrMalformedPasswordHash": {ErrMalformedPasswordHash, passwords.ErrMalformedHash},
		"ErrInvalidArgon2Profile":  {ErrInvalidArgon2Profile, passwords.ErrInvalidProfile},
	} {
		t.Run(name, func(t *testing.T) {
			if pair[0] != pair[1] {
				t.Errorf("identity.%s is a different value from the one internal/passwords "+
					"raises (%v vs %v).\nTwo sentinels for one condition agree by convention, "+
					"and the first path that forgets to translate reports an unreadable stored "+
					"hash as an unmapped 500.", name, pair[0], pair[1])
			}
		})
	}
}
