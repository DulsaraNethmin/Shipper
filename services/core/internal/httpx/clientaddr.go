package httpx

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Where a request came from (SHIP-183b).
//
// # Why this is not simply RemoteAddr
//
// Eleven of the 86 routes key their rate limit on the client's network address (Docs/12 §8), and
// the address is the one property of a request that the request does not carry. Behind a load
// balancer RemoteAddr is the balancer, so all eleven collapse into one bucket per class and the
// first caller throttles the world. In front of one, X-Forwarded-For is whatever the client typed,
// so honouring it hands every caller their own bucket and the limit is evaded completely — which
// is worse than having no limit, because it looks like one.
//
// **Neither is a property of this code, and that is the point.** Which one is true is a fact about
// the deployment, so this file reads the header only as far as a deployment has said it may
// (config.TrustedProxy) and uses RemoteAddr for everything else. A deployment that has said
// nothing gets RemoteAddr alone, which is exactly what SHIP-47 shipped and Docs/11 §9 recorded.
//
// # Why the chain is read from the right
//
// The rightmost element of `X-Forwarded-For…, RemoteAddr` is the peer this process actually
// accepted a connection from — the one entry no client can write. Every entry to its left was
// appended by something further out, and a client's invented entries are necessarily the
// leftmost of all: prepending ten addresses moves the true client ten places left and leaves the
// count landing on it regardless. Counting from the left would read the first invented entry
// instead, which is the bypass this whole file exists to refuse.
//
// # Why the resolved value is grouped for IPv6
//
// A limit keyed on a full IPv6 address is not a limit. A residential IPv6 customer is routinely
// given a /64 and often a /56, so one caller holds billions of addresses and empties a fresh
// bucket from each — the class would be unenforceable against exactly the callers it is meant to
// bound, while remaining fully enforced against IPv4 callers who cannot do the same. So IPv6 is
// keyed on its /64, which is the smallest unit an operator hands to one subscriber. IPv4 is keyed
// on the address, because there it already is one.
//
// Docs/12 §11 records this as SHIP-183b's decision rather than leaving it to be rediscovered from
// the code.

// HeaderForwardedFor is the de-facto client-address header every load balancer sets.
//
// RFC 7239's `Forwarded` is deliberately not read. Nothing in front of this service emits it
// today, and a second header honoured on the same terms would be a second way to be wrong about
// the same fact — with the two disagreeing whenever a proxy sets only one. When something in the
// deployment does emit it, adding it here is a change to this file and to Docs/12, in that order.
const HeaderForwardedFor = "X-Forwarded-For"

// clientAddrKey carries the resolved address on the request context.
//
// A private struct type rather than an entry in requestid.go's iota block, following the
// precedent authFailureKey set: nothing outside this package can construct it, so nothing outside
// can overwrite an address the deployment's own configuration decided.
type clientAddrKey struct{}

// ResolveClientAddr resolves the client's address once per request and puts it on the context.
//
// Once, at the edge, rather than per reader: [ClientAddr] is consulted by the rate-limit
// middleware in cmd/api and by internal/identity and internal/admin for their credential buckets,
// and three readers each deriving it independently is three places for the trusted-proxy rule to
// drift. It is also three chances to read the raw header by accident.
//
// hops and trusted come from config.TrustedProxy, which refuses to set both. Passing values rather
// than the configuration struct keeps this package off internal/config: httpx is infrastructure
// every domain imports, and what it needs here is two facts, not a dependency.
//
// Zero and empty mean no forwarded header is read at all, which is the default and the safe end.
func ResolveClientAddr(hops int, trusted []netip.Prefix) func(http.Handler) http.Handler {
	// Copied so that a caller mutating the slice afterwards cannot quietly widen who is
	// trusted on a running service.
	networks := make([]netip.Prefix, len(trusted))
	copy(networks, trusted)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			addr := resolveClientAddr(r, hops, networks)
			ctx := context.WithValue(r.Context(), clientAddrKey{}, addr)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClientAddr is where the request came from, as every address-keyed limit counts it.
//
// # What it answers when the middleware did not run
//
// RemoteAddr, resolved by the same rules with nothing trusted. That is a deliberate fallback
// rather than an empty string or a panic: a wiring mistake then costs the shared-bucket problem —
// a throttling fault — instead of a bypass, and the failure a rate limit must never have is the
// one where a caller picks their own bucket. TestTheRouterResolvesTheClientAddress is what holds
// the wiring itself.
func ClientAddr(r *http.Request) string {
	if addr, ok := r.Context().Value(clientAddrKey{}).(string); ok {
		return addr
	}
	return resolveClientAddr(r, 0, nil)
}

// resolveClientAddr picks the client's address out of the request under the deployment's rules.
func resolveClientAddr(r *http.Request, hops int, trusted []netip.Prefix) string {
	peer := parseAddr(r.RemoteAddr)
	if !peer.IsValid() {
		// A transport that reports something that is not an address at all. Returned as
		// written rather than as an empty string, because internal/identity refuses an empty
		// address and a bucket that cannot be keyed must not become a bucket that is not
		// applied.
		return strings.TrimSpace(r.RemoteAddr)
	}

	if hops <= 0 && len(trusted) == 0 {
		return bucketAddr(peer)
	}

	// The peer is the last element and the only one that was observed rather than reported.
	chain := append(forwardedChain(r), peer)

	if hops > 0 {
		return bucketAddr(byHopCount(r, chain, hops, peer))
	}
	return bucketAddr(byTrustedNetworks(chain, trusted, peer))
}

// byHopCount takes the entry the configured number of proxies in from the right.
//
// The count is positional and ignores what the values look like, which is what makes it
// unspoofable — see the note at the top of this file on why the chain is read from the right.
func byHopCount(r *http.Request, chain []netip.Addr, hops int, peer netip.Addr) netip.Addr {
	i := len(chain) - 1 - hops
	if i >= 0 && chain[i].IsValid() {
		return chain[i]
	}

	// The chain is shorter than the deployment says it is, or the entry at the counted
	// position is not an address. This is a proxy that is not appending what it was configured
	// to append, and the consequence is that every caller behind it now shares the peer's
	// bucket — the exact throttle-the-world failure this ticket exists to remove.
	//
	// **It is logged on every request that hits it, and that is the intended volume.** The
	// condition is not reachable by a caller: a client can only make the chain longer, never
	// shorter, so this fires when the deployment is wrong and never because somebody attacked
	// it. A log line per request is the correct alarm for a limit that has silently stopped
	// working.
	LoggerFrom(r.Context()).LogAttrs(r.Context(), slog.LevelWarn,
		"the forwarded chain is shorter than TRUSTED_PROXY_HOPS says, so the client address "+
			"fell back to the peer and every caller behind it now shares one bucket",
		slog.Int("configured_hops", hops),
		slog.Int("chain_length", len(chain)))

	return peer
}

// byTrustedNetworks skips proxies from the right and returns the first entry that is not one.
func byTrustedNetworks(chain []netip.Addr, trusted []netip.Prefix, peer netip.Addr) netip.Addr {
	// The peer is not a proxy this deployment trusts, so whatever the header says was written
	// by somebody who reached this service directly. Ignoring it outright is what stops a
	// caller choosing their own bucket.
	if !trustedContains(trusted, peer) {
		return peer
	}

	for i := len(chain) - 1; i >= 0; i-- {
		switch {
		case !chain[i].IsValid():
			// An entry that is not an address. Nothing to the left of it can be
			// positioned reliably, so reasoning stops here rather than skipping over it
			// and returning something one place out.
			return peer
		case trustedContains(trusted, chain[i]):
			continue
		default:
			return chain[i]
		}
	}

	// Every entry was a trusted proxy, so the client is inside the trusted range too and there
	// is no untrusted hop to name. The peer is the answer that cannot have been chosen by the
	// caller; the alternative — believing the leftmost entry — is a bucket a caller inside the
	// allow-list picks for themselves.
	return peer
}

// forwardedChain parses X-Forwarded-For, left to right, preserving position.
//
// An unparseable entry is kept as an invalid [netip.Addr] rather than dropped, because dropping
// one shifts every entry to its left by a place — and a hop count landing one place out is a
// caller-controlled address believed as the client's. A client can put anything it likes in this
// header, so this has to be true of malformed input and not only of well-formed input.
//
// The header may appear more than once, and the values concatenate in order: net/http keeps
// repeated headers as separate values, and a proxy chain that sets one each produces exactly that.
func forwardedChain(r *http.Request) []netip.Addr {
	var chain []netip.Addr
	for _, header := range r.Header.Values(HeaderForwardedFor) {
		for _, part := range strings.Split(header, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			chain = append(chain, parseAddr(part))
		}
	}
	return chain
}

// trustedContains reports whether addr sits in any of the allowed networks.
func trustedContains(trusted []netip.Prefix, addr netip.Addr) bool {
	for _, n := range trusted {
		if n.Contains(addr) {
			return true
		}
	}
	return false
}

// parseAddr reads an address from RemoteAddr or from one forwarded entry.
//
// Three forms arrive here and all three are real: a bare address, which is what a proxy appends;
// host:port, which is what RemoteAddr carries; and the bracketed IPv6 form of the second. The port
// is dropped in every case, so that a caller does not get a fresh bucket per connection.
//
// The result is unmapped and unzoned, so ::ffff:10.0.0.1, 10.0.0.1 and fe80::1%eth0 each key on
// one bucket rather than on two that never meet.
func parseAddr(s string) netip.Addr {
	s = strings.TrimSpace(s)
	if s == "" {
		return netip.Addr{}
	}

	if ap, err := netip.ParseAddrPort(s); err == nil {
		return normalise(ap.Addr())
	}
	if a, err := netip.ParseAddr(s); err == nil {
		return normalise(a)
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		if a, err := netip.ParseAddr(strings.TrimSpace(host)); err == nil {
			return normalise(a)
		}
	}
	return netip.Addr{}
}

func normalise(a netip.Addr) netip.Addr {
	return a.Unmap().WithZone("")
}

// bucketAddr renders the address as the rate-limit key spells it.
//
// IPv6 is narrowed to its /64 — see the note at the top of this file. IPv4 is rendered as itself.
func bucketAddr(a netip.Addr) string {
	if !a.IsValid() {
		return ""
	}
	if a.Is6() {
		if p, err := a.Prefix(ipv6BucketBits); err == nil {
			return p.String()
		}
	}
	return a.String()
}

// ipv6BucketBits is the prefix one IPv6 caller is counted as.
//
// A /64 is the smallest block an operator assigns to a single subscriber, so it is the largest
// grouping that cannot merge two unrelated callers and the smallest that one caller cannot escape.
// Narrower and a single household holds thousands of buckets; wider and an operator's customers
// share one.
const ipv6BucketBits = 64
