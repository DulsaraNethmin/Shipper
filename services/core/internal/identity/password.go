// SHIP-29: password storage.
//
// argon2id, with the cost parameters stored alongside the hash in a PHC string, exactly as
// Docs/10 §5 specifies. Nothing reversible is stored anywhere: the column holds a derived key
// and the salt that produced it, and there is no code path in this package that turns either
// back into a password.
//
// The blank line below keeps this a file note rather than a second package comment; the
// package's own documentation is in doc.go.

package identity

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// The PHC string format, as written by every argon2 implementation that speaks it:
//
//	$argon2id$v=19$m=65536,t=3,p=4$<salt>$<key>
//
// Standard base64 with no padding, per the PHC specification. The variant is fixed: argon2i
// resists side channels at the cost of GPU resistance and argon2d the reverse, and argon2id is
// the hybrid every current recommendation names for password storage.
const (
	phcVariant = "argon2id"

	// saltLength and keyLength are what this package produces. Verify reads both lengths out
	// of the stored hash instead, so a hash written with different ones still verifies.
	saltLength = 16
	keyLength  = 32
)

// argon2Bounds is the range of cost parameters this package will run.
//
// It exists because Verify takes its parameters from the stored hash rather than from
// configuration — which is the property that lets the profile be raised without a migration,
// and also means a hash that has been edited in the database gets a say in how much memory the
// process allocates. A tampered `m=4294967295` is a four-terabyte allocation. The bounds turn
// that into a rejected hash.
//
// The floor is low enough for tests to use a reduced profile: 64 MiB per hash, across packages
// that `go test ./...` runs in parallel, will thrash a laptop (Docs/10 §5).
var argon2Bounds = struct {
	minMemoryKiB, maxMemoryKiB     uint32
	minIterations, maxIterations   uint32
	minParallelism, maxParallelism uint8
	minSaltLength, maxSaltLength   int
	minKeyLength, maxKeyLength     int
}{
	minMemoryKiB:   1024,    // 1 MiB
	maxMemoryKiB:   1 << 20, // 1 GiB
	minIterations:  1,
	maxIterations:  64,
	minParallelism: 1,
	maxParallelism: 64,
	minSaltLength:  8,
	maxSaltLength:  64,
	minKeyLength:   16,
	maxKeyLength:   64,
}

// Argon2Profile is the cost of hashing one password.
//
// Docs/10 §5 fixes the production values at m=64 MiB, t=3, p=4. They are configuration rather
// than constants for two reasons: the numbers are expected to be raised as hardware improves,
// and tests need a profile cheap enough to run hundreds of times.
type Argon2Profile struct {
	// MemoryKiB is argon2's memory cost, in kibibytes. This is the parameter that does the
	// work — it is what makes a parallel attack expensive rather than merely slow.
	MemoryKiB uint32

	// Iterations is the time cost: how many passes are made over that memory.
	Iterations uint32

	// Parallelism is the number of lanes. It must be small enough that MemoryKiB leaves at
	// least eight kibibytes per lane, which is argon2's own floor.
	Parallelism uint8
}

// ProductionArgon2Profile is the profile Docs/10 §5 fixes: m=64 MiB, t=3, p=4.
//
// It is the default in internal/config, which is where a deployment overrides it.
var ProductionArgon2Profile = Argon2Profile{MemoryKiB: 64 * 1024, Iterations: 3, Parallelism: 4}

// Validate reports whether the profile is one this package will run.
func (p Argon2Profile) Validate() error {
	b := argon2Bounds
	switch {
	case p.MemoryKiB < b.minMemoryKiB || p.MemoryKiB > b.maxMemoryKiB:
		return fmt.Errorf("%w: m=%d is outside %d..%d KiB",
			ErrInvalidArgon2Profile, p.MemoryKiB, b.minMemoryKiB, b.maxMemoryKiB)
	case p.Iterations < b.minIterations || p.Iterations > b.maxIterations:
		return fmt.Errorf("%w: t=%d is outside %d..%d",
			ErrInvalidArgon2Profile, p.Iterations, b.minIterations, b.maxIterations)
	case p.Parallelism < b.minParallelism || p.Parallelism > b.maxParallelism:
		return fmt.Errorf("%w: p=%d is outside %d..%d",
			ErrInvalidArgon2Profile, p.Parallelism, b.minParallelism, b.maxParallelism)
	// argon2 divides the memory between the lanes and rounds down; below eight kibibytes a
	// lane the rounding reaches zero and the library's behaviour stops being defined.
	case p.MemoryKiB < 8*uint32(p.Parallelism):
		return fmt.Errorf("%w: m=%d leaves less than 8 KiB for each of p=%d lanes",
			ErrInvalidArgon2Profile, p.MemoryKiB, p.Parallelism)
	}
	return nil
}

// PasswordHasher hashes passwords at one profile and verifies hashes made at any profile.
//
// The asymmetry is the point. Hash uses the profile it was built with; Verify uses the
// parameters recorded in the hash it is given, so raising the profile does not invalidate a
// single stored password. NeedsRehash is how the upgrade then happens: at the owner's next
// sign-in, with no migration and no reset (Docs/10 §5).
type PasswordHasher struct {
	profile Argon2Profile
}

// NewPasswordHasher returns a hasher at the given profile, or an error if the profile is
// outside the range this package will run.
func NewPasswordHasher(profile Argon2Profile) (*PasswordHasher, error) {
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	return &PasswordHasher{profile: profile}, nil
}

// Profile is the profile new hashes are written at.
func (h *PasswordHasher) Profile() Argon2Profile { return h.profile }

// Hash derives a key from the password and returns the PHC string to store.
//
// The salt is sixteen fresh bytes from crypto/rand every time, so two accounts with the same
// password have nothing in common on disk and a precomputed table is worth nothing.
func (h *PasswordHasher) Hash(plaintext string) (string, error) {
	if plaintext == "" {
		return "", ErrEmptyPassword
	}

	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("identity: reading a salt: %w", err)
	}

	key := argon2.IDKey([]byte(plaintext), salt,
		h.profile.Iterations, h.profile.MemoryKiB, h.profile.Parallelism, keyLength)

	return encodePHC(h.profile, salt, key), nil
}

// Verify reports whether the password produces the stored hash.
//
// It reads the variant, the version and all three cost parameters out of the stored string and
// ignores h.profile entirely. That is what allows the profile to be raised later without a
// migration, and it is also why the code that hashes and the code that verifies cannot silently
// disagree about the parameters — there is only one copy of them, and it travels with the hash.
//
// A wrong password returns (false, nil). An unreadable hash returns an error, because those are
// different problems with different answers.
func (h *PasswordHasher) Verify(encoded, plaintext string) (bool, error) {
	stored, err := parsePHC(encoded)
	if err != nil {
		return false, err
	}

	derived := argon2.IDKey([]byte(plaintext), stored.salt,
		stored.profile.Iterations, stored.profile.MemoryKiB, stored.profile.Parallelism,
		uint32(len(stored.key)))

	// Constant time, so the comparison cannot be turned into an oracle by measuring how long
	// it takes to fail. ConstantTimeCompare reports 0 for differing lengths as well.
	return subtle.ConstantTimeCompare(derived, stored.key) == 1, nil
}

// NeedsRehash reports whether a stored hash was made at a weaker profile than this hasher
// writes, and should therefore be replaced.
//
// The caller for this is the sign-in path (SHIP-41): it has the plaintext in hand exactly once,
// which is the only moment a stronger hash can be written without asking anyone to reset
// anything.
func (h *PasswordHasher) NeedsRehash(encoded string) (bool, error) {
	stored, err := parsePHC(encoded)
	if err != nil {
		return false, err
	}
	return stored.profile != h.profile, nil
}

// storedHash is a parsed PHC string.
type storedHash struct {
	profile Argon2Profile
	salt    []byte
	key     []byte
}

func encodePHC(p Argon2Profile, salt, key []byte) string {
	return fmt.Sprintf("$%s$v=%d$m=%d,t=%d,p=%d$%s$%s",
		phcVariant, argon2.Version, p.MemoryKiB, p.Iterations, p.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key))
}

// parsePHC reads a stored hash, refusing anything it does not fully recognise.
//
// Every failure here returns an error rather than a zero value, and nothing in it indexes into a
// slice whose length it has not checked: this function is the one place where a value that may
// have been edited in the database becomes parameters this process acts on.
func parsePHC(encoded string) (storedHash, error) {
	// A well-formed string leaves six fields, the first of them empty, because it opens with
	// a separator: "", "argon2id", "v=19", "m=…,t=…,p=…", salt, key.
	fields := strings.Split(encoded, "$")
	if len(fields) != 6 || fields[0] != "" {
		return storedHash{}, fmt.Errorf(
			"%w: expected $argon2id$v=…$m=…,t=…,p=…$salt$key", ErrMalformedPasswordHash)
	}

	if fields[1] != phcVariant {
		return storedHash{}, fmt.Errorf("%w: variant is %q, not %s",
			ErrMalformedPasswordHash, fields[1], phcVariant)
	}

	version, err := keyedUint(fields[2], "v")
	if err != nil {
		return storedHash{}, err
	}
	if version != argon2.Version {
		return storedHash{}, fmt.Errorf("%w: version is %d, not %d",
			ErrMalformedPasswordHash, version, argon2.Version)
	}

	costs := strings.Split(fields[3], ",")
	if len(costs) != 3 {
		return storedHash{}, fmt.Errorf("%w: expected m, t and p, got %q",
			ErrMalformedPasswordHash, fields[3])
	}
	memory, err := keyedUint(costs[0], "m")
	if err != nil {
		return storedHash{}, err
	}
	iterations, err := keyedUint(costs[1], "t")
	if err != nil {
		return storedHash{}, err
	}
	parallelism, err := keyedUint(costs[2], "p")
	if err != nil {
		return storedHash{}, err
	}

	// Bounded before they are narrowed, so a value too large for the field cannot wrap into a
	// small one and pass Validate.
	if memory > uint64(argon2Bounds.maxMemoryKiB) ||
		iterations > uint64(argon2Bounds.maxIterations) ||
		parallelism > uint64(argon2Bounds.maxParallelism) {
		return storedHash{}, fmt.Errorf("%w: m=%d, t=%d, p=%d exceeds what this package will run",
			ErrInvalidArgon2Profile, memory, iterations, parallelism)
	}

	profile := Argon2Profile{
		MemoryKiB:   uint32(memory),
		Iterations:  uint32(iterations),
		Parallelism: uint8(parallelism),
	}
	if err := profile.Validate(); err != nil {
		return storedHash{}, err
	}

	salt, err := base64.RawStdEncoding.DecodeString(fields[4])
	if err != nil {
		return storedHash{}, fmt.Errorf("%w: the salt is not raw base64", ErrMalformedPasswordHash)
	}
	if len(salt) < argon2Bounds.minSaltLength || len(salt) > argon2Bounds.maxSaltLength {
		return storedHash{}, fmt.Errorf("%w: the salt is %d bytes, outside %d..%d",
			ErrMalformedPasswordHash, len(salt), argon2Bounds.minSaltLength, argon2Bounds.maxSaltLength)
	}

	key, err := base64.RawStdEncoding.DecodeString(fields[5])
	if err != nil {
		return storedHash{}, fmt.Errorf("%w: the key is not raw base64", ErrMalformedPasswordHash)
	}
	if len(key) < argon2Bounds.minKeyLength || len(key) > argon2Bounds.maxKeyLength {
		return storedHash{}, fmt.Errorf("%w: the key is %d bytes, outside %d..%d",
			ErrMalformedPasswordHash, len(key), argon2Bounds.minKeyLength, argon2Bounds.maxKeyLength)
	}

	return storedHash{profile: profile, salt: salt, key: key}, nil
}

// keyedUint reads "m=65536" given "m", refusing anything else.
//
// strconv.ParseUint rather than fmt.Sscanf, which would accept trailing rubbish after a number
// it managed to read.
func keyedUint(field, name string) (uint64, error) {
	value, ok := strings.CutPrefix(field, name+"=")
	if !ok {
		return 0, fmt.Errorf("%w: expected %s=…, got %q", ErrMalformedPasswordHash, name, field)
	}
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %s=%q is not a number", ErrMalformedPasswordHash, name, value)
	}
	return n, nil
}
