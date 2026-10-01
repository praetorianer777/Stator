//go:build integration

package test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// mcpSession speaks JSON-RPC to the MCP endpoint as one client.
type mcpSession struct {
	c  *client
	id int
}

func (m *mcpSession) rpc(t *testing.T, method string, params any) response {
	t.Helper()
	m.id++
	body := map[string]any{"jsonrpc": "2.0", "id": m.id, "method": method}
	if params != nil {
		body["params"] = params
	}
	return m.c.post(t, "/api/v1/mcp", body)
}

func (m *mcpSession) result(t *testing.T, method string, params any) map[string]any {
	t.Helper()
	resp := m.rpc(t, method, params)
	if resp.Status != http.StatusOK {
		t.Fatalf("%s answered %d: %s", method, resp.Status, resp.Raw)
	}
	if e, ok := resp.Body["error"]; ok && e != nil {
		t.Fatalf("%s: rpc error %v", method, e)
	}
	out, _ := resp.Body["result"].(map[string]any)
	return out
}

func (m *mcpSession) call(t *testing.T, name string, args map[string]any) map[string]any {
	t.Helper()
	return m.result(t, "tools/call", map[string]any{"name": name, "arguments": args})
}

func (m *mcpSession) tools(t *testing.T) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	for _, each := range m.result(t, "tools/list", nil)["tools"].([]any) {
		names[each.(map[string]any)["name"].(string)] = true
	}
	return names
}

func toolText(res map[string]any) string {
	return res["content"].([]any)[0].(map[string]any)["text"].(string)
}

func structured(t *testing.T, res map[string]any, key string) map[string]any {
	t.Helper()
	if res["isError"] == true {
		t.Fatalf("the tool refused: %s", toolText(res))
	}
	out, _ := res["structuredContent"].(map[string]any)[key].(map[string]any)
	if out == nil {
		t.Fatalf("no %s in %v", key, res)
	}
	return out
}

// MCP is the route table run in process: a tool answers what the HTTP call
// answers the same person, and is refused exactly where that call is.
func TestAnAssistantWorksAsThePersonWhoseTokenItHolds(t *testing.T) {
	h := newHarness(t)
	api := newAPIServer(t, h)
	home := h.makeMember(t, "mcp")
	slug := h.slugOf(t, home.org)
	owner := api.as(t, home.user, home.org, slug)
	bobID := h.addPerson(t, home.org, "member")
	bob := api.as(t, bobID, home.org, slug)

	docs := newTree(t, owner, "MCP", "Tools")
	open := docs.add(docs.homeID, "Open", map[string]any{"body": textDoc("the zebracorn handbook")})
	secret := docs.add(docs.homeID, "Secret", map[string]any{"body": textDoc("the zebracorn conspiracy")})
	locked := docs.add(docs.homeID, "Locked", map[string]any{"body": textDoc("read but do not touch")})
	want(t, restrict(t, owner, secret, []any{user(home.user)}, []any{user(home.user)}), http.StatusOK, "hide Secret from bob")
	want(t, restrict(t, owner, locked, nil, []any{user(home.user)}), http.StatusOK, "lock Locked against bob")

	_, full := makeToken(t, bob, map[string]any{"name": "assistant"})
	_, readOnly := makeToken(t, bob, map[string]any{"name": "reader", "scopes": []string{"read"}})
	agent := &mcpSession{c: api.withToken(full)}
	reader := &mcpSession{c: api.withToken(readOnly)}

	t.Run("it introduces itself and offers the tools the token may use", func(t *testing.T) {
		init := agent.result(t, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "0"}})
		if init["protocolVersion"] != "2025-06-18" || init["serverInfo"].(map[string]any)["name"] != "stator" {
			t.Fatalf("initialize = %v", init)
		}
		want(t, agent.c.post(t, "/api/v1/mcp", map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}), http.StatusAccepted, "a notification")
		names := agent.tools(t)
		for _, name := range []string{"get_page", "get_page_markdown", "search", "create_page", "update_page", "replace_page_markdown"} {
			if !names[name] {
				t.Errorf("%s is not offered: %v", name, names)
			}
		}
		read := reader.tools(t)
		if !read["get_page_markdown"] || !read["search"] || read["create_page"] || read["update_page"] || read["replace_page_markdown"] {
			t.Errorf("a read token is offered %v", read)
		}
	})

	t.Run("a page is read as the HTTP call reads it, and as Markdown", func(t *testing.T) {
		got := structured(t, agent.call(t, "get_page", map[string]any{"pageID": open}), "page")
		direct := obj(t, bob.get(t, pagePath(open)), "page")
		if got["title"] != direct["title"] || got["version"] != direct["version"] {
			t.Errorf("the tool says %v, the call says %v", got, direct)
		}
		md := agent.call(t, "get_page_markdown", map[string]any{"pageID": open})
		if md["isError"] == true || !strings.HasPrefix(toolText(md), "# Open\n") || !strings.Contains(toolText(md), "zebracorn handbook") {
			t.Errorf("the page as Markdown: %v", md)
		}
	})

	t.Run("search finds only what the person may see", func(t *testing.T) {
		mine := agent.call(t, "search", map[string]any{"q": "zebracorn", "space": []string{"MCP"}})
		if mine["isError"] == true || mine["structuredContent"].(map[string]any)["total"].(float64) != 1 || strings.Contains(toolText(mine), secret) {
			t.Errorf("bob's search: %s", toolText(mine))
		}
		all := obj(t, owner.get(t, "/api/v1/search?q=zebracorn&space=MCP"))
		if all["total"].(float64) != 2 {
			t.Errorf("the owner finds %v, so the search proves nothing", all["total"])
		}
	})

	var made string
	t.Run("a page is written, then rewritten as Markdown, and read back at once", func(t *testing.T) {
		created := structured(t, agent.call(t, "create_page", map[string]any{"parentId": docs.homeID, "title": "Written by a tool", "body": textDoc("first words"), "publish": true}), "page")
		made = created["id"].(string)
		updated := structured(t, agent.call(t, "update_page", map[string]any{"pageID": made, "title": "Retitled by a tool", "version": created["version"]}), "page")
		replaced := structured(t, agent.call(t, "replace_page_markdown", map[string]any{
			"pageID": made, "version": updated["version"], "content": "# Rewritten in Markdown\n\nWith **strong** words.\n",
		}), "page")
		if replaced["title"] != "Rewritten in Markdown" {
			t.Errorf("the Markdown title did not land: %v", replaced["title"])
		}
		md := toolText(agent.call(t, "get_page_markdown", map[string]any{"pageID": made}))
		if !strings.Contains(md, "**strong**") {
			t.Errorf("the tool does not read back its own write: %s", md)
		}
		direct := obj(t, owner.get(t, pagePath(made)), "page")
		if direct["title"] != "Rewritten in Markdown" || direct["version"].(float64) != 3 {
			t.Errorf("the API shows %v", direct)
		}
		imported := agent.call(t, "import_markdown", map[string]any{"pageID": made, "content": "# A child from Markdown\n\nHello.\n"})
		if imported["isError"] == true || !strings.Contains(toolText(imported), "A child from Markdown") {
			t.Errorf("import_markdown: %s", toolText(imported))
		}
		comment := agent.call(t, "add_comment", map[string]any{"pageID": made, "body": textDoc("a note from a tool")})
		if comment["isError"] == true {
			t.Errorf("add_comment: %s", toolText(comment))
		}
		label := agent.call(t, "add_page_label", map[string]any{"pageID": made, "name": "tooling"})
		if label["isError"] == true {
			t.Errorf("add_page_label: %s", toolText(label))
		}
	})

	t.Run("it is refused exactly where the API refuses the person", func(t *testing.T) {
		for name, pair := range map[string]struct {
			tool   map[string]any
			direct response
		}{
			"a hidden page": {
				agent.call(t, "get_page", map[string]any{"pageID": secret}),
				bob.get(t, pagePath(secret)),
			},
			"a hidden page as Markdown": {
				agent.call(t, "get_page_markdown", map[string]any{"pageID": secret}),
				bob.get(t, pagePath(secret, "/markdown")),
			},
			"a locked page": {
				agent.call(t, "update_page", map[string]any{"pageID": locked, "title": "Taken", "version": 1}),
				bob.patch(t, pagePath(locked), map[string]any{"title": "Taken", "version": 1}),
			},
			"a page under a locked one": {
				agent.call(t, "create_page", map[string]any{"parentId": locked, "title": "Planted"}),
				bob.post(t, "/api/v1/pages", map[string]any{"parentId": locked, "title": "Planted"}),
			},
			"the audit log": {
				agent.call(t, "list_audit_log", map[string]any{}),
				bob.get(t, "/api/v1/audit"),
			},
		} {
			if pair.direct.Status < http.StatusBadRequest {
				t.Errorf("%s: the API did not refuse bob (%d), so the test proves nothing", name, pair.direct.Status)
				continue
			}
			message, _ := obj(t, pair.direct, "error")["message"].(string)
			if pair.tool["isError"] != true || toolText(pair.tool) != message {
				t.Errorf("%s: the tool says %q, the API %q", name, toolText(pair.tool), message)
			}
		}
		if title := obj(t, owner.get(t, pagePath(locked)), "page")["title"]; title != "Locked" {
			t.Errorf("Locked is now %v", title)
		}
	})

	t.Run("the database refuses bob the same, with no API in between", func(t *testing.T) {
		conn := appConn(t)
		actAs(t, conn, home.org, bobID)
		var seen int
		if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM page WHERE id = $1`, secret).Scan(&seen); err != nil || seen != 0 {
			t.Errorf("bob reads %d rows of Secret (%v)", seen, err)
		}
		denied(t, conn, "retitling Locked", `UPDATE page SET title = 'Taken' WHERE id = $1`, locked)
		denied(t, conn, "a page under Locked", `INSERT INTO page (org_id, space_id, parent_id, rank, title, created_by) VALUES ($1, (SELECT space_id FROM page WHERE id = $2), $2, 'W', 'Planted', $3)`, home.org, locked, bobID)
	})

	t.Run("a read token reads and writes nothing", func(t *testing.T) {
		if got := reader.call(t, "get_page_markdown", map[string]any{"pageID": open}); got["isError"] == true {
			t.Fatalf("a read token cannot read: %s", toolText(got))
		}
		before := h.count(t, home.ctx, `SELECT count(*) FROM page`)
		res := reader.call(t, "create_page", map[string]any{"parentId": docs.homeID, "title": "Not allowed"})
		if res["isError"] != true || !strings.Contains(toolText(res), "can only read") {
			t.Errorf("a read token wrote: %v", res)
		}
		res = reader.call(t, "replace_page_markdown", map[string]any{"pageID": made, "version": 3, "content": "# Not allowed\n"})
		if res["isError"] != true || !strings.Contains(toolText(res), "can only read") {
			t.Errorf("a read token replaced a page: %v", res)
		}
		if after := h.count(t, home.ctx, `SELECT count(*) FROM page`); after != before {
			t.Errorf("a read token made %d pages", after-before)
		}
	})

	t.Run("the openapi document is a resource", func(t *testing.T) {
		list := agent.result(t, "resources/list", nil)["resources"].([]any)
		uri := list[0].(map[string]any)["uri"].(string)
		read := agent.result(t, "resources/read", map[string]any{"uri": uri})
		if text := read["contents"].([]any)[0].(map[string]any)["text"].(string); !strings.Contains(text, `"/mcp"`) {
			t.Error("the document does not describe the endpoint itself")
		}
	})

	t.Run("the endpoint refuses what is not a request", func(t *testing.T) {
		want(t, agent.c.send(t, http.MethodPost, "/api/v1/mcp", "application/json", strings.NewReader("not json")), http.StatusBadRequest, "malformed JSON")
		want(t, api.anonymous().post(t, "/api/v1/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}), http.StatusUnauthorized, "no credential")
		unknown := agent.rpc(t, "nothing/here", nil)
		if code := unknown.Body["error"].(map[string]any)["code"].(float64); code != -32601 {
			t.Errorf("an unknown method answered %v", unknown.Body["error"])
		}
	})
}
