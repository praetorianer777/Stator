package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/praetorianer777/stator/backend/internal/openapi"
)

// The contract: the stub serves nothing Exciter does not, and answers and
// posts in Exciter's shapes. Exciter's document is vendored under
// api/exciter by make exciter-openapi; updating it is how a change there
// reaches here.

const exciterDocument = "../../../api/exciter/openapi.json"

func loadExciter(t *testing.T) (*openapi.Document, []byte) {
	t.Helper()
	raw, err := os.ReadFile(exciterDocument)
	if err != nil {
		t.Fatalf("read Exciter's document; vendor it with make exciter-openapi: %v", err)
	}
	var doc openapi.Document
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("Exciter's document does not read as OpenAPI: %v", err)
	}
	return &doc, raw
}

func TestEveryRouteIsAnExciterOperation(t *testing.T) {
	doc, _ := loadExciter(t)
	for _, rt := range routes {
		item, ok := doc.Paths[rt.Path]
		if !ok || item[strings.ToLower(rt.Method)] == nil {
			t.Errorf("the stub serves %s %s, which Exciter's document does not have", rt.Method, rt.Path)
		}
	}
}

func patFor(tenant, person string) string { return tokenPrefix + tenant + "_" + person }

// get calls an Exciter route as token and checks the answer against the
// document, noting which routes answered with a success.
func get(t *testing.T, doc *openapi.Document, base, path, template, token string, served map[string]bool) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+apiPrefix+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	op := doc.Paths[template]["get"]
	answer, ok := op.Responses[strconv.Itoa(resp.StatusCode)]
	if !ok {
		if resp.StatusCode < http.StatusBadRequest {
			t.Errorf("GET %s answered %d, which Exciter never does", template, resp.StatusCode)
		}
		answer = op.Responses["default"]
	}
	if media, isJSON := answer.Content["application/json"]; isJSON {
		if err := doc.ValidateJSON(media.Schema, raw); err != nil {
			t.Errorf("GET %s answered %d in a shape Exciter does not: %v\n%s", template, resp.StatusCode, err, raw)
		}
	}
	if resp.StatusCode < http.StatusBadRequest {
		served["GET "+template] = true
	}
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	return resp.StatusCode, body
}

func TestEveryAnswerFitsExcitersDocument(t *testing.T) {
	doc, raw := loadExciter(t)
	server := httptest.NewServer(newStub(raw).handler())
	defer server.Close()
	served := map[string]bool{}
	alice, bob := patFor("acme", "alice"), patFor("acme", "bob")

	if status, _ := get(t, doc, server.URL, "/openapi.json", "/openapi.json", "", served); status != http.StatusOK {
		t.Errorf("the document answered %d", status)
	}
	if status, body := get(t, doc, server.URL, "/meta", "/meta", "", served); status != http.StatusOK || body["product"] != "exciter" {
		t.Errorf("/meta answered %d %v", status, body)
	}
	if status, body := get(t, doc, server.URL, "/auth/me", "/auth/me", alice, served); status != http.StatusOK || body["user"].(map[string]any)["name"] != "Alice" {
		t.Errorf("/auth/me answered %d %v", status, body)
	}
	for _, token := range []string{"", revokedToken, "armature_pat_acme_alice"} {
		if status, _ := get(t, doc, server.URL, "/auth/me", "/auth/me", token, served); status != http.StatusUnauthorized {
			t.Errorf("/auth/me with %q answered %d", token, status)
		}
	}

	const one = "/requirements/{reqKey}"
	for _, tc := range []struct {
		name, key, token string
		status           int
		current          string
	}{
		{"a requirement", "CHK-R1", bob, http.StatusOK, "CHK-R1"},
		{"a former key", "OLD-R7", bob, http.StatusOK, "CHK-R4"},
		{"one hidden from the reader", "CHK-R2", bob, http.StatusNotFound, ""},
		{"one the reader sees", "CHK-R2", alice, http.StatusOK, "CHK-R2"},
		{"one in the trash, to an administrator", "CHK-R3", alice, http.StatusOK, "CHK-R3"},
		{"one in the trash, to anybody else", "CHK-R3", bob, http.StatusNotFound, ""},
		{"no such key", "CHK-R99", alice, http.StatusNotFound, ""},
	} {
		status, body := get(t, doc, server.URL, "/requirements/"+tc.key, one, tc.token, served)
		if status != tc.status {
			t.Errorf("%s: %d, want %d", tc.name, status, tc.status)
			continue
		}
		if tc.current != "" {
			if got := body["requirement"].(map[string]any)["key"]; got != tc.current {
				t.Errorf("%s: key %v, want %s", tc.name, got, tc.current)
			}
		}
	}

	keysOf := func(body map[string]any, list string, nested bool) []string {
		var out []string
		for _, each := range body[list].([]any) {
			item := each.(map[string]any)
			if nested {
				item = item["requirement"].(map[string]any)
			}
			out = append(out, item["key"].(string))
		}
		return out
	}
	if _, body := get(t, doc, server.URL, "/search/quick?q=basket", "/search/quick", alice, served); !slices.Equal(keysOf(body, "requirements", true), []string{"CHK-R1"}) {
		t.Errorf("a quick search by a word of the title finds %v", body)
	}
	if _, body := get(t, doc, server.URL, "/search/quick?q=sec-r", "/search/quick", bob, served); len(keysOf(body, "requirements", true)) != 0 {
		t.Errorf("a quick search finds what the reader may not see: %v", body)
	}
	if _, body := get(t, doc, server.URL, "/projects", "/projects", alice, served); !slices.Equal(keysOf(body, "projects", false), []string{"CHK", "SEC"}) {
		t.Errorf("alice's projects are %v", body)
	}
	if _, body := get(t, doc, server.URL, "/projects", "/projects", bob, served); !slices.Equal(keysOf(body, "projects", false), []string{"CHK"}) {
		t.Errorf("bob's projects are %v", body)
	}

	const grid = "/projects/{projectKey}/requirements"
	mine := "/projects/CHK/requirements?query=" + url.QueryEscape("owner = currentUser() ORDER BY key DESC") + "&limit=5"
	if status, body := get(t, doc, server.URL, mine, grid, alice, served); status != http.StatusOK || !slices.Equal(keysOf(body, "rows", false), []string{"CHK-R2", "CHK-R1"}) || body["total"] != 2.0 {
		t.Errorf("currentUser() for alice: %d %v", status, body)
	}
	if _, body := get(t, doc, server.URL, mine, grid, bob, served); !slices.Equal(keysOf(body, "rows", false), []string{"CHK-R4"}) {
		t.Errorf("currentUser() for bob: %v", body)
	}
	if _, body := get(t, doc, server.URL, "/projects/CHK/requirements?limit=1&offset=1", grid, alice, served); !slices.Equal(keysOf(body, "rows", false), []string{"CHK-R2"}) || body["total"] != 4.0 {
		t.Errorf("a window of the project: %v", body)
	}
	status, body := get(t, doc, server.URL, "/projects/CHK/requirements?query="+url.QueryEscape("status = Approved AND colour = red"), grid, alice, served)
	if failure, _ := body["error"].(map[string]any); status != http.StatusBadRequest || failure["code"] != "bad_query" || failure["position"] != 23.0 {
		t.Errorf("a query naming no field answers %d %v", status, body)
	}
	if status, _ := get(t, doc, server.URL, "/projects/SEC/requirements", grid, bob, served); status != http.StatusNotFound {
		t.Errorf("a project the reader may not view answers %d", status)
	}

	const suggest = "/projects/{projectKey}/requirements/suggest"
	for _, tc := range []struct {
		q, at, slot string
		want        []string
	}{
		{"sta", "3", "field", []string{"status"}},
		{"status ", "", "operator", []string{"=", "!=", "~"}},
		{"status = A", "", "value", []string{"Approved"}},
		{"key = CHK-R1 ", "", "keyword", []string{"AND", "ORDER BY"}},
	} {
		path := "/projects/CHK/requirements/suggest?q=" + url.QueryEscape(tc.q)
		if tc.at != "" {
			path += "&at=" + tc.at
		}
		_, body := get(t, doc, server.URL, path, suggest, alice, served)
		completion, _ := body["completion"].(map[string]any)
		var words []string
		for _, w := range body["words"].([]any) {
			words = append(words, w.(map[string]any)["text"].(string))
		}
		if completion["slot"] != tc.slot || !slices.Equal(words, tc.want) {
			t.Errorf("completing %q: %v", tc.q, body)
		}
	}
	if status, _ := get(t, doc, server.URL, "/projects/CHK/requirements/suggest?q=key&at=9", suggest, alice, served); status != http.StatusUnprocessableEntity {
		t.Errorf("a caret past the end answers %d", status)
	}

	for _, rt := range routes {
		if !served[rt.Method+" "+rt.Path] {
			t.Errorf("the scenario never had %s %s answer with a success", rt.Method, rt.Path)
		}
	}
}

// A webhook from the stub is one Exciter's receivers would take: the
// envelope fits Exciter's own schema for it, and the headers sign the body
// and the time.
func TestAWebhookIsSignedAsExciterSignsIt(t *testing.T) {
	doc, raw := loadExciter(t)
	const secret = "exciter_whs_0123456789012345678901234567890123456789abc"
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

	req, _ := json.Marshal(map[string]any{"url": receiver.URL, "secret": secret, "topic": "requirement.revised", "key": "CHK-R1"})
	resp, err := http.Post(server.URL+stubPrefix+"/acme/webhooks", "application/json", bytes.NewReader(req))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var answer map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&answer)
	if resp.StatusCode != http.StatusOK || answer["status"].(float64) != http.StatusNoContent {
		t.Fatalf("the delivery answered %d %v", resp.StatusCode, answer)
	}
	r, body := <-got, <-bodies
	if r.Header.Get("X-Exciter-Signature-256") != sign(body, secret) || r.Header.Get("X-Exciter-Event") != "requirement.revised" {
		t.Errorf("headers %v do not sign the body", r.Header)
	}
	sent := r.Header.Get("X-Exciter-Timestamp")
	if r.Header.Get("X-Exciter-Signature-Timestamped-256") != sign([]byte(sent+"."+string(body)), secret) || r.Header.Get("X-Exciter-Webhook-Version") != "1" {
		t.Errorf("headers %v do not sign the time and name the version", r.Header)
	}
	op := doc.Webhooks["event"]["post"]
	for _, p := range op.Parameters {
		if p.In == "header" && p.Required && r.Header.Get(p.Name) == "" {
			t.Errorf("the delivery lacks the header %s Exciter always sends", p.Name)
		}
	}
	if err := doc.ValidateJSON(op.RequestBody.Content["application/json"].Schema, body); err != nil {
		t.Errorf("the envelope does not fit Exciter's: %v\n%s", err, body)
	}
	var envelope struct {
		Source  string `json:"source"`
		Payload struct {
			Requirement map[string]any `json:"requirement"`
		} `json:"payload"`
	}
	_ = json.Unmarshal(body, &envelope)
	if envelope.Source != "exciter" || envelope.Payload.Requirement["key"] != "CHK-R1" || envelope.Payload.Requirement["id"] != idOf("acme", "CHK-R1").String() {
		t.Errorf("the envelope does not name the requirement by id and key: %s", body)
	}
}
