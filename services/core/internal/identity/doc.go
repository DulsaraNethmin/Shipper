// Package identity owns registration, verification, credentials, and sessions — the
// first of the eight platform domains in Docs/06 §3.
//
// # What lives here
//
// Registration and role selection, argon2id password storage, email verification tokens,
// phone OTPs, access-token issue, per-device refresh sessions with rotation and reuse
// detection, and the rate limits that protect all of it. SHIP-28…SHIP-47.
//
// # Rules this domain is responsible for
//
//   - A role is chosen at registration and is immutable afterwards (SHIP-45). Changing
//     role means a new account, because a provider's bid history and a customer's job
//     history are not interchangeable.
//   - The mobile session token and the driver portal's job-scoped token are separate
//     systems, and neither can be exchanged for the other (Docs/06 §5.2). This package
//     owns the first, the delivery domain owns the second, and nothing joins them.
//   - No authorisation decision is made on the device (Docs/07 §3). This package answers
//     "who is this"; the domain being called answers "may they".
//
// # What exists so far
//
//   - password.go (SHIP-29) — argon2id hashing, with the cost parameters stored in the PHC
//     string beside each hash so the profile can be raised without a migration.
//   - token.go (SHIP-37) — access token issue: HS256 over a keyset selected by a kid header,
//     fifteen minutes, and a claim set that carries no permissions and no verification state.
//     Issue only; the middleware that verifies these is SHIP-44.
//   - service.go, postgres.go, http.go (SHIP-30) — registration: the domain rules, the SQL, and
//     POST /v1/auth/register. Duplicate addresses and numbers are refused by uq_users_email and
//     uq_users_phone rather than by a SELECT that ran first, because a check-then-insert is a
//     race two taps on a slow connection will lose.
//   - model.go — the two roles mirroring ck_users_role, the three account states mirroring
//     ck_users_status, and the User the endpoints return.
//
// The rest of the layout in Docs/10 §2.1 arrives with the tickets that need it: ports.go when
// this domain first needs something of an adapter.
//
// # Two things in http.go that should not be here
//
// apiHandler and decodeJSON are what Docs/10 §4.3 calls httpx.H and httpx.DecodeJSON, neither of
// which exists in internal/httpx. They are written here because a domain branch does not edit a
// shared surface mid-wave, and they are flagged in Docs/11 §9 so that the second domain to need
// a handler promotes them rather than copying them.
package identity
