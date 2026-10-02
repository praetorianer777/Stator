package shortcut

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestOnlyWebAddressesAreKept(t *testing.T) {
	for raw, want := range map[string]string{
		"https://example.com":                    "https://example.com",
		"HTTPS://Example.com/Path?q=1#top":       "https://Example.com/Path?q=1#top",
		"http://intranet:8080/wiki":              "http://intranet:8080/wiki",
		"https://[2001:db8::1]/":                 "https://[2001:db8::1]/",
		"https://example.com/@team":              "https://example.com/@team",
		"https://example.com/a%20b":              "https://example.com/a%20b",
		"https://armature.example.com/i/PROJ-12": "https://armature.example.com/i/PROJ-12",
	} {
		got, _, err := CleanURL(raw)
		if err != nil || got != want {
			t.Errorf("CleanURL(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{
		"javascript:alert(1)",
		"javascript://example.com/%0Aalert(1)",
		"JaVaScRiPt:alert(1)",
		"data:text/html,<script>alert(1)</script>",
		"vbscript:msgbox(1)",
		"file:///etc/passwd",
		"ftp://example.com",
		"//example.com",
		"/s/DOCS",
		"example.com",
		"https://",
		"https:///path",
		"https://user:secret@example.com",
		"https://example.com@evil.example",
		"https://exa mple.com",
		"https://example.com/\nnext",
		"https://example.com/\x00",
		"https://example.com/" + strings.Repeat("a", MaxURLLength),
	} {
		got, _, err := CleanURL(raw)
		var fe *FieldError
		if !errors.As(err, &fe) || fe.Field != "url" {
			t.Errorf("CleanURL(%q) = %q, %v; want a refusal of the url", raw, got, err)
			continue
		}
		if !strings.HasSuffix(fe.Message, ".") {
			t.Errorf("the refusal of %q is not a sentence: %q", raw, fe.Message)
		}
	}
}

func TestAnInputNamesAPageOrAnAddress(t *testing.T) {
	page := uuid.New()
	for name, tt := range map[string]struct {
		in    ShortcutInput
		field string
		link  string
		label string
	}{
		"nothing":            {in: ShortcutInput{}, field: "url"},
		"both":               {in: ShortcutInput{PageID: &page, URL: "https://example.com"}, field: "url"},
		"a page":             {in: ShortcutInput{PageID: &page, Label: "  Runbook  "}, label: "Runbook"},
		"a page untitled":    {in: ShortcutInput{PageID: &page}},
		"a link named":       {in: ShortcutInput{URL: " https://example.com/x ", Label: "Status"}, link: "https://example.com/x", label: "Status"},
		"a link by its host": {in: ShortcutInput{URL: "https://status.example.com/now"}, link: "https://status.example.com/now", label: "status.example.com"},
		"a long label":       {in: ShortcutInput{URL: "https://example.com", Label: strings.Repeat("é", MaxLabelLength+1)}, field: "label"},
		"a script":           {in: ShortcutInput{URL: "javascript:alert(1)", Label: "Click"}, field: "url"},
	} {
		link, label, err := tt.in.Clean()
		var fe *FieldError
		switch {
		case tt.field != "" && (!errors.As(err, &fe) || fe.Field != tt.field):
			t.Errorf("%s: %v, want a refusal of %s", name, err, tt.field)
		case tt.field == "" && (err != nil || link != tt.link || label != tt.label):
			t.Errorf("%s: %q %q %v, want %q %q", name, link, label, err, tt.link, tt.label)
		}
	}
}

func TestAFullSpaceSaysWhatToDo(t *testing.T) {
	msg := (&FullError{}).Error()
	if !strings.Contains(msg, "30") || !strings.HasSuffix(msg, ".") || !strings.Contains(msg, "Remove one") {
		t.Errorf("the refusal reads %q", msg)
	}
}
