//go:build integration

package test

import (
	"context"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// tree is a space's pages as a test arranges and reads them over the api.
type tree struct {
	t      *testing.T
	c      *client
	key    string
	homeID string
}

func newTree(t *testing.T, c *client, key, name string) *tree {
	t.Helper()
	sp := obj(t, want(t, c.post(t, "/api/v1/spaces", map[string]any{"key": key, "name": name}), http.StatusCreated, "make "+key), "space")
	return &tree{t: t, c: c, key: key, homeID: sp["homePageId"].(string)}
}

// add makes a page under a parent, with more fields such as afterId.
func (tr *tree) add(parent, title string, more ...map[string]any) string {
	tr.t.Helper()
	body := map[string]any{"parentId": parent, "title": title}
	for _, m := range more {
		for k, v := range m {
			body[k] = v
		}
	}
	return obj(tr.t, want(tr.t, tr.c.post(tr.t, "/api/v1/pages", body), http.StatusCreated, "add "+title), "page")["id"].(string)
}

// titles lists the titles directly under a parent, in order.
func (tr *tree) titles(parent string) []string {
	tr.t.Helper()
	path := "/api/v1/spaces/" + tr.key + "/pages"
	if parent != "" {
		path += "?parent=" + parent
	}
	var out []string
	for _, each := range list(tr.t, want(tr.t, tr.c.get(tr.t, path), http.StatusOK, "list "+path), "pages") {
		out = append(out, each.(map[string]any)["title"].(string))
	}
	return out
}

func (tr *tree) move(id string, body map[string]any) response {
	tr.t.Helper()
	return tr.c.post(tr.t, "/api/v1/pages/"+id+"/move", body)
}

func sameTitles(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %v, want %v", what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: %v, want %v", what, got, want)
		}
	}
}

// Pages nest without limit, keep the order they are put in, move anywhere
// but under themselves, and move or copy with their subtree into another space.
func TestThePageTreeOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "tree")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	member := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)

	docs := newTree(t, owner, "DOCS", "Docs")
	other := newTree(t, owner, "OTHER", "Other")
	docs.c = member

	p1 := docs.add(docs.homeID, "One")
	p2 := docs.add(docs.homeID, "Two")
	p3 := docs.add(docs.homeID, "Three")
	c1 := docs.add(p1, "One A")
	docs.add(p1, "One B")
	g1 := docs.add(c1, "One A i")

	t.Run("children list in the order they were put in, a level at a time", func(t *testing.T) {
		sameTitles(t, "under home", docs.titles(""), "One", "Two", "Three")
		sameTitles(t, "under One", docs.titles(p1), "One A", "One B")
		got := list(t, want(t, member.get(t, "/api/v1/spaces/DOCS/pages"), http.StatusOK, "top"), "pages")
		if got[0].(map[string]any)["hasChildren"] != true || got[1].(map[string]any)["hasChildren"] != false {
			t.Fatalf("hasChildren is wrong: %v", got)
		}
		docs.add(docs.homeID, "Zero", map[string]any{"beforeId": p1})
		docs.add(docs.homeID, "One and a half", map[string]any{"afterId": p1})
		sameTitles(t, "placed", docs.titles(""), "Zero", "One", "One and a half", "Two", "Three")
	})

	t.Run("a page knows its ancestors, and the outline reads in order", func(t *testing.T) {
		page := obj(t, want(t, member.get(t, "/api/v1/pages/"+g1), http.StatusOK, "read"), "page")
		var trail []string
		for _, each := range page["ancestors"].([]any) {
			trail = append(trail, each.(map[string]any)["title"].(string))
		}
		sameTitles(t, "ancestors", trail, "Docs", "One", "One A")
		var outline []string
		for _, each := range list(t, want(t, member.get(t, "/api/v1/spaces/DOCS/outline"), http.StatusOK, "outline"), "pages") {
			outline = append(outline, each.(map[string]any)["title"].(string))
		}
		sameTitles(t, "outline", outline, "Docs", "Zero", "One", "One A", "One A i", "One B", "One and a half", "Two", "Three")
	})

	t.Run("reordering and nesting by moving", func(t *testing.T) {
		want(t, docs.move(p3, map[string]any{"parentId": docs.homeID, "beforeId": p2}), http.StatusOK, "Three before Two")
		want(t, docs.move(p2, map[string]any{"parentId": p1}), http.StatusOK, "Two under One")
		sameTitles(t, "top", docs.titles(""), "Zero", "One", "One and a half", "Three")
		sameTitles(t, "under One", docs.titles(p1), "One A", "One B", "Two")
	})

	t.Run("a move under itself, below itself or of the home page is refused", func(t *testing.T) {
		for what, got := range map[string]response{
			"under itself":         docs.move(p1, map[string]any{"parentId": p1}),
			"under its grandchild": docs.move(p1, map[string]any{"parentId": g1}),
			"the home page":        docs.move(docs.homeID, map[string]any{"parentId": p1}),
			"next to a stranger":   docs.move(p3, map[string]any{"parentId": docs.homeID, "afterId": g1}),
		} {
			if got.Status != http.StatusConflict || errorCode(t, got) != "conflict" {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
		sameTitles(t, "nothing moved", docs.titles(c1), "One A i")
	})

	t.Run("without its children a page leaves them where it stood", func(t *testing.T) {
		want(t, docs.move(c1, map[string]any{"parentId": p3, "withChildren": false}), http.StatusOK, "move One A alone")
		sameTitles(t, "under One", docs.titles(p1), "One A i", "One B", "Two")
		sameTitles(t, "under Three", docs.titles(p3), "One A")
	})

	t.Run("a subtree moves into another space whole", func(t *testing.T) {
		moved := obj(t, want(t, docs.move(p1, map[string]any{"parentId": other.homeID}), http.StatusOK, "One to Other"), "page")
		if moved["spaceKey"] != "OTHER" {
			t.Fatalf("the page did not change space: %v", moved)
		}
		sameTitles(t, "Other's top", other.titles(""), "One")
		deep := obj(t, want(t, member.get(t, "/api/v1/pages/"+g1), http.StatusOK, "grandchild"), "page")
		if deep["spaceKey"] != "OTHER" {
			t.Fatalf("a page under the moved one stayed behind: %v", deep)
		}
		sameTitles(t, "Docs' top", docs.titles(""), "Zero", "One and a half", "Three")
	})

	t.Run("a copy with its children lands elsewhere and leaves the original", func(t *testing.T) {
		made := obj(t, want(t, member.post(t, "/api/v1/pages/"+p1+"/copy", map[string]any{"parentId": docs.homeID, "withChildren": true}), http.StatusCreated, "copy One back"), "page")
		if made["id"] == p1 || made["spaceKey"] != "DOCS" || made["title"] != "One" {
			t.Fatalf("the copy is %v", made)
		}
		sameTitles(t, "the copy's children", docs.titles(made["id"].(string)), "One A i", "One B", "Two")
		sameTitles(t, "the original's children", other.titles(p1), "One A i", "One B", "Two")
		alone := obj(t, want(t, member.post(t, "/api/v1/pages/"+p1+"/copy", map[string]any{"parentId": p1, "title": "One again"}), http.StatusCreated, "copy alone, under itself"), "page")
		if alone["title"] != "One again" {
			t.Fatalf("the copy did not take its new title: %v", alone)
		}
		if got := other.titles(alone["id"].(string)); len(got) != 0 {
			t.Fatalf("a copy without children has %v", got)
		}
	})

	t.Run("another organization reaches none of it", func(t *testing.T) {
		away := h.makeMember(t, "tree-away")
		stranger := api.as(t, away.user, away.org, h.slugOf(t, away.org))
		theirs := newTree(t, stranger, "DOCS", "Theirs")
		for what, got := range map[string]response{
			"list":         stranger.get(t, "/api/v1/spaces/DOCS/pages?parent="+docs.homeID),
			"move it":      stranger.post(t, "/api/v1/pages/"+p3+"/move", map[string]any{"parentId": theirs.homeID}),
			"move into it": stranger.post(t, "/api/v1/pages/"+theirs.add(theirs.homeID, "Mine")+"/move", map[string]any{"parentId": p3}),
			"copy it":      stranger.post(t, "/api/v1/pages/"+p3+"/copy", map[string]any{"parentId": theirs.homeID}),
			"add under it": stranger.post(t, "/api/v1/pages", map[string]any{"parentId": p3, "title": "Planted"}),
		} {
			if got.Status != http.StatusNotFound {
				t.Errorf("a stranger could %s: %d %s", what, got.Status, got.Raw)
			}
		}
	})
}

// Two moves that are each fine alone close a loop together; the database
// lets exactly one of them through.
func TestTwoCrossingMovesCannotCloseALoop(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "tree-race")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	docs := newTree(t, owner, "RACE", "Race")

	for round := range 5 {
		q := docs.add(docs.homeID, "Q")
		r := docs.add(docs.homeID, "R")
		codes := make([]int, 2)
		var wg sync.WaitGroup
		for i, pair := range [][2]string{{q, r}, {r, q}} {
			c := api.as(t, home.user, home.org, h.slugOf(t, home.org))
			c.eager = true
			wg.Add(1)
			go func() {
				defer wg.Done()
				codes[i] = c.post(t, "/api/v1/pages/"+pair[0]+"/move", map[string]any{"parentId": pair[1]}).Status
			}()
		}
		wg.Wait()
		ok := 0
		for _, code := range codes {
			if code == http.StatusOK {
				ok++
			} else if code != http.StatusConflict {
				t.Fatalf("round %d: a move answered %d", round, code)
			}
		}
		if ok != 1 {
			t.Fatalf("round %d: %d of the two crossing moves went through, want exactly one", round, ok)
		}
	}
}

// Straight through SQL as stator_app the tree still cannot loop, and a page
// cannot be hung into another organization's tree.
func TestTheTreeIsGuardedByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	a := h.makeMember(t, "tree-wall-a")
	b := h.makeMember(t, "tree-wall-b")
	docsA := newTree(t, api.as(t, a.user, a.org, h.slugOf(t, a.org)), "WALL", "Walled")
	docsB := newTree(t, api.as(t, b.user, b.org, h.slugOf(t, b.org)), "WALL", "Theirs")
	top := docsA.add(docsA.homeID, "Top")
	mid := docsA.add(top, "Mid")
	low := docsA.add(mid, "Low")
	theirs := docsB.add(docsB.homeID, "Theirs")

	conn := appConn(t)
	actAs(t, conn, a.org)
	refused(t, conn, "a page under its own grandchild", `UPDATE page SET parent_id = $2 WHERE id = $1`, top, low)
	refused(t, conn, "a page under itself", `UPDATE page SET parent_id = $1 WHERE id = $1`, top)
	refused(t, conn, "a new page that is its own parent", `INSERT INTO page (id, org_id, space_id, parent_id, rank, title) VALUES ($1, $2, (SELECT space_id FROM page WHERE id = $3), $1, 'W', 'Loop')`, uuid.NewString(), a.org, top)
	refused(t, conn, "a page under another organization's page", `UPDATE page SET parent_id = $2 WHERE id = $1`, mid, theirs)

	actAs(t, conn, b.org)
	untouched(t, conn, "moving A's page from B", `UPDATE page SET parent_id = $2 WHERE id = $1`, mid, theirs)

	var parent string
	if err := h.super.QueryRow(context.Background(), `SELECT parent_id::text FROM page WHERE id = $1`, mid).Scan(&parent); err != nil || parent != top {
		t.Errorf("Mid's parent is %s (%v), want Top", parent, err)
	}
}
