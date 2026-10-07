//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/events"
	"github.com/praetorianer777/stator/backend/internal/notify"
)

// Blog posts (#72): pages of a space's blog, outside its tree, dated by their
// first publish, listed by month, watched and told, and listed by a block,
// each reader seeing only the posts they may read.

func postPath(key string) string { return "/api/v1/spaces/" + key + "/posts" }

func blogPath(key string, rest ...string) string {
	return "/api/v1/spaces/" + key + "/blog" + strings.Join(rest, "")
}

func postsPath(params map[string]string) string {
	v := url.Values{}
	for k, val := range params {
		if val != "" {
			v.Set(k, val)
		}
	}
	return "/api/v1/posts?" + v.Encode()
}

// writePost makes a post, published at once unless more says otherwise.
func writePost(t *testing.T, c *client, key, title, text string, more ...map[string]any) map[string]any {
	t.Helper()
	body := map[string]any{"title": title, "body": textDoc(text), "publish": true}
	for _, m := range more {
		for k, v := range m {
			body[k] = v
		}
	}
	return obj(t, want(t, c.post(t, postPath(key), body), http.StatusCreated, "post "+title), "page")
}

func postTitles(t *testing.T, c *client, path string) string {
	t.Helper()
	var titles []string
	for _, each := range list(t, want(t, c.get(t, path), http.StatusOK, "list "+path), "posts") {
		titles = append(titles, each.(map[string]any)["title"].(string))
	}
	return strings.Join(titles, ",")
}

func blogOf(t *testing.T, c *client, key string) map[string]any {
	t.Helper()
	return obj(t, want(t, c.get(t, blogPath(key)), http.StatusOK, "read the blog of "+key), "blog")
}

func monthsOf(blog map[string]any) string {
	var out []string
	for _, each := range blog["months"].([]any) {
		m := each.(map[string]any)
		out = append(out, fmt.Sprintf("%d-%02d:%d", number(m["year"]), number(m["month"]), number(m["count"])))
	}
	return strings.Join(out, ",")
}

// backdatePost moves a post's date, as only a role other than the app's may.
func (h *harness) backdatePost(t *testing.T, id string, at time.Time) {
	t.Helper()
	tag, err := h.super.Exec(context.Background(), `UPDATE page SET posted_at = $2 WHERE id = $1 AND kind = 'post'`, id, at)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("backdate %s: %d rows, %v", id, tag.RowsAffected(), err)
	}
	h.settle(t)
}

func TestBlogPostsOverTheAPI(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "blog")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID := h.namedPerson(t, home.org, "Ann Writer")
	benID := h.addPerson(t, home.org, "member")
	ann, ben := api.as(t, annID, home.org, slug), api.as(t, benID, home.org, slug)

	news := newTree(t, owner, "BLOG", "News")
	other := newTree(t, owner, "BLOGX", "Elsewhere")
	now := time.Now().UTC()
	thisMonth := map[string]string{"space": "BLOG", "year": fmt.Sprint(now.Year()), "month": fmt.Sprint(int(now.Month()))}

	var launch string
	t.Run("a post starts as its writer's own, outside the tree, and is dated by its first publish", func(t *testing.T) {
		made := writePost(t, ann, "BLOG", "Launch", "We launch soon", map[string]any{"publish": false})
		launch = made["id"].(string)
		if made["kind"] != "post" || made["unpublished"] != true || made["parentId"] != nil || made["postedAt"] != nil || made["home"] != false {
			t.Fatalf("a new post reads %v", made)
		}
		if got := ben.get(t, pagePath(launch)); got.Status != http.StatusNotFound {
			t.Errorf("a reader opened an unpublished post: %d", got.Status)
		}
		if got := postTitles(t, ann, postsPath(map[string]string{"space": "BLOG"})); got != "" {
			t.Errorf("an unpublished post is listed: %s", got)
		}
		mine := blogOf(t, ann, "BLOG")["unpublished"].([]any)
		if len(mine) != 1 || mine[0].(map[string]any)["title"] != "Launch" {
			t.Errorf("ann's unpublished posts read %v", mine)
		}
		if theirs := blogOf(t, ben, "BLOG")["unpublished"].([]any); len(theirs) != 0 {
			t.Errorf("ben sees somebody else's unpublished posts: %v", theirs)
		}

		publishDraft(t, ann, launch, "Launch", "We launch today, with every team on board.", true)
		read := obj(t, want(t, ben.get(t, pagePath(launch)), http.StatusOK, "ben reads the post"), "page")
		posted, err := time.Parse(time.RFC3339Nano, fmt.Sprint(read["postedAt"]))
		if err != nil || time.Since(posted) > time.Hour || number(read["version"]) != 1 || len(read["ancestors"].([]any)) != 0 {
			t.Fatalf("the published post reads %v", read)
		}
		publishDraft(t, ann, launch, "Launch", "We launched.", false)
		again := obj(t, want(t, ben.get(t, pagePath(launch)), http.StatusOK, "ben reads it again"), "page")
		if again["postedAt"] != read["postedAt"] {
			t.Errorf("a second version moved the date from %v to %v", read["postedAt"], again["postedAt"])
		}

		for _, path := range []string{"/api/v1/spaces/BLOG/pages", "/api/v1/spaces/BLOG/outline"} {
			if strings.Contains(string(want(t, ann.get(t, path), http.StatusOK, path).Raw), launch) {
				t.Errorf("%s lists the post", path)
			}
		}
	})

	t.Run("a blog lists what each reader may read, newest first, a month at a time", func(t *testing.T) {
		writePost(t, ann, "BLOG", "Second", "Two")
		secret := writePost(t, ann, "BLOG", "Secret", "Only ann", map[string]any{"publish": false})["id"].(string)
		want(t, restrict(t, ann, secret, []any{user(annID)}, nil), http.StatusOK, "keep the secret to ann")
		publishDraft(t, ann, secret, "Secret", "Only ann", false)
		old := writePost(t, ann, "BLOG", "Old news", "From last year")["id"].(string)
		h.backdatePost(t, old, time.Date(2025, time.March, 10, 12, 0, 0, 0, time.UTC))
		writePost(t, owner, "BLOGX", "Elsewhere news", "Not in BLOG")

		if got := postTitles(t, ben, postsPath(map[string]string{"space": "BLOG"})); got != "Second,Launch,Old news" {
			t.Errorf("ben's blog: %s", got)
		}
		if got := postTitles(t, ann, postsPath(map[string]string{"space": "BLOG"})); got != "Secret,Second,Launch,Old news" {
			t.Errorf("ann's blog: %s", got)
		}
		if got := postTitles(t, ben, postsPath(thisMonth)); got != "Second,Launch" {
			t.Errorf("this month: %s", got)
		}
		if got := postTitles(t, ben, postsPath(map[string]string{"space": "BLOG", "year": "2025", "month": "3"})); got != "Old news" {
			t.Errorf("March 2025: %s", got)
		}
		if got := postTitles(t, ben, postsPath(map[string]string{"space": "BLOG", "year": "2025"})); got != "Old news" {
			t.Errorf("2025: %s", got)
		}
		wantMonths := fmt.Sprintf("%d-%02d:2,2025-03:1", now.Year(), int(now.Month()))
		if got := monthsOf(blogOf(t, ben, "BLOG")); got != wantMonths {
			t.Errorf("ben's months: %s, want %s", got, wantMonths)
		}
		if got := monthsOf(blogOf(t, ann, "BLOG")); got != fmt.Sprintf("%d-%02d:3,2025-03:1", now.Year(), int(now.Month())) {
			t.Errorf("ann's months: %s", got)
		}

		first := list(t, want(t, ben.get(t, postsPath(map[string]string{"space": "BLOG", "limit": "2"})), http.StatusOK, "the first two"), "posts")
		post := first[0].(map[string]any)
		if post["authorName"] != "Ann Writer" || post["spaceKey"] != "BLOG" || post["excerpt"] != "Two" {
			t.Errorf("a listed post reads %v", post)
		}
		next := want(t, ben.get(t, postsPath(map[string]string{"space": "BLOG", "limit": "2"})), http.StatusOK, "the first window").Body["next"]
		if next == nil {
			t.Fatal("two of three posts gave no cursor")
		}
		if got := postTitles(t, ben, postsPath(map[string]string{"space": "BLOG", "limit": "2", "cursor": next.(string)})); got != "Old news" {
			t.Errorf("the window after: %s", got)
		}
		if got := postTitles(t, ben, postsPath(map[string]string{"limit": "50"})); got != "Elsewhere news,Second,Launch,Old news" {
			t.Errorf("across the organization: %s", got)
		}
	})

	t.Run("lists that cannot be are refused, and a blog nobody has is not found", func(t *testing.T) {
		for what, path := range map[string]string{
			"a month without a year": postsPath(map[string]string{"month": "3"}),
			"a thirteenth month":     postsPath(map[string]string{"year": "2026", "month": "13"}),
			"a year in words":        postsPath(map[string]string{"year": "last"}),
			"no posts":               postsPath(map[string]string{"limit": "0"}),
			"too many":               postsPath(map[string]string{"limit": "51"}),
			"a made up cursor":       postsPath(map[string]string{"cursor": "nonsense"}),
		} {
			if got := ben.get(t, path); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
		for _, path := range []string{postsPath(map[string]string{"space": "NOPE"}), blogPath("NOPE")} {
			if got := ben.get(t, path); got.Status != http.StatusNotFound {
				t.Errorf("%s: %d %s", path, got.Status, got.Raw)
			}
		}
		if got := ann.post(t, postPath("BLOG"), map[string]any{"title": "  "}); got.Status != http.StatusUnprocessableEntity {
			t.Errorf("a post without a title: %d %s", got.Status, got.Raw)
		}
		if got := ann.post(t, postPath("NOPE"), map[string]any{"title": "Lost"}); got.Status != http.StatusNotFound {
			t.Errorf("a post in no space: %d %s", got.Status, got.Raw)
		}
	})

	t.Run("a post stays out of the tree, and nothing goes under it", func(t *testing.T) {
		for what, got := range map[string]response{
			"moved":        ann.post(t, pagePath(launch, "/move"), map[string]any{"parentId": news.homeID}),
			"copied":       ann.post(t, pagePath(launch, "/copy"), map[string]any{"parentId": news.homeID}),
			"given a page": ann.post(t, "/api/v1/pages", map[string]any{"parentId": launch, "title": "Under", "publish": true}),
			"made live":    ann.put(t, pagePath(launch, "/mode"), map[string]any{"mode": "live"}),
			"made a page":  ann.post(t, "/api/v1/pages", map[string]any{"parentId": news.homeID, "title": "Kind", "kind": "post"}),
		} {
			wantCode := "post"
			if what == "made a page" {
				if got.Status != http.StatusUnprocessableEntity {
					t.Errorf("a post %s: %d %s", what, got.Status, got.Raw)
				}
				continue
			}
			if got.Status != http.StatusConflict || errorCode(t, got) != wantCode {
				t.Errorf("a post %s: %d %s", what, got.Status, got.Raw)
			}
		}
	})

	t.Run("a post goes to the trash and back to its blog, and the archive takes it off the list", func(t *testing.T) {
		gone := writePost(t, ann, "BLOG", "Retracted", "Oops")["id"].(string)
		want(t, ann.delete(t, pagePath(gone)), http.StatusNoContent, "trash the post")
		if strings.Contains(postTitles(t, ben, postsPath(map[string]string{"space": "BLOG"})), "Retracted") {
			t.Error("a trashed post is listed")
		}
		var item map[string]any
		for _, each := range list(t, want(t, ann.get(t, "/api/v1/spaces/BLOG/trash"), http.StatusOK, "the trash"), "items") {
			if each.(map[string]any)["id"] == gone {
				item = each.(map[string]any)
			}
		}
		if item == nil || item["kind"] != "post" || item["parentInTree"] != true {
			t.Fatalf("the trash holds the post as %v", item)
		}
		back := obj(t, want(t, ann.post(t, "/api/v1/spaces/BLOG/trash/"+gone+"/restore", nil), http.StatusOK, "restore the post"), "page")
		if back["parentId"] != nil || back["kind"] != "post" {
			t.Errorf("the restored post reads %v", back)
		}
		if !strings.Contains(postTitles(t, ben, postsPath(map[string]string{"space": "BLOG"})), "Retracted") {
			t.Error("the restored post is not listed")
		}
		want(t, owner.put(t, pagePath(gone, "/archive"), nil), http.StatusOK, "archive the post")
		if strings.Contains(postTitles(t, ben, postsPath(map[string]string{"space": "BLOG"})), "Retracted") {
			t.Error("an archived post is listed")
		}
		want(t, ben.get(t, pagePath(gone)), http.StatusOK, "an archived post is still read")
	})

	t.Run("posting takes adding pages, and an archived space takes none", func(t *testing.T) {
		want(t, owner.put(t, "/api/v1/spaces/BLOGX/permissions", map[string]any{"grants": []any{
			map[string]any{"subject": everyone, "permissions": []any{"view"}},
			map[string]any{"subject": user(annID), "permissions": []any{"addPages"}},
		}}), http.StatusOK, "everybody reads BLOGX, ann writes")
		h.settle(t)
		if got := ben.post(t, postPath("BLOGX"), map[string]any{"title": "Not mine"}); got.Status != http.StatusForbidden {
			t.Errorf("a reader posted: %d %s", got.Status, got.Raw)
		}
		if blogOf(t, ben, "BLOGX")["canPost"] != false || blogOf(t, ann, "BLOGX")["canPost"] != true {
			t.Error("the blog says the wrong people may post")
		}
		writePost(t, ann, "BLOGX", "Allowed", "Ann may")
		want(t, owner.put(t, "/api/v1/spaces/BLOGX/archive", nil), http.StatusOK, "archive BLOGX")
		if got := ann.post(t, postPath("BLOGX"), map[string]any{"title": "Too late"}); got.Status != http.StatusConflict || errorCode(t, got) != "archived" {
			t.Errorf("a post in an archived space: %d %s", got.Status, got.Raw)
		}
		if got := postTitles(t, ben, postsPath(map[string]string{"limit": "50"})); strings.Contains(got, "Allowed") || strings.Contains(got, "Elsewhere news") {
			t.Errorf("the organization's posts take in an archived space: %s", got)
		}
		if got := postTitles(t, ben, postsPath(map[string]string{"space": "BLOGX"})); got != "Allowed,Elsewhere news" {
			t.Errorf("the archived space's own blog: %s", got)
		}
		_ = other
	})

	t.Run("a block keeps which blog and how many, never the posts", func(t *testing.T) {
		block := map[string]any{"type": "blogPosts", "attrs": map[string]any{"space": "BLOG", "limit": 5}}
		news.add(news.homeID, "Overview", map[string]any{"body": docOf(block)})
		block["attrs"].(map[string]any)["posts"] = []any{}
		if got := owner.post(t, "/api/v1/pages", map[string]any{"parentId": news.homeID, "title": "Stale", "body": docOf(block)}); got.Status != http.StatusUnprocessableEntity {
			t.Errorf("a block with its posts: %d %s", got.Status, got.Raw)
		}
	})
}

// Watching a blog hears of its new posts, a watch on the space hears of every
// version, and nobody hears of a post they may not read.
func TestBlogWatchersHearOfNewPosts(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "blog-watch")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID := h.namedPerson(t, home.org, "Ann Poster")
	benID, catID, danID := h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member")
	ann, ben, cat, dan := api.as(t, annID, home.org, slug), api.as(t, benID, home.org, slug), api.as(t, catID, home.org, slug), api.as(t, danID, home.org, slug)
	h.runWorker(t, notify.NewFanOut(h.cluster, nil, testAppURL, discard()))
	newTree(t, owner, "BW", "Watched")

	want(t, ben.put(t, blogPath("BW", "/watch"), nil), http.StatusNoContent, "ben watches the blog")
	want(t, ben.put(t, blogPath("BW", "/watch"), nil), http.StatusNoContent, "watching twice is no change")
	want(t, cat.put(t, "/api/v1/spaces/BW/watch", nil), http.StatusNoContent, "cat watches the space")
	if blogOf(t, ben, "BW")["watching"] != true || blogOf(t, cat, "BW")["watching"] != false {
		t.Error("the blog says the wrong people watch it")
	}
	if obj(t, want(t, ben.get(t, "/api/v1/spaces/BW"), http.StatusOK, "the space"), "space")["watching"] != false {
		t.Error("a blog watch reads as a watch on the whole space")
	}
	var kinds []string
	for _, w := range list(t, want(t, ben.get(t, "/api/v1/watches"), http.StatusOK, "ben's watches"), "watches") {
		kinds = append(kinds, w.(map[string]any)["kind"].(string))
	}
	if strings.Join(kinds, ",") != "blog" {
		t.Errorf("ben's watches: %v", kinds)
	}
	if got := ben.put(t, blogPath("NOPE", "/watch"), nil); got.Status != http.StatusNotFound {
		t.Errorf("watching no blog: %d", got.Status)
	}
	if got := ben.delete(t, blogPath("NOPE", "/watch")); got.Status != http.StatusNotFound {
		t.Errorf("unwatching no blog: %d", got.Status)
	}

	first := writePost(t, ann, "BW", "Hello", "First post")["id"].(string)
	publishDraft(t, ann, first, "Hello", "First post, revised", true)
	secret := writePost(t, ann, "BW", "Secret", "Not for you", map[string]any{"publish": false})["id"].(string)
	want(t, restrict(t, ann, secret, []any{user(annID)}, nil), http.StatusOK, "keep it to ann")
	publishDraft(t, ann, secret, "Secret", "Not for you", true)
	h.drained(t, home.org)

	heard := func(c *client) string {
		var out []string
		for _, n := range notificationsOf(t, c, "") {
			out = append(out, fmt.Sprintf("%s:%s", n["kind"], n["page"].(map[string]any)["title"]))
		}
		return strings.Join(out, ",")
	}
	if got := heard(ben); got != "posted:Hello" {
		t.Errorf("the blog's watcher heard %q", got)
	}
	if got := heard(cat); got != "published:Hello,posted:Hello" {
		t.Errorf("the space's watcher heard %q", got)
	}
	if got := heard(dan); got != "" {
		t.Errorf("somebody watching nothing heard %q", got)
	}

	want(t, ben.delete(t, blogPath("BW", "/watch")), http.StatusNoContent, "ben stops watching")
	if blogOf(t, ben, "BW")["watching"] != false {
		t.Error("the blog still says ben watches it")
	}
	writePost(t, ann, "BW", "Later", "Nobody listens")
	h.drained(t, home.org)
	if got := heard(ben); got != "posted:Hello" {
		t.Errorf("after he stopped, ben heard %q", got)
	}
	if n := h.countRows(t, `SELECT count(*) FROM outbox_event WHERE org_id = $1 AND topic = $2 AND payload->>'pageId' = $3 AND (payload->>'first')::boolean`,
		home.org, events.TopicPagePublished, first); n != 1 {
		t.Errorf("the first publish of a post was announced %d times", n)
	}
}

// A webhook says a published page is a post, so a receiver can pick posts out.
func TestAWebhookTellsAPostFromAPage(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "blog-hook")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	bin, address := hookBin(t)
	makeHook(t, owner, "Posts", address, events.TopicPagePublished)
	tr := newTree(t, owner, "BHOOK", "Hooked")

	post := writePost(t, owner, "BHOOK", "Announced", "Hear ye")["id"].(string)
	page := tr.add(tr.homeID, "Plain page")
	if got := awaitTopic(t, bin, events.TopicPagePublished, post, 1)[0].payload()["page"].(map[string]any); got["kind"] != "post" {
		t.Errorf("the post reached the receiver as %v", got)
	}
	if got := awaitTopic(t, bin, events.TopicPagePublished, page, 1)[0].payload()["page"].(map[string]any); got["kind"] != "page" {
		t.Errorf("the page reached the receiver as %v", got)
	}
}

// The service refusing is not proof: straight through SQL as stator_app, a
// post is written only by whoever may add pages to an open space, starts
// unpublished, never gets a parent or a page under it, keeps the date its
// first publish stamped, and a blog watch is one's own.
func TestBlogPostsAreHeldByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "blog-db")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID := h.addPerson(t, home.org, "member")
	readerID := h.addPerson(t, home.org, "member")
	ann := api.as(t, annID, home.org, slug)

	tr := newTree(t, owner, "BDB", "Guarded")
	newTree(t, owner, "BDBC", "Closed")
	want(t, owner.put(t, "/api/v1/spaces/BDB/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": everyone, "permissions": []any{"view"}},
		map[string]any{"subject": user(annID), "permissions": []any{"addPages"}},
	}}), http.StatusOK, "everybody reads BDB, ann writes")
	want(t, owner.put(t, "/api/v1/spaces/BDBC/permissions", map[string]any{"grants": []any{}}), http.StatusOK, "nobody else reads BDBC")
	post := writePost(t, ann, "BDB", "Guarded post", "Words")["id"].(string)
	h.settle(t)

	var spaceID, closedID uuid.UUID
	ctx := context.Background()
	if err := h.super.QueryRow(ctx, `SELECT id FROM space WHERE org_id = $1 AND key = 'BDB'`, home.org).Scan(&spaceID); err != nil {
		t.Fatal(err)
	}
	if err := h.super.QueryRow(ctx, `SELECT id FROM space WHERE org_id = $1 AND key = 'BDBC'`, home.org).Scan(&closedID); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO page (org_id, space_id, parent_id, rank, title, created_by, kind, version)
		VALUES (current_org_id(), $1, $2, 'a', 'Raw', current_actor_id(), $3, $4)`

	conn := appConn(t)
	actAs(t, conn, home.org, readerID)
	refused(t, conn, "a reader writing a post", insert, spaceID, nil, "post", 0)

	actAs(t, conn, home.org, annID)
	refused(t, conn, "a post born published", insert, spaceID, nil, "post", 1)
	refused(t, conn, "a post in the tree", insert, spaceID, tr.homeID, "post", 0)
	refused(t, conn, "a page under a post", insert, spaceID, post, "page", 0)
	refused(t, conn, "a home page beside the home page", insert, spaceID, nil, "page", 0)
	refused(t, conn, "a post in a space ann may not read", insert, closedID, nil, "post", 0)
	if _, err := conn.Exec(ctx, insert, spaceID, nil, "post", 0); err != nil {
		t.Errorf("ann could not write a post: %v", err)
	}
	refused(t, conn, "a post moved into the tree", `UPDATE page SET parent_id = $2 WHERE id = $1`, post, tr.homeID)
	refused(t, conn, "a post made a page", `UPDATE page SET kind = 'page' WHERE id = $1`, post)

	var before, after time.Time
	if err := conn.QueryRow(ctx, `SELECT posted_at FROM page WHERE id = $1`, post).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `UPDATE page SET posted_at = '2001-01-01' WHERE id = $1`, post); err != nil {
		t.Fatalf("the update itself failed: %v", err)
	}
	if err := conn.QueryRow(ctx, `SELECT posted_at FROM page WHERE id = $1`, post).Scan(&after); err != nil || !after.Equal(before) {
		t.Errorf("ann backdated her post from %s to %s (%v)", before, after, err)
	}

	refused(t, conn, "a blog watch for somebody else", `INSERT INTO watch (org_id, user_id, kind, space_id) VALUES (current_org_id(), $1, 'blog', $2)`, readerID, spaceID)
	refused(t, conn, "a blog watch on a closed space", `INSERT INTO watch (org_id, user_id, kind, space_id) VALUES (current_org_id(), current_actor_id(), 'blog', $1)`, closedID)
	refused(t, conn, "a blog watch on a page", `INSERT INTO watch (org_id, user_id, kind, page_id) VALUES (current_org_id(), current_actor_id(), 'blog', $1)`, post)
	if _, err := conn.Exec(ctx, `INSERT INTO watch (org_id, user_id, kind, space_id) VALUES (current_org_id(), current_actor_id(), 'blog', $1)`, spaceID); err != nil {
		t.Errorf("ann could not watch the blog: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO watch (org_id, user_id, kind, space_id) VALUES (current_org_id(), current_actor_id(), 'space', $1)`, spaceID); err != nil {
		t.Errorf("ann could not watch the space beside its blog: %v", err)
	}

	want(t, owner.put(t, "/api/v1/spaces/BDB/archive", nil), http.StatusOK, "archive BDB")
	h.settle(t)
	refused(t, conn, "a post in an archived space", insert, spaceID, nil, "post", 0)
}
