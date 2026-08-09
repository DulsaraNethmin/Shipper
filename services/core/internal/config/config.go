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
var credentialBearingDefaults = []string{"DATABASE_URL", "REDIS_URL"}

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
}
