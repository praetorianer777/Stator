package armature

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestASummaryIsItsWordsOnOneLine(t *testing.T) {
	for raw, want := range map[string]string{
		"  Renew the   TLS\ncertificate\t": "Renew the TLS certificate",
		"One":                              "One",
	} {
		if got, ok := Summary(raw); got != want || !ok {
			t.Errorf("Summary(%q) = %q %v, want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"", " \n\t ", strings.Repeat("ä", MaxSummaryLength+1)} {
		if _, ok := Summary(raw); ok {
			t.Errorf("Summary(%q) is taken", raw)
		}
	}
	if _, ok := Summary(strings.Repeat("ä", MaxSummaryLength)); !ok {
		t.Error("a summary of the most characters is refused; they are counted as bytes")
	}
}

func TestACreateIsCheckedBeforeArmatureIsAsked(t *testing.T) {
	ok := CreateIssuesInput{ProjectKey: " cp ", Items: []CreateItem{{Summary: " First  item "}}}
	if err := CheckCreate(&ok); err != nil || ok.ProjectKey != "CP" || ok.Items[0].Summary != "First item" {
		t.Errorf("CheckCreate = %v, leaving %+v", err, ok)
	}
	many := make([]CreateItem, MaxCreateItems+1)
	for i := range many {
		many[i] = CreateItem{Summary: "Item"}
	}
	for name, tc := range map[string]struct {
		in    CreateIssuesInput
		field string
	}{
		"no project":     {CreateIssuesInput{ProjectKey: " ", Items: []CreateItem{{Summary: "A"}}}, "projectKey"},
		"no items":       {CreateIssuesInput{ProjectKey: "CP"}, "items"},
		"too many items": {CreateIssuesInput{ProjectKey: "CP", Items: many}, "items"},
		"a blank one":    {CreateIssuesInput{ProjectKey: "CP", Items: []CreateItem{{Summary: "A"}, {Summary: " "}}}, "items"},
	} {
		err := CheckCreate(&tc.in)
		field, isField := err.(*FieldError)
		if !isField || field.Field != tc.field || !strings.HasSuffix(field.Message, ".") {
			t.Errorf("%s: %v, want a sentence on %s", name, err, tc.field)
		}
	}
}

func TestAnIssueDescriptionLinksBackToItsPage(t *testing.T) {
	id := uuid.MustParse("0195f000-0000-7000-8000-000000000001")
	url := PageURL("https://wiki.example.com/", "ENG", id)
	if url != "https://wiki.example.com/s/ENG/p/0195f000-0000-7000-8000-000000000001" {
		t.Errorf("the page's address is %s", url)
	}
	raw, _ := json.Marshal(Description("Review notes", url))
	want := `{"content":[{"content":[{"text":"From ","type":"text"},{"marks":[{"attrs":{"href":"` + url + `"},"type":"link"}],"text":"Review notes","type":"text"},{"text":" in Stator","type":"text"}],"type":"paragraph"}],"type":"doc"}`
	if string(raw) != want {
		t.Errorf("the description is\n%s\nwant\n%s", raw, want)
	}
}
