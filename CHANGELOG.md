# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the versioning [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- Backend skeleton: configuration from `STATOR_*` variables, a Postgres
  cluster with tenant-scoped transactions and row level security, the
  foundation migration, and an HTTP API with `/healthz`, `/readyz`, `/metrics`
  and an OpenAPI document generated from its route table.
- Custom themes in the `armature-theme/1` format, so Armature theme files
  import unchanged: colours, fonts, shape, shadows, cursors, icons, backdrop,
  a moving effect and extra CSS, with Armature's limits. Themes are a
  person's, shared with the organization when they say so; an organization
  may name a shared theme as its default. Deep-Tech and Constellation ship
  as examples, and a theme editor previews a draft live on the page.
- File storage in any S3 compatible bucket, configured with `STATOR_S3_*`.
- Compose stack for development and tests: Postgres 18 with a streaming
  replica, Valkey, SeaweedFS, Mailpit, Keycloak with the `stator-dev` realm,
  and the api, worker and web images. `make up` starts it on ports derived
  from the checkout's path; the integration suite runs against it.
- Read-your-writes with a replica serving reads: a write's position is kept
  in Valkey (`STATOR_VALKEY_URL`) for `STATOR_READ_YOUR_WRITES_TTL`, and the
  writer's reads stay on the primary until the replica they would get has
  replayed it. Replica lag is checked on each read's own connection and
  sampled across `STATOR_DB_REPLICA_LAG_SAMPLES` connections by the health
  loop, so reads stay correct behind a load balanced read service.
- Helm chart `deploy/charts/stator`: the api, worker and web, the roles,
  migrate, seed and password Jobs, an Ingress and a ServiceMonitor. With
  `cnpg.enabled` it renders a CloudNativePG cluster whose `-rw` service takes
  the writes and whose `-ro` service takes the reads once there is more than
  one instance. `tests/test-helm.sh` lints and renders it in the gate.
- Separate pool sizes for writes and reads (`STATOR_DB_PRIMARY_MAX_CONNS`,
  `STATOR_DB_REPLICA_MAX_CONNS`; `STATOR_DB_MAX_CONNS` still sizes both), and
  per pool metrics: connections taken, time spent waiting, and fallbacks to
  the primary by reason.
- Single sign-on through each organization's OIDC provider, with state,
  nonce and PKCE; people are kept by issuer and subject, and members' groups
  follow the provider's groups claim. Somebody the provider knows who is not
  a member gets no session: their request waits until an administrator lets
  them in or turns them away, with a count in the account menu, and both
  answers are kept in a new `audit_log`. `STATOR_BOOTSTRAP_MEMBERS` lets
  people in ahead of their first sign-in.
  Sessions live in an HttpOnly cookie, only the SHA-256 of the token is
  stored, and they expire and end on sign-out. A sign-in page, an account
  menu with sign-out, and single sign-on settings for administrators.
- A local administrator for a fresh deployment from
  `STATOR_BOOTSTRAP_ADMIN_EMAIL` and `STATOR_BOOTSTRAP_ADMIN_PASSWORD`,
  signing in with an argon2id password. `STATOR_BOOTSTRAP_OIDC_*` points
  the demo organization at a provider; the compose stack's seed uses both,
  so `make up` signs in through its Keycloak.
- `STATOR_SECRET_KEY` seals stored secrets with AES-256-GCM, and is required
  outside development; `STATOR_SESSION_TTL`, `STATOR_OIDC_REDIRECT_URL` and
  `STATOR_OIDC_BACKCHANNEL` tune sign-in.
- Roles from provider groups: administrators map groups of the identity
  provider to member or admin under single sign-on, and each sign-in gives
  the highest mapped role. Somebody in a mapped group joins on their first
  sign-in without waiting, roles set by hand outside mapped groups stay, the
  owner is never moved, and every change goes to `audit_log`. The members
  list marks the roles that come from the provider, and an administrator can
  remove a member there.
- Personal access tokens: `stator_pat_` and 32 random bytes, sent as a
  bearer token, of which only the SHA-256 is stored. A token acts as its
  owner in one organization, may expire, and may carry the `read` scope,
  which refuses every write. A Tokens page, reached from the account menu,
  makes one and shows its secret once, and lists and revokes them with
  their last use; administrators list and revoke every token in the
  organization over the API. Making and revoking are kept in `audit_log`.
- The integration suite checks every answer against `api/openapi.json`
  and fails when an operation was never answered successfully or never
  refused.
- Test endpoints for the browser suite, on only with `STATOR_TEST_ENDPOINTS`
  and a `STATOR_TEST_ENDPOINTS_TOKEN`: `POST /api/v1/test/orgs` makes a
  throwaway organization with the bootstrap members and provider, and
  `DELETE /api/v1/test/orgs/{slug}` removes it with all its rows and files.
  The api refuses them in production and the Helm chart never sets them.
  The theme specs each run in an organization of their own, in parallel.
- Drafts and published versions in the API. Each person autosaves a private
  draft of a page (`/pages/{id}/draft`) and publishes it with an optional
  comment as the next numbered version; a draft begun before somebody
  else's publish is refused with `publish_conflict` until it is saved again
  over theirs. A new page stays its creator's alone until published, unless
  it is made with `publish: true`. Every page's history lists its versions,
  any version reads as it was, two versions or a version and the caller's
  draft compare block by block with inserted and deleted words marked, and
  a restore publishes an old version again as the newest. The database
  keeps version numbers without gaps and the history append only.
- Permissions in the web client. Administrators set who may use Stator,
  create spaces and administer the organization under Permissions in the
  account menu; a space's settings hold a grid of people and groups against
  view, add pages, add comments, delete and administer; and a page's
  Restrictions dialog narrows who may view and edit it, showing what it
  inherits from the pages above and from where. Changes wait for Save, and
  people and groups are picked by the start of a name or an email. A
  restricted page is marked on the page and in the tree, every action
  follows what the reader may do there, a save that would shut the saver
  out says what to do instead, and a member the organization does not let
  in gets a page saying so.
- Permissions in the API. Global: `use`, granted to everyone by default,
  without which a member is answered 403 `no_access`; `createSpace`; and
  `administer`, which follows the owner and admin roles. Per space, for
  people, groups and everyone: view, add pages, add comments, delete and
  administer; a new space, and every existing one, grants everyone all but
  administer and its creator administer. Per page, view and edit
  restrictions of people and groups that the pages below inherit, which
  narrow the space's permissions and never bind space or organization
  administrators; a save that would lock its saver out is refused.
  `space.can` and `page.can` follow the rules, `GET /access/me` says what the
  caller may do, and `/people` and `/groups` serve the pickers. Row level
  security holds raw SQL as the app role to the same rules for the person a
  transaction names, and every change is written to the audit log.
- Drafts and history in the web client. The editor saves to a private
  draft a moment after typing stops and publishes it from a dialog with an
  optional comment and whether to notify watchers; when somebody published
  in between, it says so and offers to compare, keep the draft over their
  version, or discard it. Unpublished pages and waiting drafts are marked
  on the page and in the tree. A page's history lists every version with
  who published it, when and why, shows any one read-only, compares two
  versions or a version with the draft, with added text underlined and
  removed text struck through and announced to screen readers, and
  restores a version as a new one after asking.
- Search in the API. `GET /search` finds the published titles and bodies of
  pages and the names of their files, with quoted phrases, `or` and
  `-word`, ignoring case and accents, ranks title matches above body
  matches, and filters by space, author, label, type and a range of days.
  Titles and snippets come as plain text runs with the matches flagged,
  never markup. `GET /search/quick` matches the start of title words for the
  top bar and the command palette, and `GET /recent-pages` lists the pages a
  person opened last, as noted by `POST /pages/{id}/visit`. Drafts,
  unpublished pages and the trash are never found, and totals count only
  what the caller may read.
- Search in the web client. The top bar's box and Ctrl or Cmd+K open quick
  search, which offers page titles as they are typed and recent pages while
  nothing is, works from the keyboard as a combobox, and opens the full
  search for the words on Enter. The search page shows hits with their
  matches marked, filters by space, type, label, author and date, orders by
  best match or latest change, and pages through the results; the query and
  every filter are in the address. Opening a page notes the visit for the
  reader's recent pages.
- Page templates. `GET /templates` and `GET /templates/{templateKey}` serve
  the built-ins: meeting notes, how-to guide, troubleshooting article,
  retrospective, decision record, product requirements and project plan,
  each a document the allowlist validates, with a title such as
  `Meeting notes {date}` where one helps. The new page dialog offers a blank
  page and every template as a radio group with a read-only preview of the
  chosen one; the page is made unpublished with that body and opens in the
  editor.
- Hints: a template's placeholder text, the new `hint` mark, drawn muted and
  slanted. The first keystroke, paste, Backspace or Delete in a hint takes all
  of it away, and the database strips what is left of any hint from every
  published version and published page.
- Labels. A page carries labels, one lower case word each, put on and taken
  off by whoever may edit it; `Release Notes` becomes `release-notes`. The
  page shows them as links, and for an editor a combobox that offers the
  labels in use as they are typed. `/labels/{name}` lists the pages with a
  label across the organization, `/s/{key}/labels/{name}` in one space, a
  page of the list at a time. Search filters by label, and each hit shows its
  page's labels. A copy takes its original's labels. A label is seen only
  with its page: autocomplete, lists and search leave out the labels of
  pages the reader may not view, and the database refuses them to raw SQL.
- Table of contents and child pages blocks, both from the slash menu. A table
  of contents links to the page's headings down to the level chosen in the
  block, and follows the headings as they are typed. A child pages block
  lists the direct children or the whole subtree, down to a number of levels
  or all of them, in tree order, by title or the latest change first, with a
  link to each. The list comes from `GET /pages/{id}/below`, which holds only
  pages the reader may view, out of the trash, published or their own. A
  comparison of versions describes each block's settings in words.
- Watching. A person watches a page alone, a page and every page below it,
  or a whole space, from the page's header, which also says what covers the
  page from above and lists who watches it. Creating a page and publishing a
  version of it make the author watch it, unless they stopped watching it or
  turned that off. Coverage is read when a change is delivered, so a moved
  page is covered by its new place. `/settings/watching` lists one's watches.
- Notifications. A publish or a restore that tells the watchers reaches
  each of them once, never the author: in the app, through a bell in the top
  bar whose badge polls the unread count and asks again when the window
  regains focus, and by mail, at once or in an hourly or daily digest.
  `/settings/notifications` switches each kind in the app and by mail. The
  worker drains a transactional outbox with `FOR UPDATE SKIP LOCKED`, so
  several workers may run and restart, and writes each row acting for its
  recipient, so nobody is told about a page they may not view; the list,
  the count and a digest check again when they are read or sent.
- Mail settings `STATOR_SMTP_ADDR` and `STATOR_MAIL_FROM`, and `mail.smtpAddr`
  and `mail.from` in the chart. Compose sends every mail to Mailpit. Without
  a relay only the rows in the app are written.
- Page comments. Threads below a page, one level deep, in rich text held to a
  subset of the page allowlist (text, headings, lists, quotes, code, links
  and mentions, at most 64 KB), written in an editor of their own that
  arrives with the first comment somebody writes. Only the author edits a
  comment. The author deletes it, and so does an administrator of the space
  or the organization, but not somebody who merely holds the space's delete,
  which is for pages. A delete leaves a placeholder while the thread has
  replies, is recorded in the audit log when it is somebody else's, and takes
  back the notifications about it.
  The page's header counts its comments. A new thread tells the page's
  watchers, a reply everybody who wrote in the thread, and a notification
  leads to its thread with `?thread=`. Comments are search hits of type
  `comment`. The database holds raw SQL to the same rules: comments are read
  with their page and written only in one's own name, where one may comment.
- Inline comments. Select text in a page and comment on it from a floating
  button or Ctrl+Alt+M, without the right to edit the page; the passage is
  highlighted and its thread opens in a panel beside the page, or below the
  text on narrow screens. Threads are resolved and reopened by anybody who
  may comment, which tells everybody who wrote in them, and a reply reopens
  one; resolved threads are hidden behind a toggle. Every publish keeps each
  thread on its passage, finds it again by its words when the passage was
  not carried along, and otherwise detaches the thread for good; detached
  threads are listed with their quote below the page. Marking a passage makes
  no version, and neither versions nor comparisons show the mark. The
  database lets a commenter change a page's body by the mark of their own new
  thread alone.
- Mentions. An at sign in a page or a comment looks up the organization's
  people as you type and names the one picked; the list marks somebody who
  cannot see the page, who may still be named and is not told. Publishing a
  page, by any path but a copy, tells the people its new version names for
  the first time, whether or not the watchers are told; a comment tells the
  people it names, and an edit only those it adds, nobody twice. Nobody is
  told about their own mention, a mention makes nobody watch the page, and
  the notification quotes the block or the comment that names them. A
  mention holds a member's id, and the database refuses an event that
  mentions anybody the organization does not hold.
- Connecting Armature. An administrator enters the address people open
  Armature at, the Armature organization and the webhook secret under
  Settings, Armature, which also shows the webhook address and topics to
  enter in Armature. Each member pastes an Armature personal access token
  under their profile, now open from the account menu, and sees whom it acts
  as; Armature is asked before it is kept, and a refusal says what to do.
  Tokens and the secret are sealed with `STATOR_SECRET_KEY` and never shown
  again. A new address or organization forgets every stored token, which the
  database enforces too, and each member reads and changes only their own.
- Every outbound call passes an SSRF guard adapted from Armature's, which
  refuses addresses inside the network unless `STATOR_OUTBOUND_ALLOW` names
  them; `STATOR_ARMATURE_BACKCHANNEL` reaches an Armature at another address
  than the one people open. The chart takes both as `network.outboundAllow`
  and `armature.backchannel`, empty by default.
- `armature-stub`, a stand-in for the part of Armature's API Stator calls,
  in the compose stack on its own port. A unit test holds it to Armature's
  `api/openapi.json`, vendored under `api/armature` by
  `make armature-openapi REF=...`.
- Smart links to Armature issues. Typing an issue key followed by a space or
  a sign turns it into a chip when its project is one the author sees in
  Armature, so `UTF-8` stays text; pasting an issue's address of the
  connected Armature always does, and undo turns a chip back into its text.
  A page stores only the key. Each reader sees the type, summary and status
  of the issues they may see, with a card on hover or focus, the key and a
  link to connect their account without a token, and "Not available" for an
  issue that is not theirs to see. A view asks for all its keys in one
  lookup, which Stator answers from a per-person cache and one search in
  Armature, following moved issues by their old keys. Chips show in the
  editor, the page, a comparison of versions and search, which finds a page
  by the keys it names. `GET /armature/issues`, `GET /armature/issues/{key}`
  and `GET /armature/projects` answer a `status` instead of failing when
  Armature cannot be asked.
- An Armature issue block. "Armature issue" in the slash menu asks for a key
  or an issue's address, shows the issue it found, and inserts a card with
  the summary, type, status, priority, assignee, reporter, due date, when it
  changed and a link to Armature, as each reader may see it. The page stores
  only the key. Without a token or access the block shows the key and the
  hint, as a chip does; a comparison of versions names the issue in words.
- An Armature issue list block. "Armature issue list" in the slash menu asks
  for an NQL query, which Armature checks as it is typed, the columns and the
  most rows, and inserts a table of the issues the query finds, as each
  reader may see them. Rows sort by a click on a column head, arrive 20 at a
  time with "Show more", and the table says how many there are and opens the
  query in Armature. A query Armature cannot read says why and at which
  character, with the place marked for those who may edit the page. The page
  stores only the settings. `GET /armature/search` answers a page of rows per
  viewer, cached for a minute, and 422 `bad_query` with its position.
- Armature issues from selected text. Selecting text in the page editor
  offers "Create Armature issue" in the toolbar: one issue for text inside a
  paragraph, heading or cell, one per list or task item, and one per table
  row, named by its first cell with text. The dialog asks for the project,
  offering only those the author may file in, and the issue type, shows every
  summary to change or leave out, and then what was created and which item
  Armature refused and why. Selected text becomes the new issue's chip; list
  items and rows keep their text and the chip follows it, and one undo takes
  the whole edit back. Each issue is filed as the author, with a description
  linking back to the page. `POST /armature/issues` files up to 50 issues in
  order and stops at the first refusal; `GET /armature/issue-types` lists the
  types a new issue may take.
- Pages in Armature. Every issue a published page names as a chip or an
  issue block lists the page in Armature, by a remote link the worker puts on
  it as the person whose change it was, with their own token. Publishing,
  renaming, moving, restricting, trashing, restoring and purging keep the
  links in line: a key taken out loses its link, a trashed or purged page
  loses them all, and a restored page gets them back. A page not every
  member may view is titled "A restricted page in Stator" on its issues. An
  Armature that does not answer is asked again through the outbox; a refusal
  marks the key failed with a sentence saying why, and the next change to the
  page tries it again. The page shows "Linked in Armature" with each key's
  state, and `GET /pages/{id}/armature-links` answers it. The compose worker
  now gets `STATOR_SECRET_KEY`, which it needs to open the members' tokens.
- Armature webhooks. `POST /armature/webhook/{orgSlug}` takes Armature's
  deliveries, checks `X-Armature-Signature-256` against the organization's
  sealed secret in constant time, and clears the cached issues an
  `issue.created`, `issue.updated`, `issue.transitioned` or `comment.added`
  names, the old key of a moved issue and every cached search, so a chip, a
  block or a list shows a change at the next view instead of within a minute.
  An event id is acted on once in 24 hours, other topics are acknowledged and
  ignored, a body over 1 MiB is 413, and an unknown organization, one without
  a secret and a wrong signature all answer the same 401 `bad_signature`.
- Following the Armature theme. "Follow my Armature theme" on the themes
  page, offered to whoever stored an Armature token, shows the theme they use
  in Armature in place of a Stator one. Stator downloads it with their token,
  checks it as an imported theme, and keeps it as their own hidden copy,
  downloaded again only when Armature answers another theme or a later
  change; opening the theme settings asks Armature at once, and a page load
  within five minutes. Choosing any theme ends following. When Armature does
  not answer within 2 seconds, or the token is refused, the page shows what
  it would without following, and the settings say why in a sentence; a
  theme that fails the checks is refused with the reason.
  `GET`, `PUT` and `DELETE /armature/theme`, and `GET /themes/active`
  answers `source: "armature"` while following.
- Expand blocks. "Expand" in the slash menu wraps the blocks under the caret
  in a section with a title, typed in the block's own title box, from which
  Enter carries on into the blocks inside; "Remove the expand, keep its
  content" takes it away again. Readers see the title on a button that
  opens and closes the section by mouse or keyboard and says whether it is
  open; it starts closed on every visit, and opens by itself on the way to a
  heading inside it. Printing, and so a PDF, shows every section open, as
  does a comparison of versions. The page stores only the title and the
  blocks, and search finds a page by both.
- Reactions (#66). Below a published page and below each comment, members
  who may comment put an emoji on from a picker and take theirs back with
  one click or key; each emoji shows its count, whether it is yours, and,
  on hover or focus, who reacted, you first, the first ten by name and the
  rest counted. Readers without the right to comment see the same and add
  nothing. A deleted comment loses its reactions. `POST` and `DELETE` on
  `/pages/{pageID}/reactions` and `/comments/{commentID}/reactions`, and
  `Page` and `Comment` gain `reactions`. The database holds every reaction
  to its page's view rule and to its author's name (migration 00180).
- Find and replace in the page editor. Ctrl or Cmd+F inside the editor, or
  the toolbar's search button, opens a bar that highlights every match, says
  how many there are, steps through them with Enter and Shift+Enter, and
  matches case when asked. Replace changes the current match and moves on;
  Replace all changes every match in one step that one undo takes back.
  Words are found across bold, italic and other marks, never inside a chip
  or a mention, and the replacement keeps the marks of the text it replaces.
  Escape closes the bar and puts the caret on the last match; outside the
  editor Ctrl or Cmd+F stays the browser's.

### Changed

- Renaming and deleting a space and purging its trash are for the space's
  administrators, and making spaces for whoever holds `createSpace`, rather
  than for the organization's administrators alone.

- `PATCH /pages/{id}` publishes the title and body as the next version with
  no comment. Existing pages become version 1 of themselves.

- Stored files are keyed under `org/<organization id>/`, so an
  organization's files can be listed and removed together.

### Fixed

- Following the Armature theme no longer drops back to the built-in theme
  when the page is reloaded while Stator copies a changed theme: the copy is
  finished even after the request is gone, and only a finished copy is shown.

- Saving an edited comment puts focus back on its Edit button every time,
  not only when the save's answer and the next frame came in the right order.

- Subtle text (hints, timestamps, placeholders) meets WCAG AA: 4.5:1 or more
  on every surface in both palettes, and in the Deep-Tech and Constellation
  themes, with the hue kept. A unit test checks every text colour against
  every surface.
- The slash menu and the mention list keep the active option in view while
  focus stays in the editor, and the stored document on the development
  editor page is a named region the keyboard can reach and scroll. The
  browser suite's accessibility checks now pass with no known findings.
- Highlighted code meets WCAG AA: every colour a code block uses reaches
  4.5:1 or more on its background in both palettes and in the Deep-Tech and
  Constellation themes. Of the chart, success and danger colours the
  highlighting borrows, those that fell short are darker in the light
  palettes, with the hue kept.
  A unit test and a browser check in several languages hold it there.
- Danger buttons meet WCAG AA in the dark palette and in the Deep-Tech and
  Constellation themes: a new `on-danger` colour writes their label, white
  on the light palettes' red and dark on the dark palettes' lighter red.
  Links and the current item in the Deep-Tech and Constellation light
  themes are a darker accent, and Constellation's dark column carries its
  own danger and accent labels; the hue is kept throughout. The unit test
  now also checks accent and danger text on every surface and on their
  tints, and every label on its fill. "Disconnect Armature" is a danger
  button again.
- The editor's table, panel and heading tools no longer submit the page's
  form and open Publish: buttons are plain buttons unless they say they
  submit, and a lint rule refuses a `<button>` without a type.
- Spaces: a key that is part of every address (`/s/{key}`), a name, a
  description and a home page, the root of the space's pages. A directory
  at `/spaces` lists them; administrators create, rename, describe and
  delete them, recorded in the audit log, and every member reads and edits
  their pages. Space settings show the details and who may do what. A page
  is read at `/s/{key}/p/{id}/{slug}` and edited in a chunk of its own, and
  a save made from an older copy of the page is refused.
- A tree of pages in each space, shown in the sidebar a level at a time.
  It is walked with the arrow keys, pages are dragged beside or under one
  another, and M opens a dialog that moves a page from the keyboard. Pages
  move or copy with or without their children, into their own space or
  another; a move under itself is refused, by the database too. Every page
  shows where it sits in breadcrumbs, and new pages are added under any
  page.
- A trash in each space. Deleting a page takes it and every page below it
  out of the tree; the trash, under space settings, restores it where it
  was, or under the home page when the page it was under is gone.
  Administrators delete an item for good or empty the trash, which the
  audit log records.
- Files on pages, in the API: upload as a multipart part named `file`, list
  a page's files, download, and delete for good. Uploads are refused over
  `STATOR_UPLOAD_LIMIT` (50 MB unless set, as `attachments.uploadLimit` in
  the chart) with a message naming the limit. Images report their width and
  height. A download is an attachment with `nosniff`; with `inline=1`,
  pictures, PDFs and plain text show in place, and SVG and HTML never do.
  Files stay with a trashed page, come along when it is copied (the copy's
  version 1 names the copy's own files, so purging the original leaves its
  history whole), and leave
  the bucket when it is purged, its space is deleted or its file is
  deleted; the worker removes what is left behind.
- Files on pages, in the web client. Each page lists its attachments under
  the document, attached with the picker or by dropping them on the list,
  with the
  progress of each upload; they download from the list, and deleting one
  asks first. A file over the upload limit is refused with a sentence that
  names the limit and says what to do. In the editor a pasted, dropped or
  picked picture goes up first and then shows as an image with alternative
  text and a width to choose, and any other file as a chip that downloads
  it. A file deleted from the page shows as missing where the page used it.
