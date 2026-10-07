package template

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/document"
)

func blank(name string, marks ...string) document.Node {
	n := document.Node{Type: document.NodeVariable, Attrs: map[string]any{"name": name}}
	for _, m := range marks {
		n.Marks = append(n.Marks, document.Mark{Type: m})
	}
	return n
}

func text(s string) document.Node { return document.Node{Type: "text", Text: s} }

func para(inline ...document.Node) document.Node {
	return document.Node{Type: "paragraph", Content: inline}
}

func doc(blocks ...document.Node) document.Node { return document.Node{Type: "doc", Content: blocks} }

func raw(t *testing.T, n document.Node) json.RawMessage {
	t.Helper()
	out, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func fieldOf(t *testing.T, err error) (string, string) {
	t.Helper()
	var fe *FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("want a field error, got %v", err)
	}
	return fe.Field, fe.Message
}

var meeting = []Variable{
	{Name: "customer", Label: "Customer", Kind: Text, Required: true},
	{Name: "day", Label: "Day", Kind: Date, Default: Today},
	{Name: "stage", Label: "Stage", Kind: Select, Options: []string{"Lead", "Won"}, Default: "Lead"},
	{Name: "owner", Label: "Owner", Kind: Person},
	{Name: "notes", Label: "Notes", Kind: Text},
}

func TestVariablesAreTidiedAndTheUnaskableRefused(t *testing.T) {
	got, err := cleanVariables([]Variable{
		{Name: " customer ", Label: " ", Kind: Text, Default: "  Acme   Corp ", Options: []string{"x"}},
		{Name: "stage", Label: "Stage", Kind: Select, Options: []string{" Lead ", "Lead", "", "Won"}, Default: "Won"},
		{Name: "day", Label: "Day", Kind: Date, Default: "today"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Name != "customer" || got[0].Label != "customer" || got[0].Default != "Acme Corp" || len(got[0].Options) != 0 {
		t.Errorf("a text variable came out as %+v", got[0])
	}
	if strings.Join(got[1].Options, "|") != "Lead|Won" {
		t.Errorf("the choices came out as %q", got[1].Options)
	}

	for name, bad := range map[string][]Variable{
		"a name with capitals":       {{Name: "Customer", Kind: Text}},
		"a name starting with digit": {{Name: "1st", Kind: Text}},
		"the reserved date":          {{Name: "date", Kind: Date}},
		"two of one name":            {{Name: "a", Kind: Text}, {Name: "a", Kind: Date}},
		"an unknown kind":            {{Name: "a", Kind: "number"}},
		"a choice without choices":   {{Name: "a", Kind: Select, Options: []string{" "}}},
		"a default not offered":      {{Name: "a", Kind: Select, Options: []string{"x"}, Default: "y"}},
		"a day that does not exist":  {{Name: "a", Kind: Date, Default: "2026-02-30"}},
		"a person with a default":    {{Name: "a", Kind: Person, Default: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01"}},
		"a label too long":           {{Name: "a", Label: strings.Repeat("x", MaxLabelLength+1), Kind: Text}},
		"a default too long":         {{Name: "a", Kind: Text, Default: strings.Repeat("x", MaxTextLength+1)}},
	} {
		if _, err := cleanVariables(bad); err == nil {
			t.Errorf("%s was taken", name)
		} else if field, msg := fieldOf(t, err); !strings.HasPrefix(field, "variables") || !strings.HasSuffix(msg, ".") {
			t.Errorf("%s: refused on %s with %q", name, field, msg)
		}
	}
	many := make([]Variable, MaxVariables+1)
	for i := range many {
		many[i] = Variable{Name: "v" + strings.Repeat("x", i), Kind: Text}
	}
	if _, err := cleanVariables(many); err == nil {
		t.Errorf("%d variables were taken", len(many))
	}
}

func TestABodyMayOnlyUseTheVariablesItDefines(t *testing.T) {
	in := Input{Name: "Kickoff", Body: raw(t, doc(para(text("For "), blank("customer")))), Variables: meeting}
	if _, err := in.clean(); err != nil {
		t.Fatalf("a body using a defined variable: %v", err)
	}
	in.Body = raw(t, doc(para(blank("budget"))))
	if field, msg := fieldOf(t, func() error { _, err := in.clean(); return err }()); field != "body" || !strings.Contains(msg, "budget") {
		t.Errorf("an undefined variable is refused on %s with %q", field, msg)
	}
	in.Body = raw(t, doc(para(document.Node{Type: document.NodeVariable})))
	if field, _ := fieldOf(t, func() error { _, err := in.clean(); return err }()); field != "body" {
		t.Errorf("a variable without a name is refused on %s", field)
	}
	in.Body = raw(t, doc(document.Node{Type: "codeBlock", Content: []document.Node{blank("customer")}}))
	if _, err := in.clean(); !errors.Is(err, document.ErrInvalid) {
		t.Errorf("a variable inside a code block: %v", err)
	}
	for name, bad := range map[string]Input{
		"no name":        {Body: raw(t, doc(para())), Name: " "},
		"no body":        {Name: "x"},
		"a long title":   {Name: "x", Body: raw(t, doc(para())), Title: strings.Repeat("t", MaxTitleLength+1)},
		"a long summary": {Name: "x", Body: raw(t, doc(para())), Description: strings.Repeat("d", MaxDescriptionLength+1)},
	} {
		if _, err := bad.clean(); err == nil {
			t.Errorf("%s was taken", name)
		}
	}
}

func TestValuesResolveToWhatWasGivenElseTheDefault(t *testing.T) {
	got, err := Resolve(meeting, map[string]string{"customer": " Acme\n Corp ", "owner": "0199A1B2-C3D4-7E5F-8A9B-0C1D2E3F4A01"}, "2026-10-02")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"customer": "Acme Corp", "day": "2026-10-02", "stage": "Lead", "owner": "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01", "notes": ""}
	for name, value := range want {
		if got[name] != value {
			t.Errorf("%s resolved to %q, want %q", name, got[name], value)
		}
	}
	for name, values := range map[string]map[string]string{
		"values.customer": {},
		"values.day":      {"customer": "x", "day": "tomorrow"},
		"values.stage":    {"customer": "x", "stage": "Lost"},
		"values.owner":    {"customer": "x", "owner": "bob"},
		"values.budget":   {"customer": "x", "budget": "1"},
	} {
		_, err := Resolve(meeting, values, "2026-10-02")
		if err == nil {
			t.Errorf("%v was taken", values)
			continue
		}
		if field, msg := fieldOf(t, err); field != name || !strings.HasSuffix(msg, ".") {
			t.Errorf("%v is refused on %s with %q, want %s", values, field, msg, name)
		}
	}
}

func TestFillingLeavesNoVariableAndHintsAnEmptyOne(t *testing.T) {
	root := doc(
		para(text("For "), blank("customer", "bold"), text(" on "), blank("day")),
		para(text("Owner: "), blank("owner"), text(" Stage: "), blank("stage")),
		document.Node{Type: "heading", Attrs: map[string]any{"level": float64(2)}, Content: []document.Node{blank("notes")}},
	)
	values := map[string]string{"customer": "Acme", "day": "2026-10-02", "stage": "Won", "owner": "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01", "notes": ""}
	out := Fill(root, meeting, values, People{"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01": "Ada Lovelace"})
	body := raw(t, out)
	if err := document.Validate(body); err != nil {
		t.Fatalf("the filled body is no page: %v\n%s", err, body)
	}
	s := string(body)
	for _, needle := range []string{
		`{"type":"text","text":"Acme","marks":[{"type":"bold"}]}`,
		`{"type":"date","attrs":{"date":"2026-10-02"}}`,
		`"type":"mention","attrs":{"id":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01","label":"Ada Lovelace","mentionSuggestionChar":"@"}`,
		`{"type":"text","text":" Stage: Won"}`,
		`{"type":"text","text":"Notes","marks":[{"type":"hint"}]}`,
	} {
		if !strings.Contains(s, needle) {
			t.Errorf("the filled body lacks %s:\n%s", needle, s)
		}
	}
	if strings.Contains(s, document.NodeVariable) {
		t.Errorf("a variable is left: %s", s)
	}
	if !strings.Contains(string(raw(t, root)), document.NodeVariable) {
		t.Error("filling changed the template it was given")
	}
}

func TestATitleTakesTheValuesByName(t *testing.T) {
	values := map[string]string{"customer": "Acme", "owner": "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01", "notes": ""}
	people := People{"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01": "Ada"}
	for pattern, want := range map[string]string{
		"Kickoff with {customer}":         "Kickoff with Acme",
		"{customer} for {owner}":          "Acme for Ada",
		"Notes {notes} for {customer}":    "Notes for Acme",
		"{unknown} {customer} {Customer}": "{unknown} Acme {Customer}",
		"{notes}":                         "",
	} {
		if got := FillTitle(pattern, meeting, values, people); got != want {
			t.Errorf("%q filled to %q, want %q", pattern, got, want)
		}
	}
}

func TestEveryKindIsOffered(t *testing.T) {
	if len(Kinds) != 4 || len(Scopes) != 3 {
		t.Errorf("kinds %v, scopes %v", Kinds, Scopes)
	}
	if !namePattern.MatchString("customer_name2") || namePattern.MatchString("a-b") {
		t.Error("the name pattern is not the allowlist's")
	}
}
