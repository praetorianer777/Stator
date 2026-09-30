# API contract for M2: comments, mentions, watching, notifications

The agreement between whoever builds the backend and whoever builds the web
client of #22, #23, #24, #25 and #26. As in `docs/api-contract-m1.md`, the
route table in `backend/internal/httpapi/openapi.go` is the source of truth
for paths, fields and types, and this file says what they mean. Every
operation below is in the table with `pending: true` and answers 501
`not_implemented` until it is built; whoever builds one removes the mark,
routes its handler and keeps this file true.

All paths are under `/api/v1` and need a member of the organization with
`use`. "View", "comment", "edit" and "delete" are the rights of #19:
`page.can.comment` is the space's `addComments` on a page the caller may
view, and "space delete" is the space's `delete`. A page, space or comment
the caller may not view is 404, never 403. A trashed page's comments, watches
and notifications are 404 or left out like the page. The codes named here
are the ones a client branches on; everything else is the one envelope with
a sentence.

## Migrations and packages

| Range | Owner | Holds |
|---|---|---|
| 00140 to 00149 | comments (#22, #23) | `comment`, its policies, comment search rows, the anchor trigger on `page_version` |
| 00150 to 00159 | watching and notifications (#25, #26), mentions (#24) | `watch`, `watch_optout`, `outbox_event`, `notification`, `notification_preference`, `notification_digest` |

Mentions need no table: the ids are read from the document on each publish
and comment and travel in the outbox event. If #24 needs a migration after
all, it takes the next free number in 00150 to 00159, after #25 and #26.

Types live in `internal/comment`, `internal/watch` and `internal/notify`.
The outbox is `internal/events`, as in Armature.

## #22 Page comments

| Operation | Needs | Answers |
|---|---|---|
| `GET /pages/{pageID}/comments?kind` | view | `{threads}`, oldest first |
| `POST /pages/{pageID}/comments` | comment | 201 `{thread}`; 409 `unpublished` |
| `GET /comments/{commentID}` | view its page | `{thread}`, the whole thread holding it |
| `POST /comments/{commentID}/replies` | comment | 201 `{comment, thread}` |
| `PATCH /comments/{commentID}` | its author, and comment | `{comment}`; 403 `forbidden` for anybody else's |
| `DELETE /comments/{commentID}` | its author, or space delete | 204; 403 `forbidden` |

Changed: `Page` gains `comments: {page, inline, detached}`.

- **Threads.** A thread is a first comment and its replies, one level deep,
  oldest first. The thread's id is its first comment's id. A reply to any
  comment of a thread goes to the end of that thread; there are no replies
  to replies. `kind` is `page` (below the page) or `inline` (#23); the list
  answers both when `kind` is absent, resolved threads included, and has no
  paging. Threads are ordered by their first comment.
- **Bodies** are documents like a page's, held to a subset of the allowlist,
  as Armature's comments are: `doc`, `paragraph`, `heading`, `bulletList`,
  `orderedList`, `listItem`, `blockquote`, `codeBlock`, `hardBreak`, `text`
  and `mention`, with the marks `bold`, `italic`, `strike`, `code` and `link`.
  No tables, panels, images, files, generated blocks, `hint`, `inlineComment`
  or diff marks. At most `comment.MaxBodyBytes`, 64 KiB. A body with no text
  and no mention is 422 on `body`. #22 adds the subset beside the page's
  allowlist in `internal/document`, derived from it by name, and writes it to
  `api/` for the web client's test, as `make document-allowlist` does now.
- **Unpublished pages** take no comments: 409 `unpublished` with a sentence
  saying to publish first. Nobody but the creator could read them anyway.
- **Editing** is the author's alone, while they may still comment on the
  page. It sets `editedAt`. Nobody rewrites another person's words, not even
  an administrator, as in Armature.
- **Deleting** is soft: the row stays, `deleted: true`, `body: null`, the
  author's name kept, and the words are gone for good in the same statement.
  The author may delete their own while they may view the page; anyone with
  space delete may delete anyone's, and that is written to the audit log
  (`comment.deleted`). A deleted comment stays in its thread as a placeholder
  while the thread has a comment that is not deleted; a thread whose every
  comment is deleted is left out of the list and of the counts. Deleting a
  comment also deletes the notifications about it.
- **Counts.** `comments.page` counts the comments below the page that are
  not deleted, replies included. `inline` and `detached` are #23's.
- **`can`.** A thread says `reply` and `resolve`; a comment says `edit` and
  `delete`, as decided above. The client offers only those.
- **Search.** Comments become search hits of type `comment`, as the M1
  contract promised: the plain text of comments that are not deleted, on
  pages the caller may view, with `commentId` set and the page's title as the
  hit's title. `author` matches the comment's author.
- **The database** holds `stator_app` to the same: reading a comment needs
  view of its page, writing one needs `addComments` and view, changing a body
  needs to be its author, and a soft delete needs the author or space delete.
  The integration suite tries each refused write straight through SQL.

## #23 Inline comments

| Operation | Needs | Answers |
|---|---|---|
| `POST /pages/{pageID}/inline-comments` | comment | 201 `{thread, page}`; 409 `anchor_conflict`, 409 `unpublished` |
| `POST /comments/{commentID}/resolve` | comment | `{thread}`; 409 `not_inline` |
| `POST /comments/{commentID}/reopen` | comment | `{thread}`; 409 `not_inline` |

- **The mark.** A passage is marked in the page body with the mark
  `inlineComment` (`comment.AnchorMark`), whose one attribute `threadId` is
  the thread's id (a uuid). #23 adds it to the allowlist. Marks of this type
  may overlap: one text may carry several with different ids. It is allowed
  in page bodies and drafts, never in comments or templates.
- **Anchors are not content.** Adding one makes no version and changes
  neither `version` nor `updatedAt`. The database strips the mark from every
  `page_version` body on insert, as it strips `hint`, so versions and
  comparisons never show it; the live anchors are in `page.body` alone.
- **Starting a thread.** The client takes `page.body` as it read it, wraps
  the selection in the mark with a `threadId` it generates, and sends that as
  `pageBody` with the comment's `body`. In one transaction, on the locked
  page, the server takes that one mark out of `pageBody` and requires the
  rest to equal `page.body` exactly. If it does not, somebody published or
  anchored meanwhile: 409 `anchor_conflict`, and the client reads the page
  again and retries with the same selection if its text is still there. The
  mark must cover some text inside one text block (a paragraph, a heading, a
  code block, or one of those in a list item, a quote, a panel or a cell),
  else 422 on `pageBody`. The server then stores `pageBody` as the page body
  and keeps the marked text as the thread's `anchor.quote`, cut at
  `comment.MaxQuoteLength`, 500 characters. The answer carries the page, so
  the reader draws the highlight at once. A `threadId` already in use is 409
  `conflict`.
- **Commenters need no edit right.** Marking the body this way is not
  editing it; the server checks the only change is the mark.
- **On every publish** (publish, restore, `PATCH`, and the first publish of
  a page) the new body's anchors are settled in the same transaction:
  1. A mark in the new body whose thread is on this page and not wholly
     deleted keeps the thread anchored there; the editor carries marks along
     with the text it edits, and a draft holds the marks the editor loaded.
  2. A thread whose mark is missing is looked for by its quote. If the quote
     occurs exactly once in the new body, within one text block, the mark is
     put back there. This is what keeps threads started after a draft began,
     and threads through a restore of an older version, which has no marks.
  3. Otherwise the thread becomes `detached`: its passage was deleted or
     changed beyond finding. A detached thread keeps its quote and its
     comments, is listed with `anchor.state: detached`, and is never anchored
     again.
  4. A mark naming no thread of this page, or a wholly deleted one, is
     dropped. A client cannot invent anchors through a draft.
  Resolved threads take part like open ones, so reopening finds the passage.
- **Drafts** are not settled while they are drafts: the draft keeps whatever
  marks the editor saved, and the settlement happens when it is published.
  Comparisons ignore the mark on both sides.
- **Resolving** marks the thread resolved with who and when, and reopening
  clears it; both are for anybody who may comment, and doing either twice is
  no change. Only inline threads resolve: a thread below the page is 409
  `not_inline`. A reply to a resolved thread reopens it. `commentID` may be
  any comment of the thread.
- **The reader** highlights the passages of open anchored threads, lists
  resolved threads only when asked (hidden by default), and shows detached
  threads apart, under the page's comments, open ones first. It draws no
  highlight for a mark whose thread is not in the list.
- **Counts.** `comments.inline` counts open anchored inline threads,
  `comments.detached` open detached ones.

## #24 Mentions

| Operation | Needs | Answers |
|---|---|---|
| `GET /pages/{pageID}/mentionable?q&limit` | view | `{people}`: `{id, name, email, canView}` |

- **The people source** for the editor's `@` is this route, not `/people`:
  it matches like the pickers of #19 (start of the name, of any word of it,
  or of the email, ignoring case; `limit` 1 to 50, 20 by default, else 422)
  and says for each member with `use` whether they may view the page once it
  is published: the page's view rule without the unpublished rule, so a new
  page's author can mention the people who will read it. The page's
  mentionable people are no secret from someone who may view it: its
  restrictions are readable too.
- **Anybody in the organization may be mentioned.** The picker shows those
  with `canView: false` marked, and the author may still insert them, for
  example before granting them access. Only people who may view the page
  when the mention takes effect are told; the others get nothing, and their
  chip reveals nothing they could not see. Nobody is told later on gaining
  access.
- **The node.** `mention` keeps its attributes; #24 tightens `id` to a
  member's uuid (`document.UUIDPattern`). `label` is the name when the
  mention was made, drawn as the chip; a mention naming nobody in the
  organization stays in the text and tells nobody.
- **In a page** a mention takes effect when the page is published, by any
  path. The people mentioned in the new version and not in the one before
  are told, once per publish, whether or not `notifyWatchers` is set: a
  mention is addressed to one person, not to the watchers. A draft tells
  nobody. A copy mentions nobody anew.
- **In a comment** the people mentioned are told when it is posted; on an
  edit, only people the edit added. Nobody is told twice about one comment.
- **The actor** is never told about their own mention, and a mention does
  not make anybody watch the page.
- The extraction of mention ids from a document is a function of
  `internal/document`, unit tested, used by the page and comment services.

## #25 Watching

| Operation | Needs | Answers |
|---|---|---|
| `PUT /pages/{pageID}/watch` | view | `{watching}` |
| `DELETE /pages/{pageID}/watch` | view | 204, also when there was none |
| `GET /pages/{pageID}/watchers?limit&offset` | view | `{watchers, total, limit, offset}`, by name |
| `PUT /spaces/{spaceKey}/watch` | space view | 204 |
| `DELETE /spaces/{spaceKey}/watch` | space view | 204, also when there was none |
| `GET /watches?limit&offset` | member | `{watches, total, limit, offset}`, the latest first |

Changed: `Page` gains `watching: {page, subtree, inherited}`, `Space` gains
`watching`, and `RestoreInput` gains `notifyWatchers`, stored on the version
as publish stores it.

- **Kinds.** A person watches a page alone (`page`), a page and every page
  below it, now and later (`subtree`, body `{subtree: true}`), or a whole
  space (`space`). A person has at most one own watch per page; `PUT`
  replaces it. What is below a page and which space it is in are read when an
  event is delivered, so a moved page is covered by its new place's watches
  and not by its old place's.
- **`watching`** on a page is the caller's own watch on it (`page` or
  `subtree`) and `inherited`, the nearest subtree watch above or the space
  watch, with the page it is on (null for the space). The page menu offers
  watch, watch with the pages below, and stop watching, and says what
  covers the page when an inherited watch does. It is filled in by #25; until
  then every field is false or null.
- **Unwatching** a page removes the caller's own watch on it and records an
  opt out: from then on their own edits do not make them watch it again, and
  only watching it explicitly clears the opt out. It does not silence a watch
  above; that is unwatched where it is. Unwatching a space leaves the
  watches on its pages.
- **Auto watch.** Creating a page (`POST /pages`, a copy's top page
  included) and publishing a version of it (publish, restore, `PATCH`) makes
  the actor watch it as `page`, unless they already have an own watch on it,
  have opted out of it, or have turned `autoWatch` off in their
  preferences. Commenting does not make anybody watch the page: a commenter
  hears about their thread through `replied` and `resolved` (#26), and a
  mention is a one-off. Armature makes mentioned people watchers; a page's
  every future edit is more than a mention asks for.
- **Watchers** lists every person who would be told about a new version of
  the page: own watches on it, subtree watches above, and the space's
  watchers, each once, through the nearest (`via`, and `viaPage` for a
  subtree), and only people who may view the page. Watching is only ever
  for oneself: nobody adds or removes another person.
- **`GET /watches`** is the caller's own watches for a settings page, left
  out while their page or space is one they may no longer view or is in the
  trash. A watch on something the watcher may no longer view is kept but
  tells them nothing.
- Watches go with their page or space when it is purged or deleted, and stay
  with a trashed page, coming back with it.
- **The database** lets `stator_app` read and write only the actor's own
  watches and opt outs, and only on pages and spaces they may view.

## #26 Notifications

| Operation | Needs | Answers |
|---|---|---|
| `GET /notifications?unread&limit&offset` | member | `{notifications, total, limit, offset}`, the latest first |
| `GET /notifications/unread-count` | member | `{unread}` |
| `POST /notifications/read` | member | 204 |
| `GET /notification-preferences` | member | `{preferences}` |
| `PUT /notification-preferences` | member | `{preferences}` |

- **Events.** Every change that may tell somebody writes an event to
  `outbox_event` in its own transaction, with `events.Emit` as in Armature:
  `page.published` (page, version, actor, `notifyWatchers`, whether it is the
  first publish, the newly mentioned ids), `comment.created` (comment,
  thread, page, actor, whether it is a reply, the mentioned ids),
  `comment.edited` (comment, the newly mentioned ids), `thread.resolved` and
  `thread.reopened` (thread, actor). The worker claims unprocessed events in
  batches with `FOR UPDATE SKIP LOCKED`, fans each out, and marks it done;
  delivery is at least once, and a unique `(event, person)` on
  `notification` makes a second run a no-op. Armature relays its outbox to a
  Valkey stream for several consumers; Stator has one consumer and treats
  Valkey as optional, so the worker reads the table directly. The relay can
  come when Armature link sync needs the same events.
- **Kinds and who hears them** (`notify.Kinds`):

  | Kind | Who | When |
  |---|---|---|
  | `mentioned` | each person newly mentioned | publish, comment, comment edit |
  | `replied` | everybody who wrote in the thread | a reply |
  | `commented` | the page's watchers | a new thread or a reply |
  | `resolved` | everybody who wrote in the thread | resolved or reopened |
  | `published` | the page's watchers | version 2 on, with `notifyWatchers` |
  | `created` | subtree watchers above, page watchers of the page right above, and the space's | version 1, with `notifyWatchers` |

  A person gets at most one notification per event, the first kind of the
  table that applies to them, so a watcher mentioned in a comment is told
  once, as mentioned. The actor is never told. `POST /pages` with
  `publish: true`, `PATCH` and copies tell no watchers: they have no flag,
  and scripts should not flood anybody. Restore tells them when its
  `notifyWatchers` is set. The publish dialog offers the flag ticked, as in
  M1; the history's restore sends it set and says so when it asks.
- **Never about a page one may not view.** The fan-out writes a row only
  for a person who may view the page at that moment (`perm_page_viewable`
  with that person), the list and the count read only rows whose page the
  caller may still view and that is out of the trash, and a digest checks
  again before it sends. A mail already sent cannot be taken back.
- **A notification** carries its kind, the actor, the page (`id`, `title`,
  `spaceKey`, the title as it is now), the thread and comment for comments,
  the version for publishes, and `excerpt`: plain text of the comment, of the
  block holding the mention in a page, or the version's comment, cut at
  `notify.MaxExcerptLength`, 200 characters. The client words it from these
  (`web/src/i18n`); the server sends no sentence. It links to the page, with
  the thread open when there is one.
- **List and unread.** `limit` 1 to 100, 20 by default; `unread=true` lists
  only what is unread, and `total` counts what the filter lists.
  `POST /notifications/read` takes `{ids}` or `{all: true}`; ids that are not
  the caller's are ignored, and neither is 422 on `ids`. Following a
  notification marks it read. Nothing else does: reading the page does not.
- **Polling, not pushing.** The client asks for the unread count every
  `UNREAD_POLL_MS`, 30 seconds, in `web/src/config.ts`, and when the window
  regains focus, and reads the list when the notification centre opens, as
  Armature does. A push channel would have to pass nginx and reach every api
  process through Valkey; a badge a few seconds late does not justify that
  now. After marking read, read-your-writes keeps the badge from coming back.
- **Preferences** are `inApp` and `email`, a switch per kind, `digest` and
  `autoWatch`. Without a saved row everything is on, `digest` is `off` and
  `autoWatch` is true. `PUT` replaces the whole object; an unknown digest is
  422 on `digest`. A kind switched off in the app writes no row and so sends
  no mail either: mail is a copy of the row, as in Armature.
- **Mail** goes from the worker over SMTP to `STATOR_SMTP_ADDR`
  (`mailpit:1025` in compose, so every mail lands in Mailpit, as in
  Armature), from `STATOR_MAIL_FROM`, both new settings in compose and the
  chart. Without `STATOR_SMTP_ADDR` the worker writes rows only and says so
  once at start. With `digest: off` each row is mailed when the event is
  fanned out; `hourly` bundles once an hour, `daily` once a day at
  `notify.DailyDigestHour`, 08:00 UTC. A bundle is one mail listing what
  came since the last, leaving out what was read in the app meanwhile, and
  nothing is sent when nothing is left. Each mail is plain text in English
  with the page's link (`STATOR_APP_URL/s/{spaceKey}/p/{pageId}`, the client
  puts the slug right) and a link to the preferences.
- **The database** lets `stator_app` read, and mark read, only the actor's
  own notifications, and read and write only their own preferences. The
  fan-out plans as the worker, per organization, and writes each row acting
  for its recipient, so the database itself refuses a row about a page they
  may not view; the integration suite proves through SQL that no person
  reads another's rows or a row about a page they may not view.
- Notifications go with their page when it is purged and with their comment
  when it is deleted, and are hidden while the page is in the trash.
