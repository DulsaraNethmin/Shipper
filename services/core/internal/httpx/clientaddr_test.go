package httpx

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

// SHIP-183b's second clause, which is the half the eleven address-keyed limits rest on:
//
//	"the client address is read from a forwarded header only for a configured trusted-proxy hop
//	count or CIDR allow-list, and from RemoteAddr otherwise, so no caller can choose their own
//	bucket"
//
// Two properties are being held and they fail in opposite directions. **No caller may choose their
// own bucket** — the security half, and the one every case below with a forged header is about.
// **A real client behind a real proxy must get their own bucket** — the availability half, without
// which the eleven routes throttle the world at the first caller and are worse than unlimited.

// prefixes is the allow-list form config.TrustedProxy hands this package.
func prefixes(t *testing.T, cidrs ...string) []netip.Prefix {
	t.Helper()

	out := make([]netip.Prefix, 0, len(cidrs))
	for _, c := range cidrs {
		p, err := netip.ParsePrefix(c)
		if err != nil {
			t.Fatalf("parsing %q: %v", c, err)
		}
		out = append(out, p.Masked())
	}
	return out
}

// request builds one arriving from remote, carrying forwarded as X-Forwarded-For.
//
// Each element of forwarded becomes its own header line, because a chain of proxies that each
// append one produces exactly that and a resolver that reads only the first would be wrong about
// every deployment with two hops.
func request(remote string, forwarded ...string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/v1/", nil)
	r.RemoteAddr = remote
	for _, f := range forwarded {
		r.Header.Add(HeaderForwardedFor, f)
	}
	return r
}

func TestTheClientAddressIsResolvedUnderTheDeploymentsRules(t *testing.T) {
	for _, tc := range []struct {
		name     string
		remote   string
		fwd      []string
		hops     int
		trusted  []string
		want     string
		property string
	}{
		// --- Nothing configured. This is the default and what SHIP-47 shipped. ---
		{
			name:     "with no proxy configured the peer is the client",
			remote:   "203.0.113.7:41000",
			want:     "203.0.113.7",
			property: "the default",
		},
		{
			name:     "with no proxy configured a forwarded header is ignored entirely",
			remote:   "203.0.113.7:41000",
			fwd:      []string{"198.51.100.9"},
			want:     "203.0.113.7",
			property: "no caller may choose their own bucket",
		},

		// --- A hop count. Positional, and blind to what the values look like. ---
		{
			name:     "one hop takes the entry the balancer appended",
			remote:   "203.0.113.7:41000",
			fwd:      []string{"198.51.100.9"},
			hops:     1,
			want:     "198.51.100.9",
			property: "a real client behind a real proxy gets their own bucket",
		},
		{
			name:     "one hop ignores everything the caller prepended",
			remote:   "203.0.113.7:41000",
			fwd:      []string{"1.2.3.4, 5.6.7.8", "198.51.100.9"},
			hops:     1,
			want:     "198.51.100.9",
			property: "no caller may choose their own bucket",
		},
		{
			name:     "two hops walk past both proxies",
			remote:   "203.0.113.7:41000",
			fwd:      []string{"198.51.100.9, 192.0.2.5"},
			hops:     2,
			want:     "198.51.100.9",
			property: "a real client behind a real proxy gets their own bucket",
		},
		{
			name: "a chain shorter than the configured count falls back to the peer",
			// A proxy that is not appending what it was configured to append. The
			// caller must not be able to reach past the end of the chain and have
			// their own first entry believed.
			remote:   "203.0.113.7:41000",
			hops:     1,
			want:     "203.0.113.7",
			property: "no caller may choose their own bucket",
		},
		{
			name: "an unparseable entry at the counted position falls back to the peer",
			// The count lands on "unknown". Walking left to the next readable entry
			// is the one thing that must not happen: that is the shift a caller
			// engineers by writing a malformed entry of their own.
			remote:   "203.0.113.7:41000",
			fwd:      []string{"198.51.100.9, unknown"},
			hops:     1,
			want:     "203.0.113.7",
			property: "no caller may choose their own bucket",
		},
		{
			name: "an unparseable entry keeps its place in the chain",
			// If a malformed entry were dropped rather than kept, everything left of
			// it would shift one place and the count would land on 1.2.3.4 — which
			// the caller wrote. Position is what makes the count unspoofable.
			remote:   "203.0.113.7:41000",
			fwd:      []string{"1.2.3.4, unknown, 198.51.100.9"},
			hops:     1,
			want:     "198.51.100.9",
			property: "no caller may choose their own bucket",
		},

		// --- A CIDR allow-list. Gated on the peer, then skipping trusted hops. ---
		{
			name:     "a trusted peer's forwarded entry is believed",
			remote:   "203.0.113.7:41000",
			fwd:      []string{"198.51.100.9"},
			trusted:  []string{"203.0.113.0/24"},
			want:     "198.51.100.9",
			property: "a real client behind a real proxy gets their own bucket",
		},
		{
			name: "an untrusted peer's forwarded entry is ignored outright",
			// Somebody reaching the service directly, past the balancer. This is the
			// case that makes the allow-list a gate rather than a preference.
			remote:   "198.51.100.20:41000",
			fwd:      []string{"1.2.3.4"},
			trusted:  []string{"203.0.113.0/24"},
			want:     "198.51.100.20",
			property: "no caller may choose their own bucket",
		},
		{
			name:     "trusted hops are skipped from the right until an untrusted one",
			remote:   "203.0.113.7:41000",
			fwd:      []string{"198.51.100.9, 203.0.113.9"},
			trusted:  []string{"203.0.113.0/24"},
			want:     "198.51.100.9",
			property: "a real client behind a real proxy gets their own bucket",
		},
		{
			name: "a chain that is trusted end to end resolves to the peer",
			// Believing the leftmost here is a bucket a caller inside the allow-list
			// picks for themselves, so the peer — the one entry nobody wrote — wins.
			remote:   "203.0.113.7:41000",
			fwd:      []string{"203.0.113.9"},
			trusted:  []string{"203.0.113.0/24"},
			want:     "203.0.113.7",
			property: "no caller may choose their own bucket",
		},
		{
			name:     "an unparseable entry stops the walk rather than being skipped over",
			remote:   "203.0.113.7:41000",
			fwd:      []string{"198.51.100.9, unknown"},
			trusted:  []string{"203.0.113.0/24"},
			want:     "203.0.113.7",
			property: "no caller may choose their own bucket",
		},
		{
			name:     "a bare address in the allow-list is the host itself",
			remote:   "203.0.113.7:41000",
			fwd:      []string{"198.51.100.9"},
			trusted:  []string{"203.0.113.7/32"},
			want:     "198.51.100.9",
			property: "a real client behind a real proxy gets their own bucket",
		},

		// --- Rendering. Two callers must not share a bucket, and one must not hold many. ---
		{
			name:     "the port is dropped, so a caller does not get a bucket per connection",
			remote:   "203.0.113.7:52341",
			want:     "203.0.113.7",
			property: "a caller holds one bucket",
		},
		{
			name:     "an IPv4-mapped address keys as the IPv4 address",
			remote:   "203.0.113.7:41000",
			fwd:      []string{"::ffff:198.51.100.9"},
			hops:     1,
			want:     "198.51.100.9",
			property: "a caller holds one bucket",
		},
		{
			name: "IPv6 keys on its /64",
			// A residential subscriber is handed a /64 at least. Keyed on the full
			// address, one caller holds billions of buckets and the class is
			// unenforceable against exactly the callers it is meant to bound.
			remote:   "[2001:db8:1:2:3:4:5:6]:41000",
			want:     "2001:db8:1:2::/64",
			property: "a caller holds one bucket",
		},
		{
			name:     "two addresses in one /64 are one caller",
			remote:   "[2001:db8:1:2:aaaa:bbbb:cccc:dddd]:41000",
			want:     "2001:db8:1:2::/64",
			property: "a caller holds one bucket",
		},
		{
			name:     "a different /64 is a different caller",
			remote:   "[2001:db8:1:3::1]:41000",
			want:     "2001:db8:1:3::/64",
			property: "two callers do not share a bucket",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveClientAddr(request(tc.remote, tc.fwd...), tc.hops, prefixes(t, tc.trusted...))
			if got != tc.want {
				t.Errorf("resolved %q, want %q — this case is holding %q",
					got, tc.want, tc.property)
			}
		})
	}
}

// TestAForgedHeaderCannotMoveACallerOffTheirBucket is the *Done when* stated as the property
// rather than as a table row.
//
// The table above checks one resolution at a time. This checks the thing an attacker would
// actually try: whether *any* forged header value moves them off the bucket they are being
// counted on. It is the same question the rate limit asks, and the answer has to be no for every
// value rather than for the ones somebody thought to tabulate.
func TestAForgedHeaderCannotMoveACallerOffTheirBucket(t *testing.T) {
	const peer = "203.0.113.7:41000"

	honest := resolveClientAddr(request(peer), 0, nil)

	for _, forged := range []string{
		"198.51.100.9",
		"198.51.100.9, 192.0.2.5",
		"203.0.113.8",
		"::1",
		"2001:db8::1",
		"unknown",
		"",
		"   ",
		"127.0.0.1, 127.0.0.1, 127.0.0.1",
	} {
		if got := resolveClientAddr(request(peer, forged), 0, nil); got != honest {
			t.Errorf("X-Forwarded-For: %q moved the caller from %q to %q, so they chose their "+
				"own bucket", forged, honest, got)
		}
	}
}

// TestAForgedHeaderCannotMoveACallerOffTheirBucketBehindAProxy is the same property one hop in.
//
// Behind a trusted proxy the header *is* read, which is the whole point — so the guarantee has to
// be restated rather than assumed: a caller may not choose their bucket by prepending entries to
// the chain the proxy is appending to.
func TestAForgedHeaderCannotMoveACallerOffTheirBucketBehindAProxy(t *testing.T) {
	const (
		balancer = "203.0.113.7:41000"
		client   = "198.51.100.9"
	)

	honest := resolveClientAddr(request(balancer, client), 1, nil)
	if honest != client {
		t.Fatalf("behind one trusted hop the client resolved to %q, want %q — a real client "+
			"behind a real proxy is not getting their own bucket", honest, client)
	}

	for _, prepended := range []string{
		"192.0.2.5",
		"192.0.2.5, 192.0.2.6",
		"unknown",
		"203.0.113.7",
	} {
		// The proxy appends the address it accepted the connection from, whatever the
		// caller sent before it.
		got := resolveClientAddr(request(balancer, prepended+", "+client), 1, nil)
		if got != honest {
			t.Errorf("prepending %q moved the caller from %q to %q, so they chose their own "+
				"bucket from behind the proxy", prepended, honest, got)
		}
	}
}

// TestTwoClientsBehindOneProxyAreCountedApart is the availability half.
//
// Without it the eleven routes are worse than unlimited: every caller behind the balancer shares
// one bucket, so the first to empty it refuses the world. A test that only proved forged headers
// were ignored would pass on an implementation that ignored every header, which is exactly that
// failure.
func TestTwoClientsBehindOneProxyAreCountedApart(t *testing.T) {
	const balancer = "203.0.113.7:41000"

	first := resolveClientAddr(request(balancer, "198.51.100.9"), 1, nil)
	second := resolveClientAddr(request(balancer, "198.51.100.10"), 1, nil)

	switch {
	case first == second:
		t.Fatalf("both clients resolved to %q, so they share one bucket and the first to "+
			"empty it refuses the other", first)
	case first == "203.0.113.7":
		t.Fatalf("the client resolved to the balancer's own address %q, so every caller "+
			"behind it is counted as one", first)
	}
}

// TestClientAddrFallsBackToThePeerWithoutTheMiddleware holds the wiring mistake to the safe side.
//
// A handler reached without ResolveClientAddr having run must not be handed an empty key — but it
// must also not be handed a value the caller wrote. RemoteAddr is both.
func TestClientAddrFallsBackToThePeerWithoutTheMiddleware(t *testing.T) {
	r := request("203.0.113.7:41000", "198.51.100.9")

	if got := ClientAddr(r); got != "203.0.113.7" {
		t.Errorf("ClientAddr = %q with no middleware, want the peer %q", got, "203.0.113.7")
	}
}

// TestResolveClientAddrPutsOneAnswerOnTheContext checks that every reader sees the same address.
//
// The rate-limit middleware, internal/identity and internal/admin all ask for this, and the reason
// it is resolved once at the edge is so that three readers cannot come to three answers as the
// trusted-proxy rule changes.
func TestResolveClientAddrPutsOneAnswerOnTheContext(t *testing.T) {
	var seen []string
	handler := ResolveClientAddr(1, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, ClientAddr(r), ClientAddr(r))
	}))

	handler.ServeHTTP(httptest.NewRecorder(), request("203.0.113.7:41000", "198.51.100.9"))

	for _, got := range seen {
		if got != "198.51.100.9" {
			t.Fatalf("a reader saw %q, want %q", got, "198.51.100.9")
		}
	}
	if len(seen) != 2 {
		t.Fatalf("the handler ran %d times, want 1 producing 2 reads", len(seen)/2)
	}
}

// TestTheAllowListCannotBeWidenedAfterTheFactHolds the copy in ResolveClientAddr.
//
// A caller that kept the slice it passed could add a network to it on a running service and widen
// who is trusted, with nothing in the request path to notice.
func TestTheAllowListCannotBeWidenedAfterTheFact(t *testing.T) {
	trusted := prefixes(t, "203.0.113.0/24")
	middleware := ResolveClientAddr(0, trusted)

	// Widen the caller's slice in place, exactly as an accidental shared reference would.
	trusted[0] = netip.MustParsePrefix("0.0.0.0/0")

	var got string
	middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = ClientAddr(r)
	})).ServeHTTP(httptest.NewRecorder(), request("198.51.100.20:41000", "1.2.3.4"))

	if got != "198.51.100.20" {
		t.Errorf("resolved %q; widening the caller's slice made an untrusted peer trusted", got)
	}
}
