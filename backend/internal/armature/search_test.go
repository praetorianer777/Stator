package armature

import (
	"errors"
	"strings"
	"testing"
)

func TestSearchURLOpensTheQueryInArmature(t *testing.T) {
	got := SearchURL("https://armature.example.com/", `project = CP AND summary ~ "a&b"`)
	want := "https://armature.example.com/search?q=project+%3D+CP+AND+summary+~+%22a%26b%22"
	if got != want {
		t.Errorf("SearchURL = %q, want %q", got, want)
	}
}

func TestCheckQueryRefusesBlankAndOverlongQueries(t *testing.T) {
	for _, ok := range []string{"project = CP", strings.Repeat("é", MaxQueryLength)} {
		if err := CheckQuery(ok); err != nil {
			t.Errorf("CheckQuery refused a query of %d characters: %v", len([]rune(ok)), err)
		}
	}
	for name, bad := range map[string]string{"empty": "", "blank": " \t\n", "too long": strings.Repeat("a", MaxQueryLength+1)} {
		var field *FieldError
		if err := CheckQuery(bad); !errors.As(err, &field) || field.Field != "q" || field.Message == "" {
			t.Errorf("%s: CheckQuery = %v, want a sentence on q", name, err)
		}
	}
}
