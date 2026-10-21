//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/exciter"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/netguard"
	"github.com/praetorianer777/stator/backend/internal/secret"
)

// Connecting people to Exciter at sign-in (#462), as to Armature (#430):
// Stator and the stub's Exciter share the realm dynamo, and a sign-in
// through the stack's Keycloak trades the person's access token for an
// Exciter grant, with no token pasted.

// exciterControl steers the stub's OAuth server and /meta.
func exciterControl(t *testing.T, method, path string, body, out any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(method, os.Getenv(envExciterStub)+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("the stub answered %s %s with %d", method, path, resp.StatusCode)
	}
	if out != nil {
		_ = json.NewDecoder(resp.Body).Decode(out)
	}
}

type exciterGrant struct {
	Person    string `json:"person"`
	Refreshes int    `json:"refreshes"`
	Ended     bool   `json:"ended"`
}

func exciterGrants(t *testing.T, tenant, person string) []exciterGrant {
	t.Helper()
	var out struct {
		Grants []exciterGrant `json:"grants"`
	}
	exciterControl(t, http.MethodGet, "/_stub-oauth/"+tenant+"/grants", nil, &out)
	var mine []exciterGrant
	for _, g := range out.Grants {
		if g.Person == person {
			mine = append(mine, g)
		}
	}
	return mine
}

// exchangingExciter is the service as cmd/api builds it with the exchange
// on, reaching the stub under a host of the test's own, whose /meta the test
// tells to offer it without changing what another test's Exciter offers.
func (h *harness) exchangingExciter(t *testing.T) (*exciter.Service, string) {
	t.Helper()
	base := "http://" + uuid.NewString()[:8] + ".exchange-exciter.test"
	host := strings.TrimPrefix(base, "http://")
	exciterControl(t, http.MethodPut, "/_stub-meta/"+host, map[string]any{"capabilities": []string{
		"mcp@1", "requirement-embed@1", "requirement-list@1", "specifications@1", "webhooks@1", exciter.CapabilityTokenExchange,
	}}, nil)
	t.Cleanup(func() { exciterControl(t, http.MethodDelete, "/_stub-meta/"+host, nil, nil) })
	box, err := secret.New(testSecretKey(t))
	if err != nil {
		t.Fatal(err)
	}
	allow := netguard.ParseAllow(exciterStubHost)
	client := armature.NewClient(allow, map[string]string{base: os.Getenv(envExciterStub), exciterURL(t): os.Getenv(envExciterStub)})
	return exciter.NewService(h.cluster, box, client, exciter.NewCache(h.valkey(t), discard()),
		exciter.Options{AppURL: armatureAppURL, TokenExchange: true, Allow: allow, Development: true, Log: discard()}), base
}

func exciterAccountOf(t *testing.T, svc *exciter.Service, user uuid.UUID) *exciter.Account {
	t.Helper()
	got, err := svc.Account(db.PinPrimary(db.WithUser(context.Background(), user)))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestExciterConnectsPeopleAtSignIn(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	kc.reachable(t)
	kc.audience(t, statorClient, exciterClient)
	svc, base := h.exchangingExciter(t)
	a := h.startAPI(t, testSessionTTL, func(s *httpapi.Server) { s.Exciter = svc })
	h.emptySite(t, "exciter-exchange-"+uuid.NewString()[:6])
	tenant := exciterTenant(t)
	configureProvider(t, a, kc.issuer(), false)
	for _, email := range []string{"alice@suite.test", "bob@suite.test", "carol@suite.test"} {
		h.letIn(t, email, "member")
	}
	h.letIn(t, "keeper@exchange.test", "admin")
	admin := db.WithUser(context.Background(), h.userID(t, "keeper@exchange.test"))
	if _, _, err := svc.SaveConnection(admin, exciter.ConnectionInput{BaseURL: base}, ""); err != nil {
		t.Fatalf("connect the stub: %v", err)
	}

	t.Run("without the app nobody is connected, and the profile says why", func(t *testing.T) {
		signedIn(t, a, "carol", carolPassword)
		got := exciterAccountOf(t, svc, h.userID(t, "carol@suite.test"))
		if got.Connected || got.SignInRefusal == nil || !strings.Contains(*got.SignInRefusal, "not a partner app") {
			t.Fatalf("carol's account is %+v", got)
		}
	})

	var app struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	exciterControl(t, http.MethodPost, "/_stub-oauth/clients", map[string]any{"name": "Stator", "exchange": map[string]any{
		"keycloakClientId": statorClient, "tenant": tenant, "people": map[string]string{"alice@suite.test": "alice", "bob@suite.test": "bob"},
	}}, &app)
	if _, _, err := svc.SaveApp(admin, exciter.AppInput{ClientID: app.ClientID, ClientSecret: "not a secret"}, ""); err == nil {
		t.Error("a secret without Exciter's prefix was stored")
	}
	conn, _, err := svc.SaveApp(admin, exciter.AppInput{ClientID: app.ClientID, ClientSecret: app.ClientSecret}, "")
	if err != nil {
		t.Fatalf("store the app: %v", err)
	}
	if conn.App.ClientID == nil || *conn.App.ClientID != app.ClientID || !conn.App.SecretSet || !conn.App.Offered || !conn.App.Exchanging {
		t.Fatalf("the connection's app is %+v", conn.App)
	}

	alice := h.userID(t, "alice@suite.test")
	t.Run("alice is connected by signing in, and reads a requirement without pasting a token", func(t *testing.T) {
		b := signedIn(t, a, "alice", alicePassword)
		got := exciterAccountOf(t, svc, alice)
		if !got.Connected || got.Via == nil || *got.Via != exciter.ViaSignIn || got.Status != exciter.StatusOK || got.User == nil || got.User.Name != "Alice" {
			t.Fatalf("alice's account after signing in is %+v", got)
		}
		if grants := exciterGrants(t, tenant, "alice"); len(grants) != 1 || grants[0].Ended {
			t.Fatalf("Exciter's grants for alice are %+v", grants)
		}
		var sealed []byte
		if err := h.super.QueryRow(context.Background(), `SELECT refresh_token FROM exciter_token WHERE user_id = $1`, alice).Scan(&sealed); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(sealed, []byte("exciter_ort_")) {
			t.Error("the refresh token is readable at rest")
		}
		resp, body := b.get(t, a.URL+httpapi.APIPrefix+"/exciter/requirements/CHK-R1")
		if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"status":"ok"`) || !strings.Contains(body, `"key":"CHK-R1"`) {
			t.Fatalf("alice's CHK-R1 answered %d %s", resp.StatusCode, body)
		}

		// Exciter ends access tokens within the hour; the next read renews.
		exciterControl(t, http.MethodPost, "/_stub-oauth/"+tenant+"/people/alice/expire", nil, nil)
		resp, body = b.get(t, a.URL+httpapi.APIPrefix+"/exciter/requirements/CHK-R4")
		if resp.StatusCode != http.StatusOK || !strings.Contains(body, `"status":"ok"`) {
			t.Fatalf("after the access token ended CHK-R4 answered %d %s", resp.StatusCode, body)
		}
		if grants := exciterGrants(t, tenant, "alice"); len(grants) != 1 || grants[0].Refreshes != 1 {
			t.Errorf("after the access token ended the grant is %+v, want one refresh", grants)
		}
	})

	t.Run("bob keeps the token he pasted", func(t *testing.T) {
		bob := h.userID(t, "bob@suite.test")
		if _, _, err := svc.Connect(db.WithUser(context.Background(), bob), exciter.TokenPrefix+tenant+"_bob"); err != nil {
			t.Fatalf("bob pastes his token: %v", err)
		}
		signedIn(t, a, "bob", bobPassword)
		if got := exciterAccountOf(t, svc, bob); got.Via == nil || *got.Via != exciter.ViaToken {
			t.Errorf("bob's account after signing in is %+v", got)
		}
		if grants := exciterGrants(t, tenant, "bob"); len(grants) != 0 {
			t.Errorf("Exciter was asked for bob, who has a token: %+v", grants)
		}
	})

	t.Run("carol, whom Exciter does not know, signs in and reads why", func(t *testing.T) {
		b := newBrowser(t)
		if landing := b.signIn(t, a, "carol", carolPassword, "/spaces"); landing != "http://"+appHost+"/spaces" {
			t.Fatalf("carol landed on %q; a refused exchange must not fail the sign-in", landing)
		}
		got := exciterAccountOf(t, svc, h.userID(t, "carol@suite.test"))
		if got.Connected || got.SignInRefusal == nil || !strings.Contains(*got.SignInRefusal, "has no Exciter account yet") {
			t.Fatalf("carol's account is %+v, want Exciter's sentence", got)
		}
	})

	t.Run("disconnecting revokes the grant in Exciter", func(t *testing.T) {
		if _, err := svc.Disconnect(db.WithUser(context.Background(), alice)); err != nil {
			t.Fatal(err)
		}
		if grants := exciterGrants(t, tenant, "alice"); len(grants) != 1 || !grants[0].Ended {
			t.Errorf("disconnecting left the grant alive in Exciter: %+v", grants)
		}
	})

	t.Run("the app is the administrators', and a member cannot forge a grant's row for another", func(t *testing.T) {
		conn := appConn(t)
		actAs(t, conn, alice)
		var n int
		if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM exciter_app`).Scan(&n); err != nil || n != 0 {
			t.Errorf("a member reads %d apps with their sealed secret: %v", n, err)
		}
		untouched(t, conn, "forgetting the app", `DELETE FROM exciter_app`)
		denied(t, conn, "writing a refusal for carol", `INSERT INTO exciter_sign_in (user_id, refusal) VALUES ($1, 'mine')`, h.userID(t, "carol@suite.test"))
	})

	t.Run("a new address forgets the app and every grant", func(t *testing.T) {
		if _, _, err := svc.SaveConnection(admin, exciter.ConnectionInput{BaseURL: exciterURL(t)}, ""); err != nil {
			t.Fatal(err)
		}
		if n := h.countRows(t, `SELECT (SELECT count(*) FROM exciter_app) + (SELECT count(*) FROM exciter_sign_in) + (SELECT count(*) FROM exciter_token)`); n != 0 {
			t.Errorf("%d app, refusal and token rows outlived the move", n)
		}
	})
}
