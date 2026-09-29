//go:build integration

package test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/objectstore"
	"github.com/praetorianer777/stator/backend/internal/oidc"
	"github.com/praetorianer777/stator/backend/internal/secret"
	"github.com/praetorianer777/stator/backend/internal/seed"
	"github.com/praetorianer777/stator/backend/internal/testorg"
	"github.com/praetorianer777/stator/backend/internal/theme"
)

const suiteTestToken = "the integration suite's test token"

// startTestOrgAPI is the api as the compose stack runs it for the browser
// suite: sign-in, themes, the real bucket and, when on, the test endpoints.
func (h *harness) startTestOrgAPI(t *testing.T, kc *keycloak, on bool) (*api, objectstore.Store) {
	t.Helper()
	key := make([]byte, secret.KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	box, err := secret.New(key)
	if err != nil {
		t.Fatal(err)
	}
	store := newAPIServer(t, h).store
	server := httptest.NewUnstartedServer(nil)
	base := "http://" + server.Listener.Addr().String()
	callback := base + httpapi.APIPrefix + "/auth/oidc/callback"
	a := &api{
		URL:      base,
		accounts: auth.NewService(h.cluster, cheapPasswords(), testSessionTTL),
		sso:      oidc.NewService(h.cluster, box, callback),
		box:      box,
	}
	s := &httpapi.Server{
		DB: h.cluster, Fresh: h.freshness(t), Auth: a.accounts, Accounts: a.accounts, OIDC: a.sso,
		OIDCCallbackURL: callback, Themes: theme.NewService(h.cluster, store), Log: discard(),
		CookieName: h.cfg.Auth.SessionCookie, AppBaseURL: "http://" + appHost,
	}
	if on {
		s.TestOrgs = testorg.NewService(h.cluster, a.accounts, a.sso, store, seed.People{
			Members: []seed.Member{{Email: "alice@stator.test", Role: auth.RoleAdmin}, {Email: "bob@stator.test", Role: auth.RoleMember}},
			Provider: &oidc.Provider{Issuer: kc.issuer(), ClientID: statorClient, ClientSecret: statorSecret,
				CreateGroups: true, Enabled: true},
		})
		s.TestToken = suiteTestToken
	}
	server.Config.Handler = s.Routes(nil)
	server.Start()
	t.Cleanup(server.Close)
	return a, store
}

// testCall sends one request to the test endpoints with the token given.
func testCall(t *testing.T, a *api, method, path, token, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, a.URL+httpapi.APIPrefix+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set(httpapi.TestTokenHeader, token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

// sendJSON makes a request as the browser, with its session cookie.
func (b *browser) sendJSON(t *testing.T, method, target, contentType string, body io.Reader) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, target, body)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := b.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

// orgColumns names every column that points at an organization: each foreign
// key to org, and every column called org_id whether or not it has one.
func (h *harness) orgColumns(t *testing.T) [][2]string {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT c.conrelid::regclass::text, a.attname
		FROM pg_constraint c
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = ANY (c.conkey)
		WHERE c.contype = 'f' AND c.confrelid = 'public.org'::regclass AND cardinality(c.conkey) = 1
		UNION
		SELECT table_name::text, column_name::text FROM information_schema.columns
		WHERE table_schema = 'public' AND column_name = 'org_id'
		ORDER BY 1, 2`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out [][2]string
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			t.Fatal(err)
		}
		out = append(out, [2]string{table, column})
	}
	if len(out) == 0 {
		t.Fatal("no column points at an organization; the query is wrong")
	}
	return out
}

// rowsNaming counts, per table and column, the rows that name the organization.
func (h *harness) rowsNaming(t *testing.T, org uuid.UUID) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, tc := range h.orgColumns(t) {
		var n int
		sql := `SELECT count(*) FROM ` + quoteIdent(tc[0]) + ` WHERE ` + quoteIdent(tc[1]) + ` = $1`
		if err := h.super.QueryRow(context.Background(), sql, org).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		out[tc[0]+"."+tc[1]] = n
	}
	var n int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM org WHERE id = $1`, org).Scan(&n); err != nil {
		t.Fatal(err)
	}
	out["org.id"] = n
	return out
}

func quoteIdent(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

// An organization made by the endpoint is signed in to, used and removed, and
// nothing of it is left in any table or in the bucket.
func TestThrowawayOrganizations(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	kc.reachable(t)
	a, store := h.startTestOrgAPI(t, kc, true)
	ctx := context.Background()

	t.Run("a call without the token is refused", func(t *testing.T) {
		for _, token := range []string{"", "not the token at all"} {
			if status, body := testCall(t, a, http.MethodPost, "/test/orgs", token, `{}`); status != http.StatusUnauthorized {
				t.Errorf("create with token %q = %d %v, want 401", token, status, body)
			}
			if status, _ := testCall(t, a, http.MethodDelete, "/test/orgs/"+seed.DemoOrgSlug, token, ""); status != http.StatusUnauthorized {
				t.Errorf("delete with token %q = %d, want 401", token, status)
			}
		}
	})

	status, made := testCall(t, a, http.MethodPost, "/test/orgs", suiteTestToken, `{"label":"Integration"}`)
	if status != http.StatusCreated {
		t.Fatalf("create = %d %v", status, made)
	}
	slug, _ := made["slug"].(string)
	orgID, err := uuid.Parse(made["id"].(string))
	if err != nil || !strings.HasPrefix(slug, "integration-") {
		t.Fatalf("made %v, want a slug starting with the label and an id", made)
	}
	// The endpoint may fail the test halfway; the organization goes anyway.
	t.Cleanup(func() { testCall(t, a, http.MethodDelete, "/test/orgs/"+slug, suiteTestToken, "") })

	t.Run("alice and bob are let in and the provider is set", func(t *testing.T) {
		rows, err := h.super.Query(ctx, `
			SELECT u.email, m.org_role FROM org_member m JOIN app_user u ON u.id = m.user_id
			WHERE m.org_id = $1 ORDER BY u.email`, orgID)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for rows.Next() {
			var email, role string
			_ = rows.Scan(&email, &role)
			got = append(got, email+"="+role)
		}
		rows.Close()
		if strings.Join(got, ",") != "alice@stator.test=admin,bob@stator.test=member" {
			t.Errorf("members = %v", got)
		}
		var issuer string
		if err := h.super.QueryRow(ctx, `SELECT issuer FROM oidc_provider WHERE org_id = $1 AND enabled`, orgID).Scan(&issuer); err != nil || issuer != kc.issuer() {
			t.Errorf("provider issuer = %q, %v", issuer, err)
		}
	})

	alice := newBrowser(t)
	bob := newBrowser(t)
	var themeID, assetID string
	t.Run("alice and bob sign in and alice leaves a theme with a file", func(t *testing.T) {
		alice.signIn(t, a, slug, "alice", alicePassword, "")
		bob.signIn(t, a, slug, "bob", bobPassword, "")
		for who, b := range map[string]*browser{"admin": alice, "member": bob} {
			status, me := b.me(t, a)
			current, _ := me["organization"].(map[string]any)
			if status != http.StatusOK || current["slug"] != slug || current["role"] != who {
				t.Fatalf("/auth/me = %d %v, want %s as %s", status, me, slug, who)
			}
		}

		status, made := alice.sendJSON(t, http.MethodPost, a.URL+httpapi.APIPrefix+"/themes", "application/json", strings.NewReader(`{"name":"Throwaway","shared":true}`))
		if status != http.StatusCreated {
			t.Fatalf("make a theme = %d %v", status, made)
		}
		themeID = made["theme"].(map[string]any)["id"].(string)
		// Bob reads what alice wrote, which only the replica catching up promises.
		h.settle(t)
		if status, chose := bob.sendJSON(t, http.MethodPut, a.URL+httpapi.APIPrefix+"/themes/active", "application/json", strings.NewReader(`{"themeId":"`+themeID+`"}`)); status != http.StatusOK {
			t.Fatalf("bob chooses the theme = %d %v", status, chose)
		}
		if status, set := alice.sendJSON(t, http.MethodPut, a.URL+httpapi.APIPrefix+"/themes/default", "application/json", strings.NewReader(`{"themeId":"`+themeID+`"}`)); status != http.StatusOK {
			t.Fatalf("make it the default = %d %v", status, set)
		}

		var form bytes.Buffer
		mp := multipart.NewWriter(&form)
		part, _ := mp.CreateFormFile("file", "square.svg")
		_, _ = part.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16"><path d="M2 2h12v12H2z"/></svg>`))
		_ = mp.Close()
		status, uploaded := alice.sendJSON(t, http.MethodPost, a.URL+httpapi.APIPrefix+"/themes/"+themeID+"/assets", mp.FormDataContentType(), &form)
		if status != http.StatusCreated {
			t.Fatalf("upload = %d %v", status, uploaded)
		}
		assetID = uploaded["asset"].(map[string]any)["id"].(string)
		if keys, err := store.List(ctx, objectstore.OrgPrefix(orgID)); err != nil || len(keys) != 1 ||
			keys[0] != theme.ObjectKey(orgID, uuid.MustParse(themeID), uuid.MustParse(assetID)) {
			t.Fatalf("the bucket holds %v under the organization (%v), want the one file", keys, err)
		}
	})

	t.Run("only a throwaway organization can be deleted", func(t *testing.T) {
		if status, body := testCall(t, a, http.MethodDelete, "/test/orgs/"+seed.DemoOrgSlug, suiteTestToken, ""); status != http.StatusNotFound {
			t.Fatalf("delete demo = %d %v, want 404", status, body)
		}
		var n int
		if err := h.super.QueryRow(ctx, `SELECT count(*) FROM org WHERE slug = $1`, seed.DemoOrgSlug).Scan(&n); err != nil || n != 1 {
			t.Fatalf("demo is gone: %d %v", n, err)
		}
	})

	before := h.rowsNaming(t, orgID)
	for _, table := range []string{"org.id", "org_member.org_id", "oidc_provider.org_id", "theme.org_id", "theme_asset.org_id", "user_theme.org_id", "user_session.proof_org_id"} {
		if before[table] == 0 {
			t.Errorf("before deleting, %s names the organization in no row, so the check after proves nothing there", table)
		}
	}

	t.Run("deleting leaves no row, no file and no session", func(t *testing.T) {
		if status, body := testCall(t, a, http.MethodDelete, "/test/orgs/"+slug, suiteTestToken, ""); status != http.StatusNoContent {
			t.Fatalf("delete = %d %v", status, body)
		}
		for column, n := range h.rowsNaming(t, orgID) {
			if n != 0 {
				t.Errorf("%s still names the deleted organization in %d rows", column, n)
			}
		}
		if keys, err := store.List(ctx, objectstore.OrgPrefix(orgID)); err != nil || len(keys) != 0 {
			t.Errorf("the bucket still holds %v under the organization (%v)", keys, err)
		}
		for _, b := range []*browser{alice, bob} {
			if status, _ := b.me(t, a); status != http.StatusUnauthorized {
				t.Errorf("a session proven in the deleted organization still answers /auth/me with %d", status)
			}
		}
		if status, _ := testCall(t, a, http.MethodDelete, "/test/orgs/"+slug, suiteTestToken, ""); status != http.StatusNotFound {
			t.Errorf("deleting twice = %d, want 404", status)
		}
	})
}

// Off, the endpoints are not there at all, token or no token.
func TestTheTestEndpointsAreNotFoundWhenOff(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	a, _ := h.startTestOrgAPI(t, kc, false)
	for _, token := range []string{"", suiteTestToken} {
		if status, body := testCall(t, a, http.MethodPost, "/test/orgs", token, `{}`); status != http.StatusNotFound {
			t.Errorf("create with the endpoints off = %d %v, want 404", status, body)
		}
		if status, _ := testCall(t, a, http.MethodDelete, "/test/orgs/"+seed.DemoOrgSlug, token, ""); status != http.StatusNotFound {
			t.Errorf("delete with the endpoints off = %d, want 404", status)
		}
	}
}
