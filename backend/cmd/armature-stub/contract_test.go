package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/armature"
	"github.com/praetorianer777/stator/backend/internal/openapi"
)

// The contract, as docs/api-contract-m3.md sets it: the stub serves nothing
// Armature does not, and answers in Armature's shapes. Armature's document is
// vendored under api/armature; updating it is how a change there reaches here.

const armatureDocument = "../../../api/armature/openapi.json"

func loadArmature(t *testing.T) (*openapi.Document, []byte) {
	t.Helper()
	raw, err := os.ReadFile(armatureDocument)
	if err != nil {
		t.Fatalf("read Armature's document; vendor it with make armature-openapi: %v", err)
	}
	var doc openapi.Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Armature's document does not read as OpenAPI: %v", err)
	}
	return &doc, raw
}

func TestEveryRouteIsAnArmatureOperation(t *testing.T) {
	doc, _ := loadArmature(t)
	for _, rt := range routes {
		item, ok := doc.Paths[rt.Path]
		if !ok || item[strings.ToLower(rt.Method)] == nil {
			t.Errorf("the stub serves %s %s, which Armature's document does not have", rt.Method, rt.Path)
			continue
		}
	}
}

// contract drives the stub and checks every request and answer against
// Armature's document, counting which routes answered with a success.
type contract struct {
	t      *testing.T
	doc    *openapi.Document
	base   string
	served map[string]bool
}

// route finds the stub's route for a concrete path; a {name} segment of the
// template matches any one segment. Literal templates win over templated ones.
func (c *contract) route(method, path string) *route {
	var found *route
	for i, rt := range routes {
		want, got := strings.Split(rt.Path, "/"), strings.Split(path, "/")
		if rt.Method != method || len(want) != len(got) {
			continue
		}
		matches := true
		for j := range want {
			if !strings.HasPrefix(want[j], "{") && !strings.EqualFold(want[j], got[j]) {
				matches = false
			}
		}
		if matches && (found == nil || strings.Count(rt.Path, "{") < strings.Count(found.Path, "{")) {
			found = &routes[i]
		}
	}
	return found
}

// call sends a request as token (empty for none) and returns the status and
// the body; for an Armature route both are checked against the document.
func (c *contract) call(method, path, token string, body any) (int, []byte) {
	c.t.Helper()
	var reader io.Reader
	var sent []byte
	if body != nil {
		sent, _ = json.Marshal(body)
		reader = bytes.NewReader(sent)
	}
	full := path
	if !strings.HasPrefix(path, stubPrefix) {
		full = apiPrefix + path
	}
	req, _ := http.NewRequest(method, c.base+full, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if strings.HasPrefix(path, stubPrefix) {
		return resp.StatusCode, raw
	}

	bare, _, _ := strings.Cut(path, "?")
	rt := c.route(method, bare)
	if rt == nil {
		c.t.Fatalf("%s %s is no route of the stub", method, bare)
	}
	op := c.doc.Paths[rt.Path][strings.ToLower(method)]
	if sent != nil {
		if op.RequestBody == nil {
			c.t.Errorf("%s %s takes no body in Armature, and was sent one", method, rt.Path)
		} else if err := c.doc.ValidateJSON(op.RequestBody.Content["application/json"].Schema, sent); err != nil {
			c.t.Errorf("%s %s: Armature would refuse the body %s: %v", method, rt.Path, sent, err)
		}
	}
	answer, ok := op.Responses[strconv.Itoa(resp.StatusCode)]
	if !ok {
		if resp.StatusCode < http.StatusBadRequest {
			c.t.Errorf("%s %s answered %d, which Armature never does", method, rt.Path, resp.StatusCode)
			return resp.StatusCode, raw
		}
		answer = op.Responses["default"]
	}
	if media, isJSON := answer.Content["application/json"]; isJSON {
		if err := c.doc.ValidateJSON(media.Schema, raw); err != nil {
			c.t.Errorf("%s %s answered %d in a shape Armature does not: %v\n%s", method, rt.Path, resp.StatusCode, err, raw)
		}
	} else if len(answer.Content) == 0 && len(raw) > 0 {
		c.t.Errorf("%s %s answered %d with a body, where Armature has none", method, rt.Path, resp.StatusCode)
	}
	if resp.StatusCode < http.StatusBadRequest {
		c.served[rt.Method+" "+rt.Path] = true
	}
	return resp.StatusCode, raw
}

func (c *contract) expect(method, path, token string, body any, want int) map[string]any {
	c.t.Helper()
	status, raw := c.call(method, path, token, body)
	if status != want {
		c.t.Fatalf("%s %s = %d, want %d: %s", method, path, status, want, raw)
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func patFor(tenant, person string) string { return tokenPrefix + tenant + "_" + person }

func keysOf(t *testing.T, answer map[string]any) []string {
	t.Helper()
	var out []string
	for _, is := range answer["issues"].([]any) {
		out = append(out, is.(map[string]any)["key"].(string))
	}
	return out
}

func TestEveryAnswerFitsArmaturesDocument(t *testing.T) {
	doc, raw := loadArmature(t)
	server := httptest.NewServer(newStub(raw).handler())
	defer server.Close()
	c := &contract{t: t, doc: doc, base: server.URL, served: map[string]bool{}}
	alice, admin, reader := patFor("acme", "alice"), patFor("acme", adminName), patFor("acme", readerName)

	if got := c.expect("GET", "/openapi.json", "", nil, 200); got["info"].(map[string]any)["title"] != "Armature" {
		t.Errorf("the document served is not Armature's: %v", got["info"])
	}

	me := c.expect("GET", "/auth/me", alice, nil, 200)["principal"].(map[string]any)
	if me["org"].(map[string]any)["slug"] != "acme" || me["user"].(map[string]any)["name"] != "Alice" {
		t.Errorf("a token names its tenant and person: %v", me)
	}
	c.expect("GET", "/auth/me", revokedToken, nil, 401)
	c.expect("GET", "/auth/me", "", nil, 401)
	c.expect("GET", "/auth/me", "stator_pat_nothing", nil, 401)

	access := c.expect("GET", "/access/me", alice, nil, 200)
	if fmt.Sprint(access["projects"]) != "[CP]" {
		t.Errorf("alice sees %v, want CP alone", access["projects"])
	}
	if fmt.Sprint(c.expect("GET", "/access/me", admin, nil, 200)["projects"]) != "[CP SEC]" {
		t.Error("the admin does not see SEC")
	}
	if n := len(c.expect("GET", "/projects", alice, nil, 200)["projects"].([]any)); n != 1 {
		t.Errorf("alice lists %d projects, want 1", n)
	}
	if n := len(c.expect("GET", "/issue-types", alice, nil, 200)["issueTypes"].([]any)); n != 5 {
		t.Errorf("%d issue types, want 5", n)
	}

	for _, tc := range []struct {
		q, as string
		want  string
	}{
		{"key in (CP-1, cp-2, SEC-1)", alice, "[CP-1 CP-2]"},
		{"key in (CP-1, SEC-1)", admin, "[CP-1 SEC-1]"},
		{"project = CP AND statusCategory != done ORDER BY priority DESC", alice, "[CP-4 CP-2 CP-5 CP-3]"},
		{"assignee = currentUser() ORDER BY key", alice, "[CP-1 CP-4]"},
		{"project in (CP) AND statusCategory = 'in_progress'", alice, "[CP-2]"},
		{"key = CP-3", alice, "[CP-3]"},
		{"", alice, "[CP-1 CP-2 CP-3 CP-4 CP-5]"},
	} {
		got := c.expect("GET", "/issues?q="+strings.ReplaceAll(tc.q, " ", "%20")+"&limit=100", tc.as, nil, 200)
		if keys := fmt.Sprint(keysOf(t, got)); keys != tc.want {
			t.Errorf("%q found %s, want %s", tc.q, keys, tc.want)
		}
	}
	paged := c.expect("GET", "/issues?q=project%20%3D%20CP&limit=2&offset=1", alice, nil, 200)
	if fmt.Sprint(keysOf(t, paged)) != "[CP-2 CP-3]" || paged["total"].(float64) != 5 {
		t.Errorf("a page is %v of %v", keysOf(t, paged), paged["total"])
	}
	bad := c.expect("GET", "/issues?q=summary%20%3D%20x", alice, nil, 400)["error"].(map[string]any)
	if bad["code"] != "bad_query" || bad["position"].(float64) != 1 {
		t.Errorf("a query the stub cannot read is %v", bad)
	}
	c.expect("GET", "/issues?q=key%20in%20(CP-1&limit=5", alice, nil, 400)
	c.expect("GET", "/issues?limit=0", alice, nil, 422)

	c.expect("GET", "/issues/cp-1", alice, nil, 200)
	c.expect("GET", "/issues/SEC-1", alice, nil, 404)
	c.expect("GET", "/issues/SEC-1", admin, nil, 200)
	if moved := c.expect("GET", "/issues/SEC-2", alice, nil, 200)["issue"].(map[string]any); moved["key"] != "CP-5" {
		t.Errorf("an old key finds %v, want CP-5", moved["key"])
	}

	made := c.expect("POST", "/issues", alice, map[string]any{"projectKey": "CP", "summary": "Filed from a page",
		"description": map[string]any{"type": "doc", "content": []any{}}}, 201)["issue"].(map[string]any)
	if made["key"] != "CP-6" {
		t.Errorf("a new issue is %v, want CP-6", made["key"])
	}
	c.expect("POST", "/issues", reader, map[string]any{"projectKey": "CP", "summary": "Not allowed"}, 403)
	c.expect("POST", "/issues", alice, map[string]any{"projectKey": "SEC", "summary": "Not visible"}, 404)
	c.expect("POST", "/issues", alice, map[string]any{"projectKey": "CP", "summary": " "}, 422)

	// The body Stator files an issue from a page with, as #31 sends it.
	page := armature.CreateRequest{ProjectKey: "CP", Summary: "Renew the TLS certificate",
		Description: armature.Description("Review notes", armature.PageURL("https://stator.example", "ENG", uuid.New()))}
	filed := c.expect("POST", "/issues", alice, page, 201)["issue"].(map[string]any)
	kept := c.expect("GET", stubPrefix+"/acme/issues/"+filed["key"].(string), "", nil, 200)["issue"].(map[string]any)
	if desc, _ := json.Marshal(kept["description"]); !bytes.Contains(desc, []byte(`"href":"https://stator.example/s/ENG/p/`)) {
		t.Errorf("the stub keeps the description %s", desc)
	}
	c.expect("PUT", stubPrefix+"/acme/people/alice/read-only-projects", "", map[string]any{"projects": []string{"CP"}}, 204)
	c.expect("POST", "/issues", alice, page, 403)
	if perms := c.expect("GET", "/access/me", alice, nil, 200)["permissions"].(map[string]any)["projects"].(map[string]any)["CP"]; len(perms.([]any)) != 1 {
		t.Errorf("alice may still write CP: %v", perms)
	}
	c.expect("PUT", stubPrefix+"/acme/people/alice/read-only-projects", "", map[string]any{"projects": []string{}}, 204)
	c.expect("PUT", stubPrefix+"/acme/refused-summary", "", map[string]any{"summary": page.Summary}, 204)
	c.expect("POST", "/issues", alice, page, 422)
	c.expect("PUT", stubPrefix+"/acme/refused-summary", "", map[string]any{"summary": ""}, 204)

	link := map[string]any{"url": "https://stator.example/s/ENG/p/1", "title": "Runbook", "source": "Stator"}
	put := c.expect("POST", "/issues/CP-1/remote-links", alice, link, 201)["remoteLink"].(map[string]any)
	link["title"] = "Runbook, renamed"
	if again := c.expect("POST", "/issues/CP-1/remote-links", alice, link, 200)["remoteLink"].(map[string]any); again["id"] != put["id"] {
		t.Error("the same url made a second link")
	}
	c.expect("POST", "/issues/CP-1/remote-links", reader, link, 403)
	c.expect("POST", "/issues/SEC-1/remote-links", alice, link, 404)
	if n := len(c.expect("GET", "/issues/CP-1/remote-links", alice, nil, 200)["remoteLinks"].([]any)); n != 1 {
		t.Errorf("CP-1 has %d links, want 1", n)
	}
	held := c.expect("GET", stubPrefix+"/acme/remote-links", "", nil, 200)["remoteLinks"].([]any)
	if len(held) != 1 || held[0].(map[string]any)["issueKey"] != "CP-1" {
		t.Errorf("the stub holds %v", held)
	}
	c.expect("DELETE", "/issues/CP-1/remote-links/"+put["id"].(string), alice, nil, 204)
	c.expect("DELETE", "/issues/CP-1/remote-links/"+put["id"].(string), alice, nil, 404)

	// A link as link sync sends it (#32), refused where the person may only
	// read, and an Armature that does not answer for a while.
	sent := armature.RemoteLinkRequest{URL: armature.PageURL("https://stator.example", "ENG", uuid.New()), Title: armature.RestrictedLinkTitle, Source: armature.LinkSource}
	linked := c.expect("POST", "/issues/CP-2/remote-links", alice, sent, 201)["remoteLink"].(map[string]any)
	c.expect("PUT", stubPrefix+"/acme/people/alice/read-only-projects", "", map[string]any{"projects": []string{"CP"}}, 204)
	c.expect("POST", "/issues/CP-2/remote-links", alice, sent, 403)
	c.expect("DELETE", "/issues/CP-2/remote-links/"+linked["id"].(string), alice, nil, 403)
	c.expect("PUT", stubPrefix+"/acme/people/alice/read-only-projects", "", map[string]any{"projects": []string{}}, 204)
	c.expect("PUT", stubPrefix+"/acme/remote-links/outage", "", map[string]any{"status": 503, "count": 1}, 204)
	c.expect("DELETE", "/issues/CP-2/remote-links/"+linked["id"].(string), alice, nil, 503)
	c.expect("DELETE", "/issues/CP-2/remote-links/"+linked["id"].(string), alice, nil, 204)
	c.expect("PUT", stubPrefix+"/acme/remote-links/outage", "", map[string]any{"status": 404, "count": 1}, 422)

	if got := c.expect("GET", "/themes/active", alice, nil, 200); got["theme"] != nil {
		t.Errorf("nobody follows a theme before the stub is told: %v", got)
	}
	c.expect("PUT", stubPrefix+"/acme/people/alice/theme", "", map[string]any{"theme": "deep-tech"}, 204)
	active := c.expect("GET", "/themes/active", alice, nil, 200)["theme"].(map[string]any)
	status, pkg := c.call("GET", "/themes/"+active["id"].(string)+"/export", alice, nil)
	if status != 200 || !bytes.Contains(pkg, []byte(`"format":"armature-theme/1"`)) {
		t.Errorf("the export is %d %s", status, pkg)
	}
	c.expect("GET", "/themes/"+active["id"].(string)+"/export", patFor("acme", "bob"), nil, 404)

	changed := c.expect("PATCH", stubPrefix+"/acme/issues/CP-3", "", map[string]any{"summary": "Renamed", "statusCategory": "done", "assignee": "bob"}, 200)
	if changed["issue"].(map[string]any)["summary"] != "Renamed" {
		t.Errorf("a change did not hold: %v", changed)
	}
	if changed["issue"].(map[string]any)["resolvedAt"] == nil {
		t.Errorf("an issue made done has no resolvedAt: %v", changed)
	}

	// The reports a chart block draws (#51), counted over what the person
	// may see and the query matches.
	pie := c.expect("GET", "/projects/CP/reports/chart?groupBy=statusCategory&measure=count&shape=donut&q=project+%3D+CP", alice, nil, 200)
	sum := 0.0
	for _, g := range pie["groups"].([]any) {
		sum += g.(map[string]any)["value"].(float64)
	}
	if len(pie["groups"].([]any)) != 3 || sum != pie["total"] {
		t.Errorf("the chart by status category is %v", pie)
	}
	flow := c.expect("GET", "/projects/CP-1/reports/created_vs_resolved?days=7&q=project+%3D+CP", alice, nil, 200)
	if days := flow["days"].([]any); len(days) != 7 || days[6].(map[string]any)["resolved"] != float64(1) {
		t.Errorf("created against resolved is %v", flow)
	}
	c.expect("GET", "/projects/SEC/reports/chart?groupBy=type&q=project+%3D+SEC", alice, nil, 404)
	c.expect("GET", "/projects/CP/reports/chart?groupBy=type&q=project+%3D", alice, nil, 400)
	// The plan a roadmap block draws (#52): an epic's children give it the
	// span it has no days of its own for.
	epic := c.expect("POST", stubPrefix+"/acme/projects/CP/issues", "", map[string]any{"summary": "Launch", "type": "Epic"}, 201)["issue"].(map[string]any)["key"].(string)
	c.expect("PATCH", stubPrefix+"/acme/issues/CP-1", "", map[string]any{"parent": epic, "startDate": "2026-10-01", "dueDate": "2026-10-09", "team": "Platform"}, 200)
	c.expect("PATCH", stubPrefix+"/acme/issues/CP-4", "", map[string]any{"parent": epic, "startDate": "2026-10-12", "team": "Platform"}, 200)
	c.expect("PATCH", stubPrefix+"/acme/issues/CP-4", "", map[string]any{"startDate": "someday"}, 422)
	plan := c.expect("GET", "/projects/CP/plan?q=key+in+%28CP-1%2C+CP-4%29", alice, nil, 200)
	var launch map[string]any
	for _, it := range plan["items"].([]any) {
		if it.(map[string]any)["issue"].(map[string]any)["key"] == epic {
			launch = it.(map[string]any)
		}
	}
	if launch == nil || launch["derived"] != true || !strings.HasPrefix(fmt.Sprint(launch["start"]), "2026-10-01") ||
		!strings.HasPrefix(fmt.Sprint(launch["due"]), "2026-10-15") || len(launch["children"].([]any)) != 2 {
		t.Errorf("the epic in the plan is %v", launch)
	}
	if fmt.Sprint(plan["matched"]) != "[CP-1 CP-4]" {
		t.Errorf("a plan for two keys matches %v", plan["matched"])
	}
	c.expect("GET", "/projects/SEC/plan", alice, nil, 404)
	c.expect("POST", stubPrefix+"/acme/issues/CP-3/move", "", map[string]any{"projectKey": "SEC"}, 200)
	if got := c.expect("GET", "/issues/CP-3", admin, nil, 200)["issue"].(map[string]any); got["key"] != "SEC-3" {
		t.Errorf("a moved issue answers to %v, want SEC-3", got["key"])
	}

	if other := c.expect("GET", "/issues/CP-6", patFor("globex", "alice"), nil, 404); other["error"] == nil {
		t.Error("an issue made in one tenant is seen in another")
	}

	for _, rt := range routes {
		if !c.served[rt.Method+" "+rt.Path] {
			t.Errorf("the scenario never had %s %s answer with a success", rt.Method, rt.Path)
		}
	}
}

// A webhook from the stub is one Armature's receiver code would accept: the
// envelope, the headers and the signature over the raw body.
func TestAWebhookIsSignedAsArmatureSignsIt(t *testing.T) {
	_, raw := loadArmature(t)
	const secret = "armature_whs_0123456789012345678901234567890123456789abc"
	got := make(chan *http.Request, 1)
	bodies := make(chan []byte, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- r
		bodies <- body
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	server := httptest.NewServer(newStub(raw).handler())
	defer server.Close()

	req, _ := json.Marshal(map[string]any{"url": receiver.URL, "secret": secret, "topic": "issue.updated", "payload": map[string]any{"key": "CP-1"}})
	resp, err := http.Post(server.URL+stubPrefix+"/acme/webhooks", "application/json", bytes.NewReader(req))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var answer map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&answer)
	if resp.StatusCode != 200 || answer["status"].(float64) != 204 {
		t.Fatalf("the delivery answered %d %v", resp.StatusCode, answer)
	}
	r, body := <-got, <-bodies
	if r.Header.Get("X-Armature-Signature-256") != sign(body, secret) || r.Header.Get("X-Armature-Event") != "issue.updated" {
		t.Errorf("headers %v do not sign the body", r.Header)
	}
	var envelope map[string]any
	_ = json.Unmarshal(body, &envelope)
	for _, key := range []string{"id", "topic", "orgId", "occurredAt", "payload"} {
		if _, ok := envelope[key]; !ok {
			t.Errorf("the envelope lacks %s: %s", key, body)
		}
	}
	if envelope["orgId"] != idOf("acme", "org").String() {
		t.Errorf("the envelope names org %v, not the tenant's", envelope["orgId"])
	}
}
