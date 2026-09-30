package comment

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/document"
)

var (
	threadA = uuid.MustParse("0195f000-0000-7000-8000-00000000000a")
	threadB = uuid.MustParse("0195f000-0000-7000-8000-00000000000b")
)

func txt(text string, marks ...map[string]any) map[string]any {
	n := map[string]any{"type": "text", "text": text}
	if len(marks) > 0 {
		n["marks"] = marks
	}
	return n
}

func anchor(id uuid.UUID) map[string]any {
	return map[string]any{"type": AnchorMark, "attrs": map[string]any{"threadId": id.String()}}
}

func para(inline ...map[string]any) map[string]any {
	return map[string]any{"type": "paragraph", "content": inline}
}

func docOf(t *testing.T, blocks ...map[string]any) json.RawMessage {
	t.Helper()
	out, err := json.Marshal(map[string]any{"type": "doc", "content": blocks})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// quotesIn reads back which text each thread's mark covers.
func quotesIn(t *testing.T, body json.RawMessage) map[uuid.UUID]string {
	t.Helper()
	root, err := document.Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	out := map[uuid.UUID]string{}
	var walk func(n document.Node)
	walk = func(n document.Node) {
		for _, m := range n.Marks {
			if id, ok := anchorOf(m); ok {
				out[id] += n.Text
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(root)
	return out
}

func TestANewPassageMayOnlyAddItsMark(t *testing.T) {
	bold := map[string]any{"type": "bold"}
	stored := docOf(t, para(txt("The plan is "), txt("bold", bold), txt(" and simple.")), para(txt("Second.")))

	t.Run("the mark alone, across styles, is taken with its quote", func(t *testing.T) {
		sent := docOf(t,
			para(txt("The "), txt("plan is ", anchor(threadA)), txt("bold", bold, anchor(threadA)), txt(" and simple.")),
			para(txt("Second.")))
		quote, err := NewAnchor(stored, sent, threadA)
		if err != nil || quote != "plan is bold" {
			t.Fatalf("got %q, %v", quote, err)
		}
	})

	t.Run("beside another thread's passage, overlapping it", func(t *testing.T) {
		before := docOf(t, para(txt("The plan is ", anchor(threadB)), txt("bold", bold), txt(" and simple.")), para(txt("Second.")))
		sent := docOf(t, para(txt("The ", anchor(threadB)), txt("plan is ", anchor(threadB), anchor(threadA)), txt("bold", bold), txt(" and simple.")), para(txt("Second.")))
		if quote, err := NewAnchor(before, sent, threadA); err != nil || quote != "plan is " {
			t.Fatalf("got %q, %v", quote, err)
		}
	})

	for name, sent := range map[string]json.RawMessage{
		"another word changed": docOf(t, para(txt("The "), txt("plan is ", anchor(threadA)), txt("bold", bold), txt(" and hard.")), para(txt("Second."))),
		"a style dropped":      docOf(t, para(txt("The "), txt("plan is ", anchor(threadA)), txt("bold and simple.")), para(txt("Second."))),
		"a block added":        docOf(t, para(txt("The "), txt("plan is ", anchor(threadA)), txt("bold", bold), txt(" and simple.")), para(txt("Second.")), para(txt("Third."))),
		"another mark too": docOf(t,
			para(txt("The "), txt("plan is ", anchor(threadA)), txt("bold", bold), txt(" and simple.")),
			para(txt("Second.", anchor(threadB)))),
	} {
		t.Run(name+" is a conflict", func(t *testing.T) {
			if _, err := NewAnchor(stored, sent, threadA); !errors.Is(err, ErrAnchorConflict) {
				t.Errorf("got %v, want anchor_conflict", err)
			}
		})
	}

	for name, sent := range map[string]json.RawMessage{
		"no mark":          stored,
		"two blocks":       docOf(t, para(txt("The plan is bold and simple.", anchor(threadA))), para(txt("Second.", anchor(threadA)))),
		"a gap":            docOf(t, para(txt("The", anchor(threadA)), txt(" plan is "), txt("bold", bold, anchor(threadA)), txt(" and simple.")), para(txt("Second."))),
		"only spaces":      docOf(t, para(txt("The plan is"), txt(" ", anchor(threadA)), txt("bold", bold), txt(" and simple.")), para(txt("Second."))),
		"not a document":   json.RawMessage(`{"type":"doc","content":[{"type":"iframe"}]}`),
		"nothing at all":   nil,
		"the mark doubled": docOf(t, para(txt("The plan is ", anchor(threadA), anchor(threadA)), txt("bold", bold), txt(" and simple.")), para(txt("Second."))),
	} {
		t.Run(name+" is refused on pageBody", func(t *testing.T) {
			_, err := NewAnchor(stored, sent, threadA)
			var field *FieldError
			if !errors.As(err, &field) || field.Field != "pageBody" {
				t.Errorf("got %v, want a refusal of pageBody", err)
			}
		})
	}

	t.Run("a long passage keeps the start of its words", func(t *testing.T) {
		long := make([]rune, MaxQuoteLength+20)
		for i := range long {
			long[i] = 'ä'
		}
		before := docOf(t, para(txt(string(long))))
		sent := docOf(t, para(txt(string(long), anchor(threadA))))
		quote, err := NewAnchor(before, sent, threadA)
		if err != nil || len([]rune(quote)) != MaxQuoteLength {
			t.Errorf("got %d characters, %v", len([]rune(quote)), err)
		}
	})
}

func TestSettlingAnchorsOnPublish(t *testing.T) {
	live := map[uuid.UUID]string{threadA: "plan", threadB: "Second"}

	t.Run("marks carried by the editor keep their threads where they are", func(t *testing.T) {
		body := docOf(t, para(txt("The "), txt("new plan", anchor(threadA)), txt(" is here.")), para(txt("Second", anchor(threadB))))
		s, err := Settle(body, live)
		if err != nil || len(s.Moved) != 0 || len(s.Detached) != 0 || string(s.Body) != string(body) {
			t.Fatalf("got %+v, %v", s, err)
		}
	})

	t.Run("a missing mark is put back where its quote occurs once", func(t *testing.T) {
		body := docOf(t, map[string]any{"type": "bulletList", "content": []any{
			map[string]any{"type": "listItem", "content": []any{para(txt("A "), txt("pla", map[string]any{"type": "bold"}), txt("n here."))}},
		}}, para(txt("Second", anchor(threadB))))
		s, err := Settle(body, live)
		if err != nil || !slices.Equal(s.Moved, []uuid.UUID{threadA}) || len(s.Detached) != 0 {
			t.Fatalf("got %+v, %v", s, err)
		}
		if got := quotesIn(t, s.Body); got[threadA] != "plan" || got[threadB] != "Second" {
			t.Errorf("the marks cover %v", got)
		}
		if err := document.Validate(s.Body); err != nil {
			t.Errorf("the settled body is refused: %v", err)
		}
	})

	t.Run("a quote gone, found twice or split over blocks detaches its thread", func(t *testing.T) {
		for name, body := range map[string]json.RawMessage{
			"gone":        docOf(t, para(txt("Nothing here.")), para(txt("Second", anchor(threadB)))),
			"twice":       docOf(t, para(txt("plan and plan")), para(txt("Second", anchor(threadB)))),
			"over blocks": docOf(t, para(txt("pl")), para(txt("an")), para(txt("Second", anchor(threadB)))),
		} {
			s, err := Settle(body, live)
			if err != nil || !slices.Equal(s.Detached, []uuid.UUID{threadA}) || len(s.Moved) != 0 {
				t.Errorf("%s: got %+v, %v", name, s, err)
			}
			if _, marked := quotesIn(t, s.Body)[threadA]; marked {
				t.Errorf("%s: the detached thread is still marked", name)
			}
		}
	})

	t.Run("marks of no live thread are dropped and the text joined again", func(t *testing.T) {
		stranger := uuid.MustParse("0195f000-0000-7000-8000-0000000000ff")
		body := docOf(t, para(txt("A "), txt("plan", anchor(threadA), anchor(stranger)), txt(" here.")), para(txt("Second", anchor(threadB))))
		s, err := Settle(body, map[uuid.UUID]string{threadA: "plan"})
		if err != nil {
			t.Fatal(err)
		}
		got := quotesIn(t, s.Body)
		if len(got) != 1 || got[threadA] != "plan" {
			t.Errorf("the marks left are %v", got)
		}
		s, err = Settle(body, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := docOf(t, para(txt("A plan here.")), para(txt("Second")))
		var read, wanted any
		_ = json.Unmarshal(s.Body, &read)
		_ = json.Unmarshal(want, &wanted)
		if !reflect.DeepEqual(read, wanted) {
			t.Errorf("got %s, want %s", s.Body, want)
		}
	})

	t.Run("a body with no marks and no threads is left as it came", func(t *testing.T) {
		body := json.RawMessage(`{"type":"doc","content":[{"type":"paragraph","attrs":{}}]}`)
		s, err := Settle(body, nil)
		if err != nil || string(s.Body) != string(body) {
			t.Errorf("got %s, %v", s.Body, err)
		}
	})
}
