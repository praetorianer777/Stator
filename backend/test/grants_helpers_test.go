//go:build integration

package test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/google/uuid"
)

// everyoneGroup is the id of the built-in group that holds every member.
func (h *harness) everyoneGroup(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.super.QueryRow(context.Background(), `SELECT id FROM groups WHERE builtin = 'everyone'`).Scan(&id); err != nil {
		t.Fatalf("find Everyone: %v", err)
	}
	return id
}

// roleFor is the role that holds just these space permissions, as the
// migration and a space's import find or make it.
func (h *harness) roleFor(t *testing.T, permissions []string) string {
	t.Helper()
	var key string
	if err := h.super.QueryRow(context.Background(), `SELECT role_for_space_permissions($1::text[])`, permissions).Scan(&key); err != nil {
		t.Fatalf("find the role for %v: %v", permissions, err)
	}
	return key
}

type tableGrant struct{ role, user, group string }

// subjectOf reads a subject as the tests write it: a user, a group, or
// everyone, which is the Everyone group.
func (c *client) subjectOf(t *testing.T, subject any) (user, group string) {
	t.Helper()
	s, _ := subject.(map[string]any)
	id := fmt.Sprint(s["id"])
	switch s["type"] {
	case "everyone":
		return "", c.api.h.everyoneGroup(t).String()
	case "group":
		return "", id
	}
	return id, ""
}

// setTable makes a space's grants the table given, rows of a subject and the
// permissions it held in the grids of before, through the API as the caller:
// each row is granted the role that holds just those permissions, and every
// other grant in the space is taken away, new ones first and the caller's own
// last. It answers the first refusal, or the grants it made as "grants".
func (c *client) setTable(t *testing.T, key string, rows []any) response {
	t.Helper()
	var wanted []tableGrant
	for _, r := range rows {
		row, _ := r.(map[string]any)
		var perms []string
		switch p := row["permissions"].(type) {
		case []string:
			perms = p
		case []any:
			for _, each := range p {
				perms = append(perms, fmt.Sprint(each))
			}
		}
		g := tableGrant{role: c.api.h.roleFor(t, perms)}
		g.user, g.group = c.subjectOf(t, row["subject"])
		wanted = append(wanted, g)
	}
	have := c.get(t, "/api/v1/role-assignments?limit=200&space="+url.QueryEscape(key))
	if have.Status != http.StatusOK {
		return have
	}
	var kept []string
	made := []any{}
	for _, g := range wanted {
		body := map[string]any{"role": g.role, "spaceKey": key}
		if g.user != "" {
			body["userId"] = g.user
		} else {
			body["groupId"] = g.group
		}
		got := c.post(t, "/api/v1/role-assignments", body)
		if got.Status != http.StatusCreated {
			return got
		}
		made = append(made, obj(t, got, "assignment"))
		kept = append(kept, obj(t, got, "assignment")["id"].(string))
	}
	var gone, own []string
	for _, a := range list(t, have, "assignments") {
		row := a.(map[string]any)
		id := row["id"].(string)
		switch {
		case slices.Contains(kept, id):
		case row["userId"] == c.user.String():
			own = append(own, id)
		default:
			gone = append(gone, id)
		}
	}
	for _, id := range append(gone, own...) {
		if got := c.delete(t, "/api/v1/role-assignments/"+id); got.Status != http.StatusNoContent {
			return got
		}
	}
	return response{Status: http.StatusOK, Body: map[string]any{"grants": made}}
}

// setAcross makes who holds a permission of the installation of before the
// subjects given: use is the member role, createSpace the space creator's.
func (c *client) setAcross(t *testing.T, permission string, subjects []any) response {
	t.Helper()
	role := map[string]string{"use": "member", "createSpace": "space_creator"}[permission]
	if role == "" {
		t.Fatalf("no role holds %s alone", permission)
	}
	have := c.get(t, "/api/v1/role-assignments?limit=200&scope=installation&role="+role)
	if have.Status != http.StatusOK {
		return have
	}
	var kept []string
	for _, s := range subjects {
		body := map[string]any{"role": role}
		if user, group := c.subjectOf(t, s); user != "" {
			body["userId"] = user
		} else {
			body["groupId"] = group
		}
		got := c.post(t, "/api/v1/role-assignments", body)
		if got.Status != http.StatusCreated {
			return got
		}
		kept = append(kept, obj(t, got, "assignment")["id"].(string))
	}
	for _, a := range list(t, have, "assignments") {
		id := a.(map[string]any)["id"].(string)
		if !slices.Contains(kept, id) {
			if got := c.delete(t, "/api/v1/role-assignments/"+id); got.Status != http.StatusNoContent {
				return got
			}
		}
	}
	return c.get(t, "/api/v1/role-assignments?limit=200&scope=installation&role="+role)
}
