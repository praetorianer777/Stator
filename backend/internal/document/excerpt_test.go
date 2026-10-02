package document

import (
	"encoding/json"
	"strings"
	"testing"
)

const (
	excerptA = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a71"
	excerptB = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a72"
)

func excerptJSON(id, name string, blocks ...string) string {
	return `{"type":"excerpt","attrs":{"id":"` + id + `","name":"` + name + `"},"content":[` + strings.Join(blocks, ",") + `]}`
}

func line(text string) string {
	return `{"type":"paragraph","content":[{"type":"text","text":"` + text + `"}]}`
}

func TestExcerptsAreListedInReadingOrder(t *testing.T) {
	body := `{"type":"doc","content":[` + line("Intro") + `,` +
		excerptJSON(excerptA, "Support hours", line("Nine to five"), line("on weekdays")) + `,` +
		`{"type":"panel","attrs":{"kind":"info"},"content":[` + excerptJSON(excerptB, "Escalation", line(strings.Repeat("word ", 60))) + `]}]}`
	if err := Validate(json.RawMessage(body)); err != nil {
		t.Fatal(err)
	}
	root, _ := Parse(json.RawMessage(body))
	got := Excerpts(root)
	if len(got) != 2 || got[0].ID != excerptA || got[0].Name != "Support hours" || got[0].Text != "Nine to five on weekdays" || got[1].Name != "Escalation" {
		t.Fatalf("Excerpts = %+v", got)
	}
	if n := []rune(got[1].Text); len(n) != MaxExcerptTextLength || !strings.HasSuffix(got[1].Text, "...") {
		t.Errorf("a long excerpt's text is %d long: %q", len(n), got[1].Text)
	}
	if n, ok := FindExcerpt(root, excerptB); !ok || n.Attrs["name"] != "Escalation" {
		t.Errorf("FindExcerpt = %v, %v", n, ok)
	}
	if _, ok := FindExcerpt(root, "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4aff"); ok {
		t.Error("an excerpt that is not there was found")
	}
}

func TestExcerptsAreUniqueAndNeverNested(t *testing.T) {
	cases := map[string]struct{ body, want string }{
		"nested":      {excerptJSON(excerptA, "Outer", excerptJSON(excerptB, "Inner", line("x"))), "inside another"},
		"nested deep": {excerptJSON(excerptA, "Outer", `{"type":"panel","attrs":{"kind":"info"},"content":[`+excerptJSON(excerptB, "Inner", line("x"))+`]}`), "inside another"},
		"same id":     {excerptJSON(excerptA, "One", line("x")) + "," + excerptJSON(excerptA, "Two", line("y")), "share one id"},
		"same name":   {excerptJSON(excerptA, "Hours", line("x")) + "," + excerptJSON(excerptB, " hours ", line("y")), `named "hours"`},
		"no name":     {excerptJSON(excerptA, "  ", line("x")), `name="  "`},
		"bad id":      {excerptJSON("not-an-id", "Hours", line("x")), `id="not-an-id"`},
		"empty":       {excerptJSON(excerptA, "Hours"), ""},
	}
	for name, c := range cases {
		err := Validate(json.RawMessage(`{"type":"doc","content":[` + c.body + `]}`))
		if c.want == "" {
			if err != nil {
				t.Errorf("%s: %v", name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
}
