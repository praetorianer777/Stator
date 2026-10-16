//go:build integration

package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/armature"
)

// Capabilities (#409): Stator reads what the connected Armature offers at
// /meta and turns off what it lacks, saying so, instead of failing later.

// olderArmature tells the stub to answer /meta, reached as h's older
// Armature, with capabilities, or with none and a 404 for absent.
func olderArmature(t *testing.T, h *harness, capabilities []string, absent bool) {
	t.Helper()
	host := strings.TrimPrefix(olderArmatureURL(h), "http://")
	metaControl(t, http.MethodPut, host, map[string]any{"capabilities": capabilities, "absent": absent})
	t.Cleanup(func() { metaControl(t, http.MethodDelete, host, nil) })
}

func metaControl(t *testing.T, method, host string, body any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(method, os.Getenv(envArmatureStub)+"/_stub-meta/"+url.PathEscape(host), bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("the stub answered %s %s with %d", method, host, resp.StatusCode)
	}
}

func missingOf(t *testing.T, m map[string]any) []string {
	t.Helper()
	var out []string
	for _, each := range m["missing"].([]any) {
		f := each.(map[string]any)
		out = append(out, fmt.Sprint(f["feature"], ":", f["capability"]))
	}
	return out
}

func TestWhatArmatureOffersDecidesWhatPagesShow(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "arm-capabilities")
	slug := h.site.Slug
	owner := api.as(t, home.user)
	bob := api.as(t, h.addPerson(t, "member"))
	base := olderArmatureURL(h)

	withoutReports := []string{"calendar@1", "issue-embed@1", "plan@1", "remote-links@1", "theme-export@1", "webhooks@1"}
	olderArmature(t, h, withoutReports, false)

	conn := obj(t, want(t, owner.put(t, "/api/v1/armature/connection", map[string]any{"baseUrl": base}), http.StatusOK, "connect an older Armature"), "connection")
	partner := conn["partner"].(map[string]any)
	if partner["status"] != "ok" || partner["meta"] != true || partner["version"] != "0.0.0+stub" {
		t.Errorf("the older Armature is %v", partner)
	}
	if got := missingOf(t, partner); !slices.Equal(got, []string{"charts:reports@1"}) {
		t.Errorf("connecting says %v is missing, want charts alone", got)
	}
	want(t, owner.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "admin")}), http.StatusOK, "the owner connects")
	want(t, bob.put(t, "/api/v1/armature/account/token", map[string]any{"token": patFor(slug, "alice")}), http.StatusOK, "bob connects")

	account := obj(t, want(t, bob.get(t, "/api/v1/armature/account"), http.StatusOK, "bob's account"), "account")
	if got := missingOf(t, account); !slices.Equal(got, []string{"charts:reports@1"}) {
		t.Errorf("bob's account says %v is missing", got)
	}
	chart := chartPath("CP", "project = CP", "pie", "statusCategory", 0)
	if r := want(t, bob.get(t, chart), http.StatusOK, "a chart"); r.Body["status"] != string(armature.StatusOutdated) || r.Body["chart"] != nil {
		t.Errorf("a chart without reports@1 is %s", r.Raw)
	}
	if r := want(t, bob.get(t, "/api/v1/armature/issues?key=CP-1"), http.StatusOK, "a chip"); r.Body["status"] != "ok" {
		t.Errorf("a chip, which issue-embed@1 is there for, is %s", r.Raw)
	}

	t.Run("an Armature updated since shows on the settings page at once, and the chart with it", func(t *testing.T) {
		metaControl(t, http.MethodDelete, strings.TrimPrefix(base, "http://"), nil)
		conn := obj(t, want(t, owner.get(t, "/api/v1/armature/connection"), http.StatusOK, "the settings page"), "connection")
		if got := missingOf(t, conn["partner"].(map[string]any)); len(got) != 0 {
			t.Errorf("after the update %v is still missing", got)
		}
		if r := want(t, bob.get(t, chart), http.StatusOK, "a chart"); r.Body["status"] != "ok" {
			t.Errorf("the chart after the update is %s", r.Raw)
		}
	})

	t.Run("without theme-export@1 following the theme is refused in a sentence", func(t *testing.T) {
		olderArmature(t, h, []string{"issue-embed@1"}, false)
		want(t, owner.get(t, "/api/v1/armature/connection"), http.StatusOK, "the settings page asks again")
		r := want(t, owner.put(t, "/api/v1/armature/theme", nil), http.StatusConflict, "follow the theme")
		if code := errorCode(t, r); code != "armature_outdated" {
			t.Errorf("code %s", code)
		}
		if msg := obj(t, r, "error")["message"].(string); !strings.Contains(msg, "theme-export@1") || !strings.Contains(msg, "update Armature") {
			t.Errorf("the refusal says %q", msg)
		}
		if f := obj(t, want(t, owner.get(t, "/api/v1/armature/theme"), http.StatusOK, "the follow"), "follow"); f["status"] != "outdated" {
			t.Errorf("the follow is %v", f)
		}
	})

	t.Run("an Armature from before /meta offers nothing, and says so", func(t *testing.T) {
		olderArmature(t, h, nil, true)
		conn := obj(t, want(t, owner.get(t, "/api/v1/armature/connection"), http.StatusOK, "the settings page"), "connection")
		partner := conn["partner"].(map[string]any)
		if partner["status"] != "ok" || partner["meta"] != false || partner["version"] != nil || len(missingOf(t, partner)) != len(armature.Features) {
			t.Errorf("an Armature without /meta is %v", partner)
		}
		if r := want(t, bob.get(t, "/api/v1/armature/issues?key=CP-1"), http.StatusOK, "a chip"); r.Body["status"] != "outdated" {
			t.Errorf("a chip from an Armature without /meta is %s", r.Raw)
		}
		if r := want(t, bob.get(t, "/api/v1/armature/find?words=onboarding"), http.StatusOK, "a find"); r.Body["status"] != "outdated" || len(list(t, r, "issues")) != 0 {
			t.Errorf("a find in an Armature without /meta is %s", r.Raw)
		}
		docs := newTree(t, owner, "CAP", "Capabilities")
		page := docs.add(docs.homeID, "Filed from here")
		errorCodeIs(t, want(t, owner.post(t, "/api/v1/armature/issues", createBody(page, "CP", "Not filed")), http.StatusConflict, "create an issue"), "armature_outdated")
		resp, err := http.Get(os.Getenv(envArmatureStub) + "/_stub/" + slug + "/issues/CP-6")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("an issue was filed in an Armature that cannot embed it: %d", resp.StatusCode)
		}
	})
}
