package document

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAnAnonymousReaderIsToldNobodysName(t *testing.T) {
	root, err := Parse(json.RawMessage(richDoc))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(root)
	out, err := json.Marshal(ForAnonymous(root))
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, personal := range []string{"Ada Lovelace", `"label":"Ada"`, "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01", "0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"} {
		if strings.Contains(got, personal) {
			t.Errorf("the anonymous document still holds %s", personal)
		}
	}
	for _, kept := range []string{`"type":"mention"`, `"assignee":"me"`, "report.pdf", "Ada", "CP-12"} {
		if !strings.Contains(got, kept) {
			t.Errorf("the anonymous document lost %s", kept)
		}
	}
	after, _ := json.Marshal(root)
	if string(before) != string(after) {
		t.Error("anonymizing changed the document it was handed")
	}
}

func TestAnAnonymousReaderSeesNoThreadsInTheText(t *testing.T) {
	root, err := Parse(json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[
		{"type":"text","text":"Discussed","marks":[{"type":"bold"},{"type":"inlineComment","attrs":{"threadId":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01"}}]}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	text := ForAnonymous(root).Content[0].Content[0]
	if len(text.Marks) != 1 || text.Marks[0].Type != "bold" {
		t.Errorf("the marks left are %v, want bold alone", text.Marks)
	}
}
