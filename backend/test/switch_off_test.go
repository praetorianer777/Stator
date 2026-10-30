//go:build integration

package test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

// Users under Admin (#584): an administrator switches an account off and back
// on; switched off, the person's session stops at the next request. Nobody
// else may, nobody switches themselves off, and straight through SQL the
// account policies refuse the flag to everybody but the service.
func TestAnAdministratorSwitchesAnAccountOffAndBackOn(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "switch-off")
	owner := api.as(t, home.user)
	annID, bobID := h.addPerson(t, "member"), h.addPerson(t, "member")
	ann, bob := api.as(t, annID), api.as(t, bobID)
	active := func() bool {
		t.Helper()
		var on bool
		if err := h.super.QueryRow(context.Background(), `SELECT is_active FROM app_user WHERE id = $1`, annID).Scan(&on); err != nil {
			t.Fatal(err)
		}
		return on
	}
	t.Cleanup(func() { h.cleanupExec(t, h.super, `UPDATE app_user SET is_active = true WHERE id = $1`, annID) })

	want(t, ann.get(t, "/api/v1/auth/me"), http.StatusOK, "ann before")
	want(t, bob.patch(t, "/api/v1/users/"+annID.String(), map[string]any{"active": false}), http.StatusForbidden, "a member switching somebody off")
	conn := appConn(t)
	actAs(t, conn, bobID)
	denied(t, conn, "a member switching somebody off", `UPDATE app_user SET is_active = false WHERE id = $1`, annID)
	actAs(t, conn, home.user)
	denied(t, conn, "an administrator switching somebody off past the service", `UPDATE app_user SET is_active = false WHERE id = $1`, annID)
	if !active() {
		t.Fatal("ann was switched off past the service")
	}

	want(t, owner.patch(t, "/api/v1/users/"+annID.String(), map[string]any{"active": false}), http.StatusNoContent, "switch ann off")
	if active() {
		t.Fatal("ann is still switched on")
	}
	want(t, ann.get(t, "/api/v1/auth/me"), http.StatusForbidden, "ann's session once switched off")
	members := list(t, want(t, owner.get(t, "/api/v1/users"), http.StatusOK, "the members"), "members")
	for _, m := range members {
		if each := m.(map[string]any); each["userId"] == annID.String() && each["active"] != false {
			t.Errorf("the members list shows ann switched on: %v", each)
		}
	}

	want(t, owner.patch(t, "/api/v1/users/"+annID.String(), map[string]any{"active": true}), http.StatusNoContent, "switch ann back on")
	want(t, ann.get(t, "/api/v1/auth/me"), http.StatusOK, "ann once back on")

	want(t, owner.patch(t, "/api/v1/users/"+home.user.String(), map[string]any{"active": false}), http.StatusConflict, "switching oneself off")
	want(t, owner.patch(t, "/api/v1/users/"+uuid.NewString(), map[string]any{"active": false}), http.StatusNotFound, "somebody not a member")
}
