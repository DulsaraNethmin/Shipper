package config

import (
	"net/netip"
	"strings"
	"testing"
)

// SHIP-183b's configuration half.
//
// The *Done when* is that the client address comes from a forwarded header "only for a configured
// trusted-proxy hop count or CIDR allow-list". Two of those words carry the weight. **Configured**
// — so the default must trust nothing, and these tests fail if it ever starts trusting something
// by omission. **Or** — so setting both is refused rather than silently resolved.

func TestTrustedProxyDefaultsToTrustingNothing(t *testing.T) {
	clearEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned %v, want nil", err)
	}

	if cfg.TrustedProxy.Hops != 0 {
		t.Errorf("TrustedProxy.Hops = %d, want 0 — a deployment that said nothing must not "+
			"believe a forwarded header", cfg.TrustedProxy.Hops)
	}
	if len(cfg.TrustedProxy.Networks) != 0 {
		t.Errorf("TrustedProxy.Networks = %v, want none", cfg.TrustedProxy.Networks)
	}
	if cfg.TrustedProxy.Configured() {
		t.Error("Configured() is true with nothing set, so X-Forwarded-For would be read by " +
			"default and every caller could choose their own rate-limit bucket")
	}
}

func TestTrustedProxyReadsAHopCount(t *testing.T) {
	clearEnv(t)
	t.Setenv("TRUSTED_PROXY_HOPS", "2")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned %v, want nil", err)
	}
	if cfg.TrustedProxy.Hops != 2 {
		t.Errorf("TrustedProxy.Hops = %d, want 2", cfg.TrustedProxy.Hops)
	}
	if !cfg.TrustedProxy.Configured() {
		t.Error("Configured() is false with a hop count set")
	}
}

func TestTrustedProxyReadsNetworks(t *testing.T) {
	clearEnv(t)
	// A CIDR, a bare address, and whitespace a person editing a .env file leaves behind.
	t.Setenv("TRUSTED_PROXY_NETWORKS", "10.0.0.0/8 , 203.0.113.7, 2001:db8::/32")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned %v, want nil", err)
	}

	want := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("203.0.113.7/32"),
		netip.MustParsePrefix("2001:db8::/32"),
	}
	if len(cfg.TrustedProxy.Networks) != len(want) {
		t.Fatalf("read %v, want %v", cfg.TrustedProxy.Networks, want)
	}
	for i, w := range want {
		if cfg.TrustedProxy.Networks[i] != w {
			t.Errorf("network %d = %s, want %s", i, cfg.TrustedProxy.Networks[i], w)
		}
	}
}

// TestTrustedProxyMasksNetworks holds the normalisation that stops a prefix matching nothing.
//
// `10.1.2.3/8` is a prefix with host bits set. Stored as written it is not equal to the network
// any address in it belongs to, and the allow-list quietly trusts nobody — which shows up as every
// caller behind the proxy sharing one bucket, with the cause four characters into an environment
// variable.
func TestTrustedProxyMasksNetworks(t *testing.T) {
	clearEnv(t)
	t.Setenv("TRUSTED_PROXY_NETWORKS", "10.1.2.3/8")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned %v, want nil", err)
	}

	got := cfg.TrustedProxy.Networks[0]
	if want := netip.MustParsePrefix("10.0.0.0/8"); got != want {
		t.Fatalf("network = %s, want %s", got, want)
	}
	if !got.Contains(netip.MustParseAddr("10.9.9.9")) {
		t.Error("the stored network does not contain an address inside it, so the allow-list " +
			"trusts nobody")
	}
}

// TestTrustedProxyRefusesBothMechanisms is the "or" in the Done when.
func TestTrustedProxyRefusesBothMechanisms(t *testing.T) {
	clearEnv(t)
	t.Setenv("TRUSTED_PROXY_HOPS", "1")
	t.Setenv("TRUSTED_PROXY_NETWORKS", "10.0.0.0/8")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() accepted both a hop count and an allow-list. There is no agreed rule " +
			"for combining them, so a deployment setting both would get whichever the code " +
			"happened to prefer")
	}
	if !strings.Contains(err.Error(), "TRUSTED_PROXY_HOPS") ||
		!strings.Contains(err.Error(), "TRUSTED_PROXY_NETWORKS") {
		t.Errorf("the error names neither variable, so nobody knows what to unset: %v", err)
	}
}

func TestTrustedProxyRefusesValuesItCannotUse(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   string
		value string
	}{
		{"a hop count that is not a number", "TRUSTED_PROXY_HOPS", "one"},
		{"a negative hop count", "TRUSTED_PROXY_HOPS", "-1"},
		{"a hop count past any real chain", "TRUSTED_PROXY_HOPS", "100"},
		{"a network that is not one", "TRUSTED_PROXY_NETWORKS", "the-load-balancer"},
		{"a prefix with no length", "TRUSTED_PROXY_NETWORKS", "10.0.0.0/"},
		{"one good network and one typo", "TRUSTED_PROXY_NETWORKS", "10.0.0.0/8,192.168.0/16"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearEnv(t)
			t.Setenv(tc.key, tc.value)

			if _, err := Load(); err == nil {
				t.Fatalf("Load() accepted %s=%q. A value that cannot be used is a proxy that "+
					"stops being trusted, and the symptom is every caller behind it sharing "+
					"one bucket", tc.key, tc.value)
			}
		})
	}
}
