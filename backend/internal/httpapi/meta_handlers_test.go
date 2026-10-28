package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/diagram"
	"github.com/praetorianer777/stator/backend/internal/version"
)

// Armature, Exciter and the web client tell which build answers by this, so
// it must say exactly what the linker stamped, with no session at all.
func TestMetaReportsTheBuildStamp(t *testing.T) {
	saved := [3]string{version.Version, version.Commit, version.BuiltAt}
	t.Cleanup(func() { version.Version, version.Commit, version.BuiltAt = saved[0], saved[1], saved[2] })
	version.Version, version.Commit, version.BuiltAt = "0.1.0+4.gabc1234", "abc1234", "2026-10-14T09:00:00Z"

	rec := httptest.NewRecorder()
	newServer(t).Routes(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, APIPrefix+"/meta", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("meta = %d %s", rec.Code, rec.Body)
	}
	raw, _ := io.ReadAll(rec.Body)
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"product":      "stator",
		"build":        map[string]any{"version": "0.1.0+4.gabc1234", "commit": "abc1234", "builtAt": "2026-10-14T09:00:00Z"},
		"apiVersion":   Spec().Info.Version,
		"capabilities": []any{"oauth.partner-tokens@1", "oauth.token-exchange@1", "page-links@1", "page-remote-links@1", "theme-export@1", "webhook-receiver@1", "webhooks@1"},
		// Without Kroki a server draws Mermaid alone, in the browser.
		"diagramLanguages": []any{"mermaid"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("meta =\n%s\nwant %v", raw, want)
	}
}

// The editor offers the languages meta names, so a server with Kroki names
// every language it draws through it.
func TestMetaNamesTheDiagramLanguagesKrokiDraws(t *testing.T) {
	s := newServer(t)
	s.Diagrams = diagram.New("http://kroki:8000", nil, nil)
	rec := httptest.NewRecorder()
	s.Routes(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, APIPrefix+"/meta", nil))
	var got struct {
		DiagramLanguages []string `json:"diagramLanguages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if want := []string{"mermaid", "plantuml", "structurizr", "graphviz", "d2"}; !reflect.DeepEqual(got.DiagramLanguages, want) {
		t.Errorf("meta names %v, want %v", got.DiagramLanguages, want)
	}
}
