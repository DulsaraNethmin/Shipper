// Package passwords stores a password so that nothing can read it back (SHIP-29).
//
// argon2id, with the cost parameters stored alongside the hash in a PHC string, exactly as
// Docs/10 §5 specifies. Nothing reversible is stored anywhere: the column holds a derived key
// and the salt that produced it, and there is no code path in this package that turns either
// back into a password.
//
// # Why it is infrastructure and not part of internal/identity
//
// It was internal/identity's until SHIP-15r, and it moved because a second domain needs it.
// SHIP-147 gives an administrator a password, `internal/admin` may not import `internal/identity`
// — domains do not import each other, enforced by `make lint-imports` and by a test — and the two
// remaining options were this package or a second argon2id implementation inside `admin`.
//
// **Hashing parameters are a security control, and two implementations that agree by comment are
// what Docs/10 §3.4 exists to refuse.** The concrete failure is not exotic: one copy is raised to
// m=128 MiB and the other is not, both still verify every hash they are given because the
// parameters travel in the PHC string, and nothing anywhere reports that half the platform's
// passwords are stored at the old cost. A single implementation makes that impossible rather than
// unlikely.
//
// **It imports neither a domain nor an adapter, and it must not** (SHIP-15c). Nothing here knows
// what a user is, what a role is, or where a hash is stored — it takes a string and returns a
// string. That is what makes it safe for every domain to sit on.
//
// # What did not move with it
//
// [Hasher.SpendEquivalentWork] is here and is the exception worth naming: it is a disclosure
// control rather than a hashing primitive, and it belongs beside the derivation whose cost it
// reproduces. Its *consumer* is `internal/identity`'s sign-in path, and the end-to-end proof —
// that an unknown address costs what a wrong password costs — stays there, in
// TestSignInSpendsTheSameWorkWhetherOrNotTheAccountExists, because that is where the disclosure
// would actually happen.

package passwords

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// The sentinel errors this package raises.
//
// **They moved here from internal/identity's errors.go rather than being re-created**, and that
// distinction is the whole of why error identity survived the move: `identity` binds its three
// exported names to these values, so `errors.Is` answers the same question whichever name a caller
// reaches for, because there is one value behind both. A fresh `errors.New` on either side plus a
// translation somewhere in between would be two values agreeing by convention, and the first path
// that forgot to translate would report a malformed stored hash as an unmapped 500 — the exact
// failure the distinction between "wrong password" and "unreadable hash" exists to prevent.
var (
	// ErrEmptyPassword is returned rather than hashing the empty string, which would otherwise
	// produce a perfectly valid hash that any empty submission then matches. Length and strength
	// rules belong to whoever accepts the password; this is only the floor below which hashing is
	// meaningless.
	ErrEmptyPassword = errors.New("passwords: the password is empty")

	// ErrMalformedHash means the stored PHC string could not be read: a truncated column, a hash
	// written by something else, or a value someone has edited.
	//
	// It is deliberately distinct from "the password did not match". A wrong password is an
	// ordinary event; an unreadable hash is a data defect, and answering "wrong password" to it
	// would hide the defect behind a sign-in failure the owner cannot explain.
	ErrMalformedHash = errors.New("passwords: the stored password hash is malformed")

	// ErrInvalidProfile means the cost parameters are outside the range this package will run —
	// either configured that way, or read out of a hash that has been tampered with. See
	// argon2Bounds for why the range exists.
	ErrInvalidProfile = errors.New("passwords: the argon2id profile is out of range")
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
	minMemoryKiB, maxMemoryKiB   uint32
	minIterations, maxIterations uint32
	minParallelism               uint8
	minSaltLength, maxSaltLength int
	minKeyLength, maxKeyLength   int
}{
	minMemoryKiB:   1024,    // 1 MiB
	maxMemoryKiB:   1 << 20, // 1 GiB
	minIterations:  1,
	maxIterations:  64,
	minParallelism: 1,
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
			ErrInvalidProfile, p.MemoryKiB, b.minMemoryKiB, b.maxMemoryKiB)
	case p.Iterations < b.minIterations || p.Iterations > b.maxIterations:
		return fmt.Errorf("%w: t=%d is outside %d..%d",
			ErrInvalidProfile, p.Iterations, b.minIterations, b.maxIterations)
	case p.Parallelism < b.minParallelism:
		return fmt.Errorf("%w: p=%d, and argon2 needs at least %d lane",
			ErrInvalidProfile, p.Parallelism, b.minParallelism)
	// There is no separate ceiling on the lane count: this is the real rule. argon2 divides
	// the memory between the lanes and rounds down, so below eight kibibytes a lane the
	// rounding reaches zero and the library's behaviour stops being defined.
	case p.MemoryKiB < 8*uint32(p.Parallelism):
		return fmt.Errorf("%w: m=%d leaves less than 8 KiB for each of p=%d lanes",
			ErrInvalidProfile, p.MemoryKiB, p.Parallelism)
	}
	return nil
}

// Hasher hashes passwords at one profile and verifies hashes made at any profile.
//
// The asymmetry is the point. Hash uses the profile it was built with; Verify uses the
// parameters recorded in the hash it is given, so raising the profile does not invalidate a
// single stored password. NeedsRehash is how the upgrade then happens: at the owner's next
// sign-in, with no migration and no reset (Docs/10 §5).
type Hasher struct {
	profile Argon2Profile
}

// NewHasher returns a hasher at the given profile, or an error if the profile is
// outside the range this package will run.
func NewHasher(profile Argon2Profile) (*Hasher, error) {
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	return &Hasher{profile: profile}, nil
}

// Profile is the profile new hashes are written at.
func (h *Hasher) Profile() Argon2Profile { return h.profile }

// Hash derives a key from the password and returns the PHC string to store.
//
// The salt is sixteen fresh bytes from crypto/rand every time, so two accounts with the same
// password have nothing in common on disk and a precomputed table is worth nothing.
func (h *Hasher) Hash(plaintext string) (string, error) {
	if plaintext == "" {
		return "", ErrEmptyPassword
	}

	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("passwords: reading a salt: %w", err)
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
func (h *Hasher) Verify(encoded, plaintext string) (bool, error) {
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

// decoySalt is the salt [Hasher.SpendEquivalentWork] derives against.
//
// Fixed, and in the source deliberately: it salts nothing, because nothing here is stored or
// compared. A fresh random salt would suggest the derived key mattered, and the next reader would
// go looking for where it was kept.
var decoySalt = []byte("shipper-decoy-salt")[:saltLength]

// SpendEquivalentWork derives a key from plaintext and discards it, so that a sign-in against an
// address with no account costs what one against a real account costs (SHIP-41).
//
// # Why this exists
//
// `internal/identity`'s CodeCredentialsInvalid gives one answer to "no such address" and to "wrong
// password", so that sign-in cannot be used to find out which addresses have accounts. That is the
// consumer this exists for, and it is named rather than linked because this package may not import
// a domain. Without this, the *response time* would answer anyway and rather more cheaply than the
// status code refuses to: argon2id at m=64 MiB takes tens of milliseconds, and a lookup that misses
// takes none of them. A caller timing two requests reads the difference off a wall clock.
//
// **This is a disclosure control, not a performance detail**, which is why it moved with the
// derivation rather than staying beside its caller: what it has to equal is the cost of
// [Hasher.Verify], and the only place that cost is decided is here. SHIP-147 gets it for free —
// an administrator sign-in has exactly the same enumeration problem.
//
// It is one argon2id derivation at this hasher's profile, which is the cost [Hasher.Verify]
// is made of. The PHC parse Verify also does is microseconds against that and is not reproduced —
// what is being equalised is the cost that dominates, not every instruction.
//
// The result is deliberately unused, and runtime.KeepAlive is what says so to a reader: without
// it the call reads like something left behind by a deletion.
func (h *Hasher) SpendEquivalentWork(plaintext string) {
	key := argon2.IDKey([]byte(plaintext), decoySalt,
		h.profile.Iterations, h.profile.MemoryKiB, h.profile.Parallelism, keyLength)
	runtime.KeepAlive(key)
}

// NeedsRehash reports whether a stored hash was made at a weaker profile than this hasher
// writes, and should therefore be replaced.
//
// The caller for this is the sign-in path (SHIP-41): it has the plaintext in hand exactly once,
// which is the only moment a stronger hash can be written without asking anyone to reset
// anything.
func (h *Hasher) NeedsRehash(encoded string) (bool, error) {
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
			"%w: expected $argon2id$v=…$m=…,t=…,p=…$salt$key", ErrMalformedHash)
	}

	if fields[1] != phcVariant {
		return storedHash{}, fmt.Errorf("%w: variant is %q, not %s",
			ErrMalformedHash, fields[1], phcVariant)
	}

	version, err := keyedUint(fields[2], "v")
	if err != nil {
		return storedHash{}, err
	}
	if version != argon2.Version {
		return storedHash{}, fmt.Errorf("%w: version is %d, not %d",
			ErrMalformedHash, version, argon2.Version)
	}

	costs := strings.Split(fields[3], ",")
	if len(costs) != 3 {
		return storedHash{}, fmt.Errorf("%w: expected m, t and p, got %q",
			ErrMalformedHash, fields[3])
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
		parallelism > 255 {
		return storedHash{}, fmt.Errorf("%w: m=%d, t=%d, p=%d exceeds what this package will run",
			ErrInvalidProfile, memory, iterations, parallelism)
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
		return storedHash{}, fmt.Errorf("%w: the salt is not raw base64", ErrMalformedHash)
	}
	if len(salt) < argon2Bounds.minSaltLength || len(salt) > argon2Bounds.maxSaltLength {
		return storedHash{}, fmt.Errorf("%w: the salt is %d bytes, outside %d..%d",
			ErrMalformedHash, len(salt), argon2Bounds.minSaltLength, argon2Bounds.maxSaltLength)
	}

	key, err := base64.RawStdEncoding.DecodeString(fields[5])
	if err != nil {
		return storedHash{}, fmt.Errorf("%w: the key is not raw base64", ErrMalformedHash)
	}
	if len(key) < argon2Bounds.minKeyLength || len(key) > argon2Bounds.maxKeyLength {
		return storedHash{}, fmt.Errorf("%w: the key is %d bytes, outside %d..%d",
			ErrMalformedHash, len(key), argon2Bounds.minKeyLength, argon2Bounds.maxKeyLength)
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
		return 0, fmt.Errorf("%w: expected %s=…, got %q", ErrMalformedHash, name, field)
	}
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %s=%q is not a number", ErrMalformedHash, name, value)
	}
	return n, nil
}
