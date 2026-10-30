//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/httpapi"
)

// adminOf signs a fresh bootstrap owner of the site in with a password.
func adminOf(t *testing.T, h *harness, a *api) (*browser, uuid.UUID) {
	t.Helper()
	email := bootstrapAdmin(t, h, a)
	b := newBrowser(t)
	if resp, body := postJSON(t, b, a.URL+httpapi.APIPrefix+"/auth/login", map[string]string{"email": email, "password": adminPassword}); resp.StatusCode != http.StatusOK {
		t.Fatalf("admin login = %d %v", resp.StatusCode, body)
	}
	return b, h.userID(t, email)
}

func (h *harness) userID(t *testing.T, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM app_user WHERE email = $1`, email).Scan(&id); err != nil {
		t.Fatalf("no account for %s: %v", email, err)
	}
	return id
}

// standing is somebody's standing, member or guest, straight from the table;
// empty for somebody who is not in.
func (h *harness) standing(t *testing.T, email string) string {
	t.Helper()
	var role string
	err := h.super.QueryRow(context.Background(), `
		SELECT m.role FROM member m JOIN app_user u ON u.id = m.user_id
		WHERE u.email = $1`, email).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return role
}

// auditRows lists what the record holds about one person, oldest first.
func (h *harness) auditRows(t *testing.T, action string, target uuid.UUID) []map[string]any {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT data, actor_user_id IS NULL FROM audit_log
		WHERE action = $1 AND target_id = $2 ORDER BY created_at, id`, action, target)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var raw []byte
		var byNobody bool
		if err := rows.Scan(&raw, &byNobody); err != nil {
			t.Fatal(err)
		}
		var data map[string]any
		if err := json.Unmarshal(raw, &data); err != nil {
			t.Fatal(err)
		}
		data["byProvider"] = byNobody
		out = append(out, data)
	}
	return out
}

// mapGroup grants a role across the installation to the group the provider
// names group in its claim, making the group if need be, as an administrator
// does on the Groups and Roles tabs: admin is the administrator role, member
// the member role. It answers the grant's id.
func mapGroup(t *testing.T, admin *browser, a *api, group, role string) string {
	t.Helper()
	groupID := providerGroup(t, admin, a, group)
	key := map[string]string{"admin": "administrator", "member": "member"}[role]
	resp, body := postJSON(t, admin, a.URL+httpapi.APIPrefix+"/role-assignments", map[string]any{"role": key, "groupId": groupID})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("grant %s to %s = %d %v", key, group, resp.StatusCode, body)
	}
	id, _ := body["assignment"].(map[string]any)["id"].(string)
	return id
}

// providerGroup is the group carrying the provider's name in its claim, made
// through the API if there is none yet.
func providerGroup(t *testing.T, admin *browser, a *api, group string) string {
	t.Helper()
	_, list := admin.get(t, a.URL+httpapi.APIPrefix+"/groups?limit=50")
	var out struct {
		Groups []struct {
			ID          string `json:"id"`
			ExternalRef string `json:"externalRef"`
		} `json:"groups"`
	}
	_ = json.Unmarshal([]byte(list), &out)
	for _, g := range out.Groups {
		if g.ExternalRef == group {
			return g.ID
		}
	}
	resp, body := postJSON(t, admin, a.URL+httpapi.APIPrefix+"/groups", map[string]any{"name": group, "externalRef": group})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("make the group %s = %d %v", group, resp.StatusCode, body)
	}
	id, _ := body["group"].(map[string]any)["id"].(string)
	return id
}

func roleIn(t *testing.T, b *browser, a *api) string {
	t.Helper()
	status, me := b.me(t, a)
	if status != http.StatusOK {
		t.Fatalf("/auth/me = %d %v", status, me)
	}
	current, _ := me["organization"].(map[string]any)
	role, _ := current["role"].(string)
	return role
}

// administers says what /access/me tells the web app about administering,
// which is what decides its buttons.
func administers(t *testing.T, b *browser, a *api) bool {
	t.Helper()
	status, access := sendJSON(t, b, http.MethodGet, a.URL+httpapi.APIPrefix+"/access/me", nil)
	if status != http.StatusOK {
		t.Fatalf("/access/me = %d %v", status, access)
	}
	can, _ := access["can"].(map[string]any)
	return can["administer"] == true
}

// Alice is in stator-administrators and engineering at the provider. A grant
// to the group stator-administrators makes her an administrator here from her
// next sign-in, and a group the provider no longer names at a sign-in takes its
// grants with it.
func TestAProviderGroupsGrantsReachItsPeopleAndGoWithTheGroup(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	kc.reachable(t)
	a := h.startAPI(t, testSessionTTL)
	h.emptySite(t, "mapped")
	configureProvider(t, a, kc.issuer(), false)
	h.letIn(t, "alice@suite.test", "member")
	h.letIn(t, "dave@suite.test", "admin")
	admin, adminID := adminOf(t, h, a)
	aliceID := h.userID(t, "alice@suite.test")

	t.Run("with no group granted anything alice is a member who administers nothing", func(t *testing.T) {
		b := signedIn(t, a, "alice", alicePassword)
		if role := roleIn(t, b, a); role != "member" || administers(t, b, a) {
			t.Fatalf("alice is %q and administers %v", role, administers(t, b, a))
		}
	})

	var grantID string
	t.Run("a grant to stator-administrators makes her an administrator", func(t *testing.T) {
		grantID = mapGroup(t, admin, a, "stator-administrators", "admin")
		mapGroup(t, admin, a, "engineering", "member")
		b := signedIn(t, a, "alice", alicePassword)
		if role := roleIn(t, b, a); role != "member" || !administers(t, b, a) {
			t.Fatalf("alice is %q and administers %v, want a member who administers", role, administers(t, b, a))
		}
		if got := h.providerGroups(t, "alice@suite.test"); !slices.Contains(got, "stator-administrators") || !slices.Contains(got, "engineering") {
			t.Fatalf("alice's provider groups = %q", got)
		}
	})

	t.Run("the list of members says which group the role comes through", func(t *testing.T) {
		h.settle(t)
		resp, body := admin.get(t, a.URL+httpapi.APIPrefix+"/users")
		var out struct {
			Members []struct {
				Email string `json:"email"`
				Roles []struct {
					Role      string  `json:"role"`
					GroupName *string `json:"groupName"`
				} `json:"roles"`
			} `json:"members"`
		}
		if err := json.Unmarshal([]byte(body), &out); err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /users = %s %s", resp.Status, body)
		}
		got := map[string][]string{}
		for _, m := range out.Members {
			for _, r := range m.Roles {
				via := "by name"
				if r.GroupName != nil {
					via = *r.GroupName
				}
				got[m.Email] = append(got[m.Email], r.Role+" through "+via)
			}
		}
		if !slices.Contains(got["alice@suite.test"], "administrator through stator-administrators") || !slices.Contains(got["dave@suite.test"], "administrator through by name") {
			t.Fatalf("members hold %v", got)
		}
	})

	t.Run("dave, an administrator by name in no granted group, is left alone", func(t *testing.T) {
		if b := signedIn(t, a, "dave", davePassword); !administers(t, b, a) {
			t.Fatal("dave no longer administers")
		}
	})

	t.Run("a group the provider no longer names at a sign-in takes its grants along", func(t *testing.T) {
		// Alice is put in a group of the provider's that Keycloak does not
		// name for her, as if she had been in it and was taken out there.
		former := providerGroup(t, admin, a, "former-team")
		if _, err := h.super.Exec(context.Background(), `INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)`, former, aliceID); err != nil {
			t.Fatal(err)
		}
		resp, body := postJSON(t, admin, a.URL+httpapi.APIPrefix+"/role-assignments", map[string]any{"role": "space_creator", "groupId": former})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("grant former-team = %d %v", resp.StatusCode, body)
		}
		h.settle(t)
		var before bool
		if err := h.super.QueryRow(context.Background(), `SELECT 'createSpace' = ANY (perm_global_grants($1))`, aliceID).Scan(&before); err != nil || !before {
			t.Fatalf("alice does not create spaces through former-team before her sign-in (%v)", err)
		}
		signedIn(t, a, "alice", alicePassword)
		if got := h.providerGroups(t, "alice@suite.test"); slices.Contains(got, "former-team") {
			t.Fatalf("alice is still in former-team: %q", got)
		}
		var after bool
		if err := h.super.QueryRow(context.Background(), `SELECT 'createSpace' = ANY (perm_global_grants($1))`, aliceID).Scan(&after); err != nil || after {
			t.Fatalf("alice still creates spaces through a group the provider took her out of (%v)", err)
		}
	})

	t.Run("taking the group's grant away takes her administering away at once", func(t *testing.T) {
		resp, _ := admin.send(t, http.MethodDelete, a.URL+httpapi.APIPrefix+"/role-assignments/"+grantID)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("revoke = %s", resp.Status)
		}
		if b := signedIn(t, a, "alice", alicePassword); administers(t, b, a) || roleIn(t, b, a) != "member" {
			t.Fatal("alice still administers after the group's grant went")
		}
	})

	t.Run("every grant the administrator gave and took is on the record", func(t *testing.T) {
		var added, removed int
		_ = h.super.QueryRow(context.Background(), `
			SELECT count(*) FILTER (WHERE action = 'grant.added'), count(*) FILTER (WHERE action = 'grant.removed')
			FROM audit_log WHERE actor_user_id = $1`, adminID).Scan(&added, &removed)
		if added != 3 || removed != 1 {
			t.Errorf("the administrator's grants recorded %d added and %d removed, want 3 and 1", added, removed)
		}
		if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'member.role_changed'`); n != 0 {
			t.Errorf("%d role changes recorded, which grants no longer make", n)
		}
	})
}

// signedIn signs somebody in through Keycloak in a browser of their own,
// which a previous sign-in's provider session would otherwise skip the form of.
func signedIn(t *testing.T, a *api, username, password string) *browser {
	t.Helper()
	b := newBrowser(t)
	if landing := b.signIn(t, a, username, password, "/"); landing != "http://"+appHost+"/" {
		t.Fatalf("%s landed on %q", username, landing)
	}
	return b
}

// A group with a grant is the administrators' yes given in advance, once they
// let people in by their groups: carol, whom nobody let in, joins directly
// once quality holds a grant. Bob still waits.
func TestAMappedGroupLetsSomebodyInWithoutWaiting(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	kc.reachable(t)
	a := h.startAPI(t, testSessionTTL)
	h.emptySite(t, "welcome")
	configureProvider(t, a, kc.issuer(), true)
	admin, _ := adminOf(t, h, a)

	t.Run("before the mapping carol waits like anybody", func(t *testing.T) {
		if landing := newBrowser(t).signIn(t, a, "carol", carolPassword, "/"); landing != "http://"+appHost+"/login?sso=not_a_member" {
			t.Fatalf("carol landed on %q", landing)
		}
		if n := h.countRows(t, `SELECT count(*) FROM join_request r JOIN app_user u ON u.id = r.user_id WHERE u.email = 'carol@suite.test'`); n != 1 {
			t.Fatalf("carol has %d requests, want 1", n)
		}
	})

	mapGroup(t, admin, a, "quality", "member")
	admitByGroup(t, admin, a, true)
	carolID := h.userID(t, "carol@suite.test")

	t.Run("once quality is mapped carol joins directly", func(t *testing.T) {
		b := newBrowser(t)
		if landing := b.signIn(t, a, "carol", carolPassword, "/spaces"); landing != "http://"+appHost+"/spaces" {
			t.Fatalf("carol landed on %q, want where she was going", landing)
		}
		if role := roleIn(t, b, a); role != "member" {
			t.Fatalf("carol is %q, want member", role)
		}
		if role := h.standing(t, "carol@suite.test"); role != "member" {
			t.Fatalf("carol's membership is %s", role)
		}
		if got := h.providerGroups(t, "carol@suite.test"); !slices.Equal(got, []string{"portal-owners", "quality", "scrum-masters"}) {
			t.Fatalf("carol's groups = %q", got)
		}
	})

	t.Run("her request is answered and her joining is on the record", func(t *testing.T) {
		if n := h.countRows(t, `SELECT count(*) FROM join_request WHERE user_id = $1`, carolID); n != 0 {
			t.Fatalf("carol still has %d requests", n)
		}
		joined := h.auditRows(t, "member.joined", carolID)
		if len(joined) != 1 || joined[0]["role"] != "member" || joined[0]["byProvider"] != true {
			t.Fatalf("joining recorded as %v", joined)
		}
		if groups, _ := joined[0]["groups"].([]any); len(groups) != 1 || groups[0] != "quality" {
			t.Fatalf("the join names groups %v", joined[0]["groups"])
		}
	})

	t.Run("bob, in no mapped group, still waits", func(t *testing.T) {
		if landing := newBrowser(t).signIn(t, a, "bob", bobPassword, "/"); landing != "http://"+appHost+"/login?sso=not_a_member" {
			t.Fatalf("bob landed on %q", landing)
		}
		if role := h.standing(t, "bob@suite.test"); role != "" {
			t.Fatalf("bob became %s", role)
		}
	})
}

// appConn is a connection as stator_app.
func (h *harness) appConn(t *testing.T) *pgx.Conn {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv(envPrimary))
	if err != nil {
		t.Fatalf("connect as the app role: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func (h *harness) countRows(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := h.super.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// Taking somebody out ends their reach at once; the caller stays.
func TestAnAdministratorRemovesAMember(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	kc.reachable(t)
	a := h.startAPI(t, testSessionTTL)
	h.emptySite(t, "leaving")
	configureProvider(t, a, kc.issuer(), true)
	h.letIn(t, "alice@suite.test", "admin")
	h.letIn(t, "bob@suite.test", "member")
	admin, _ := adminOf(t, h, a)
	aliceID, bobID := h.userID(t, "alice@suite.test"), h.userID(t, "bob@suite.test")
	bob := signedIn(t, a, "bob", bobPassword)
	alice := signedIn(t, a, "alice", alicePassword)
	remove := func(b *browser, id uuid.UUID) int {
		resp, _ := b.send(t, http.MethodDelete, a.URL+httpapi.APIPrefix+"/users/"+id.String())
		return resp.StatusCode
	}

	t.Run("a member may not remove anybody", func(t *testing.T) {
		if status := remove(bob, aliceID); status != http.StatusForbidden {
			t.Fatalf("bob removing alice = %d", status)
		}
	})

	t.Run("nobody removes themselves", func(t *testing.T) {
		if status := remove(alice, aliceID); status != http.StatusConflict {
			t.Fatalf("alice removing herself = %d, want 409", status)
		}
	})

	t.Run("removing bob takes his membership, his groups and his reach", func(t *testing.T) {
		if len(h.providerGroups(t, "bob@suite.test")) == 0 {
			t.Fatal("bob has no groups to lose; the test proves nothing")
		}
		if status := remove(admin, bobID); status != http.StatusNoContent {
			t.Fatalf("remove bob = %d", status)
		}
		if role := h.standing(t, "bob@suite.test"); role != "" {
			t.Fatalf("bob is still %s", role)
		}
		if got := h.providerGroups(t, "bob@suite.test"); len(got) != 0 {
			t.Fatalf("bob kept the groups %q", got)
		}
		if status, me := bob.me(t, a); status != http.StatusOK || me["organization"] != nil {
			t.Fatalf("bob's open session still reaches the organization: %d %v", status, me)
		}
		if status := remove(admin, bobID); status != http.StatusNotFound {
			t.Fatalf("removing bob twice = %d, want 404", status)
		}
		removed := h.auditRows(t, "member.removed", bobID)
		if len(removed) != 1 || removed[0]["role"] != "member" || removed[0]["byProvider"] != false {
			t.Fatalf("the removal recorded as %v", removed)
		}
	})
}
