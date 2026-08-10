// Command lintboundaries fails the build when an import crosses a boundary that
// Docs/06 §4.1 says it may not (SHIP-11).
//
//	make lint-imports
//
// The same rules also run as a Go test (TestNoBoundaryIsCrossed), so a violation fails
// `go test ./...` whether or not anyone remembers to invoke this command. This exists so
// the check can be run and read on its own, with a message that says what to do instead.
package main

import (
	"fmt"
	"os"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/boundaries"
)

func main() {
	root, err := boundaries.ModuleRoot(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "lintboundaries: %v\n", err)
		os.Exit(2)
	}

	violations, err := boundaries.Check(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lintboundaries: %v\n", err)
		os.Exit(2)
	}

	if len(violations) == 0 {
		fmt.Printf("lintboundaries: no boundary crossed (%d domains, adapters under %s)\n",
			len(boundaries.Domains), boundaries.AdapterRoot)
		return
	}

	for _, v := range violations {
		fmt.Fprintln(os.Stderr, v)
	}
	fmt.Fprintf(os.Stderr, "\nlintboundaries: %d boundary violation(s)\n", len(violations))
	os.Exit(1)
}
