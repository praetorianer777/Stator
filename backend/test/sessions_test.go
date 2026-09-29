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
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/httpapi"
	"github.com/praetorianer777/stator/backend/internal/oidc"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

const adminPassword = "a long bootstrap password"

// bootstrapAdmin makes a local administrator of org the way cmd/seed does.
func bootstrapAdmin(t *testing.T, h *harness, a *api, org tenant.Org) string {
	t.Helper()
	email := "admin-" + uuid.NewString()[:8] + "@stator.test"
	h.forgetPerson(t, email)
	if _, err := a.accounts.EnsureAdmin(context.Background(), org.Slug, email, "Admin", adminPassword); err != nil {
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
	org := h.makeOrg(t, "boot")
	ctx := context.Background()
	email := "boot-" + uuid.NewString()[:8] + "@stator.test"
	h.forgetPerson(t, email)

	created, err := a.accounts.EnsureAdmin(ctx, org.Slug, email, "Boot", adminPassword)
	if err != nil || !created {
		t.Fatalf("first run = %v, %v", created, err)
	}
	var hash, role string
	if err := h.super.QueryRow(ctx, `
		SELECT u.password_hash, m.org_role FROM app_user u JOIN org_member m ON m.user_id = u.id
		WHERE u.email = $1 AND m.org_id = $2`, email, org.ID).Scan(&hash, &role); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") || role != string(auth.RoleOwner) {
		t.Fatalf("hash %q, role %q", hash, role)
	}

	created, err = a.accounts.EnsureAdmin(ctx, org.Slug, email, "Boot", adminPassword)
	var again string
	_ = h.super.QueryRow(ctx, `SELECT password_hash FROM app_user WHERE email = $1`, email).Scan(&again)
	if err != nil || created || again != hash {
		t.Fatalf("second run changed something: created %v, err %v, hash changed %v", created, err, again != hash)
	}

	if _, err := a.accounts.EnsureAdmin(ctx, org.Slug, email, "Boot", "a different long password"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.accounts.Login(ctx, email, "a different long password", "", ""); err != nil {
		t.Fatalf("the reset password does not sign in: %v", err)
	}
	if _, err := a.accounts.EnsureAdmin(ctx, org.Slug, email, "Boot", "short"); !errors.Is(err, auth.ErrPasswordTooShort) {
		t.Fatalf("a short password = %v", err)
	}
}

func TestPasswordLoginSessionsExpireAndEnd(t *testing.T) {
	h := newHarness(t)
	a := h.startAPI(t, testSessionTTL)
	org := h.makeOrg(t, "local")
	email := bootstrapAdmin(t, h, a, org)
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
	if current, _ := body["organization"].(map[string]any); current["slug"] != org.Slug || current["role"] != "owner" {
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
		if _, err := h.super.Exec(ctx, `UPDATE org_member SET org_role = 'member' WHERE org_id = $1`, org.ID); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = h.super.Exec(context.Background(), `UPDATE org_member SET org_role = 'owner' WHERE org_id = $1`, org.ID)
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
			_, _ = h.super.Exec(context.Background(), `UPDATE app_user SET is_active = true WHERE email = $1`, email)
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

// A password reaches everywhere the person is a member, a provider's sign-in
// only where that provider is trusted, even against a write that skips the service.
func TestSwitchingOrganizationsFollowsTheProof(t *testing.T) {
	h := newHarness(t)
	a := h.startAPI(t, testSessionTTL)
	home := h.makeOrg(t, "home")
	away := h.makeOrg(t, "away")
	email := bootstrapAdmin(t, h, a, home)
	ctx := context.Background()

	var userID uuid.UUID
	if err := h.super.QueryRow(ctx, `SELECT id FROM app_user WHERE email = $1`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO org_member (org_id, user_id, org_role) VALUES ($1, $2, 'member')`, away.ID, userID); err != nil {
		t.Fatal(err)
	}

	t.Run("a password session switches and lists both", func(t *testing.T) {
		creds, err := a.accounts.Login(ctx, email, adminPassword, "", "")
		if err != nil {
			t.Fatal(err)
		}
		orgs, err := a.accounts.Organizations(ctx, creds.Principal)
		if err != nil || len(orgs) != 2 {
			t.Fatalf("organizations = %v, %v", orgs, err)
		}
		switched, _, err := a.accounts.SwitchOrg(ctx, *creds.Principal.SessionID, userID, away.Slug)
		if err != nil || switched.ID != away.ID || switched.Role != auth.RoleMember {
			t.Fatalf("switch = %+v, %v", switched, err)
		}
		if _, _, err := a.accounts.SwitchOrg(ctx, *creds.Principal.SessionID, userID, "no-such-org"); !errors.Is(err, auth.ErrNotAMember) {
			t.Fatalf("switching to an unknown organization = %v", err)
		}
	})

	t.Run("a provider's session stays with the organizations that trust it", func(t *testing.T) {
		var sessionID uuid.UUID
		_, err := h.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			// Made the way the provider's sign-in makes one, for home.
			if _, err := tx.Exec(ctx, `UPDATE org_member SET org_role = 'member' WHERE org_id = $1 AND user_id = $2`, home.ID, userID); err != nil {
				return err
			}
			id, _, err := auth.OpenSession(ctx, tx, userID, &home.ID, auth.ProofOIDC, time.Now().Add(time.Hour), "", "")
			sessionID = id
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		p := &auth.Principal{UserID: userID, SessionID: &sessionID}
		if orgs, _ := a.accounts.Organizations(ctx, p); len(orgs) != 1 || orgs[0].OrgID != home.ID {
			t.Errorf("a provider's session lists %v, want only home", orgs)
		}
		if _, _, err := a.accounts.SwitchOrg(ctx, sessionID, userID, away.Slug); !errors.Is(err, auth.ErrSessionStaysHome) {
			t.Fatalf("switching away = %v, want ErrSessionStaysHome", err)
		}
		_, err = h.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `UPDATE user_session SET current_org_id = $2 WHERE id = $1`, sessionID, away.ID)
			return err
		})
		if !isCheckViolation(err) {
			t.Fatalf("a direct update moved the session away: %v", err)
		}

		// Once both organizations trust the same provider, the proof reaches.
		for _, org := range []tenant.Org{home, away} {
			if _, _, err := a.sso.Save(tenant.WithOrg(ctx, org), oidc.Provider{Issuer: "https://id.shared.test", ClientID: "stator", Enabled: true}); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, err := a.accounts.SwitchOrg(ctx, sessionID, userID, away.Slug); err != nil {
			t.Fatalf("switching between organizations sharing a provider = %v", err)
		}
	})
}

// The same subject is the same person whatever address it arrives with, and
// groups kept by hand are the provider's to leave alone.
func TestSignInUpsertsBySubjectAndSyncsOnlyProviderGroups(t *testing.T) {
	h := newHarness(t)
	a := h.startAPI(t, testSessionTTL)
	org := h.makeOrg(t, "upsert")
	ctx := context.Background()
	orgCtx := tenant.WithOrg(ctx, org)
	if _, _, err := a.sso.Save(orgCtx, oidc.Provider{Issuer: "https://id.upsert.test", ClientID: "stator", CreateGroups: true, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	email := "carol-" + uuid.NewString()[:8] + "@stator.test"
	renamed := "carol-renamed-" + uuid.NewString()[:8] + "@stator.test"
	h.forgetPerson(t, email)
	h.forgetPerson(t, renamed)
	identity := &oidc.Identity{Issuer: "https://id.upsert.test", Subject: "carol-" + uuid.NewString(), Email: email, Name: "Carol", Groups: []string{"a", "b"}}

	first, err := a.sso.SignIn(ctx, org.ID, identity, time.Hour, "", "")
	if err != nil {
		t.Fatal(err)
	}
	var local uuid.UUID
	if err := h.super.QueryRow(ctx, `INSERT INTO groups (org_id, name) VALUES ($1, $2) RETURNING id`, org.ID, "hand-made-"+uuid.NewString()[:8]).Scan(&local); err != nil {
		t.Fatal(err)
	}
	if _, err := h.super.Exec(ctx, `INSERT INTO group_member (org_id, group_id, user_id) VALUES ($1, $2, $3)`, org.ID, local, first.UserID); err != nil {
		t.Fatal(err)
	}

	identity.Email, identity.Name, identity.Groups = renamed, "Carol Renamed", []string{"b", "c"}
	second, err := a.sso.SignIn(ctx, org.ID, identity, time.Hour, "", "")
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
		SELECT u.email, u.name, (SELECT count(*) FROM group_member m WHERE m.user_id = u.id AND m.org_id = $2)
		FROM app_user u WHERE u.id = $1`, first.UserID, org.ID).Scan(&gotEmail, &gotName, &memberships); err != nil {
		t.Fatal(err)
	}
	if gotEmail != renamed || gotName != "Carol Renamed" || memberships != 3 {
		t.Errorf("account = %s %q in %d groups, want the new address and name in b, c and the local group", gotEmail, gotName, memberships)
	}

	t.Run("a first sign-in finds the account using the address", func(t *testing.T) {
		admin := bootstrapAdmin(t, h, a, org)
		var adminID uuid.UUID
		_ = h.super.QueryRow(ctx, `SELECT id FROM app_user WHERE email = $1`, admin).Scan(&adminID)
		s, err := a.sso.SignIn(ctx, org.ID, &oidc.Identity{Issuer: "https://id.upsert.test", Subject: "admin-" + uuid.NewString(), Email: admin, Name: "Admin"}, time.Hour, "", "")
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
