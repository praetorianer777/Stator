package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/praetorianer777/stator/backend/internal/auth"
)

// Why an operation is not a tool. docs/decisions.md and docs/mcp.md say the
// same in prose.
const (
	whyEdge       = "a probe, the document itself (a resource instead), the endpoint itself or a sign-in step"
	whySelf       = "the caller's own session, settings, tokens, themes or Armature account, changed by a person at the keyboard"
	whyAdmin      = "administration or who may do what: the provider, members, tokens, permissions, restrictions, spaces themselves and their shortcuts, archiving, webhooks"
	whyRemoves    = "deletes or takes something away; no tool removes anything, as in Armature"
	whyBrowser    = "furniture of the browser client: typeahead, badges, pickers, visits and drafts"
	whyFiles      = "moves files rather than words; get_page_markdown and replace_page_markdown carry a page's words"
	whyArmature   = "Armature's own MCP endpoint serves its issues as the person, without Stator in between"
	whyAttention  = "reaches other people or a page's standing: shares, reactions, watches, stars, owners and verification"
	whyReorganize = "moves, copies, restores or publishes a draft; a person does that in the tree, the trash or the history"
	whyThreads    = "an inline thread needs the body with a passage marked, and threads are rewritten or resolved where they are read"
	whyReaders    = "names the people who read a page, which stays with its editors in the page; get_page_views counts them"
)

// notTools holds the decision for every operation that is not a tool, so a
// route added without one fails TestEveryOperationHasAnMCPDecision.
var notTools = map[string]string{
	"GET /healthz":                                    whyEdge,
	"GET /readyz":                                     whyEdge,
	"GET /openapi.json":                               whyEdge,
	"POST /auth/login":                                whyEdge,
	"GET /auth/oidc/{orgSlug}/start":                  whyEdge,
	"GET /auth/oidc/callback":                         whyEdge,
	"POST /armature/webhook/{orgSlug}":                whyEdge,
	"POST /mcp":                                       whyEdge,
	"POST /auth/logout":                               whySelf,
	"PATCH /auth/me":                                  whySelf,
	"POST /auth/switch-org":                           whySelf,
	"GET /tokens":                                     whySelf,
	"POST /tokens":                                    whySelf,
	"DELETE /tokens/{tokenID}":                        whySelf,
	"GET /themes":                                     whySelf,
	"POST /themes":                                    whySelf,
	"GET /themes/examples":                            whySelf,
	"GET /themes/active":                              whySelf,
	"PUT /themes/active":                              whySelf,
	"PUT /themes/default":                             whySelf,
	"POST /themes/import":                             whySelf,
	"GET /themes/{themeID}/export":                    whySelf,
	"GET /themes/{themeID}":                           whySelf,
	"PATCH /themes/{themeID}":                         whySelf,
	"DELETE /themes/{themeID}":                        whySelf,
	"POST /themes/{themeID}/assets":                   whySelf,
	"GET /themes/{themeID}/assets/{assetID}":          whySelf,
	"DELETE /themes/{themeID}/assets/{assetID}":       whySelf,
	"GET /notification-preferences":                   whySelf,
	"PUT /notification-preferences":                   whySelf,
	"GET /armature/account":                           whySelf,
	"PUT /armature/account/token":                     whySelf,
	"POST /armature/account/check":                    whySelf,
	"DELETE /armature/account/token":                  whySelf,
	"GET /armature/theme":                             whySelf,
	"PUT /armature/theme":                             whySelf,
	"DELETE /armature/theme":                          whySelf,
	"GET /oidc-provider":                              whyAdmin,
	"PUT /oidc-provider":                              whyAdmin,
	"GET /oidc-provider/group-roles":                  whyAdmin,
	"POST /oidc-provider/group-roles":                 whyAdmin,
	"DELETE /oidc-provider/group-roles/{groupRoleID}": whyAdmin,
	"GET /users":                                      whyAdmin,
	"DELETE /users/{userID}":                          whyAdmin,
	"GET /users/requests":                             whyAdmin,
	"POST /users/requests/{userID}/admit":             whyAdmin,
	"DELETE /users/requests/{userID}":                 whyAdmin,
	"GET /org/tokens":                                 whyAdmin,
	"DELETE /org/tokens/{tokenID}":                    whyAdmin,
	"GET /access/me":                                  whyAdmin,
	"GET /org/permissions":                            whyAdmin,
	"PUT /pages/{pageID}/appearance":                  whyBrowser,
	"GET /pages/{pageID}/included":                    whyBrowser,
	"GET /link-preview":                               whyBrowser,
	"PUT /org/hub":                                    whyAdmin,
	"PUT /org/permissions/{permission}":               whyAdmin,
	"GET /spaces/{spaceKey}/permissions":              whyAdmin,
	"PUT /spaces/{spaceKey}/permissions":              whyAdmin,
	"GET /pages/{pageID}/restrictions":                whyAdmin,
	"PUT /pages/{pageID}/restrictions":                whyAdmin,
	"GET /pages/{pageID}/access/{userID}":             whyAdmin,
	"GET /pages/{pageID}/viewers":                     whyAdmin,
	"GET /pages/{pageID}/share/recipients":            whyBrowser,
	"POST /pages/{pageID}/share":                      whyAttention,
	"GET /groups":                                     whyAdmin,
	"POST /spaces":                                    whyAdmin,
	"PATCH /spaces/{spaceKey}":                        whyAdmin,
	"GET /audit/facets":                               whyAdmin,
	"GET /audit/export":                               whyAdmin,
	"GET /armature/connection":                        whyAdmin,
	"PUT /armature/connection":                        whyAdmin,
	"DELETE /armature/connection":                     whyAdmin,
	"PUT /spaces/{spaceKey}/archive":                  whyAdmin,
	"DELETE /spaces/{spaceKey}/archive":               whyAdmin,
	"PUT /pages/{pageID}/archive":                     whyAdmin,
	"DELETE /pages/{pageID}/archive":                  whyAdmin,
	"GET /webhooks":                                   whyAdmin,
	"POST /webhooks":                                  whyAdmin,
	"PATCH /webhooks/{webhookID}":                     whyAdmin,
	"DELETE /webhooks/{webhookID}":                    whyAdmin,
	"POST /webhooks/{webhookID}/rotate-secret":        whyAdmin,
	"POST /webhooks/{webhookID}/test":                 whyAdmin,
	"GET /webhooks/{webhookID}/deliveries":            whyAdmin,
	"POST /webhooks/{webhookID}/deliveries/{deliveryID}/redeliver": whyAdmin,
	"DELETE /spaces/{spaceKey}":                                    whyRemoves,
	"DELETE /spaces/{spaceKey}/trash":                              whyRemoves,
	"DELETE /spaces/{spaceKey}/trash/{pageID}":                     whyRemoves,
	"DELETE /pages/{pageID}":                                       whyRemoves,
	"DELETE /pages/{pageID}/labels/{labelName}":                    whyRemoves,
	"DELETE /attachments/{attachmentID}":                           whyRemoves,
	"DELETE /comments/{commentID}":                                 whyRemoves,
	"GET /search/quick":                                            whyBrowser,
	"GET /recent-pages":                                            whyBrowser,
	"POST /pages/{pageID}/visit":                                   whyBrowser,
	"GET /pages/{pageID}/readers":                                  whyReaders,
	"GET /notifications/unread-count":                              whyBrowser,
	"POST /notifications/read":                                     whyBrowser,
	"GET /pages/{pageID}/mentionable":                              whyBrowser,
	"GET /home/edited":                                             whyBrowser,
	"GET /stars":                                                   whyBrowser,
	"GET /watches":                                                 whyBrowser,
	"GET /pages/{pageID}/watchers":                                 whyBrowser,
	"GET /pages/{pageID}/draft":                                    whyBrowser,
	"PUT /pages/{pageID}/draft":                                    whyBrowser,
	"DELETE /pages/{pageID}/draft":                                 whyBrowser,
	"GET /pages/{pageID}/export":                                   whyFiles,
	"POST /pages/{pageID}/attachments":                             whyFiles,
	"GET /attachments/{attachmentID}":                              whyFiles,
	"GET /attachments/{attachmentID}/preview":                      whyFiles,
	"GET /armature/issues":                                         whyArmature,
	"GET /armature/issues/{issueKey}":                              whyArmature,
	"GET /armature/search":                                         whyArmature,
	"GET /armature/chart":                                          whyArmature,
	"GET /armature/roadmap":                                        whyArmature,
	"GET /armature/calendar":                                       whyArmature,
	"GET /armature/projects":                                       whyArmature,
	"GET /armature/issue-types":                                    whyArmature,
	"POST /armature/issues":                                        whyArmature,
	"GET /pages/{pageID}/armature-links":                           whyArmature,
	"POST /pages/{pageID}/reactions":                               whyAttention,
	"DELETE /pages/{pageID}/reactions":                             whyAttention,
	"POST /comments/{commentID}/reactions":                         whyAttention,
	"DELETE /comments/{commentID}/reactions":                       whyAttention,
	"PUT /pages/{pageID}/owner":                                    whyAttention,
	"DELETE /pages/{pageID}/owner":                                 whyAttention,
	"PUT /pages/{pageID}/verification":                             whyAttention,
	"DELETE /pages/{pageID}/verification":                          whyAttention,
	"PUT /pages/{pageID}/watch":                                    whyAttention,
	"DELETE /pages/{pageID}/watch":                                 whyAttention,
	"PUT /spaces/{spaceKey}/watch":                                 whyAttention,
	"DELETE /spaces/{spaceKey}/watch":                              whyAttention,
	"PUT /pages/{pageID}/star":                                     whyAttention,
	"DELETE /pages/{pageID}/star":                                  whyAttention,
	"PUT /spaces/{spaceKey}/star":                                  whyAttention,
	"DELETE /spaces/{spaceKey}/star":                               whyAttention,
	"GET /spaces/{spaceKey}/trash":                                 whyReorganize,
	"POST /spaces/{spaceKey}/trash/{pageID}/restore":               whyReorganize,
	"POST /pages/{pageID}/move":                                    whyReorganize,
	"POST /pages/{pageID}/copy":                                    whyReorganize,
	"POST /pages/{pageID}/publish":                                 whyReorganize,
	"POST /pages/{pageID}/versions/{versionNumber}/restore":        whyReorganize,
	"POST /pages/{pageID}/inline-comments":                         whyThreads,
	"PATCH /comments/{commentID}":                                  whyThreads,
	"POST /comments/{commentID}/resolve":                           whyThreads,
	"POST /comments/{commentID}/reopen":                            whyThreads,

	// Shortcuts are the space's administrators' furniture, kept where they show.
	"POST /spaces/{spaceKey}/shortcuts":                   whyAdmin,
	"POST /spaces/{spaceKey}/shortcuts/{shortcutID}/move": whyAdmin,
	"DELETE /spaces/{spaceKey}/shortcuts/{shortcutID}":    whyRemoves,

	"DELETE /calendars/{calendarID}":                  whyRemoves,
	"DELETE /calendars/{calendarID}/events/{eventID}": whyRemoves,
}

// Offering an operation to assistants is decided for each one: a route added
// to the table without a tool name or a line in notTools fails here.
func TestEveryOperationHasAnMCPDecision(t *testing.T) {
	seen := map[string]bool{}
	for _, op := range operations {
		key := op.method + " " + op.path
		seen[key] = true
		_, declined := notTools[key]
		switch {
		case op.tool == "" && !declined:
			t.Errorf("%s is neither a tool nor declined in notTools; decide whether assistants get it", key)
		case op.tool != "" && declined:
			t.Errorf("%s is a tool and declined at once", key)
		case op.tool != "" && (op.public || op.pending || op.binary && op.method != http.MethodGet):
			t.Errorf("%s cannot be a tool: it is public, pending or answers a file to a write", key)
		case op.tool != "" && op.multipart != (op.toolFile != ""):
			t.Errorf("%s: a multipart tool, and only one, names the file it sends", key)
		case op.tool != "" && op.method == http.MethodDelete:
			t.Errorf("%s: no tool removes anything", key)
		}
	}
	for key := range notTools {
		if !seen[key] {
			t.Errorf("notTools declines %s, which is not in the table", key)
		}
	}
}

func TestEveryToolIsNamedOnceAndExplained(t *testing.T) {
	seen := map[string]string{}
	for _, op := range operations {
		if op.tool == "" {
			continue
		}
		key := op.method + " " + op.path
		if prior, ok := seen[op.tool]; ok {
			t.Errorf("%s and %s share the tool name %s", prior, key, op.tool)
		}
		seen[op.tool] = key
		if !strings.HasSuffix(op.toolHelp, ".") {
			t.Errorf("%s: the tool sentence %q does not end", key, op.toolHelp)
		}
		if strings.ToLower(op.tool) != op.tool || strings.ContainsAny(op.tool, "- ") {
			t.Errorf("%s: %q is not a snake_case tool name", key, op.tool)
		}
	}
	if len(seen) < 20 {
		t.Errorf("only %d tools are marked", len(seen))
	}
}

func TestAToolRequiresItsPathAndReadsWhenItGets(t *testing.T) {
	for _, tool := range toolCatalog() {
		if tool.ReadOnly != (tool.Method == http.MethodGet) {
			t.Errorf("%s: readOnly %v for %s", tool.Name, tool.ReadOnly, tool.Method)
		}
		for _, name := range pathParams(tool.Path) {
			if _, ok := tool.Input.Properties[name]; !ok {
				t.Errorf("%s: the schema lacks the path value %s", tool.Name, name)
			}
			if !contains(tool.Input.Required, name) {
				t.Errorf("%s: %s is not required", tool.Name, name)
			}
		}
		encoded, err := json.Marshal(tool.Input)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "#/components/") {
			t.Errorf("%s: the schema refers outside itself: %s", tool.Name, encoded)
		}
	}
	create, _ := toolByName("create_page")
	for _, field := range []string{"parentId", "title", "body", "publish"} {
		if _, ok := create.Input.Properties[field]; !ok {
			t.Errorf("create_page does not take %s", field)
		}
	}
	search, _ := toolByName("search")
	if q := search.Input.Properties["q"]; q == nil || q.Type != "string" {
		t.Error("search does not take q as a string")
	}
	if space := search.Input.Properties["space"]; space == nil || space.Type != "array" {
		t.Error("a repeated query parameter is not an array")
	}
	replace, _ := toolByName("replace_page_markdown")
	if !contains(replace.Input.Required, mcpContentArg) || replace.Input.Properties["version"] == nil {
		t.Errorf("replace_page_markdown does not take its content and version: %+v", replace.Input)
	}
}

func TestAReadOnlyTokenIsOfferedOnlyReadingTools(t *testing.T) {
	all, reading := listTools(false, false), listTools(true, false)
	if len(reading) == 0 || len(reading) >= len(all) {
		t.Fatalf("%d tools for a read token of %d", len(reading), len(all))
	}
	for _, tool := range reading {
		if tool["annotations"].(map[string]any)["readOnlyHint"] != true {
			t.Errorf("a read token is offered %s", tool["name"])
		}
	}
	names := map[string]bool{}
	for _, tool := range all {
		names[tool["name"].(string)] = true
	}
	for _, want := range []string{"create_page", "update_page", "replace_page_markdown", "get_page_markdown", "search"} {
		if !names[want] {
			t.Errorf("%s is not offered", want)
		}
	}
}

func TestTheEndpointSpeaksJSONRPC(t *testing.T) {
	s := &Server{}
	call := func(body string, p *auth.Principal) (int, rpcResponse) {
		req := httptest.NewRequest(http.MethodPost, mcpPath, strings.NewReader(body))
		if p != nil {
			req = req.WithContext(context.WithValue(req.Context(), ctxPrincipal, p))
		}
		rec := httptest.NewRecorder()
		s.handleMCP(rec, req)
		var resp rpcResponse
		if rec.Code == http.StatusOK {
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("not a JSON-RPC answer: %s", rec.Body.String())
			}
		}
		return rec.Code, resp
	}

	status, resp := call(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`, nil)
	if status != http.StatusOK || resp.Error != nil || !strings.Contains(string(resp.Result), MCPProtocolVersion) {
		t.Fatalf("initialize: %d %+v", status, resp)
	}
	if string(resp.ID) != "1" {
		t.Errorf("the id %s was not echoed", resp.ID)
	}

	status, resp = call(`{"jsonrpc":"2.0","id":"a","method":"tools/list"}`, nil)
	if status != http.StatusOK || !strings.Contains(string(resp.Result), `"get_page"`) || !strings.Contains(string(resp.Result), `"create_page"`) {
		t.Fatalf("tools/list: %d %s", status, resp.Result)
	}
	reader := &auth.Principal{UserID: uuid.New(), TokenID: &uuid.UUID{}, Scopes: []string{auth.ScopeRead}}
	_, resp = call(`{"jsonrpc":"2.0","id":"b","method":"tools/list"}`, reader)
	if !strings.Contains(string(resp.Result), `"get_page"`) || strings.Contains(string(resp.Result), `"create_page"`) {
		t.Errorf("a read token is offered: %s", resp.Result)
	}
	_, resp = call(`{"jsonrpc":"2.0","id":"c","method":"tools/call","params":{"name":"create_page","arguments":{}}}`, reader)
	if resp.Error != nil || !strings.Contains(string(resp.Result), `"isError":true`) || !strings.Contains(string(resp.Result), "can only read") {
		t.Errorf("a read token calling a writing tool: %+v %s", resp.Error, resp.Result)
	}

	if status, _ = call(`{"jsonrpc":"2.0","method":"notifications/initialized"}`, nil); status != http.StatusAccepted {
		t.Errorf("a notification answers %d", status)
	}
	if _, resp = call(`{"jsonrpc":"2.0","id":2,"method":"nothing/here"}`, nil); resp.Error == nil || resp.Error.Code != rpcMethodNotFound {
		t.Errorf("an unknown method answers %+v", resp.Error)
	}
	if _, resp = call(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"no_such_tool"}}`, nil); resp.Error == nil || resp.Error.Code != rpcInvalidParams {
		t.Errorf("an unknown tool answers %+v", resp.Error)
	}
	if _, resp = call(`[{"jsonrpc":"2.0","id":4,"method":"ping"}]`, nil); resp.Error == nil || resp.Error.Code != rpcInvalidRequest {
		t.Errorf("a batch answers %+v", resp.Error)
	}
	if _, resp = call(`{"id":5,"method":"ping"}`, nil); resp.Error == nil || resp.Error.Code != rpcInvalidRequest {
		t.Errorf("a request without jsonrpc 2.0 answers %+v", resp.Error)
	}
	if status, _ = call(`not json`, nil); status != http.StatusBadRequest {
		t.Errorf("malformed JSON answers %d", status)
	}
	_, resp = call(`{"jsonrpc":"2.0","id":6,"method":"resources/read","params":{"uri":"`+mcpOpenAPIResource+`"}}`, nil)
	if resp.Error != nil || !strings.Contains(string(resp.Result), `/mcp`) {
		t.Errorf("the document is not a resource: %+v", resp.Error)
	}
}

func TestAToolCallBecomesTheHTTPCallItStandsFor(t *testing.T) {
	tool, _ := toolByName("search")
	outer := httptest.NewRequest(http.MethodPost, mcpPath, nil)
	outer.Header.Set("Authorization", "Bearer stator_pat_x")
	inner, err := tool.request(outer, map[string]json.RawMessage{
		"q": json.RawMessage(`"release notes"`), "limit": json.RawMessage(`5`), "space": json.RawMessage(`["ENG","OPS"]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if inner.Method != http.MethodGet || inner.URL.Path != APIPrefix+"/search" {
		t.Errorf("inner call is %s %s", inner.Method, inner.URL.Path)
	}
	q := inner.URL.Query()
	if q.Get("q") != "release notes" || q.Get("limit") != "5" || len(q["space"]) != 2 {
		t.Errorf("query = %s", inner.URL.RawQuery)
	}
	if inner.Header.Get("Authorization") != "Bearer stator_pat_x" {
		t.Error("the credential did not travel")
	}

	get, _ := toolByName("get_page")
	if _, err := get.request(outer, nil); err == nil {
		t.Error("a missing path value was accepted")
	}
	if _, err := get.request(outer, map[string]json.RawMessage{"pageID": json.RawMessage(`"` + uuid.NewString() + `"`), "extra": json.RawMessage(`1`)}); err == nil {
		t.Error("an argument a GET cannot carry was accepted")
	}

	create, _ := toolByName("create_page")
	parent := uuid.NewString()
	inner, err = create.request(outer, map[string]json.RawMessage{"parentId": json.RawMessage(`"` + parent + `"`), "title": json.RawMessage(`"hello"`)})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(inner.Body)
	if inner.Method != http.MethodPost || inner.URL.Path != APIPrefix+"/pages" || !strings.Contains(string(body), `"title":"hello"`) {
		t.Errorf("create call: %s %s %s", inner.Method, inner.URL.Path, body)
	}

	replace, _ := toolByName("replace_page_markdown")
	page := uuid.NewString()
	inner, err = replace.request(outer, map[string]json.RawMessage{
		"pageID": json.RawMessage(`"` + page + `"`), "version": json.RawMessage(`3`), mcpContentArg: json.RawMessage(`"# Title\n\nWords."`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if inner.Method != http.MethodPut || inner.URL.Path != APIPrefix+"/pages/"+page+"/markdown" || inner.URL.Query().Get("version") != "3" {
		t.Errorf("replace call: %s %s", inner.Method, inner.URL)
	}
	kind, params, err := mime.ParseMediaType(inner.Header.Get("Content-Type"))
	if err != nil || kind != "multipart/form-data" {
		t.Fatalf("replace sends %q", inner.Header.Get("Content-Type"))
	}
	part, err := multipart.NewReader(inner.Body, params["boundary"]).NextPart()
	if err != nil {
		t.Fatal(err)
	}
	text, _ := io.ReadAll(part)
	if part.FormName() != "file" || part.FileName() != "page.md" || string(text) != "# Title\n\nWords." {
		t.Errorf("the part is %s %s %q", part.FormName(), part.FileName(), text)
	}
	if _, err := replace.request(outer, map[string]json.RawMessage{"pageID": json.RawMessage(`"` + page + `"`)}); err == nil {
		t.Error("a multipart tool without its content was accepted")
	}
	if _, err := replace.request(outer, map[string]json.RawMessage{"pageID": json.RawMessage(`"` + page + `"`), mcpContentArg: json.RawMessage(`"x"`), "title": json.RawMessage(`"y"`)}); err == nil {
		t.Error("a multipart tool took an argument it cannot send")
	}
}

// Through the whole router a read token reaches the endpoint, its reading tool
// calls go on to their routes, and its writing ones stop at the tool.
func TestAReadTokenReadsThroughTheEndpointAndWritesNothing(t *testing.T) {
	router := tokenServer(t).Routes(nil)
	rpc := func(credential, body string) map[string]any {
		t.Helper()
		resp, decoded := serve(t, router, withBearer(http.MethodPost, mcpPath, credential, body))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d %v", body, resp.StatusCode, decoded)
		}
		result, _ := decoded["result"].(map[string]any)
		if result == nil {
			t.Fatalf("%s: no result in %v", body, decoded)
		}
		return result
	}
	listed := rpc("reader", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)["tools"].([]any)
	if len(listed) != len(listTools(true, false)) {
		t.Errorf("a read token is offered %d tools, want %d", len(listed), len(listTools(true, false)))
	}
	refused := rpc("reader", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"update_page","arguments":{"pageID":"`+uuid.NewString()+`","version":1}}}`)
	if refused["isError"] != true || !strings.Contains(refused["content"].([]any)[0].(map[string]any)["text"].(string), "can only read") {
		t.Errorf("a read token's write: %v", refused)
	}
	written := rpc("full", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"update_page","arguments":{"pageID":"`+uuid.NewString()+`","version":1}}}`)
	if text := written["content"].([]any)[0].(map[string]any)["text"].(string); strings.Contains(text, "can only read") {
		t.Errorf("a full token's write was refused as a read token's: %s", text)
	}
}

func TestAResultTooLargeIsCutAndSaysSo(t *testing.T) {
	rec := &capture{header: http.Header{}}
	rec.WriteHeader(http.StatusOK)
	_, _ = rec.Write([]byte(`{"text":"` + strings.Repeat("a", MCPMaxToolResultBytes) + `"}`))
	got := rec.toolResult()
	if got.StructuredContent != nil || !strings.HasSuffix(got.Content[0].Text, string(truncatedNote)) {
		t.Errorf("a long result was not cut: %d bytes", len(got.Content[0].Text))
	}
	refused := &capture{header: http.Header{}}
	refused.WriteHeader(http.StatusNotFound)
	_, _ = refused.Write([]byte(`{"error":{"code":"not_found","message":"There is no such page."}}`))
	if r := refused.toolResult(); !r.IsError || r.Content[0].Text != "There is no such page." {
		t.Errorf("a refusal reads %+v", r)
	}
}

func contains(list []string, want string) bool {
	for _, each := range list {
		if each == want {
			return true
		}
	}
	return false
}
