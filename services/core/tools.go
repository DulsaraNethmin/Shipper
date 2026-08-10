//go:build tools

// This file exists to pin dependencies that are chosen now but first imported later.
//
// `Docs/10` §9.2 makes adding a module a deliberate act rather than an opportunistic one,
// because two branches that each add a different dependency produce a `go.mod` conflict and
// a `go.sum` conflict, and the second of those must never be resolved by hand. Choosing the
// versions once, here, means the tickets that need them — SHIP-29 for argon2id, SHIP-37 for
// the access token — import a module that is already in the graph at a version somebody
// decided on.
//
// Without this file `go mod tidy` would drop both, since nothing imports them yet, and the
// choice would be made again by whoever happened to run `go get` first.
//
// The build tag keeps the file out of every real build; `go mod tidy` reads it regardless.
// Delete an entry once something genuinely imports it.
package tools

import (
	// SHIP-37, access token issue. See Docs/10 §5.
	_ "github.com/golang-jwt/jwt/v5"

	// SHIP-29, password hashing with argon2id. See Docs/10 §5.
	_ "golang.org/x/crypto/argon2"
)
