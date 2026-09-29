//go:build integration

package test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// waitingFor lists the addresses an administrator's browser sees waiting.
func waitingFor(t *testing.T, b *browser, a *api) (int, []string) {
	t.Helper()
	resp, body := b.get(t, a.URL+httpapi.APIPrefix+"/users/requests")
	var out struct {
		Requests []struct {
			Email string `json:"email"`
		} `json:"requests"`
	}
	_ = json.Unmarshal([]byte(body), &out)
	var emails []string
	for _, r := range out.Requests {
		emails = append(emails, r.Email)
	}
	return resp.StatusCode, emails
}

// Somebody the provider knows but nobody here has let in is refused, waits for
// an administrator, and is let in or turned away by one, as in Armature.
func TestANewPersonWaitsUntilAnAdministratorLetsThemIn(t *testing.T) {
	h := newHarness(t)
	kc := newKeycloak(t)
	kc.reachable(t)
	a := h.startAPI(t, testSessionTTL)
	org := h.makeOrg(t, "doorway")
	configureProvider(t, a, org, kc.issuer(), true)
	ctx := context.Background()

	suffix := uuid.NewString()[:8]
	const password = "a new person's password"
	carol := kc.newPerson(t, h, "carol-"+suffix, password)
	dave := kc.newPerson(t, h, "dave-"+suffix, password)

	carolBrowser := newBrowser(t)
	t.Run("a first sign-in is refused and noted", func(t *testing.T) {
		landing := carolBrowser.signIn(t, a, org.Slug, "carol-"+suffix, password, "/spaces")
		if landing != "http://"+appHost+"/login?sso=not_a_member" {
			t.Fatalf("landed on %q, want the sign-in page saying the request waits", landing)
		}
		if carolBrowser.sessionCookie(t, a, h.cfg.Auth.SessionCookie) != "" {
			t.Fatal("somebody not let in was given a session cookie")
		}
		if status, _ := carolBrowser.me(t, a); status != http.StatusUnauthorized {
			t.Fatalf("/auth/me while waiting = %d", status)
		}
	})

	t.Run("straight through SQL there is a request and nothing else", func(t *testing.T) {
		var requests, members, sessions, groups int
		if err := h.super.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM org_join_request r WHERE r.org_id = $1 AND r.user_id = u.id),
			       (SELECT count(*) FROM org_member m WHERE m.user_id = u.id),
			       (SELECT count(*) FROM user_session s WHERE s.user_id = u.id),
			       (SELECT count(*) FROM group_member g WHERE g.user_id = u.id)
			FROM app_user u WHERE u.email = $2`, org.ID, carol).Scan(&requests, &members, &sessions, &groups); err != nil {
			t.Fatal(err)
		}
		if requests != 1 || members != 0 || sessions != 0 || groups != 0 {
			t.Fatalf("carol has %d requests, %d memberships, %d sessions, %d groups; want 1, 0, 0, 0", requests, members, sessions, groups)
		}

		// As the application, scoped to the organization, the person is not
		// among its people either: app_user shows members only.
		conn, err := pgx.Connect(ctx, os.Getenv(envPrimary))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close(ctx)
		if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, tenant.PostgresVar, org.ID.String()); err != nil {
			t.Fatal(err)
		}
		var visible int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM app_user WHERE email = $1`, carol).Scan(&visible); err != nil || visible != 0 {
			t.Fatalf("the organization sees carol among its people: %d (%v)", visible, err)
		}
	})

	newBrowser(t).signIn(t, a, org.Slug, "dave-"+suffix, password, "")

	admin := newBrowser(t)
	email := bootstrapAdmin(t, h, a, org)
	if resp, body := postJSON(t, admin, a.URL+httpapi.APIPrefix+"/auth/login", map[string]string{"email": email, "password": adminPassword}); resp.StatusCode != http.StatusOK {
		t.Fatalf("admin login = %d %v", resp.StatusCode, body)
	}

	var carolIn *browser
	var carolID, daveID uuid.UUID
	_ = h.super.QueryRow(ctx, `SELECT id FROM app_user WHERE email = $1`, carol).Scan(&carolID)
	_ = h.super.QueryRow(ctx, `SELECT id FROM app_user WHERE email = $1`, dave).Scan(&daveID)

	t.Run("an administrator sees who waits", func(t *testing.T) {
		status, emails := waitingFor(t, admin, a)
		if status != http.StatusOK || len(emails) != 2 || emails[0] != carol || emails[1] != dave {
			t.Fatalf("waiting = %d %v, want carol then dave", status, emails)
		}
	})

	t.Run("letting carol in makes her a member who signs in directly", func(t *testing.T) {
		resp, body := postJSON(t, admin, a.URL+httpapi.APIPrefix+"/users/requests/"+carolID.String()+"/admit", map[string]string{"role": "member"})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("admit = %d %v", resp.StatusCode, body)
		}
		// A browser of her own: the first one still holds Keycloak's session,
		// which would skip the login form.
		carolIn = newBrowser(t)
		landing := carolIn.signIn(t, a, org.Slug, "carol-"+suffix, password, "/spaces")
		if landing != "http://"+appHost+"/spaces" {
			t.Fatalf("carol landed on %q after being let in", landing)
		}
		if status, me := carolIn.me(t, a); status != http.StatusOK || me["organization"].(map[string]any)["role"] != "member" {
			t.Fatalf("/auth/me = %d %v", status, me)
		}
	})

	t.Run("a member cannot let anybody in", func(t *testing.T) {
		if status, _ := waitingFor(t, carolIn, a); status != http.StatusForbidden {
			t.Fatalf("a member listing requests = %d", status)
		}
		resp, _ := postJSON(t, carolIn, a.URL+httpapi.APIPrefix+"/users/requests/"+daveID.String()+"/admit", map[string]string{"role": "admin"})
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("a member admitting = %d", resp.StatusCode)
		}
	})

	t.Run("turning dave away forgets the request and lets nobody in", func(t *testing.T) {
		resp, _ := admin.send(t, http.MethodDelete, a.URL+httpapi.APIPrefix+"/users/requests/"+daveID.String())
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("decline = %s", resp.Status)
		}
		if _, emails := waitingFor(t, admin, a); len(emails) != 0 {
			t.Fatalf("still waiting: %v", emails)
		}
		var members int
		_ = h.super.QueryRow(ctx, `SELECT count(*) FROM org_member WHERE user_id = $1`, daveID).Scan(&members)
		if members != 0 {
			t.Fatal("a person turned away became a member")
		}
		resp, _ = admin.send(t, http.MethodDelete, a.URL+httpapi.APIPrefix+"/users/requests/"+daveID.String())
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("declining twice = %s, want 404", resp.Status)
		}
	})

	t.Run("both answers are on the record, which cannot be rewritten", func(t *testing.T) {
		var adminID uuid.UUID
		_ = h.super.QueryRow(ctx, `SELECT id FROM app_user WHERE email = $1`, email).Scan(&adminID)
		for target, action := range map[uuid.UUID]string{carolID: "member.admitted", daveID: "member.declined"} {
			var n int
			if err := h.super.QueryRow(ctx, `
				SELECT count(*) FROM audit_log
				WHERE org_id = $1 AND action = $2 AND target_type = 'user' AND target_id = $3 AND actor_user_id = $4`,
				org.ID, action, target, adminID).Scan(&n); err != nil || n != 1 {
				t.Errorf("%s for %s recorded %d times (%v)", action, target, n, err)
			}
		}
		conn, err := pgx.Connect(ctx, os.Getenv(envPrimary))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close(ctx)
		if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, tenant.PostgresVar, org.ID.String()); err != nil {
			t.Fatal(err)
		}
		for _, sql := range []string{`UPDATE audit_log SET action = 'nothing'`, `DELETE FROM audit_log`} {
			_, err := conn.Exec(ctx, sql)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
				t.Errorf("%s as the app role = %v, want permission denied", sql, err)
			}
		}
	})
}
