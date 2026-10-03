package document

import (
	"encoding/json"
	"strings"
	"testing"
)

const propertiesDoc = `{"type":"doc","content":[
	{"type":"paragraph","content":[{"type":"text","text":"Intro"}]},
	{"type":"properties","content":[
		{"type":"propertyRow","attrs":{"key":" Owner "},"content":[{"type":"mention","attrs":{"id":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b","label":"Ada"}}]},
		{"type":"propertyRow","attrs":{"key":"Review   date"},"content":[{"type":"date","attrs":{"date":"2026-11-01"}}]},
		{"type":"propertyRow","attrs":{"key":""},"content":[{"type":"text","text":"half typed"}]},
		{"type":"propertyRow","attrs":{"key":"Status"}}
	]},
	{"type":"columns","content":[{"type":"column","content":[
		{"type":"properties","content":[
			{"type":"propertyRow","attrs":{"key":"owner"},"content":[{"type":"text","text":"Bob"}]},
			{"type":"propertyRow","attrs":{"key":"Budget"},"content":[{"type":"text","text":"12k","marks":[{"type":"bold"}]}]}
		]}
	]},{"type":"column","content":[{"type":"paragraph"}]}]}
]}`

func TestPropertiesAreReadInOrderTheFirstValueOfANameKept(t *testing.T) {
	var root Node
	if err := json.Unmarshal([]byte(propertiesDoc), &root); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range Properties(root) {
		got = append(got, p.Key+"="+p.Text)
	}
	if strings.Join(got, ";") != "Owner=@Ada;Review date=2026-11-01;Status=;Budget=12k" {
		t.Errorf("the properties: %v", got)
	}
	if p := Properties(root)[3]; len(p.Content) != 1 || p.Content[0].Marks[0].Type != "bold" {
		t.Errorf("a value keeps its marks: %+v", p)
	}
	if Properties(Node{Type: "doc"}) == nil {
		t.Error("a page without properties has an empty list")
	}
	if PropertyName("  Review   DATE ") != "review date" {
		t.Error("names are told apart by their words in any case")
	}
}

func TestPropertiesReadAsTableRowsInTheirWords(t *testing.T) {
	var root Node
	_ = json.Unmarshal([]byte(propertiesDoc), &root)
	want := "Intro\n Owner \t@Ada\nReview   date\t2026-11-01\n\thalf typed\nStatus\nowner\tBob\nBudget\t12k"
	if got := PlainText(root); got != want {
		t.Errorf("PlainText is %q", got)
	}
}
