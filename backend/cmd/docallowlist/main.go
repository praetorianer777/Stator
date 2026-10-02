// Command docallowlist writes the document allowlists as JSON: what a page
// may hold to the first path given, what a comment may hold to the
// second, what a template may hold to the third, or the page's to stdout
// when no path is given.
package main

import (
	"fmt"
	"os"

	"github.com/praetorianer777/stator/backend/internal/document"
)

func main() {
	lists := []document.Allowlist{document.Allowed, document.CommentAllowed, document.TemplateAllowed}
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
		encoded, err := lists[i].JSON()
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
