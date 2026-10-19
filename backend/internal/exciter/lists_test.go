package exciter

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/armature"
)

func TestAProjectKeyIsExcitersShape(t *testing.T) {
	for raw, want := range map[string]string{"CHK": "CHK", " chk ": "CHK", "A1": "A1"} {
		if got, ok := NormalizeProject(raw); !ok || got != want {
			t.Errorf("%q reads as %q %v, want %q", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"", "C", "1CHK", "CHK-R1", "CHECKOUTXYZ", "CH K"} {
		if key, ok := NormalizeProject(raw); ok {
			t.Errorf("%q reads as the project %q", raw, key)
		}
	}
}

func TestAQueryAndWordsAreBoundedInASentence(t *testing.T) {
	if err := CheckQuery(""); err != nil {
		t.Errorf("a blank query, which lists the whole project, is refused: %v", err)
	}
	var field *armature.FieldError
	if err := CheckQuery(strings.Repeat("é", MaxQueryLength+1)); !errors.As(err, &field) || field.Field != "query" {
		t.Errorf("a query too long is refused with %v", err)
	}
	if err := CheckQuery(strings.Repeat("é", MaxQueryLength)); err != nil {
		t.Errorf("a query of %d characters, counted as characters, is refused: %v", MaxQueryLength, err)
	}
	if got, err := CheckWords("  basket \t keeps "); err != nil || got != "basket keeps" {
		t.Errorf("the words read as %q, %v", got, err)
	}
	for _, words := range []string{" ", strings.Repeat("a", MaxSearchLength+1)} {
		if _, err := CheckWords(words); !errors.As(err, &field) || field.Field != "q" {
			t.Errorf("%d characters are refused with %v", len(words), err)
		}
	}
}

func TestThePickerOffersRequirementsAndNoSections(t *testing.T) {
	found := hits([]quickHit{
		{Requirement: gridRow{Key: "CHK-R1", ProjectKey: "CHK", Kind: "requirement", Title: "The basket"}, ProjectName: "Checkout"},
		{Requirement: gridRow{Key: "CHK-R9", ProjectKey: "CHK", Kind: "section", Title: "Payment"}, ProjectName: "Checkout"},
	}, "https://exciter.example.com")
	if len(found) != 1 || found[0].Key != "CHK-R1" || found[0].URL != "https://exciter.example.com/p/CHK/r/CHK-R1" || found[0].ProjectName != "Checkout" {
		t.Errorf("the picker offers %+v", found)
	}
}

func TestAListIsCachedForItsReaderAlone(t *testing.T) {
	alice, bob := uuid.New(), uuid.New()
	same := listField(alice, "CHK", "status = Approved", 20, 0)
	if same != listField(alice, "CHK", "status = Approved", 20, 0) {
		t.Error("the same list is cached under two names")
	}
	for name, other := range map[string]string{
		"another reader":  listField(bob, "CHK", "status = Approved", 20, 0),
		"another project": listField(alice, "SEC", "status = Approved", 20, 0),
		"another query":   listField(alice, "CHK", "status = Draft", 20, 0),
		"another window":  listField(alice, "CHK", "status = Approved", 20, 20),
	} {
		if other == same {
			t.Errorf("%s shares the cache entry", name)
		}
	}
}
