package document

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

const (
	threadOne = `{"type":"inlineComment","attrs":{"threadId":"0195f000-0000-7000-8000-000000000001"}}`
	threadTwo = `{"type":"inlineComment","attrs":{"threadId":"0195f000-0000-7000-8000-000000000002"}}`
)

func paragraph(inline string) json.RawMessage {
	return json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","content":[` + inline + `]}]}`)
}

func TestPassagesMayOverlapButNotRepeat(t *testing.T) {
	if err := Validate(paragraph(`{"type":"text","text":"both","marks":[` + threadOne + `,` + threadTwo + `,{"type":"bold"}]}`)); err != nil {
		t.Errorf("two threads on one text are refused: %v", err)
	}
	for name, marks := range map[string]string{
		"one thread twice": threadOne + `,` + threadOne,
		"a bad id":         `{"type":"inlineComment","attrs":{"threadId":"x"}}`,
		"bold twice":       `{"type":"bold"},{"type":"bold"}`,
	} {
		if err := Validate(paragraph(`{"type":"text","text":"x","marks":[` + marks + `]}`)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: got %v, want a refusal", name, err)
		}
	}
}

func TestWithoutAnchorsReadsAsIfNeverMarked(t *testing.T) {
	marked := paragraph(`{"type":"text","text":"The "},{"type":"text","text":"plan","marks":[` + threadOne + `]},{"type":"text","text":" is ","marks":[` + threadOne + `,{"type":"bold"}]},{"type":"text","text":"set.","marks":[{"type":"bold"}]}`)
	got, err := WithoutAnchors(marked)
	if err != nil {
		t.Fatal(err)
	}
	want := paragraph(`{"type":"text","text":"The plan"},{"type":"text","text":" is set.","marks":[{"type":"bold"}]}`)
	var a, b any
	_ = json.Unmarshal(got, &a)
	_ = json.Unmarshal(want, &b)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("got %s, want %s", got, want)
	}
	plain := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","attrs":{}}]}`)
	if same, _ := WithoutAnchors(plain); string(same) != string(plain) {
		t.Errorf("a body with no passages became %s", same)
	}
}
