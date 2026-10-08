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
call is made. A token limited to spaces sees those spaces alone, as it does
over HTTP, and is not offered the tools the route table marks `orgWide`,
such as `list_audit_log`; one called anyway is refused with the
`spaces_token` sentence. A guest of a space, who has no tokens and reaches
the tools with their session, is held the same way: their space and the
people in it alone, no `orgWide` tool offered, and one called anyway refused
with the `guest` sentence. A result longer than 64 KiB is cut and says how
to ask for less.

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
| `list_space_shortcuts` | `GET /spaces/{spaceKey}/shortcuts` | yes |
| `list_pages_below` | `GET /pages/{pageID}/below` | yes |
| `list_templates` | `GET /templates`, with a space's own | yes |
| `get_template` | `GET /templates/{templateKey}` | yes |
| `list_versions` | `GET /pages/{pageID}/versions` | yes |
| `list_page_contributors` | `GET /pages/{pageID}/contributors` | yes |
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
| `list_calendars` | `GET /spaces/{spaceKey}/calendars` | yes |
| `list_calendar_events` | `GET /calendars/{calendarID}/events` | yes |
| `get_blog` | `GET /spaces/{spaceKey}/blog` | yes |
| `list_posts` | `GET /posts` | yes |
| `create_page` | `POST /pages` | no |
| `create_page_from_template` | `POST /templates/{templateKey}/pages` | no |
| `create_post` | `POST /spaces/{spaceKey}/posts` | no |
| `update_page` | `PATCH /pages/{pageID}` | no |
| `replace_page_markdown` | `PUT /pages/{pageID}/markdown` | no |
| `import_markdown` | `POST /pages/{pageID}/import` | no |
| `add_page_label` | `POST /pages/{pageID}/labels` | no |
| `add_comment` | `POST /pages/{pageID}/comments` | no |
| `reply_to_comment` | `POST /comments/{commentID}/replies` | no |
| `set_task_done` | `PATCH /pages/{pageID}/tasks/{taskID}` | no |
| `create_calendar` | `POST /spaces/{spaceKey}/calendars` | no |
| `rename_calendar` | `PATCH /calendars/{calendarID}` | no |
| `create_calendar_event` | `POST /calendars/{calendarID}/events` | no |
| `update_calendar_event` | `PUT /calendars/{calendarID}/events/{eventID}` | no |

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
  tokens, permissions and copying them between spaces, previews included,
  page restrictions, making or changing spaces and the
  space templates a new one starts from, and
  archiving or unarchiving pages and spaces and pinning, ordering or
  removing a space's shortcuts, which is for a space's administrators, the
  organization's webhooks, and keeping templates;
- who read a page (`GET /pages/{pageID}/readers`), which stays with its
  editors in the page; `get_page_views` counts them;
- the caller's own session, settings, tokens, themes and Armature account;
- what reaches other people or a page's standing: shares, reactions,
  watches, stars, owners and verification;
- moving, copying, restoring, publishing and scheduling drafts, which a
  person does in the tree, the trash, the history or the editor;
- inline threads, rewriting and resolving comments, file uploads and
  downloads, and the browser's furniture (typeahead, badges, pickers,
  drafts, a live page's saves and the choice between drafts and live);
- the shared draft of a page edited together, a WebSocket a browser holds
  open while its person edits, not a call and an answer;
- a page as PDF (`GET /pages/{pageID}/pdf`), a file printed by a browser
  for a person to keep or hand on; `get_page` and `get_page_markdown`
  carry the same words to a model;
- exporting and importing whole spaces (`/spaces/{spaceKey}/exports`,
  `/space-exports`, `/space-imports`), an administrator's act on a file the
  worker makes or reads; `get_space_outline` and `get_page_markdown` carry
  a space's words;
- a page as a Word document (`GET /pages/{pageID}/docx`), a file for a
  person to edit offline or hand on; `get_page` and `get_page_markdown`
  carry the same words to a model;
- Word import (`POST /pages/{pageID}/import/docx`, `/word-imports` and
  `GET /word-imports/{importID}`): a Word document is a file a person
  uploads, and several are imported by the worker; `import_markdown`
  carries a model's words into a new page;
- Armature's issues, which Armature's own MCP endpoint serves as the person;
- the reading view for people who are not signed in (`/public/{orgSlug}`
  and below, a public link's page and both their PDFs and Word documents
  included) and the
  switches that open it:
  an assistant acts for a member, for whom `get_page` and `search` read the
  same pages;
- a page's public links (`/pages/{pageID}/public-links`), which open the
  page to anybody outside the organization: a decision its editors make in
  the share dialog, and a token shown once, to a person.
