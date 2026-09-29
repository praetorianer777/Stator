// Command docallowlist writes the document allowlist, the nodes and marks a
// page may hold, as JSON to the path given or to stdout.
package main

import (
	"fmt"
	"os"

	"github.com/praetorianer777/stator/backend/internal/document"
)

func main() {
	encoded, err := document.Allowed.JSON()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(os.Args) > 1 {
		if err := os.WriteFile(os.Args[1], encoded, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	os.Stdout.Write(encoded)
}
