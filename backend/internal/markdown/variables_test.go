package markdown

import (
	"reflect"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/document"
)

// A template's variable leaves as a marked span and comes back as itself in a
// template, and as its words in a page, which holds no variables.
func TestATemplateVariableComesBackAsItLeft(t *testing.T) {
	body := doc(
		para(txt("Kickoff with "), `{"type":"templateVariable","attrs":{"name":"customer"},"marks":[{"type":"bold"}]}`, txt(" on "),
			`{"type":"templateVariable","attrs":{"name":"kick_off_day"}}`),
		`{"type":"heading","attrs":{"level":2},"content":[`+`{"type":"templateVariable","attrs":{"name":"owner"}}`+`]}`,
	)
	root, err := document.ParseTemplate([]byte(body))
	if err != nil {
		t.Fatalf("the test template is not valid: %v", err)
	}
	md := Render("Kickoff", root, testLinks)
	if !strings.Contains(md, `<span data-stator="variable" data-name="customer">{customer}</span>`) {
		t.Errorf("the variable is written as\n%s", md)
	}
	got, err := ConvertTemplate([]byte(md), testResolver)
	if err != nil {
		t.Fatalf("convert:\n%s\n%v", md, err)
	}
	if !reflect.DeepEqual(canon(t, []byte(body)), canon(t, got.Body)) || len(got.Warnings) > 0 {
		t.Errorf("the template changed on its way through\n%s\nwant %s\ngot  %s\n%v", md, body, got.Body, got.Warnings)
	}

	page, err := Convert([]byte(md), testResolver)
	if err != nil {
		t.Fatalf("convert as a page: %v", err)
	}
	if s := string(page.Body); strings.Contains(s, "templateVariable") || !strings.Contains(s, "{customer}") {
		t.Errorf("a page read the variable as %s", s)
	}
	bad, err := ConvertTemplate([]byte(`<span data-stator="variable" data-name="Not A Name">x</span>`), nil)
	if err != nil || strings.Contains(string(bad.Body), "templateVariable") {
		t.Errorf("a malformed name came back as %s, %v", bad.Body, err)
	}
}
