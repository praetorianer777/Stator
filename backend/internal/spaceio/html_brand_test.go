package spaceio

import (
	"strings"
	"testing"
)

func TestTheStyleSheetTakesTheOrganizationsAccent(t *testing.T) {
	base := []byte("a { color: var(--accent); }")
	if got := (*htmlBrand)(nil).style(base); string(got) != string(base) {
		t.Errorf("no brand changed the style sheet: %s", got)
	}
	if got := (&htmlBrand{name: "Acme"}).style(base); string(got) != string(base) {
		t.Errorf("a brand with no accent changed the style sheet: %s", got)
	}
	both := string((&htmlBrand{light: "#336699", dark: "#99ccff"}).style(base))
	if !strings.Contains(both, ":root { --accent: #336699; }") || !strings.Contains(both, "prefers-color-scheme: dark) { :root { --accent: #99ccff; }") {
		t.Errorf("the light and dark accents are not both there:\n%s", both)
	}
	onlyLight := string((&htmlBrand{light: "#336699"}).style(base))
	if strings.Count(onlyLight, "#336699") != 2 {
		t.Errorf("a dark scheme without an accent of its own takes the light one:\n%s", onlyLight)
	}
}

func TestOnlyAPictureABrowserShowsIsAHeaderLogo(t *testing.T) {
	var b *htmlBrand
	if b.logoPath() != "" {
		t.Error("no brand has a logo")
	}
	b = &htmlBrand{logoKey: "org/x/brand/logo-1", logoExt: "webp"}
	if b.logoPath() != "files/brand/logo.webp" {
		t.Errorf("the logo is at %q", b.logoPath())
	}
	for _, accent := range []string{"#336699", "#ABCDEF"} {
		if !accentHex.MatchString(accent) {
			t.Errorf("%s is a colour", accent)
		}
	}
	for _, accent := range []string{"red", "#fff", "#33669", "#336699; } body { display: none", "url(x)"} {
		if accentHex.MatchString(accent) {
			t.Errorf("%q is taken as a colour", accent)
		}
	}
}
