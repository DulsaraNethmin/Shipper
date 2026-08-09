// Package migrations holds the SQL schema history and embeds it into the binary.
//
// Embedding rather than reading from disk means a deployed binary carries the exact
// migrations it was built against. A container that has the new binary but an old
// migrations directory mounted is not a failure mode worth keeping available.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
