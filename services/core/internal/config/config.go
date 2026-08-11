// Package config loads every runtime setting from the environment (SHIP-8).
//
// Two rules hold here, both from Docs/06 §5.1:
//
//   - All configuration comes from environment variables. Nothing is read from a
//     configuration file at runtime, and nothing that varies by environment is compiled
//     into the binary.
//   - No secret has a usable default outside development. The development defaults below
//     exist so that `make up && make run` works on a fresh clone; Load rejects them
//     outright in staging and production rather than trusting anyone to notice.
//
// deploy/.env.example documents the same variables for humans. If you add one here, add
// it there too — a setting nobody can find is a setting nobody sets.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment selects behaviour that legitimately differs between deployments — chiefly
// which adapter implementations are used. In development, email and SMS log to the
// console instead of dispatching (Docs/06 §4.1, SHIP-32, SHIP-35).
type Environment string

const (
	Development Environment = "development"
	Staging     Environment = "staging"
	Production  Environment = "production"
)

func (e Environment) IsDevelopment() bool { return e == Development }

func (e Environment) valid() bool {
	switch e {
	case Development, Staging, Production:
		return true
	}
	return false
}

// Config is the whole of the service's configuration.
type Config struct {
	Env         Environment
	HTTP        HTTP
	Log         Log
	Database    Database
	Redis       Redis
	Kafka       Kafka
	Idempotency Idempotency
	Identity    Identity
	Email       Email
	SMS         SMS
	App         App
}

// Email configures the transactional email adapter (SHIP-32), first consumed by the
// verification message SHIP-31 sends.
//
// Which implementation is built is decided from Env and not from here: development logs to the
// console and sends nothing; staging and production hand the message to the provider. These
// three values are all the provider needs, because the adapter speaks a generic HTTP contract
// rather than a vendor's SDK — so naming a vendor is setting them and changing no code
// (Docs/11 §7).
//
// They are deliberately not validated here. An empty base URL is correct in development, and in
// staging it is caught where the adapter is constructed, which is at startup and with a message
// that says which variable is missing.
type Email struct {
	// ProviderBaseURL is the root of the vendor's API. Empty in development, where nothing
	// reaches it.
	ProviderBaseURL string

	// ProviderAPIKey is presented as a bearer credential and is never logged.
	ProviderAPIKey string

	// Sender is the From address every message is dispatched with. One address for the whole
	// service in the MVP; per-domain senders are a deliverability decision nobody has needed
	// to make yet.
	Sender string
}

// Identity configures credentials and sessions (SHIP-29, SHIP-37).
//
// Both halves are configuration rather than compiled-in constants because both are expected to
// change without a release: the argon2id cost is raised as hardware improves, and a signing key
// is rotated on a schedule or in a hurry. Docs/10 §5 is the specification.
type Identity struct {
	// Argon2 is the cost new password hashes are written at.
	//
	// Verification does not read it. The parameters travel with each hash in its PHC string,
	// so raising these values leaves every stored password verifiable and upgrades each one at
	// its owner's next sign-in, with no migration (Docs/10 §5).
	Argon2 Argon2

	// AccessTokenTTL is how long an issued access token stays valid. Docs/10 §5 fixes it at
	// fifteen minutes: long enough that a phone on a poor connection is not refreshing
	// constantly, short enough that revoking a device session takes effect quickly — an
	// access token is not checked against the database, so nothing shortens it once issued.
	AccessTokenTTL time.Duration

	// AccessTokenKeys is the HMAC signing keyset, by key identifier.
	//
	// More than one so a key can be rotated by configuration: the incoming key becomes active
	// and signs, while the outgoing one stays in the set and keeps verifying the tokens it
	// already signed until the last of them expires.
	AccessTokenKeys map[string][]byte

	// AccessTokenActiveKID names the key in AccessTokenKeys that signs new tokens. It travels
	// in each token's `kid` header, which is how a verifier knows which key to use before it
	// can trust anything else in the token.
	AccessTokenActiveKID string
}

// SMS configures the text-message adapter (SHIP-35), first consumed by the phone verification
// code SHIP-34 sends.
//
// The same shape as [Email] and chosen the same way — from Env, not from here. It matters more
// here than it does for email: a message costs money and reaches a real handset, so an
// environment that dispatched by accident would be a bill as well as a nuisance to whoever last
// used that number for testing.
type SMS struct {
	// ProviderBaseURL is the root of the gateway's API. Empty in development.
	ProviderBaseURL string

	// ProviderAPIKey is presented as a bearer credential and is never logged.
	ProviderAPIKey string

	// Sender is what the message appears to come from — an alphanumeric sender ID or an
	// originating number, depending on what the gateway and the destination country permit.
	// Australia allows both; the choice is made with the vendor.
	Sender string
}

// Argon2 is the password hashing cost.
//
// It mirrors identity.Argon2Profile, which is the type the domain takes. Two shapes rather than
// one shared type on purpose: internal/config does not import a domain, the domain does not
// import configuration, and the two meet in cmd/api — which is the rule that keeps every domain
// independently buildable (Docs/06 §4.1).
type Argon2 struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
}

// App is what the mobile client is told about itself at launch (SHIP-167).
//
// It lives in configuration rather than in code because Docs/07 §6 makes raising the floor "an
// operational decision with an owner, not a side effect of a deploy" — and because Flutter has
// no over-the-air path for Dart code, so anything that has to change under pressure has to
// change server-side (Docs/06 §5.3).
type App struct {
	// MinimumIOSBuild and MinimumAndroidBuild are build numbers, not version strings.
	//
	// Docs/07 §8 requires every build to carry a unique, monotonically increasing build
	// number, which makes the comparison an integer one. Comparing semantic versions
	// would mean agreeing on an ordering for pre-release suffixes, and getting that
	// slightly wrong would either lock out a valid build or admit one that should have
	// been blocked.
	MinimumIOSBuild     int
	MinimumAndroidBuild int

	// IOSStoreURL and AndroidStoreURL are where a blocked build sends the user.
	//
	// Empty during the pilot: distribution is TestFlight and Play internal testing with no
	// public listing (Docs/01 §8), so there is no store page to link to yet. The client
	// shows the prompt without a link when these are blank, which is why an empty value is
	// allowed rather than refused at startup.
	IOSStoreURL     string
	AndroidStoreURL string
}

// HTTP configures the public API listener.
type HTTP struct {
	Port int

	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration

	// ShutdownTimeout is how long in-flight requests get to finish after a termination
	// signal before they are dropped.
	ShutdownTimeout time.Duration
}

// Addr is the listen address, in the form ":8080".
func (h HTTP) Addr() string { return ":" + strconv.Itoa(h.Port) }

// Log configures structured logging (SHIP-9).
type Log struct {
	Level slog.Level

	// Format is "json" or "text". Anything that ships logs to Datadog must use json
	// (SHIP-174); text exists purely because a developer reading a terminal is not a
	// log aggregator.
	Format string
}

// Database configures the connection to PostgreSQL, which is the source of truth for
// every business decision (Docs/06 §4).
type Database struct {
	URL string

	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// Redis holds refresh-token state, the device registry, idempotency keys, and rate
// limits. It must never be the only copy of a job, bid, or status (Docs/06 §2.1, §4).
type Redis struct {
	URL string
}

// Kafka is the event backbone for notifications, reporting, and background processing.
type Kafka struct {
	Brokers []string
}

// Idempotency configures how long the platform remembers what it answered a
// state-changing request, so that a retry replays rather than repeats it (SHIP-15).
type Idempotency struct {
	// TTL is how long a completed response stays replayable.
	//
	// It has to outlast the client's retry behaviour, not the request. A phone can be
	// out of coverage for hours and drain its queue on the drive home (SHIP-124,
	// SHIP-125), so this is measured in hours rather than minutes.
	TTL time.Duration

	// InFlightTTL bounds how long a claim survives with no response recorded against it,
	// which is what happens when the process handling the original request dies. Until
	// it lapses, retries of that request are refused — so it wants to be comfortably
	// longer than the slowest handler and much shorter than TTL.
	InFlightTTL time.Duration
}

// credentialBearingDefaults are variables whose built-in defaults embed a local
// throwaway credential. They make a fresh clone work against docker-compose and are
// refused outside development — see loader.validate.
var credentialBearingDefaults = []string{"DATABASE_URL", "REDIS_URL", "IDENTITY_ACCESS_TOKEN_KEYS"}

// The development signing key, which is in the repository and therefore public.
//
// It exists for the same reason the database password does: a fresh clone has to run without
// setup. validate refuses it outside development, so the failure mode it is guarding against —
// a staging deployment signing tokens anybody can forge — is a startup error rather than a
// discovery.
const (
	developmentActiveKID  = "dev"
	developmentSigningKey = "shipper-local-development-signing-key-not-a-secret"
)

// developmentSigningKeys builds a fresh map each time rather than sharing one package-level
// value, so that a caller holding the loaded configuration cannot change what the next Load
// returns.
func developmentSigningKeys() map[string][]byte {
	return map[string][]byte{developmentActiveKID: []byte(developmentSigningKey)}
}

// minimumSigningKeyBytes mirrors the same constant in internal/identity, which is where the
// keyset enforces it.
//
// Two copies rather than one shared constant, because internal/config does not import a domain
// and the domain does not import configuration (Docs/06 §4.1). Checking it here as well means a
// short key is refused at startup rather than at the first sign-in.
const minimumSigningKeyBytes = 32

// Load reads configuration from the process environment.
//
// It reports every problem it finds rather than stopping at the first, because a
// misconfigured deployment usually has more than one thing wrong with it and discovering
// them one restart at a time is miserable.
func Load() (*Config, error) {
	l := &loader{defaulted: map[string]bool{}}

	cfg := &Config{
		Env: Environment(strings.ToLower(l.str("SHIPPER_ENV", string(Development)))),
		HTTP: HTTP{
			Port:            l.port("HTTP_PORT", 8080),
			ReadTimeout:     l.duration("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:    l.duration("HTTP_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:     l.duration("HTTP_IDLE_TIMEOUT", 120*time.Second),
			ShutdownTimeout: l.duration("HTTP_SHUTDOWN_TIMEOUT", 15*time.Second),
		},
		Log: Log{
			Level:  l.level("LOG_LEVEL", slog.LevelInfo),
			Format: strings.ToLower(l.str("LOG_FORMAT", "json")),
		},
		Database: Database{
			URL:             l.str("DATABASE_URL", "postgres://shipper:shipper@localhost:5432/shipper?sslmode=disable"),
			MaxOpenConns:    l.positiveInt("DATABASE_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    l.positiveInt("DATABASE_MAX_IDLE_CONNS", 5),
			ConnMaxLifetime: l.duration("DATABASE_CONN_MAX_LIFETIME", 30*time.Minute),
		},
		Redis: Redis{
			URL: l.str("REDIS_URL", "redis://localhost:6379/0"),
		},
		Kafka: Kafka{
			Brokers: l.csv("KAFKA_BROKERS", []string{"localhost:29092"}),
		},
		Idempotency: Idempotency{
			TTL:         l.duration("IDEMPOTENCY_TTL", 24*time.Hour),
			InFlightTTL: l.duration("IDEMPOTENCY_IN_FLIGHT_TTL", 60*time.Second),
		},
		Identity: Identity{
			// m=64 MiB, t=3, p=4 — Docs/10 §5. The bounds are the range the identity
			// package will run; a value outside them is refused here rather than at the
			// first sign-in.
			Argon2: Argon2{
				MemoryKiB:   uint32(l.boundedInt("IDENTITY_ARGON2_MEMORY_KIB", 64*1024, 1024, 1<<20)),
				Iterations:  uint32(l.boundedInt("IDENTITY_ARGON2_ITERATIONS", 3, 1, 64)),
				Parallelism: uint8(l.boundedInt("IDENTITY_ARGON2_PARALLELISM", 4, 1, 255)),
			},
			AccessTokenTTL:       l.duration("IDENTITY_ACCESS_TOKEN_TTL", 15*time.Minute),
			AccessTokenKeys:      l.signingKeys("IDENTITY_ACCESS_TOKEN_KEYS", developmentSigningKeys()),
			AccessTokenActiveKID: l.str("IDENTITY_ACCESS_TOKEN_ACTIVE_KID", developmentActiveKID),
		},
		Email: Email{
			ProviderBaseURL: l.str("EMAIL_PROVIDER_BASE_URL", ""),
			ProviderAPIKey:  l.str("EMAIL_PROVIDER_API_KEY", ""),
			Sender:          l.str("EMAIL_SENDER", "no-reply@shipper.com.au"),
		},
		SMS: SMS{
			ProviderBaseURL: l.str("SMS_PROVIDER_BASE_URL", ""),
			ProviderAPIKey:  l.str("SMS_PROVIDER_API_KEY", ""),
			Sender:          l.str("SMS_SENDER", "Shipper"),
		},
		App: App{
			MinimumIOSBuild:     l.positiveInt("MIN_SUPPORTED_IOS_BUILD", 1),
			MinimumAndroidBuild: l.positiveInt("MIN_SUPPORTED_ANDROID_BUILD", 1),
			IOSStoreURL:         l.str("IOS_STORE_URL", ""),
			AndroidStoreURL:     l.str("ANDROID_STORE_URL", ""),
		},
	}

	l.validate(cfg)

	if err := errors.Join(l.errs...); err != nil {
		return nil, fmt.Errorf("invalid configuration:\n%w", err)
	}
	return cfg, nil
}

// LogValue renders the configuration for a startup log line with credentials removed.
//
// Implementing slog.LogValuer rather than exposing a String method means there is no
// convenient way to print the unredacted struct by accident.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("env", string(c.Env)),
		slog.Int("http_port", c.HTTP.Port),
		slog.String("log_level", c.Log.Level.String()),
		slog.String("log_format", c.Log.Format),
		slog.String("database_url", redactURL(c.Database.URL)),
		slog.String("redis_url", redactURL(c.Redis.URL)),
		slog.String("kafka_brokers", strings.Join(c.Kafka.Brokers, ",")),
		slog.Duration("idempotency_ttl", c.Idempotency.TTL),
		slog.Duration("idempotency_in_flight_ttl", c.Idempotency.InFlightTTL),
		// The cost is worth having in the startup line: a deployment that has silently
		// fallen back to a cheap profile is otherwise invisible.
		slog.String("argon2", fmt.Sprintf("m=%d,t=%d,p=%d",
			c.Identity.Argon2.MemoryKiB, c.Identity.Argon2.Iterations, c.Identity.Argon2.Parallelism)),
		slog.Duration("access_token_ttl", c.Identity.AccessTokenTTL),
		// The identifier and the count, never the key material. Which key is active and how
		// many are loaded is exactly what a rotation needs to confirm from a log line, and
		// neither says anything an attacker can sign with.
		slog.String("access_token_active_kid", c.Identity.AccessTokenActiveKID),
		slog.Int("access_token_keys", len(c.Identity.AccessTokenKeys)),
		// Whether a base URL is set, not what it is, and never the key. "Email is going to
		// the console" is the line somebody needs when a verification message has not
		// arrived, and it is the one thing a hostname would not tell them.
		slog.Bool("email_provider_configured", c.Email.ProviderBaseURL != ""),
		slog.String("email_sender", c.Email.Sender),
		slog.Bool("sms_provider_configured", c.SMS.ProviderBaseURL != ""),
		slog.String("sms_sender", c.SMS.Sender),
	)
}

// redactURL replaces the password in a URL's userinfo with "xxxxx", leaving the rest
// intact so the host and database name stay diagnosable.
//
// A URL that cannot be parsed is reported as "<unparseable>" rather than echoed back:
// the one thing worse than an unhelpful log line is one that leaks the credential it
// failed to parse.
func redactURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "<unparseable>"
	}
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(u.User.Username(), "xxxxx")
		}
	}
	return u.String()
}

// loader accumulates parse failures and records which variables fell back to a default,
// so validate can refuse credential-bearing defaults outside development.
type loader struct {
	errs      []error
	defaulted map[string]bool
}

func (l *loader) lookup(key string) (string, bool) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		l.defaulted[key] = true
		return "", false
	}
	return strings.TrimSpace(v), true
}

func (l *loader) str(key, def string) string {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}
	return v
}

func (l *loader) csv(key string, def []string) []string {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		l.errf("%s: must list at least one value", key)
		return def
	}
	return out
}

func (l *loader) positiveInt(key string, def int) int {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.errf("%s: %q is not a whole number", key, v)
		return def
	}
	if n <= 0 {
		l.errf("%s: must be greater than zero, got %d", key, n)
		return def
	}
	return n
}

// signingKeys reads a keyset written as comma-separated `kid:base64secret` pairs.
//
// One variable rather than one per key, because the number of keys changes during a rotation and
// a scheme that needs a new variable name to add a key is a scheme nobody rotates. Standard
// base64 for the secret so that key material is bytes rather than whatever survived a shell.
//
// No error message here ever contains a secret, only the identifier it was filed under. A
// configuration error that echoes the value it could not parse puts key material in the startup
// log, where it is collected, shipped and retained.
func (l *loader) signingKeys(key string, def map[string][]byte) map[string][]byte {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}

	keys := map[string][]byte{}
	for _, pair := range strings.Split(v, ",") {
		if pair = strings.TrimSpace(pair); pair == "" {
			continue
		}

		kid, encoded, separated := strings.Cut(pair, ":")
		kid = strings.TrimSpace(kid)
		if !separated || kid == "" {
			l.errf("%s: expected kid:base64secret pairs; one entry has no key identifier", key)
			continue
		}
		if _, duplicate := keys[kid]; duplicate {
			l.errf("%s: the key identifier %q appears twice", key, kid)
			continue
		}

		secret, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err != nil {
			l.errf("%s: the secret for key %q is not standard base64", key, kid)
			continue
		}
		if len(secret) < minimumSigningKeyBytes {
			l.errf("%s: the secret for key %q is %d bytes; HS256 wants at least %d",
				key, kid, len(secret), minimumSigningKeyBytes)
			continue
		}

		keys[kid] = secret
	}

	if len(keys) == 0 {
		l.errf("%s: no usable signing key", key)
		return def
	}
	return keys
}

// boundedInt reads a whole number that has a range, and reports the range when it is missed.
//
// The alternative — accept anything positive and fail later where the value is used — produces
// an error a long way from the variable that caused it. A cost parameter that is out of range is
// a startup problem, and this is startup.
func (l *loader) boundedInt(key string, def, low, high int) int {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.errf("%s: %q is not a whole number", key, v)
		return def
	}
	if n < low || n > high {
		l.errf("%s: must be between %d and %d, got %d", key, low, high, n)
		return def
	}
	return n
}

func (l *loader) port(key string, def int) int {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.errf("%s: %q is not a whole number", key, v)
		return def
	}
	if n < 1 || n > 65535 {
		l.errf("%s: must be a port between 1 and 65535, got %d", key, n)
		return def
	}
	return n
}

func (l *loader) duration(key string, def time.Duration) time.Duration {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		l.errf("%s: %q is not a duration (try 15s, 2m, 1h)", key, v)
		return def
	}
	if d <= 0 {
		l.errf("%s: must be greater than zero, got %s", key, d)
		return def
	}
	return d
}

func (l *loader) level(key string, def slog.Level) slog.Level {
	v, ok := l.lookup(key)
	if !ok {
		return def
	}
	var lvl slog.Level
	// slog's own text unmarshaller expects upper case; accept any case for the humans.
	if err := lvl.UnmarshalText([]byte(strings.ToUpper(v))); err != nil {
		l.errf("%s: %q is not a level (want debug, info, warn, or error)", key, v)
		return def
	}
	return lvl
}

func (l *loader) errf(format string, args ...any) {
	l.errs = append(l.errs, fmt.Errorf(format, args...))
}

// validate applies the cross-field rules that no individual parser can see.
func (l *loader) validate(cfg *Config) {
	if !cfg.Env.valid() {
		l.errf("SHIPPER_ENV: %q is not an environment (want development, staging, or production)", cfg.Env)
	}

	if cfg.Log.Format != "json" && cfg.Log.Format != "text" {
		l.errf("LOG_FORMAT: %q is not a format (want json or text)", cfg.Log.Format)
	}

	if cfg.Database.MaxIdleConns > cfg.Database.MaxOpenConns {
		l.errf("DATABASE_MAX_IDLE_CONNS (%d) cannot exceed DATABASE_MAX_OPEN_CONNS (%d)",
			cfg.Database.MaxIdleConns, cfg.Database.MaxOpenConns)
	}

	// A claim that outlived the replay window would refuse a client's retries for longer
	// than it could ever answer them, which is the worst of both.
	if cfg.Idempotency.InFlightTTL > cfg.Idempotency.TTL {
		l.errf("IDEMPOTENCY_IN_FLIGHT_TTL (%s) cannot exceed IDEMPOTENCY_TTL (%s)",
			cfg.Idempotency.InFlightTTL, cfg.Idempotency.TTL)
	}

	// argon2 divides its memory between the lanes and rounds down, so a profile with more
	// lanes than kibibytes to give them is not a slow hash but an undefined one.
	if cfg.Identity.Argon2.MemoryKiB < 8*uint32(cfg.Identity.Argon2.Parallelism) {
		l.errf("IDENTITY_ARGON2_MEMORY_KIB (%d) leaves less than 8 KiB for each of "+
			"IDENTITY_ARGON2_PARALLELISM (%d) lanes",
			cfg.Identity.Argon2.MemoryKiB, cfg.Identity.Argon2.Parallelism)
	}

	// A signature nobody can produce is not a token anybody can use, and this is the only
	// place it can be caught: the failure is otherwise the first sign-in after a deploy.
	if _, ok := cfg.Identity.AccessTokenKeys[cfg.Identity.AccessTokenActiveKID]; !ok {
		l.errf("IDENTITY_ACCESS_TOKEN_ACTIVE_KID: %q names no key in IDENTITY_ACCESS_TOKEN_KEYS",
			cfg.Identity.AccessTokenActiveKID)
	}

	// An access token is not checked against the database, so nothing can shorten one that has
	// already been issued: a long TTL quietly turns "revoke this device" into "revoke this
	// device, eventually". The cap is generous rather than exact — Docs/10 §5 says fifteen
	// minutes, and this only refuses the values that would defeat the design.
	if cfg.Identity.AccessTokenTTL > time.Hour {
		l.errf("IDENTITY_ACCESS_TOKEN_TTL (%s) is longer than an hour; an issued access token "+
			"cannot be revoked before it expires", cfg.Identity.AccessTokenTTL)
	}

	// Everything below this point is a deployment-safety rule. Development is exempt by
	// design: the whole point of the defaults is that a fresh clone runs without setup.
	if cfg.Env.IsDevelopment() || !cfg.Env.valid() {
		return
	}

	for _, key := range credentialBearingDefaults {
		if l.defaulted[key] {
			l.errf("%s: must be set explicitly when SHIPPER_ENV is %s — "+
				"its default embeds a local development credential", key, cfg.Env)
		}
	}

	// Text logs outside development mean the log aggregator receives unparseable lines,
	// which is discovered during the first incident rather than before it (SHIP-174).
	if cfg.Log.Format == "text" {
		l.errf("LOG_FORMAT: must be json when SHIPPER_ENV is %s", cfg.Env)
	}

	if strings.Contains(cfg.Database.URL, "sslmode=disable") {
		l.errf("DATABASE_URL: sslmode=disable is not permitted when SHIPPER_ENV is %s", cfg.Env)
	}

	// The credential-default rule above catches a variable that was never set. This catches
	// the other way in: the development key copied out of deploy/.env.example into a real
	// environment, which is a signing key published in this repository.
	//
	// Only when the variable was set explicitly — an unset one has already been refused above,
	// and reporting it twice would say the same thing in two ways.
	if !l.defaulted["IDENTITY_ACCESS_TOKEN_KEYS"] {
		for kid, secret := range cfg.Identity.AccessTokenKeys {
			if string(secret) == developmentSigningKey {
				l.errf("IDENTITY_ACCESS_TOKEN_KEYS: key %q is the development key from "+
					"deploy/.env.example, which is public; it may not be used when "+
					"SHIPPER_ENV is %s", kid, cfg.Env)
			}
		}
	}
}
