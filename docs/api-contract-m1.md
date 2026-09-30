# API contract for M1: drafts, history, search, permissions, attachments

The agreement between whoever builds the backend and whoever builds the web
client of #13, #14, #17, #18, #19 and #20. The route table in
`backend/internal/httpapi/openapi.go` is the source of truth for paths, fields
and types; this file says what they mean. Every operation below is in the
table with `pending: true` and answers 501 `not_implemented` until it is built.
Whoever builds one removes the mark, routes its handler and keeps this file
true.

All paths are under `/api/v1` and need a member of the organization. "View",
"edit" and so on in the tables are the page and space rights of #19.
A page or space the caller may not view is answered 404, never 403: existence
is privileged. Errors use the one envelope; the codes named here are the ones
a client branches on.

## #13 Drafts and publishing

| Operation | Needs | Answers |
|---|---|---|
| `GET /pages/{pageID}/draft` | view | `{draft}`, null when the caller has none |
| `PUT /pages/{pageID}/draft` | edit | `{draft}` |
| `DELETE /pages/{pageID}/draft` | view | 204, also when there was none |
| `POST /pages/{pageID}/publish` | edit | `{page, version}`; 409 `publish_conflict`, 409 `no_draft` |

Changed: `POST /pages` takes `publish`, `PATCH /pages/{pageID}` publishes,
`Page` gains `unpublished`, `draft`, `restricted`, `can`, and `TreeNode` gains
`unpublished` and `restricted`.

- **Numbering.** Published versions of a page are numbered 1, 2, 3 with no
  gaps and never reused. `page.version` is the number of the latest one, and
  is 0 while the page has never been published (`unpublished: true`). A draft
  is not a version and has no number. The migration makes every existing page
  version 1 with its current title and body.
- **Drafts.** At most one per page and person, seen by nobody else. It holds
  the whole title and body, validated like a page's, and `baseVersion`, the
  `page.version` editing began from. Each autosave replaces it; between one
  person's own tabs the last save wins. The autosave delay is a constant in
  `web/src/config.ts`.
- **New pages.** `POST /pages` makes an unpublished page with version 0 unless
  `publish: true`, which makes version 1 at once (scripts, tests, copies). An
  unpublished page, and every page below it, is seen by its creator alone:
  404 to anybody else, left out of the tree, the outline and search.
- **Publish.** Takes the caller's draft, or an unpublished page's own content
  when it has no draft, and in one transaction writes `page_version`
  number `page.version + 1` with the comment (trimmed, at most
  `page.MaxCommentLength`, 500, characters, may be empty) and
  `notifyWatchers`, copies title and body onto the page, and deletes the
  draft. A published page without a draft answers 409 `no_draft`.
- **Conflict.** When `draft.baseVersion` is not `page.version`, publish
  answers 409 `publish_conflict`. The client shows
  `compare?from=<page.version>&to=draft`, then either discards the draft or
  saves it again with `baseVersion` set to the current version, which takes
  the other publish as its base, and publishes again. There is no force flag.
  The refusal holds the caller's next reads to the state it was decided on,
  so the page read after it names the newer version and the comparison finds
  it, even on a replica that has not replayed that publish yet.
- **Discard.** Deletes the draft; the page stays as last published. For an
  unpublished page it stays with the content it was made with; trashing it is
  how it goes.
- **PATCH.** `PATCH /pages/{pageID}` stays for scripts and the current editor:
  it publishes title and body as the next version with no comment, refused
  with 409 `conflict` when `version` is stale, as today. It leaves drafts alone.
- **Watchers.** `notifyWatchers` is stored on the version. Nobody is told
  until watching exists; the publish dialog offers it ticked.
- Copies are published pages at version 1 with no history. A draft goes to
  the trash and is purged with its page, and is 404 while trashed.

## #14 History, compare, restore

| Operation | Needs | Answers |
|---|---|---|
| `GET /pages/{pageID}/versions?limit&offset` | view | `{versions, total, limit, offset}`, latest first |
| `GET /pages/{pageID}/versions/{versionNumber}` | view | `{version}` with body |
| `GET /pages/{pageID}/compare?from&to` | view | `{comparison}` |
| `POST /pages/{pageID}/versions/{versionNumber}/restore` | edit | `{page, version}`; 409 `conflict` |

- History lists published versions only: number, title, comment, who
  published and when, and `restoredFrom`. `limit` is 1 to 100, 20 by default,
  as everywhere in this contract that pages.
- **Diffable.** Any two versions of one page, in either order, and the
  caller's own draft as `draft` on either side. `to` defaults to the latest
  version; `from` defaults to the version before `to`, or to the draft's
  `baseVersion` when `to` is `draft`. `from=0` is the empty page, and is the
  default when `to` is 1. A side is `{number, draft, title, authorName,
  createdAt}`; the empty page is number 0 with `draft: false`.
- **Diff format.** `blocks` are the top-level blocks of both sides in reading
  order, each `{change, node}` with `change` one of `equal`, `inserted`,
  `deleted`, `modified`. A `modified` node is the newer side's block with
  changed text marked inline: inserted text carries the mark `diffInsert`,
  deleted text is put back where it was with the mark `diffDelete`. A block
  that cannot be compared inline, such as a table whose shape changed, is a
  `deleted` block followed by an `inserted` one. The two marks exist only in
  comparisons; the allowlist refuses them in a saved page, and the read-only
  renderer draws them. A title change shows as `from.title` and `to.title`.
- **Restore.** Publishes version N's title and body as version
  `page.version + 1` with `restoredFrom: N` and the comment given, or
  "Restored version N". `baseVersion` must be `page.version`, else 409
  `conflict`; restoring the latest version is refused the same way. Nobody's
  draft is touched: a draft based on an older version now conflicts on publish.
- Versions of a trashed page are 404 like the page; a purge deletes them.

## #17 Labels

| Operation | Needs | Answers |
|---|---|---|
| `GET /pages/{pageID}/labels` | view | `{labels}`, names in order |
| `POST /pages/{pageID}/labels` | edit | `{labels}`, the page's labels after the add |
| `DELETE /pages/{pageID}/labels/{labelName}` | edit | 204, also when the page did not carry it |
| `GET /labels?q&space&limit` | view, per page | `{labels}`: `{name, pages}`, the most used first |
| `GET /labels/{labelName}/pages?space&limit&offset` | view, per page | `{pages, total, limit, offset}`, by title |

- **Names.** A label is one word: trimmed, lower case, each run of spaces a
  hyphen, then only letters, digits, `-`, `_` and `.`, starting with a letter
  or a digit, at most `label.MaxNameLength`, 40, characters. Anything else is
  422 on `name` (or `labelName`) with a sentence. As in Armature, a word
  nobody uses yet is simply added; unlike Armature a label has no colour and
  no case of its own, since its name is its address.
- **A page** carries at most `label.MaxPerPage`, 50; one more is 422. Adding
  a label it carries is no change. `Page` gains `labels`, so the reader needs
  no second request. A trashed page's labels stay with it and come back with
  it, and are 404 while it is in the trash; a copy takes its original's
  labels.
- **No label rows.** A label exists while some page carries it; there is no
  list of the organization's labels to rename or delete. So nobody learns a
  word from a page they may not view: autocomplete counts and offers only
  labels on pages the caller may view, out of the trash.
- **Autocomplete** matches the start of the name, what was typed read as a
  name is (`release n` is `release-n`), `_` and `%` literally. `limit` 1 to
  50, 10 by default; `space` stays in one space.
- **A label's pages** are the pages out of the trash that carry it and that
  the caller may view (`perm.ViewablePage`), their own unpublished pages
  included, each with its `path`, all its `labels` and its last change;
  `space` narrows to one space, 404 when the caller may not view it.
- **Audit.** Labels are page content, and page edits are not in the audit
  log, so neither are label changes.

## #18 Search

| Operation | Needs | Answers |
|---|---|---|
| `GET /search` | view, per hit | `{hits, total, limit, offset}` |
| `GET /search/quick?q&space&limit` | view, per hit | `{pages}` |
| `GET /recent-pages?limit` | view, per page | `{pages}`, latest visit first |
| `POST /pages/{pageID}/visit` | view | 204 |

- **What is found.** The published title and body of pages (the body's plain
  text from `internal/document`), the names of the files attached to
  them (not their contents), and comments once comments exist. Never
  drafts, unpublished pages or anything in the trash, nor a file on such a
  page. A file's hit has the file name as `title`, an empty `snippet`, and
  its upload as the change and its uploader as the author.
- **Query syntax** (`q`, at most 200 characters, else 422 on `q`): as
  PostgreSQL's `websearch_to_tsquery`. Every word must match, `"quoted words"`
  match as a phrase, `or` matches either side, `-word` leaves out what has it.
  Matching ignores case and accents. An empty `q` lists by the filters alone,
  latest change first.
- **Ranking.** Title above body; ties go to the latest change. `sort=updated`
  orders by the latest change instead.
- **Filters,** all optional, each repeatable one matching any of its values:
  `space` (keys), `author` (user ids: who published a version, uploaded the
  file or wrote the comment), `label` (names, normalized as #17 does, so
  `Release Notes` finds `release-notes`; pages only, since a file carries no
  labels, and a name that cannot be a label matches nothing),
  `type` (`page`, `attachment`, `comment`; `comment` matches nothing until
  comments), `updatedAfter` (on or after the day) and `updatedBefore` (before
  the day), days as `YYYY-MM-DD` in UTC. A malformed date is 422; an unknown
  space key simply matches nothing.
- **Labels.** A page hit's `labels` are its page's, by name; a file's are
  empty.
- **Highlighting.** `title` and `snippet` are arrays of `{text, match}`:
  plain text runs, the matched ones marked. The client joins them and wraps
  `match: true` runs in `<mark>`; the server never sends markup. The snippet is
  the best passage around the matches, about 30 words, or the body's first
  words when only the title matched.
- **Quick search.** Pages only, for the top bar and the Ctrl/Cmd+K palette.
  Every word typed is a prefix of a word of the title; best title matches
  first, then the latest change. `limit` 1 to 20, 8 by default; an empty `q`
  answers no pages, and the palette then shows recent pages. Each page carries
  `path`, the titles above it, the home page first. The typing delay is a
  constant in `web/src/config.ts`.
- **Recent pages.** The reader posts `visit` each time it shows a page; the
  server keeps the last visit per person and page. `GET /recent-pages`, 1 to
  20, 10 by default, leaves out what the caller may no longer see.
- **Trimming.** Visibility is decided in the SQL that finds the hits, with the
  same rules as reading a page, so `total` counts only what the caller sees.

## #19 Permissions

| Operation | Needs | Answers |
|---|---|---|
| `GET /access/me` | member | `{can: {use, createSpace, administer}}` |
| `GET /org/permissions` | administer | `{permissions}` |
| `PUT /org/permissions/{permission}` | administer | `{permission}`; 409 for `administer` |
| `GET /spaces/{spaceKey}/permissions` | space administer | `{grants}` |
| `PUT /spaces/{spaceKey}/permissions` | space administer | `{grants}` |
| `GET /pages/{pageID}/restrictions` | view | `{restrictions}` |
| `PUT /pages/{pageID}/restrictions` | edit | `{restrictions}`; 409 `conflict` when it locks the caller out |
| `GET /people?q&limit`, `GET /groups?q&limit` | use | pickers, prefix match on name or email, 20 by default, at most 50 |

- **Subjects.** A grant names a `user`, a `group` or `everyone` (every member
  who may use the organization). In a request a subject is `{type, id}`; in an
  answer it also has `name`. What a person holds is the union of their own
  grants, their groups' and everyone's.
- **Global permissions.** `use` is reading anything in the organization;
  granted to everyone by default. A member without it gets 403 `no_access`
  from every route but `/auth/*` and `/access/me`. `createSpace` is making
  spaces; nobody beyond the administrators by default. `administer` is held by
  the organization's owners and administrators, `fixed: true`: it follows their
  roles, changed under Users, and PUT on it answers 409. `PUT` replaces the
  whole subject list.
- **Space permissions.** `view`, `addPages` (add, edit, move, copy, publish,
  restore versions, attach files), `addComments`, `delete` (move pages to the
  trash and back, delete others' comments and files), `administer` (settings,
  permissions, purge and empty the trash, delete the space). Every permission
  implies `view`, and `administer` implies all. Organization administrators
  hold every permission in every space, so no space can be orphaned. A new
  space grants everyone `view`, `addPages`, `addComments` and `delete`, and its
  creator `administer`; the migration gives every existing space the same,
  which is today's behaviour. `PUT` replaces the whole table; a row with no
  permissions or an unknown subject is 422.
- **Page restrictions.** A page has an optional view list and an optional edit
  list of users and groups (`everyone` is 422). Only people on the view list
  see the page and the pages below it; only people on the edit list edit it
  and the pages below it. Both inherit: a person must pass every list on the
  page and on every page above it. Restrictions only narrow what the space
  grants, and bind neither space nor organization administrators. Being on an
  edit list does not let anybody past a view list. The home page takes an
  edit list but not a view list (422): that is what space permissions are for.
  A save that would leave the caller unable to view or edit the page is 409
  unless they administer the space. Moving a page keeps its own lists and
  takes on those of its new parents; copying copies its own lists.
- **`restrictions`** is the page's own `view` and `edit` subjects and
  `inherited`, each restricted page above with its lists, the home page first.
  It is what the page's lock dialog shows as why and who.
- **`space.can`** keeps its fields and adds two: `editPages` is `addPages`,
  `addComments`, `deletePages` is `delete`, `administer`, `delete` (the space)
  and `purgeTrash` are `administer`.
- **`page.can`** is `edit` (`addPages` and every edit list passed), `delete`
  (`delete` and every edit list passed), `restrict` (same as `edit`) and
  `comment` (`addComments`). **`page.restricted`** says whether any view or edit
  list applies to the page, own or inherited, whether or not the caller passes
  it; **`TreeNode.restricted`** says the same for view lists. The web client
  offers only what `can` allows.
- **Pickers** match the start of a person's name, of any word of it, or of
  their email, and the start of a group's name or of any word of it,
  ignoring case. A `limit` outside 1 to 50 is 422. `everyone` is answered
  with the name `Everyone`; the client may show its own words for it.
- **Lists of pages.** Every query that lists pages narrows itself with
  `perm.ViewablePage(alias, n)`, the SQL condition `perm_page_viewable(
  alias.id, $n)`, which is the rule above: search and macros use it too.
- **Everywhere.** A page the caller may not view is 404 and missing from the
  tree, the outline, `hasChildren`, the trash list, search, recent pages and
  its attachments. The database enforces the same: policies on pages, drafts,
  versions, attachments and search rows refuse a raw SQL read or write the
  service would refuse, and the integration suite proves it.

## #20 Attachments

| Operation | Needs | Answers |
|---|---|---|
| `GET /pages/{pageID}/attachments` | view | `{attachments}`, latest first |
| `POST /pages/{pageID}/attachments` | edit | 201 `{attachment}`; 413 `too_large` |
| `GET /attachments/{attachmentID}?inline=1` | view its page | the bytes |
| `DELETE /attachments/{attachmentID}` | edit its page | 204 |

- **Upload** as in Armature: through the API as one multipart part named
  `file`, not presigned. The limit is `STATOR_UPLOAD_LIMIT` in bytes (a suffix
  such as `50MB` works, as in Armature), 50 MB by default
  (`attachment.DefaultMaxSize`), set in compose and in the chart's values. Over
  it the answer is 413 `too_large` with a sentence naming the limit; an empty
  file is 422 on `file`. The content type sent is kept when specific, else the
  bytes are sniffed. Images report `width` and `height`.
- **Storage.** S3-compatible, key `org/<org id>/page/<page id>/<attachment id>`.
  Attachments stay with a trashed page and are deleted, objects included, when
  it is purged or its space deleted. Copying a page copies its attachments and
  rewrites the copy's references.
- **Download** answers `Content-Disposition: attachment` with the file name,
  `nosniff` and `private` caching. With `inline=1` PNG, JPEG, GIF, WebP, PDF
  and plain text show in place; everything else, SVG included, downloads.
- **In the editor.** #20 adds two nodes to the document allowlist: a block
  `image` with `attachmentId` (uuid), `alt` (string, at most 500 characters)
  and `width` (pixels, or null for the natural size), and an inline
  `attachment` with `attachmentId` and `fileName`, drawn as a download chip.
  The client draws an image from `/api/v1/attachments/<id>?inline=1`. Drop,
  paste and the file picker upload first and insert the node on 201.
- **Delete** is final. Bodies and versions keep the reference, and the reader
  draws a missing file in its place. There are no attachment versions (#95).
