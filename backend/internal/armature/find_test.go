package armature

import (
	"errors"
	"strings"
	"testing"
)

func TestQuoteNQLKeepsTypedTextOneString(t *testing.T) {
	for in, want := range map[string]string{
		"onboarding":                     `"onboarding"`,
		`say "hi"`:                       `"say \"hi\""`,
		`C:\temp`:                        `"C:\\temp"`,
		`\"`:                             `"\\\""`,
		`" OR project = SEC OR text ~ "`: `"\" OR project = SEC OR text ~ \""`,
		"it's 50% done":                  `"it's 50% done"`,
	} {
		if got := QuoteNQL(in); got != want {
			t.Errorf("QuoteNQL(%q) = %s, want %s", in, got, want)
		}
	}
}

// unquote reads a literal back as NQL's lexer does: a backslash takes the
// next character as it is, and the first bare quote ends the string.
func unquote(t *testing.T, literal string) (text, rest string) {
	t.Helper()
	runes := []rune(literal)
	if len(runes) == 0 || runes[0] != '"' {
		t.Fatalf("%s does not open with a quote", literal)
	}
	var b strings.Builder
	for i := 1; i < len(runes); i++ {
		switch runes[i] {
		case '\\':
			if i+1 < len(runes) {
				i++
				b.WriteRune(runes[i])
			}
		case '"':
			return b.String(), string(runes[i+1:])
		default:
			b.WriteRune(runes[i])
		}
	}
	t.Fatalf("%s is never closed", literal)
	return "", ""
}

func TestQuoteNQLReadsBackAsExactlyTheTypedText(t *testing.T) {
	for _, in := range []string{"", `\`, `\\"`, `"`, `a"b\c`, `" ORDER BY key --`, "naïve 'quoted' words"} {
		text, rest := unquote(t, QuoteNQL(in)+" ORDER BY updated DESC")
		if text != in || rest != " ORDER BY updated DESC" {
			t.Errorf("QuoteNQL(%q) reads back as %q, then %q", in, text, rest)
		}
	}
}

func TestFindQueryLooksInSummaryAndDescriptionNewestFirst(t *testing.T) {
	if got, want := FindQuery(`sign "in"`), `text ~ "sign \"in\"" ORDER BY updated DESC`; got != want {
		t.Errorf("FindQuery = %s, want %s", got, want)
	}
}

func TestCheckWordsTidiesSpaceAndRefusesNoneOrTooMany(t *testing.T) {
	got, err := CheckWords("  onboarding \t\n guide ")
	if err != nil || got != "onboarding guide" {
		t.Errorf("CheckWords = %q, %v", got, err)
	}
	if _, err := CheckWords(strings.Repeat("é", MaxFindLength)); err != nil {
		t.Errorf("CheckWords refused %d characters: %v", MaxFindLength, err)
	}
	for name, bad := range map[string]string{"empty": "", "blank": " \t\n", "too long": strings.Repeat("a", MaxFindLength+1)} {
		var field *FieldError
		if _, err := CheckWords(bad); !errors.As(err, &field) || field.Field != "words" || field.Message == "" {
			t.Errorf("%s: CheckWords = %v, want a sentence on words", name, err)
		}
	}
}
