//go:build integration

package test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/example"
)

// Renewing the example space (#320): an example made from an older version
// of the content is brought to the current one by the worker, as the
// administrator who asked; what people changed or added stays theirs, and
// only the worker says which version the example has.

// renewExample asks to renew the example and waits until the job ends.
func (a *apiServer) renewExample(t *testing.T, c *client, body map[string]any) map[string]any {
	t.Helper()
	queued := want(t, c.post(t, "/api/v1/example-space/renew", body), http.StatusAccepted, "renew the example")
	if job := obj(t, queued, "job"); job["kind"] != string(example.KindRenew) {
		t.Fatalf("renewing queued %s", queued.Raw)
	}
	done := a.exampleJob(t, c)
	job := obj(t, done, "job")
	if job["state"] != "done" || job["kind"] != string(example.KindRenew) {
		t.Fatalf("the example was not renewed: %s", done.Raw)
	}
	return job
}

// titles are the titles of a report's pages under one heading.
func titles(t *testing.T, report map[string]any, heading string) []string {
	t.Helper()
	var out []string
	for _, p := range report[heading].([]any) {
		out = append(out, p.(map[string]any)["title"].(string))
	}
	return out
}

func TestAnOlderExampleIsRenewedAndWhatPeopleDidStays(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "example-renew")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	memberID := h.addPerson(t, home.org, "member")
	member := api.as(t, memberID, home.org, slug)
	ctx := context.Background()

	spaceID := api.makeExample(t, owner, map[string]any{"language": "en"})["id"].(string)
	current := float64(example.Current())
	edition := obj(t, want(t, owner.get(t, "/api/v1/example-space"), http.StatusOK, "look at the example"), "edition")
	if edition["version"] != current || edition["current"] != current || len(edition["new"].([]any)) != 0 || edition["language"] != "en" {
		t.Fatalf("a new example's edition is %v", edition)
	}

	pageOf := func(lang, name string) string {
		t.Helper()
		title, err := example.Title(lang, name)
		if err != nil {
			t.Fatal(err)
		}
		var id string
		if err := h.super.QueryRow(ctx, `SELECT id FROM page WHERE space_id = $1 AND title = $2`, spaceID, title).Scan(&id); err != nil {
			t.Fatalf("no page titled %q: %v", title, err)
		}
		return id
	}
	showcase, search, labels, writing := pageOf("en", example.Showcase), pageOf("en", "search"), pageOf("en", "labels"), pageOf("en", "writing")
	files := h.countRows(t, `SELECT count(*) FROM attachment WHERE page_id = $1`, showcase)

	// As an example made before the content had versions, and before its
	// newest guide: what it made is known only by its titles.
	for _, sql := range []string{
		`DELETE FROM example_page WHERE space_id = $1`,
		`DELETE FROM page WHERE space_id = $1 AND title = 'Moving pages in and out'`,
		`UPDATE space SET example_version = 0 WHERE id = $1`,
	} {
		if _, err := h.super.Exec(ctx, sql, spaceID); err != nil {
			t.Fatal(err)
		}
	}
	h.settle(t)

	// What people did since: a guide renamed, a comment, a page of their own.
	want(t, owner.patch(t, "/api/v1/pages/"+labels, map[string]any{"title": "Our labels", "version": 1}), http.StatusOK, "rename a guide")
	want(t, owner.post(t, "/api/v1/pages/"+search+"/comments", map[string]any{"body": textDoc("Where do I search Armature?")}), http.StatusCreated, "comment on a guide")
	own := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": writing, "title": "Our writing rules", "publish": true}), http.StatusCreated, "add a page of one's own"), "page")["id"]

	older := obj(t, want(t, owner.get(t, "/api/v1/example-space?language=de"), http.StatusOK, "look at the older example"), "edition")
	if older["version"] != float64(0) || !slices.Equal(strs(older["new"]), example.NewSince(0, example.German)) {
		t.Fatalf("the older example's edition is %v", older)
	}
	if got := member.post(t, "/api/v1/example-space/renew", map[string]any{}); got.Status != http.StatusForbidden {
		t.Errorf("a member renews the example: %d %s", got.Status, got.Raw)
	}

	report := api.renewExample(t, owner, map[string]any{})["report"].(map[string]any)
	if got := titles(t, report, "added"); !slices.Equal(got, []string{"Moving pages in and out"}) {
		t.Errorf("the renewal added %v", got)
	}
	if got := titles(t, report, "kept"); !slices.Equal(got, []string{"Our labels"}) {
		t.Errorf("the renewal kept %v", got)
	}
	if n := len(report["remade"].([]any)) + len(report["retired"].([]any)); n != 0 {
		t.Errorf("the renewal remade or retired pages: %v", report)
	}

	t.Run("the guides nobody changed are published again, the changed one is left", func(t *testing.T) {
		for id, wantVersion := range map[string]int{search: 2, showcase: 2, labels: 2} {
			if n := h.countRows(t, `SELECT version FROM page WHERE id = $1`, id); n != wantVersion {
				t.Errorf("page %s is at version %d, want %d", id, n, wantVersion)
			}
		}
		if n := h.countRows(t, `SELECT count(*) FROM page WHERE id = $1 AND title = 'Our labels'`, labels); n != 1 {
			t.Errorf("the renamed guide was renamed back")
		}
		if n := h.countRows(t, `SELECT count(*) FROM page WHERE space_id = $1 AND title = 'Every block a page can hold'`, spaceID); n != 1 {
			t.Errorf("the showcase is there %d times", n)
		}
		if n := h.countRows(t, `SELECT count(*) FROM attachment WHERE page_id = $1`, showcase); n != files {
			t.Errorf("the showcase holds %d files, had %d", n, files)
		}
	})

	t.Run("what people added stays", func(t *testing.T) {
		if n := h.countRows(t, `SELECT count(*) FROM comment_thread WHERE page_id = $1`, search); n != 1 {
			t.Errorf("the guide's comment is gone")
		}
		if n := h.countRows(t, `SELECT count(*) FROM page WHERE id = $1 AND parent_id = $2 AND trashed_at IS NULL`, own, writing); n != 1 {
			t.Errorf("the page of one's own is gone")
		}
	})

	t.Run("the new guide is made and published where it belongs", func(t *testing.T) {
		if n := h.countRows(t, `SELECT count(*) FROM page p JOIN page_label l ON l.page_id = p.id AND l.name = 'guide'
			WHERE p.space_id = $1 AND p.title = 'Moving pages in and out' AND p.parent_id = $2 AND p.version = 1`, spaceID, writing); n != 1 {
			t.Errorf("the new guide is not published below Writing with its label")
		}
	})

	t.Run("the example has the current version, known page by page, and it is audited", func(t *testing.T) {
		after := obj(t, want(t, owner.get(t, "/api/v1/example-space"), http.StatusOK, "look at the renewed example"), "edition")
		if after["version"] != current || len(after["new"].([]any)) != 0 {
			t.Errorf("the renewed example's edition is %v", after)
		}
		names := 1 + len(example.Posts)
		example.Walk(func(example.Entry) { names++ })
		if n := h.countRows(t, `SELECT count(*) FROM example_page WHERE space_id = $1 AND page_id IS NOT NULL`, spaceID); n != names {
			t.Errorf("the example knows %d pages, want %d", n, names)
		}
		if data := h.recordedOnce(t, home.org, audit.ActionExampleSpaceRenewed, &home.user, spaceID); !strings.Contains(data, `"from": 0`) {
			t.Errorf("the record reads %s", data)
		}
	})

	t.Run("renewed in German, it speaks German and still leaves the changed guide", func(t *testing.T) {
		report := api.renewExample(t, owner, map[string]any{"language": "de"})["report"].(map[string]any)
		if got := titles(t, report, "kept"); !slices.Equal(got, []string{"Our labels"}) {
			t.Errorf("the renewal kept %v", got)
		}
		if len(report["added"].([]any)) != 0 {
			t.Errorf("a second renewal added %v", report["added"])
		}
		sp := obj(t, want(t, owner.get(t, "/api/v1/spaces/"+example.Key), http.StatusOK, "read the example"), "space")
		if sp["name"] != "Stator kennenlernen" {
			t.Errorf("the example is called %v", sp["name"])
		}
		pageOf("de", example.Showcase)
		if got := obj(t, want(t, owner.get(t, "/api/v1/example-space"), http.StatusOK, "look again"), "edition")["language"]; got != "de" {
			t.Errorf("the example's pages are in %v", got)
		}
	})

	t.Run("a renewal whose example was deleted says so", func(t *testing.T) {
		want(t, owner.delete(t, "/api/v1/spaces/"+example.Key), http.StatusNoContent, "delete the example")
		if got := owner.post(t, "/api/v1/example-space/renew", map[string]any{}); got.Status != http.StatusConflict {
			t.Errorf("renewing no example answers %d %s", got.Status, got.Raw)
		}
		// As a job queued before the space went, which the deletion leaves without it.
		if _, err := h.super.Exec(ctx, `INSERT INTO example_job (id, org_id, requested_by, language, kind) VALUES ($1, $2, $3, 'en', 'renew')`,
			uuid.New(), home.org, home.user); err != nil {
			t.Fatal(err)
		}
		job := obj(t, api.exampleJob(t, owner), "job")
		if job["state"] != "failed" || job["failure"] != string(example.FailureGone) || job["message"] != example.FailureGone.Message(example.KindRenew) {
			t.Errorf("the renewal of a deleted example ended %v", job)
		}
	})
}

func strs(v any) []string {
	var out []string
	for _, s := range v.([]any) {
		out = append(out, s.(string))
	}
	return out
}

// Straight through SQL as stator_app: only an administrator queues a renewal,
// only of the organization's example; only the worker says which version the
// example has, and only it reads which pages the example made.
func TestTheDatabaseKeepsTheRenewalTheWorkers(t *testing.T) {
	h := newHarness(t)
	home := h.makeMember(t, "example-renew-sql")
	memberID := h.addPerson(t, home.org, "member")
	ctx := context.Background()
	conn := appConn(t)
	actAs(t, conn, home.org, home.user)
	exampleID, plainID := uuid.New(), uuid.New()
	if _, err := conn.Exec(ctx, `INSERT INTO space (id, org_id, key, name, created_by, example, example_version, example_language)
		VALUES ($1, $2, 'EXAMPLE', 'Example', $3, true, 1, 'en')`, exampleID, home.org, home.user); err != nil {
		t.Fatalf("an administrator cannot make the example through SQL: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO space (id, org_id, key, name, created_by) VALUES ($1, $2, 'PLAIN', 'Plain', $3)`, plainID, home.org, home.user); err != nil {
		t.Fatal(err)
	}

	refused(t, conn, "an administrator claiming a newer version", `UPDATE space SET example_version = 99 WHERE id = $1`, exampleID)
	refused(t, conn, "an administrator changing the example's language", `UPDATE space SET example_language = 'de' WHERE id = $1`, exampleID)
	refused(t, conn, "an administrator reading the pages the example made", `SELECT count(*) FROM example_page`)
	renew := `INSERT INTO example_job (id, org_id, requested_by, language, kind, space_id) VALUES ($1, $2, $3, 'en', $4, $5)`
	refused(t, conn, "renewing an ordinary space", renew, uuid.New(), home.org, home.user, "renew", plainID)
	refused(t, conn, "renewing nothing", renew, uuid.New(), home.org, home.user, "renew", nil)
	refused(t, conn, "making the example on a space there is", renew, uuid.New(), home.org, home.user, "make", exampleID)
	actAs(t, conn, home.org, memberID)
	refused(t, conn, "a member renewing the example", renew, uuid.New(), home.org, memberID, "renew", exampleID)
	actAs(t, conn, home.org, home.user)
	if _, err := conn.Exec(ctx, renew, uuid.New(), home.org, home.user, "renew", exampleID); err != nil {
		t.Fatalf("an administrator cannot queue a renewal through SQL: %v", err)
	}
}
