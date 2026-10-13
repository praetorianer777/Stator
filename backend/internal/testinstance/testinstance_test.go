package testinstance

import (
	"net/mail"
	"strings"
	"testing"
)

func TestAnyLabelMakesAnAddress(t *testing.T) {
	for label, want := range map[string]string{
		"alice-themes-0a1b2c3d":         "alice-themes-0a1b2c3d",
		"Bob Themes.spec.ts":            "bob-themes-spec-ts",
		"  --spaces  and   pages--  ":   "spaces-and-pages",
		"":                              defaultLabel,
		"???":                           defaultLabel,
		"Überschrift":                   "berschrift",
		strings.Repeat("abcdefghij", 6): "abcdefghijabcdefghijabcdefghijabcdefghij",
	} {
		got := localPart(label)
		if got != want {
			t.Errorf("localPart(%q) = %q, want %q", label, got, want)
		}
		if _, err := mail.ParseAddress(got + "-0123abcd@" + PeopleDomain); err != nil {
			t.Errorf("%q makes %q, which is no address: %v", label, got, err)
		}
	}
}
