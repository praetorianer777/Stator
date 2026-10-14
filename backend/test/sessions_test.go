//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/oidc"
)

const adminPassword = "a long bootstrap password"

// bootstrapAdmin makes a local administrator of org the way cmd/seed does.
func bootstrapAdmin(t *testing.T, h *harness, a *api) string {
	t.Helper()
	email := "admin-" + uuid.NewString()[:8] + "@stator.test"
	h.forgetPerson(t, email)
	if _, err := a.accounts.EnsureAdmin(context.Background(), email, "Admin", adminPassword); err != nil {
		t.Fatalf("bootstrap the administrator: %v", err)
	}
	return email
}

func postJSON(t *testing.T, b *browser, target string, body any) (*http.Response, map[string]any) {
	t.Helper()
	encoded, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, target, bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestTheBootstrapAdministratorIsMadeOnce(t *testing.T) {
	h := newHarness(t)
	a := h.startAPI(t, testSessionTTL)
	h.emptySite(t, "boot")
	ctx := context.Background()
	email := "boot-" + uuid.NewString()[:8] + "@stator.test"
	h.forgetPerson(t, email)

	created, err := a.accounts.EnsureAdmin(ctx, email, "Boot", adminPassword)
	if err != nil || !created {
		t.Fatalf("first run = %v, %v", created, err)
	}
	var hash, role string
	if err := h.super.QueryRow(ctx, `
		SELECT u.password_hash, m.role FROM app_user u JOIN member m ON m.user_id = u.id
		WHERE u.email = $1`, email).Scan(&hash, &role); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") || role != string(auth.RoleOwner) {
		t.Fatalf("hash %q, role %q", hash, role)
	}

	created, err = a.accounts.EnsureAdmin(ctx, email, "Boot", adminPassword)
	var again string
	_ = h.super.QueryRow(ctx, `SELECT password_hash FROM app_user WHERE email = $1`, email).Scan(&again)
	if err != nil || created || again != hash {
		t.Fatalf("second run changed something: created %v, err %v, hash changed %v", created, err, again != hash)
	}

	if _, err := a.accounts.EnsureAdmin(ctx, email, "Boot", "a different long password"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.accounts.Login(ctx, email, "a different long password", "", ""); err != nil {
		t.Fatalf("the reset password does not sign in: %v", err)
	}
	if _, err := a.accounts.EnsureAdmin(ctx, email, "Boot", "short"); !errors.Is(err, auth.ErrPasswordTooShort) {
		t.Fatalf("a short password = %v", err)
	}
}

func TestPasswordLoginSessionsExpireAndEnd(t *testing.T) {
	h := newHarness(t)
	a := h.startAPI(t, testSessionTTL)
	h.emptySite(t, "local")
	email := bootstrapAdmin(t, h, a)
	ctx := context.Background()
	login := a.URL + httpapi.APIPrefix + "/auth/login"

	t.Run("a wrong password and an unknown address fail alike", func(t *testing.T) {
		for _, attempt := range []map[string]string{
			{"email": email, "password": "not the password at all"},
			{"email": "nobody-" + uuid.NewString()[:8] + "@stator.test", "password": adminPassword},
		} {
			resp, body := postJSON(t, newBrowser(t), login, attempt)
			e, _ := body["error"].(map[string]any)
			if resp.StatusCode != http.StatusUnauthorized || e["code"] != "invalid_credentials" {
				t.Errorf("%v = %d %v", attempt, resp.StatusCode, body)
			}
		}
	})

	b := newBrowser(t)
	resp, body := postJSON(t, b, login, map[string]string{"email": strings.ToUpper(email), "password": adminPassword})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login = %d %v", resp.StatusCode, body)
	}
	if current, _ := body["organization"].(map[string]any); current["name"] == nil || current["role"] != "owner" {
		t.Fatalf("login answered %v", body)
	}
	secret := b.sessionCookie(t, a, h.cfg.Auth.SessionCookie)
	digest := auth.HashToken(secret)

	t.Run("the session is recorded with its expiry and last use", func(t *testing.T) {
		var expires, seen time.Time
		var proof string
		if err := h.super.QueryRow(ctx, `SELECT expires_at, last_seen_at, proof FROM user_session WHERE token_hash = $1`, digest).Scan(&expires, &seen, &proof); err != nil {
			t.Fatal(err)
		}
		if proof != "password" || time.Until(expires) < testSessionTTL-time.Minute || time.Until(expires) > testSessionTTL {
			t.Fatalf("proof %s expires in %s", proof, time.Until(expires))
		}
		if _, err := h.super.Exec(ctx, `UPDATE user_session SET last_seen_at = now() - interval '1 hour' WHERE token_hash = $1`, digest); err != nil {
			t.Fatal(err)
		}
		if status, _ := b.me(t, a); status != http.StatusOK {
			t.Fatalf("/auth/me = %d", status)
		}
		if err := h.super.QueryRow(ctx, `SELECT last_seen_at FROM user_session WHERE token_hash = $1`, digest).Scan(&seen); err != nil || time.Since(seen) > time.Minute {
			t.Fatalf("last_seen_at was not refreshed: %s (%v)", seen, err)
		}
	})

	t.Run("a removed membership leaves the session without the organization", func(t *testing.T) {
		if _, err := h.super.Exec(ctx, `UPDATE member SET role = 'member'`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			h.cleanupExec(t, h.super, `UPDATE member SET role = 'owner'`)
		})
		p, err := a.accounts.Authenticate(ctx, secret)
		if err != nil || p.Role != auth.RoleMember || p.CanAdminister() {
			t.Fatalf("a demoted owner = %+v, %v", p, err)
		}
	})

	t.Run("an expired session is anonymous and its cookie is cleared", func(t *testing.T) {
		if _, err := h.super.Exec(ctx, `UPDATE user_session SET expires_at = now() - interval '1 second' WHERE token_hash = $1`, digest); err != nil {
			t.Fatal(err)
		}
		resp, _ := b.get(t, a.URL+httpapi.APIPrefix+"/auth/me")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("/auth/me with an expired session = %s", resp.Status)
		}
		if b.sessionCookie(t, a, h.cfg.Auth.SessionCookie) != "" {
			t.Error("the expired cookie was not cleared")
		}
		if _, err := a.accounts.Authenticate(ctx, secret); !errors.Is(err, auth.ErrInvalidToken) {
			t.Errorf("authenticate an expired session = %v", err)
		}
	})

	t.Run("a deactivated account is refused, not made anonymous", func(t *testing.T) {
		b := newBrowser(t)
		if resp, body := postJSON(t, b, login, map[string]string{"email": email, "password": adminPassword}); resp.StatusCode != http.StatusOK {
			t.Fatalf("login = %d %v", resp.StatusCode, body)
		}
		if _, err := h.super.Exec(ctx, `UPDATE app_user SET is_active = false WHERE email = $1`, email); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			h.cleanupExec(t, h.super, `UPDATE app_user SET is_active = true WHERE email = $1`, email)
		})
		if status, body := b.me(t, a); status != http.StatusForbidden {
			t.Fatalf("/auth/me when deactivated = %d %v", status, body)
		}
		resp, body := postJSON(t, newBrowser(t), login, map[string]string{"email": email, "password": adminPassword})
		if e, _ := body["error"].(map[string]any); resp.StatusCode != http.StatusForbidden || e["code"] != "account_inactive" {
			t.Fatalf("login when deactivated = %d %v", resp.StatusCode, body)
		}
	})
}

// The same subject is the same person whatever address it arrives with, and
// groups kept by hand are the provider's to leave alone.
func TestSignInUpsertsBySubjectAndSyncsOnlyProviderGroups(t *testing.T) {
	h := newHarness(t)
	a := h.startAPI(t, testSessionTTL)
	h.emptySite(t, "upsert")
	ctx := context.Background()
	if err := a.sso.Configure(ctx, oidc.Provider{Issuer: "https://id.upsert.test", ClientID: "stator", CreateGroups: true, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	email := "carol-" + uuid.NewString()[:8] + "@stator.test"
	renamed := "carol-renamed-" + uuid.NewString()[:8] + "@stator.test"
	h.forgetPerson(t, email)
	h.forgetPerson(t, renamed)
	identity := &oidc.Identity{Issuer: "https://id.upsert.test", Subject: "carol-" + uuid.NewString(), Email: email, Name: "Carol", Groups: []string{"a", "b"}}

	// Not a member yet: refused, with a request noted and nothing else given.
	if _, err := a.sso.SignIn(ctx, identity, time.Hour, "", ""); !errors.Is(err, oidc.ErrNotAMember) {
		t.Fatalf("a first sign-in = %v, want oidc.ErrNotAMember", err)
	}
	var waiting uuid.UUID
	if err := h.super.QueryRow(ctx, `
		SELECT r.user_id FROM join_request r JOIN app_user u ON u.id = r.user_id
		WHERE u.email = $1
		  AND NOT EXISTS (SELECT 1 FROM member m WHERE m.user_id = r.user_id)
		  AND NOT EXISTS (SELECT 1 FROM user_session s WHERE s.user_id = r.user_id)
		  AND NOT EXISTS (SELECT 1 FROM group_member g WHERE g.user_id = r.user_id)`, email).Scan(&waiting); err != nil {
		t.Fatalf("no bare request for carol: %v", err)
	}
	if _, _, err := a.accounts.AdmitJoinRequest(ctx, waiting, auth.RoleMember, uuid.Nil, ""); err != nil {
		t.Fatal(err)
	}

	first, err := a.sso.SignIn(ctx, identity, time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if first.UserID != waiting {
		t.Fatal("the admitted person signed in as somebody else")
	}
	var local uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO groups (name) VALUES ($1) RETURNING id`, "hand-made-"+uuid.NewString()[:8]).Scan(&local); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO group_member (group_id, user_id) VALUES ($1, $2)`, local, first.UserID); err != nil {
		t.Fatal(err)
	}

	identity.Email, identity.Name, identity.Groups = renamed, "Carol Renamed", []string{"b", "c"}
	second, err := a.sso.SignIn(ctx, identity, time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if second.UserID != first.UserID {
		t.Fatal("the same subject became a second person")
	}
	if !slices.Equal(second.Joined, []string{"c"}) || !slices.Equal(second.Left, []string{"a"}) {
		t.Errorf("joined %q left %q, want c and a", second.Joined, second.Left)
	}
	var gotEmail, gotName string
	var memberships int
	if err := h.super.QueryRow(ctx, `
		SELECT u.email, u.name, (SELECT count(*) FROM group_member m WHERE m.user_id = u.id)
		FROM app_user u WHERE u.id = $1`, first.UserID).Scan(&gotEmail, &gotName, &memberships); err != nil {
		t.Fatal(err)
	}
	if gotEmail != renamed || gotName != "Carol Renamed" || memberships != 3 {
		t.Errorf("account = %s %q in %d groups, want the new address and name in b, c and the local group", gotEmail, gotName, memberships)
	}

	t.Run("a first sign-in finds the member account using the address", func(t *testing.T) {
		admin := bootstrapAdmin(t, h, a)
		var adminID uuid.UUID
		_ = h.super.QueryRow(ctx, `SELECT id FROM app_user WHERE email = $1`, admin).Scan(&adminID)
		s, err := a.sso.SignIn(ctx, &oidc.Identity{Issuer: "https://id.upsert.test", Subject: "admin-" + uuid.NewString(), Email: admin, Name: "Admin"}, time.Hour, "", "")
		if err != nil || s.UserID != adminID {
			t.Fatalf("the administrator signed in as %v (%v), want %v", s, err, adminID)
		}
	})
}

// isCheckViolation recognises the error a trigger's check raises.
func isCheckViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23514"
}
