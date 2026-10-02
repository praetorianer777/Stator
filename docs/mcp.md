# MCP server

Stator speaks the Model Context Protocol, so an assistant can search, read
and write documentation as the person whose token it holds. The server is
Armature's, adapted: the tools are rows of the route table
(`backend/internal/httpapi/openapi.go`), and each call is the HTTP call it
stands for, run in the api process as the caller. `docs/decisions.md` says
why.

## Connecting a client

The endpoint is `POST /api/v1/mcp` on the address people open Stator at,
over the streamable HTTP transport without sessions: one JSON-RPC 2.0 request
per POST, answered as JSON. Make a personal access token under Tokens in the
account menu and give it to the client as a bearer:

```json
{
  "mcpServers": {
    "stator": {
      "type": "http",
      "url": "https://wiki.example.com/api/v1/mcp",
      "headers": { "Authorization": "Bearer stator_pat_..." }
    }
  }
}
```

The tokens page shows the same settings with this deployment's address. A
token acts in the one organization it was made in. A signed-in session
reaches the endpoint as it reaches every route, but a client holds a token.
There is no OAuth flow, as in Armature.

The methods are `initialize`, `ping`, `tools/list`, `tools/call`,
`resources/list` and `resources/read`; notifications are accepted with 202.
The one resource, `stator://openapi.json`, is the whole API document. Batches
are refused.

## What a call may do

A tool call is built into the request the browser would send (path values
into the address, query values onto it, the rest as the JSON body) and run
through the router's whole chain: authentication, the read-only rule, the
use check, the handler and the database's row level security, as the
caller. So a tool answers exactly what the HTTP call answers that person,
and is refused exactly where it is, with the same sentence. Nothing in the
MCP code decides a permission.

A read-only token is offered only the tools that read (`GET` routes), and a
writing tool called anyway is refused with the read-only sentence before the
call is made. A result longer than 64 KiB is cut and says how to ask for
less.

## Tools

| Tool | Operation | Reads |
|---|---|---|
| `whoami` | `GET /auth/me` | yes |
| `list_spaces` | `GET /spaces` | yes |
| `get_space` | `GET /spaces/{spaceKey}` | yes |
| `list_child_pages` | `GET /spaces/{spaceKey}/pages` | yes |
| `get_space_outline` | `GET /spaces/{spaceKey}/outline` | yes |
| `get_page` | `GET /pages/{pageID}` | yes |
| `get_page_markdown` | `GET /pages/{pageID}/markdown` | yes |
| `list_archived_pages` | `GET /spaces/{spaceKey}/archived-pages` | yes |
| `list_pages_below` | `GET /pages/{pageID}/below` | yes |
| `list_templates` | `GET /templates` | yes |
| `get_template` | `GET /templates/{templateKey}` | yes |
| `list_versions` | `GET /pages/{pageID}/versions` | yes |
| `get_version` | `GET /pages/{pageID}/versions/{versionNumber}` | yes |
| `compare_versions` | `GET /pages/{pageID}/compare` | yes |
| `search` | `GET /search` | yes |
| `list_page_labels` | `GET /pages/{pageID}/labels` | yes |
| `list_labels` | `GET /labels` | yes |
| `list_label_pages` | `GET /labels/{labelName}/pages` | yes |
| `list_people` | `GET /people` | yes |
| `list_attachments` | `GET /pages/{pageID}/attachments` | yes |
| `list_comments` | `GET /pages/{pageID}/comments` | yes |
| `get_comment_thread` | `GET /comments/{commentID}` | yes |
| `list_recent_updates` | `GET /home/updates` | yes |
| `list_notifications` | `GET /notifications` | yes |
| `list_audit_log` | `GET /audit` (administrators) | yes |
| `list_stale_pages` | `GET /stale-pages` (administrators of a space) | yes |
| `get_page_views` | `GET /pages/{pageID}/views` | yes |
| `list_my_tasks` | `GET /tasks` | yes |
| `create_page` | `POST /pages` | no |
| `update_page` | `PATCH /pages/{pageID}` | no |
| `replace_page_markdown` | `PUT /pages/{pageID}/markdown` | no |
| `import_markdown` | `POST /pages/{pageID}/import` | no |
| `add_page_label` | `POST /pages/{pageID}/labels` | no |
| `add_comment` | `POST /pages/{pageID}/comments` | no |
| `reply_to_comment` | `POST /comments/{commentID}/replies` | no |
| `set_task_done` | `PATCH /pages/{pageID}/tasks/{taskID}` | no |

`tools/list` gives each tool's input schema: path values, query values and
body fields in one object, every type it refers to carried along in
`$defs`. A page's body and a comment's are documents as `get_page` returns
them; Markdown is the easier way in and out. The two Markdown writing tools
take the file's text as `content` and send it as an upload named `page.md`,
or `fileName`; `docs/markdown.md` says what the conversion keeps.

## What is not a tool

Every operation in the table is either a tool or declined with a reason in
`backend/internal/httpapi/mcp_test.go`, and a route added without one fails
the unit tests. Declined, following Armature's rule of reads and safe writes:

- anything that deletes or takes something away, the trash included;
- administration and who may do what: the identity provider, members,
  tokens, permissions, page restrictions, making or changing spaces, and
  archiving or unarchiving pages and spaces, which is for a space's
  administrators, and the organization's webhooks;
- who read a page (`GET /pages/{pageID}/readers`), which stays with its
  editors in the page; `get_page_views` counts them;
- the caller's own session, settings, tokens, themes and Armature account;
- what reaches other people or a page's standing: shares, reactions,
  watches, stars, owners and verification;
- moving, copying, restoring and publishing drafts, which a person does in
  the tree, the trash or the history;
- inline threads, rewriting and resolving comments, file uploads and
  downloads, and the browser's furniture (typeahead, badges, pickers,
  drafts);
- Armature's issues, which Armature's own MCP endpoint serves as the person.
