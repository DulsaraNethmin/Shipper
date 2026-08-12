package config

import (
	"encoding/base64"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// allKeys is every variable Load reads. Tests blank them so a value left behind by the
// developer's shell — or by an earlier test — cannot change the outcome.
var allKeys = []string{
	"SHIPPER_ENV",
	"HTTP_PORT", "HTTP_READ_TIMEOUT", "HTTP_WRITE_TIMEOUT", "HTTP_IDLE_TIMEOUT", "HTTP_SHUTDOWN_TIMEOUT",
	"LOG_LEVEL", "LOG_FORMAT",
	"DATABASE_URL", "DATABASE_MAX_OPEN_CONNS", "DATABASE_MAX_IDLE_CONNS", "DATABASE_CONN_MAX_LIFETIME",
	"REDIS_URL",
	"KAFKA_BROKERS",
	"IDENTITY_ARGON2_MEMORY_KIB", "IDENTITY_ARGON2_ITERATIONS", "IDENTITY_ARGON2_PARALLELISM",
	"IDENTITY_ACCESS_TOKEN_TTL", "IDENTITY_ACCESS_TOKEN_KEYS", "IDENTITY_ACCESS_TOKEN_ACTIVE_KID",
	"GEOCODING_BASE_URL", "GEOCODING_API_KEY",
	"PAGINATION_DEFAULT_PAGE_SIZE", "PAGINATION_MAX_PAGE_SIZE",
}

// deploymentSigningKeys is a keyset a staging or production configuration can legitimately be
// given: thirty-two bytes, and not the development key this repository publishes.
const deploymentSigningKeys = "2026-08:ZGVwbG95bWVudC1zaWduaW5nLWtleS0wMTIzNDU2YWM="

// clearEnv blanks every configuration variable for the duration of the test. An empty
// value is treated as absent by loader.lookup, which is what makes this equivalent to
// unsetting without losing t.Setenv's automatic restore.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range allKeys {
		t.Setenv(k, "")
	}
}

func TestLoadAppliesDocumentedDefaults(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned %v, want nil", err)
	}

	if cfg.Env != Development {
		t.Errorf("Env = %q, want %q", cfg.Env, Development)
	}
	if cfg.HTTP.Port != 8080 {
		t.Errorf("HTTP.Port = %d, want 8080", cfg.HTTP.Port)
	}
	if cfg.HTTP.Addr() != ":8080" {
		t.Errorf("HTTP.Addr() = %q, want \":8080\"", cfg.HTTP.Addr())
	}
	if cfg.HTTP.ShutdownTimeout != 15*time.Second {
		t.Errorf("HTTP.ShutdownTimeout = %s, want 15s", cfg.HTTP.ShutdownTimeout)
	}
	if cfg.Log.Level != slog.LevelInfo {
		t.Errorf("Log.Level = %s, want INFO", cfg.Log.Level)
	}
	if cfg.Log.Format != "json" {
		t.Errorf("Log.Format = %q, want \"json\"", cfg.Log.Format)
	}
	if len(cfg.Kafka.Brokers) != 1 || cfg.Kafka.Brokers[0] != "localhost:29092" {
		t.Errorf("Kafka.Brokers = %v, want [localhost:29092]", cfg.Kafka.Brokers)
	}
}

func TestLoadReadsEveryValueFromTheEnvironment(t *testing.T) {
	clearEnv(t)

	t.Setenv("SHIPPER_ENV", "staging")
	t.Setenv("HTTP_PORT", "9999")
	t.Setenv("HTTP_READ_TIMEOUT", "1m")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("DATABASE_URL", "postgres://u:p@db.internal:5432/shipper?sslmode=require")
	t.Setenv("DATABASE_MAX_OPEN_CONNS", "40")
	t.Setenv("DATABASE_MAX_IDLE_CONNS", "8")
	t.Setenv("REDIS_URL", "redis://cache.internal:6379/1")
	t.Setenv("KAFKA_BROKERS", "a:9092, b:9092 ,c:9092")
	t.Setenv("IDENTITY_ARGON2_MEMORY_KIB", "32768")
	t.Setenv("IDENTITY_ACCESS_TOKEN_TTL", "10m")
	t.Setenv("IDENTITY_ACCESS_TOKEN_KEYS", deploymentSigningKeys)
	t.Setenv("IDENTITY_ACCESS_TOKEN_ACTIVE_KID", "2026-08")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned %v, want nil", err)
	}

	if cfg.Env != Staging {
		t.Errorf("Env = %q, want staging", cfg.Env)
	}
	if cfg.HTTP.Port != 9999 {
		t.Errorf("HTTP.Port = %d, want 9999", cfg.HTTP.Port)
	}
	if cfg.HTTP.ReadTimeout != time.Minute {
		t.Errorf("HTTP.ReadTimeout = %s, want 1m", cfg.HTTP.ReadTimeout)
	}
	if cfg.Log.Level != slog.LevelDebug {
		t.Errorf("Log.Level = %s, want DEBUG", cfg.Log.Level)
	}
	if cfg.Database.MaxOpenConns != 40 {
		t.Errorf("Database.MaxOpenConns = %d, want 40", cfg.Database.MaxOpenConns)
	}
	if cfg.Identity.Argon2.MemoryKiB != 32768 {
		t.Errorf("Identity.Argon2.MemoryKiB = %d, want 32768", cfg.Identity.Argon2.MemoryKiB)
	}
	if cfg.Identity.AccessTokenTTL != 10*time.Minute {
		t.Errorf("Identity.AccessTokenTTL = %s, want 10m", cfg.Identity.AccessTokenTTL)
	}
	if cfg.Identity.AccessTokenActiveKID != "2026-08" {
		t.Errorf("Identity.AccessTokenActiveKID = %q, want 2026-08", cfg.Identity.AccessTokenActiveKID)
	}
	if got := len(cfg.Identity.AccessTokenKeys["2026-08"]); got != 32 {
		t.Errorf("the signing key decoded to %d bytes, want 32", got)
	}

	want := []string{"a:9092", "b:9092", "c:9092"}
	if len(cfg.Kafka.Brokers) != len(want) {
		t.Fatalf("Kafka.Brokers = %v, want %v", cfg.Kafka.Brokers, want)
	}
	for i := range want {
		if cfg.Kafka.Brokers[i] != want[i] {
			t.Errorf("Kafka.Brokers[%d] = %q, want %q — surrounding spaces should be trimmed",
				i, cfg.Kafka.Brokers[i], want[i])
		}
	}
}

func TestLoadAcceptsAnyCaseForLevelAndEnvironment(t *testing.T) {
	clearEnv(t)
	t.Setenv("SHIPPER_ENV", "DEVELOPMENT")
	t.Setenv("LOG_LEVEL", "WaRn")
	t.Setenv("LOG_FORMAT", "TEXT")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned %v, want nil", err)
	}
	if cfg.Env != Development {
		t.Errorf("Env = %q, want development", cfg.Env)
	}
	if cfg.Log.Level != slog.LevelWarn {
		t.Errorf("Log.Level = %s, want WARN", cfg.Log.Level)
	}
	if cfg.Log.Format != "text" {
		t.Errorf("Log.Format = %q, want text", cfg.Log.Format)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"unparseable port", map[string]string{"HTTP_PORT": "eighty"}, "HTTP_PORT"},
		{"out of range port", map[string]string{"HTTP_PORT": "70000"}, "between 1 and 65535"},
		{"unparseable duration", map[string]string{"HTTP_READ_TIMEOUT": "15"}, "is not a duration"},
		{"zero duration", map[string]string{"HTTP_IDLE_TIMEOUT": "0s"}, "greater than zero"},
		{"negative pool size", map[string]string{"DATABASE_MAX_OPEN_CONNS": "-1"}, "greater than zero"},
		{"unknown level", map[string]string{"LOG_LEVEL": "chatty"}, "is not a level"},
		{"unknown format", map[string]string{"LOG_FORMAT": "xml"}, "is not a format"},
		{"unknown environment", map[string]string{"SHIPPER_ENV": "qa"}, "is not an environment"},
		{"idle exceeds open", map[string]string{
			"DATABASE_MAX_OPEN_CONNS": "5", "DATABASE_MAX_IDLE_CONNS": "10",
		}, "cannot exceed"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			_, err := Load()
			if err == nil {
				t.Fatal("Load() returned nil, want an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error was %q, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

// Load is expected to report everything wrong at once rather than one restart at a time.
func TestLoadReportsEveryProblemAtOnce(t *testing.T) {
	clearEnv(t)
	t.Setenv("HTTP_PORT", "eighty")
	t.Setenv("LOG_LEVEL", "chatty")
	t.Setenv("LOG_FORMAT", "xml")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() returned nil, want an error")
	}
	for _, want := range []string{"HTTP_PORT", "LOG_LEVEL", "LOG_FORMAT"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error was %q, want it to mention %q as well", err, want)
		}
	}
}

// The rule from SHIP-8 is "no secrets in code". The development defaults embed a local
// throwaway credential, so the guard that stops them reaching a real deployment is the
// thing that actually enforces it.
func TestCredentialBearingDefaultsAreRefusedOutsideDevelopment(t *testing.T) {
	for _, env := range []string{"staging", "production"} {
		t.Run(env, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("SHIPPER_ENV", env)

			_, err := Load()
			if err == nil {
				t.Fatal("Load() returned nil, want an error")
			}
			for _, key := range credentialBearingDefaults {
				if !strings.Contains(err.Error(), key) {
					t.Errorf("error was %q, want it to refuse the defaulted %s", err, key)
				}
			}
		})
	}
}

func TestDevelopmentAcceptsTheDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("SHIPPER_ENV", "development")

	if _, err := Load(); err != nil {
		t.Fatalf("Load() returned %v — a fresh clone must run without configuration", err)
	}
}

func TestDeploymentGuards(t *testing.T) {
	base := map[string]string{
		"SHIPPER_ENV":                      "production",
		"DATABASE_URL":                     "postgres://u:p@db.internal:5432/shipper?sslmode=require",
		"REDIS_URL":                        "redis://cache.internal:6379/0",
		"LOG_FORMAT":                       "json",
		"IDENTITY_ACCESS_TOKEN_KEYS":       deploymentSigningKeys,
		"IDENTITY_ACCESS_TOKEN_ACTIVE_KID": "2026-08",
	}

	t.Run("valid production configuration loads", func(t *testing.T) {
		clearEnv(t)
		for k, v := range base {
			t.Setenv(k, v)
		}
		if _, err := Load(); err != nil {
			t.Fatalf("Load() returned %v, want nil", err)
		}
	})

	t.Run("text logs are refused", func(t *testing.T) {
		clearEnv(t)
		for k, v := range base {
			t.Setenv(k, v)
		}
		t.Setenv("LOG_FORMAT", "text")

		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), "LOG_FORMAT: must be json") {
			t.Fatalf("Load() returned %v, want a refusal of text logs", err)
		}
	})

	t.Run("unencrypted database connections are refused", func(t *testing.T) {
		clearEnv(t)
		for k, v := range base {
			t.Setenv(k, v)
		}
		t.Setenv("DATABASE_URL", "postgres://u:p@db.internal:5432/shipper?sslmode=disable")

		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), "sslmode=disable is not permitted") {
			t.Fatalf("Load() returned %v, want a refusal of sslmode=disable", err)
		}
	})

	// The published development key, set deliberately rather than left to default. The
	// credential-default guard does not see this one, and it is the likelier mistake: somebody
	// copies deploy/.env.example into a real environment and changes the parts that break.
	t.Run("the published development key is refused", func(t *testing.T) {
		clearEnv(t)
		for k, v := range base {
			t.Setenv(k, v)
		}
		t.Setenv("IDENTITY_ACCESS_TOKEN_KEYS",
			"dev:"+base64.StdEncoding.EncodeToString([]byte(developmentSigningKey)))
		t.Setenv("IDENTITY_ACCESS_TOKEN_ACTIVE_KID", "dev")

		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), "development key") {
			t.Fatalf("Load() returned %v, want a refusal of the published development key", err)
		}
	})
}

// TestIdentityConfiguration covers the two settings a deployment gets wrong quietly: a keyset
// that cannot sign, and a token lifetime that defeats revocation.
func TestIdentityConfiguration(t *testing.T) {
	t.Run("defaults follow Docs/10 §5", func(t *testing.T) {
		clearEnv(t)

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() returned %v, want nil", err)
		}
		if cfg.Identity.Argon2 != (Argon2{MemoryKiB: 65536, Iterations: 3, Parallelism: 4}) {
			t.Errorf("argon2 default is %+v, want m=64 MiB, t=3, p=4", cfg.Identity.Argon2)
		}
		if cfg.Identity.AccessTokenTTL != 15*time.Minute {
			t.Errorf("access token TTL default is %s, want 15m", cfg.Identity.AccessTokenTTL)
		}
	})

	t.Run("more than one key, so rotation is a configuration change", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("IDENTITY_ACCESS_TOKEN_KEYS",
			"old:b2xkLXNpZ25pbmcta2V5LTAxMjM0NTY3ODlhYmNkZWY=,new:bmV3LXNpZ25pbmcta2V5LTAxMjM0NTY3ODlhYmNkZWY=")
		t.Setenv("IDENTITY_ACCESS_TOKEN_ACTIVE_KID", "new")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() returned %v, want nil", err)
		}
		if len(cfg.Identity.AccessTokenKeys) != 2 {
			t.Fatalf("loaded %d keys, want 2 — the outgoing key must stay in the set",
				len(cfg.Identity.AccessTokenKeys))
		}
		if cfg.Identity.AccessTokenActiveKID != "new" {
			t.Errorf("active kid = %q, want new", cfg.Identity.AccessTokenActiveKID)
		}
	})

	bad := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"active key names nothing", map[string]string{
			"IDENTITY_ACCESS_TOKEN_ACTIVE_KID": "not-loaded",
		}, "names no key"},
		{"a key with no identifier", map[string]string{
			"IDENTITY_ACCESS_TOKEN_KEYS": "bm90LWEtcGFpci0wMTIzNDU2Nzg5YWJjZGVmZ2hpams=",
		}, "no key identifier"},
		{"a secret that is not base64", map[string]string{
			"IDENTITY_ACCESS_TOKEN_KEYS": "k1:not base64 at all",
		}, "not standard base64"},
		{"a secret too short for HS256", map[string]string{
			"IDENTITY_ACCESS_TOKEN_KEYS": "k1:c2hvcnQ=",
		}, "at least 32"},
		{"the same identifier twice", map[string]string{
			"IDENTITY_ACCESS_TOKEN_KEYS": "k1:b2xkLXNpZ25pbmcta2V5LTAxMjM0NTY3ODlhYmNkZWY=,k1:bmV3LXNpZ25pbmcta2V5LTAxMjM0NTY3ODlhYmNkZWY=",
		}, "appears twice"},
		{"a token lifetime that defeats revocation", map[string]string{
			"IDENTITY_ACCESS_TOKEN_TTL": "24h",
		}, "cannot be revoked before it expires"},
		{"argon2 memory below its lanes", map[string]string{
			"IDENTITY_ARGON2_MEMORY_KIB": "1024", "IDENTITY_ARGON2_PARALLELISM": "200",
		}, "leaves less than 8 KiB"},
		{"argon2 memory out of range", map[string]string{
			"IDENTITY_ARGON2_MEMORY_KIB": "16",
		}, "must be between"},
	}

	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			_, err := Load()
			if err == nil {
				t.Fatal("Load() returned nil, want an error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error was %q, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestRedactURL(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"password removed", "postgres://user:hunter2@host:5432/db", "postgres://user:xxxxx@host:5432/db"},
		{"username kept", "redis://alice:s3cret@cache:6379/0", "redis://alice:xxxxx@cache:6379/0"},
		{"no credentials untouched", "redis://cache:6379/0", "redis://cache:6379/0"},
		{"empty stays empty", "", ""},
		{"unparseable is not echoed", "postgres://user:pw@ho st:5432/\x7f", "<unparseable>"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := redactURL(tc.in); got != tc.want {
				t.Errorf("redactURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// A configuration logged at startup must not put the database password into the log
// aggregator, where it would then need rotating.
func TestLogValueOmitsCredentials(t *testing.T) {
	cfg := Config{
		Env:      Production,
		Database: Database{URL: "postgres://user:hunter2@host:5432/db"},
		Redis:    Redis{URL: "redis://alice:s3cret@cache:6379/0"},
		Log:      Log{Level: slog.LevelInfo, Format: "json"},
		Identity: Identity{
			AccessTokenActiveKID: "2026-08",
			AccessTokenKeys:      map[string][]byte{"2026-08": []byte("a-signing-key-nobody-should-see")},
		},
	}

	rendered := cfg.LogValue().String()
	for _, secret := range []string{"hunter2", "s3cret", "a-signing-key-nobody-should-see"} {
		if strings.Contains(rendered, secret) {
			t.Errorf("LogValue() rendered %q, which contains the credential %q", rendered, secret)
		}
	}
	if !strings.Contains(rendered, "host:5432") {
		t.Errorf("LogValue() rendered %q, want the host retained for diagnosis", rendered)
	}
	// Which key is active is what a rotation needs to confirm from a log line, and it says
	// nothing anybody can sign with.
	if !strings.Contains(rendered, "2026-08") {
		t.Errorf("LogValue() rendered %q, want the active key identifier retained", rendered)
	}
}

// SHIP-15g moved two parked requests into configuration: GEOCODING_* (SHIP-60) and the page sizes
// (SHIP-66). Both lanes had written a documented constant instead, because internal/config is a
// shared file a domain branch must not edit.

func TestGeocodingAndPaginationDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("GEOCODING_BASE_URL", "")
	t.Setenv("GEOCODING_API_KEY", "")
	t.Setenv("PAGINATION_DEFAULT_PAGE_SIZE", "")
	t.Setenv("PAGINATION_MAX_PAGE_SIZE", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Empty is the supported "no provider" state, not a missing setting: cmd/api builds the
	// stub in development and nothing outside it, and addresses are stored unresolved.
	if cfg.Geocoding.ProviderBaseURL != "" || cfg.Geocoding.ProviderAPIKey != "" {
		t.Errorf("geocoding defaults = %+v, want both empty", cfg.Geocoding)
	}
	if cfg.Pagination.DefaultPageSize != 20 || cfg.Pagination.MaxPageSize != 100 {
		t.Errorf("pagination defaults = %+v, want 20 and 100 (Docs/10 §4.5)", cfg.Pagination)
	}
}

func TestGeocodingAndPaginationAreReadFromTheEnvironment(t *testing.T) {
	clearEnv(t)
	t.Setenv("GEOCODING_BASE_URL", "https://geo.internal/v1")
	t.Setenv("GEOCODING_API_KEY", "a-key")
	t.Setenv("PAGINATION_DEFAULT_PAGE_SIZE", "25")
	t.Setenv("PAGINATION_MAX_PAGE_SIZE", "250")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Geocoding.ProviderBaseURL != "https://geo.internal/v1" || cfg.Geocoding.ProviderAPIKey != "a-key" {
		t.Errorf("geocoding = %+v", cfg.Geocoding)
	}
	if cfg.Pagination.DefaultPageSize != 25 || cfg.Pagination.MaxPageSize != 250 {
		t.Errorf("pagination = %+v", cfg.Pagination)
	}
}

// A default above the ceiling would give a request that asked for nothing a bigger page than one
// that asked for the maximum — obvious in a sentence, invisible in two variables.
func TestPaginationRefusesADefaultAboveTheMaximum(t *testing.T) {
	clearEnv(t)
	t.Setenv("PAGINATION_DEFAULT_PAGE_SIZE", "200")
	t.Setenv("PAGINATION_MAX_PAGE_SIZE", "100")

	if _, err := Load(); err == nil {
		t.Fatal("Load accepted a default page size above the maximum")
	} else if !strings.Contains(err.Error(), "PAGINATION_DEFAULT_PAGE_SIZE") {
		t.Errorf("the error does not name the variable: %v", err)
	}
}

// A key with no base URL builds no geocoder at all, so the only symptom would be addresses
// silently stored unresolved while a credential sits in the environment suggesting otherwise.
func TestGeocodingRefusesAKeyWithoutABaseURL(t *testing.T) {
	clearEnv(t)
	t.Setenv("GEOCODING_API_KEY", "a-key")
	t.Setenv("GEOCODING_BASE_URL", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load accepted GEOCODING_API_KEY with no GEOCODING_BASE_URL")
	} else if !strings.Contains(err.Error(), "GEOCODING_BASE_URL") {
		t.Errorf("the error does not name the missing variable: %v", err)
	}
}

// The reverse is legitimate: a local or self-hosted geocoder needs no credential.
func TestGeocodingAcceptsABaseURLWithoutAKey(t *testing.T) {
	clearEnv(t)
	t.Setenv("GEOCODING_BASE_URL", "http://localhost:8088")
	t.Setenv("GEOCODING_API_KEY", "")

	if _, err := Load(); err != nil {
		t.Fatalf("Load refused a base URL with no key: %v", err)
	}
}
