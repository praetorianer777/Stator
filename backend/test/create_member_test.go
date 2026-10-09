//go:build integration

package test

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// An administrator adds people with a password, who then sign in without any
// identity provider; nobody else may, whether through the API or straight
// through SQL as the application.
func TestAnAdministratorAddsPeopleWhoSignInWithAPassword(t *testing.T) {
	h := newHarness(t)
	a := h.startAPI(t, testSessionTTL)
	org := h.makeOrg(t, "adders")
	ctx := context.Background()
	users := a.URL + httpapi.APIPrefix + "/users"

	admin := newBrowser(t)
	adminEmail := bootstrapAdmin(t, h, a, org)
	if resp, body := postJSON(t, admin, a.URL+httpapi.APIPrefix+"/auth/login", map[string]string{"email": adminEmail, "password": adminPassword}); resp.StatusCode != http.StatusOK {
		t.Fatalf("admin login = %d %v", resp.StatusCode, body)
	}
	suffix := uuid.NewString()[:8]
	ada := "ada-" + suffix + "@stator.test"
	bob := "bob-" + suffix + "@stator.test"
	h.forgetPerson(t, ada)
	h.forgetPerson(t, bob)

	var generated string
	t.Run("a password left out is made and shown once", func(t *testing.T) {
		resp, body := postJSON(t, admin, users, map[string]string{"email": strings.ToUpper(ada), "name": "Ada", "role": "member"})
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("create = %d %v", resp.StatusCode, body)
		}
		member := body["member"].(map[string]any)
		generated, _ = member["password"].(string)
		if len(generated) < 12 || member["email"] != ada || member["newAccount"] != true || member["role"] != "member" {
			t.Fatalf("created member = %v", member)
		}
	})

	t.Run("the person signs in with it and reaches this organization", func(t *testing.T) {
		person := newBrowser(t)
		if resp, body := postJSON(t, person, a.URL+httpapi.APIPrefix+"/auth/login", map[string]string{"email": ada, "password": generated}); resp.StatusCode != http.StatusOK {
			t.Fatalf("login = %d %v", resp.StatusCode, body)
		}
		status, me := person.me(t, a)
		if status != http.StatusOK || me["organization"].(map[string]any)["slug"] != org.Slug {
			t.Fatalf("/auth/me = %d %v", status, me)
		}
	})

	t.Run("the password is stored hashed and the record is kept", func(t *testing.T) {
		var hash string
		var audited int
		if err := h.super.QueryRow(ctx, `
			SELECT u.password_hash, (SELECT count(*) FROM audit_log WHERE org_id = $2 AND action = 'member.created' AND target_id = u.id)
			FROM app_user u WHERE u.email = $1`, ada, org.ID).Scan(&hash, &audited); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(hash, generated) || !strings.HasPrefix(hash, "$argon2id$") || audited != 1 {
			t.Fatalf("hash %q, audit entries %d", hash, audited)
		}
	})

	t.Run("a password the administrator chooses is not echoed, and a short one is refused", func(t *testing.T) {
		resp, body := postJSON(t, admin, users, map[string]string{"email": bob, "role": "admin", "password": "short"})
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("a short password = %d %v", resp.StatusCode, body)
		}
		resp, body = postJSON(t, admin, users, map[string]string{"email": bob, "role": "admin", "password": "chosen by the administrator"})
		member, _ := body["member"].(map[string]any)
		if resp.StatusCode != http.StatusCreated || member["password"] != nil || member["name"] != "bob-"+suffix || member["role"] != "admin" {
			t.Fatalf("create = %d %v", resp.StatusCode, body)
		}
	})

	t.Run("the same address twice, a bad address and a bad role are refused in sentences", func(t *testing.T) {
		for name, tc := range map[string]struct {
			body map[string]string
			want int
		}{
			"already a member": {map[string]string{"email": ada, "role": "member"}, http.StatusConflict},
			"not an address":   {map[string]string{"email": "nobody", "role": "member"}, http.StatusUnprocessableEntity},
			"owner":            {map[string]string{"email": "o-" + suffix + "@stator.test", "role": "owner"}, http.StatusUnprocessableEntity},
		} {
			if resp, body := postJSON(t, admin, users, tc.body); resp.StatusCode != tc.want {
				t.Errorf("%s = %d %v, want %d", name, resp.StatusCode, body, tc.want)
			}
		}
	})

	t.Run("an address with an account elsewhere joins and keeps its own password", func(t *testing.T) {
		other := h.makeOrg(t, "elsewhere")
		_ = other
		var before string
		if err := h.super.QueryRow(ctx, `SELECT password_hash FROM app_user WHERE email = $1`, bob).Scan(&before); err != nil {
			t.Fatal(err)
		}
		second := h.makeOrg(t, "adders-two")
		secondAdmin := newBrowser(t)
		email := bootstrapAdmin(t, h, a, second)
		if resp, body := postJSON(t, secondAdmin, a.URL+httpapi.APIPrefix+"/auth/login", map[string]string{"email": email, "password": adminPassword}); resp.StatusCode != http.StatusOK {
			t.Fatalf("second admin login = %d %v", resp.StatusCode, body)
		}
		resp, body := postJSON(t, secondAdmin, users, map[string]string{"email": bob, "role": "member", "password": "a different password!"})
		member, _ := body["member"].(map[string]any)
		if resp.StatusCode != http.StatusCreated || member["newAccount"] != false || member["password"] != nil {
			t.Fatalf("create = %d %v", resp.StatusCode, body)
		}
		var after string
		if err := h.super.QueryRow(ctx, `SELECT password_hash FROM app_user WHERE email = $1`, bob).Scan(&after); err != nil || after != before {
			t.Fatalf("another organization's administrator changed bob's password (%v)", err)
		}
	})

	t.Run("a member cannot add anybody", func(t *testing.T) {
		person := newBrowser(t)
		if resp, body := postJSON(t, person, a.URL+httpapi.APIPrefix+"/auth/login", map[string]string{"email": ada, "password": generated}); resp.StatusCode != http.StatusOK {
			t.Fatalf("login = %d %v", resp.StatusCode, body)
		}
		if resp, body := postJSON(t, person, users, map[string]string{"email": "x-" + suffix + "@stator.test", "role": "member"}); resp.StatusCode != http.StatusForbidden {
			t.Fatalf("a member adding somebody = %d %v", resp.StatusCode, body)
		}
	})

	t.Run("straight through SQL as the application, making a person is refused too", func(t *testing.T) {
		conn, err := pgx.Connect(ctx, os.Getenv(envPrimary))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close(ctx)
		if _, err := conn.Exec(ctx, `SELECT set_config($1, $2, false)`, tenant.PostgresVar, org.ID.String()); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(ctx, `INSERT INTO app_user (email, name, password_hash) VALUES ($1, 'Sneaky', 'x')`, "sneaky-"+suffix+"@stator.test"); err == nil {
			t.Fatal("the application role made a person")
		}
	})
}
