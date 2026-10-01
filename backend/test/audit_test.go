//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/praetorianer777/stator/backend/internal/audit"
	"github.com/praetorianer777/stator/backend/internal/auth"
	"github.com/praetorianer777/stator/backend/internal/db"
	"github.com/praetorianer777/stator/backend/internal/oidc"
	"github.com/praetorianer777/stator/backend/internal/tenant"
)

// The audit log (#107): what is recorded, by whom and on what, who may read it,
// that it is only ever added to, and that retention takes only the old.

// recordedOnce fails unless the log holds exactly one entry of the action by
// the actor on the target, nil meaning none, and returns its data as text.
func (h *harness) recordedOnce(t *testing.T, org uuid.UUID, action string, actor *uuid.UUID, target any) string {
	t.Helper()
	rows, err := h.super.Query(context.Background(), `
		SELECT data::text FROM audit_log
		WHERE org_id = $1 AND action = $2 AND actor_user_id IS NOT DISTINCT FROM $3 AND target_id IS NOT DISTINCT FROM $4::uuid`,
		org, action, actor, target)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var found []string
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			t.Fatal(err)
		}
		found = append(found, data)
	}
	if len(found) != 1 {
		t.Errorf("%s by %v on %v is recorded %d times, want once", action, actor, target, len(found))
		return ""
	}
	return found[0]
}

func idOf(t *testing.T, r response, keys ...string) string {
	t.Helper()
	id, _ := obj(t, r, keys...)["id"].(string)
	if id == "" {
		t.Fatalf("no id in %s", r.Raw)
	}
	return id
}

// Every act the log records, done once through the API or the service that
// does it, is one entry naming who did it and what it was done to.
func TestEveryAuditedActIsRecordedOnceWithItsActorAndTarget(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "audit-acts")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	annID := h.namedPerson(t, home.org, "Ann Audit")
	ann := api.as(t, annID, home.org, slug)
	me := &home.user
	covered := map[string]bool{}
	once := func(action string, actor *uuid.UUID, target any) string {
		t.Helper()
		covered[action] = true
		return h.recordedOnce(t, home.org, action, actor, target)
	}

	spaceID := idOf(t, want(t, owner.post(t, "/api/v1/spaces", map[string]any{"key": "AUD", "name": "Audited"}), http.StatusCreated, "make a space"), "space")
	homeID := obj(t, want(t, owner.get(t, "/api/v1/spaces/AUD"), http.StatusOK, "the space"), "space")["homePageId"].(string)
	docs := &tree{t: t, c: owner, key: "AUD", homeID: homeID}
	once(audit.ActionSpaceCreated, me, spaceID)

	want(t, owner.patch(t, "/api/v1/spaces/AUD", map[string]any{"name": "Audited log"}), http.StatusOK, "rename the space")
	once(audit.ActionSpaceUpdated, me, spaceID)

	want(t, owner.put(t, "/api/v1/spaces/AUD/permissions", map[string]any{"grants": []any{
		map[string]any{"subject": everyone, "permissions": []any{"view", "addPages", "addComments"}},
		map[string]any{"subject": user(home.user), "permissions": []any{"administer"}},
	}}), http.StatusOK, "set the space's table")
	once(audit.ActionSpacePermissionsSet, me, spaceID)

	plans := docs.add(homeID, "Plans")
	want(t, restrict(t, owner, plans, []any{user(home.user), user(annID)}, nil), http.StatusOK, "restrict Plans")
	once(audit.ActionPageRestrictionsSet, me, plans)

	want(t, owner.put(t, "/api/v1/org/permissions/createSpace", map[string]any{"subjects": []any{user(annID)}}), http.StatusOK, "grant createSpace")
	once(audit.ActionOrgPermissionSet, me, nil)

	made := want(t, ann.post(t, "/api/v1/tokens", map[string]any{"name": "Ann's script"}), http.StatusCreated, "ann makes a token")
	tokenID := idOf(t, made, "token")
	secret := obj(t, made, "token")["secret"].(string)
	if data := once(audit.ActionTokenCreated, &annID, tokenID); strings.Contains(data, secret) || strings.Contains(data, auth.APITokenPrefix) {
		t.Errorf("the record of a new token holds its secret: %s", data)
	}
	want(t, owner.delete(t, "/api/v1/org/tokens/"+tokenID), http.StatusNoContent, "the owner revokes ann's token")
	once(audit.ActionTokenRevoked, me, tokenID)

	thread := startThread(t, ann, plans, "Ann's note.")
	threadID := thread["id"].(string)
	want(t, owner.delete(t, "/api/v1/comments/"+threadID), http.StatusNoContent, "the owner deletes ann's comment")
	once(audit.ActionCommentDeleted, me, threadID)

	gone := docs.add(homeID, "Gone")
	want(t, owner.delete(t, pagePath(gone)), http.StatusNoContent, "trash Gone")
	want(t, owner.delete(t, "/api/v1/spaces/AUD/trash/"+gone), http.StatusNoContent, "purge Gone")
	once(audit.ActionPagePurged, me, gone)
	want(t, owner.delete(t, pagePath(docs.add(homeID, "Swept"))), http.StatusNoContent, "trash Swept")
	want(t, owner.delete(t, "/api/v1/spaces/AUD/trash"), http.StatusNoContent, "empty the trash")
	once(audit.ActionTrashEmptied, me, spaceID)

	themeID := idOf(t, want(t, owner.post(t, "/api/v1/themes", map[string]any{"name": "House", "spec": map[string]any{"colors": map[string]any{"light": map[string]string{"accent": "#336699"}}}}), http.StatusCreated, "make a theme"), "theme")
	want(t, owner.patch(t, "/api/v1/themes/"+themeID, map[string]any{"shared": true}), http.StatusOK, "share it")
	want(t, owner.put(t, "/api/v1/themes/default", map[string]any{"themeId": themeID}), http.StatusOK, "make it the default")
	if data := once(audit.ActionThemeDefaultSet, me, themeID); !strings.Contains(data, "House") {
		t.Errorf("the default theme's record does not name it: %s", data)
	}
	want(t, owner.put(t, "/api/v1/themes/default", map[string]any{"themeId": nil}), http.StatusOK, "back to the built-in theme")
	once(audit.ActionThemeDefaultSet, me, nil)
	var ip *string
	if err := h.super.QueryRow(context.Background(), `SELECT host(ip) FROM audit_log WHERE org_id = $1 AND action = $2 AND target_id = $3`, home.org, audit.ActionThemeDefaultSet, themeID).Scan(&ip); err != nil || ip == nil {
		t.Errorf("the caller's address is not recorded: %v %v", ip, err)
	}

	if resp, _ := owner.download(t, pagePath(plans, "/export?subtree=true")); resp.StatusCode != http.StatusOK {
		t.Fatalf("export Plans: %s", resp.Status)
	}
	if data := once(audit.ActionPageExported, me, plans); !strings.Contains(data, `"scope": "subtree"`) {
		t.Errorf("the export's record reads %s", data)
	}
	notes := docs.add(homeID, "Notes")
	if resp, _ := ann.download(t, pagePath(notes, "/markdown")); resp.StatusCode != http.StatusOK {
		t.Fatalf("ann takes Notes as Markdown: %s", resp.Status)
	}
	if data := once(audit.ActionPageExported, &annID, notes); !strings.Contains(data, `"scope": "markdown"`) {
		t.Errorf("the Markdown export's record reads %s", data)
	}
	want(t, owner.post(t, pagePath(notes, "/share"), map[string]any{"recipients": []any{user(annID)}, "message": "A private word for ann."}),
		http.StatusCreated, "share Notes with ann")
	if data := once(audit.ActionPageShared, me, notes); !strings.Contains(data, "Ann Audit") || strings.Contains(data, "private word") {
		t.Errorf("the share's record reads %s", data)
	}

	base := armatureURL(t)
	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": base, "orgSlug": slug, "webhookSecret": webhookSecret}), http.StatusOK, "connect Armature")
	if data := once(audit.ActionArmatureConnectionSaved, me, nil); strings.Contains(data, webhookSecret) || !strings.Contains(data, `"webhookSecret": "set"`) {
		t.Errorf("the connection's record reads %s", data)
	}
	want(t, owner.delete(t, "/api/v1/armature/connection"), http.StatusNoContent, "disconnect Armature")
	once(audit.ActionArmatureConnectionRemoved, me, nil)

	waiting := func(name string) uuid.UUID {
		var id uuid.UUID
		if err := h.super.QueryRow(context.Background(), `INSERT INTO app_user (email, name) VALUES ($1, $2) RETURNING id`,
			fmt.Sprintf("%s-%s@example.test", name, uuid.NewString()[:8]), name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE id = $1`, id) })
		if _, err := h.super.Exec(context.Background(), `INSERT INTO org_join_request (org_id, user_id) VALUES ($1, $2)`, home.org, id); err != nil {
			t.Fatal(err)
		}
		h.settle(t)
		return id
	}
	carol, dave := waiting("Carol"), waiting("Dave")
	want(t, owner.post(t, "/api/v1/users/requests/"+carol.String()+"/admit", map[string]any{"role": "member"}), http.StatusOK, "let carol in")
	once(audit.ActionMemberAdmitted, me, carol)
	want(t, owner.delete(t, "/api/v1/users/requests/"+dave.String()), http.StatusNoContent, "turn dave away")
	once(audit.ActionMemberDeclined, me, dave)
	want(t, owner.delete(t, "/api/v1/users/"+carol.String()), http.StatusNoContent, "take carol out again")
	once(audit.ActionMemberRemoved, me, carol)

	// Signing in is not this server's to do, so the provider's settings are
	// changed through the service, as the request would.
	sso := oidc.NewService(h.cluster, nil, "http://stator.test/callback")
	if _, _, err := sso.Save(home.ctx, oidc.Provider{Issuer: "https://id.audit.test", ClientID: "stator", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if data := once(audit.ActionSSOProviderSaved, me, nil); !strings.Contains(data, `"clientSecret": "kept"`) || !strings.Contains(data, "id.audit.test") {
		t.Errorf("the provider's record reads %s", data)
	}
	mapped, _, err := sso.SetGroupRole(home.ctx, "auditors", auth.RoleAdmin, home.user, "")
	if err != nil {
		t.Fatal(err)
	}
	once(audit.ActionGroupRoleSet, me, mapped.ID)
	if _, err := sso.RemoveGroupRole(home.ctx, mapped.ID, home.user, ""); err != nil {
		t.Fatal(err)
	}
	once(audit.ActionGroupRoleRemoved, me, mapped.ID)

	if resp, _ := owner.download(t, "/api/v1/audit/export?targetType=space"); resp.StatusCode != http.StatusOK {
		t.Fatalf("export the log: %s", resp.Status)
	}
	if data := once(audit.ActionAuditExported, me, nil); !strings.Contains(data, `"targetType": "space"`) {
		t.Errorf("the log's export does not note its filter: %s", data)
	}

	want(t, owner.put(t, pagePath(notes, "/owner"), map[string]any{"userId": annID}), http.StatusOK, "ann owns Notes")
	if data := once(audit.ActionPageOwnerSet, me, notes); !strings.Contains(data, "Ann Audit") {
		t.Errorf("the owner's record does not name her: %s", data)
	}
	want(t, owner.delete(t, pagePath(notes, "/owner")), http.StatusNoContent, "Notes has no owner")
	once(audit.ActionPageOwnerRemoved, me, notes)
	want(t, ann.put(t, pagePath(notes, "/verification"), map[string]any{"days": 30}), http.StatusOK, "ann verifies Notes")
	if data := once(audit.ActionPageVerified, &annID, notes); !strings.Contains(data, `"days": 30`) {
		t.Errorf("the verification's record reads %s", data)
	}
	want(t, ann.delete(t, pagePath(notes, "/verification")), http.StatusNoContent, "ann takes it back")
	once(audit.ActionPageUnverified, &annID, notes)

	want(t, owner.put(t, pagePath(notes, "/archive"), nil), http.StatusOK, "archive Notes")
	if data := once(audit.ActionPageArchived, me, notes); !strings.Contains(data, `"pages": 1`) {
		t.Errorf("the archive's record reads %s", data)
	}
	want(t, owner.delete(t, pagePath(notes, "/archive")), http.StatusOK, "unarchive Notes")
	once(audit.ActionPageUnarchived, me, notes)
	want(t, owner.put(t, "/api/v1/spaces/AUD/archive", nil), http.StatusOK, "archive the space")
	once(audit.ActionSpaceArchived, me, spaceID)
	want(t, owner.delete(t, "/api/v1/spaces/AUD/archive"), http.StatusOK, "unarchive the space")
	once(audit.ActionSpaceUnarchived, me, spaceID)

	want(t, owner.delete(t, "/api/v1/spaces/AUD"), http.StatusNoContent, "delete the space")
	once(audit.ActionSpaceDeleted, me, spaceID)

	// What a sign-in does by itself is held to once by the group roles suite,
	// which signs people in through the provider.
	covered[audit.ActionMemberJoined] = true
	covered[audit.ActionMemberRoleChanged] = true
	for _, action := range audit.Actions {
		if !covered[action] {
			t.Errorf("%s is never done here, so nothing holds it to one entry", action)
		}
	}
}

// Only the organization's administrators read the record, over the API and
// straight through SQL, and nobody rewrites or deletes an entry.
func TestTheAuditLogIsTheAdministratorsAndOnlyEverAddedTo(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "audit-read")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	memberID := h.addPerson(t, home.org, "member")
	member := api.as(t, memberID, home.org, slug)
	adminID := h.addPerson(t, home.org, "admin")
	admin := api.as(t, adminID, home.org, slug)
	nobody := api.anonymous()
	want(t, owner.post(t, "/api/v1/spaces", map[string]any{"key": "RD", "name": "Read"}), http.StatusCreated, "make a space")

	t.Run("administrators read and export it, members and strangers are refused", func(t *testing.T) {
		got := want(t, admin.get(t, "/api/v1/audit"), http.StatusOK, "an administrator reads the log")
		entries := list(t, got, "entries")
		if len(entries) != 1 || entries[0].(map[string]any)["action"] != audit.ActionSpaceCreated || entries[0].(map[string]any)["actorName"] == "" {
			t.Errorf("the log reads %s", got.Raw)
		}
		facets := obj(t, want(t, owner.get(t, "/api/v1/audit/facets"), http.StatusOK, "facets"), "facets")
		if number(facets["retentionDays"]) != 365 || len(facets["actions"].([]any)) != 1 || len(facets["actors"].([]any)) != 1 {
			t.Errorf("the facets read %v", facets)
		}
		for path, c := range map[string]*client{"/api/v1/audit": member, "/api/v1/audit/facets": member, "/api/v1/audit/export": member} {
			if code := errorCode(t, want(t, c.get(t, path), http.StatusForbidden, "a member reads "+path)); code != "forbidden" {
				t.Errorf("%s refuses a member with %s", path, code)
			}
			want(t, nobody.get(t, path), http.StatusUnauthorized, "nobody reads "+path)
		}
	})

	t.Run("straight through SQL the app role reads it only as an administrator", func(t *testing.T) {
		as := func(who uuid.UUID) context.Context {
			return db.WithUser(tenant.WithOrg(context.Background(), tenant.Org{ID: home.org}), who)
		}
		if n := h.count(t, as(home.user), `SELECT count(*) FROM audit_log`); n == 0 {
			t.Error("the owner reads nothing")
		}
		if n := h.count(t, as(memberID), `SELECT count(*) FROM audit_log`); n != 0 {
			t.Errorf("a member reads %d entries", n)
		}
		if n := h.count(t, tenant.WithOrg(context.Background(), tenant.Org{ID: home.org}), `SELECT count(*) FROM audit_log`); n != 0 {
			t.Errorf("a transaction acting for nobody reads %d entries", n)
		}
	})

	t.Run("nobody rewrites, deletes or forges an entry", func(t *testing.T) {
		ctx := db.WithUser(tenant.WithOrg(context.Background(), tenant.Org{ID: home.org}), home.user)
		denied := func(what string, err error) {
			t.Helper()
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "42501" {
				t.Errorf("%s = %v, want permission denied", what, err)
			}
		}
		for _, sql := range []string{`UPDATE audit_log SET action = 'nothing'`, `DELETE FROM audit_log`, `SELECT audit_log_prune(now() - interval '400 days')`} {
			_, err := h.cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
				_, err := tx.Exec(ctx, sql)
				return err
			})
			denied(sql+" as the app role", err)
			_, err = h.cluster.WriteAdmin(ctx, func(ctx context.Context, tx db.DBTX) error {
				_, err := tx.Exec(ctx, sql)
				return err
			})
			if !strings.Contains(sql, "audit_log_prune") {
				denied(sql+" as the admin role", err)
			}
		}
		_, err := h.cluster.Write(ctx, func(ctx context.Context, tx db.DBTX) error {
			return audit.Write(ctx, tx, home.org, audit.Entry{Action: audit.ActionSpaceDeleted, TargetType: "space", Actor: memberID})
		})
		denied("an entry naming somebody else", err)
		_, err = h.cluster.WriteAdmin(context.Background(), func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `SELECT audit_log_prune(now() - interval '400 days')`)
			return err
		})
		denied("pruning outside an organization", err)
		_, err = h.cluster.WriteAdmin(tenant.WithOrg(context.Background(), tenant.Org{ID: home.org}), func(ctx context.Context, tx db.DBTX) error {
			_, err := tx.Exec(ctx, `SELECT audit_log_prune(now() - interval '23 hours')`)
			return err
		})
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "22023" {
			t.Errorf("pruning the last day = %v, want refused", err)
		}
		if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1`, home.org); n == 0 {
			t.Error("the record lost its entries")
		}
	})
}

// An entry is written in the transaction of its act: a rolled back act leaves
// none, a committed one always has one.
func TestAnEntryCommitsAndRollsBackWithItsAct(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "audit-tx")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	marker := uuid.New()
	refused := errors.New("the act failed after its entry was written")
	_, err := h.cluster.Write(home.ctx, func(ctx context.Context, tx db.DBTX) error {
		if err := audit.Write(ctx, tx, home.org, audit.Entry{Action: audit.ActionSpaceCreated, TargetType: "space", TargetID: &marker, Actor: home.user}); err != nil {
			return err
		}
		return refused
	})
	if !errors.Is(err, refused) {
		t.Fatalf("the act = %v", err)
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE target_id = $1`, marker); n != 0 {
		t.Errorf("a rolled back act left %d entries", n)
	}
	if _, err := h.cluster.Write(home.ctx, func(ctx context.Context, tx db.DBTX) error {
		return audit.Write(ctx, tx, home.org, audit.Entry{Action: audit.ActionSpaceCreated, TargetType: "space", TargetID: &marker, Actor: home.user})
	}); err != nil {
		t.Fatal(err)
	}
	h.recordedOnce(t, home.org, audit.ActionSpaceCreated, &home.user, marker)

	// A refusal the database makes after the act began leaves nothing either.
	themeID := idOf(t, want(t, owner.post(t, "/api/v1/themes", map[string]any{"name": "Private", "spec": map[string]any{"colors": map[string]any{"light": map[string]string{"accent": "#336699"}}}}), http.StatusCreated, "make a theme"), "theme")
	if got := owner.put(t, "/api/v1/themes/default", map[string]any{"themeId": themeID}); got.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a private theme became the default: %d %s", got.Status, got.Raw)
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = $2`, home.org, audit.ActionThemeDefaultSet); n != 0 {
		t.Errorf("a refused default left %d entries", n)
	}
}

// plantEntry writes an entry behind the policies' back, at a time of the
// test's choosing.
func (h *harness) plantEntry(t *testing.T, org uuid.UUID, action string, actor *uuid.UUID, targetType string, target *uuid.UUID, at time.Time) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.super.QueryRow(context.Background(), `
		INSERT INTO audit_log (org_id, actor_user_id, action, target_type, target_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, org, actor, action, targetType, target, at).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// actionsOf walks a filtered log a window of two at a time and lists the
// actions it met, newest first.
func actionsOf(t *testing.T, c *client, query string) []string {
	t.Helper()
	var out []string
	cursor := ""
	for range 50 {
		r := want(t, c.get(t, "/api/v1/audit?limit=2&"+query+"&cursor="+url.QueryEscape(cursor)), http.StatusOK, "a window of the log")
		for _, e := range list(t, r, "entries") {
			out = append(out, e.(map[string]any)["action"].(string))
		}
		next, _ := r.Body["next"].(string)
		if next == "" {
			return out
		}
		cursor = next
	}
	t.Fatal("the walk through the log did not end")
	return nil
}

// The log is read newest first a window at a time, and narrowed by action,
// actor, target and time; the export holds what the filter holds.
func TestTheAuditLogIsWalkedAndNarrowed(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "audit-walk")
	owner := api.as(t, home.user, home.org, h.slugOf(t, home.org))
	annID := h.namedPerson(t, home.org, "Ann Walker")
	space, page := uuid.New(), uuid.New()
	day := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -10)
	at := func(days int) time.Time { return day.AddDate(0, 0, days).Add(12 * time.Hour) }
	h.plantEntry(t, home.org, audit.ActionSpaceCreated, &home.user, "space", &space, at(0))
	h.plantEntry(t, home.org, audit.ActionSpaceUpdated, &annID, "space", &space, at(1))
	h.plantEntry(t, home.org, audit.ActionPageExported, &annID, "page", &page, at(2))
	// Two at one instant: the id keeps them in order across a window's edge.
	h.plantEntry(t, home.org, audit.ActionOrgPermissionSet, &home.user, "org", nil, at(3))
	h.plantEntry(t, home.org, audit.ActionPageRestrictionsSet, &home.user, "page", &page, at(3))
	h.plantEntry(t, home.org, audit.ActionMemberJoined, nil, "user", &annID, at(4))
	h.settle(t)

	all := actionsOf(t, owner, "")
	if len(all) != 6 || all[0] != audit.ActionMemberJoined || all[5] != audit.ActionSpaceCreated {
		t.Errorf("the whole log reads %v", all)
	}
	sameList(t, "by action", actionsOf(t, owner, "action="+audit.ActionSpaceUpdated), audit.ActionSpaceUpdated)
	sameList(t, "by actor", actionsOf(t, owner, "actor="+annID.String()), audit.ActionPageExported, audit.ActionSpaceUpdated)
	sameList(t, "by target", actionsOf(t, owner, "target="+page.String()), audit.ActionPageRestrictionsSet, audit.ActionPageExported)
	sameList(t, "by kind of target", actionsOf(t, owner, "targetType=space"), audit.ActionSpaceUpdated, audit.ActionSpaceCreated)
	sameList(t, "by days", actionsOf(t, owner, "from="+at(1).Format("2006-01-02")+"&to="+at(2).Format("2006-01-02")), audit.ActionPageExported, audit.ActionSpaceUpdated)
	sameList(t, "by instants", actionsOf(t, owner, "from="+url.QueryEscape(at(2).Format(time.RFC3339))+"&to="+url.QueryEscape(at(3).Format(time.RFC3339))), audit.ActionPageExported)

	entry := list(t, want(t, owner.get(t, "/api/v1/audit?action="+audit.ActionMemberJoined), http.StatusOK, "the system's entry"), "entries")[0].(map[string]any)
	if entry["actorId"] != nil || entry["actorName"] != "" || entry["targetId"] != annID.String() {
		t.Errorf("an entry nobody made reads %v", entry)
	}
	for query, field := range map[string]string{"action=page.edited": "action", "actor=ann": "actor", "from=2026-13-01": "from", "cursor=nonsense": "cursor"} {
		if got := want(t, owner.get(t, "/api/v1/audit?"+query), http.StatusUnprocessableEntity, query); got.Body["error"].(map[string]any)["fields"].(map[string]any)[field] == nil {
			t.Errorf("%s does not name %s: %s", query, field, got.Raw)
		}
	}
	if got := owner.get(t, "/api/v1/audit/export?to=2026-01-01&from=2026-02-01"); got.Status != http.StatusUnprocessableEntity {
		t.Errorf("an export of a range ending before it starts = %d", got.Status)
	}

	resp, body := owner.download(t, "/api/v1/audit/export?actor="+annID.String())
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") || !strings.Contains(resp.Header.Get("Content-Disposition"), "audit-log.csv") {
		t.Fatalf("the export answered %s %v", resp.Status, resp.Header)
	}
	lines, err := csv.NewReader(bytes.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 || strings.Join(lines[0], ",") != strings.Join(audit.CSVHeader, ",") || lines[1][1] != audit.ActionPageExported || lines[1][3] != "Ann Walker" || lines[2][5] != space.String() {
		t.Errorf("the export reads %q", lines)
	}
}

// Retention deletes what is past its window, in every organization, and
// nothing younger.
func TestRetentionTakesOnlyTheOld(t *testing.T) {
	h := newHarness(t)
	a, b := h.makeMember(t, "audit-keep-a"), h.makeMember(t, "audit-keep-b")
	now := time.Now()
	old := h.plantEntry(t, a.org, audit.ActionSpaceCreated, &a.user, "space", nil, now.AddDate(0, 0, -40))
	oldB := h.plantEntry(t, b.org, audit.ActionSpaceCreated, &b.user, "space", nil, now.AddDate(0, 0, -31))
	young := h.plantEntry(t, a.org, audit.ActionSpaceUpdated, &a.user, "space", nil, now.AddDate(0, 0, -29))
	fresh := h.plantEntry(t, b.org, audit.ActionSpaceUpdated, &b.user, "space", nil, now)

	n, err := audit.NewRetention(h.cluster, 30*24*time.Hour, discard()).Once(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Errorf("retention took %d entries, want at least the two old ones", n)
	}
	for id, kept := range map[uuid.UUID]bool{old: false, oldB: false, young: true, fresh: true} {
		if got := h.countRows(t, `SELECT count(*) FROM audit_log WHERE id = $1`, id) == 1; got != kept {
			t.Errorf("entry %s kept %v, want %v", id, got, kept)
		}
	}
	if n, err := audit.NewRetention(h.cluster, 0, discard()).Once(context.Background()); n != 0 || err != nil {
		t.Errorf("keeping forever took %d entries, %v", n, err)
	}
}
