// Command docallowlist writes the page, comment and template allowlists and
// the rules of a sketch's drawing as JSON to the paths given, in that order,
// or the page's allowlist to stdout.
package main

import (
	"fmt"
	"os"

	"github.com/praetorianer777/stator/backend/internal/document"
)

func main() {
	lists := []func() ([]byte, error){document.Allowed.JSON, document.CommentAllowed.JSON, document.TemplateAllowed.JSON, document.SketchDrawing.JSON}
	if len(os.Args) == 1 {
		encoded, err := document.Allowed.JSON()
		if err != nil {
			fail(err)
		}
		os.Stdout.Write(encoded)
		return
	}
	for i, path := range os.Args[1:] {
		if i >= len(lists) {
			fail(fmt.Errorf("give at most %d paths", len(lists)))
		}
		encoded, err := lists[i]()
		if err != nil {
			fail(err)
		}
		if err := os.WriteFile(path, encoded, 0o644); err != nil {
			fail(err)
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
