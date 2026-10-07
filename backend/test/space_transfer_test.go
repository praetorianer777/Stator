//go:build integration

package test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"maps"
	"mime/multipart"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/spaceio"
)

// transferWait bounds one export or import in the suite.
const transferWait = 2 * time.Minute

// transferAPI keeps the files on pages in the app's bucket too, so a space
// export the stack's worker runs finds them as the suite's own watch does.
func transferAPI(t *testing.T, h *harness, tweaks ...func(*httpapi.Server)) *apiServer {
	t.Helper()
	store := h.appStore(t)
	return newAPIServer(t, h, append([]func(*httpapi.Server){func(s *httpapi.Server) {
		s.Attachments = attachment.NewService(h.cluster, store, s.Pages).WithMaxSize(testUploadLimit).WithLogger(discard())
	}}, tweaks...)...)
}

// followTransfer runs the worker's watch until the job read reports itself
// finished, and answers it.
func (a *apiServer) followTransfer(t *testing.T, read func() map[string]any) map[string]any {
	t.Helper()
	watch := spaceio.NewWatch(a.h.cluster, a.h.appStore(t), discard(), time.Hour, time.Hour)
	deadline := time.Now().Add(transferWait)
	for {
		if _, err := watch.Once(context.Background()); err != nil {
			t.Logf("the suite's watch: %v", err)
		}
		job := read()
		if job["state"] != "queued" && job["state"] != "running" {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("the job did not finish within %s: %v", transferWait, job)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// exportSpace exports a space and downloads the file.
func (a *apiServer) exportSpace(t *testing.T, c *client, key, format string) (map[string]any, []byte) {
	t.Helper()
	queued := obj(t, want(t, c.post(t, "/api/v1/spaces/"+key+"/exports", map[string]any{"format": format}), http.StatusAccepted, "export "+key), "export")
	id := queued["id"].(string)
	done := a.followTransfer(t, func() map[string]any {
		for _, each := range list(t, want(t, c.get(t, "/api/v1/spaces/"+key+"/exports"), http.StatusOK, "follow the export"), "exports") {
			if job := each.(map[string]any); job["id"] == id {
				return job
			}
		}
		t.Fatalf("the export %s is not listed", id)
		return nil
	})
	if done["state"] != "done" {
		t.Fatalf("the export of %s did not finish: %v", key, done)
	}
	resp, data := c.download(t, "/api/v1/space-exports/"+id+"/file")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/zip" {
		t.Fatalf("the export downloads as %d %s: %s", resp.StatusCode, resp.Header.Get("Content-Type"), data)
	}
	return done, data
}

// sendArchive uploads an archive to import, with the query given.
func sendArchive(t *testing.T, c *client, data []byte, query string) response {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "space.zip")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	_ = form.Close()
	path := "/api/v1/space-imports"
	if query != "" {
		path += "?" + query
	}
	return c.send(t, http.MethodPost, path, form.FormDataContentType(), &body)
}

// importSpace imports an archive and follows the job to its end.
func (a *apiServer) importSpace(t *testing.T, c *client, data []byte, query string) map[string]any {
	t.Helper()
	queued := obj(t, want(t, sendArchive(t, c, data, query), http.StatusAccepted, "import the archive"), "import")
	id := queued["id"].(string)
	return a.followTransfer(t, func() map[string]any {
		return obj(t, want(t, c.get(t, "/api/v1/space-imports/"+id), http.StatusOK, "follow the import"), "import")
	})
}

func archiveFiles(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("the export is no zip: %v", err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name], _ = io.ReadAll(rc)
		_ = rc.Close()
	}
	return out
}

func zipped(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(data)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// smallArchive is an archive of a space of one page, made by hand as an
// archive from elsewhere would be.
func smallArchive(t *testing.T, key string) []byte {
	t.Helper()
	home := uuid.New()
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	body, _ := json.Marshal(textDoc("Made elsewhere"))
	page := spaceio.ArchivePage{
		ID: home, Kind: "page", Rank: "m", Title: "Small", Mode: "draft", Width: "fixed", CreatedAt: at, UpdatedAt: at, PublishedAt: &at, Body: body,
		Versions: []spaceio.PageVersion{{Number: 1, Title: "Small", Body: body, CreatedAt: at, UpdatedAt: at}},
		Labels:   []spaceio.Label{}, Restrictions: []spaceio.Restriction{}, Files: []spaceio.File{}, Threads: []spaceio.Thread{}, Reactions: []spaceio.Reaction{},
	}
	m := spaceio.Manifest{
		Format: spaceio.Format, Version: spaceio.Version, ExportedAt: at,
		Space:  spaceio.ArchiveSpace{Key: key, Name: "Small", CreatedAt: at, HomePage: home},
		People: []spaceio.Person{}, Groups: []spaceio.Group{}, Grants: []spaceio.Grant{}, Pages: []uuid.UUID{home},
		Templates: []spaceio.Template{}, Calendars: []spaceio.Calendar{}, Counts: spaceio.Counts{Pages: 1, Versions: 1},
	}
	manifest, _ := json.Marshal(m)
	entry, _ := json.Marshal(page)
	return zipped(t, map[string][]byte{"manifest.json": manifest, "pages/" + home.String() + ".json": entry})
}

// outline is a space's pages in reading order, as titles with their depth,
// and their ids by title.
func outline(t *testing.T, c *client, key string) ([]string, map[string]string) {
	t.Helper()
	var titles []string
	ids := map[string]string{}
	for _, each := range list(t, want(t, c.get(t, "/api/v1/spaces/"+key+"/outline"), http.StatusOK, "the outline of "+key), "pages") {
		p := each.(map[string]any)
		titles = append(titles, strings.Repeat("  ", number(p["depth"]))+p["title"].(string))
		ids[p["title"].(string)] = p["id"].(string)
	}
	return titles, ids
}

func emailOf(t *testing.T, h *harness, id uuid.UUID) string {
	t.Helper()
	var email string
	if err := h.super.QueryRow(context.Background(), `SELECT email::text FROM app_user WHERE id = $1`, id).Scan(&email); err != nil {
		t.Fatal(err)
	}
	return email
}

// A space goes out of one organization as an archive and comes into another
// whole: its tree, every version with its author, files with their versions,
// labels, discussions, reactions, the blog, calendars, templates and who may
// do what, with the people and groups found again by address and name.
func TestASpaceTravelsToAnotherOrganizationWhole(t *testing.T) {
	h := newHarness(t)
	api := transferAPI(t, h)
	from := h.makeMember(t, "travel-from")
	fromSlug := h.slugOf(t, from.org)
	owner := api.as(t, from.user, from.org, fromSlug)
	annID := h.namedPerson(t, from.org, "Ann Traveller")
	ann := api.as(t, annID, from.org, fromSlug)
	bob := api.as(t, h.addPerson(t, from.org, "member"), from.org, fromSlug)
	writers := h.makeGroup(t, from.org, "Writers", annID)

	trip := newTree(t, owner, "TRIP", "Trip")
	top := trip.add(trip.homeID, "Top")
	nested := trip.add(top, "Nested")
	first := obj(t, want(t, owner.upload(t, "/api/v1/pages/"+nested+"/attachments", "plan.txt", []byte("first plan")), http.StatusCreated, "upload the plan"), "attachment")
	second := obj(t, want(t, owner.upload(t, "/api/v1/pages/"+nested+"/attachments", "plan.txt", []byte("second plan")), http.StatusCreated, "upload it again"), "attachment")
	publishBody(t, owner, nested, map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "paragraph", "content": []any{
			map[string]any{"type": "text", "text": "Ask "},
			map[string]any{"type": "mention", "attrs": map[string]any{"id": annID.String(), "label": "Ann Traveller"}},
			map[string]any{"type": "text", "text": " or "},
			map[string]any{"type": "mention", "attrs": map[string]any{"id": from.user.String(), "label": "The Organizer"}},
			map[string]any{"type": "text", "text": ", read "},
			map[string]any{"type": "text", "text": "the top", "marks": []any{map[string]any{"type": "link", "attrs": map[string]any{"href": "/s/TRIP/p/" + top + "/top"}}}},
			map[string]any{"type": "text", "text": " and "},
			map[string]any{"type": "attachment", "attrs": map[string]any{"attachmentId": second["id"], "fileName": "plan.txt"}},
		}},
	}})
	publishDraft(t, ann, nested, "Nested", "Ann's words", false, "Ann's turn")
	box := obj(t, want(t, owner.post(t, "/api/v1/pages", map[string]any{"parentId": trip.homeID, "title": "Box", "kind": "folder"}), http.StatusCreated, "make a folder"), "page")["id"].(string)
	trip.add(box, "In the box")
	want(t, addLabel(t, owner, nested, "trip"), http.StatusOK, "label the page")
	want(t, owner.put(t, "/api/v1/spaces/TRIP/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": everyone, "permissions": []any{"view", "addComments"}},
		map[string]any{"subject": group(writers), "permissions": []any{"addPages"}},
		map[string]any{"subject": user(annID), "permissions": []any{"administer"}},
		map[string]any{"subject": user(from.user), "permissions": []any{"administer"}},
	}}), http.StatusOK, "TRIP's grants")
	want(t, restrict(t, owner, top, []any{group(writers), user(annID)}, []any{user(annID)}), http.StatusOK, "restrict Top")
	startThread(t, ann, nested, "Looks good")
	want(t, react(t, ann, "pages", nested, "👍"), http.StatusOK, "ann reacts")
	want(t, react(t, owner, "pages", nested, "🎉"), http.StatusOK, "the owner reacts")
	want(t, owner.post(t, "/api/v1/spaces/TRIP/posts", map[string]any{"title": "Departure", "body": textDoc("We leave"), "publish": true}), http.StatusCreated, "post")
	calendarID := obj(t, want(t, owner.post(t, calendarsPath("TRIP"), map[string]any{"name": "Trips"}), http.StatusCreated, "add a calendar"), "calendar")["id"].(string)
	want(t, owner.post(t, calendarPath(calendarID, "/events"), allDay("Leave", "event", "2026-11-02", "2026-11-03")), http.StatusCreated, "add an event")
	want(t, owner.post(t, "/api/v1/templates", map[string]any{"spaceKey": "TRIP", "name": "Trip report", "body": textDoc("Where we went"), "variables": []any{}}), http.StatusCreated, "add a template")

	var (
		archive []byte
		counts  spaceio.Counts
	)
	t.Run("an administrator exports the space as an archive", func(t *testing.T) {
		job, data := api.exportSpace(t, owner, "TRIP", "archive")
		if job["fileName"] != "trip-space.zip" || number(job["progress"].(map[string]any)["done"]) == 0 {
			t.Errorf("the finished export reads %v", job)
		}
		archive = data
		var m spaceio.Manifest
		defer func() { counts = m.Counts }()
		files := archiveFiles(t, data)
		if err := json.Unmarshal(files["manifest.json"], &m); err != nil {
			t.Fatalf("the manifest: %v", err)
		}
		if m.Format != spaceio.Format || m.Version != spaceio.Version || m.Space.Key != "TRIP" || len(m.Pages) != 6 || m.Counts.Files != 2 {
			t.Errorf("the manifest reads %+v", m)
		}
		var p spaceio.ArchivePage
		if err := json.Unmarshal(files["pages/"+nested+".json"], &p); err != nil {
			t.Fatal(err)
		}
		if len(p.Versions) != 3 || len(p.Files) != 2 || len(p.Labels) != 1 || len(p.Threads) != 1 || len(p.Reactions) != 2 {
			t.Errorf("Nested is archived as %d versions, %d files, %d labels, %d threads, %d reactions", len(p.Versions), len(p.Files), len(p.Labels), len(p.Threads), len(p.Reactions))
		}
		if string(files["files/"+first["id"].(string)]) != "first plan" || string(files["files/"+second["id"].(string)]) != "second plan" {
			t.Error("the archive does not hold both versions of the plan")
		}
	})

	t.Run("only administrators of the space export it, and read its exports", func(t *testing.T) {
		if code := errorCode(t, want(t, bob.post(t, "/api/v1/spaces/TRIP/exports", map[string]any{"format": "archive"}), http.StatusForbidden, "a member exports")); code != "forbidden" {
			t.Errorf("a member's export is refused as %s", code)
		}
		if got := list(t, want(t, bob.get(t, "/api/v1/spaces/TRIP/exports"), http.StatusOK, "a member lists the exports"), "exports"); len(got) != 0 {
			t.Errorf("a member reads %d exports", len(got))
		}
		mine := list(t, want(t, owner.get(t, "/api/v1/spaces/TRIP/exports"), http.StatusOK, "the owner lists them"), "exports")[0].(map[string]any)
		want(t, bob.get(t, "/api/v1/space-exports/"+mine["id"].(string)+"/file"), http.StatusNotFound, "a member downloads one")
		want(t, owner.get(t, "/api/v1/space-exports/"+uuid.NewString()+"/file"), http.StatusNotFound, "no such export")
		fieldError(t, want(t, owner.post(t, "/api/v1/spaces/TRIP/exports", map[string]any{"format": "pdf"}), http.StatusUnprocessableEntity, "a format there is not"), "format")
		want(t, owner.post(t, "/api/v1/spaces/NOPE/exports", map[string]any{"format": "archive"}), http.StatusNotFound, "a space there is not")
		want(t, owner.get(t, "/api/v1/spaces/NOPE/exports"), http.StatusNotFound, "the exports of a space there is not")
	})

	t.Run("an export not made, or past its time, is not downloaded", func(t *testing.T) {
		ctx := context.Background()
		var spaceID uuid.UUID
		if err := h.super.QueryRow(ctx, `SELECT id FROM space WHERE org_id = $1 AND key = 'TRIP'`, from.org).Scan(&spaceID); err != nil {
			t.Fatal(err)
		}
		failed := uuid.New()
		if _, err := h.super.Exec(ctx, `
			INSERT INTO space_export (id, org_id, space_id, space_key, requested_by, format, state, failure)
			VALUES ($1, $2, $3, 'TRIP', $4, 'archive', 'failed', 'failed')`, failed, from.org, spaceID, from.user); err != nil {
			t.Fatal(err)
		}
		h.settle(t)
		if code := errorCode(t, want(t, owner.get(t, "/api/v1/space-exports/"+failed.String()+"/file"), http.StatusConflict, "a failed export")); code != "not_ready" {
			t.Errorf("a failed export is refused as %s", code)
		}
		done := list(t, want(t, owner.get(t, "/api/v1/spaces/TRIP/exports"), http.StatusOK, "the exports"), "exports")
		var made map[string]any
		for _, each := range done {
			if job := each.(map[string]any); job["state"] == "done" {
				made = job
			}
		}
		var key string
		if err := h.super.QueryRow(ctx, `UPDATE space_export SET expires_at = now() - interval '1 minute' WHERE id = $1 RETURNING object_key`, made["id"]).Scan(&key); err != nil {
			t.Fatal(err)
		}
		if _, err := spaceio.NewWatch(h.cluster, h.appStore(t), discard(), time.Hour, time.Hour).Once(ctx); err != nil {
			t.Fatal(err)
		}
		h.settle(t)
		if code := errorCode(t, want(t, owner.get(t, "/api/v1/space-exports/"+made["id"].(string)+"/file"), http.StatusGone, "an expired export")); code != "export_expired" {
			t.Errorf("an expired export is refused as %s", code)
		}
		var state string
		var tombstones int
		if err := h.super.QueryRow(ctx, `SELECT state, (SELECT count(*) FROM attachment_tombstone WHERE object_key = $2) FROM space_export WHERE id = $1`, made["id"], key).Scan(&state, &tombstones); err != nil || state != "expired" {
			t.Errorf("the expired export is %s (%v)", state, err)
		}
		if tombstones != 1 {
			t.Errorf("the expired export's file has %d tombstones, want one for the reaper", tombstones)
		}
	})

	t.Run("the HTML export reads offline", func(t *testing.T) {
		_, data := api.exportSpace(t, owner, "TRIP", "html")
		files := archiveFiles(t, data)
		index, page := string(files["index.html"]), string(files["nested.html"])
		if index == "" || page == "" || len(files["style.css"]) == 0 {
			t.Fatalf("the HTML export holds %v", slices.Sorted(maps.Keys(files)))
		}
		for _, want := range []string{`href="top.html"`, `href="box.html"`, `href="departure.html"`, "<title>Trip - Trip</title>"} {
			if !strings.Contains(index, want) {
				t.Errorf("index.html lacks %s", want)
			}
		}
		for _, want := range []string{`href="top.html"`, `files/` + second["id"].(string) + `/plan.txt`, "Ann", "trip"} {
			if !strings.Contains(page, want) {
				t.Errorf("nested.html lacks %s:\n%s", want, page)
			}
		}
		if string(files["files/"+second["id"].(string)+"/plan.txt"]) != "second plan" {
			t.Error("the HTML export does not hold the current plan")
		}
		for name, data := range files {
			if strings.Contains(string(data), "<script") {
				t.Errorf("%s holds a script", name)
			}
		}
	})

	to := h.makeMember(t, "travel-to")
	toSlug := h.slugOf(t, to.org)
	landedOwner := api.as(t, to.user, to.org, toSlug)
	if _, err := h.super.Exec(context.Background(), `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'member')`, to.org, annID); err != nil {
		t.Fatal(err)
	}
	landedWriters := h.makeGroup(t, to.org, "writers")
	annThere := api.as(t, annID, to.org, toSlug)

	var landed map[string]any
	t.Run("the archive comes into another organization under a new key", func(t *testing.T) {
		landed = api.importSpace(t, landedOwner, archive, "key=landed&name=Landed+trip")
		if landed["state"] != "done" || landed["spaceKey"] != "LANDED" {
			t.Fatalf("the import ended as %v", landed)
		}
		report := landed["report"].(map[string]any)
		if number(report["pages"]) != 6 || number(report["versions"]) != counts.Versions || number(report["files"]) != 2 || number(report["comments"]) != 1 ||
			number(report["templates"]) != 1 || number(report["calendars"]) != 1 {
			t.Errorf("the report counts %v", report)
		}
		people, _ := json.Marshal(report["people"])
		if !strings.Contains(string(people), emailOf(t, h, from.user)) || strings.Contains(string(people), emailOf(t, h, annID)) {
			t.Errorf("the people not found are %s", people)
		}
		if groups := report["groups"].([]any); len(groups) != 0 {
			t.Errorf("the groups not found are %v", groups)
		}
		dropped, _ := json.Marshal(report["dropped"])
		if !strings.Contains(string(dropped), `"permission":"administer"`) || !strings.Contains(string(dropped), emailOf(t, h, from.user)) {
			t.Errorf("the dropped grants are %s", dropped)
		}
		if number(report["droppedReactions"]) != 1 || number(report["reattributed"]) == 0 || number(report["mentionsAsText"]) == 0 {
			t.Errorf("the report reads %v", report)
		}
	})

	theirs, _ := outline(t, owner, "TRIP")
	ours, ids := outline(t, landedOwner, "LANDED")
	t.Run("the tree is the same", func(t *testing.T) {
		if !slices.Equal(theirs[1:], ours[1:]) || ours[0] != "Trip" {
			t.Errorf("the tree came across as %q, from %q", ours, theirs)
		}
		space := obj(t, want(t, landedOwner.get(t, "/api/v1/spaces/LANDED"), http.StatusOK, "the new space"), "space")
		if space["name"] != "Landed trip" {
			t.Errorf("the new space is called %v", space["name"])
		}
	})

	nestedThere := ids["Nested"]
	t.Run("versions keep their authors, or their names", func(t *testing.T) {
		versions := list(t, want(t, landedOwner.get(t, pagePath(nestedThere, "/versions")), http.StatusOK, "the history"), "versions")
		if len(versions) != 3 {
			t.Fatalf("Nested has %d versions", len(versions))
		}
		latest, oldest := versions[0].(map[string]any), versions[2].(map[string]any)
		if latest["authorId"] != annID.String() || latest["comment"] != "Ann's turn" || latest["originalAuthor"] != nil {
			t.Errorf("Ann's version came across as %v", latest)
		}
		if oldest["authorId"] != to.user.String() || oldest["originalAuthor"] != "Person of travel-from" {
			t.Errorf("the owner's version came across as %v", oldest)
		}
		body := string(want(t, landedOwner.get(t, pagePath(nestedThere, "/versions/2")), http.StatusOK, "version 2").Raw)
		if !strings.Contains(body, annID.String()) || strings.Contains(body, from.user.String()) || !strings.Contains(body, "@The Organizer") ||
			!strings.Contains(body, "/s/LANDED/p/"+ids["Top"]) || strings.Contains(body, second["id"].(string)) {
			t.Errorf("version 2 reads %s", body)
		}
	})

	t.Run("files keep their versions and their bytes", func(t *testing.T) {
		files := list(t, want(t, landedOwner.get(t, "/api/v1/pages/"+nestedThere+"/attachments"), http.StatusOK, "the files"), "attachments")
		if len(files) != 2 {
			t.Fatalf("Nested has %d files", len(files))
		}
		for _, each := range files {
			f := each.(map[string]any)
			_, data := landedOwner.download(t, "/api/v1/attachments/"+f["id"].(string))
			want := map[int]string{1: "first plan", 2: "second plan"}[number(f["version"])]
			if string(data) != want {
				t.Errorf("version %d of the plan reads %q", number(f["version"]), data)
			}
		}
	})

	t.Run("labels, discussions, reactions, the blog, calendars and templates come along", func(t *testing.T) {
		if got := labelsOf(t, want(t, landedOwner.get(t, pagePath(nestedThere, "/labels")), http.StatusOK, "the labels")); !slices.Equal(got, []string{"trip"}) {
			t.Errorf("the labels are %v", got)
		}
		threads := string(want(t, landedOwner.get(t, pagePath(nestedThere, "/comments")), http.StatusOK, "the discussion").Raw)
		if !strings.Contains(threads, "Looks good") || !strings.Contains(threads, annID.String()) {
			t.Errorf("the discussion reads %s", threads)
		}
		reactionsOn, _ := json.Marshal(obj(t, want(t, landedOwner.get(t, pagePath(nestedThere)), http.StatusOK, "the page"), "page")["reactions"])
		reactions := string(reactionsOn)
		if !strings.Contains(reactions, "👍") || strings.Contains(reactions, "🎉") {
			t.Errorf("the reactions read %s", reactions)
		}
		posts := string(want(t, landedOwner.get(t, "/api/v1/posts?space=LANDED"), http.StatusOK, "the blog").Raw)
		if !strings.Contains(posts, "Departure") {
			t.Errorf("the blog reads %s", posts)
		}
		calendars := string(want(t, landedOwner.get(t, calendarsPath("LANDED")), http.StatusOK, "the calendars").Raw)
		if !strings.Contains(calendars, "Trips") {
			t.Errorf("the calendars read %s", calendars)
		}
		templates := string(want(t, landedOwner.get(t, "/api/v1/templates?space=LANDED"), http.StatusOK, "the templates").Raw)
		if !strings.Contains(templates, "Trip report") {
			t.Errorf("the templates read %s", templates)
		}
	})

	t.Run("who may do what is found again by address and name", func(t *testing.T) {
		grants := string(want(t, landedOwner.get(t, "/api/v1/spaces/LANDED/permissions"), http.StatusOK, "the permissions").Raw)
		for _, want := range []string{landedWriters.String(), annID.String(), to.user.String()} {
			if !strings.Contains(grants, want) {
				t.Errorf("the permissions lack %s: %s", want, grants)
			}
		}
		lists := string(want(t, annThere.get(t, pagePath(ids["Top"], "/restrictions")), http.StatusOK, "Top's lists").Raw)
		if !strings.Contains(lists, landedWriters.String()) || !strings.Contains(lists, annID.String()) {
			t.Errorf("Top's lists read %s", lists)
		}
	})

	t.Run("a key is taken once", func(t *testing.T) {
		fieldError(t, want(t, sendArchive(t, landedOwner, archive, "key=LANDED"), http.StatusUnprocessableEntity, "the same key again"), "key")
		fieldError(t, want(t, sendArchive(t, landedOwner, archive, "key=9X"), http.StatusUnprocessableEntity, "a key no space takes"), "key")
	})
}

// Whatever is not an archive of Stator's, or not one it accepts, is refused
// in a sentence, at once when it can be and by the job when only reading
// it all tells.
func TestInvalidArchivesAreRefusedInSentences(t *testing.T) {
	h := newHarness(t)
	api := transferAPI(t, h)
	home := h.makeMember(t, "transfer-refusals")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	member := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)
	docs := newTree(t, owner, "SRC", "Source")
	page := docs.add(docs.homeID, "Plain")
	_, archive := api.exportSpace(t, owner, "SRC", "archive")
	files := archiveFiles(t, archive)

	newer := map[string]any{}
	_ = json.Unmarshal(files["manifest.json"], &newer)
	newer["version"] = spaceio.Version + 1
	newerManifest, _ := json.Marshal(newer)
	for what, data := range map[string][]byte{
		"a file that is no zip":   []byte("plain words"),
		"a zip without manifest":  zipped(t, map[string][]byte{"readme.md": []byte("# hello")}),
		"an archive from later":   zipped(t, map[string][]byte{"manifest.json": newerManifest}),
		"a manifest of something": zipped(t, map[string][]byte{"manifest.json": []byte(`{"format":"other"}`)}),
	} {
		got := want(t, sendArchive(t, owner, data, "key=OTHER"), http.StatusUnprocessableEntity, what)
		if msg := fieldError(t, got, "file"); !strings.Contains(msg, "xport") {
			t.Errorf("%s is refused with %q, which does not say to export again", what, msg)
		}
	}
	if code := errorCode(t, want(t, sendArchive(t, member, archive, "key=OTHER"), http.StatusForbidden, "a member imports")); code != "forbidden" {
		t.Errorf("a member's import is refused as %s", code)
	}
	want(t, owner.send(t, http.MethodPost, "/api/v1/space-imports", "application/json", strings.NewReader("{}")), http.StatusBadRequest, "no upload at all")

	t.Run("a document Stator does not accept fails the import by its page", func(t *testing.T) {
		var p map[string]any
		if err := json.Unmarshal(files["pages/"+page+".json"], &p); err != nil {
			t.Fatal(err)
		}
		p["body"] = map[string]any{"type": "doc", "content": []any{map[string]any{"type": "script", "text": "alert(1)"}}}
		tampered := map[string][]byte{}
		for name, data := range files {
			tampered[name] = data
		}
		tampered["pages/"+page+".json"], _ = json.Marshal(p)
		job := api.importSpace(t, owner, zipped(t, tampered), "key=TAMPER")
		if job["state"] != "failed" || job["failure"] != "invalid" || !strings.Contains(job["message"].(string), `"Plain"`) {
			t.Errorf("the tampered import ended as %v", job)
		}
		want(t, owner.get(t, "/api/v1/spaces/TAMPER"), http.StatusNotFound, "the space of a failed import")
		imports := list(t, want(t, owner.get(t, "/api/v1/space-imports"), http.StatusOK, "the imports"), "imports")
		if len(imports) != 1 || imports[0].(map[string]any)["id"] != job["id"] {
			t.Errorf("the owner's imports are %v", imports)
		}
		want(t, member.get(t, "/api/v1/space-imports/"+job["id"].(string)), http.StatusNotFound, "a member reads the owner's import")
		if got := list(t, want(t, member.get(t, "/api/v1/space-imports"), http.StatusOK, "a member's imports"), "imports"); len(got) != 0 {
			t.Errorf("a member lists %d imports", len(got))
		}
		want(t, api.anonymous().get(t, "/api/v1/space-imports"), http.StatusUnauthorized, "somebody not signed in lists imports")
	})

	t.Run("an archive over the limit is refused", func(t *testing.T) {
		small := transferAPI(t, h, func(s *httpapi.Server) { s.SpaceTransfers.MaxImportBytes = 64 })
		if code := errorCode(t, want(t, sendArchive(t, small.as(t, home.user, home.org, slug), archive, ""), http.StatusRequestEntityTooLarge, "a large archive")); code != "too_large" {
			t.Errorf("a large archive is refused as %s", code)
		}
	})
}

// Straight through SQL as stator_app: an export is queued only by an
// administrator of its space and an import only by whoever may create
// spaces, each for themselves and as a job still to run; the rows are read
// by their requester and administrators, and the worker alone writes on.
func TestTheDatabaseKeepsSpaceTransfersToTheirPeople(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "transfer-sql")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	memberID := h.addPerson(t, home.org, "member")
	sp := obj(t, want(t, owner.post(t, "/api/v1/spaces", map[string]any{"key": "SQLX", "name": "SQL"}), http.StatusCreated, "make a space"), "space")
	spaceID := uuid.MustParse(sp["id"].(string))
	ctx := context.Background()
	exportSQL := `INSERT INTO space_export (id, org_id, space_id, space_key, requested_by, format) VALUES ($1, $2, $3, 'SQLX', $4, 'archive')`
	importSQL := `INSERT INTO space_import (id, org_id, requested_by, key, size_bytes) VALUES ($1, $2, $3, 'NEWX', 10)`

	conn := appConn(t)
	actAs(t, conn, home.org, memberID)
	refused(t, conn, "a member exporting a space they do not administer", exportSQL, uuid.New(), home.org, spaceID, memberID)
	refused(t, conn, "a member importing a space", importSQL, uuid.New(), home.org, memberID)

	actAs(t, conn, home.org, home.user)
	refused(t, conn, "an administrator exporting for somebody else", exportSQL, uuid.New(), home.org, spaceID, memberID)
	refused(t, conn, "an administrator importing for somebody else", importSQL, uuid.New(), home.org, memberID)
	refused(t, conn, "an export queued as done",
		`INSERT INTO space_export (id, org_id, space_id, space_key, requested_by, format, state) VALUES ($1, $2, $3, 'SQLX', $4, 'archive', 'done')`, uuid.New(), home.org, spaceID, home.user)
	refused(t, conn, "an import queued with its space made",
		`INSERT INTO space_import (id, org_id, requested_by, key, size_bytes, space_id) VALUES ($1, $2, $3, 'NEWX', 10, $4)`, uuid.New(), home.org, home.user, spaceID)
	export, imported := uuid.New(), uuid.New()
	if _, err := conn.Exec(ctx, exportSQL, export, home.org, spaceID, home.user); err != nil {
		t.Fatalf("an administrator cannot queue an export through SQL: %v", err)
	}
	if _, err := conn.Exec(ctx, importSQL, imported, home.org, home.user); err != nil {
		t.Fatalf("an administrator cannot queue an import through SQL: %v", err)
	}
	for what, sql := range map[string]string{
		"marking an export done":   `UPDATE space_export SET state = 'done' WHERE id = $1`,
		"deleting an export":       `DELETE FROM space_export WHERE id = $1`,
		"marking an import done":   `UPDATE space_import SET state = 'done' WHERE id = $1`,
		"naming an import's space": `UPDATE space_import SET space_id = NULL WHERE id = $1`,
		"deleting an import":       `DELETE FROM space_import WHERE id = $1`,
	} {
		id := export
		if strings.Contains(what, "import") {
			id = imported
		}
		refused(t, conn, "an administrator "+what, sql, id)
	}
	var seen int
	if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM space_export WHERE id = $1) + (SELECT count(*) FROM space_import WHERE id = $2)`, export, imported).Scan(&seen); err != nil || seen != 2 {
		t.Errorf("the administrator reads %d of their jobs: %v", seen, err)
	}
	actAs(t, conn, home.org, memberID)
	if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM space_export) + (SELECT count(*) FROM space_import)`).Scan(&seen); err != nil || seen != 0 {
		t.Errorf("a member reads %d jobs: %v", seen, err)
	}
	// The suite's rows would otherwise wait for a worker that cannot read
	// their files.
	h.cleanupExec(t, h.super, `DELETE FROM space_export WHERE id = $1`, export)
	h.cleanupExec(t, h.super, `DELETE FROM space_import WHERE id = $1`, imported)
}
