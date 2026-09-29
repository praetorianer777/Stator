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
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

const carolPassword = "carol password"

// adminOf signs a fresh bootstrap owner of org in with a password.
func adminOf(t *testing.T, h *harness, a *api, org tenant.Org) (*browser, uuid.UUID) {
	t.Helper()
	email := bootstrapAdmin(t, h, a, org)
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

// standing is somebody's role in org and who decided it, straight from the table.
func (h *harness) standing(t *testing.T, org tenant.Org, email string) (string, string) {
	t.Helper()
	var role, source string
	err := h.super.QueryRow(context.Background(), `
		SELECT m.org_role, m.role_source FROM org_member m JOIN app_user u ON u.id = m.user_id
		WHERE m.org_id = $1 AND u.email = $2`, org.ID, email).Scan(&role, &source)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return role, source
}

// auditRows lists what the record holds about one person, oldest first.
func (h *harness) auditRows(t *testing.T, org tenant.Org, action string, target uuid.UUID) []map[string]any {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT data, actor_user_id IS NULL FROM audit_log
		WHERE org_id = $1 AND action = $2 AND target_id = $3 ORDER BY created_at, id`, org.ID, action, target)
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

func mapGroup(t *testing.T, admin *browser, a *api, group, role string) string {
	t.Helper()
	resp, body := postJSON(t, admin, a.URL+httpapi.APIPrefix+"/oidc-provider/group-roles", map[string]string{"group": group, "role": role})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("map %s to %s = %d %v", group, role, resp.StatusCode, body)
	}
	id, _ := body["groupRole"].(map[string]any)["id"].(string)
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

// Alice is in stator-administrators at the provider. Mapping that group makes
// her an administrator here at her next sign-in, and unmapping it takes it back.
func TestAMappedGroupGrantsItsRoleAndTakesItBack(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	kc.reachable(t)
	a := h.startAPI(t, testSessionTTL)
	org := h.makeOrg(t, "mapped")
	configureProvider(t, a, org, kc.issuer(), false)
	h.letIn(t, org, "alice@stator.test", "member")
	h.letIn(t, org, "bob@stator.test", "admin")
	admin, adminID := adminOf(t, h, a, org)
	aliceID := h.userID(t, "alice@stator.test")

	t.Run("with nothing mapped alice keeps the role she was given", func(t *testing.T) {
		if role := roleIn(t, signedIn(t, a, org, "alice", alicePassword), a); role != "member" {
			t.Fatalf("alice is %q, want member", role)
		}
	})

	var mappingID string
	t.Run("mapping stator-administrators makes her an admin", func(t *testing.T) {
		mappingID = mapGroup(t, admin, a, "stator-administrators", "admin")
		mapGroup(t, admin, a, "engineering", "member")
		if role := roleIn(t, signedIn(t, a, org, "alice", alicePassword), a); role != "admin" {
			t.Fatalf("alice is %q after the mapping, want admin, the higher of her two groups", role)
		}
		if role, source := h.standing(t, org, "alice@stator.test"); role != "admin" || source != "oidc" {
			t.Fatalf("alice's membership is %s from %s, want admin from oidc", role, source)
		}
	})

	t.Run("the administrator sees both mappings", func(t *testing.T) {
		resp, body := admin.get(t, a.URL+httpapi.APIPrefix+"/oidc-provider/group-roles")
		var out struct {
			GroupRoles []struct {
				Group string `json:"group"`
				Role  string `json:"role"`
			} `json:"groupRoles"`
		}
		if err := json.Unmarshal([]byte(body), &out); err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /oidc-provider/group-roles = %s %s", resp.Status, body)
		}
		got := map[string]string{}
		for _, m := range out.GroupRoles {
			got[m.Group] = m.Role
		}
		if len(got) != 2 || got["stator-administrators"] != "admin" || got["engineering"] != "member" {
			t.Fatalf("group roles = %v", got)
		}
	})

	t.Run("the list of members says the role comes from the provider", func(t *testing.T) {
		resp, body := admin.get(t, a.URL+httpapi.APIPrefix+"/users")
		var out struct {
			Members []struct {
				Email      string `json:"email"`
				Role       string `json:"role"`
				RoleSource string `json:"roleSource"`
			} `json:"members"`
		}
		if err := json.Unmarshal([]byte(body), &out); err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /users = %s %s", resp.Status, body)
		}
		got := map[string]string{}
		for _, m := range out.Members {
			got[m.Email] = m.Role + "/" + m.RoleSource
		}
		if got["alice@stator.test"] != "admin/oidc" || got["bob@stator.test"] != "admin/manual" {
			t.Fatalf("members = %v", got)
		}
	})

	t.Run("bob, an admin by hand in no mapped group, is left alone", func(t *testing.T) {
		if role := roleIn(t, signedIn(t, a, org, "bob", bobPassword), a); role != "admin" {
			t.Fatalf("bob is %q, want the admin somebody made him", role)
		}
		if role, source := h.standing(t, org, "bob@stator.test"); role != "admin" || source != "manual" {
			t.Fatalf("bob's membership is %s from %s", role, source)
		}
	})

	t.Run("unmapping the group makes her a member again at her next sign-in", func(t *testing.T) {
		resp, _ := admin.send(t, http.MethodDelete, a.URL+httpapi.APIPrefix+"/oidc-provider/group-roles/"+mappingID)
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("unmap = %s", resp.Status)
		}
		if role := roleIn(t, signedIn(t, a, org, "alice", alicePassword), a); role != "member" {
			t.Fatalf("alice is %q after the unmapping, want member", role)
		}
		if role, source := h.standing(t, org, "alice@stator.test"); role != "member" || source != "oidc" {
			t.Fatalf("alice's membership is %s from %s, want member from oidc through engineering", role, source)
		}
	})

	t.Run("with no mapped group left she stays a member", func(t *testing.T) {
		var engineering string
		_ = h.super.QueryRow(context.Background(), `SELECT id FROM oidc_group_role WHERE org_id = $1 AND group_ref = 'engineering'`, org.ID).Scan(&engineering)
		if resp, _ := admin.send(t, http.MethodDelete, a.URL+httpapi.APIPrefix+"/oidc-provider/group-roles/"+engineering); resp.StatusCode != http.StatusNoContent {
			t.Fatalf("unmap engineering = %s", resp.Status)
		}
		if role := roleIn(t, signedIn(t, a, org, "alice", alicePassword), a); role != "member" {
			t.Fatalf("alice is %q, want member: the floor while she is still a member", role)
		}
		if role, source := h.standing(t, org, "alice@stator.test"); role != "member" || source != "manual" {
			t.Fatalf("alice's membership is %s from %s, want member from manual", role, source)
		}
	})

	t.Run("every role the mapping changed is on the record, by nobody here", func(t *testing.T) {
		changes := h.auditRows(t, org, "member.role_changed", aliceID)
		want := [][2]string{{"member", "admin"}, {"admin", "member"}}
		if len(changes) != len(want) {
			t.Fatalf("recorded changes = %v, want %v", changes, want)
		}
		for i, change := range changes {
			if change["from"] != want[i][0] || change["to"] != want[i][1] || change["byProvider"] != true {
				t.Errorf("change %d = %v, want %s to %s by the provider", i, change, want[i][0], want[i][1])
			}
		}
		if groups, _ := changes[0]["groups"].([]any); len(groups) != 1 || groups[0] != "stator-administrators" {
			t.Errorf("the promotion names groups %v", changes[0]["groups"])
		}
		var set, removed int
		_ = h.super.QueryRow(context.Background(), `
			SELECT count(*) FILTER (WHERE action = 'sso.group_role_set'), count(*) FILTER (WHERE action = 'sso.group_role_removed')
			FROM audit_log WHERE org_id = $1 AND actor_user_id = $2`, org.ID, adminID).Scan(&set, &removed)
		if set != 2 || removed != 2 {
			t.Errorf("the administrator's mapping changes recorded %d sets and %d removals, want 2 and 2", set, removed)
		}
	})
}

// signedIn signs somebody into org through Keycloak in a browser of their own,
// which a previous sign-in's provider session would otherwise skip the form of.
func signedIn(t *testing.T, a *api, org tenant.Org, username, password string) *browser {
	t.Helper()
	b := newBrowser(t)
	if landing := b.signIn(t, a, org.Slug, username, password, "/"); landing != "http://"+appHost+"/" {
		t.Fatalf("%s landed on %q", username, landing)
	}
	return b
}

// A mapped group is the administrators' yes given in advance: carol, whom
// nobody let in, joins directly once engineering is mapped. Bob still waits.
func TestAMappedGroupLetsSomebodyInWithoutWaiting(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	kc.reachable(t)
	a := h.startAPI(t, testSessionTTL)
	org := h.makeOrg(t, "welcome")
	configureProvider(t, a, org, kc.issuer(), true)
	admin, _ := adminOf(t, h, a, org)

	t.Run("before the mapping carol waits like anybody", func(t *testing.T) {
		if landing := newBrowser(t).signIn(t, a, org.Slug, "carol", carolPassword, "/"); landing != "http://"+appHost+"/login?sso=not_a_member" {
			t.Fatalf("carol landed on %q", landing)
		}
		if n := h.countRows(t, `SELECT count(*) FROM org_join_request r JOIN app_user u ON u.id = r.user_id WHERE r.org_id = $1 AND u.email = 'carol@stator.test'`, org.ID); n != 1 {
			t.Fatalf("carol has %d requests, want 1", n)
		}
	})

	mapGroup(t, admin, a, "engineering", "member")
	carolID := h.userID(t, "carol@stator.test")

	t.Run("once engineering is mapped carol joins directly", func(t *testing.T) {
		b := newBrowser(t)
		if landing := b.signIn(t, a, org.Slug, "carol", carolPassword, "/spaces"); landing != "http://"+appHost+"/spaces" {
			t.Fatalf("carol landed on %q, want where she was going", landing)
		}
		if role := roleIn(t, b, a); role != "member" {
			t.Fatalf("carol is %q, want member", role)
		}
		if role, source := h.standing(t, org, "carol@stator.test"); role != "member" || source != "oidc" {
			t.Fatalf("carol's membership is %s from %s", role, source)
		}
		if got := h.providerGroups(t, org, "carol@stator.test"); !slices.Equal(got, []string{"engineering"}) {
			t.Fatalf("carol's groups = %q", got)
		}
	})

	t.Run("her request is answered and her joining is on the record", func(t *testing.T) {
		if n := h.countRows(t, `SELECT count(*) FROM org_join_request WHERE org_id = $1 AND user_id = $2`, org.ID, carolID); n != 0 {
			t.Fatalf("carol still has %d requests", n)
		}
		joined := h.auditRows(t, org, "member.joined", carolID)
		if len(joined) != 1 || joined[0]["role"] != "member" || joined[0]["byProvider"] != true {
			t.Fatalf("joining recorded as %v", joined)
		}
		if groups, _ := joined[0]["groups"].([]any); len(groups) != 1 || groups[0] != "engineering" {
			t.Fatalf("the join names groups %v", joined[0]["groups"])
		}
	})

	t.Run("bob, in no mapped group, still waits", func(t *testing.T) {
		if landing := newBrowser(t).signIn(t, a, org.Slug, "bob", bobPassword, "/"); landing != "http://"+appHost+"/login?sso=not_a_member" {
			t.Fatalf("bob landed on %q", landing)
		}
		if role, _ := h.standing(t, org, "bob@stator.test"); role != "" {
			t.Fatalf("bob became %s", role)
		}
	})
}

// The owner is how an organization is always entered again, so no mapping
// moves them, and the database refuses to let a later code path try.
func TestTheOwnerIsNeverDemotedByAMapping(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	kc.reachable(t)
	a := h.startAPI(t, testSessionTTL)
	org := h.makeOrg(t, "keep")
	configureProvider(t, a, org, kc.issuer(), false)
	h.letIn(t, org, "alice@stator.test", "owner")
	admin, _ := adminOf(t, h, a, org)

	for _, mapping := range [][2]string{{"engineering", "member"}, {"stator-administrators", "admin"}} {
		mapGroup(t, admin, a, mapping[0], mapping[1])
		if role := roleIn(t, signedIn(t, a, org, "alice", alicePassword), a); role != "owner" {
			t.Fatalf("with %s mapped to %s alice is %q, want owner", mapping[0], mapping[1], role)
		}
		if role, source := h.standing(t, org, "alice@stator.test"); role != "owner" || source != "manual" {
			t.Fatalf("alice's membership is %s from %s", role, source)
		}
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action LIKE 'member.%'`, org.ID); n != 0 {
		t.Fatalf("%d role changes recorded for an owner nobody moved", n)
	}

	t.Run("straight through SQL the database refuses to hand an owner to the provider", func(t *testing.T) {
		conn := h.appConn(t, org.ID)
		aliceID := h.userID(t, "alice@stator.test")
		for _, sql := range []string{
			`UPDATE org_member SET org_role = 'member', role_source = 'oidc' WHERE user_id = $1`,
			`UPDATE org_member SET role_source = 'oidc' WHERE user_id = $1`,
		} {
			_, err := conn.Exec(context.Background(), sql, aliceID)
			if !isCheckViolation(err) {
				t.Errorf("%s = %v, want a check violation", sql, err)
			}
		}
	})
}

// appConn is a connection as stator_app scoped to one organization.
func (h *harness) appConn(t *testing.T, org uuid.UUID) *pgx.Conn {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, os.Getenv(envPrimary))
	if err != nil {
		t.Fatalf("connect as the app role: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, tenant.PostgresVar, org.String()); err != nil {
		t.Fatal(err)
	}
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

// Straight through SQL as stator_app, one organization's mapping is out of
// another's reach, and a mapping cannot grant ownership.
func TestRawSQLCannotReachAnotherTenantsGroupRoles(t *testing.T) {
	h := newHarness(t)
	a := h.makeMember(t, "nu")
	b := h.makeMember(t, "xi")
	ctx := context.Background()
	for _, m := range []member{a, b} {
		if _, err := h.super.Exec(ctx, `INSERT INTO oidc_provider (org_id, issuer, client_id) VALUES ($1, 'https://id.test', 'stator')`, m.org); err != nil {
			t.Fatal(err)
		}
		if _, err := h.super.Exec(ctx, `INSERT INTO oidc_group_role (org_id, group_ref, org_role) VALUES ($1, 'staff', 'admin')`, m.org); err != nil {
			t.Fatal(err)
		}
	}

	unscoped, err := pgx.Connect(ctx, os.Getenv(envPrimary))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unscoped.Close(context.Background()) })
	var n int
	if err := unscoped.QueryRow(ctx, `SELECT count(*) FROM oidc_group_role`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("an unscoped connection sees %d mappings (%v)", n, err)
	}

	conn := h.appConn(t, a.org)
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM oidc_group_role`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("org A sees %d mappings, want its own one (%v)", n, err)
	}
	tag, err := conn.Exec(ctx, `UPDATE oidc_group_role SET org_role = 'member' WHERE org_id = $1`, b.org)
	if err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("org A changed org B's mapping: %v %v", tag, err)
	}
	tag, err = conn.Exec(ctx, `DELETE FROM oidc_group_role WHERE org_id = $1`, b.org)
	if err != nil || tag.RowsAffected() != 0 {
		t.Fatalf("org A deleted org B's mapping: %v %v", tag, err)
	}
	for name, attempt := range map[string]struct {
		sql  string
		args []any
		code string
	}{
		"mapping a group for another tenant": {`INSERT INTO oidc_group_role (org_id, group_ref, org_role) VALUES ($1, 'evil', 'admin')`, []any{b.org}, "42501"},
		"granting ownership through a group": {`INSERT INTO oidc_group_role (org_id, group_ref, org_role) VALUES ($1, 'founders', 'owner')`, []any{a.org}, "23514"},
		"mapping the same group twice":       {`INSERT INTO oidc_group_role (org_id, group_ref, org_role) VALUES ($1, 'staff', 'member')`, []any{a.org}, "23505"},
		"mapping a blank group":              {`INSERT INTO oidc_group_role (org_id, group_ref, org_role) VALUES ($1, '  ', 'member')`, []any{a.org}, "23514"},
	} {
		t.Run(name+" is refused", func(t *testing.T) {
			_, err := conn.Exec(ctx, attempt.sql, attempt.args...)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != attempt.code {
				t.Fatalf("%s = %v, want SQLSTATE %s", attempt.sql, err, attempt.code)
			}
		})
	}
	var stillB int
	_ = h.super.QueryRow(ctx, `SELECT count(*) FROM oidc_group_role WHERE org_id = $1 AND org_role = 'admin'`, b.org).Scan(&stillB)
	if stillB != 1 {
		t.Fatal("org B's mapping did not survive org A")
	}
}

// Taking somebody out ends their reach at once; the owner and the caller stay.
func TestAnAdministratorRemovesAMember(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	kc.reachable(t)
	a := h.startAPI(t, testSessionTTL)
	org := h.makeOrg(t, "leaving")
	configureProvider(t, a, org, kc.issuer(), true)
	h.letIn(t, org, "alice@stator.test", "admin")
	h.letIn(t, org, "bob@stator.test", "member")
	admin, adminID := adminOf(t, h, a, org)
	aliceID, bobID := h.userID(t, "alice@stator.test"), h.userID(t, "bob@stator.test")
	bob := signedIn(t, a, org, "bob", bobPassword)
	alice := signedIn(t, a, org, "alice", alicePassword)
	remove := func(b *browser, id uuid.UUID) int {
		resp, _ := b.send(t, http.MethodDelete, a.URL+httpapi.APIPrefix+"/users/"+id.String())
		return resp.StatusCode
	}

	t.Run("a member may not remove anybody", func(t *testing.T) {
		if status := remove(bob, aliceID); status != http.StatusForbidden {
			t.Fatalf("bob removing alice = %d", status)
		}
	})

	t.Run("nobody removes the owner or themselves", func(t *testing.T) {
		if status := remove(alice, adminID); status != http.StatusConflict {
			t.Fatalf("alice removing the owner = %d, want 409", status)
		}
		if status := remove(alice, aliceID); status != http.StatusConflict {
			t.Fatalf("alice removing herself = %d, want 409", status)
		}
	})

	t.Run("removing bob takes his membership, his groups and his reach", func(t *testing.T) {
		if len(h.providerGroups(t, org, "bob@stator.test")) == 0 {
			t.Fatal("bob has no groups to lose; the test proves nothing")
		}
		if status := remove(admin, bobID); status != http.StatusNoContent {
			t.Fatalf("remove bob = %d", status)
		}
		if role, _ := h.standing(t, org, "bob@stator.test"); role != "" {
			t.Fatalf("bob is still %s", role)
		}
		if got := h.providerGroups(t, org, "bob@stator.test"); len(got) != 0 {
			t.Fatalf("bob kept the groups %q", got)
		}
		if status, me := bob.me(t, a); status != http.StatusOK || me["organization"] != nil {
			t.Fatalf("bob's open session still reaches the organization: %d %v", status, me)
		}
		if status := remove(admin, bobID); status != http.StatusNotFound {
			t.Fatalf("removing bob twice = %d, want 404", status)
		}
		removed := h.auditRows(t, org, "member.removed", bobID)
		if len(removed) != 1 || removed[0]["role"] != "member" || removed[0]["byProvider"] != false {
			t.Fatalf("the removal recorded as %v", removed)
		}
	})
}
