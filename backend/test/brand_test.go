//go:build integration

package test

import (
	"bytes"
	"context"
	"net/http"
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
