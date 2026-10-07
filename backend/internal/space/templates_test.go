package space

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/document"
)

func TestChooseTemplate(t *testing.T) {
	if tpl, err := chooseTemplate(CreateInput{Key: "DOCS", Name: "Docs"}); tpl != nil || err != nil {
		t.Errorf("no template gives %v, %v; want a blank space", tpl, err)
	}
	tpl, err := chooseTemplate(CreateInput{Template: "team"})
	if err != nil || tpl == nil || tpl.Key != "team" {
		t.Fatalf("the team template gives %v, %v", tpl, err)
	}
	for name, in := range map[string]CreateInput{
		"unknown":  {Template: "no-such-template"},
		"page":     {Template: "meeting-notes"},
		"personal": {Template: "team", Personal: true},
	} {
		_, err := chooseTemplate(in)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != "template" || !strings.HasSuffix(fe.Message, ".") {
			t.Errorf("%s: %v, want a sentence about the template field", name, err)
		}
	}
}

// The lists a template puts on a page read the new space, not every space,
// and one that names a space keeps it.
func TestForSpaceScopesListsToTheNewSpace(t *testing.T) {
	body := json.RawMessage(`{"type":"doc","content":[
		{"type":"labelledPages","attrs":{"labels":["faq"],"match":"any","sort":"updated","limit":5,"space":null}},
		{"type":"panel","attrs":{"kind":"info"},"content":[{"type":"recentlyUpdated","attrs":{"limit":5,"space":null}}]},
		{"type":"recentlyUpdated","attrs":{"limit":5,"space":"OTHER"}},
		{"type":"childPages","attrs":{"depth":null,"scope":"children","sort":"tree"}}]}`)
	got, err := forSpace(body, "KB")
	if err != nil {
		t.Fatal(err)
	}
	if err := document.Validate(got); err != nil {
		t.Fatalf("the filled body no longer validates: %v", err)
	}
	root, _ := document.Parse(got)
	spaces := []any{root.Content[0].Attrs["space"], root.Content[1].Content[0].Attrs["space"], root.Content[2].Attrs["space"]}
	if spaces[0] != "KB" || spaces[1] != "KB" || spaces[2] != "OTHER" {
		t.Errorf("the lists read %v", spaces)
	}
	if _, set := root.Content[3].Attrs["space"]; set {
		t.Errorf("a block without a space was given one: %s", got)
	}
}
