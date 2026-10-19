//go:build integration

package test

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
)

// Checking an address before it is saved (#429): what answers there, or the
// sentence saying what to do, and nothing stored either way.
func TestAnAdministratorChecksAnArmatureAddressBeforeSaving(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	owner := api.as(t, h.makeMember(t, "arm-check").user)
	bob := api.as(t, h.addPerson(t, "member"))
	base := armatureURL(t)
	const check = "/api/v1/armature/connection/check"

	found := obj(t, want(t, owner.post(t, check, map[string]any{"baseUrl": " " + base + "/ "}), http.StatusOK, "check the stub"), "check")
	partner := found["partner"].(map[string]any)
	if found["baseUrl"] != base || partner["status"] != "ok" || partner["meta"] != true || partner["version"] != "0.0.0+stub" {
		t.Errorf("the check found %v", found)
	}

	olderArmature(t, h, nil, true)
	older := obj(t, want(t, owner.post(t, check, map[string]any{"baseUrl": olderArmatureURL(h)}), http.StatusOK, "check an Armature older than /meta"), "check")
	if p := older["partner"].(map[string]any); p["meta"] != false || p["version"] != nil {
		t.Errorf("the older Armature is %v", p)
	}

	valkeyAddrs, err := net.DefaultResolver.LookupHost(context.Background(), "valkey")
	if err != nil || len(valkeyAddrs) == 0 {
		t.Fatalf("valkey does not resolve on the stack's network: %v", err)
	}
	for _, tc := range []struct {
		name, baseURL string
		says          []string
		never         []string
	}{
		{"a name that resolves to a private address", "http://valkey:6379",
			[]string{"add valkey to network.outboundAllow in the chart values (STATOR_OUTBOUND_ALLOW)", "resolves to a private address"},
			append([]string{"6379"}, valkeyAddrs...)},
		{"a private address typed as such", "http://10.11.12.13",
			[]string{"Stator may not reach 10.11.12.13", "add 10.11.12.13 to network.outboundAllow"}, nil},
		{"an address where nothing answers", "http://" + armatureStubHost + ":1", []string{"did not answer"}, nil},
		{"an address with a path", base + "/api/v1", []string{"without a path"}, nil},
	} {
		msg := fieldError(t, want(t, owner.post(t, check, map[string]any{"baseUrl": tc.baseURL}), http.StatusUnprocessableEntity, tc.name), "baseUrl")
		for _, s := range tc.says {
			if !strings.Contains(msg, s) {
				t.Errorf("%s: %q does not say %q", tc.name, msg, s)
			}
		}
		for _, s := range tc.never {
			if strings.Contains(msg, s) {
				t.Errorf("%s: %q tells %q", tc.name, msg, s)
			}
		}
	}

	errorCode(t, want(t, bob.post(t, check, map[string]any{"baseUrl": base}), http.StatusForbidden, "a member checks an address"))

	if got := want(t, owner.get(t, "/api/v1/armature/connection"), http.StatusOK, "after the checks"); got.Body["connection"] != nil {
		t.Errorf("a check stored a connection: %s", got.Raw)
	}
	var rows int
	if err := h.super.QueryRow(context.Background(), `SELECT count(*) FROM armature_connection`).Scan(&rows); err != nil || rows != 0 {
		t.Errorf("a check left %d rows in armature_connection: %v", rows, err)
	}
}
