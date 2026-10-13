//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/spaceio"
	"github.com/praetorianer777/stator/backend/internal/theme"
)

// orgTransferAPI keeps themes in the app's bucket too, as transferAPI does
// files on pages.
func orgTransferAPI(t *testing.T, h *harness) *apiServer {
	t.Helper()
	return transferAPI(t, h, func(s *httpapi.Server) { s.Themes = theme.NewService(h.cluster, h.appStore(t)) })
}

// exportOrg exports the caller's organization and downloads the archive.
func (a *apiServer) exportOrg(t *testing.T, c *client) (map[string]any, []byte) {
	t.Helper()
	queued := obj(t, want(t, c.post(t, "/api/v1/site/exports", nil), http.StatusAccepted, "export the organization"), "export")
	id := queued["id"].(string)
	done := a.followTransfer(t, func() map[string]any {
		for _, each := range list(t, want(t, c.get(t, "/api/v1/site/exports"), http.StatusOK, "follow the export"), "exports") {
			if job := each.(map[string]any); job["id"] == id {
				return job
			}
		}
		t.Fatalf("the export %s is not listed", id)
		return nil
	})
	if done["state"] != "done" {
		t.Fatalf("the organization's export did not finish: %v", done)
	}
	resp, data := c.download(t, "/api/v1/site/exports/"+id+"/file")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/zip" {
		t.Fatalf("the export downloads as %d %s: %s", resp.StatusCode, resp.Header.Get("Content-Type"), data)
	}
	return done, data
}

func sendOrgArchive(t *testing.T, c *client, data []byte) response {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "organization.zip")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write(data)
	_ = form.Close()
	return c.send(t, http.MethodPost, "/api/v1/site/imports", form.FormDataContentType(), &body)
}

// importOrg imports an organization's archive and follows the job to its end.
func (a *apiServer) importOrg(t *testing.T, c *client, data []byte) map[string]any {
	t.Helper()
	queued := obj(t, want(t, sendOrgArchive(t, c, data), http.StatusAccepted, "import the organization"), "import")
	id := queued["id"].(string)
	return a.followTransfer(t, func() map[string]any {
		return obj(t, want(t, c.get(t, "/api/v1/site/imports/"+id), http.StatusOK, "follow the import"), "import")
	})
}

// An organization goes out as one archive and comes into a fresh instance
// whole: its spaces with their trees, versions, files, comments,
// restrictions and drafts, a link from one space to another, its people in
// their roles, a guest in their space, its groups, permissions, hub, brand,
// themes, templates and webhooks, with what does not come across listed.
func TestAnOrganizationTravelsToAFreshOneWhole(t *testing.T) {
	h := newHarness(t)
	api := orgTransferAPI(t, h)
	ctx := context.Background()
	from := h.makeMember(t, "org-from")
	fromSlug := h.slugOf(t, from.org)
	t.Cleanup(func() { h.cleanupExec(t, h.super, `UPDATE org SET default_theme_id = NULL WHERE id = $1`, from.org) })
	owner := api.as(t, from.user, from.org, fromSlug)
	annID := h.namedPerson(t, from.org, "Ann Mover")
	ann := api.as(t, annID, from.org, fromSlug)
	bobID := h.addPerson(t, from.org, "member")
	bob := api.as(t, bobID, from.org, fromSlug)
	doraID := h.namedPerson(t, from.org, "Dora Leaver")
	doraEmail := emailOf(t, h, doraID)
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE email = $1`, doraEmail) })
	writers := h.makeGroup(t, from.org, "Writers", annID, doraID)
	want(t, owner.put(t, "/api/v1/site/permissions/createSpace", map[string]any{"subjects": []any{group(writers)}}), http.StatusOK, "let the writers create spaces")

	trip := newTree(t, owner, "TRIP", "Trip")
	top := trip.add(trip.homeID, "Top")
	nested := trip.add(top, "Nested")
	plan := obj(t, want(t, owner.upload(t, pagePath(nested, "/attachments"), "plan.txt", []byte("the plan")), http.StatusCreated, "upload the plan"), "attachment")
	publishDraft(t, ann, nested, "Nested", "Ann's words", false, "Ann's turn")
	want(t, restrict(t, owner, top, []any{group(writers), user(annID)}, []any{user(annID)}), http.StatusOK, "restrict Top")
	startThread(t, ann, nested, "Looks good")
	secret := trip.add(trip.homeID, "Secret plan", map[string]any{"publish": false, "body": textDoc("Not yet")})
	want(t, owner.put(t, pagePath(secret, "/draft"), map[string]any{"title": "Secret plan", "body": textDoc("Still not yet"), "baseVersion": 0}), http.StatusOK, "the owner drafts the secret plan")
	want(t, ann.put(t, pagePath(nested, "/draft"), map[string]any{"title": "Nested, again", "body": textDoc("Ann's next words"), "baseVersion": 2}), http.StatusOK, "ann drafts Nested")
	guestEmail := "gus-" + uuid.NewString()[:8] + "@example.test"
	guestID := obj(t, want(t, owner.post(t, "/api/v1/spaces/TRIP/guests", map[string]any{"email": guestEmail, "role": "viewer"}), http.StatusCreated, "invite a guest"), "guest")["userId"].(string)
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE email = $1`, guestEmail) })

	ops := newTree(t, owner, "OPS", "Operations")
	pointer := ops.add(ops.homeID, "Pointer")
	publishBody(t, owner, pointer, map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "paragraph", "content": []any{
			map[string]any{"type": "text", "text": "the top", "marks": []any{map[string]any{"type": "link", "attrs": map[string]any{"href": "/s/TRIP/p/" + top + "/top"}}}},
		}},
	}})

	want(t, ann.post(t, "/api/v1/spaces", map[string]any{"key": "ANNS", "name": "Ann's own", "personal": true}), http.StatusCreated, "ann's personal space")
	newTree(t, owner, "OLD", "Old times")
	want(t, owner.put(t, "/api/v1/spaces/OLD/archive", nil), http.StatusOK, "archive OLD")
	want(t, owner.put(t, "/api/v1/site/hub", map[string]any{"pageId": nested, "landing": true}), http.StatusOK, "choose the hub")
	want(t, owner.put(t, "/api/v1/site/brand/footer", map[string]any{"en": "Internal use only", "de": "Nur intern"}), http.StatusOK, "set the footer")
	want(t, owner.uploadWith(t, http.MethodPut, "/api/v1/site/brand/logo", "logo.png", pngOf(t, 8, 4)), http.StatusOK, "set the logo")
	house := obj(t, want(t, owner.post(t, "/api/v1/themes", map[string]any{"name": "House"}), http.StatusCreated, "make a theme"), "theme")["id"].(string)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><path d="M2 2h12v12H2z"/></svg>`)
	glyph := obj(t, want(t, owner.upload(t, "/api/v1/themes/"+house+"/assets", "block.svg", svg), http.StatusCreated, "upload a glyph"), "asset")["id"].(string)
	want(t, owner.patch(t, "/api/v1/themes/"+house, map[string]any{"shared": true, "spec": map[string]any{
		"colors": map[string]any{"light": map[string]string{"accent": "#336699"}},
		"icons":  map[string]any{"plus": map[string]any{"assetId": glyph}},
	}}), http.StatusOK, "share the theme with its glyph")
	want(t, owner.put(t, "/api/v1/themes/default", map[string]any{"themeId": house}), http.StatusOK, "make it the default")
	want(t, owner.post(t, "/api/v1/templates", map[string]any{"name": "Meeting notes", "title": "Meeting", "body": textDoc("Who came"), "variables": []any{}}), http.StatusCreated, "an organization's template")
	makeHook(t, owner, "Chat", "https://hooks.example.test/in", "page.published")

	var archive []byte
	t.Run("an administrator exports the organization as one archive", func(t *testing.T) {
		job, data := api.exportOrg(t, owner)
		if !strings.HasSuffix(job["fileName"].(string), ".zip") || !strings.HasPrefix(job["fileName"].(string), fromSlug+"-organization-") {
			t.Errorf("the export is named %v", job["fileName"])
		}
		archive = data
		files := archiveFiles(t, data)
		var m spaceio.OrgManifest
		if err := json.Unmarshal(files["manifest.json"], &m); err != nil {
			t.Fatalf("the manifest: %v", err)
		}
		if m.Format != spaceio.OrgFormat || m.Counts.Spaces != 4 || len(m.Spaces) != 4 || m.Brand.Logo == nil || len(m.Themes) != 1 ||
			len(m.Themes[0].Files) != 1 || len(m.Webhooks) != 1 || len(m.Templates) != 1 || m.AuditEntries == 0 {
			t.Errorf("the manifest reads %+v", m)
		}
		var lists []string
		for _, o := range m.NotCarried {
			lists = append(lists, o.What)
		}
		for _, what := range []string{"Passwords", "Sessions", "Personal access tokens", "Linked Armature accounts", "Webhook signing secrets"} {
			if !slices.Contains(lists, what) {
				t.Errorf("the archive does not say it leaves out %s: %v", what, lists)
			}
		}
		people := map[string]spaceio.OrgPerson{}
		for _, p := range m.People {
			people[p.Ref] = p
		}
		if people[annID.String()].Role != "member" || people[from.user.String()].Role != "owner" || people[guestID].GuestSpace != "TRIP" {
			t.Errorf("the people are %+v", m.People)
		}
		trip := archiveFiles(t, files["spaces/TRIP.zip"])
		var sm spaceio.Manifest
		if err := json.Unmarshal(trip["manifest.json"], &sm); err != nil {
			t.Fatalf("TRIP's manifest: %v", err)
		}
		if sm.Format != spaceio.Format || sm.Version != spaceio.Version || !slices.Contains(sm.Pages, uuid.MustParse(secret)) {
			t.Errorf("TRIP's archive is %+v", sm)
		}
		var p spaceio.ArchivePage
		if err := json.Unmarshal(trip["pages/"+nested+".json"], &p); err != nil {
			t.Fatal(err)
		}
		if len(p.Drafts) != 1 || p.Drafts[0].Person != annID.String() {
			t.Errorf("Nested's drafts are %+v", p.Drafts)
		}
		if string(trip["files/"+plan["id"].(string)]) != "the plan" {
			t.Error("TRIP's archive does not hold the plan")
		}
		if len(files["audit.jsonl"]) == 0 || len(files["brand/logo"]) == 0 {
			t.Error("the archive lacks the audit log or the logo")
		}
	})

	t.Run("nobody but an administrator of the organization exports it or reads its exports", func(t *testing.T) {
		if code := errorCode(t, want(t, bob.post(t, "/api/v1/site/exports", nil), http.StatusForbidden, "a member exports")); code != "forbidden" {
			t.Errorf("a member's export is refused as %s", code)
		}
		want(t, bob.get(t, "/api/v1/site/exports"), http.StatusForbidden, "a member lists the exports")
		made := list(t, want(t, owner.get(t, "/api/v1/site/exports"), http.StatusOK, "the owner lists them"), "exports")[0].(map[string]any)
		want(t, bob.get(t, "/api/v1/site/exports/"+made["id"].(string)+"/file"), http.StatusForbidden, "a member downloads one")
		want(t, owner.get(t, "/api/v1/site/exports/"+uuid.NewString()+"/file"), http.StatusNotFound, "no such export")
		want(t, ann.post(t, "/api/v1/site/imports", nil), http.StatusForbidden, "a member imports")
	})

	// The archive lands in a fresh instance, whose one organization has no
	// spaces, where everybody but dora already has an account to be found by.
	fresh := newFreshHarness(t)
	freshAPI := orgTransferAPI(t, fresh)
	to := fresh.makeMember(t, "org-to")
	toSlug := fresh.slugOf(t, to.org)
	there := map[uuid.UUID]uuid.UUID{}
	for _, id := range []uuid.UUID{from.user, annID, bobID, uuid.MustParse(guestID)} {
		there[id] = fresh.accountLike(t, h, id)
	}
	landed := freshAPI.as(t, to.user, to.org, toSlug)

	t.Run("what is not an organization's archive is refused in a sentence", func(t *testing.T) {
		_, spaceArchive := api.exportSpace(t, owner, "OPS", "archive")
		got := want(t, sendOrgArchive(t, landed, spaceArchive), http.StatusUnprocessableEntity, "a space's archive")
		if msg := fieldError(t, got, "file"); !strings.Contains(msg, "organization") {
			t.Errorf("a space's archive is refused with %q", msg)
		}
	})

	var job map[string]any
	t.Run("the archive fills a fresh organization", func(t *testing.T) {
		job = freshAPI.importOrg(t, landed, archive)
		if job["state"] != "done" {
			t.Fatalf("the import ended as %v", job)
		}
		report := job["report"].(map[string]any)
		if number(report["spaces"]) != 4 || number(report["drafts"]) < 2 || number(report["files"]) != 1 || number(report["comments"]) != 1 ||
			number(report["themes"]) != 1 || number(report["webhooks"]) != 1 || number(report["auditEntries"]) == 0 {
			t.Errorf("the report counts %v", report)
		}
		if number(report["people"]) != 5 || number(report["matched"]) != 4 || number(report["groups"]) != 1 {
			t.Errorf("the report counts the people as %v, %v of them found, and %v groups", report["people"], report["matched"], report["groups"])
		}
		if len(report["notCarried"].([]any)) == 0 {
			t.Errorf("the report lists nothing left out: %v", report)
		}
		if imports := list(t, want(t, landed.get(t, "/api/v1/site/imports"), http.StatusOK, "the imports"), "imports"); len(imports) != 1 || imports[0].(map[string]any)["id"] != job["id"] {
			t.Errorf("the organization's imports are %v", imports)
		}
	})

	annThere := freshAPI.as(t, there[annID], to.org, toSlug)
	// The owner came across in their role, as did the page only they read.
	ownerThere := freshAPI.as(t, there[from.user], to.org, toSlug)
	theirs, _ := outline(t, owner, "TRIP")
	ours, ids := outline(t, ownerThere, "TRIP")
	t.Run("the spaces, their trees and the people in their roles are the same", func(t *testing.T) {
		if !slices.Equal(theirs, ours) {
			t.Errorf("the tree came across as %q, from %q", ours, theirs)
		}
		var role string
		if err := fresh.super.QueryRow(ctx, `SELECT org_role FROM org_member WHERE org_id = $1 AND user_id = $2`, to.org, there[annID]).Scan(&role); err != nil || role != "member" {
			t.Errorf("ann is %q in the new organization: %v", role, err)
		}
		var doraThere uuid.UUID
		if err := fresh.super.QueryRow(ctx, `
			SELECT u.id FROM app_user u JOIN org_member m ON m.user_id = u.id AND m.org_id = $1 WHERE u.email = $2`, to.org, doraEmail).Scan(&doraThere); err != nil {
			t.Errorf("dora was not made again: %v", err)
		}
		var guestSpace string
		if err := fresh.super.QueryRow(ctx, `
			SELECT s.key FROM org_member m JOIN space s ON s.id = m.guest_space_id WHERE m.org_id = $1 AND m.user_id = $2`, to.org, there[uuid.MustParse(guestID)]).Scan(&guestSpace); err != nil || guestSpace != "TRIP" {
			t.Errorf("the guest's space is %q: %v", guestSpace, err)
		}
		var personalOwner, archived string
		if err := fresh.super.QueryRow(ctx, `
			SELECT COALESCE((SELECT owner_id::text FROM space WHERE org_id = $1 AND key = 'ANNS'), ''),
			       COALESCE((SELECT archived_at::text FROM space WHERE org_id = $1 AND key = 'OLD'), '')`, to.org).Scan(&personalOwner, &archived); err != nil ||
			personalOwner != there[annID].String() || archived == "" {
			t.Errorf("ann's personal space is %q's and OLD archived at %q: %v", personalOwner, archived, err)
		}
		groups := string(want(t, landed.get(t, "/api/v1/groups"), http.StatusOK, "the groups").Raw)
		if !strings.Contains(groups, "Writers") {
			t.Errorf("the groups are %s", groups)
		}
		var members int
		if err := fresh.super.QueryRow(ctx, `
			SELECT count(*) FROM group_member m JOIN groups g ON g.id = m.group_id WHERE g.org_id = $1 AND g.name = 'Writers'`, to.org).Scan(&members); err != nil || members != 2 {
			t.Errorf("Writers has %d members: %v", members, err)
		}
	})

	nestedThere, topThere := ids["Nested"], ids["Top"]
	t.Run("restrictions hold, versions keep their authors, files and comments come along", func(t *testing.T) {
		want(t, annThere.get(t, pagePath(topThere)), http.StatusOK, "ann reads Top")
		var bobThere uuid.UUID
		if err := fresh.super.QueryRow(ctx, `SELECT user_id FROM org_member WHERE org_id = $1 AND user_id = $2`, to.org, there[bobID]).Scan(&bobThere); err != nil {
			t.Fatalf("bob did not come across: %v", err)
		}
		bobLanded := freshAPI.as(t, bobThere, to.org, toSlug)
		want(t, bobLanded.get(t, pagePath(topThere)), http.StatusNotFound, "bob reads Top")
		lists := string(want(t, annThere.get(t, pagePath(topThere, "/restrictions")), http.StatusOK, "Top's lists").Raw)
		if !strings.Contains(lists, there[annID].String()) || !strings.Contains(lists, "Writers") {
			t.Errorf("Top's lists read %s", lists)
		}
		versions := list(t, want(t, landed.get(t, pagePath(nestedThere, "/versions")), http.StatusOK, "the history"), "versions")
		if len(versions) != 2 || versions[0].(map[string]any)["authorId"] != there[annID].String() || versions[1].(map[string]any)["authorId"] != there[from.user].String() {
			t.Errorf("Nested's history is %v", versions)
		}
		files := list(t, want(t, landed.get(t, pagePath(nestedThere, "/attachments")), http.StatusOK, "the files"), "attachments")
		if len(files) != 1 {
			t.Fatalf("Nested has %d files", len(files))
		}
		if _, data := landed.download(t, "/api/v1/attachments/"+files[0].(map[string]any)["id"].(string)); string(data) != "the plan" {
			t.Errorf("the plan reads %q", data)
		}
		if threads := string(want(t, landed.get(t, pagePath(nestedThere, "/comments")), http.StatusOK, "the discussion").Raw); !strings.Contains(threads, "Looks good") {
			t.Errorf("the discussion reads %s", threads)
		}
	})

	t.Run("drafts and pages never published stay their authors' own", func(t *testing.T) {
		draft := obj(t, want(t, annThere.get(t, pagePath(nestedThere, "/draft")), http.StatusOK, "ann's draft"), "draft")
		if draft["title"] != "Nested, again" || !strings.Contains(string(want(t, annThere.get(t, pagePath(nestedThere, "/draft")), http.StatusOK, "again").Raw), "Ann's next words") {
			t.Errorf("ann's draft is %v", draft)
		}
		var secretThere string
		if err := fresh.super.QueryRow(ctx, `
			SELECT p.id::text FROM page p JOIN space s ON s.id = p.space_id WHERE s.org_id = $1 AND p.title = 'Secret plan' AND p.version = 0`, to.org).Scan(&secretThere); err != nil {
			t.Fatalf("the secret plan did not come across unpublished: %v", err)
		}
		want(t, ownerThere.get(t, pagePath(secretThere)), http.StatusOK, "its author reads the secret plan")
		want(t, annThere.get(t, pagePath(secretThere)), http.StatusNotFound, "ann reads the secret plan")
		mine := obj(t, want(t, ownerThere.get(t, pagePath(secretThere, "/draft")), http.StatusOK, "the owner's draft"), "draft")
		if mine == nil || mine["title"] != "Secret plan" {
			t.Errorf("the owner's draft of the secret plan is %v", mine)
		}
	})

	t.Run("a link from one space to another finds its page", func(t *testing.T) {
		_, opsIDs := outline(t, landed, "OPS")
		body := string(want(t, landed.get(t, pagePath(opsIDs["Pointer"])), http.StatusOK, "the pointer").Raw)
		if !strings.Contains(body, "/s/TRIP/p/"+topThere) || strings.Contains(body, top) {
			t.Errorf("the pointer reads %s", body)
		}
	})

	t.Run("settings, brand, themes, templates and webhooks come along", func(t *testing.T) {
		hub := obj(t, want(t, landed.get(t, "/api/v1/site/hub"), http.StatusOK, "the hub"), "hub")
		if page, _ := hub["page"].(map[string]any); page == nil || page["id"] != nestedThere || hub["landing"] != true {
			t.Errorf("the hub is %v", hub)
		}
		b := obj(t, want(t, landed.get(t, "/api/v1/site/brand"), http.StatusOK, "the brand"), "brand")
		if footer := b["footer"].(map[string]any); footer["en"] != "Internal use only" || b["logo"] == nil {
			t.Errorf("the brand is %v", b)
		}
		if resp, data := landed.download(t, "/api/v1/site/brand/logo"); resp.StatusCode != http.StatusOK || len(data) == 0 {
			t.Errorf("the logo answers %d", resp.StatusCode)
		}
		var themeID, spec string
		var assets int
		if err := fresh.super.QueryRow(ctx, `
			SELECT t.id::text, t.spec::text, (SELECT count(*) FROM theme_asset a WHERE a.theme_id = t.id)
			FROM org o JOIN theme t ON t.id = o.default_theme_id WHERE o.id = $1`, to.org).Scan(&themeID, &spec, &assets); err != nil {
			t.Fatalf("the default theme did not come across: %v", err)
		}
		if assets != 1 || strings.Contains(spec, glyph) || !strings.Contains(spec, "#336699") {
			t.Errorf("the theme %s has %d files and reads %s", themeID, assets, spec)
		}
		if templates := string(want(t, landed.get(t, "/api/v1/templates"), http.StatusOK, "the templates").Raw); !strings.Contains(templates, "Meeting notes") {
			t.Errorf("the templates are %s", templates)
		}
		hooks := list(t, want(t, landed.get(t, "/api/v1/webhooks"), http.StatusOK, "the webhooks"), "webhooks")
		if len(hooks) != 1 || hooks[0].(map[string]any)["enabled"] != false {
			t.Errorf("the webhooks are %v", hooks)
		}
		var canCreate int
		if err := fresh.super.QueryRow(ctx, `
			SELECT count(*) FROM global_grant gg JOIN groups g ON g.id = gg.group_id
			WHERE gg.org_id = $1 AND gg.permission = 'createSpace' AND g.name = 'Writers'`, to.org).Scan(&canCreate); err != nil || canCreate != 1 {
			t.Errorf("Writers may create spaces %d times: %v", canCreate, err)
		}
		var created int
		if err := fresh.super.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = 'space.created'`, to.org).Scan(&created); err != nil || created != 4 {
			t.Errorf("the audit log holds %d of the spaces made in the old record: %v", created, err)
		}
		if data := fresh.recordedOnce(t, to.org, audit.ActionOrgImported, &to.user, nil); !strings.Contains(data, fromSlug) {
			t.Errorf("the import's record reads %s", data)
		}
	})

	t.Run("an organization with spaces takes no archive", func(t *testing.T) {
		if code := errorCode(t, want(t, sendOrgArchive(t, landed, archive), http.StatusConflict, "import again")); code != "org_not_empty" {
			t.Errorf("a second import is refused as %s", code)
		}
	})
}

// Straight through SQL as stator_app: only an administrator of the
// organization queues an export or an import of it, for themselves and as
// a job still to run, reads them, and nobody but the worker writes on.
func TestTheDatabaseKeepsOrgTransfersToAdministrators(t *testing.T) {
	h := newHarness(t)
	home := h.makeMember(t, "org-transfer-sql")
	memberID := h.addPerson(t, home.org, "member")
	adminID := h.addPerson(t, home.org, "admin")
	ctx := context.Background()
	exportSQL := `INSERT INTO org_export (id, org_id, requested_by) VALUES ($1, $2, $3)`
	importSQL := `INSERT INTO org_import (id, org_id, requested_by, size_bytes) VALUES ($1, $2, $3, 10)`

	conn := appConn(t)
	actAs(t, conn, home.org, memberID)
	refused(t, conn, "a member exporting the organization", exportSQL, uuid.New(), home.org, memberID)
	refused(t, conn, "a member importing into the organization", importSQL, uuid.New(), home.org, memberID)

	actAs(t, conn, home.org, adminID)
	refused(t, conn, "an administrator exporting for somebody else", exportSQL, uuid.New(), home.org, memberID)
	refused(t, conn, "an administrator importing for somebody else", importSQL, uuid.New(), home.org, memberID)
	refused(t, conn, "an export queued as done",
		`INSERT INTO org_export (id, org_id, requested_by, state) VALUES ($1, $2, $3, 'done')`, uuid.New(), home.org, adminID)
	refused(t, conn, "an import queued with its report",
		`INSERT INTO org_import (id, org_id, requested_by, size_bytes, report) VALUES ($1, $2, $3, 10, '{}')`, uuid.New(), home.org, adminID)
	export, imported := uuid.New(), uuid.New()
	if _, err := conn.Exec(ctx, exportSQL, export, home.org, adminID); err != nil {
		t.Fatalf("an administrator cannot queue an export through SQL: %v", err)
	}
	if _, err := conn.Exec(ctx, importSQL, imported, home.org, adminID); err != nil {
		t.Fatalf("an administrator cannot queue an import through SQL: %v", err)
	}
	// The suite's rows would otherwise wait for a worker that has no file
	// for them.
	t.Cleanup(func() {
		h.cleanupExec(t, h.super, `DELETE FROM org_export WHERE id = $1`, export)
		h.cleanupExec(t, h.super, `DELETE FROM org_import WHERE id = $1`, imported)
	})
	for what, sql := range map[string]string{
		"marking an export done":   `UPDATE org_export SET state = 'done' WHERE id = $1`,
		"deleting an export":       `DELETE FROM org_export WHERE id = $1`,
		"marking an import done":   `UPDATE org_import SET state = 'done' WHERE id = $1`,
		"writing an import report": `UPDATE org_import SET report = '{}' WHERE id = $1`,
		"deleting an import":       `DELETE FROM org_import WHERE id = $1`,
	} {
		id := export
		if strings.Contains(what, "import") {
			id = imported
		}
		refused(t, conn, "an administrator "+what, sql, id)
	}
	var seen int
	if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM org_export WHERE id = $1) + (SELECT count(*) FROM org_import WHERE id = $2)`, export, imported).Scan(&seen); err != nil || seen != 2 {
		t.Errorf("the administrator reads %d of the jobs: %v", seen, err)
	}
	actAs(t, conn, home.org, memberID)
	if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM org_export) + (SELECT count(*) FROM org_import)`).Scan(&seen); err != nil || seen != 0 {
		t.Errorf("a member reads %d jobs: %v", seen, err)
	}
}
