package space

import (
	"errors"
	"strings"
	"testing"
)

func TestValidKey(t *testing.T) {
	for _, k := range []string{"AB", "DOCS", "A1", "ABCDEFGHIJ", "X9Y8Z7"} {
		if !ValidKey(k) {
			t.Errorf("ValidKey(%q) = false, want true", k)
		}
	}
	for _, k := range []string{"", "A", "1AB", "docs", "A-B", "A B", "ABCDEFGHIJK", "A_B", "ÄB"} {
		if ValidKey(k) {
			t.Errorf("ValidKey(%q) = true, want false", k)
		}
	}
}

func TestNormalizeKey(t *testing.T) {
	for in, want := range map[string]string{"  docs  ": "DOCS", "docs": "DOCS", "DOCS": "DOCS", "": ""} {
		if got := NormalizeKey(in); got != want {
			t.Errorf("NormalizeKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSuggestKey(t *testing.T) {
	for _, tt := range []struct{ name, want string }{
		{"Engineering", "ENGINEERIN"},
		{"Team Handbook", "TH"},
		{"docs", "DOCS"},
		{"Q & A", "QA"},
		{"X", "XX"},
		{"2026 Plans", ""},
		{"", ""},
		{"!!!", ""},
	} {
		if got := SuggestKey(tt.name); got != tt.want {
			t.Errorf("SuggestKey(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// Whatever SuggestKey offers has to be a key the database takes, or an
// unusual name becomes a refused form.
func TestSuggestKeyOffersOnlyUsableKeys(t *testing.T) {
	for _, name := range []string{"Engineering", "Team Handbook", "X", "Ünïcödé Späce", "A Very Long Name With Many Words In It Indeed", "docs 2"} {
		if got := SuggestKey(name); got != "" && !ValidKey(got) {
			t.Errorf("SuggestKey(%q) = %q, which the key constraint refuses", name, got)
		}
	}
}

func TestFieldsAreRefusedInSentences(t *testing.T) {
	refused := []struct {
		field string
		err   error
	}{
		{"key", checkKey("")},
		{"key", checkKey("1AB")},
		{"key", checkKey("TOOLONGAKEY")},
		{"name", func() error { _, err := cleanName("   "); return err }()},
		{"name", func() error { _, err := cleanName(strings.Repeat("n", MaxNameLength+1)); return err }()},
		{"description", func() error { _, err := cleanDescription(strings.Repeat("d", MaxDescriptionLength+1)); return err }()},
	}
	for _, r := range refused {
		var field *FieldError
		if !errors.As(r.err, &field) {
			t.Errorf("want a refusal of %s, got %v", r.field, r.err)
			continue
		}
		if field.Field != r.field || !strings.HasSuffix(field.Message, ".") || strings.ToUpper(field.Message[:1]) != field.Message[:1] {
			t.Errorf("the refusal of %s is %q on %s, want a sentence on %s", r.field, field.Message, field.Field, r.field)
		}
	}
	if err := checkKey("DOCS"); err != nil {
		t.Errorf("DOCS was refused: %v", err)
	}
	if name, err := cleanName("  Handbook  "); err != nil || name != "Handbook" {
		t.Errorf("cleanName trims to %q, %v", name, err)
	}
}
