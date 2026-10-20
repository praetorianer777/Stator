//go:build integration

package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/exciter"
	"github.com/praetorianer777/stator/backend/internal/netguard"
	"github.com/praetorianer777/stator/backend/internal/secret"
)

// Exciter requirements on pages (#440) against the exciter-stub, which a
// contract test holds to Exciter's own document, as the armature-stub is
// held to Armature's.

const (
	// envExciter is the stub as a browser opens it, which is the base URL an
	// administrator enters; envExciterStub is where this process reaches it.
	envExciter        = "STATOR_TEST_EXCITER_URL"
	envExciterStub    = "STATOR_TEST_EXCITER_STUB_URL"
	exciterStubHost   = "exciter-stub"
	exciterHookSecret = "exciter_whs_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
)

func exciterURL(t *testing.T) string {
	t.Helper()
	if os.Getenv(envExciter) == "" || os.Getenv(envExciterStub) == "" {
		t.Fatalf("%s is not set; run the suite with make test-integration against the running stack", envExciter)
	}
	return os.Getenv(envExciter)
}

// exciter is the service as cmd/api builds it, over the stack's stub.
func (h *harness) exciter(t *testing.T) *exciter.Service {
	t.Helper()
	box, err := secret.New(testSecretKey(t))
	if err != nil {
		t.Fatal(err)
	}
	allow := netguard.ParseAllow(exciterStubHost + "," + armatureStubHost)
	client := armature.NewClient(allow, map[string]string{exciterURL(t): os.Getenv(envExciterStub)})
	return exciter.NewService(h.cluster, box, client, exciter.NewCache(h.valkey(t), discard()),
		exciter.Options{AppURL: armatureAppURL, Allow: allow, Development: true, Log: discard()})
}

// exciterTenant is a stub tenant of the test's own, so no other test's
// changes to the stub's requirements reach it; it is forgotten afterwards.
func exciterTenant(t *testing.T) string {
	t.Helper()
	tenant := "it-" + strings.ReplaceAll(uuid.NewString()[:13], "-", "")
	t.Cleanup(func() { exciterStub(t, http.MethodDelete, tenant, nil) })
	return tenant
}

func exciterPat(tenant, person string) string { return exciter.TokenPrefix + tenant + "_" + person }

// exciterStub changes the stub's world for a tenant, as the tests' own hand.
func exciterStub(t *testing.T, method, path string, body any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(method, os.Getenv(envExciterStub)+"/_stub/"+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("the stub answered %s %s with %d", method, path, resp.StatusCode)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

func requirementOf(t *testing.T, r response) map[string]any {
	t.Helper()
	found, _ := r.Body["requirement"].(map[string]any)
	return found
}

func TestAnAdministratorConnectsExciterAndMembersTheirTokens(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "exc-connect")
	owner := api.as(t, home.user)
	bobID := h.addPerson(t, "member")
	bob := api.as(t, bobID)
	base := exciterURL(t)
	tenant := exciterTenant(t)

	if got := want(t, owner.get(t, "/api/v1/exciter/connection"), http.StatusOK, "no connection yet"); got.Body["connection"] != nil {
		t.Errorf("a new instance has an Exciter connection: %s", got.Raw)
	}
	errorCode(t, want(t, bob.get(t, "/api/v1/exciter/connection"), http.StatusForbidden, "a member reads the connection"))
	account := obj(t, want(t, bob.get(t, "/api/v1/exciter/account"), http.StatusOK, "an account before any connection"), "account")
	if account["configured"] != false || account["status"] != "not_configured" {
		t.Errorf("before a connection the account is %v", account)
	}
	if code := errorCode(t, want(t, bob.put(t, "/api/v1/exciter/account/token", map[string]any{"token": exciterPat(tenant, "bob")}), http.StatusConflict, "a token before a connection")); code != "exciter_not_configured" {
		t.Errorf("code %s", code)
	}
	r := want(t, bob.get(t, "/api/v1/exciter/requirements?key=CHK-R1"), http.StatusOK, "a lookup before any connection")
	if r.Body["status"] != "not_configured" {
		t.Errorf("a lookup without Exciter answers %s", r.Raw)
	}

	for _, tc := range []struct{ name, baseURL, secret, field, says string }{
		{"an address that is no origin", base + "/api/v1", "", "baseUrl", "without a path"},
		{"a host the guard does not let through", "http://valkey:6379", "", "baseUrl", "STATOR_OUTBOUND_ALLOW"},
		{"an address inside the network", "http://10.11.12.13", "", "baseUrl", "Use Exciter's public address"},
		{"Armature's stub, which is no Exciter", "http://" + armatureStubHost + ":8080", "", "baseUrl", "it is not Exciter"},
		{"a secret Exciter never shows", base, "armature_whs_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG", "webhookSecret", exciter.WebhookSecretPrefix},
	} {
		body := map[string]any{"baseUrl": tc.baseURL}
		if tc.secret != "" {
			body["webhookSecret"] = tc.secret
		}
		if msg := fieldError(t, want(t, owner.put(t, "/api/v1/exciter/connection", body), http.StatusUnprocessableEntity, tc.name), tc.field); !strings.Contains(msg, tc.says) {
			t.Errorf("%s: %q does not say %q", tc.name, msg, tc.says)
		}
	}
	errorCode(t, want(t, bob.put(t, "/api/v1/exciter/connection", map[string]any{"baseUrl": base}), http.StatusForbidden, "a member connects Exciter"))

	saved := want(t, owner.put(t, "/api/v1/exciter/connection", map[string]any{"baseUrl": base + "/", "webhookSecret": exciterHookSecret}), http.StatusOK, "connect Exciter")
	conn := obj(t, saved, "connection")
	if conn["baseUrl"] != base || conn["webhookSecretSet"] != true || conn["webhookUrl"] != armatureAppURL+"/api/v1/exciter/webhook" {
		t.Errorf("the connection is %v", conn)
	}
	if topics, _ := conn["webhookTopics"].([]any); len(topics) != len(exciter.WebhookTopics) {
		t.Errorf("the webhook topics are %v", conn["webhookTopics"])
	}
	partner := obj(t, saved, "connection", "partner")
	if partner["status"] != "ok" || len(partner["missing"].([]any)) != 0 {
		t.Errorf("the stub's Exciter offers %v", partner)
	}
	if bytes.Contains(saved.Raw, []byte(exciterHookSecret)) {
		t.Error("the webhook secret was answered")
	}
	var sealed []byte
	if err := h.super.QueryRow(context.Background(), `SELECT webhook_secret FROM exciter_connection`).Scan(&sealed); err != nil || len(sealed) == 0 || bytes.Contains(sealed, []byte(exciterHookSecret)) {
		t.Errorf("the webhook secret is not sealed at rest: %v", err)
	}
	if n := h.countRows(t, `SELECT count(*) FROM audit_log WHERE action = 'exciter.connection_saved'`); n == 0 {
		t.Error("saving the connection was not audited")
	}

	for _, tc := range []struct{ name, token, says string }{
		{"an Armature token", "armature_pat_x", exciter.TokenPrefix},
		{"a token Exciter refuses", "exciter_pat_revoked", "Exciter did not accept this token."},
	} {
		if msg := fieldError(t, want(t, bob.put(t, "/api/v1/exciter/account/token", map[string]any{"token": tc.token}), http.StatusUnprocessableEntity, tc.name), "token"); !strings.Contains(msg, tc.says) {
			t.Errorf("%s: %q does not say %q", tc.name, msg, tc.says)
		}
	}
	stored := want(t, bob.put(t, "/api/v1/exciter/account/token", map[string]any{"token": exciterPat(tenant, "bob")}), http.StatusOK, "bob connects")
	account = obj(t, stored, "account")
	if account["connected"] != true || account["status"] != "ok" || obj(t, stored, "account", "user")["name"] != "Bob" {
		t.Errorf("after connecting the account is %v", account)
	}
	if bytes.Contains(stored.Raw, []byte(exciterPat(tenant, "bob"))) {
		t.Error("the token was answered")
	}
	var sealedToken []byte
	if err := h.super.QueryRow(context.Background(), `SELECT token FROM exciter_token WHERE user_id = $1`, bobID).Scan(&sealedToken); err != nil || bytes.Contains(sealedToken, []byte("_bob")) {
		t.Errorf("the token is readable at rest: %v", err)
	}
	checked := obj(t, want(t, bob.post(t, "/api/v1/exciter/account/check", nil), http.StatusOK, "bob checks"), "account")
	if checked["status"] != "ok" {
		t.Errorf("a check of a good token says %v", checked)
	}
	if checked["via"] != "token" {
		t.Errorf("a pasted token is connected via %v", checked["via"])
	}
	found := obj(t, want(t, owner.post(t, "/api/v1/exciter/connection/check", map[string]any{"baseUrl": base}), http.StatusOK, "check the address again"), "check")
	if found["baseUrl"] != base {
		t.Errorf("the check found %v", found)
	}
	want(t, bob.delete(t, "/api/v1/exciter/account/token"), http.StatusNoContent, "bob disconnects")
	if n := h.countRows(t, `SELECT count(*) FROM exciter_token WHERE user_id = $1`, bobID); n != 0 {
		t.Error("disconnecting kept bob's token")
	}
	want(t, bob.put(t, "/api/v1/exciter/account/token", map[string]any{"token": exciterPat(tenant, "bob")}), http.StatusOK, "bob connects again")

	// The partner app that connects people at sign-in (#462) is the administrators'.
	app := map[string]any{"clientId": "0199-stator", "clientSecret": exciter.ClientSecretPrefix + "abc"}
	errorCode(t, want(t, bob.put(t, "/api/v1/exciter/connection/app", app), http.StatusForbidden, "a member stores the app"))
	errorCode(t, want(t, bob.delete(t, "/api/v1/exciter/connection/app"), http.StatusForbidden, "a member forgets the app"))
	if msg := fieldError(t, want(t, owner.put(t, "/api/v1/exciter/connection/app", map[string]any{"clientId": "0199-stator", "clientSecret": "armature_ocs_x"}),
		http.StatusUnprocessableEntity, "an Armature secret"), "clientSecret"); !strings.Contains(msg, exciter.ClientSecretPrefix) {
		t.Errorf("a wrong secret is refused with %q", msg)
	}
	withApp := obj(t, want(t, owner.put(t, "/api/v1/exciter/connection/app", app), http.StatusOK, "store the app"), "connection", "app")
	if withApp["clientId"] != "0199-stator" || withApp["secretSet"] != true || withApp["exchanging"] != false {
		t.Errorf("the stored app is %v", withApp)
	}
	want(t, owner.delete(t, "/api/v1/exciter/connection/app"), http.StatusNoContent, "forget the app")

	// A new address forgets every stored token, so none is sent to another host.
	want(t, owner.put(t, "/api/v1/exciter/connection", map[string]any{"baseUrl": strings.Replace(base, "localhost", "127.0.0.1", 1)}), http.StatusUnprocessableEntity, "an address the guard refuses")
	if n := h.countRows(t, `SELECT count(*) FROM exciter_token`); n != 1 {
		t.Errorf("a refused address forgot the tokens: %d left", n)
	}
	want(t, owner.delete(t, "/api/v1/exciter/connection"), http.StatusNoContent, "disconnect Exciter")
	if n := h.countRows(t, `SELECT count(*) FROM exciter_token`); n != 0 {
		t.Errorf("disconnecting kept %d tokens", n)
	}
}

func TestChipsShowEachViewerTheRequirementsExciterShowsThem(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "exc-chips")
	owner := api.as(t, home.user)
	bobID := h.addPerson(t, "member")
	bob := api.as(t, bobID)
	carolID := h.addPerson(t, "member")
	carol := api.as(t, carolID)
	tenant := exciterTenant(t)
	want(t, owner.put(t, "/api/v1/exciter/connection", map[string]any{"baseUrl": exciterURL(t)}), http.StatusOK, "connect Exciter")
	t.Cleanup(func() { owner.delete(t, "/api/v1/exciter/connection") })
	want(t, owner.put(t, "/api/v1/exciter/account/token", map[string]any{"token": exciterPat(tenant, "alice")}), http.StatusOK, "alice connects")
	want(t, bob.put(t, "/api/v1/exciter/account/token", map[string]any{"token": exciterPat(tenant, "bob")}), http.StatusOK, "bob connects")
	h.settle(t)

	lookup := func(c *client) (string, map[string]map[string]any) {
		t.Helper()
		r := want(t, c.get(t, "/api/v1/exciter/requirements?key=chk-r1&key=CHK-R2&key=OLD-R7&key=CHK-R3&key=CHK-R1"), http.StatusOK, "look requirements up")
		found := map[string]map[string]any{}
		results, _ := r.Body["requirements"].([]any)
		for _, each := range results {
			result := each.(map[string]any)
			found[result["key"].(string)], _ = result["requirement"].(map[string]any)
		}
		status, _ := r.Body["status"].(string)
		return status, found
	}

	status, alice := lookup(owner)
	if status != "ok" || len(alice) != 4 {
		t.Fatalf("alice's lookup is %s %v", status, alice)
	}
	r1 := alice["CHK-R1"]
	if r1["title"] != "The basket keeps its items for 30 days" || r1["revision"] != float64(3) || r1["url"] != exciterURL(t)+"/p/CHK/r/CHK-R1" {
		t.Errorf("CHK-R1 reads %v", r1)
	}
	if st, _ := r1["status"].(map[string]any); st["name"] != "Approved" || st["category"] != "approved" {
		t.Errorf("CHK-R1's status is %v", r1["status"])
	}
	if moved := alice["OLD-R7"]; moved["key"] != "CHK-R4" || moved["url"] != exciterURL(t)+"/p/CHK/r/CHK-R4" {
		t.Errorf("a former key reads %v, want CHK-R4", moved)
	}
	if trashed := alice["CHK-R3"]; trashed == nil || trashed["trashedAt"] == nil {
		t.Errorf("a trashed requirement alice may still find reads %v", trashed)
	}

	_, bobs := lookup(bob)
	if bobs["CHK-R2"] != nil || bobs["CHK-R3"] != nil || bobs["CHK-R1"] == nil {
		t.Errorf("bob sees %v; Exciter hides CHK-R2 and the trashed CHK-R3 from him", bobs)
	}
	if one := requirementOf(t, want(t, bob.get(t, "/api/v1/exciter/requirements/CHK-R2"), http.StatusOK, "bob reads CHK-R2")); one != nil {
		t.Errorf("bob reads a requirement Exciter hides from him: %v", one)
	}
	if status, _ := lookup(carol); status != "not_connected" {
		t.Errorf("a member without a token is answered %s", status)
	}
	errorCode(t, want(t, owner.get(t, "/api/v1/exciter/requirements?key=CP-12"), http.StatusUnprocessableEntity, "an Armature issue key"))
	errorCode(t, want(t, owner.get(t, "/api/v1/exciter/requirements/CHK-R0"), http.StatusUnprocessableEntity, "a key numbered from zero"))

	// A change in Exciter reaches a reader within the cache's minute, not before.
	exciterStub(t, http.MethodPut, tenant+"/requirements/CHK-R1", map[string]any{"title": "The basket keeps its items for 90 days", "revision": 4})
	if got := requirementOf(t, want(t, owner.get(t, "/api/v1/exciter/requirements/CHK-R1"), http.StatusOK, "read CHK-R1 again")); got["revision"] != float64(3) {
		t.Errorf("within the cache's minute CHK-R1 reads %v", got)
	}
}

func rowKeys(t *testing.T, r response, name string) []string {
	t.Helper()
	var keys []string
	for _, each := range list(t, r, name) {
		keys = append(keys, each.(map[string]any)["key"].(string))
	}
	return keys
}

func TestBlocksPickersAndListsReadExciterAsTheirReader(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "exc-lists")
	owner := api.as(t, home.user)
	bobID := h.addPerson(t, "member")
	bob := api.as(t, bobID)
	carol := api.as(t, h.addPerson(t, "member"))
	tenant := exciterTenant(t)
	want(t, owner.put(t, "/api/v1/exciter/connection", map[string]any{"baseUrl": exciterURL(t), "webhookSecret": exciterHookSecret}), http.StatusOK, "connect Exciter")
	t.Cleanup(func() { owner.delete(t, "/api/v1/exciter/connection") })
	want(t, owner.put(t, "/api/v1/exciter/account/token", map[string]any{"token": exciterPat(tenant, "alice")}), http.StatusOK, "alice connects")
	want(t, bob.put(t, "/api/v1/exciter/account/token", map[string]any{"token": exciterPat(tenant, "bob")}), http.StatusOK, "bob connects")
	h.settle(t)

	t.Run("a block reads the statement and rationale, as Exciter wrote them", func(t *testing.T) {
		got := requirementOf(t, want(t, owner.get(t, "/api/v1/exciter/requirements/CHK-R1"), http.StatusOK, "read CHK-R1 for a block"))
		statement, _ := got["statement"].(map[string]any)
		rationale, _ := got["rationale"].(map[string]any)
		if statement["type"] != "doc" || rationale["type"] != "doc" || got["key"] != "CHK-R1" {
			t.Errorf("a block reads %v", got)
		}
		if plain := requirementOf(t, want(t, owner.get(t, "/api/v1/exciter/requirements/CHK-R4"), http.StatusOK, "read CHK-R4")); plain["rationale"] != nil {
			t.Errorf("a requirement without a rationale reads %v", plain["rationale"])
		}
		// The chips' lookup shares the answer without the documents.
		r := want(t, owner.get(t, "/api/v1/exciter/requirements?key=CHK-R1"), http.StatusOK, "look CHK-R1 up")
		if strings.Contains(string(r.Raw), "statement") {
			t.Errorf("a chip's lookup carries the statement: %s", r.Raw)
		}
	})

	t.Run("the picker finds by a word of the title and by a former key", func(t *testing.T) {
		if keys := rowKeys(t, want(t, owner.get(t, "/api/v1/exciter/search?q=basket"), http.StatusOK, "find basket"), "requirements"); !slices.Equal(keys, []string{"CHK-R1"}) {
			t.Errorf("basket finds %v", keys)
		}
		if keys := rowKeys(t, want(t, owner.get(t, "/api/v1/exciter/search?q=old-r7"), http.StatusOK, "find a former key"), "requirements"); !slices.Equal(keys, []string{"CHK-R4"}) {
			t.Errorf("a former key finds %v", keys)
		}
		if keys := rowKeys(t, want(t, bob.get(t, "/api/v1/exciter/search?q=session"), http.StatusOK, "bob looks for SEC-R1"), "requirements"); len(keys) != 0 {
			t.Errorf("bob finds %v, which Exciter hides from him", keys)
		}
		if msg := fieldError(t, want(t, owner.get(t, "/api/v1/exciter/search?q=%20"), http.StatusUnprocessableEntity, "look for nothing"), "q"); !strings.Contains(msg, "CHK-R42") {
			t.Errorf("looking for nothing says %q", msg)
		}
		if r := want(t, carol.get(t, "/api/v1/exciter/search?q=basket"), http.StatusOK, "carol looks without a token"); r.Body["status"] != "not_connected" {
			t.Errorf("a member without a token is answered %s", r.Raw)
		}
	})

	t.Run("each reader lists their own projects", func(t *testing.T) {
		if keys := rowKeys(t, want(t, owner.get(t, "/api/v1/exciter/projects"), http.StatusOK, "alice's projects"), "projects"); !slices.Equal(keys, []string{"CHK", "SEC"}) {
			t.Errorf("alice's projects are %v", keys)
		}
		if keys := rowKeys(t, want(t, bob.get(t, "/api/v1/exciter/projects"), http.StatusOK, "bob's projects"), "projects"); !slices.Equal(keys, []string{"CHK"}) {
			t.Errorf("bob's projects are %v", keys)
		}
	})

	mine := "/api/v1/exciter/projects/CHK/requirements?query=" + url.QueryEscape("owner = currentUser() ORDER BY key")
	t.Run("a list reads as its reader, currentUser() included", func(t *testing.T) {
		alice := want(t, owner.get(t, mine), http.StatusOK, "alice's list")
		if keys := rowKeys(t, alice, "requirements"); !slices.Equal(keys, []string{"CHK-R1", "CHK-R2"}) || alice.Body["total"] != float64(2) || alice.Body["found"] != true {
			t.Errorf("alice's list is %s", alice.Raw)
		}
		first := list(t, alice, "requirements")[0].(map[string]any)
		if first["url"] != exciterURL(t)+"/p/CHK/r/CHK-R1" || first["revision"] != float64(3) {
			t.Errorf("a row reads %v", first)
		}
		if keys := rowKeys(t, want(t, bob.get(t, mine), http.StatusOK, "bob's list"), "requirements"); !slices.Equal(keys, []string{"CHK-R4"}) {
			t.Errorf("bob's list is %v", keys)
		}
		window := want(t, owner.get(t, "/api/v1/exciter/projects/chk/requirements?limit=2&offset=2"), http.StatusOK, "a later window")
		if keys := rowKeys(t, window, "requirements"); !slices.Equal(keys, []string{"CHK-R3", "CHK-R4"}) || window.Body["total"] != float64(4) || window.Body["offset"] != float64(2) {
			t.Errorf("the second window is %s", window.Raw)
		}
		if unseen := want(t, bob.get(t, "/api/v1/exciter/projects/SEC/requirements"), http.StatusOK, "bob lists SEC"); unseen.Body["found"] != false || len(list(t, unseen, "requirements")) != 0 {
			t.Errorf("a project bob may not view lists %s", unseen.Raw)
		}
	})

	t.Run("a query that does not read is refused with where it went wrong", func(t *testing.T) {
		r := want(t, owner.get(t, "/api/v1/exciter/projects/CHK/requirements?query="+url.QueryEscape("status = Approved AND colour = red")), http.StatusUnprocessableEntity, "a query naming no field")
		failure, _ := r.Body["error"].(map[string]any)
		if failure["code"] != "bad_query" || failure["position"] != float64(23) || !strings.Contains(failure["message"].(string), "colour") {
			t.Errorf("a bad query is refused with %s", r.Raw)
		}
		for _, tc := range []struct {
			who         *client
			path, field string
		}{
			{owner, "/projects/CHK-R1/requirements", "project"},
			{owner, "/projects/CHK/requirements?limit=501", "limit"},
			{owner, "/projects/CHK/requirements?query=" + strings.Repeat("a", exciter.MaxQueryLength+1), "query"},
			{owner, "/projects/CHK/requirements/suggest?q=key&at=9", "at"},
			{owner, "/projects/CHK/requirements/suggest?q=key&at=-1", "at"},
			{bob, "/projects/SEC/requirements/suggest?q=key", "project"},
		} {
			fieldError(t, want(t, tc.who.get(t, "/api/v1/exciter"+tc.path), http.StatusUnprocessableEntity, tc.path), tc.field)
		}
	})

	t.Run("the editor completes at the caret", func(t *testing.T) {
		r := want(t, owner.get(t, "/api/v1/exciter/projects/CHK/requirements/suggest?q="+url.QueryEscape("status = Ap AND key = CHK-R1")+"&at=11"), http.StatusOK, "complete a status")
		s, _ := r.Body["suggestions"].(map[string]any)
		completion, _ := s["completion"].(map[string]any)
		words, _ := s["words"].([]any)
		if completion["slot"] != "value" || completion["from"] != float64(9) || completion["to"] != float64(11) || len(words) != 1 || words[0].(map[string]any)["text"] != "Approved" {
			t.Errorf("completing a status answers %s", r.Raw)
		}
	})

	t.Run("a webhook clears every list, as any change may change what a query finds", func(t *testing.T) {
		before := want(t, owner.get(t, mine), http.StatusOK, "alice's list")
		changed := exciterStub(t, http.MethodPut, tenant+"/requirements/CHK-R1", map[string]any{"title": "The basket keeps its items for 90 days", "revision": 4})["requirement"].(map[string]any)
		if again := want(t, owner.get(t, mine), http.StatusOK, "alice's list again"); string(again.Raw) != string(before.Raw) {
			t.Errorf("within the cache's minute the list changed: %s", again.Raw)
		}
		hook := exciterEnvelope(t, uuid.New(), exciter.WebhookSource, "requirement.revised", map[string]any{"requirement": map[string]any{"id": changed["id"], "key": "CHK-R1"}})
		if r := exciterDelivery(t, api, hook, exciterHookSecret, time.Now(), "1"); r.Status != http.StatusNoContent {
			t.Fatalf("a signed delivery is answered %d %s", r.Status, r.Raw)
		}
		after := want(t, owner.get(t, mine), http.StatusOK, "alice's list after the webhook")
		if first := list(t, after, "requirements")[0].(map[string]any); first["revision"] != float64(4) || first["title"] != "The basket keeps its items for 90 days" {
			t.Errorf("after the webhook the list reads %s", after.Raw)
		}
	})
}

// exciterDelivery is one signed post to the receiver, as Exciter's sender
// sends it: the envelope, and the headers over the raw body and its time.
func exciterDelivery(t *testing.T, api *apiServer, body []byte, secret string, sent time.Time, version string) response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, api.srv.URL+"/api/v1/exciter/webhook", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(exciter.SignatureHeader, armature.Sign(body, secret))
	if !sent.IsZero() {
		req.Header.Set(exciter.TimestampHeader, strconv.FormatInt(sent.Unix(), 10))
		req.Header.Set(exciter.TimestampedSignatureHeader, armature.SignTimestamped(body, secret, sent))
	}
	req.Header.Set(exciter.WebhookVersionHeader, version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := response{Status: resp.StatusCode, Raw: readAll(resp)}
	_ = json.Unmarshal(out.Raw, &out.Body)
	return out
}

func exciterEnvelope(t *testing.T, id uuid.UUID, source, topic string, payload any) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"version": 1, "id": id, "source": source, "topic": topic, "occurredAt": time.Now().UTC(), "payload": payload})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestExciterWebhooksClearTheCachedRequirementsTheyAnnounce(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "exc-hooks")
	owner := api.as(t, home.user)
	tenant := exciterTenant(t)
	want(t, owner.put(t, "/api/v1/exciter/connection", map[string]any{"baseUrl": exciterURL(t), "webhookSecret": exciterHookSecret}), http.StatusOK, "connect Exciter")
	t.Cleanup(func() { owner.delete(t, "/api/v1/exciter/connection") })
	want(t, owner.put(t, "/api/v1/exciter/account/token", map[string]any{"token": exciterPat(tenant, "alice")}), http.StatusOK, "alice connects")
	h.settle(t)

	read := func(key string) map[string]any {
		t.Helper()
		return requirementOf(t, want(t, owner.get(t, "/api/v1/exciter/requirements/"+key), http.StatusOK, "read "+key))
	}
	revise := func(key, title string) map[string]any {
		t.Helper()
		return exciterStub(t, http.MethodPut, tenant+"/requirements/"+key, map[string]any{"title": title, "revision": 5})["requirement"].(map[string]any)
	}
	ref := func(r map[string]any) map[string]any {
		return map[string]any{"requirement": map[string]any{"id": r["id"], "key": r["key"]}}
	}

	if read("OLD-R7")["key"] != "CHK-R4" || read("CHK-R1")["revision"] != float64(3) {
		t.Fatal("the stub's requirements are not as seeded")
	}
	moved := revise("CHK-R4", "An order is confirmed by mail and text")
	revised := revise("CHK-R1", "The basket keeps its items for 90 days")
	if read("CHK-R1")["revision"] != float64(3) || read("OLD-R7")["revision"] != float64(2) {
		t.Fatal("the cache did not keep the answers it was given")
	}

	t.Run("a refused delivery clears nothing", func(t *testing.T) {
		now := time.Now()
		for _, tc := range []struct {
			name, secret, version string
			sent                  time.Time
			status                int
		}{
			{"another secret", exciterHookSecret + "x", "1", now, http.StatusUnauthorized},
			{"no signed time", exciterHookSecret, "1", time.Time{}, http.StatusUnauthorized},
			{"a signed time long ago", exciterHookSecret, "1", now.Add(-time.Hour), http.StatusUnauthorized},
			{"envelope version 2", exciterHookSecret, "2", now, http.StatusUnprocessableEntity},
		} {
			body := exciterEnvelope(t, uuid.New(), exciter.WebhookSource, "requirement.revised", ref(revised))
			if r := exciterDelivery(t, api, body, tc.secret, tc.sent, tc.version); r.Status != tc.status {
				t.Errorf("%s: answered %d %s, want %d", tc.name, r.Status, r.Raw, tc.status)
			}
		}
		// Signed right, but from another source: not Exciter's, so ignored.
		other := exciterEnvelope(t, uuid.New(), "armature", "requirement.revised", ref(revised))
		if r := exciterDelivery(t, api, other, exciterHookSecret, now, "1"); r.Status != http.StatusNoContent {
			t.Errorf("another source is answered %d", r.Status)
		}
		if read("CHK-R1")["revision"] != float64(3) {
			t.Error("a refused or foreign delivery cleared the cache")
		}
	})

	t.Run("a signed delivery clears the requirement by key, and by id the keys it was read by", func(t *testing.T) {
		d := exciterEnvelope(t, uuid.New(), exciter.WebhookSource, "requirement.revised", ref(revised))
		if r := exciterDelivery(t, api, d, exciterHookSecret, time.Now(), "1"); r.Status != http.StatusNoContent {
			t.Fatalf("a signed delivery is answered %d %s", r.Status, r.Raw)
		}
		if got := read("CHK-R1"); got["revision"] != float64(5) {
			t.Errorf("after the webhook CHK-R1 reads %v", got)
		}
		// The page named CHK-R4 by its former key; the delivery names its current one and its id.
		hook := exciterEnvelope(t, uuid.New(), exciter.WebhookSource, "requirement.changed", ref(moved))
		if r := exciterDelivery(t, api, hook, exciterHookSecret, time.Now(), "1"); r.Status != http.StatusNoContent {
			t.Fatalf("a signed delivery is answered %d %s", r.Status, r.Raw)
		}
		if got := read("OLD-R7"); got["title"] != "An order is confirmed by mail and text" {
			t.Errorf("a requirement read by its former key still reads %v", got)
		}
		// A replay of an event already acted on changes nothing.
		revise("CHK-R1", "The basket keeps its items for a year")
		if r := exciterDelivery(t, api, d, exciterHookSecret, time.Now(), "1"); r.Status != http.StatusNoContent {
			t.Errorf("a replay is answered %d", r.Status)
		}
		if got := read("CHK-R1"); got["title"] != "The basket keeps its items for 90 days" {
			t.Errorf("a replay cleared the cache: %v", got)
		}
		last := obj(t, want(t, owner.get(t, "/api/v1/exciter/connection"), http.StatusOK, "read the connection"), "connection")["lastDelivery"]
		if taken, _ := last.(map[string]any); taken["topic"] == nil {
			t.Errorf("the settings do not show the last delivery: %v", last)
		}
	})

}

func TestExciterRowsAreWalledByTheDatabase(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "exc-wall")
	other := h.makeOutsider(t, "exc-wall-other")
	owner := api.as(t, home.user)
	bobID := h.addPerson(t, "member")
	bob := api.as(t, bobID)
	tenant := exciterTenant(t)
	want(t, owner.put(t, "/api/v1/exciter/connection", map[string]any{"baseUrl": exciterURL(t), "webhookSecret": exciterHookSecret}), http.StatusOK, "connect Exciter")
	t.Cleanup(func() { owner.delete(t, "/api/v1/exciter/connection") })
	want(t, owner.put(t, "/api/v1/exciter/account/token", map[string]any{"token": exciterPat(tenant, "alice")}), http.StatusOK, "alice connects")
	want(t, bob.put(t, "/api/v1/exciter/account/token", map[string]any{"token": exciterPat(tenant, "bob")}), http.StatusOK, "bob connects")
	h.settle(t)

	conn := appConn(t)
	ctx := context.Background()
	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}

	t.Run("a member reads their own token and where Exciter is, nothing more", func(t *testing.T) {
		actAs(t, conn, bobID)
		if n := count(`SELECT count(*) FROM exciter_token`); n != 1 {
			t.Errorf("bob reads %d tokens, want his own", n)
		}
		if n := count(`SELECT count(*) FROM exciter_connection`); n != 0 {
			t.Error("bob reads the connection with its sealed secret")
		}
		if n := count(`SELECT count(*) FROM exciter_endpoint()`); n != 1 {
			t.Error("bob cannot learn where Exciter is")
		}
		var connected *int
		_ = conn.QueryRow(ctx, `SELECT exciter_connected_count()`).Scan(&connected)
		if connected != nil {
			t.Errorf("bob counts the connected members: %d", *connected)
		}
		untouched(t, conn, "pointing the connection elsewhere", `UPDATE exciter_connection SET base_url = 'https://evil.example'`)
		untouched(t, conn, "removing the connection", `DELETE FROM exciter_connection`)
		untouched(t, conn, "marking alice's token", `UPDATE exciter_token SET status = 'rejected' WHERE user_id = $1`, home.user)
		untouched(t, conn, "removing alice's token", `DELETE FROM exciter_token WHERE user_id = $1`, home.user)
		denied(t, conn, "storing a token for alice", `INSERT INTO exciter_token (user_id, token, status) VALUES ($1, '\x00', 'ok')`, home.user)
		denied(t, conn, "rewriting his token in place", `UPDATE exciter_token SET token = '\x00' WHERE user_id = $1`, bobID)
		denied(t, conn, "choosing his row id", `INSERT INTO exciter_token (user_id, id, token, status) VALUES ($1, $2, '\x00', 'ok')`, bobID, uuid.New())
	})

	t.Run("an administrator keeps the connection, and moving it forgets every token", func(t *testing.T) {
		actAs(t, conn, home.user)
		if n := count(`SELECT count(*) FROM exciter_token`); n != 1 {
			t.Errorf("the owner reads %d tokens, want their own", n)
		}
		if n := count(`SELECT exciter_connected_count()`); n != 2 {
			t.Errorf("the owner counts %d connected members, want 2", n)
		}
		denied(t, conn, "a second connection", `INSERT INTO exciter_connection (singleton, base_url) VALUES (false, 'https://evil.example')`)
		denied(t, conn, "an address with a path", `UPDATE exciter_connection SET base_url = 'https://evil.example/x'`)
		if _, err := conn.Exec(ctx, `UPDATE exciter_connection SET base_url = 'https://elsewhere.example'`); err != nil {
			t.Fatalf("the owner moves the connection: %v", err)
		}
		if n := h.countRows(t, `SELECT count(*) FROM exciter_token`); n != 0 {
			t.Errorf("raw SQL moved the connection and kept %d tokens", n)
		}
	})

	t.Run("a member without the right reaches none of the connection", func(t *testing.T) {
		actAs(t, conn, other.user)
		if n := count(`SELECT count(*) FROM exciter_connection`); n != 0 {
			t.Error("a member reads the connection")
		}
		untouched(t, conn, "removing the connection", `DELETE FROM exciter_connection`)
		denied(t, conn, "storing a token for somebody else", `INSERT INTO exciter_token (user_id, token, status) VALUES ($1, '\x00', 'ok')`, bobID)
	})
}
