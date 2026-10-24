//go:build integration

package test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// The avatar menu's "Switch to" group (#494): every member, guests included,
// learns where each connected tool opens, and a removed connection is gone.
func TestEveryMemberLearnsWhereTheConnectedToolsOpen(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "tools")
	owner := api.as(t, home.user)
	member := api.as(t, h.addPerson(t, "member"))

	newTree(t, owner, "TOOL", "Tool work")
	email := fmt.Sprintf("tilda-%s@example.test", uuid.NewString()[:8])
	t.Cleanup(func() { h.cleanupExec(t, h.super, `DELETE FROM app_user WHERE email = $1`, email) })
	made := obj(t, want(t, owner.post(t, "/api/v1/spaces/TOOL/guests", map[string]any{"email": email, "role": "viewer"}), http.StatusCreated, "invite a guest"), "guest")
	h.settle(t)
	guest := api.as(t, uuid.MustParse(made["userId"].(string)))

	tools := func(c *client, what string) map[string]string {
		t.Helper()
		out := map[string]string{}
		for _, each := range list(t, want(t, c.get(t, "/api/v1/connected-tools"), http.StatusOK, what), "tools") {
			tool := each.(map[string]any)
			out[tool["product"].(string)] = tool["url"].(string)
		}
		return out
	}

	if got := tools(member, "no tool connected yet"); len(got) != 0 {
		t.Errorf("before any connection the tools are %v", got)
	}
	want(t, api.anonymous().get(t, "/api/v1/connected-tools"), http.StatusUnauthorized, "an anonymous caller asks")

	want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": armatureURL(t)}), http.StatusOK, "connect Armature")
	if got := tools(member, "Armature connected"); len(got) != 1 || got["armature"] != armatureURL(t) {
		t.Errorf("with Armature connected the tools are %v", got)
	}

	want(t, owner.put(t, "/api/v1/exciter/connection", map[string]any{"baseUrl": exciterURL(t)}), http.StatusOK, "connect Exciter")
	for name, c := range map[string]*client{"the owner": owner, "a member": member, "a guest": guest} {
		if got := tools(c, name+" with both connected"); len(got) != 2 || got["armature"] != armatureURL(t) || got["exciter"] != exciterURL(t) {
			t.Errorf("%s sees the tools %v", name, got)
		}
	}

	want(t, owner.delete(t, "/api/v1/armature/connection"), http.StatusNoContent, "disconnect Armature")
	if got := tools(guest, "Armature removed"); len(got) != 1 || got["exciter"] != exciterURL(t) {
		t.Errorf("with Armature removed the tools are %v", got)
	}
}
