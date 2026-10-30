//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/oauth"
)

// admitByGroup turns letting people in by their groups on or off, as an
// administrator does on the settings page, keeping the rest as it is.
func admitByGroup(t *testing.T, admin *browser, a *api, on bool) {
	t.Helper()
	target := a.URL + httpapi.APIPrefix + "/oidc-provider"
	status, view := sendJSON(t, admin, http.MethodGet, target, nil)
	was, _ := view["provider"].(map[string]any)
	if status != http.StatusOK || was == nil {
		t.Fatalf("GET /oidc-provider = %d %v", status, view)
	}
	status, saved := sendJSON(t, admin, http.MethodPut, target, map[string]any{
		"issuer": was["issuer"], "clientId": was["clientId"], "groupsClaim": was["groupsClaim"], "scopes": was["scopes"],
		"createGroups": was["createGroups"], "admitByGroup": on, "enabled": was["enabled"],
	})
	if provider, _ := saved["provider"].(map[string]any); status != http.StatusOK || provider["admitByGroup"] != on {
		t.Fatalf("admitting by group %v answered %d %v", on, status, saved)
	}
}

// switchedOff makes the account an address belongs to, switched off.
func (h *harness) switchedOff(t *testing.T, email string) {
	t.Helper()
	if _, err := h.super.Exec(context.Background(), `
		INSERT INTO app_user (email, name, is_active) VALUES ($1, $2, false)
		ON CONFLICT (email) DO UPDATE SET is_active = false`, email, email); err != nil {
		t.Fatal(err)
	}
}

// admittedBy is the group the audit log says let somebody in, and where.
func (h *harness) admittedBy(t *testing.T, email string) (group, via string) {
	t.Helper()
	joined := h.auditRows(t, "member.joined", h.userID(t, email))
	if len(joined) != 1 {
		t.Fatalf("%s joined %d times on the record, want once: %v", email, len(joined), joined)
	}
	group, _ = joined[0]["group"].(string)
	via, _ = joined[0]["via"].(string)
	return group, via
}

// A mapped group lets somebody new in at sign-in only once an administrator
// turns it on; it sets the role of somebody already in either way. Carol is
// in quality, dave in marketing, bob in neither.
func TestAMappedGroupLetsSomebodyNewInAtSignInOnlyWhenTurnedOn(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	kc.reachable(t)
	a := h.startAPI(t, testSessionTTL)
	h.emptySite(t, "admit-sign-in")
	configureProvider(t, a, kc.issuer(), true)
	h.letIn(t, "alice@suite.test", "member")
	admin, _ := adminOf(t, h, a)
	mapGroup(t, admin, a, "quality", "member")
	mapGroup(t, admin, a, "marketing", "member")
	mapGroup(t, admin, a, "stator-administrators", "admin")
	notAMember := "http://" + appHost + "/login?sso=not_a_member"

	t.Run("off, the default, carol in a mapped group waits like anybody", func(t *testing.T) {
		if landing := newBrowser(t).signIn(t, a, "carol", carolPassword, "/"); landing != notAMember {
			t.Fatalf("carol landed on %q", landing)
		}
		if role, _ := h.standing(t, "carol@suite.test"); role != "" {
			t.Fatalf("carol became %s with the setting off", role)
		}
		if n := h.countRows(t, `SELECT count(*) FROM join_request r JOIN app_user u ON u.id = r.user_id WHERE u.email = 'carol@suite.test'`); n != 1 {
			t.Fatalf("carol has %d requests, want 1", n)
		}
	})

	t.Run("off, a mapped group still sets the role of somebody already in", func(t *testing.T) {
		if role := roleIn(t, signedIn(t, a, "alice", alicePassword), a); role != "admin" {
			t.Fatalf("alice is %q, want the admin her group maps to", role)
		}
	})

	admitByGroup(t, admin, a, true)

	t.Run("on, carol is let in with her group's role and stops waiting", func(t *testing.T) {
		b := newBrowser(t)
		if landing := b.signIn(t, a, "carol", carolPassword, "/spaces"); landing != "http://"+appHost+"/spaces" {
			t.Fatalf("carol landed on %q, want where she was going", landing)
		}
		if role, source := h.standing(t, "carol@suite.test"); role != "member" || source != "oidc" {
			t.Fatalf("carol's membership is %s from %s", role, source)
		}
		if n := h.countRows(t, `SELECT count(*) FROM join_request r JOIN app_user u ON u.id = r.user_id WHERE u.email = 'carol@suite.test'`); n != 0 {
			t.Fatalf("carol still has %d requests", n)
		}
		if group, via := h.admittedBy(t, "carol@suite.test"); group != "quality" || via != "sign_in" {
			t.Errorf("the audit log says admitted by %q via %q, want quality at sign-in", group, via)
		}
	})

	t.Run("on, bob, in no mapped group, still waits", func(t *testing.T) {
		if landing := newBrowser(t).signIn(t, a, "bob", bobPassword, "/"); landing != notAMember {
			t.Fatalf("bob landed on %q", landing)
		}
		if role, _ := h.standing(t, "bob@suite.test"); role != "" {
			t.Fatalf("bob became %s", role)
		}
	})

	t.Run("on, dave, switched off, is refused whatever his groups", func(t *testing.T) {
		h.switchedOff(t, "dave@suite.test")
		if landing := newBrowser(t).signIn(t, a, "dave", davePassword, "/"); landing != "http://"+appHost+"/login?sso=inactive" {
			t.Fatalf("dave landed on %q", landing)
		}
		if role, _ := h.standing(t, "dave@suite.test"); role != "" {
			t.Fatalf("a switched off account became %s", role)
		}
	})
}

// A partner's token exchange connects somebody Stator has never seen when
// their group is mapped, only once an administrator turns it on, and keeps
// no account for anybody it refuses.
func TestAMappedGroupLetsAPartnerConnectSomebodyNewOnlyWhenTurnedOn(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	kc.reachable(t)
	kc.audience(t, exciterClient, statorClient)
	a := h.startAPI(t, testSessionTTL, func(s *httpapi.Server) {
		s.OAuth = oauth.NewService(h.cluster, s.OIDC, "http://"+appHost, discard())
	})
	h.emptySite(t, "admit-exchange")
	configureProvider(t, a, kc.issuer(), false)
	admin, _ := adminOf(t, h, a)
	id, secret := registerApp(t, admin, a, map[string]any{"name": "Exciter", "scopes": []string{"read"}, "keycloakClientId": exciterClient})
	mapGroup(t, admin, a, "quality", "member")
	mapGroup(t, admin, a, "marketing", "member")
	exchange := func(username, password string) oauthAnswer {
		t.Helper()
		return oauthPost(t, a, "/oauth/token", id, secret, exchangeForm(partnerToken(t, kc, exciterClient, exciterSecret, username, password)))
	}
	refused := func(t *testing.T, who string, got oauthAnswer) {
		t.Helper()
		if got.Status != http.StatusBadRequest || got.str("error") != "invalid_grant" {
			t.Fatalf("the exchange for %s answered %d %v", who, got.Status, got.Body)
		}
	}
	accounts := func(email string) int {
		return h.countRows(t, `SELECT count(*) FROM app_user WHERE email = $1`, email)
	}

	t.Run("off, the default, carol in a mapped group is not connected", func(t *testing.T) {
		refused(t, "carol", exchange("carol", carolPassword))
		if n := accounts("carol@suite.test"); n != 0 {
			t.Errorf("a refused exchange kept carol's account")
		}
	})

	admitByGroup(t, admin, a, true)

	t.Run("on, carol is connected with her group's role, and the record names the group", func(t *testing.T) {
		if got := exchange("carol", carolPassword); got.Status != http.StatusOK || got.str("scope") != "read" {
			t.Fatalf("the exchange for carol answered %d %v", got.Status, got.Body)
		}
		if role, source := h.standing(t, "carol@suite.test"); role != "member" || source != "oidc" {
			t.Fatalf("carol's membership is %s from %s", role, source)
		}
		if group, via := h.admittedBy(t, "carol@suite.test"); group != "quality" || via != "token_exchange" {
			t.Errorf("the audit log says admitted by %q via %q, want quality at a token exchange", group, via)
		}
	})

	t.Run("on, bob, in no mapped group, is refused and no account is kept", func(t *testing.T) {
		refused(t, "bob", exchange("bob", bobPassword))
		if n := accounts("bob@suite.test"); n != 0 {
			t.Errorf("a refused exchange kept bob's account")
		}
	})

	t.Run("on, dave, switched off, is refused whatever his groups", func(t *testing.T) {
		h.switchedOff(t, "dave@suite.test")
		refused(t, "dave", exchange("dave", davePassword))
		if role, _ := h.standing(t, "dave@suite.test"); role != "" {
			t.Fatalf("a switched off account became %s", role)
		}
	})
}

// An installation that already maps a group let people in by it from the
// moment it was mapped, so the upgrade turns the setting on there, and leaves
// it off where nothing is mapped.
func TestTheUpgradeKeepsLettingPeopleInWhereAGroupIsMapped(t *testing.T) {
	newHarness(t)
	ctx := context.Background()
	adding, before := migrationsAround(t, "_oidc_admit_by_group.sql")
	scratch := scratchDatabase(t)
	upTo(t, scratch.db, before)
	if _, err := scratch.pool.Exec(ctx, `INSERT INTO oidc_provider (issuer, client_id) VALUES ('https://id.test', 'stator')`); err != nil {
		t.Fatal(err)
	}
	admits := func() bool {
		t.Helper()
		var on bool
		if err := scratch.pool.QueryRow(ctx, `SELECT admit_by_group FROM oidc_provider`).Scan(&on); err != nil {
			t.Fatal(err)
		}
		return on
	}

	upTo(t, scratch.db, adding)
	if admits() {
		t.Error("the upgrade let people in by their groups where no group is mapped")
	}

	if err := goose.DownToContext(ctx, scratch.db, ".", before); err != nil {
		t.Fatalf("migrate back to %d: %v", before, err)
	}
	if _, err := scratch.pool.Exec(ctx, `INSERT INTO oidc_group_role (group_ref, role) VALUES ('staff', 'member')`); err != nil {
		t.Fatal(err)
	}
	upTo(t, scratch.db, adding)
	if !admits() {
		t.Error("the upgrade stopped letting people in by the group an installation maps")
	}
}
