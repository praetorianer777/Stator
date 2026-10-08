//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/audit"
)

// picture is a PNG of the size given, made here so the suite carries no binary files.
func picture(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// htmlPage is a page file of an HTML space export in the structure
// docs/wiki-import.md describes: breadcrumbs, a marked title, who wrote it,
// the content, labels and comments.
func htmlPage(title, crumbs, author, content, labels, comments string) string {
	return `<!DOCTYPE html><html><head><title>Team Wiki : ` + title + `</title></head><body>
<div id="breadcrumbs"><ul>` + crumbs + `</ul></div>
<h1><span id="title-text">Team Wiki : ` + title + `</span></h1>
<div class="page-metadata">Created by <span class="author">` + author + `</span> on <time datetime="2026-03-04">Mar 04, 2026</time></div>
<div id="main-content">` + content + `</div>
<div class="labels"><ul>` + labels + `</ul></div>
<div id="comments-section">` + comments + `</div></body></html>`
}

// htmlExport is an HTML space export of a nested tree, a picture, a label
// and a discussion by somebody of this organization and somebody not.
func htmlExport(t *testing.T, knownEmail string) []byte {
	t.Helper()
	return zipped(t, map[string][]byte{
		"team/index.html": []byte(`<html><head><title>Team Wiki</title></head><body><h2>Pages</h2>
<ul><li><a href="Home_1.html">Home</a><ul><li><a href="Guide_2.html">Guide</a><ul><li><a href="Setup_3.html">Setup</a></li></ul></li></ul></li></ul></body></html>`),
		"team/Home_1.html": []byte(htmlPage("Home", "", "Pat Unknown", `<p>Start with the <a href="Guide_2.html">guide</a>.</p>`, "", "")),
		"team/Guide_2.html": []byte(htmlPage("Guide", `<li><a href="Home_1.html">Home</a></li>`, "Pat Unknown",
			`<h2>Overview</h2><p><img src="attachments/2/pic.png" alt="The overview"></p>
<div class="callout callout-info"><p>Read this first.</p></div>
<table><tr><th>Step</th></tr><tr><td>One</td></tr></table>`,
			`<li><a href="labels/how-to.html">How To</a></li>`,
			`<div class="comment"><span class="author"><a href="mailto:`+knownEmail+`">Known Member</a></span><time datetime="2026-03-05T10:00:00Z">Mar 5</time>
  <div class="comment-body"><p>Clear and <strong>short</strong>.</p></div>
  <div class="comment"><span class="author">Pat Unknown</span><time datetime="2026-03-06T10:00:00Z">Mar 6</time><div class="comment-body"><p>Thanks.</p></div></div>
</div>`)),
		"team/Setup_3.html": []byte(htmlPage("Setup", `<li><a href="Home_1.html">Home</a></li><li><a href="Guide_2.html">Guide</a></li>`, "Pat Unknown",
			`<p>Install it. See <a href="Gone_9.html">the old page</a>.</p><iframe src="https://video.example.com/1"></iframe>`, "", "")),
		"team/attachments/2/pic.png": picture(t, 4, 3),
	})
}

func xmlText(name, value string) string {
	return fmt.Sprintf(`<property name=%q><![CDATA[%s]]></property>`, name, strings.ReplaceAll(value, "]]>", "]]]]><![CDATA[>"))
}

func xmlRef(name, class, id string) string {
	return fmt.Sprintf(`<property name=%q class=%q><id name="id">%s</id></property>`, name, class, id)
}

func xmlObject(class, id string, props ...string) string {
	return fmt.Sprintf(`<object class=%q><id name="id">%s</id>%s</object>`, class, id, strings.Join(props, ""))
}

// xmlExport is an XML space export with a page of two versions by two
// people, a file of two versions, a discussion, a label and a blog post.
func xmlExport(t *testing.T, knownEmail string) []byte {
	t.Helper()
	objects := []string{
		xmlObject("Space", "1", xmlText("key", "ENG"), xmlText("name", "Engineering"), xmlRef("homePage", "Page", "10")),
		xmlObject("User", "u1", xmlText("name", "known"), xmlText("email", knownEmail), xmlText("fullName", "Known Member")),
		xmlObject("User", "u2", xmlText("name", "old"), xmlText("fullName", "Old Timer")),
		xmlObject("Page", "10", xmlText("title", "Engineering"), xmlRef("space", "Space", "1"), xmlText("contentStatus", "current"),
			xmlRef("creator", "User", "u1"), xmlText("creationDate", "2026-01-01 09:00:00.000"), xmlRef("lastModifier", "User", "u1"), xmlText("lastModificationDate", "2026-01-01 09:00:00.000")),
		xmlObject("BodyContent", "100", xmlRef("content", "Page", "10"), xmlText("bodyType", "2"),
			xmlText("body", `<p>Read <ac:link><ri:page ri:content-title="Build" /></ac:link>.</p>`)),
		xmlObject("Page", "11", xmlText("title", "Build"), xmlRef("space", "Space", "1"), xmlRef("parent", "Page", "10"), xmlText("version", "2"),
			xmlText("contentStatus", "current"), xmlRef("creator", "User", "u2"), xmlText("creationDate", "2026-02-01 09:00:00.000"),
			xmlRef("lastModifier", "User", "u1"), xmlText("lastModificationDate", "2026-03-01 09:00:00.000"), xmlText("versionComment", "Added the diagram")),
		xmlObject("BodyContent", "110", xmlRef("content", "Page", "11"), xmlText("bodyType", "2"), xmlText("body",
			`<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">go</ac:parameter><ac:plain-text-body><![CDATA[go build ./...]]></ac:plain-text-body></ac:structured-macro>
<p><ac:image><ri:attachment ri:filename="diagram.png" /></ac:image></p>`)),
		xmlObject("Page", "12", xmlText("title", "Build"), xmlRef("space", "Space", "1"), xmlText("version", "1"), xmlRef("originalVersion", "Page", "11"),
			xmlText("contentStatus", "current"), xmlRef("creator", "User", "u2"), xmlText("creationDate", "2026-02-01 09:00:00.000"),
			xmlRef("lastModifier", "User", "u2"), xmlText("lastModificationDate", "2026-02-01 09:00:00.000")),
		xmlObject("BodyContent", "120", xmlRef("content", "Page", "12"), xmlText("bodyType", "2"), xmlText("body", "<p>First draft.</p>")),
		xmlObject("BlogPost", "13", xmlText("title", "We shipped"), xmlRef("space", "Space", "1"), xmlText("contentStatus", "current"),
			xmlRef("creator", "User", "u1"), xmlText("creationDate", "2026-04-01 09:00:00.000")),
		xmlObject("BodyContent", "130", xmlRef("content", "BlogPost", "13"), xmlText("bodyType", "2"), xmlText("body", "<p>Out now.</p>")),
		xmlObject("Attachment", "20", xmlText("title", "diagram.png"), xmlRef("containerContent", "Page", "11"), xmlText("version", "2"),
			xmlText("contentStatus", "current"), xmlText("contentType", "image/png"), xmlRef("creator", "User", "u1"), xmlText("creationDate", "2026-02-20 09:00:00.000")),
		xmlObject("Attachment", "21", xmlText("title", "diagram.png"), xmlRef("containerContent", "Page", "11"), xmlText("version", "1"),
			xmlRef("originalVersion", "Attachment", "20"), xmlText("contentStatus", "current"), xmlText("contentType", "image/png"), xmlRef("creator", "User", "u2"),
			xmlText("creationDate", "2026-02-10 09:00:00.000")),
		xmlObject("Comment", "40", xmlRef("containerContent", "Page", "11"), xmlText("contentStatus", "current"), xmlRef("creator", "User", "u2"),
			xmlText("creationDate", "2026-03-02 09:00:00.000")),
		xmlObject("BodyContent", "400", xmlRef("content", "Comment", "40"), xmlText("bodyType", "2"), xmlText("body", "<p>Does it build on Fridays?</p>")),
		xmlObject("Comment", "41", xmlRef("containerContent", "Page", "11"), xmlRef("parent", "Comment", "40"), xmlText("contentStatus", "current"),
			xmlRef("creator", "User", "u1"), xmlText("creationDate", "2026-03-03 09:00:00.000")),
		xmlObject("BodyContent", "410", xmlRef("content", "Comment", "41"), xmlText("bodyType", "2"), xmlText("body", "<p>Every day.</p>")),
		xmlObject("Label", "50", xmlText("name", "build"), xmlText("namespace", "global")),
		xmlObject("Labelling", "51", xmlRef("label", "Label", "50"), xmlRef("content", "Page", "11")),
	}
	return zipped(t, map[string][]byte{
		"entities.xml":        []byte(`<?xml version="1.0" encoding="UTF-8"?><export>` + strings.Join(objects, "\n") + `</export>`),
		"attachments/11/20/1": picture(t, 2, 2),
		"attachments/11/20/2": picture(t, 6, 5),
	})
}

func lossesOf(report map[string]any) []string {
	var out []string
	for _, each := range report["lost"].([]any) {
		l := each.(map[string]any)
		out = append(out, l["page"].(string)+"/"+l["kind"].(string)+"/"+l["detail"].(string))
	}
	return out
}

// An administrator imports an HTML space export of another wiki as a new
// space: its tree, content held to the allowlist, the picture as a file of
// its page, labels, comments by people found here or kept by name, and a
// report of what did not come across.
func TestAnHTMLExportOfAnotherWikiBecomesASpace(t *testing.T) {
	h := newHarness(t)
	api := transferAPI(t, h)
	home := h.makeMember(t, "wiki-html")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	knownID := h.addPerson(t, home.org, "member")
	known := api.as(t, knownID, home.org, slug)

	job := api.importSpace(t, owner, htmlExport(t, emailOf(t, h, knownID)), "key=wiki")
	if job["state"] != "done" || job["source"] != "html" || job["spaceKey"] != "WIKI" {
		t.Fatalf("the import ended as %v", job)
	}
	report := job["report"].(map[string]any)
	if number(report["pages"]) != 3 || number(report["versions"]) != 3 || number(report["files"]) != 1 || number(report["comments"]) != 2 {
		t.Errorf("the report counts %v", report)
	}
	lost := lossesOf(report)
	for _, want := range []string{"Setup/embed/iframe", "Setup/outsideLink/Gone_9.html"} {
		if !slices.Contains(lost, want) {
			t.Errorf("the report's losses %v lack %s", lost, want)
		}
	}
	people, _ := json.Marshal(report["people"])
	if !strings.Contains(string(people), "Pat Unknown") || strings.Contains(string(people), "Known Member") {
		t.Errorf("the people not found are %s", people)
	}

	space := obj(t, want(t, owner.get(t, "/api/v1/spaces/WIKI"), http.StatusOK, "the new space"), "space")
	if space["name"] != "Team Wiki" {
		t.Errorf("the new space is called %v", space["name"])
	}
	titles, ids := outline(t, owner, "WIKI")
	if !slices.Equal(titles, []string{"Home", "  Guide", "    Setup"}) {
		t.Errorf("the tree came across as %q", titles)
	}

	t.Run("links, pictures and blocks point at the new space", func(t *testing.T) {
		homePage := string(want(t, owner.get(t, pagePath(ids["Home"])), http.StatusOK, "the home page").Raw)
		if !strings.Contains(homePage, "/s/WIKI/p/"+ids["Guide"]) {
			t.Errorf("the home page does not link the guide: %s", homePage)
		}
		files := list(t, want(t, owner.get(t, pagePath(ids["Guide"], "/attachments")), http.StatusOK, "the guide's files"), "attachments")
		if len(files) != 1 {
			t.Fatalf("the guide has %d files", len(files))
		}
		f := files[0].(map[string]any)
		if f["fileName"] != "pic.png" || f["contentType"] != "image/png" || number(f["width"]) != 4 || number(f["height"]) != 3 {
			t.Errorf("the picture came across as %v", f)
		}
		if _, data := owner.download(t, "/api/v1/attachments/"+f["id"].(string)); !bytes.Equal(data, picture(t, 4, 3)) {
			t.Error("the picture's bytes changed")
		}
		guide := string(want(t, owner.get(t, pagePath(ids["Guide"])), http.StatusOK, "the guide").Raw)
		for _, want := range []string{`"attachmentId":"` + f["id"].(string) + `"`, `"kind":"info"`, `"tableHeader"`, `"text":"Overview"`} {
			if !strings.Contains(guide, want) {
				t.Errorf("the guide lacks %s: %s", want, guide)
			}
		}
	})

	t.Run("labels, comments and versions keep who wrote them", func(t *testing.T) {
		if got := labelsOf(t, want(t, owner.get(t, pagePath(ids["Guide"], "/labels")), http.StatusOK, "the labels")); !slices.Equal(got, []string{"how-to"}) {
			t.Errorf("the guide's labels are %v", got)
		}
		threads := list(t, want(t, owner.get(t, pagePath(ids["Guide"], "/comments")), http.StatusOK, "the discussion"), "threads")
		if len(threads) != 1 {
			t.Fatalf("the guide has %d threads", len(threads))
		}
		comments := threads[0].(map[string]any)["comments"].([]any)
		first, reply := comments[0].(map[string]any), comments[1].(map[string]any)
		if first["authorId"] != knownID.String() || first["originalAuthor"] != nil {
			t.Errorf("the known member's comment came across as %v", first)
		}
		if reply["authorId"] != home.user.String() || reply["originalAuthor"] != "Pat Unknown" {
			t.Errorf("the unknown person's comment came across as %v", reply)
		}
		versions := list(t, want(t, owner.get(t, pagePath(ids["Guide"], "/versions")), http.StatusOK, "the history"), "versions")
		if len(versions) != 1 || versions[0].(map[string]any)["originalAuthor"] != "Pat Unknown" {
			t.Errorf("the guide's history is %v", versions)
		}
	})

	t.Run("the space starts with the permissions of any new space", func(t *testing.T) {
		want(t, known.get(t, "/api/v1/spaces/WIKI"), http.StatusOK, "a member reads the space")
		spaceID, _ := uuid.Parse(space["id"].(string))
		if data := h.recordedOnce(t, home.org, audit.ActionSpaceImported, &home.user, spaceID); !strings.Contains(data, `"source": "html"`) || !strings.Contains(data, `"lost": 2`) {
			t.Errorf("the import's record reads %s", data)
		}
	})
}

// An XML export brings each page's versions with their authors, every
// version of its files, threads with replies, labels and the blog.
func TestAnXMLExportOfAnotherWikiKeepsItsHistory(t *testing.T) {
	h := newHarness(t)
	api := transferAPI(t, h)
	home := h.makeMember(t, "wiki-xml")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	knownID := h.addPerson(t, home.org, "member")

	job := api.importSpace(t, owner, xmlExport(t, emailOf(t, h, knownID)), "key=eng2&name=Engineering+wiki")
	if job["state"] != "done" || job["source"] != "xml" || job["spaceKey"] != "ENG2" {
		t.Fatalf("the import ended as %v", job)
	}
	titles, ids := outline(t, owner, "ENG2")
	if !slices.Equal(titles, []string{"Engineering", "  Build"}) {
		t.Errorf("the tree came across as %q", titles)
	}
	versions := list(t, want(t, owner.get(t, pagePath(ids["Build"], "/versions")), http.StatusOK, "the history"), "versions")
	if len(versions) != 2 {
		t.Fatalf("Build has %d versions", len(versions))
	}
	latest, oldest := versions[0].(map[string]any), versions[1].(map[string]any)
	if latest["authorId"] != knownID.String() || latest["comment"] != "Added the diagram" || oldest["originalAuthor"] != "Old Timer" {
		t.Errorf("the versions came across as %v and %v", latest, oldest)
	}
	first := string(want(t, owner.get(t, pagePath(ids["Build"], "/versions/1")), http.StatusOK, "version 1").Raw)
	if !strings.Contains(first, "First draft.") {
		t.Errorf("version 1 reads %s", first)
	}
	files := list(t, want(t, owner.get(t, pagePath(ids["Build"], "/attachments")), http.StatusOK, "the files"), "attachments")
	if len(files) != 2 {
		t.Fatalf("Build has %d files", len(files))
	}
	for _, each := range files {
		f := each.(map[string]any)
		size := map[int]int{1: 2, 2: 6}[number(f["version"])]
		if f["fileName"] != "diagram.png" || number(f["width"]) != size {
			t.Errorf("a version of the diagram came across as %v", f)
		}
	}
	page := string(want(t, owner.get(t, pagePath(ids["Build"])), http.StatusOK, "Build").Raw)
	if !strings.Contains(page, `"language":"go"`) || !strings.Contains(page, `"type":"image"`) {
		t.Errorf("Build reads %s", page)
	}
	threads := list(t, want(t, owner.get(t, pagePath(ids["Build"], "/comments")), http.StatusOK, "the discussion"), "threads")
	if len(threads) != 1 || len(threads[0].(map[string]any)["comments"].([]any)) != 2 {
		t.Errorf("the discussion came across as %v", threads)
	}
	if got := labelsOf(t, want(t, owner.get(t, pagePath(ids["Build"], "/labels")), http.StatusOK, "the labels")); !slices.Equal(got, []string{"build"}) {
		t.Errorf("Build's labels are %v", got)
	}
	posts := string(want(t, owner.get(t, "/api/v1/posts?space=ENG2"), http.StatusOK, "the blog").Raw)
	if !strings.Contains(posts, "We shipped") {
		t.Errorf("the blog reads %s", posts)
	}
	space := obj(t, want(t, owner.get(t, "/api/v1/spaces/ENG2"), http.StatusOK, "the space"), "space")
	if space["name"] != "Engineering wiki" {
		t.Errorf("the space is called %v", space["name"])
	}
}

// An export without a key, with a taken one, by somebody who may not create
// spaces, or that is no export at all, is refused at once in a sentence; one
// that holds nothing to import fails its job in one.
func TestExportsOfOtherWikisAreRefusedInSentences(t *testing.T) {
	h := newHarness(t)
	api := transferAPI(t, h)
	home := h.makeMember(t, "wiki-refusals")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	member := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)
	newTree(t, owner, "TAKEN", "Taken")
	export := htmlExport(t, "nobody@example.com")

	if msg := fieldError(t, want(t, sendArchive(t, owner, export, ""), http.StatusUnprocessableEntity, "an export without a key"), "key"); !strings.Contains(msg, "Choose a key") {
		t.Errorf("an export without a key is refused with %q", msg)
	}
	fieldError(t, want(t, sendArchive(t, owner, export, "key=TAKEN"), http.StatusUnprocessableEntity, "a taken key"), "key")
	if code := errorCode(t, want(t, sendArchive(t, member, export, "key=MINE"), http.StatusForbidden, "a member imports")); code != "forbidden" {
		t.Errorf("a member's import is refused as %s", code)
	}
	neither := zipped(t, map[string][]byte{"notes/readme.md": []byte("# hi")})
	if msg := fieldError(t, want(t, sendArchive(t, owner, neither, "key=NOPE"), http.StatusUnprocessableEntity, "a zip of neither"), "file"); !strings.Contains(msg, "HTML or XML") {
		t.Errorf("a zip of neither is refused with %q", msg)
	}

	empty := zipped(t, map[string][]byte{"index.html": []byte("<html><body><p>Nothing here.</p></body></html>")})
	job := api.importSpace(t, owner, empty, "key=EMPTY")
	if job["state"] != "failed" || job["failure"] != "invalid" || !strings.Contains(job["message"].(string), "no page") {
		t.Errorf("an export without pages ended as %v", job)
	}
	want(t, owner.get(t, "/api/v1/spaces/EMPTY"), http.StatusNotFound, "the space of a failed import")
}

// Straight through SQL as stator_app: an import of another wiki's export is
// queued only with a key, of a source the table knows, by whoever may create
// spaces, and the app role may not change what it was made of.
func TestTheDatabaseHoldsExportImportsToAKey(t *testing.T) {
	h := newHarness(t)
	home := h.makeMember(t, "wiki-sql")
	memberID := h.addPerson(t, home.org, "member")
	ctx := context.Background()
	conn := appConn(t)

	actAs(t, conn, home.org, home.user)
	refused(t, conn, "an export queued without a key",
		`INSERT INTO space_import (id, org_id, requested_by, size_bytes, source) VALUES ($1, $2, $3, 10, 'html')`, uuid.New(), home.org, home.user)
	refused(t, conn, "an import of a source nobody reads",
		`INSERT INTO space_import (id, org_id, requested_by, key, size_bytes, source) VALUES ($1, $2, $3, 'SRCX', 10, 'pdf')`, uuid.New(), home.org, home.user)
	queued := uuid.New()
	if _, err := conn.Exec(ctx, `INSERT INTO space_import (id, org_id, requested_by, key, size_bytes, source) VALUES ($1, $2, $3, 'SRCX', 10, 'xml')`,
		queued, home.org, home.user); err != nil {
		t.Fatalf("an administrator cannot queue an export's import through SQL: %v", err)
	}
	h.cleanupExec(t, h.super, `DELETE FROM space_import WHERE id = $1`, queued)
	refused(t, conn, "an administrator changing what an import is made of", `UPDATE space_import SET source = 'archive' WHERE id = $1`, queued)

	actAs(t, conn, home.org, memberID)
	refused(t, conn, "a member importing an export",
		`INSERT INTO space_import (id, org_id, requested_by, key, size_bytes, source) VALUES ($1, $2, $3, 'SRCY', 10, 'html')`, uuid.New(), home.org, memberID)
}
