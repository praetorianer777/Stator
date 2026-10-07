package httpapi

import (
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/page"
)

func TestOnlyPublishedWordsAreExportedToWord(t *testing.T) {
	if exportable(page.KindPage, 3) != nil || exportable(page.KindPost, 1) != nil {
		t.Error("a published page or post was refused")
	}
	if exportable(page.KindFolder, 1) != errExportFolder {
		t.Error("a folder was exportable")
	}
	if exportable(page.KindPage, 0) != errExportUnpublished {
		t.Error("a page never published was exportable")
	}
}

func TestAnExportedPageLinksToItsAddressWhenTheServerKnowsIt(t *testing.T) {
	id := uuid.MustParse("6f1c2b1e-0000-4000-8000-000000000001")
	s := &Server{AppBaseURL: "https://stator.example"}
	if got := s.appURL(appPagePath("DOC", id, "Setting up")); got != "https://stator.example/s/DOC/p/"+id.String()+"/setting-up" {
		t.Errorf("got %q", got)
	}
	if got := (&Server{}).appURL("/s/DOC"); got != "" {
		t.Errorf("a server without an address links to %q", got)
	}
}
