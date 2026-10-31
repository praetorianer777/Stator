//go:build integration

package test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// correctedQuery reads the words a full search ran instead of those typed,
// empty when it ran them as typed.
func correctedQuery(t *testing.T, r response) string {
	t.Helper()
	v, ok := r.Body["correctedQuery"]
	if !ok {
		t.Fatalf("the answer names no correctedQuery: %s", r.Raw)
	}
	if v == nil {
		return ""
	}
	return v.(string)
}

func quickTitles(t *testing.T, c *client, typed string) []string {
	t.Helper()
	return pageTitles(t, want(t, c.get(t, "/api/v1/search/quick?q="+url.QueryEscape(typed)), http.StatusOK, "quick "+typed))
}

// A word typed with a letter wrong finds what the right word finds, in the
// full search and the quick one, and the answer says which words it ran. A
// word only in a page the reader may not view is never offered to them.
func TestSearchCorrectsMistypedWords(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "typo")
	owner := api.as(t, home.user)
	annID := h.addPerson(t, "member")
	ann := api.as(t, annID)
	benID := h.addPerson(t, "member")
	ben := api.as(t, benID)

	ops := newTree(t, owner, "OPS", "Operations")
	upgrade := ops.add(ops.homeID, "Kubernetes upgrade", map[string]any{"body": textDoc("Drain every node before the upgrade.")})
	ops.add(ops.homeID, "Cluster notes", map[string]any{"body": textDoc("The Konfiguration of the kubernetes cluster.")})
	ops.add(ops.homeID, "Kubernetes rollout")
	want(t, owner.post(t, pagePath(upgrade, "/comments"), map[string]any{"body": commentDoc("Remember the zeppelin hangar.")}), http.StatusCreated, "a comment")
	want(t, owner.upload(t, pagePath(upgrade, "/attachments"), "flamingo-budget.txt", []byte("x")), http.StatusCreated, "a file")
	vault := newTree(t, owner, "VLT", "Vault")
	h.closeSpace(t, vault.space, annID)
	vault.add(vault.homeID, "Secret plans", map[string]any{"body": textDoc("Quarterly zanzibarian review.")})
	h.settle(t)

	t.Run("a mistyped word finds what the right one does, the title match first", func(t *testing.T) {
		for _, typed := range []string{"kubernets", "Kuberntes"} {
			r := searchFor(t, ben, url.Values{"q": {typed}})
			if got := correctedQuery(t, r); got != "kubernetes" {
				t.Errorf("%s ran %q, want kubernetes", typed, got)
			}
			got := hitTitles(t, r)
			if !sameSet(got, []string{"Kubernetes upgrade", "Kubernetes rollout", "Cluster notes"}) || got[2] != "Cluster notes" {
				t.Errorf("%s finds %v", typed, got)
			}
			if marked := matched(list(t, r, "hits")[0].(map[string]any)["title"]); len(marked) != 1 || marked[0] != "Kubernetes" {
				t.Errorf("%s marks %v in the title", typed, marked)
			}
		}
	})

	t.Run("the right word runs as typed and ranks the exact match first", func(t *testing.T) {
		r := searchFor(t, ben, url.Values{"q": {"kubernetes upgrade"}})
		if got := correctedQuery(t, r); got != "" {
			t.Errorf("the right words ran as %q", got)
		}
		if got := hitTitles(t, r); len(got) != 1 || got[0] != "Kubernetes upgrade" {
			t.Errorf("the right words find %v", got)
		}
		if got := hitTitles(t, searchFor(t, ben, url.Values{"q": {"kubernetes"}})); len(got) != 3 || got[2] != "Cluster notes" {
			t.Errorf("kubernetes finds %v", got)
		}
	})

	t.Run("only the words that find nothing are corrected, as written", func(t *testing.T) {
		r := searchFor(t, ben, url.Values{"q": {`Kubernets "upgrde"`}})
		if got := correctedQuery(t, r); got != `kubernetes "upgrade"` {
			t.Errorf("ran %q", got)
		}
		if got := hitTitles(t, r); len(got) != 1 || got[0] != "Kubernetes upgrade" {
			t.Errorf("finds %v", got)
		}
		r = searchFor(t, ben, url.Values{"q": {"Konfiguraton -rollout"}})
		if got := correctedQuery(t, r); got != "konfiguration -rollout" {
			t.Errorf("ran %q", got)
		}
		if got := hitTitles(t, r); len(got) != 1 || got[0] != "Cluster notes" {
			t.Errorf("finds %v", got)
		}
	})

	t.Run("comments and files are found by a mistyped word too", func(t *testing.T) {
		r := searchFor(t, ben, url.Values{"q": {"zepelin"}})
		if got := correctedQuery(t, r); got != "zeppelin" || number(r.Body["total"]) != 1 || list(t, r, "hits")[0].(map[string]any)["type"] != "comment" {
			t.Errorf("zepelin finds %s", r.Raw)
		}
		r = searchFor(t, ben, url.Values{"q": {"flamngo"}})
		if got := correctedQuery(t, r); got != "flamingo" || number(r.Body["total"]) != 1 || list(t, r, "hits")[0].(map[string]any)["type"] != "attachment" {
			t.Errorf("flamngo finds %s", r.Raw)
		}
	})

	t.Run("as typed, a mistyped word finds nothing", func(t *testing.T) {
		r := searchFor(t, ben, url.Values{"q": {"kubernets"}, "asTyped": {"true"}})
		if got := correctedQuery(t, r); got != "" || number(r.Body["total"]) != 0 {
			t.Errorf("as typed: %s", r.Raw)
		}
		if got := ben.get(t, "/api/v1/search?q=kubernets&asTyped=maybe"); got.Status != http.StatusUnprocessableEntity || errorCode(t, got) != "validation_failed" {
			t.Errorf("asTyped=maybe: %d %s", got.Status, got.Raw)
		}
	})

	t.Run("a word with nothing near still finds nothing", func(t *testing.T) {
		for _, typed := range []string{"xylophonequartz", "kubernets xylophonequartz", "qz"} {
			r := searchFor(t, ben, url.Values{"q": {typed}})
			if got := correctedQuery(t, r); got != "" || number(r.Body["total"]) != 0 {
				t.Errorf("%s: %s", typed, r.Raw)
			}
		}
		if got := quickTitles(t, ben, "xylophonequartz"); len(got) != 0 {
			t.Errorf("quick search finds %v", got)
		}
	})

	t.Run("quick search makes up what the prefix misses from the corrected words", func(t *testing.T) {
		if got := quickTitles(t, ben, "kuberntes"); !sameSet(got, []string{"Kubernetes upgrade", "Kubernetes rollout"}) {
			t.Errorf("kuberntes finds %v", got)
		}
		if got := quickTitles(t, ben, "kubernets up"); !sameSet(got, []string{"Kubernetes upgrade"}) {
			t.Errorf("kubernets up finds %v", got)
		}
		// The start of a word is no misspelling: kube asks for what starts with it.
		if got := quickTitles(t, ben, "kube"); !sameSet(got, []string{"Kubernetes upgrade", "Kubernetes rollout"}) {
			t.Errorf("kube finds %v", got)
		}
	})

	t.Run("a word only in a page the reader may not view is never offered", func(t *testing.T) {
		r := searchFor(t, ann, url.Values{"q": {"zanzibarain"}})
		if got := correctedQuery(t, r); got != "zanzibarian" || len(hitTitles(t, r)) != 1 {
			t.Errorf("ann, who may view the vault: %s", r.Raw)
		}
		if got := quickTitles(t, ann, "secrte"); !sameSet(got, []string{"Secret plans"}) {
			t.Errorf("ann's quick search finds %v", got)
		}
		r = searchFor(t, ben, url.Values{"q": {"zanzibarain"}})
		if got := correctedQuery(t, r); got != "" || number(r.Body["total"]) != 0 {
			t.Errorf("ben, whom the vault is closed to, is offered a word of it: %s", r.Raw)
		}
		if got := quickTitles(t, ben, "secrte"); len(got) != 0 {
			t.Errorf("ben's quick search finds %v", got)
		}
	})

	t.Run("straight through SQL the vocabulary stays closed", func(t *testing.T) {
		conn := appConn(t)
		ctx := context.Background()
		actAs(t, conn, benID)
		refused(t, conn, "reading the vocabulary", `SELECT word FROM search_word`)
		refused(t, conn, "adding to the vocabulary", `INSERT INTO search_word (word) VALUES ('planted')`)
		refused(t, conn, "asking for the nearest word", `SELECT * FROM search_word_nearest(ARRAY['zanzibarain'], 0.35, false)`)
		for _, c := range []struct {
			who   string
			id    uuid.UUID
			words string
		}{{"ben", benID, ""}, {"ann", annID, "zanzibarian"}} {
			actAs(t, conn, c.id)
			var words string
			err := conn.QueryRow(ctx, `SELECT corrected_words FROM search_corrected('zanzibarain', 0.35)`).Scan(&words)
			if errors.Is(err, pgx.ErrNoRows) {
				err = nil
			}
			if err != nil || words != c.words {
				t.Errorf("%s is offered %q (%v), want %q", c.who, words, err, c.words)
			}
		}
		actAs(t, conn, benID)
		var prefix *string
		if err := conn.QueryRow(ctx, `SELECT search_prefix_corrected(ARRAY['secrte'], 0.35)::text`).Scan(&prefix); err != nil || prefix != nil {
			t.Errorf("ben is offered %v (%v) for a title of the vault", prefix, err)
		}
	})

	t.Run("every write keeps the vocabulary, the migration filled it from what was there", func(t *testing.T) {
		ctx := context.Background()
		if _, err := h.super.Exec(ctx, `UPDATE page SET title = 'Kubernetes quokka upgrade' WHERE id = $1`, upgrade); err != nil {
			t.Fatal(err)
		}
		var learned, kept int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FILTER (WHERE word = 'quokka'), count(*) FILTER (WHERE word IN ('kubernetes', 'zeppelin', 'flamingo'))
			FROM search_word`).Scan(&learned, &kept); err != nil {
			t.Fatal(err)
		}
		if learned != 1 || kept != 3 {
			t.Errorf("the vocabulary holds quokka %d times and %d of the words written before", learned, kept)
		}
		var short int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM search_word WHERE NOT search_word_kept(word)`).Scan(&short); err != nil || short != 0 {
			t.Errorf("%d words that are never offered are held (%v)", short, err)
		}
	})
}
