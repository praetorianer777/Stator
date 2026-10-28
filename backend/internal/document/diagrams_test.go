package document

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestADiagramNamesALanguageOfTheSetOrNone(t *testing.T) {
	doc := func(attrs string) json.RawMessage {
		return json.RawMessage(`{"type":"doc","content":[{"type":"diagram","attrs":` + attrs + `}]}`)
	}
	for _, ok := range []string{
		`{"source":"graph TD"}`,
		`{"source":"graph TD","language":"mermaid"}`,
		`{"source":"a -> b","language":"plantuml"}`,
		`{"source":"workspace {}","language":"structurizr"}`,
		`{"source":"digraph { a -> b }","language":"graphviz"}`,
		`{"source":"a -> b","language":"d2"}`,
	} {
		if err := Validate(doc(ok)); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{
		`{"source":"<xml/>","language":"bpmn"}`,
		`{"source":"a -> b","language":"PlantUML"}`,
		`{"source":"a -> b","language":""}`,
		`{"source":"a -> b","language":null}`,
	} {
		if err := Validate(doc(bad)); err == nil {
			t.Errorf("%s was taken", bad)
		}
	}
}

func TestEveryDiagramIsFoundWhereverItSits(t *testing.T) {
	var root Node
	if err := json.Unmarshal([]byte(`{"type":"doc","content":[
		{"type":"diagram","attrs":{"source":"graph TD"}},
		{"type":"columns","content":[{"type":"column","content":[
			{"type":"panel","content":[{"type":"diagram","attrs":{"source":"a -> b","language":"plantuml"}}]}
		]}]},
		{"type":"expand","content":[{"type":"diagram","attrs":{"source":"x -> y","language":"d2"}}]}
	]}`), &root); err != nil {
		t.Fatal(err)
	}
	want := []Diagram{{DiagramMermaid, "graph TD"}, {DiagramPlantUML, "a -> b"}, {DiagramD2, "x -> y"}}
	if got := Diagrams(root); !reflect.DeepEqual(got, want) {
		t.Errorf("found %v, want %v", got, want)
	}
}
