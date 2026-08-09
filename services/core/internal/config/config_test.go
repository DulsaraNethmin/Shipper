package config

import (
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
}

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
		"SHIPPER_ENV":  "production",
		"DATABASE_URL": "postgres://u:p@db.internal:5432/shipper?sslmode=require",
		"REDIS_URL":    "redis://cache.internal:6379/0",
		"LOG_FORMAT":   "json",
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
	}

	rendered := cfg.LogValue().String()
	for _, secret := range []string{"hunter2", "s3cret"} {
		if strings.Contains(rendered, secret) {
			t.Errorf("LogValue() rendered %q, which contains the credential %q", rendered, secret)
		}
	}
	if !strings.Contains(rendered, "host:5432") {
		t.Errorf("LogValue() rendered %q, want the host retained for diagnosis", rendered)
	}
}
