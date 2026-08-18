package identity

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/ratelimit"
)

// The Credential class's per-address bucket (SHIP-183, SHIP-183b).
//
// # What this bounds that the per-account bucket does not
//
// Docs/12 §3 keys the Credential class on the identifier *and* the address, and the second half is
// the one that matters against somebody working through a list rather than through one password.
// The account bucket stops five guesses at one account; this stops thirty guesses from one place,
// whichever accounts they are aimed at.
//
// # Why one bucket rather than one per route
//
// This domain serves four routes that test a secret — sign-in, refresh, email verification and
// phone verification — and Docs/12 §9 settled the shape when SHIP-183a asked the same question of
// the middleware classes: **one bucket per class per caller, never one per route.** The figures in
// §3 are a budget for a *kind* of work, and four buckets of thirty is a hundred and twenty guesses
// from an address the document intended to allow thirty. An attacker who found the account bucket
// too tight would simply spread the same campaign across the four endpoints.
//
// So all four spend from `credential:address:<addr>`, and an attacker's budget is the class's
// rather than the class's times the number of ways in.
//
// # Why internal/admin has its own and does not share this one
//
// Two reasons that point the same way. The domains cannot import each other, so a shared key would
// have to be two string constants agreeing by comment — which is what Docs/10 §3.4 refuses. And
// the administrator console is a separate credential system from the mobile one by an invariant
// CLAUDE.md names, so a bucket spanning both would let traffic against one throttle sign-in to the
// other. The consequence is stated rather than buried: **an attacker who probes both surfaces from
// one address gets thirty on each rather than thirty in total.** That is a factor of two against a
// control whose figure is already an order of magnitude above legitimate use, and it is recorded
// in Docs/12 §11 rather than left to be discovered.
//
// # Why the address is not hashed here, where the account identifier is
//
// [signInBuckets] hashes the email because a cache dump would otherwise enumerate every address
// anybody has tried to sign in as, including the ones that have accounts — the key would disclose
// something the platform works hard not to. A network address discloses nothing of the kind: it is
// not a secret, not an identifier of a person, and it is already in every request log.

// credentialAddressBucket is the Credential class's address half, from Docs/12 §3.
func credentialAddressBucket() ratelimit.Bucket {
	return ratelimit.Bucket{Capacity: credentialAddressCapacity, Interval: credentialAddressInterval}
}

// credentialAddressKey names the bucket one network address counts against, across every route in
// this domain that tests a secret.
//
// The address arrives from [httpx.ClientAddr], which reads a forwarded header only as far as the
// deployment's trusted-proxy configuration allows and RemoteAddr otherwise — so what is keyed here
// is never something the caller chose (SHIP-183b).
func credentialAddressKey(addr string) string {
	return "credential:address:" + addr
}

// admitCredentialAddress refuses an attempt from an address whose allowance is gone.
//
// It is consulted *before* the secret is checked, and Docs/12 §6 is explicit about why the
// tempting alternative destroys the limit: honouring a correct credential even while throttled
// makes every wrong guess a 429 and the right one a 200, so the attacker guesses indefinitely and
// the split tells them the moment they have won.
func (s *Service) admitCredentialAddress(ctx context.Context, addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		// Not a validation error the caller could act on — they did not supply this, the
		// transport did. An address the platform cannot determine is not an address with no
		// limit, so this fails rather than defaulting to unlimited.
		return fmt.Errorf("identity: the request carries no client address, so Docs/12's " +
			"Credential class has nothing to count against")
	}

	decision, err := s.limiter.Allow(ctx, credentialAddressKey(addr), credentialAddressBucket())
	if err != nil {
		// Fail closed, and 503 rather than 429: "this is temporarily unavailable" is true
		// where "you have done too much" is not. A limiter an attacker turns off by taking
		// Redis down is not a limiter (Docs/12 §4).
		return fmt.Errorf("%w: %w", errUnavailable, err)
	}
	if !decision.Allowed {
		return &ThrottledError{RetryAfter: decision.RetryAfter}
	}
	return nil
}

// chargeCredentialAddress records one refused credential against the address.
//
// # Why a failure to record is logged rather than returned
//
// The attempt has already been refused and the caller is being told so. Turning a Redis blip into
// a different answer would replace a correct refusal with a 503, which tells the client to retry —
// and the one thing that must not happen at this point is the platform inviting more attempts.
// [Service.admitCredentialAddress] is where an unreachable Redis fails closed; this is where it is
// merely counted.
func (s *Service) chargeCredentialAddress(ctx context.Context, addr string) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return
	}

	if _, err := s.limiter.Spend(ctx, credentialAddressKey(addr), credentialAddressBucket()); err != nil {
		httpx.LoggerFrom(ctx).LogAttrs(ctx, slog.LevelWarn,
			"a refused credential could not be counted against its rate limit",
			slog.String("error", err.Error()))
	}
}
