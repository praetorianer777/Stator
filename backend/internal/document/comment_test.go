package document

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

const commentDoc = `{"type":"doc","content":[
 {"type":"heading","attrs":{"level":2,"id":null},"content":[{"type":"text","text":"Thoughts"}]},
 {"type":"paragraph","content":[
  {"type":"text","text":"Ask ","marks":[{"type":"bold"},{"type":"italic"}]},
  {"type":"mention","attrs":{"id":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01","label":"Ada Lovelace","mentionSuggestionChar":"@"}},
  {"type":"hardBreak"},
  {"type":"text","text":"old","marks":[{"type":"strike"},{"type":"code"}]},
  {"type":"text","text":"site","marks":[{"type":"link","attrs":{"href":"https://example.test","target":"_blank","rel":"noopener noreferrer nofollow","class":null,"title":null}}]}]},
 {"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"one"}]}]}]},
 {"type":"orderedList","attrs":{"start":1,"type":null},"content":[{"type":"listItem","content":[{"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"two"}]}]}]}]},
 {"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"x := 1"}]}
]}`

func TestACommentTakesTextAndItsStructure(t *testing.T) {
	root, err := ParseComment(json.RawMessage(commentDoc))
	if err != nil {
		t.Fatalf("a comment of every node it may hold was refused: %v", err)
	}
	if got := PlainText(root); !strings.Contains(got, "@Ada Lovelace") || !strings.Contains(got, "x := 1") {
		t.Errorf("the comment reads %q", got)
	}
}

func TestACommentRefusesWhatOnlyAPageMayHold(t *testing.T) {
	para := func(inner string) string {
		return `{"type":"doc","content":[{"type":"paragraph","content":[` + inner + `]}]}`
	}
	for name, body := range map[string]string{
		"a table":           `{"type":"doc","content":[{"type":"table","content":[]}]}`,
		"a panel":           `{"type":"doc","content":[{"type":"panel","attrs":{"kind":"info"},"content":[{"type":"paragraph"}]}]}`,
		"an expand block":   `{"type":"doc","content":[{"type":"expand","attrs":{"title":"More"},"content":[{"type":"paragraph"}]}]}`,
		"columns":           columns(column(`50`), column(`50`)),
		"an image":          `{"type":"doc","content":[{"type":"image","attrs":{"attachmentId":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b"}}]}`,
		"a task list":       `{"type":"doc","content":[{"type":"taskList","content":[]}]}`,
		"a rule":            `{"type":"doc","content":[{"type":"horizontalRule"}]}`,
		"a contents block":  `{"type":"doc","content":[{"type":"tableOfContents","attrs":{"maxLevel":2}}]}`,
		"a child pages":     `{"type":"doc","content":[{"type":"childPages","attrs":{"scope":"children","depth":null,"sort":"tree"}}]}`,
		"a file":            para(`{"type":"attachment","attrs":{"attachmentId":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b","fileName":"a.pdf"}}`),
		"an Armature issue": para(`{"type":"armatureIssue","attrs":{"key":"CP-12"}}`),
		"an issue block":    `{"type":"doc","content":[{"type":"armatureIssueBlock","attrs":{"key":"CP-12"}}]}`,
		"a hint":            para(`{"type":"text","text":"Say more","marks":[{"type":"hint"}]}`),
		"an anchor mark":    para(`{"type":"text","text":"here","marks":[{"type":"inlineComment","attrs":{"threadId":"x"}}]}`),
		"not a document":    `{"type":"paragraph"}`,
		"broken JSON":       `{"type":"doc"`,
	} {
		_, err := ParseComment(json.RawMessage(body))
		var bad *InvalidError
		if !errors.As(err, &bad) {
			t.Errorf("%s was not refused as invalid: %v", name, err)
			continue
		}
		if !strings.HasPrefix(bad.Message, "This comment") {
			t.Errorf("%s is refused as %q, which does not speak of a comment", name, bad.Message)
		}
	}
}

func TestACommentIsCappedBelowAPage(t *testing.T) {
	long := `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"` + strings.Repeat("a", MaxCommentBytes) + `"}]}]}`
	if _, err := ParseComment(json.RawMessage(long)); err == nil || !strings.Contains(err.Error(), "64 KB") {
		t.Fatalf("a comment over the cap is answered %v", err)
	}
	if err := Validate(json.RawMessage(long)); err != nil {
		t.Fatalf("the same text on a page was refused: %v", err)
	}
}

func TestTheCommentAllowlistIsASubsetByName(t *testing.T) {
	for name, spec := range CommentAllowed.Nodes {
		if !slices.Contains(CommentNodes, name) {
			t.Errorf("%s is in the comment allowlist but not named", name)
		}
		for _, child := range spec.Content {
			if _, ok := CommentAllowed.Nodes[child]; !ok {
				t.Errorf("%s may contain %s, which a comment may not hold", name, child)
			}
		}
		if !slices.Equal(maps(spec.Attrs), maps(Allowed.Nodes[name].Attrs)) {
			t.Errorf("%s has other attributes in a comment than on a page", name)
		}
	}
	if len(CommentAllowed.Nodes) != len(CommentNodes) || len(CommentAllowed.Marks) != len(CommentMarks) {
		t.Errorf("the comment allowlist holds %d nodes and %d marks", len(CommentAllowed.Nodes), len(CommentAllowed.Marks))
	}
	if _, ok := CommentAllowed.Marks["hint"]; ok {
		t.Error("a comment may carry a hint")
	}
	if !slices.Contains(Allowed.Nodes["doc"].Content, "table") {
		t.Error("taking the subset changed the page allowlist")
	}
}

func maps(attrs map[string]Attr) []string {
	var names []string
	for name := range attrs {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func TestTheGeneratedCommentAllowlistIsCurrent(t *testing.T) {
	want, err := CommentAllowed.JSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../../api/comment-allowlist.json")
	if err != nil {
		t.Fatalf("read api/comment-allowlist.json: %v", err)
	}
	if string(got) != string(want) {
		t.Error("api/comment-allowlist.json is out of date; run make document-allowlist")
	}
}
