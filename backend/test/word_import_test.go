//go:build integration

package test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/document"
	"github.com/praetorianer777/stator/backend/internal/docx"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/wordio"
)

// Word import (#89): one document becomes a page in the request; several,
// and folders of them in an archive, through the worker. Both as the
// importer, refused to whoever may not add pages there, by the service and
// by the database.

// wordWait bounds how long a test waits for the worker to import.
const wordWait = 2 * time.Minute

// wordAPI is the api with its files in the app's bucket, which the stack's
// worker may run an import from too, and the watch the worker runs over
// the same services.
func wordAPI(t *testing.T, h *harness) (*apiServer, *wordio.Watch) {
	t.Helper()
	store := h.appStore(t)
	var svc *wordio.Service
	api := newAPIServer(t, h, func(s *httpapi.Server) {
		s.Attachments = attachment.NewService(h.cluster, store, s.Pages).WithMaxSize(testUploadLimit).WithLogger(discard())
		svc = wordio.NewService(h.cluster, s.Pages, s.Attachments, store)
		s.Word = svc
	})
	return api, wordio.NewWatch(svc, discard(), time.Hour)
}

// wordDoc is a Word document written by the export from blocks, with the
// picture given wherever an image names any file.
func wordDoc(t *testing.T, title string, picture []byte, blocks ...any) []byte {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"type": "doc", "content": blocks})
	body, err := document.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	data, err := docx.Bytes(context.Background(), docx.Page{ID: uuid.New(), Title: title, Language: "en", Body: body},
		docx.Reader{Picture: func(context.Context, uuid.UUID) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(picture)), nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func wText(s string) map[string]any { return map[string]any{"type": "text", "text": s} }
func wPara(s string) map[string]any {
	return map[string]any{"type": "paragraph", "content": []any{wText(s)}}
}
func wNode(typ string, attrs map[string]any, content ...any) map[string]any {
	n := map[string]any{"type": typ}
	if attrs != nil {
		n["attrs"] = attrs
	}
	if len(content) > 0 {
		n["content"] = content
	}
	return n
}

func wordZip(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(data)
	}
	_ = z.Close()
	return buf.Bytes()
}

// followWordImport runs the worker's watch until the import is finished,
// and answers what GET /word-imports/{id} then says.
func followWordImport(t *testing.T, c *client, watch *wordio.Watch, id string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(wordWait)
	for {
		if _, err := watch.Once(context.Background()); err != nil {
			t.Logf("the suite's watch: %v", err)
		}
		job := obj(t, want(t, c.get(t, "/api/v1/word-imports/"+id), http.StatusOK, "follow the import"), "job")
		if job["state"] != "queued" && job["state"] != "running" {
			return job
		}
		if time.Now().After(deadline) {
			t.Fatalf("the import did not finish within %s: %v", wordWait, job)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestWordDocumentsAreImported(t *testing.T) {
	h := newHarness(t)
	api, watch := wordAPI(t, h)
	home := h.makeMember(t, "word-import")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	memberID := h.addPerson(t, home.org, "member")
	member := api.as(t, memberID, home.org, slug)
	readerID := h.addPerson(t, home.org, "member")
	reader := api.as(t, readerID, home.org, slug)

	docs := newTree(t, owner, "WORD", "Word")
	picture := pngOf(t, 6, 4)
	guide := wordDoc(t, "Release guide", picture,
		wNode("heading", map[string]any{"level": 1}, wText("Steps")),
		wNode("bulletList", nil, wNode("listItem", nil, wPara("Build")), wNode("listItem", nil, wPara("Ship"))),
		wNode("table", nil,
			wNode("tableRow", nil, wNode("tableHeader", map[string]any{"colspan": 1, "rowspan": 1}, wPara("Who")), wNode("tableHeader", map[string]any{"colspan": 1, "rowspan": 1}, wPara("What"))),
			wNode("tableRow", nil, wNode("tableCell", map[string]any{"colspan": 1, "rowspan": 1}, wPara("Ada")), wNode("tableCell", map[string]any{"colspan": 1, "rowspan": 1}, wPara("Tests")))),
		wNode("image", map[string]any{"attachmentId": uuid.NewString(), "alt": "The release train"}),
	)

	t.Run("one document becomes a published page with its picture as a file", func(t *testing.T) {
		got := obj(t, want(t, member.importFiles(t, http.MethodPost, pagePath(docs.homeID, "/import/docx"), [2]string{"guide.docx", string(guide)}), http.StatusCreated, "import a document"))
		pages := got["pages"].([]any)
		if len(pages) != 1 {
			t.Fatalf("the import made %v", pages)
		}
		made := pages[0].(map[string]any)
		if made["title"] != "Release guide" || made["parentId"] != docs.homeID {
			t.Errorf("the page made is %v", made)
		}
		id := made["id"].(string)
		read := obj(t, want(t, reader.get(t, pagePath(id)), http.StatusOK, "another member reads the page"), "page")
		if read["version"].(float64) != 1 || read["unpublished"] == true {
			t.Errorf("the imported page is not published: %v", read)
		}
		files := list(t, want(t, member.get(t, pagePath(id, "/attachments")), http.StatusOK, "the page's files"), "attachments")
		if len(files) != 1 {
			t.Fatalf("the page's files: %v", files)
		}
		body := fmt.Sprint(read["body"])
		for _, want := range []string{"heading", "bulletList", "tableHeader", "image", files[0].(map[string]any)["id"].(string), "The release train"} {
			if !strings.Contains(body, want) {
				t.Errorf("the page lacks %s: %s", want, body)
			}
		}
	})

	t.Run("what cannot be read is refused in a sentence naming it", func(t *testing.T) {
		for name, data := range map[string]string{"notes.docx": "plain words", "old.docx": "\xD0\xCF\x11\xE0 an old document"} {
			got := member.importFiles(t, http.MethodPost, pagePath(docs.homeID, "/import/docx"), [2]string{name, data})
			if got.Status != http.StatusUnprocessableEntity || !strings.Contains(string(got.Raw), name) {
				t.Errorf("%s: %d %s", name, got.Status, got.Raw)
			}
		}
		if got := member.importFiles(t, http.MethodPost, pagePath(docs.homeID, "/import/docx"), [2]string{"a.md", "# A"}); got.Status != http.StatusUnprocessableEntity {
			t.Errorf("a Markdown file: %d %s", got.Status, got.Raw)
		}
		if got := member.importFiles(t, http.MethodPost, pagePath(docs.homeID, "/import/docx"), [2]string{"a.docx", string(guide)}, [2]string{"b.docx", string(guide)}); got.Status != http.StatusUnprocessableEntity {
			t.Errorf("two documents at once: %d %s", got.Status, got.Raw)
		}
	})

	t.Run("several documents and a folder are made pages by the worker", func(t *testing.T) {
		archive := wordZip(t, map[string][]byte{
			"handbook/intro.docx":       wordDoc(t, "Introduction", picture, wPara("Welcome.")),
			"handbook/team/people.docx": wordDoc(t, "People", picture, wPara("Ada and Grace.")),
			"handbook/logo.png":         picture,
		})
		queued := obj(t, want(t, member.importFiles(t, http.MethodPost, pagePath(docs.homeID, "/word-imports"),
			[2]string{"plan.docx", string(wordDoc(t, "Plan", picture, wPara("The plan.")))},
			[2]string{"broken.docx", "not a document"},
			[2]string{"handbook.zip", string(archive)},
		), http.StatusAccepted, "queue the import"), "job")
		if queued["state"] != "queued" {
			t.Errorf("the import is %v", queued)
		}
		if got := reader.get(t, "/api/v1/word-imports/"+queued["id"].(string)); got.Status != http.StatusNotFound {
			t.Errorf("another member follows the import: %d", got.Status)
		}
		job := followWordImport(t, member, watch, queued["id"].(string))
		if job["state"] != "done" || job["done"].(float64) != 6 || job["total"].(float64) != 6 {
			t.Fatalf("the import ended as %v", job)
		}
		if fmt.Sprint(job["skipped"]) != "[handbook/logo.png]" {
			t.Errorf("the files passed over are %v", job["skipped"])
		}
		byPath := map[string]map[string]any{}
		for _, f := range job["files"].([]any) {
			byPath[f.(map[string]any)["path"].(string)] = f.(map[string]any)
		}
		if byPath["broken.docx"]["page"] != nil || !strings.Contains(fmt.Sprint(byPath["broken.docx"]["error"]), "not a Word document") {
			t.Errorf("the broken file is reported as %v", byPath["broken.docx"])
		}
		plan, handbook, people := byPath["plan.docx"]["page"], byPath["handbook/"]["page"], byPath["handbook/team/people.docx"]["page"]
		team := byPath["handbook/team/"]["page"]
		if plan == nil || handbook == nil || people == nil || team == nil {
			t.Fatalf("the report is %v", job["files"])
		}
		if team.(map[string]any)["parentId"] != handbook.(map[string]any)["id"] || people.(map[string]any)["parentId"] != team.(map[string]any)["id"] {
			t.Errorf("the folders did not become the tree: %v", job["files"])
		}
		h.settle(t)
		peopleID := people.(map[string]any)["id"].(string)
		read := obj(t, want(t, reader.get(t, pagePath(peopleID)), http.StatusOK, "another member reads an imported page"), "page")
		if read["title"] != "People" || !strings.Contains(fmt.Sprint(read["body"]), "Ada and Grace.") {
			t.Errorf("the page is %v", read)
		}
		var stored int
		if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM attachment WHERE page_id = $1`, peopleID).Scan(&stored); err != nil || stored != 0 {
			t.Errorf("a page without pictures holds %d files: %v", stored, err)
		}
	})

	t.Run("a reader is refused, and so is a stranger", func(t *testing.T) {
		reading := newTree(t, owner, "WORDREAD", "Read only")
		readPage := reading.add(reading.homeID, "Read me")
		want(t, owner.put(t, "/api/v1/spaces/WORDREAD/permissions", map[string]any{"grants": []any{
			map[string]any{"subject": everyone, "permissions": []any{"view"}},
		}}), http.StatusOK, "WORDREAD is read only")
		h.settle(t)
		doc := [2]string{"guide.docx", string(guide)}
		if got := reader.importFiles(t, http.MethodPost, pagePath(readPage, "/import/docx"), doc); got.Status != http.StatusForbidden {
			t.Errorf("an import where the caller may only read: %d %s", got.Status, got.Raw)
		}
		if got := reader.importFiles(t, http.MethodPost, pagePath(readPage, "/word-imports"), doc); got.Status != http.StatusForbidden {
			t.Errorf("a queued import where the caller may only read: %d %s", got.Status, got.Raw)
		}
		nobody := api.anonymous()
		if got := nobody.importFiles(t, http.MethodPost, pagePath(readPage, "/import/docx"), doc); got.Status != http.StatusUnauthorized {
			t.Errorf("an import by nobody: %d", got.Status)
		}
		away := h.makeMember(t, "word-away")
		stranger := api.as(t, away.user, away.org, h.slugOf(t, away.org))
		if got := stranger.importFiles(t, http.MethodPost, pagePath(readPage, "/word-imports"), doc); got.Status != http.StatusNotFound {
			t.Errorf("an import by somebody of another organization: %d", got.Status)
		}
	})

	t.Run("too many documents are refused before anything is stored", func(t *testing.T) {
		var files [][2]string
		for i := range wordio.MaxFiles + 1 {
			files = append(files, [2]string{fmt.Sprintf("d%d.docx", i), "x"})
		}
		got := member.importFiles(t, http.MethodPost, pagePath(docs.homeID, "/word-imports"), files...)
		if got.Status != http.StatusUnprocessableEntity || !strings.Contains(string(got.Raw), "more than 50") {
			t.Errorf("51 documents: %d %s", got.Status, got.Raw)
		}
	})
}

// queueWordImport stores an upload and queues its import straight in the
// database, as the request would have, for what a request no longer allows.
func queueWordImport(t *testing.T, h *harness, org, requester uuid.UUID, parent string, extra string, args []any, docs map[string][]byte) uuid.UUID {
	t.Helper()
	id := uuid.New()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for name, data := range docs {
		w, err := z.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(data)
	}
	_ = z.Close()
	if err := h.appStore(t).Put(context.Background(), wordio.ObjectKey(org, id), bytes.NewReader(buf.Bytes()), int64(buf.Len()), "application/zip"); err != nil {
		t.Fatal(err)
	}
	all := append([]any{id, org, requester, parent, len(docs), buf.Len()}, args...)
	if _, err := h.super.Exec(context.Background(), `INSERT INTO word_import (id, org_id, requested_by, parent_id, file_count, size_bytes`+extra, all...); err != nil {
		t.Fatal(err)
	}
	// Planted as the superuser, the row is nobody's own write, so the
	// requester's first look may reach a replica that has not replayed it.
	h.settle(t)
	return id
}

// A requester who may no longer add pages fails the import, which leaves
// nothing behind; a worker that died part way leaves its import to the
// next, which trashes what was made and begins again.
func TestAWordImportThatCannotFinishLeavesNothing(t *testing.T) {
	h := newHarness(t)
	api, watch := wordAPI(t, h)
	home := h.makeMember(t, "word-fail")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	memberID := h.addPerson(t, home.org, "member")
	member := api.as(t, memberID, home.org, slug)
	ctx := context.Background()

	t.Run("a requester who may no longer add pages", func(t *testing.T) {
		docs := newTree(t, owner, "WORDFAIL", "Failing")
		parent := docs.add(docs.homeID, "Imports")
		want(t, owner.put(t, "/api/v1/spaces/WORDFAIL/permissions", map[string]any{"grants": []any{
			map[string]any{"subject": everyone, "permissions": []any{"view"}},
		}}), http.StatusOK, "WORDFAIL is read only")
		h.settle(t)
		id := queueWordImport(t, h, home.org, memberID, parent, `) VALUES ($1, $2, $3, $4, $5, $6)`, nil,
			map[string][]byte{"a.docx": wordDoc(t, "A", nil, wPara("a"))})
		job := followWordImport(t, member, watch, id.String())
		if job["state"] != "failed" || job["failure"] != "forbidden" || !strings.Contains(fmt.Sprint(job["message"]), "Ask an administrator") {
			t.Fatalf("the import ended as %v", job)
		}
		var live int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM page WHERE parent_id = $1 AND trashed_at IS NULL`, parent).Scan(&live); err != nil || live != 0 {
			t.Errorf("the failed import left %d pages: %v", live, err)
		}
		if rc, err := h.appStore(t).Get(ctx, wordio.ObjectKey(home.org, id)); err == nil {
			_ = rc.Close()
			t.Errorf("the failed import's upload is still stored")
		}
	})

	t.Run("a worker that died part way", func(t *testing.T) {
		docs := newTree(t, owner, "WORDDIED", "Died")
		docs.c = member
		leftover := docs.add(docs.homeID, "Half made")
		id := queueWordImport(t, h, home.org, memberID, docs.homeID,
			`, state, attempts, lease_until, made) VALUES ($1, $2, $3, $4, $5, $6, 'running', 1, now() - interval '1 minute', $7)`,
			[]any{[]uuid.UUID{uuid.MustParse(leftover)}},
			map[string][]byte{"again.docx": wordDoc(t, "Again", nil, wPara("again"))})
		job := followWordImport(t, member, watch, id.String())
		if job["state"] != "done" || job["done"].(float64) != 1 {
			t.Fatalf("the import ended as %v", job)
		}
		var trashed bool
		if err := h.super.QueryRow(ctx, `SELECT trashed_at IS NOT NULL FROM page WHERE id = $1`, leftover).Scan(&trashed); err != nil || !trashed {
			t.Errorf("what the dead worker made is still in the tree: %v", err)
		}
		var titles []string
		if err := h.super.QueryRow(ctx, `SELECT array_agg(title) FROM page WHERE parent_id = $1 AND trashed_at IS NULL`, docs.homeID).Scan(&titles); err != nil || fmt.Sprint(titles) != "[Again]" {
			t.Errorf("the pages under the home are %v: %v", titles, err)
		}
	})
}

// Straight through SQL as stator_app: an import is queued only by whoever
// may add pages under its parent, for themselves and as one still to run;
// what it then does is the worker's alone to write, and only its requester
// reads it.
func TestTheDatabaseKeepsWordImportsTheirImporters(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "word-sql")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	memberID := h.addPerson(t, home.org, "member")
	readerID := h.addPerson(t, home.org, "member")
	open := newTree(t, owner, "WORDSQL", "Open")
	reading := newTree(t, owner, "WORDRO", "Read only")
	want(t, owner.put(t, "/api/v1/spaces/WORDRO/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": everyone, "permissions": []any{"view"}},
	}}), http.StatusOK, "WORDRO is read only")
	h.settle(t)
	ctx := context.Background()
	insert := `INSERT INTO word_import (id, org_id, requested_by, parent_id, file_count, size_bytes) VALUES ($1, $2, $3, $4, 1, 10)`

	conn := appConn(t)
	actAs(t, conn, home.org, readerID)
	refused(t, conn, "a member queuing an import where they may only read", insert, uuid.New(), home.org, readerID, reading.homeID)
	actAs(t, conn, home.org, memberID)
	refused(t, conn, "a member queuing an import for somebody else", insert, uuid.New(), home.org, readerID, open.homeID)
	refused(t, conn, "a member queuing an import already done",
		`INSERT INTO word_import (id, org_id, requested_by, parent_id, file_count, size_bytes, state) VALUES ($1, $2, $3, $4, 1, 10, 'done')`, uuid.New(), home.org, memberID, open.homeID)
	refused(t, conn, "a member queuing an import of no documents",
		`INSERT INTO word_import (id, org_id, requested_by, parent_id, file_count, size_bytes) VALUES ($1, $2, $3, $4, 0, 10)`, uuid.New(), home.org, memberID, open.homeID)
	job := uuid.New()
	if _, err := conn.Exec(ctx, insert, job, home.org, memberID, open.homeID); err != nil {
		t.Fatalf("a member who may add pages cannot queue an import through SQL: %v", err)
	}
	for what, sql := range map[string]string{
		"marking it done":       `UPDATE word_import SET state = 'done' WHERE id = $1`,
		"writing its report":    `UPDATE word_import SET report = '[]' WHERE id = $1`,
		"naming pages it made":  `UPDATE word_import SET made = '{}' WHERE id = $1`,
		"deleting it":           `DELETE FROM word_import WHERE id = $1`,
		"moving it to a parent": `UPDATE word_import SET parent_id = parent_id WHERE id = $1`,
	} {
		refused(t, conn, "the requester "+what, sql, job)
	}
	var seen int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM word_import WHERE id = $1`, job).Scan(&seen); err != nil || seen != 1 {
		t.Errorf("the requester reads %d of their import: %v", seen, err)
	}
	actAs(t, conn, home.org, home.user)
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM word_import WHERE id = $1`, job).Scan(&seen); err != nil || seen != 0 {
		t.Errorf("an administrator reads %d of another's import: %v", seen, err)
	}
	if _, err := h.super.Exec(ctx, `DELETE FROM word_import WHERE id = $1`, job); err != nil {
		t.Fatal(err)
	}
}
