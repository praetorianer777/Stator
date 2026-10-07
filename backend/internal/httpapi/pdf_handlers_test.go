package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/page"
)

func TestAPrintIsInTheReadersChosenLanguageElseTheirBrowsersElseEnglish(t *testing.T) {
	for _, tc := range []struct {
		chosen auth.Locale
		header string
		want   string
	}{
		{auth.LocaleGerman, "en-GB,en;q=0.9", "de"},
		{auth.LocaleBrowser, "fr-FR, de-AT;q=0.8, en;q=0.5", "de"},
		{auth.LocaleBrowser, "EN-us", "en"},
		{auth.LocaleBrowser, "fr, es", "en"},
		{auth.LocaleBrowser, "", "en"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept-Language", tc.header)
		if got := printLanguage(r, &auth.Principal{Locale: tc.chosen}); got != tc.want {
			t.Errorf("chosen %q with %q: %q, want %q", tc.chosen, tc.header, got, tc.want)
		}
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Accept-Language", "de")
	if got := printLanguage(r, nil); got != "de" {
		t.Errorf("nobody signed in with a German browser: %q", got)
	}
}

func TestAPDFIsNamedAfterItsSpacePageAndDay(t *testing.T) {
	day := time.Now().Format("2006-01-02")
	if got := pdfName("DOCS", "Setting up, again"); got != "DOCS-setting-up-again-"+day+".pdf" {
		t.Errorf("got %q", got)
	}
	if got := pdfName("", "Guide"); got != "guide-"+day+".pdf" || strings.HasPrefix(got, "-") {
		t.Errorf("a link's PDF is named %q", got)
	}
}

func TestOnlyPublishedWordsArePrinted(t *testing.T) {
	if printable(page.KindPage, 3) != nil || printable(page.KindPost, 1) != nil {
		t.Error("a published page or post was refused")
	}
	if printable(page.KindFolder, 1) != errPrintFolder {
		t.Error("a folder was printable")
	}
	if printable(page.KindPage, 0) != errPrintUnpublished {
		t.Error("a page never published was printable")
	}
}
