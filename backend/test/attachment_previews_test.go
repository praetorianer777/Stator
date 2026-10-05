//go:build integration

package test

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/attachment"
	"github.com/praetorianer777/stator/backend/internal/convert"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
)

// Previews of PDFs and office documents (#61), the documents converted by the
// stack's own conversion service, once each.

// docxOf is the smallest word processing document an office suite opens: one
// paragraph of text.
func docxOf(t *testing.T, text string) []byte {
	t.Helper()
	parts := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`,
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p></w:body></w:document>`,
	}
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, name := range []string{"[Content_Types].xml", "_rels/.rels", "word/document.xml"} {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(parts[name]))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// tinyPDF is a PDF as far as a type and a reader's first look go.
const tinyPDF = "%PDF-1.4\n1 0 obj << /Type /Catalog >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF\n"

// countingConverter is the stack's converter with a tally of the calls.
type countingConverter struct {
	inner convert.Converter
	calls atomic.Int32
}

func (c *countingConverter) ToPDF(ctx context.Context, name string, data []byte) ([]byte, error) {
	c.calls.Add(1)
	return c.inner.ToPDF(ctx, name, data)
}

func previewPath(id string) string { return "/api/v1/attachments/" + id + "/preview" }

func TestPreviewsShowPDFsAndConvertOfficeDocumentsOnce(t *testing.T) {
	h := newHarness(t)
	converter := &countingConverter{inner: convert.New(h.cfg.ConverterURL, attachment.MaxPreviewSize)}
	api := newAPIServer(t, h, func(s *httpapi.Server) { s.Attachments.WithConverter(converter) })
	home := h.makeMember(t, "previews")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	readerID := h.addPerson(t, home.org, "member")
	reader := api.as(t, readerID, home.org, slug)
	docs := newTree(t, owner, "PREV", "Previews")
	pageID := docs.add(docs.homeID, "Plans")
	want(t, owner.put(t, "/api/v1/spaces/PREV/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": user(home.user), "permissions": []any{"view", "addPages", "administer"}},
		map[string]any{"subject": user(readerID), "permissions": []any{"view"}},
	}}), http.StatusOK, "the reader only reads PREV")
	files := pagePath(pageID, "/attachments")

	upload := func(name, contentType string, data []byte) map[string]any {
		t.Helper()
		return obj(t, want(t, owner.uploadAs(t, files, name, contentType, data), http.StatusCreated, "upload "+name), "attachment")
	}
	pdf := upload("report.pdf", "application/pdf", []byte(tinyPDF))
	plan := upload("plan.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", docxOf(t, "Quarterly plan"))
	// What a client that names no type sends: the bytes sniff as a zip.
	notes := upload("Notes.ODT", "application/octet-stream", docxOf(t, "Notes"))
	text := upload("readme.txt", "text/plain", []byte("plain words"))
	// The reader has written nothing, so a replica may answer them.
	h.settle(t)

	t.Run("each file says how it is shown in place", func(t *testing.T) {
		for _, c := range []struct {
			file map[string]any
			want string
		}{{pdf, "pdf"}, {plan, "office"}, {notes, "office"}, {text, "none"}} {
			if c.file["preview"] != c.want {
				t.Errorf("%s previews as %v, want %s", c.file["fileName"], c.file["preview"], c.want)
			}
		}
		for _, f := range filesOf(t, reader, files) {
			if f["id"] == plan["id"] && f["preview"] != "office" {
				t.Errorf("the list shows the plan's preview as %v", f["preview"])
			}
		}
	})

	t.Run("a PDF is shown as it is, in place", func(t *testing.T) {
		resp, data := reader.download(t, previewPath(pdf["id"].(string)))
		if resp.StatusCode != http.StatusOK || string(data) != tinyPDF {
			t.Fatalf("the PDF previews %d %q", resp.StatusCode, data)
		}
		if got := resp.Header.Get("Content-Type"); got != "application/pdf" {
			t.Errorf("served as %q", got)
		}
		if got := resp.Header.Get("Content-Disposition"); got != `inline; filename=report.pdf` {
			t.Errorf("disposed as %q", got)
		}
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Error("the browser may sniff the preview")
		}
	})

	planID := plan["id"].(string)
	var converted []byte
	t.Run("an office document is converted by the first reader and kept", func(t *testing.T) {
		resp, data := reader.download(t, previewPath(planID))
		if resp.StatusCode != http.StatusOK || !bytes.HasPrefix(data, []byte("%PDF-")) {
			t.Fatalf("the plan previews %d %.200q", resp.StatusCode, data)
		}
		if got := resp.Header.Get("Content-Disposition"); got != `inline; filename=plan.pdf` {
			t.Errorf("disposed as %q", got)
		}
		converted = data
		if n := converter.calls.Load(); n != 1 {
			t.Fatalf("converted %d times", n)
		}
		key := "org/" + home.org.String() + "/preview/" + planID + ".pdf"
		if stored := inBucket(t, api.store, key); !bytes.Equal(stored, converted) {
			t.Errorf("the bucket keeps %d bytes under %s, want the %d served", len(stored), key, len(converted))
		}
		if n := h.countRows(t, `SELECT count(*) FROM attachment_preview WHERE attachment_id = $1 AND state = 'ready' AND size_bytes = $2`, planID, len(converted)); n != 1 {
			t.Errorf("%d ready previews recorded", n)
		}

		for _, who := range []*client{owner, reader} {
			resp, again := who.download(t, previewPath(planID))
			if resp.StatusCode != http.StatusOK || !bytes.Equal(again, converted) {
				t.Errorf("the kept preview reads %d, %d bytes", resp.StatusCode, len(again))
			}
		}
		if n := converter.calls.Load(); n != 1 {
			t.Errorf("a kept preview was converted again: %d calls", n)
		}
	})

	t.Run("a document named in capitals and sent untyped converts too", func(t *testing.T) {
		resp, data := owner.download(t, previewPath(notes["id"].(string)))
		if resp.StatusCode != http.StatusOK || !bytes.HasPrefix(data, []byte("%PDF-")) {
			t.Errorf("the notes preview %d %.200q", resp.StatusCode, data)
		}
		if got := resp.Header.Get("Content-Disposition"); got != `inline; filename=Notes.pdf` {
			t.Errorf("disposed as %q", got)
		}
	})

	t.Run("anything else has no preview, and nobody else's file is found", func(t *testing.T) {
		r := want(t, reader.get(t, previewPath(text["id"].(string))), http.StatusUnsupportedMediaType, "a text file's preview")
		if code := errorCode(t, r); code != "no_preview" {
			t.Errorf("answered %s", code)
		}
		want(t, reader.get(t, previewPath(uuid.NewString())), http.StatusNotFound, "a file that does not exist")
		other := h.makeMember(t, "previews-other")
		stranger := api.as(t, other.user, other.org, h.slugOf(t, other.org))
		want(t, stranger.get(t, previewPath(planID)), http.StatusNotFound, "another organization's preview")
		want(t, api.anonymous().get(t, previewPath(planID)), http.StatusUnauthorized, "a preview without a session")
	})

	t.Run("a new version is converted afresh", func(t *testing.T) {
		next := upload("plan.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", docxOf(t, "Revised plan"))
		h.settle(t)
		resp, _ := reader.download(t, previewPath(next["id"].(string)))
		if resp.StatusCode != http.StatusOK || converter.calls.Load() != 3 {
			t.Errorf("the second version previews %d after %d conversions", resp.StatusCode, converter.calls.Load())
		}
	})

	t.Run("the preview goes with its file", func(t *testing.T) {
		key := "org/" + home.org.String() + "/preview/" + planID + ".pdf"
		want(t, owner.delete(t, "/api/v1/attachments/"+planID), http.StatusNoContent, "delete the plan")
		if n := h.countRows(t, `SELECT count(*) FROM attachment_preview WHERE attachment_id = $1`, planID); n != 0 {
			t.Errorf("%d preview rows outlive the file", n)
		}
		if inBucket(t, api.store, key) != nil {
			t.Error("the preview's bytes outlive the file")
		}
		if n := h.tombstones(t, key); n != 0 {
			t.Errorf("%d tombstones left for a preview already removed", n)
		}
	})
}

func TestAPreviewThatCannotBeMadeSaysSo(t *testing.T) {
	h := newHarness(t)
	home := h.makeMember(t, "previews-down")
	slug := h.slugOf(t, home.org)

	// Nothing answers at this address, as when the service is down.
	gone := newAPIServer(t, h, func(s *httpapi.Server) {
		s.Attachments.WithConverter(convert.New("http://converter-gone.invalid:3000", attachment.MaxPreviewSize))
	})
	owner := gone.as(t, home.user, home.org, slug)
	docs := newTree(t, owner, "DOWN", "Down")
	files := pagePath(docs.add(docs.homeID, "Plans"), "/attachments")
	plan := obj(t, want(t, owner.upload(t, files, "plan.docx", docxOf(t, "Plan")), http.StatusCreated, "upload the plan"), "attachment")
	planID := plan["id"].(string)

	t.Run("a converter that does not answer is asked again next time", func(t *testing.T) {
		r := want(t, owner.get(t, previewPath(planID)), http.StatusServiceUnavailable, "the preview while the converter is down")
		if code := errorCode(t, r); code != "preview_unavailable" {
			t.Errorf("answered %s", code)
		}
		if n := h.countRows(t, `SELECT count(*) FROM attachment_preview WHERE attachment_id = $1`, planID); n != 0 {
			t.Errorf("an outage was remembered in %d rows", n)
		}
	})

	t.Run("without a converter office documents have no preview", func(t *testing.T) {
		off := newAPIServer(t, h, func(s *httpapi.Server) { s.Attachments.WithConverter(nil) })
		viewer := off.as(t, home.user, home.org, slug)
		// A browser of its own, with no write of its own to wait for.
		h.settle(t)
		for _, f := range filesOf(t, viewer, files) {
			if f["preview"] != "none" {
				t.Errorf("%s previews as %v with no converter", f["fileName"], f["preview"])
			}
		}
		r := want(t, viewer.get(t, previewPath(planID)), http.StatusServiceUnavailable, "the preview with no converter")
		if code := errorCode(t, r); code != "preview_off" {
			t.Errorf("answered %s", code)
		}
	})

	t.Run("a document the converter refuses is remembered as failed", func(t *testing.T) {
		working := newAPIServer(t, h)
		writer := working.as(t, home.user, home.org, slug)
		// Not a presentation at all: the suite opens it as nothing it can draw.
		junk := obj(t, want(t, writer.upload(t, files, "deck.pptx", []byte("\x00\x01not a presentation")), http.StatusCreated, "upload the junk"), "attachment")
		r := writer.get(t, previewPath(junk["id"].(string)))
		if r.Status == http.StatusOK {
			t.Skip("the converter made a PDF of the junk; there is nothing it refuses to try")
		}
		want(t, r, http.StatusUnprocessableEntity, "the junk's preview")
		if code := errorCode(t, r); code != "preview_failed" {
			t.Errorf("answered %s", code)
		}
		if n := h.countRows(t, `SELECT count(*) FROM attachment_preview WHERE attachment_id = $1 AND state = 'failed'`, junk["id"]); n != 1 {
			t.Errorf("%d failures remembered", n)
		}
		want(t, writer.get(t, previewPath(junk["id"].(string))), http.StatusUnprocessableEntity, "the junk's preview again")
	})
}

// The service refusing is not proof: straight through SQL, a preview is its
// file's, made only by who may see the file, and never changed.
func TestPreviewsAreGuardedByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "previews-rls")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID, carlID := h.addPerson(t, home.org, "member"), h.addPerson(t, home.org, "member")
	ann := api.as(t, annID, home.org, slug)
	docs := newTree(t, owner, "PRLS", "Guarded")
	open := docs.add(docs.homeID, "Open")
	hidden := docs.add(docs.homeID, "Hidden")
	want(t, restrict(t, ann, hidden, []any{user(annID)}, []any{user(annID)}), http.StatusOK, "ann hides a page")
	upload := func(c *client, page string) string {
		t.Helper()
		return obj(t, want(t, c.upload(t, pagePath(page, "/attachments"), "plan.docx", docxOf(t, "Plan")), http.StatusCreated, "upload"), "attachment")["id"].(string)
	}
	openFile, hiddenFile := upload(owner, open), upload(ann, hidden)
	want(t, owner.get(t, previewPath(openFile)), http.StatusOK, "preview the open file")
	h.settle(t)

	conn := appConn(t)
	insert := `INSERT INTO attachment_preview (attachment_id, org_id, state) VALUES ($1, $2, 'failed')`

	t.Run("somebody who cannot see the file can neither see nor make its preview", func(t *testing.T) {
		actAs(t, conn, home.org, carlID)
		denied(t, conn, "a preview of a hidden file", insert, hiddenFile, home.org)
		var n int
		if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM attachment_preview WHERE attachment_id = $1`, openFile).Scan(&n); err != nil || n != 1 {
			t.Errorf("carl reads %d previews of the open file (%v)", n, err)
		}
	})

	t.Run("a preview cannot be changed, removed or pointed elsewhere", func(t *testing.T) {
		actAs(t, conn, home.org, home.user)
		denied(t, conn, "marking it failed", `UPDATE attachment_preview SET state = 'failed', size_bytes = NULL WHERE attachment_id = $1`, openFile)
		denied(t, conn, "removing it", `DELETE FROM attachment_preview WHERE attachment_id = $1`, openFile)
		refused(t, conn, "a second preview of the file", insert, openFile, home.org)
		refused(t, conn, "naming its object", `INSERT INTO attachment_preview (attachment_id, org_id, state, object_key) VALUES ($1, $2, 'failed', 'org/elsewhere')`, hiddenFile, home.org)
		denied(t, conn, "a ready preview of no size", `INSERT INTO attachment_preview (attachment_id, org_id, state) VALUES ($1, $2, 'ready')`, hiddenFile, home.org)
	})

	t.Run("another organization reaches none of it", func(t *testing.T) {
		other := h.makeMember(t, "previews-rls-other")
		actAs(t, conn, other.org, other.user)
		denied(t, conn, "a preview in another organization", insert, hiddenFile, home.org)
		refused(t, conn, "a preview of another organization's file", insert, hiddenFile, other.org)
		var n int
		if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM attachment_preview`).Scan(&n); err != nil || n != 0 {
			t.Errorf("another organization reads %d previews (%v)", n, err)
		}
	})

	if n := h.countRows(t, `SELECT count(*) FROM attachment_preview WHERE attachment_id = $1`, hiddenFile); n != 0 {
		t.Errorf("%d previews of the hidden file were made", n)
	}
}
