//go:build integration

package test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
)

// The brand of an organization's exports: a logo and a footer line in each
// language, changed by administrators and read by everybody.

var pngLogo = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 64)...)

func TestTheBrandIsChangedByAdministratorsAndReadByMembers(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "brand")
	slug := h.slugOf(t, home.org)
	admin := api.as(t, home.user, home.org, slug)
	member := api.as(t, h.addPerson(t, home.org, "member"), home.org, slug)

	t.Run("a new organization has no brand", func(t *testing.T) {
		got := obj(t, want(t, member.get(t, "/api/v1/org/brand"), http.StatusOK, "read the brand"), "brand")
		footer, _ := got["footer"].(map[string]any)
		if got["logo"] != nil || footer["en"] != "" || footer["de"] != "" {
			t.Fatalf("a new organization has the brand %v", got)
		}
		if got := member.get(t, "/api/v1/org/brand/logo"); got.Status != http.StatusNotFound {
			t.Errorf("a logo that is not there answered %d", got.Status)
		}
	})

	t.Run("an administrator sets the footer and the logo, and a member reads both", func(t *testing.T) {
		want(t, admin.put(t, "/api/v1/org/brand/footer", map[string]any{"en": " Internal ", "de": "Intern"}), http.StatusOK, "set the footer")
		set := obj(t, want(t, admin.uploadWith(t, http.MethodPut, "/api/v1/org/brand/logo", "logo.png", pngLogo), http.StatusOK, "set the logo"), "brand")
		logo, _ := set["logo"].(map[string]any)
		if logo["contentType"] != "image/png" || logo["size"] != float64(len(pngLogo)) || logo["version"] != float64(1) {
			t.Fatalf("the logo reads %v", logo)
		}
		got := obj(t, want(t, member.get(t, "/api/v1/org/brand"), http.StatusOK, "the member reads it"), "brand")
		if footer, _ := got["footer"].(map[string]any); footer["en"] != "Internal" || footer["de"] != "Intern" {
			t.Errorf("the member reads the footer %v", footer)
		}
		resp, data := member.download(t, "/api/v1/org/brand/logo")
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" || !bytes.Equal(data, pngLogo) || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("the logo came as %d %s with %d bytes", resp.StatusCode, resp.Header.Get("Content-Type"), len(data))
		}
	})

	t.Run("a new upload is a new version and taking it away leaves none", func(t *testing.T) {
		again := obj(t, want(t, admin.uploadWith(t, http.MethodPut, "/api/v1/org/brand/logo", "logo.png", pngLogo), http.StatusOK, "replace the logo"), "brand")
		if logo, _ := again["logo"].(map[string]any); logo["version"] != float64(2) {
			t.Fatalf("the replaced logo reads %v", logo)
		}
		gone := obj(t, want(t, admin.delete(t, "/api/v1/org/brand/logo"), http.StatusOK, "take it away"), "brand")
		if gone["logo"] != nil {
			t.Errorf("the logo is still %v", gone["logo"])
		}
		if got := member.get(t, "/api/v1/org/brand/logo"); got.Status != http.StatusNotFound {
			t.Errorf("a removed logo answered %d", got.Status)
		}
	})

	t.Run("a member changes nothing", func(t *testing.T) {
		if got := member.put(t, "/api/v1/org/brand/footer", map[string]any{"en": "Mine", "de": ""}); got.Status != http.StatusForbidden {
			t.Errorf("a member set the footer: %d %s", got.Status, got.Raw)
		}
		if got := member.uploadWith(t, http.MethodPut, "/api/v1/org/brand/logo", "logo.png", pngLogo); got.Status != http.StatusForbidden {
			t.Errorf("a member set the logo: %d %s", got.Status, got.Raw)
		}
		if got := member.delete(t, "/api/v1/org/brand/logo"); got.Status != http.StatusForbidden {
			t.Errorf("a member took the logo away: %d %s", got.Status, got.Raw)
		}
	})

	t.Run("what is not a logo or not a footer is refused in a sentence", func(t *testing.T) {
		for what, data := range map[string][]byte{"an SVG": []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`), "text": []byte("logo"), "a script": []byte("<script>1</script>")} {
			if got := admin.uploadWith(t, http.MethodPut, "/api/v1/org/brand/logo", "logo.png", data); got.Status != http.StatusBadRequest {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
		if got := admin.uploadWith(t, http.MethodPut, "/api/v1/org/brand/logo", "big.png", append(append([]byte{}, pngLogo...), make([]byte, 3<<20)...)); got.Status != http.StatusRequestEntityTooLarge {
			t.Errorf("a 3 MB logo: %d", got.Status)
		}
		long := string(bytes.Repeat([]byte("x"), 201))
		for what, body := range map[string]map[string]any{
			"a line break": {"en": "one\ntwo", "de": ""},
			"too long":     {"en": "", "de": long},
		} {
			if got := admin.put(t, "/api/v1/org/brand/footer", body); got.Status != http.StatusUnprocessableEntity {
				t.Errorf("%s: %d %s", what, got.Status, got.Raw)
			}
		}
	})

	t.Run("setting it is in the audit log", func(t *testing.T) {
		if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = 'org.brand_set'`, home.org); n < 3 {
			t.Errorf("%d brand changes are in the log", n)
		}
	})
}

func TestTheBrandIsHeldByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "brand-db")
	away := h.makeMember(t, "brand-db-away")
	admin := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	api.as(t, away.user, away.org, h.slugOf(t, away.org))
	memberID := h.addPerson(t, home.org, "member")
	want(t, admin.put(t, "/api/v1/org/brand/footer", map[string]any{"en": "Internal", "de": ""}), http.StatusOK, "set the footer")

	conn := appConn(t)
	ctx := context.Background()
	actAs(t, conn, home.org, memberID)
	refused(t, conn, "a member changing the footer", `UPDATE org_brand SET footer_en = 'Mine' WHERE org_id = current_org_id()`)
	refused(t, conn, "a member removing the brand", `DELETE FROM org_brand WHERE org_id = current_org_id()`)

	actAs(t, conn, home.org, home.user)
	refused(t, conn, "a footer over 200 characters", `UPDATE org_brand SET footer_en = repeat('x', 201) WHERE org_id = current_org_id()`)
	refused(t, conn, "a logo that is no picture", `UPDATE org_brand SET logo_type = 'image/svg+xml', logo_size = 10 WHERE org_id = current_org_id()`)
	refused(t, conn, "a logo with no size", `UPDATE org_brand SET logo_type = 'image/png' WHERE org_id = current_org_id()`)
	refused(t, conn, "another organization's brand", `INSERT INTO org_brand (org_id) VALUES ($1)`, away.org)
	if _, err := conn.Exec(ctx, `UPDATE org_brand SET footer_de = 'Intern' WHERE org_id = current_org_id()`); err != nil {
		t.Fatalf("an administrator could not change the footer: %v", err)
	}
}

func TestAnonymousReadersSeeTheBrandOfAnOpenOrganization(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "brand-public")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	anon := api.anonymous()
	base := "/api/v1/public/" + slug

	open := newTree(t, owner, "OPEN", "Open handbook")
	open.add(open.homeID, "Guide")
	want(t, owner.put(t, "/api/v1/spaces/OPEN/anonymous-access", map[string]any{"view": true}), http.StatusOK, "open the space")
	want(t, owner.put(t, "/api/v1/org/anonymous-access", map[string]any{"enabled": true}), http.StatusOK, "open the organization")

	site := func() map[string]any {
		return obj(t, want(t, anon.get(t, base), http.StatusOK, "the public site"), "site")
	}
	if got := site(); got["logoVersion"] != nil || got["footer"].(map[string]any)["en"] != "" {
		t.Fatalf("a site with no brand reads %v", got)
	}
	if got := anon.get(t, base+"/logo"); got.Status != http.StatusNotFound {
		t.Errorf("a logo that is not there answered %d", got.Status)
	}

	want(t, owner.put(t, "/api/v1/org/brand/footer", map[string]any{"en": "Internal", "de": "Intern"}), http.StatusOK, "set the footer")
	want(t, owner.uploadWith(t, http.MethodPut, "/api/v1/org/brand/logo", "logo.png", pngLogo), http.StatusOK, "set the logo")
	got := site()
	if got["logoVersion"] != float64(1) || got["footer"].(map[string]any)["de"] != "Intern" {
		t.Fatalf("a site with a brand reads %v", got)
	}
	resp, data := anon.download(t, base+"/logo")
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/png" || !bytes.Equal(data, pngLogo) {
		t.Errorf("anybody reads the logo as %d %s with %d bytes", resp.StatusCode, resp.Header.Get("Content-Type"), len(data))
	}

	t.Run("a public link shows the brand too", func(t *testing.T) {
		page := open.add(open.homeID, "Linked")
		made := want(t, owner.post(t, pagePath(page, "/public-links"), map[string]any{"label": "For the board"}), http.StatusCreated, "make a link")
		token, _ := made.Body["token"].(string)
		if token == "" {
			t.Fatalf("the link carries no token: %v", made.Body)
		}
		linked := obj(t, want(t, anon.get(t, base+"/links/"+token), http.StatusOK, "open the link"), "site")
		if linked["logoVersion"] != float64(1) || linked["footer"].(map[string]any)["en"] != "Internal" {
			t.Errorf("a link's site reads %v", linked)
		}
		resp, data := anon.download(t, base+"/links/"+token+"/logo")
		if resp.StatusCode != http.StatusOK || !bytes.Equal(data, pngLogo) {
			t.Errorf("the link's logo came as %d with %d bytes", resp.StatusCode, len(data))
		}
		if got := anon.get(t, base+"/links/"+strings.Repeat("x", len(token))+"/logo"); got.Status != http.StatusNotFound {
			t.Errorf("a link nobody holds showed the logo: %d", got.Status)
		}
	})

	want(t, owner.put(t, "/api/v1/org/anonymous-access", map[string]any{"enabled": false}), http.StatusOK, "close the organization")
	if got := anon.get(t, base+"/logo"); got.Status != http.StatusNotFound {
		t.Errorf("a closed organization's logo answered %d", got.Status)
	}
}

func TestAWordExportCarriesTheBrand(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "brand-word")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	anon := api.anonymous()

	docs := newTree(t, owner, "BRW", "Branded handbook")
	guide := docs.add(docs.homeID, "Branded guide")
	publishBody(t, owner, guide, map[string]any{"type": "doc", "content": []any{plainPara("Printed with our brand.")}})
	h.settle(t)

	var orgName string
	if err := h.super.QueryRow(context.Background(), `SELECT name FROM org WHERE id = $1`, home.org).Scan(&orgName); err != nil {
		t.Fatal(err)
	}

	t.Run("an organization with no brand gets its name in the header", func(t *testing.T) {
		resp, data := owner.download(t, pagePath(guide, "/docx"))
		parts := wordFile(t, resp, data, "the plain export")
		if !strings.Contains(parts["word/header1.xml"], orgName) || strings.Contains(parts["word/header1.xml"], "<w:drawing>") {
			t.Errorf("the plain header reads %s", parts["word/header1.xml"])
		}
	})

	t.Run("the logo, the footer line in the reader's language and the accent are in the file", func(t *testing.T) {
		accentTheme := idOf(t, want(t, owner.post(t, "/api/v1/themes", map[string]any{"name": "House", "spec": map[string]any{"colors": map[string]any{"light": map[string]string{"accent": "#336699"}}}}), http.StatusCreated, "make a theme"), "theme")
		want(t, owner.patch(t, "/api/v1/themes/"+accentTheme, map[string]any{"shared": true}), http.StatusOK, "share it")
		want(t, owner.put(t, "/api/v1/themes/default", map[string]any{"themeId": accentTheme}), http.StatusOK, "make it the default")
		want(t, owner.put(t, "/api/v1/org/brand/footer", map[string]any{"en": "Internal use only", "de": "Nur intern"}), http.StatusOK, "set the footer")
		want(t, owner.uploadWith(t, http.MethodPut, "/api/v1/org/brand/logo", "logo.png", pngOf(t, 80, 40)), http.StatusOK, "set the logo")

		resp, data := owner.download(t, pagePath(guide, "/docx"))
		parts := wordFile(t, resp, data, "the branded export")
		if !strings.Contains(parts["word/header1.xml"], "<w:drawing>") || !strings.Contains(parts["word/header1.xml"], orgName) {
			t.Errorf("the header reads %s", parts["word/header1.xml"])
		}
		if !strings.Contains(parts["word/footer1.xml"], "Internal use only") && !strings.Contains(parts["word/footer1.xml"], "Nur intern") {
			t.Errorf("the footer reads %s", parts["word/footer1.xml"])
		}
		if !strings.Contains(parts["word/styles.xml"], `w:color w:val="336699"`) {
			t.Error("the styles do not take the default theme's accent")
		}
		if parts["word/media/brandlogo.png"] == "" {
			t.Error("the logo is not in the file")
		}
	})

	t.Run("a public export carries it too", func(t *testing.T) {
		want(t, owner.put(t, "/api/v1/spaces/BRW/anonymous-access", map[string]any{"view": true}), http.StatusOK, "open the space")
		want(t, owner.put(t, "/api/v1/org/anonymous-access", map[string]any{"enabled": true}), http.StatusOK, "open the organization")
		resp, data := anon.download(t, "/api/v1/public/"+slug+"/pages/"+guide+"/docx")
		parts := wordFile(t, resp, data, "the public export")
		if !strings.Contains(parts["word/header1.xml"], "<w:drawing>") || !strings.Contains(parts["word/footer1.xml"], "Internal use only") {
			t.Errorf("the public export lacks the brand:\nheader %s\nfooter %s", parts["word/header1.xml"], parts["word/footer1.xml"])
		}
	})
}
