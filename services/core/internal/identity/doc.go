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
// The package is empty at SHIP-10 by design. The skeleton exists so the boundaries are
// enforced before there is code to bend them — see the file layout and the boundary rules
// in services/core/README.md.
package identity
