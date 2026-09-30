package comment

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestABodyMustSaySomething(t *testing.T) {
	for name, body := range map[string]string{
		"nothing":          ``,
		"null":             `null`,
		"an empty doc":     `{"type":"doc","content":[]}`,
		"empty paragraphs": `{"type":"doc","content":[{"type":"paragraph"},{"type":"paragraph"}]}`,
		"only spaces":      `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"   "}]}]}`,
		"only a break":     `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"hardBreak"}]}]}`,
		"a table":          `{"type":"doc","content":[{"type":"table","content":[]}]}`,
	} {
		_, err := cleanBody(json.RawMessage(body))
		var field *FieldError
		if !errors.As(err, &field) || field.Field != "body" {
			t.Errorf("%s is answered %v, want a refusal of body", name, err)
		}
	}
	for name, body := range map[string]string{
		"words":     `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Looks good"}]}]}`,
		"a mention": `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"mention","attrs":{"id":"0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a01","label":"Ada"}}]}]}`,
	} {
		if _, err := cleanBody(json.RawMessage(body)); err != nil {
			t.Errorf("%s was refused: %v", name, err)
		}
	}
}

func TestCommentingNeedsAPublishedPageAndTheRight(t *testing.T) {
	p := &onPage{}
	if !errors.Is(p.canComment(), ErrUnpublished) {
		t.Error("an unpublished page takes comments")
	}
	p.published = true
	if p.canComment() == nil {
		t.Error("somebody without addComments may comment")
	}
	p.access.Comment = true
	if err := p.canComment(); err != nil {
		t.Errorf("a commenter on a published page is refused: %v", err)
	}
}
