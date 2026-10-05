package template_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/label"
	"github.com/praetorianer777/stator/backend/internal/page"
	"github.com/praetorianer777/stator/backend/internal/perm"
	"github.com/praetorianer777/stator/backend/internal/template"
)

// Seeded pages skip what publishing does for tasks, mentions and threads, so
// a template may not carry them; a hint would be stripped on the way in.
var unseedable = []string{"taskList", "taskItem", "mention", "hint", document.AnchorMark}

func kinds(n document.Node, into map[string]bool) {
	into[n.Type] = true
	for _, m := range n.Marks {
		into[m.Type] = true
	}
	for _, c := range n.Content {
		kinds(c, into)
	}
}

func listed(n document.Node, into *[]string) {
	if n.Type == document.NodeLabelledPages {
		for _, l := range n.Attrs["labels"].([]any) {
			*into = append(*into, l.(string))
		}
	}
	for _, c := range n.Content {
		listed(c, into)
	}
}

// checkBody holds one body to what a seeded page may be, and returns the
// labels its lists ask for.
func checkBody(t *testing.T, where string, body json.RawMessage) []string {
	t.Helper()
	if err := document.Validate(body); err != nil {
		t.Errorf("%s: the allowlist refuses the body: %v", where, err)
		return nil
	}
	root, _ := document.Parse(body)
	found := map[string]bool{}
	kinds(root, found)
	for _, k := range unseedable {
		if found[k] {
			t.Errorf("%s carries %s, which a seeded page cannot", where, k)
		}
	}
	for _, h := range document.Headings(root) {
		if h.Anchor == "" {
			t.Errorf("%s: heading %q has no anchor", where, h.Text)
		}
	}
	var asked []string
	listed(root, &asked)
	return asked
}

func TestEverySpaceTemplateIsAStructureASpaceWouldTake(t *testing.T) {
	want := []string{"knowledge-base", "team", "documentation"}
	list := template.SpaceBuiltIns()
	if len(list) != len(want) {
		t.Fatalf("%d built-in space templates, want %d", len(list), len(want))
	}
	presets := map[string]bool{}
	for i, tpl := range list {
		if tpl.Key != want[i] {
			t.Errorf("space template %d is %q, want %q", i, tpl.Key, want[i])
		}
		if strings.TrimSpace(tpl.Name) == "" || strings.TrimSpace(tpl.Description) == "" || !tpl.BuiltIn {
			t.Errorf("%s: %+v", tpl.Key, tpl)
		}
		asked := checkBody(t, tpl.Key+" home", tpl.Home)
		carried := map[string]bool{}
		pages := 0
		var siblings func(where string, ps []template.SpacePage)
		siblings = func(where string, ps []template.SpacePage) {
			titles := map[string]bool{}
			for _, p := range ps {
				if titles[p.Title] {
					t.Errorf("%s: two pages under %s are titled %q", tpl.Key, where, p.Title)
				}
				titles[p.Title] = true
				siblings(p.Title, p.Children)
			}
		}
		siblings("the home page", tpl.Pages)
		tpl.Walk(func(p template.SpacePage) {
			pages++
			where := tpl.Key + ": " + p.Title
			if strings.TrimSpace(p.Title) != p.Title || p.Title == "" || utf8.RuneCountInString(p.Title) > page.MaxTitleLength {
				t.Errorf("%s: the title is not one a page takes", where)
			}
			asked = append(asked, checkBody(t, where, p.Body)...)
			if len(p.Labels) > label.MaxPerPage {
				t.Errorf("%s carries %d labels", where, len(p.Labels))
			}
			for i, l := range p.Labels {
				if got, err := label.Normalize(l); err != nil || got != l {
					t.Errorf("%s: label %q is not a label as typed (%q, %v)", where, l, got, err)
				}
				if slices.Contains(p.Labels[:i], l) {
					t.Errorf("%s: label %q twice", where, l)
				}
				carried[l] = true
			}
		})
		if pages == 0 {
			t.Errorf("%s makes no page below the home page", tpl.Key)
		}
		for _, l := range asked {
			if !carried[l] {
				t.Errorf("%s lists pages labelled %q, which none of its pages is", tpl.Key, l)
			}
		}
		for _, p := range tpl.Permissions.Everyone {
			if !slices.Contains(perm.SpacePermissions, p) || p == perm.SpaceAdminister {
				t.Errorf("%s grants everyone %q, which is not a permission a template hands out", tpl.Key, p)
			}
		}
		preset := fmt.Sprint(tpl.Permissions.Everyone)
		if presets[preset] {
			t.Errorf("%s grants what another template grants: %s", tpl.Key, preset)
		}
		presets[preset] = true
	}
}

func TestSpaceByKey(t *testing.T) {
	got, err := template.SpaceByKey("documentation")
	if err != nil || got.Name != "Documentation" || len(got.Pages) == 0 {
		t.Fatalf("SpaceByKey(documentation) = %+v, %v", got, err)
	}
	if _, err := template.SpaceByKey("meeting-notes"); err != template.ErrUnknownSpace {
		t.Fatalf("a page template's key gives %v", err)
	}
}
