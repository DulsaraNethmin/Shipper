package config

import (
	"encoding/base64"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/passwords"
)

// allKeys is every variable Load reads. Tests blank them so a value left behind by the
// developer's shell — or by an earlier test — cannot change the outcome.
var allKeys = []string{
	"SHIPPER_ENV",
	"HTTP_PORT", "HTTP_READ_TIMEOUT", "HTTP_WRITE_TIMEOUT", "HTTP_IDLE_TIMEOUT", "HTTP_SHUTDOWN_TIMEOUT",
	"LOG_LEVEL", "LOG_FORMAT",
	"DATABASE_URL", "DATABASE_MAX_OPEN_CONNS", "DATABASE_MAX_IDLE_CONNS", "DATABASE_CONN_MAX_LIFETIME",
	"REDIS_URL",
	"KAFKA_BROKERS", "KAFKA_REPLICATION_FACTOR",
	"PASSWORDS_ARGON2_MEMORY_KIB", "PASSWORDS_ARGON2_ITERATIONS", "PASSWORDS_ARGON2_PARALLELISM",
	"IDENTITY_ACCESS_TOKEN_TTL", "IDENTITY_ACCESS_TOKEN_KEYS", "IDENTITY_ACCESS_TOKEN_ACTIVE_KID",
	"DELIVERY_DRIVER_TOKEN_TTL", "DELIVERY_DRIVER_TOKEN_KEYS", "DELIVERY_DRIVER_TOKEN_ACTIVE_KID",
	"GEOCODING_BASE_URL", "GEOCODING_API_KEY",
	"PAGINATION_DEFAULT_PAGE_SIZE", "PAGINATION_MAX_PAGE_SIZE",
	"STORAGE_ENDPOINT", "STORAGE_BUCKET", "STORAGE_REGION",
	"STORAGE_ACCESS_KEY_ID", "STORAGE_SECRET_ACCESS_KEY", "STORAGE_USE_PATH_STYLE",
	"STORAGE_PRESIGN_TTL", "STORAGE_MAX_UPLOAD_BYTES", "STORAGE_ACCEPTED_CONTENT_TYPES",
	"UNSYNCED_NUDGE_AFTER", "PROOF_COMPRESSION_BUDGET_BYTES",
	// SHIP-183a's global lever and SHIP-183b's trusted proxy. The first four lines of this
	// list predate them and did not include the lever, which meant a deploy/.env setting one
	// leaked into every test here — make exports the file's contents, so the tests would have
	// been reading a developer's incident tuning.
	"RATE_LIMIT_BURST_SCALE", "RATE_LIMIT_RATE_SCALE",
	"TRUSTED_PROXY_HOPS", "TRUSTED_PROXY_NETWORKS",
}

// deploymentStorageCredentials is a credential a staging or production configuration can
// legitimately be given: not the pair this repository publishes in deploy/.env.example.
const (
	deploymentStorageAccessKeyID     = "AKIAIOSFODNN7EXAMPLE"
	deploymentStorageSecretAccessKey = "wJalrXUtnFEMI-K7MDENG-bPxRfiCYEXAMPLEKEY"
)

// deploymentSigningKeys is a keyset a staging or production configuration can legitimately be
// given: thirty-two bytes, and not the development key this repository publishes.
const deploymentSigningKeys = "2026-08:ZGVwbG95bWVudC1zaWduaW5nLWtleS0wMTIzNDU2YWM="

// deploymentDriverSigningKeys is the same for the driver token, and it is deliberately a different
// secret: Docs/10 §5 requires separate signing key material and validate refuses a configuration in
// which the two sets share one (SHIP-107).
const deploymentDriverSigningKeys = "2026-08:ZGVwbG95bWVudC1kcml2ZXItdG9rZW4ta2V5LTAxMjM0NTY3"

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
	// One replica, which is all a single-broker compose stack can do. cmd/topics holds this
	// against the event catalogue's own constant.
	if cfg.Kafka.ReplicationFactor != DefaultKafkaReplicationFactor {
		t.Errorf("Kafka.ReplicationFactor = %d, want %d",
			cfg.Kafka.ReplicationFactor, DefaultKafkaReplicationFactor)
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
	t.Setenv("PASSWORDS_ARGON2_MEMORY_KIB", "32768")
	t.Setenv("IDENTITY_ACCESS_TOKEN_TTL", "10m")
	t.Setenv("IDENTITY_ACCESS_TOKEN_KEYS", deploymentSigningKeys)
	t.Setenv("IDENTITY_ACCESS_TOKEN_ACTIVE_KID", "2026-08")
	t.Setenv("DELIVERY_DRIVER_TOKEN_TTL", "72h")
	t.Setenv("DELIVERY_DRIVER_TOKEN_KEYS", deploymentDriverSigningKeys)
	t.Setenv("DELIVERY_DRIVER_TOKEN_ACTIVE_KID", "2026-08")
	t.Setenv("STORAGE_ENDPOINT", "https://s3.ap-southeast-2.amazonaws.com")
	t.Setenv("STORAGE_ACCESS_KEY_ID", deploymentStorageAccessKeyID)
	t.Setenv("STORAGE_SECRET_ACCESS_KEY", deploymentStorageSecretAccessKey)

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
	if cfg.Passwords.Argon2.MemoryKiB != 32768 {
		t.Errorf("Passwords.Argon2.MemoryKiB = %d, want 32768", cfg.Passwords.Argon2.MemoryKiB)
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
		// A topic needs at least one replica, and 30 typed for 3 is refused before a
		// connection is opened rather than by the broker.
		{"no replicas", map[string]string{"KAFKA_REPLICATION_FACTOR": "0"}, "between 1 and 10"},
		{"absurd replication", map[string]string{"KAFKA_REPLICATION_FACTOR": "30"}, "between 1 and 10"},
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
		"DELIVERY_DRIVER_TOKEN_KEYS":       deploymentDriverSigningKeys,
		"DELIVERY_DRIVER_TOKEN_ACTIVE_KID": "2026-08",
		"STORAGE_ENDPOINT":                 "https://s3.ap-southeast-2.amazonaws.com",
		"STORAGE_ACCESS_KEY_ID":            deploymentStorageAccessKeyID,
		"STORAGE_SECRET_ACCESS_KEY":        deploymentStorageSecretAccessKey,
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

	// The same mistake on the object store's credential, and it is worth its own case because the
	// consequence differs: a published signing key forges sessions, a published storage secret
	// hands over a bucket of proof photographs and verification documents.
	t.Run("the published development storage credential is refused", func(t *testing.T) {
		clearEnv(t)
		for k, v := range base {
			t.Setenv(k, v)
		}
		t.Setenv("STORAGE_SECRET_ACCESS_KEY", developmentStorageSecretAccessKey)

		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), "STORAGE_SECRET_ACCESS_KEY") {
			t.Fatalf("Load() returned %v, want a refusal of the published development credential", err)
		}
	})

	// An endpoint carries no credential, so the rule above cannot see a deployment still pointing
	// at the development container. The symptom would be a proof photograph the platform believes
	// it stored and nobody can ever retrieve — the same class as sslmode=disable above.
	t.Run("a loopback object store is refused", func(t *testing.T) {
		clearEnv(t)
		for k, v := range base {
			t.Setenv(k, v)
		}
		t.Setenv("STORAGE_ENDPOINT", "http://localhost:9000")

		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), "STORAGE_ENDPOINT") {
			t.Fatalf("Load() returned %v, want a refusal of a loopback object store", err)
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
		if cfg.Passwords.Argon2 != (Argon2{MemoryKiB: 65536, Iterations: 3, Parallelism: 4}) {
			t.Errorf("argon2 default is %+v, want m=64 MiB, t=3, p=4", cfg.Passwords.Argon2)
		}

		// The same three numbers are stated a second time, in passwords.ProductionArgon2Profile,
		// as the profile Docs/10 §5 fixes. Nothing reads it — the default above is compiled in
		// here — so the two cannot diverge in behaviour today, and could tomorrow.
		//
		// **Held together in a test rather than unified in the code**, deliberately: importing
		// internal/passwords from internal/config would be infrastructure on infrastructure and
		// permitted, but it would put argon2 in the link graph of cmd/migrate for a value that is
		// three integers. A test import costs nothing and gives the same guarantee — the copies
		// cannot drift apart without this failing (SHIP-147a).
		reference := passwords.ProductionArgon2Profile
		if cfg.Passwords.Argon2 != (Argon2{
			MemoryKiB:   reference.MemoryKiB,
			Iterations:  reference.Iterations,
			Parallelism: reference.Parallelism,
		}) {
			t.Errorf("the default here is %+v and passwords.ProductionArgon2Profile is %+v.\n"+
				"These are the platform's password cost written down twice. They agree by "+
				"comment until somebody raises one, which is what Docs/10 §3.4 refuses.",
				cfg.Passwords.Argon2, reference)
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
			"PASSWORDS_ARGON2_MEMORY_KIB": "1024", "PASSWORDS_ARGON2_PARALLELISM": "200",
		}, "leaves less than 8 KiB"},
		{"argon2 memory out of range", map[string]string{
			"PASSWORDS_ARGON2_MEMORY_KIB": "16",
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

// --- the driver's job-scoped token (SHIP-107) --------------------------------------------------

// TestTheTwoTokenKeysetsMayNotShareASecret is Docs/10 §5's "separate signing key material", enforced
// rather than documented.
//
// The audience check refuses a token from either system on the other side even when the keys are
// shared, and internal/delivery has a test that proves exactly that. This is the second lock: the
// arrangement where the invariant rests on one check while every log line and every response looks
// entirely correct is the one worth refusing at startup.
//
// Checked in development too, which is why this test does not set SHIPPER_ENV: it is a structural
// rule rather than a deployment-hardening one, and a developer who set both variables to the same
// value should be told immediately.
func TestTheTwoTokenKeysetsMayNotShareASecret(t *testing.T) {
	clearEnv(t)
	t.Setenv("IDENTITY_ACCESS_TOKEN_KEYS", deploymentSigningKeys)
	t.Setenv("IDENTITY_ACCESS_TOKEN_ACTIVE_KID", "2026-08")
	// The same bytes under a different identifier, which is the form the mistake actually takes:
	// somebody copies a value rather than a variable name.
	t.Setenv("DELIVERY_DRIVER_TOKEN_KEYS", "driver-2026-08:"+strings.SplitN(deploymentSigningKeys, ":", 2)[1])
	t.Setenv("DELIVERY_DRIVER_TOKEN_ACTIVE_KID", "driver-2026-08")

	_, err := Load()
	if err == nil {
		t.Fatal("Load accepted one secret shared between the mobile and driver token systems")
	}
	if !strings.Contains(err.Error(), "DELIVERY_DRIVER_TOKEN_KEYS") {
		t.Errorf("the error does not name the variable: %v", err)
	}
}

// TestTheDevelopmentDefaultsAreTwoDifferentKeys.
//
// The pair above is only enforced when both are set; this is the case nobody sets anything, which
// is every developer machine. Sharing one development key would leave the two systems separated by
// the audience alone everywhere anybody works.
func TestTheDevelopmentDefaultsAreTwoDifferentKeys(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned %v", err)
	}

	access := cfg.Identity.AccessTokenKeys[cfg.Identity.AccessTokenActiveKID]
	driver := cfg.Delivery.DriverTokenKeys[cfg.Delivery.DriverTokenActiveKID]

	if len(access) == 0 || len(driver) == 0 {
		t.Fatal("one of the development keysets has no active key")
	}
	if string(access) == string(driver) {
		t.Error("the development defaults sign both token systems with one key")
	}
	if cfg.Identity.AccessTokenActiveKID == cfg.Delivery.DriverTokenActiveKID {
		t.Error("the two development keysets share a key identifier, so a decoded header cannot " +
			"say which system signed a token")
	}
}

// TestTheDriverTokenActiveKeyMustExist.
//
// A signature nobody can produce is not a token anybody can use, and the symptom is the first
// assignment after a deploy — a provider being told the platform is broken.
func TestTheDriverTokenActiveKeyMustExist(t *testing.T) {
	clearEnv(t)
	t.Setenv("DELIVERY_DRIVER_TOKEN_KEYS", deploymentDriverSigningKeys)
	t.Setenv("DELIVERY_DRIVER_TOKEN_ACTIVE_KID", "a-kid-that-is-not-in-the-set")

	if _, err := Load(); err == nil {
		t.Fatal("Load accepted an active identifier naming no key")
	} else if !strings.Contains(err.Error(), "DELIVERY_DRIVER_TOKEN_ACTIVE_KID") {
		t.Errorf("the error does not name the variable: %v", err)
	}
}

// TestTheDriverTokenTTLIsBounded.
//
// Nothing shortens an issued driver token — SHIP-109 is what reissues a link, and there is no
// revocation before it — so an over-long value is a standing grant to one job's delivery detail
// sitting in whatever message thread the link was forwarded through.
func TestTheDriverTokenTTLIsBounded(t *testing.T) {
	clearEnv(t)
	t.Setenv("DELIVERY_DRIVER_TOKEN_TTL", "8760h") // a year

	if _, err := Load(); err == nil {
		t.Fatal("Load accepted a driver token TTL of a year")
	} else if !strings.Contains(err.Error(), "DELIVERY_DRIVER_TOKEN_TTL") {
		t.Errorf("the error does not name the variable: %v", err)
	}
}

// TestTheDriverTokenDefaultsToSevenDays holds the number the tracker and deploy/.env.example both
// state, so that a change to it is a change somebody makes rather than one that drifts.
func TestTheDriverTokenDefaultsToSevenDays(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned %v", err)
	}
	if cfg.Delivery.DriverTokenTTL != 7*24*time.Hour {
		t.Errorf("Delivery.DriverTokenTTL = %s, want 168h", cfg.Delivery.DriverTokenTTL)
	}
}

// --- the object store (SHIP-15p, first consumed at SHIP-114) ------------------------------------

// TestStorageDefaults holds the values deploy/.env.example, deploy/docker-compose.yml and the
// Makefile all state, so that a change to any one of them is a change somebody makes rather than
// a drift somebody discovers.
//
// The endpoint is the pointed one. SigV4 signs the `host` header, so a URL minted against
// `minio:9000` inside the compose network and fetched from the host is refused as
// SignatureDoesNotMatch — an error naming neither the address nor the cause.
func TestStorageDefaults(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned %v", err)
	}

	if cfg.Storage.Endpoint != "http://localhost:9000" {
		t.Errorf("Storage.Endpoint = %q, want the published host port", cfg.Storage.Endpoint)
	}
	if cfg.Storage.Bucket != "shipper-dev" {
		t.Errorf("Storage.Bucket = %q, want shipper-dev", cfg.Storage.Bucket)
	}
	if cfg.Storage.Region != "ap-southeast-2" {
		t.Errorf("Storage.Region = %q, want ap-southeast-2 — it must match MINIO_REGION in "+
			"deploy/docker-compose.yml, because SigV4 signs the region", cfg.Storage.Region)
	}
	if !cfg.Storage.UsePathStyle {
		t.Error("Storage.UsePathStyle = false; the virtual-host form needs " +
			"shipper-dev.localhost to resolve, and it does not")
	}
	if cfg.Storage.PresignTTL != 15*time.Minute {
		t.Errorf("Storage.PresignTTL = %s, want 15m", cfg.Storage.PresignTTL)
	}
	// Shorter than the upload's, deliberately and by default (SHIP-15r): a download link only
	// has to outlast an image rendering, and every one of them is a live link to a photograph.
	if cfg.Storage.DownloadTTL != 5*time.Minute {
		t.Errorf("Storage.DownloadTTL = %s, want 5m", cfg.Storage.DownloadTTL)
	}
	if cfg.Storage.DownloadTTL >= cfg.Storage.PresignTTL {
		t.Errorf("the download lifetime (%s) is not shorter than the upload's (%s); the two "+
			"requirements are different and only the upload's is generous",
			cfg.Storage.DownloadTTL, cfg.Storage.PresignTTL)
	}
	if cfg.Storage.MaxUploadBytes != defaultMaxUploadBytes {
		t.Errorf("Storage.MaxUploadBytes = %d, want %d", cfg.Storage.MaxUploadBytes, defaultMaxUploadBytes)
	}
	want := []string{"image/jpeg", "image/png", "image/heic"}
	if len(cfg.Storage.AcceptedContentTypes) != len(want) {
		t.Fatalf("Storage.AcceptedContentTypes = %v, want %v", cfg.Storage.AcceptedContentTypes, want)
	}
	for i := range want {
		if cfg.Storage.AcceptedContentTypes[i] != want[i] {
			t.Errorf("Storage.AcceptedContentTypes[%d] = %q, want %q",
				i, cfg.Storage.AcceptedContentTypes[i], want[i])
		}
	}
}

// TestStorageIsReadFromTheEnvironment is the half that matters for a deployment: every field is a
// variable, including the endpoint, which is always explicit rather than inferred from the region.
func TestStorageIsReadFromTheEnvironment(t *testing.T) {
	clearEnv(t)
	t.Setenv("STORAGE_ENDPOINT", "https://s3.ap-southeast-4.amazonaws.com")
	t.Setenv("STORAGE_BUCKET", "shipper-production-evidence")
	t.Setenv("STORAGE_REGION", "ap-southeast-4")
	t.Setenv("STORAGE_ACCESS_KEY_ID", deploymentStorageAccessKeyID)
	t.Setenv("STORAGE_SECRET_ACCESS_KEY", deploymentStorageSecretAccessKey)
	t.Setenv("STORAGE_USE_PATH_STYLE", "false")
	t.Setenv("STORAGE_PRESIGN_TTL", "5m")
	t.Setenv("STORAGE_DOWNLOAD_TTL", "90s")
	t.Setenv("STORAGE_MAX_UPLOAD_BYTES", "2097152")
	t.Setenv("STORAGE_ACCEPTED_CONTENT_TYPES", "image/jpeg, image/webp")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned %v", err)
	}

	if cfg.Storage.Endpoint != "https://s3.ap-southeast-4.amazonaws.com" {
		t.Errorf("Storage.Endpoint = %q", cfg.Storage.Endpoint)
	}
	if cfg.Storage.Bucket != "shipper-production-evidence" {
		t.Errorf("Storage.Bucket = %q", cfg.Storage.Bucket)
	}
	if cfg.Storage.Region != "ap-southeast-4" {
		t.Errorf("Storage.Region = %q", cfg.Storage.Region)
	}
	if cfg.Storage.AccessKeyID != deploymentStorageAccessKeyID {
		t.Errorf("Storage.AccessKeyID = %q", cfg.Storage.AccessKeyID)
	}
	if cfg.Storage.SecretAccessKey != deploymentStorageSecretAccessKey {
		t.Error("Storage.SecretAccessKey did not come from the environment")
	}
	if cfg.Storage.UsePathStyle {
		t.Error("Storage.UsePathStyle = true; STORAGE_USE_PATH_STYLE=false was set")
	}
	if cfg.Storage.PresignTTL != 5*time.Minute {
		t.Errorf("Storage.PresignTTL = %s, want 5m", cfg.Storage.PresignTTL)
	}
	if cfg.Storage.DownloadTTL != 90*time.Second {
		t.Errorf("Storage.DownloadTTL = %s, want 90s", cfg.Storage.DownloadTTL)
	}
	if cfg.Storage.MaxUploadBytes != 2<<20 {
		t.Errorf("Storage.MaxUploadBytes = %d, want %d", cfg.Storage.MaxUploadBytes, 2<<20)
	}
	if len(cfg.Storage.AcceptedContentTypes) != 2 ||
		cfg.Storage.AcceptedContentTypes[1] != "image/webp" {
		t.Errorf("Storage.AcceptedContentTypes = %v, want the surrounding spaces trimmed",
			cfg.Storage.AcceptedContentTypes)
	}
}

// TestStorageUsePathStyleRefusesAValueItCannotRead.
//
// The values people actually type are "yes" and "on", and reading either as false would turn a
// switch somebody deliberately set into one they did not — an upload URL that 404s rather than a
// configuration error.
func TestStorageUsePathStyleRefusesAValueItCannotRead(t *testing.T) {
	clearEnv(t)
	t.Setenv("STORAGE_USE_PATH_STYLE", "yes")

	if _, err := Load(); err == nil {
		t.Fatal("Load accepted STORAGE_USE_PATH_STYLE=yes")
	} else if !strings.Contains(err.Error(), "STORAGE_USE_PATH_STYLE") {
		t.Errorf("the error does not name the variable: %v", err)
	}
}

// TestStorageEndpointMustBeAnAbsoluteURL.
//
// There is no empty state — an empty variable is an absent one to loader.lookup, so it would fall
// back to the development default and put a production deployment on somebody's loopback. The
// endpoint is therefore always typed, and something that is not a URL is caught here rather than
// by the SDK at the first upload.
func TestStorageEndpointMustBeAnAbsoluteURL(t *testing.T) {
	for _, endpoint := range []string{"s3.amazonaws.com", "localhost:9000", "ftp://files.internal"} {
		t.Run(endpoint, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("STORAGE_ENDPOINT", endpoint)

			if _, err := Load(); err == nil {
				t.Fatalf("Load accepted the endpoint %q", endpoint)
			} else if !strings.Contains(err.Error(), "STORAGE_ENDPOINT") {
				t.Errorf("the error does not name the variable: %v", err)
			}
		})
	}
}

// TestStorageBucketNamesArePlausible.
//
// The bucket name is per-worktree in development, which makes it a value a person types. Without
// this the first thing to notice a name S3 will not accept is a driver's proof upload.
func TestStorageBucketNamesArePlausible(t *testing.T) {
	refused := map[string]string{
		"underscores":      "shipper_dev",
		"upper case":       "Shipper-Dev",
		"too short":        "sd",
		"a trailing dash":  "shipper-dev-",
		"a leading dash":   "-shipper-dev",
		"a leading dot":    ".shipper-dev",
		"a slash":          "shipper/dev",
		"a path with keys": "shipper-dev/proof",
	}
	for name, bucket := range refused {
		t.Run(name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("STORAGE_BUCKET", bucket)

			if _, err := Load(); err == nil {
				t.Fatalf("Load accepted the bucket name %q", bucket)
			} else if !strings.Contains(err.Error(), "STORAGE_BUCKET") {
				t.Errorf("the error does not name the variable: %v", err)
			}
		})
	}

	// A worktree's derived-looking name has to pass, or the isolation CLAUDE.md's worktree table
	// prescribes would be refused at startup by the very check meant to protect it.
	for _, bucket := range []string{"shipper-dev", "ship-84-88-bids-and-offers", "shipper.proof.au"} {
		t.Run(bucket, func(t *testing.T) {
			clearEnv(t)
			t.Setenv("STORAGE_BUCKET", bucket)

			if _, err := Load(); err != nil {
				t.Fatalf("Load refused the bucket name %q: %v", bucket, err)
			}
		})
	}
}

// TestStoragePresignTTLIsBounded, in both directions and for both variables.
//
// Nothing revokes a pre-signed URL once it is signed: the signature is the whole of the
// authorisation, and no server-side check runs when it is redeemed (Docs/06 §5.2). The window is
// therefore the whole of the exposure.
//
// **Both are checked separately rather than by taking the larger of the two** (SHIP-15r). They are
// independent settings, and a deployment that raised only the read window is the one this is for —
// the direction where the exposure is a standing link to somebody's front door rather than a wider
// window to finish an upload nobody else can start.
//
// Zero is refused as well, and that is the less obvious half: an unparseable duration is already
// reported, but a deliberate `0s` parses. Without this the first symptom is delivery's handler
// panicking during attach — a configuration fault reported as a wiring one.
func TestStoragePresignTTLIsBounded(t *testing.T) {
	for name, c := range map[string]struct{ key, value string }{
		"an upload lifetime of a day":  {"STORAGE_PRESIGN_TTL", "24h"},
		"a download lifetime of a day": {"STORAGE_DOWNLOAD_TTL", "24h"},
		"no upload lifetime at all":    {"STORAGE_PRESIGN_TTL", "0s"},
		"no download lifetime at all":  {"STORAGE_DOWNLOAD_TTL", "0s"},
		"a negative upload lifetime":   {"STORAGE_PRESIGN_TTL", "-1m"},
		"a negative download lifetime": {"STORAGE_DOWNLOAD_TTL", "-1m"},
	} {
		t.Run(name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(c.key, c.value)

			if _, err := Load(); err == nil {
				t.Fatalf("Load accepted %s=%s", c.key, c.value)
			} else if !strings.Contains(err.Error(), c.key) {
				t.Errorf("the error does not name the variable: %v", err)
			}
		})
	}
}

// TestStorageUploadPolicyIsRefusedWhenItCannotWork.
//
// Both halves are policy the platform is expected to move under operational pressure, which is
// exactly why a wrong value has to fail at startup: a size below any real photograph, or a media
// type that matches nothing, both present as a driver unable to finish a delivery.
func TestStorageUploadPolicyIsRefusedWhenItCannotWork(t *testing.T) {
	cases := map[string]struct{ key, value, want string }{
		"a size below any photograph": {"STORAGE_MAX_UPLOAD_BYTES", "1024", "STORAGE_MAX_UPLOAD_BYTES"},
		"a size past a photograph":    {"STORAGE_MAX_UPLOAD_BYTES", "268435456", "STORAGE_MAX_UPLOAD_BYTES"},
		"a media type with no slash":  {"STORAGE_ACCEPTED_CONTENT_TYPES", "jpeg", "STORAGE_ACCEPTED_CONTENT_TYPES"},
		"a media type in upper case":  {"STORAGE_ACCEPTED_CONTENT_TYPES", "Image/JPEG", "STORAGE_ACCEPTED_CONTENT_TYPES"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(c.key, c.value)

			if _, err := Load(); err == nil {
				t.Fatalf("Load accepted %s=%s", c.key, c.value)
			} else if !strings.Contains(err.Error(), c.want) {
				t.Errorf("the error does not name the variable: %v", err)
			}
		})
	}
}

// TestLogValueOmitsTheStorageCredential.
//
// The bucket and the endpoint are worth a startup line — the bucket especially, because it is
// per-worktree and a process writing into the wrong one succeeds at everything. The secret is
// worth none, for the reason the signing keys are not logged either: a startup line is collected,
// shipped and retained.
func TestLogValueOmitsTheStorageCredential(t *testing.T) {
	clearEnv(t)
	t.Setenv("STORAGE_SECRET_ACCESS_KEY", "a-secret-nobody-should-see-in-a-log")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned %v", err)
	}

	rendered := cfg.LogValue().String()
	if strings.Contains(rendered, "a-secret-nobody-should-see-in-a-log") {
		t.Error("the storage secret appears in the startup log line")
	}
	for _, want := range []string{"storage_bucket", "storage_endpoint", "storage_region"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("the startup log line does not carry %s: %s", want, rendered)
		}
	}
}

// TestClientPolicyDefaults — the two numbers GET /v1/app/policy serves (SHIP-167a).
//
// The defaults matter more here than they usually do, because they are the values a fresh clone
// and the pilot both run on: the endpoint exists so operations can move them, and until somebody
// does, these are what every handset is told.
func TestClientPolicyDefaults(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.App.UnsyncedNudgeAfter != 4*time.Hour {
		t.Errorf("UnsyncedNudgeAfter = %s, want 4h — Docs/02 §3.1's second rung",
			cfg.App.UnsyncedNudgeAfter)
	}
	if cfg.App.ProofCompressionBudgetBytes != 1<<20 {
		t.Errorf("ProofCompressionBudgetBytes = %d, want %d",
			cfg.App.ProofCompressionBudgetBytes, 1<<20)
	}
}

func TestClientPolicyIsReadFromTheEnvironment(t *testing.T) {
	clearEnv(t)
	t.Setenv("UNSYNCED_NUDGE_AFTER", "90m")
	t.Setenv("PROOF_COMPRESSION_BUDGET_BYTES", "524288")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.App.UnsyncedNudgeAfter != 90*time.Minute {
		t.Errorf("UnsyncedNudgeAfter = %s, want 1h30m0s", cfg.App.UnsyncedNudgeAfter)
	}
	if cfg.App.ProofCompressionBudgetBytes != 524288 {
		t.Errorf("ProofCompressionBudgetBytes = %d, want 524288", cfg.App.ProofCompressionBudgetBytes)
	}
}

// TestClientPolicyIsRefusedWhenItCannotWork.
//
// Both values are policy operations may legitimately move, which is exactly why a wrong one has
// to fail at startup rather than on a handset: a threshold of zero prompts a provider about every
// update the instant they record it, and a budget above the platform's own bound tells every
// device to compress towards a size the platform will refuse to sign for.
//
// The budget case is the one worth having: it is a **cross-section** rule, so neither variable is
// wrong on its own and nothing in either section could catch it.
func TestClientPolicyIsRefusedWhenItCannotWork(t *testing.T) {
	cases := map[string]struct {
		env  map[string]string
		want string
	}{
		"a threshold of nothing at all": {
			env:  map[string]string{"UNSYNCED_NUDGE_AFTER": "0s"},
			want: "UNSYNCED_NUDGE_AFTER",
		},
		"a negative threshold": {
			env:  map[string]string{"UNSYNCED_NUDGE_AFTER": "-1h"},
			want: "UNSYNCED_NUDGE_AFTER",
		},
		"a budget that would compress a licence plate away": {
			env:  map[string]string{"PROOF_COMPRESSION_BUDGET_BYTES": "1024"},
			want: "PROOF_COMPRESSION_BUDGET_BYTES",
		},
		"a budget above what the platform will sign for": {
			env: map[string]string{
				"PROOF_COMPRESSION_BUDGET_BYTES": "4194304", // 4 MiB
				"STORAGE_MAX_UPLOAD_BYTES":       "2097152", // 2 MiB
			},
			want: "PROOF_COMPRESSION_BUDGET_BYTES",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			clearEnv(t)
			for k, v := range c.env {
				t.Setenv(k, v)
			}

			if _, err := Load(); err == nil {
				t.Fatalf("Load accepted %v", c.env)
			} else if !strings.Contains(err.Error(), c.want) {
				t.Errorf("the error does not name the variable: %v", err)
			}
		})
	}
}

// SHIP-147a's *Done when* asks for "no second cost knob anywhere", and a claim like that is worth
// a guard rather than a search somebody did once.
//
// Two halves, because the knob has two forms and a second one could appear as either. The failure
// this prevents is not exotic: a domain that needs a password adds its own cost section, both are
// plausible, both verify every hash they are handed — the parameters travel in the PHC string —
// and nothing reports that half the platform's passwords are stored at the old cost the day
// somebody raises one. Docs/10 §3.4 exists to refuse exactly that, and internal/passwords was
// promoted out of internal/identity at SHIP-15r for the same reason.
func TestThereIsOneArgon2CostSetting(t *testing.T) {
	// The Go half: exactly one field of type Argon2 anywhere in the Config tree.
	var found []string
	var walk func(prefix string, typ reflect.Type)
	walk = func(prefix string, typ reflect.Type) {
		for i := range typ.NumField() {
			f := typ.Field(i)
			path := prefix + "." + f.Name
			switch {
			case f.Type == reflect.TypeOf(Argon2{}):
				found = append(found, path)
			case f.Type.Kind() == reflect.Struct:
				walk(path, f.Type)
			}
		}
	}
	walk("Config", reflect.TypeOf(Config{}))

	if len(found) != 1 {
		t.Errorf("the platform's argon2id cost is configured in %d places: %s.\n"+
			"There is one password cost and every hasher reads it — two that agree by comment "+
			"is Docs/10 §3.4's failure, and the second one is invisible until somebody raises "+
			"the first.", len(found), strings.Join(found, ", "))
	} else if found[0] != "Config.Passwords.Argon2" {
		t.Errorf("the argon2id cost is at %s, want Config.Passwords.Argon2 — SHIP-147a named it "+
			"for the platform rather than for one domain", found[0])
	}

	// The environment half: exactly one prefix, and it is not a domain's.
	source, err := os.ReadFile(configSource)
	if err != nil {
		t.Fatalf("reading %s: %v", configSource, err)
	}

	want := map[string]bool{
		"PASSWORDS_ARGON2_MEMORY_KIB":  true,
		"PASSWORDS_ARGON2_ITERATIONS":  true,
		"PASSWORDS_ARGON2_PARALLELISM": true,
	}
	got := map[string]bool{}
	for _, m := range envKey.FindAllStringSubmatch(string(source), -1) {
		if strings.Contains(m[1], "ARGON2") {
			got[m[1]] = true
		}
	}

	for key := range got {
		if !want[key] {
			t.Errorf("%s reads %s. One cost, one prefix: the argon2id variables are "+
				"PASSWORDS_ARGON2_* (SHIP-147a).", configSource, key)
		}
	}
	for key := range want {
		if !got[key] {
			t.Errorf("%s no longer reads %s; deploy/.env.example's release note tells every "+
				"deployment to set it.", configSource, key)
		}
	}
}

// --- SHIP-187a: the messaging transport ---------------------------------------------------

// The rule this asserts used to live in email.UseConsole and sms.UseConsole and was tested
// there. It moved here when the transport became configurable, and the test moved with it —
// one place owning the rule is the whole point of the move, and a rule tested where it no
// longer lives is how two places start to disagree.
func TestTransportDefaultsToTheEnvironmentWhenUnset(t *testing.T) {
	cases := map[string]struct {
		env  Environment
		want Transport
	}{
		"development logs":                 {env: Development, want: TransportConsole},
		"staging dispatches":               {env: Staging, want: TransportHTTP},
		"production dispatches":            {env: Production, want: TransportHTTP},
		"an unrecognised environment logs": {env: Environment("prod"), want: TransportConsole},
		"an empty environment logs":        {env: Environment(""), want: TransportConsole},
	}

	// resolveTransports directly rather than through Load, because two of these environments
	// are invalid and Load returns no configuration at all for an invalid one — so the rule
	// under test would be unreachable through it for exactly the cases that matter most.
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := &Config{Env: c.env}
			resolveTransports(cfg)

			if cfg.Email.Transport != c.want {
				t.Errorf("email transport = %q, want %q", cfg.Email.Transport, c.want)
			}
			if cfg.SMS.Transport != c.want {
				t.Errorf("sms transport = %q, want %q", cfg.SMS.Transport, c.want)
			}
		})
	}
}

// Defaulting fills a gap and never overrides a choice.
func TestResolveTransportsLeavesAnExplicitChoiceAlone(t *testing.T) {
	cfg := &Config{
		Env:   Development,
		Email: Email{Transport: TransportSMTP},
		SMS:   SMS{Transport: TransportHTTP},
	}
	resolveTransports(cfg)

	if cfg.Email.Transport != TransportSMTP {
		t.Errorf("email transport = %q, want smtp", cfg.Email.Transport)
	}
	if cfg.SMS.Transport != TransportHTTP {
		t.Errorf("sms transport = %q, want http", cfg.SMS.Transport)
	}
}

// The case the environment could not express, and the reason the setting exists: an instance
// hardened in every other respect that deliberately sends its mail somewhere free.
func TestTransportCanBeChosenAgainstTheEnvironment(t *testing.T) {
	clearEnv(t)
	t.Setenv("SHIPPER_ENV", "staging")
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("DATABASE_URL", "postgres://u:p@db.internal:5432/shipper?sslmode=require")
	t.Setenv("REDIS_URL", "redis://cache.internal:6379/0")
	t.Setenv("IDENTITY_ACCESS_TOKEN_KEYS", deploymentSigningKeys)
	t.Setenv("DELIVERY_DRIVER_TOKEN_KEYS", deploymentDriverSigningKeys)
	t.Setenv("IDENTITY_ACCESS_TOKEN_ACTIVE_KID", "2026-08")
	t.Setenv("DELIVERY_DRIVER_TOKEN_ACTIVE_KID", "2026-08")
	t.Setenv("STORAGE_ACCESS_KEY_ID", "AKIAEXAMPLE")
	t.Setenv("STORAGE_SECRET_ACCESS_KEY", "secretexamplesecretexample")
	t.Setenv("STORAGE_ENDPOINT", "https://s3.ap-southeast-2.amazonaws.com")
	t.Setenv("EMAIL_TRANSPORT", "smtp")
	t.Setenv("EMAIL_SMTP_HOST", "mailpit")
	t.Setenv("EMAIL_SMTP_PORT", "1025")
	t.Setenv("EMAIL_SMTP_ENCRYPTION", "none")
	t.Setenv("SMS_TRANSPORT", "console")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned %v, want nil", err)
	}
	if cfg.Email.Transport != TransportSMTP {
		t.Errorf("email transport = %q, want smtp", cfg.Email.Transport)
	}
	if cfg.Email.SMTP.Host != "mailpit" || cfg.Email.SMTP.Port != 1025 {
		t.Errorf("smtp = %s:%d, want mailpit:1025", cfg.Email.SMTP.Host, cfg.Email.SMTP.Port)
	}
	if cfg.SMS.Transport != TransportConsole {
		t.Errorf("sms transport = %q, want console", cfg.SMS.Transport)
	}
}

// A value that is present and wrong is somebody's intention spelled incorrectly, and
// defaulting it would either send real email from a deployment that asked for the console or
// swallow it in one that asked for a provider. Both are silent.
func TestLoadRefusesAnUnknownTransport(t *testing.T) {
	cases := map[string]struct{ key, value, want string }{
		"an unknown email transport": {"EMAIL_TRANSPORT", "sendmail", "EMAIL_TRANSPORT"},
		"an unknown sms transport":   {"SMS_TRANSPORT", "carrier-pigeon", "SMS_TRANSPORT"},
		// smtp is a real transport and a real mistake: it is the email one, and an SMS
		// gateway that spoke it would be news. Named separately so the message can say so.
		"smtp asked of sms": {"SMS_TRANSPORT", "smtp", "SMS_TRANSPORT"},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(c.key, c.value)

			_, err := Load()
			if err == nil {
				t.Fatalf("Load() accepted %s=%s", c.key, c.value)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, want it to name %s", err, c.want)
			}
		})
	}
}

// The failure without this is a panic out of cmd/api's composition root — a configuration
// fault reported as a wiring one, at the point furthest from the line that caused it.
func TestLoadRefusesSMTPWithoutAHost(t *testing.T) {
	clearEnv(t)
	t.Setenv("EMAIL_TRANSPORT", "smtp")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() accepted EMAIL_TRANSPORT=smtp with no host")
	}
	if !strings.Contains(err.Error(), "EMAIL_SMTP_HOST") {
		t.Errorf("error = %v, want it to name EMAIL_SMTP_HOST", err)
	}
}
