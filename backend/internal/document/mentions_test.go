package document

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/google/uuid"
)

const (
	ada   = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01"
	alan  = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a02"
	grace = "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a03"
)

func mentionNode(id, label string) string {
	return `{"type":"mention","attrs":{"id":"` + id + `","label":"` + label + `"}}`
}

func mentionsOf(t *testing.T, body string) []Mention {
	t.Helper()
	root, err := Parse(json.RawMessage(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return Mentions(root)
}

func TestMentionsFindsEachPersonOnceWithTheirBlock(t *testing.T) {
	body := `{"type":"doc","content":[
	 {"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":"Owners"}]},
	 {"type":"paragraph","content":[{"type":"text","text":"Ask "},` + mentionNode(ada, "Ada") + `,{"type":"text","text":" and "},` + mentionNode(alan, "Alan") + `]},
	 {"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[` + mentionNode(ada, "Ada") + `,{"type":"text","text":" again"}]}]}]},
	 {"type":"table","content":[{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"Lead: "},` + mentionNode(grace, "Grace") + `]}]}]}]},
	 {"type":"paragraph","content":[` + mentionNode("u1", "Nobody") + `,` + mentionNode("0199A1B2-C3D4-7E5F-8A9B-0C1D2E3F4A04", "Shouting") + `]}
	]}`
	got := mentionsOf(t, body)
	want := []Mention{
		{ID: uuid.MustParse(ada), Block: "Ask @Ada and @Alan"},
		{ID: uuid.MustParse(alan), Block: "Ask @Ada and @Alan"},
		{ID: uuid.MustParse(grace), Block: "Lead: @Grace"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mentions are %+v, want %+v", got, want)
	}
}

func TestMentionsOfADocumentWithoutAnyIsEmpty(t *testing.T) {
	if got := mentionsOf(t, `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"@Ada"}]}]}`); len(got) != 0 {
		t.Fatalf("typed words mention %+v", got)
	}
	if got := MentionIDs(nil); got == nil || len(got) != 0 {
		t.Fatalf("no mentions are %#v, want an empty list", got)
	}
}

func TestNewMentionsAreThoseTheEarlierVersionLacks(t *testing.T) {
	para := func(ids ...string) string {
		body := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Hi"}`
		for _, id := range ids {
			body += `,` + mentionNode(id, "Someone")
		}
		return body + `]}]}`
	}
	cases := []struct {
		name          string
		before, after string
		want          []string
	}{
		{"the first version names everybody", `{"type":"doc","content":[]}`, para(ada, alan), []string{ada, alan}},
		{"a republish names nobody anew", para(ada, alan), para(alan, ada), []string{}},
		{"an added person is new", para(ada), para(ada, grace), []string{grace}},
		{"a removed person is not", para(ada, alan), para(alan), []string{}},
		{"removed and named again is new against the version before", para(alan), para(alan, ada), []string{ada}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MentionIDs(NewMentions(mentionsOf(t, c.before), mentionsOf(t, c.after)))
			want := make([]uuid.UUID, 0, len(c.want))
			for _, id := range c.want {
				want = append(want, uuid.MustParse(id))
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("new mentions are %v, want %v", got, want)
			}
		})
	}
}
