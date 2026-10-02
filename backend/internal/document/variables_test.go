package document

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func variable(name string) string {
	return `{"type":"templateVariable","attrs":{"name":"` + name + `"}}`
}

func TestATemplateHoldsVariablesWhereInlineContentGoes(t *testing.T) {
	body := `{"type":"doc","content":[
		{"type":"paragraph","content":[{"type":"text","text":"For "},` + variable("customer") + `]},
		{"type":"heading","attrs":{"level":2,"id":"x"},"content":[` + variable("day") + `]},
		{"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"b"},` + variable("owner_2") + `]}]}]}
	]}`
	root, err := ParseTemplate(json.RawMessage(body))
	if err != nil {
		t.Fatalf("a template with variables was refused: %v", err)
	}
	if got := PlainText(root); !strings.Contains(got, "For {customer}") || !strings.Contains(got, "{owner_2}") {
		t.Errorf("the plain text is %q", got)
	}
	if err := Validate(json.RawMessage(body)); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "templateVariable") {
		t.Errorf("a page holding a variable: %v", err)
	}
	if _, err := ParseComment(json.RawMessage(inlinePara(variable("x")))); err == nil {
		t.Error("a comment took a variable")
	}
}

func TestATemplateVariableIsRefusedInASentence(t *testing.T) {
	for name, body := range map[string]string{
		"a name in capitals":  inlinePara(variable("Customer")),
		"a name with a dash":  inlinePara(variable("a-b")),
		"a block of its own":  `{"type":"doc","content":[` + variable("x") + `]}`,
		"inside a code block": `{"type":"doc","content":[{"type":"codeBlock","content":[` + variable("x") + `]}]}`,
		"an attribute more":   inlinePara(`{"type":"templateVariable","attrs":{"name":"x","label":"X"}}`),
	} {
		_, err := ParseTemplate(json.RawMessage(body))
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if msg := err.Error(); !strings.HasPrefix(msg, "This template") || !strings.HasSuffix(msg, ".") {
			t.Errorf("%s: %q", name, msg)
		}
	}
}

func TestTheTemplateAllowlistIsThePagesAndAVariable(t *testing.T) {
	if len(TemplateAllowed.Nodes) != len(Allowed.Nodes)+1 || len(TemplateAllowed.Marks) != len(Allowed.Marks) {
		t.Fatalf("%d nodes and %d marks, the page has %d and %d", len(TemplateAllowed.Nodes), len(TemplateAllowed.Marks), len(Allowed.Nodes), len(Allowed.Marks))
	}
	if _, ok := Allowed.Nodes[NodeVariable]; ok {
		t.Error("a page's allowlist names the variable")
	}
	for _, n := range Allowed.Nodes {
		for _, child := range n.Content {
			if child == NodeVariable {
				t.Error("a page's node may hold a variable")
			}
		}
	}
	want, err := TemplateAllowed.JSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../../api/template-allowlist.json")
	if err != nil {
		t.Fatalf("read api/template-allowlist.json: %v", err)
	}
	if string(got) != string(want) {
		t.Error("api/template-allowlist.json is out of date; run make document-allowlist")
	}
}
