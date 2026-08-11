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
//   - verification.go (SHIP-31, SHIP-33) — email verification tokens: issue, resend, and confirm.
//   - otp.go (SHIP-34, SHIP-36) — phone one-time codes: issue, rate limit, and confirm.
//   - ports.go — what this domain needs of the email and SMS adapters, declared here because the
//     consumer declares the interface.
//   - model.go — the two roles mirroring ck_users_role, the three account states mirroring
//     ck_users_status, and the User the endpoints return.
//
// # Two hashes, chosen opposite ways
//
// The email token is 32 bytes from crypto/rand and is stored as SHA-256; the phone code is six
// digits and is stored as argon2id. A work factor exists to make a *small* search space
// expensive, so it is worth 64 MiB on 10^6 possibilities and worth nothing on 2^256. Getting it
// the other way round is the mistake worth naming: SHA-256 over six digits is a table a laptop
// builds in under a second, and argon2id on the email token would make the confirm endpoint a
// denial-of-service lever anybody can pull without an account.
//
// # What the endpoints deliberately do not say
//
// Registration reports a duplicate address, because the caller is trying to create the account
// and silence would leave somebody who mistyped their address on a success screen. Nothing else
// here reports whether contact details are known: resend and request-otp answer 202 with a fixed
// interval for every outcome, and verify-phone has exactly one failure code. The reasoning is on
// CodeEmailTaken and CodeOTPInvalid, and it is a decision rather than an inconsistency.
//
// # Two things that used to be in http.go
//
// apiHandler and decodeJSON lived here for one wave, because Docs/10 §4.3 named httpx.H and
// httpx.DecodeJSON as the way every handler is written and neither existed, and a domain branch
// does not edit a shared surface mid-wave. SHIP-15e promoted both into internal/httpx before a
// second domain copied them, which is what Docs/11 §9 had asked for. Handlers here now use
// httpx.H and httpx.DecodeJSON like everybody else.
package identity
