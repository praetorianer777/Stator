//go:build integration

package test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
)

// Connecting people to Armature at sign-in (#430): Stator and the stub's
// Armature share the realm dynamo, and a sign-in through Keycloak trades the
// person's access token for a grant of Stator's app, with no click. The
// Keycloak is the stack's, with nothing stood in for but Armature.

// exchangeApp registers Stator in the stub as an Armature administrator does,
// its Keycloak client named and people mapped by address, and pastes the
// app into Stator as a Stator administrator does.
func exchangeApp(t *testing.T, ctx context.Context, svc *armature.Service, tenant string, keycloakClient string, people map[string]string) {
	t.Helper()
	exchange := map[string]any{"keycloakClientId": keycloakClient, "tenant": tenant, "people": people}
	if keycloakClient == "" {
		exchange = nil
	}
	var app struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	}
	oauthControl(t, http.MethodPost, "/_stub-oauth/clients", map[string]any{"name": armature.AppName, "scope": armature.AppScopes,
		"redirectUris": []string{armatureAppURL + armature.OAuthCallbackPath}, "exchange": exchange}, &app)
	if _, _, err := svc.SaveApp(ctx, armature.AppInput{ClientID: app.ClientID, ClientSecret: app.ClientSecret}, ""); err != nil {
		t.Fatalf("paste the app: %v", err)
	}
}

func accountOf(t *testing.T, svc *armature.Service, user uuid.UUID) *armature.Account {
	t.Helper()
	got, err := svc.Account(db.PinPrimary(db.WithUser(context.Background(), user)))
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestArmatureConnectsPeopleAtSignIn(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	kc.reachable(t)
	kc.audience(t, statorClient, "armature")
	svc := h.armatureWith(t, armature.Options{TokenExchange: true})
	a := h.startAPI(t, testSessionTTL, func(s *httpapi.Server) { s.Armature = svc })
	site := h.emptySite(t, "exchange-"+uuid.NewString()[:6])
	tenant := site.Slug
	t.Cleanup(func() { oauthControl(t, http.MethodDelete, "/_stub/"+tenant, nil, nil) })
	configureProvider(t, a, kc.issuer(), false)
	for _, email := range []string{"alice@suite.test", "bob@suite.test", "carol@suite.test", "dave@suite.test"} {
		h.letIn(t, email, "member")
	}
	h.letIn(t, "keeper@exchange.test", "admin")
	admin := db.WithUser(context.Background(), h.userID(t, "keeper@exchange.test"))
	host := strings.TrimPrefix(connectedAppURL(h), "http://")
	metaControl(t, http.MethodPut, host, map[string]any{"capabilities": stubCapabilities})
	t.Cleanup(func() { metaControl(t, http.MethodDelete, host, nil) })
	if _, _, err := svc.SaveConnection(admin, armature.ConnectionInput{BaseURL: connectedAppURL(h)}, ""); err != nil {
		t.Fatalf("connect the stub: %v", err)
	}
	exchangeApp(t, admin, svc, tenant, statorClient, map[string]string{"alice@suite.test": "alice", "bob@suite.test": "bob", "dave@suite.test": "dave"})

	t.Run("an Armature that does not offer the exchange is not asked", func(t *testing.T) {
		signedIn(t, a, "dave", davePassword)
		dave := h.userID(t, "dave@suite.test")
		if got := accountOf(t, svc, dave); got.Connected || got.App.SignInRefusal != nil {
			t.Fatalf("dave's account after signing in is %+v", got)
		}
		if grants := stubGrants(t, tenant, "dave"); len(grants) != 0 {
			t.Fatalf("Armature was asked for dave: %+v", grants)
		}
	})

	metaControl(t, http.MethodPut, host, map[string]any{"capabilities": append(stubCapabilities, armature.CapabilityTokenExchange)})
	// The settings page asks Armature afresh, which is how an upgrade is
	// learned; read on the primary, where the connection surely is.
	if conn, err := svc.Connection(db.PinPrimary(admin)); err != nil || conn == nil || !conn.Partner.OffersTokenExchange() {
		t.Fatalf("the connection after the upgrade is %+v: %v", conn, err)
	}

	alice := h.userID(t, "alice@suite.test")
	t.Run("alice is connected by signing in, and her issues embed without a click", func(t *testing.T) {
		b := signedIn(t, a, "alice", alicePassword)
		got := accountOf(t, svc, alice)
		if !got.Connected || got.Via == nil || *got.Via != armature.ViaApp || got.Status != armature.StatusOK || got.App.SignInRefusal != nil {
			t.Fatalf("alice's account after signing in is %+v", got)
		}
		if strings.Join(got.App.Scopes, " ") != armature.AppScopes || got.User == nil || got.User.Email != "alice@"+tenant+".armature.test" {
			t.Errorf("alice's grant holds %v for %+v", got.App.Scopes, got.User)
		}
		grants := stubGrants(t, tenant, "alice")
		if len(grants) != 1 || !grants[0].Exchanged || grants[0].Ended {
			t.Fatalf("Armature's grants for alice are %+v, want one from the exchange", grants)
		}
		resp, body := b.get(t, a.URL+httpapi.APIPrefix+"/armature/issues/CP-1")
		var answer struct {
			Status string `json:"status"`
			Issue  *struct {
				Key string `json:"key"`
			} `json:"issue"`
		}
		_ = json.Unmarshal([]byte(body), &answer)
		if resp.StatusCode != http.StatusOK || answer.Status != "ok" || answer.Issue == nil || answer.Issue.Key != "CP-1" {
			t.Errorf("alice's issue CP-1 answered %d %s", resp.StatusCode, body)
		}
	})

	t.Run("a live grant is kept, and a dead one filled in again", func(t *testing.T) {
		var before uuid.UUID
		if err := h.super.QueryRow(context.Background(), `SELECT id FROM armature_grant WHERE user_id = $1`, alice).Scan(&before); err != nil {
			t.Fatal(err)
		}
		signedIn(t, a, "alice", alicePassword)
		if grants := stubGrants(t, tenant, "alice"); len(grants) != 1 || grants[0].Refreshes != 0 {
			t.Errorf("signing in again with a live grant asked Armature: %+v", grants)
		}
		if _, err := h.super.Exec(context.Background(), `UPDATE armature_grant SET status = 'rejected' WHERE user_id = $1`, alice); err != nil {
			t.Fatal(err)
		}
		signedIn(t, a, "alice", alicePassword)
		var after uuid.UUID
		var status string
		if err := h.super.QueryRow(context.Background(), `SELECT id, status FROM armature_grant WHERE user_id = $1`, alice).Scan(&after, &status); err != nil {
			t.Fatal(err)
		}
		if after == before || status != "ok" {
			t.Errorf("the rejected grant %s is now %s %s, want a new live one", before, after, status)
		}
	})

	t.Run("bob keeps the token he pasted", func(t *testing.T) {
		bob := h.userID(t, "bob@suite.test")
		if _, _, err := svc.Connect(db.WithUser(context.Background(), bob), patFor(tenant, "bob")); err != nil {
			t.Fatalf("bob pastes his token: %v", err)
		}
		signedIn(t, a, "bob", bobPassword)
		if got := accountOf(t, svc, bob); got.Via == nil || *got.Via != armature.ViaToken || got.Status != armature.StatusOK {
			t.Errorf("bob's account after signing in is %+v", got)
		}
		if grants := stubGrants(t, tenant, "bob"); len(grants) != 0 {
			t.Errorf("Armature was asked for bob, who has a token: %+v", grants)
		}
	})

	t.Run("carol, whom Armature does not know, signs in and reads why", func(t *testing.T) {
		b := newBrowser(t)
		if landing := b.signIn(t, a, "carol", carolPassword, "/spaces"); landing != "http://"+appHost+"/spaces" {
			t.Fatalf("carol landed on %q; a refused exchange must not fail the sign-in", landing)
		}
		got := accountOf(t, svc, h.userID(t, "carol@suite.test"))
		if got.Connected || got.App.SignInRefusal == nil || !strings.Contains(*got.App.SignInRefusal, "has no Armature account yet") {
			t.Fatalf("carol's account is %+v, want Armature's sentence", got)
		}
	})

	t.Run("a refusal is the person's own, and the database keeps it so", func(t *testing.T) {
		conn := appConn(t)
		actAs(t, conn, alice)
		var n int
		if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM armature_sign_in`).Scan(&n); err != nil || n != 0 {
			t.Errorf("alice reads %d refusals of others: %v", n, err)
		}
		carol := h.userID(t, "carol@suite.test")
		untouched(t, conn, "clearing carol's refusal", `DELETE FROM armature_sign_in WHERE user_id = $1`, carol)
		denied(t, conn, "writing a refusal for carol", `INSERT INTO armature_sign_in (user_id, refusal) VALUES ($1, 'mine')`, carol)
	})
}

func TestAnAppWithoutAKeycloakClientLeavesConnectToThePerson(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	kc.reachable(t)
	kc.audience(t, statorClient, "armature")
	svc := h.armatureWith(t, armature.Options{TokenExchange: true})
	a := h.startAPI(t, testSessionTTL, func(s *httpapi.Server) { s.Armature = svc })
	site := h.emptySite(t, "no-exchange-"+uuid.NewString()[:6])
	t.Cleanup(func() { oauthControl(t, http.MethodDelete, "/_stub/"+site.Slug, nil, nil) })
	configureProvider(t, a, kc.issuer(), false)
	h.letIn(t, "alice@suite.test", "member")
	h.letIn(t, "keeper@exchange.test", "admin")
	admin := db.WithUser(context.Background(), h.userID(t, "keeper@exchange.test"))
	host := strings.TrimPrefix(connectedAppURL(h), "http://")
	metaControl(t, http.MethodPut, host, map[string]any{"capabilities": append(stubCapabilities, armature.CapabilityTokenExchange)})
	t.Cleanup(func() { metaControl(t, http.MethodDelete, host, nil) })
	if _, _, err := svc.SaveConnection(admin, armature.ConnectionInput{BaseURL: connectedAppURL(h)}, ""); err != nil {
		t.Fatalf("connect the stub: %v", err)
	}
	exchangeApp(t, admin, svc, site.Slug, "", nil)

	signedIn(t, a, "alice", alicePassword)
	got := accountOf(t, svc, h.userID(t, "alice@suite.test"))
	if got.Connected || got.App.SignInRefusal == nil || !strings.Contains(*got.App.SignInRefusal, "Keycloak client") || !got.App.Offered {
		t.Fatalf("alice's account is %+v, want Armature's sentence and Connect offered", got)
	}
}
