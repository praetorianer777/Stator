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
- A personal access token can be limited to spaces: the Tokens page picks
  them, and `spaces` on `POST /api/v1/tokens` names their keys. Such a token
  reaches those spaces alone, even where its owner reaches more, and is
  refused with `spaces_token` what concerns the whole organization, such as
  making spaces, the audit log or tokens; an assistant holding it is not
  offered those tools. The database holds it there too, through
  `app.token_spaces`, so search, the home feed, the stale report, page views
  and its own views and shares keep to its spaces. The list says what each token reaches, and `audit_log` names
  the spaces a token was made for.
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
  who may comment put an emoji on from a picker of common ones, or any
  other found by name in the editor's emoji list, and take theirs back with
  one click or key; each emoji shows its count, whether it is yours, and,
  on hover or focus, who reacted, you first, the first ten by name and the
  rest counted. Readers without the right to comment see the same and add
  nothing. A deleted comment loses its reactions. `POST` and `DELETE` on
  `/pages/{pageID}/reactions` and `/comments/{commentID}/reactions`, and
  `Page` and `Comment` gain `reactions`. The database holds every reaction
  to its page's view rule and to its author's name (migration 00180).
- German and English interface (#113). Every string the web client shows
  exists in both languages, and a missing German one fails the type check.
  Each person chooses under Profile, Language, kept with their account by
  `PATCH /auth/me` (`User` gains `locale`: `en`, `de`, or empty to follow
  the browser); until they choose, the browser's own languages decide, and
  English is the fallback. Dates and numbers are written in the language
  shown, the page's `lang` follows it, and switching redraws the page at
  once. Mails and the sentences the server writes stay English for now. The
  database holds the choice to the two languages (migration 00195).
- Status labels, dates and emoji in running text. A status is a coloured
  label with words of the author's own, drawn like an Armature issue's
  status in one of five theme colours; a date is a day picked with the
  browser's date field, stored as `YYYY-MM-DD` and shown in each reader's
  own format. Both go in from the slash menu, open their dialog on a click
  or on Enter once the arrow keys select them, and are found by search
  through their words and their day. Typing a colon at the start of a word,
  in a page or a comment, offers emoji by name from a list bundled with the
  client; the one picked goes in as text. The server refuses a status
  without words or longer than 40 characters, a colour of its own, and a
  day that does not exist.
- Find and replace in the page editor. Ctrl or Cmd+F inside the editor, or
  the toolbar's search button, opens a bar that highlights every match, says
  how many there are, steps through them with Enter and Shift+Enter, and
  matches case when asked. Replace changes the current match and moves on;
  Replace all changes every match in one step that one undo takes back.
  Words are found across bold, italic and other marks, never inside a chip
  or a mention, and the replacement keeps the marks of the text it replaces.
  Escape closes the bar and puts the caret on the last match; outside the
  editor Ctrl or Cmd+F stays the browser's.
- Checking somebody's access to a page (#81). An administrator of the space,
  or of the organization, picks a person under the page's Check access and
  sees whether they may view, edit, move to the trash and comment, each with
  the conditions behind it: their standing in the organization, the use and
  space grants that reach them, unpublished pages above, and every view or
  edit list on the page and the pages above it with whom it names. The one
  that decides a no is marked. The answers are the database's own, from the
  functions its policies call, through `GET /pages/{pageID}/access/{userID}`;
  migration 00200 lets an administrator of a space read which use grants
  reach the person.
- Markdown import and export. A page exports as one Markdown file, or as a
  `.zip` with its files and, if asked, every page below it the reader may
  view, each in a folder named after the page above it. Markdown files, a
  folder of them or a `.zip` import as pages under any page the importer may
  edit: each file is a page, each folder a page whose `index.md` or
  `README.md` is its content, the pictures and files they show become the
  page's files, and links between the files become links between the pages.
  The leading level 1 heading is the title. `PUT /pages/{id}/markdown`
  publishes one file as a page's next version. The conversion runs on the
  server, so a script with a token gets what the page menu gets; raw HTML
  never reaches a page, and `docs/markdown.md` says how each block is
  written, including the panels, expand blocks, statuses, dates, mentions
  and Armature issues Markdown has no syntax for. One import takes up to
  100 MB, 1000 files and 200 pages, and one Markdown file up to 1 MB.
- Stars and a home page (#38). A star in a page's header, on a space's home
  page and in the space directory keeps a page or a space close; stars are
  one's own and say nothing to anybody else. The home page lists them beside
  the pages one viewed and edited last (drafts and unpublished pages marked),
  and the updates others published, everywhere one may read or only in what
  one watches. Every list leaves out what the reader may no longer view, is
  read a window at a time by cursor (`GET /stars`, `/home/updates`,
  `/home/edited`), and pages and spaces answer `starred`.
- Sharing a page (#67). The page header's Share sends a published page to
  people and groups with an optional note of up to 200 characters
  (`POST /pages/{id}/share`); they are told in the app and by mail as a new
  notification kind, `shared`, with its own switches in the preferences.
  The dialog says who can already view the page (`GET /pages/{id}/viewers`)
  and its picker marks whoever may not (`GET /pages/{id}/share/recipients`).
  Sharing gives nobody access: a share naming a person, or a group none of
  whose members may view the page, is refused with `cannot_view` and sends
  nothing, a group tells only its members who may view it, and the worker
  checks each recipient again as it writes their row. One person shares at
  most 30 pages an hour in an organization (`rate_limited`), a limit the
  database keeps as well. Each share is recorded in the audit log as
  `page.shared`, naming whom it was sent to but never its note.
- An audit log for administrators (#107), under Audit log in the account
  menu: who changed members, single sign-on, tokens, spaces, permissions,
  the default theme and the Armature connection, what was deleted for good,
  and which pages and logs were exported, newest first. It narrows by
  action, person, target and days, pages by cursor (`GET /audit`,
  `/audit/facets`) and exports as CSV (`GET /audit/export`). Each entry is
  written in the transaction of its act, records ids, names and whether a
  secret changed but never a secret, and cannot be changed or deleted by the
  app. The worker keeps entries for `STATOR_RETAIN_AUDIT` (a year;
  `retention.audit` in the chart) and prunes older ones daily.
- Page owners and verified pages (#68). Whoever may edit a published page
  names its owner, a member who may view it, and verifies the page for 30,
  90, 180 or 365 days (`PUT` and `DELETE` on `/pages/{id}/owner` and
  `/pages/{id}/verification`, at most 730 days through the API). The page's
  header shows the owner and a Verified badge, or Verification expired once
  the term ran out, which opens who verified it, when, and whether the page
  changed since; search and the home page's updates mark verified pages.
  The worker looks for lapses every `STATOR_VERIFICATION_CHECK_INTERVAL`
  (10 minutes; `verification.checkInterval` in the chart) and reminds the
  owner, or whoever verified the page when the owner may no longer view it,
  with a new notification kind, `expired`. Owner changes and verifications
  are written to the audit log. Editing a verified page keeps its
  verification; the badge's details say which version was checked.
- Archived pages and spaces (#37). An administrator of a space archives a
  page with every page below it from the page menu, or the whole space from
  its settings (`PUT` and `DELETE` on `/pages/{id}/archive` and
  `/spaces/{key}/archive`). Archived content stays readable and keeps its
  addresses, with a banner that says how and since when, but leaves the page
  tree, search, quick search and the home page's lists, and nothing in it
  changes until it is unarchived: no edit, move, comment, reaction, label,
  file or restriction, which the database refuses too, with a refusal that
  says to unarchive first. Each space has an Archive tab listing its
  archived pages (`GET /spaces/{key}/archived-pages`), the search has an
  Include archived pages filter (`archived=true`), and the list of spaces
  shows archived spaces when asked (`GET /spaces?archived=true`). The access
  inspector names the archive as what refuses a change, and archiving and
  unarchiving are written to the audit log.
- A stale content report (#99), under Stale pages in the account menu for
  organization administrators and in a space's settings for its
  administrators: published pages nobody published again or opened for at
  least 180 days, or a period of 30 to 730 days, the longest untouched first,
  with when each was last published and opened, its owner and its
  verification. It narrows by space, owner and verification, leaves archived
  pages out unless asked (`archived=true`), and pages by cursor
  (`GET /stale-pages`). Chosen rows are archived in bulk from the report,
  each through the page's own archive. Each reader sees the spaces they
  administer, and only pages they may view; opening a page from the report
  to review it does not count as a view.
- An MCP server at `POST /api/v1/mcp` (#106), so an assistant can search,
  read and write pages as the person whose token it holds. Its tools are
  rows of the route table marked with a name and a sentence, as in
  Armature, and each call runs as the HTTP call it stands for, through the
  same middleware, handlers and row level security. Pages read and write as
  Markdown (`get_page_markdown`, `replace_page_markdown`, `import_markdown`)
  besides their documents; search, spaces, versions, labels, comments,
  people and notifications are there to read. A read-only token is offered
  only the reading tools, and nothing removes, shares, administers or
  touches another person's attention. The tokens page says how to connect a
  client, and `docs/mcp.md` lists the tools.
- Page views (#97): the line under a page's title says how often it was
  read, and opens its views in all and over the last 30 days, counting each
  person once a day however often they open it
  (`GET /pages/{id}/views`, the `get_page_views` tool). People who may edit
  the page also see who read it within the retention, the latest first
  (`GET /pages/{id}/readers`); anybody can hide their name from that list
  in their profile (`showInReaders` on `PATCH /auth/me`) and is still
  counted. The worker keeps who read what for `STATOR_RETAIN_PAGE_VIEWS`
  (90 days; at least 30; 0 keeps them) and then keeps only the count.
  Reloading a page writes nothing to the database, and recent pages no
  longer write when the page opened is already the latest one of the day.
- Outbound webhooks (#110), under Webhooks in the account menu, kept by the
  organization's administrators as in Armature. A webhook takes page
  published, page moved, page deleted and comment added, or everything,
  and the worker posts each event as Armature's envelope, signed with
  `X-Stator-Signature-256: sha256=` and the HMAC of the body under a secret
  shown once and sealed with `STATOR_SECRET_KEY`. A failed attempt is tried
  again after 1, 5 and 30 minutes, 2 and 12 hours; a webhook that fails
  every attempt for a day is turned off and the audit log says so. The
  delivery log keeps every attempt for 30 days, any of which can be sent
  again, and a test ping checks the address. Payloads are read as the
  webhook's owner, the administrator who saved it last, when they are sent,
  so a page the owner may not view is withheld rather than posted. Every
  request goes through the SSRF guard (`STATOR_OUTBOUND_ALLOW`). Adding,
  changing, rotating and deleting a webhook is written to the audit log.
  Moving a published page to another parent or space and deleting one now
  write `page.moved` and `page.deleted` to the outbox.
- Space shortcuts (#39): the administrators of a space pin up to 30 links
  above its page tree, to pages of any space or to web addresses, under
  Shortcuts in the space's settings, and order them with move up and move
  down buttons (`/spaces/{key}/shortcuts`, read by the
  `list_space_shortcuts` tool). Everybody who reads the space sees them; a
  shortcut to a page is left out for whoever may not view the page or while
  it is in the trash. Only `http` and `https` addresses without a name or
  password are taken, refused by the database as well as the API, and they
  open in a new tab with `rel="noopener noreferrer nofollow"`. Adding,
  moving and removing a shortcut is written to the audit log.
- Column layouts (#42). "Two columns" and "Three columns" in the slash menu
  put the blocks under the caret in the first of that many columns side by
  side. The toolbar changes the layout, even or with one column wider, and
  going from three columns to two folds the third into the second;
  "Remove the columns, keep their content" puts the blocks back one after
  another. Under 48rem the columns stack in reading order. The page stores
  each column's share of the row, from 10 to 80 percent; the API refuses
  fewer than two columns or more than three, and a comment holds none.
  Search reads every column, and a Markdown export writes them one after
  another.
- Personal spaces (#35). Everybody may make one space of their own from the
  space directory, named for them, for drafts and notes. Nobody else sees it
  until its owner shares it from its permissions like any other space;
  administrators of the organization reach it, as every space. The directory
  lists personal spaces apart, with whose each is. The database holds to one
  each, made only by and for its owner, and refuses handing it to somebody
  else; members still need `createSpace` for any other space.
- Folders in the page tree (#36). "New folder" in a page's menu, or on a
  folder, adds a folder: a named group of pages and folders with no text of
  its own, seen at once by everybody who may see where it is. Opening one
  lists what it holds; it is renamed from its menu, and moved, copied,
  restricted, archived and deleted as a page is, with what it holds. The
  tree marks it with a folder icon. A folder takes no body, drafts,
  versions, comments, reactions, labels, files, shares or stewardship, and
  the database refuses them whichever request asks; `POST /pages` takes
  `kind: "folder"`.
- The organization's hub page (#40). Under Hub page in the account menu, an
  administrator names one page of any space as the organization's hub,
  which everybody who may read it finds under Hub in the navigation, and
  may make it where everybody lands when they open Stator; their own home
  is then at `/home`. `GET /org/hub` and `PUT /org/hub` (administrators
  only, in the audit log). Only administrators change it, the database
  included, and a hub page deleted for good stops being the hub.
- Decision items (#44). "Decision" in the slash menu turns a line into a
  decision, undecided until its label is pressed to mark it decided; readers
  see the state in words before the line. Each space has a decision log,
  under Decisions in its navigation, quoting every decision on its published
  pages the reader may read, newest page first, filtered by state and linked
  to its page (`GET /spaces/{key}/decisions`, the `list_decisions` tool).
  Search finds a page by its decisions, and a Markdown export writes each
  as a line that starts with its state.
- Math formulas (#45). "Inline formula" and "Formula block" in the slash
  menu ask for LaTeX source in a dialog that typesets it as it is typed and
  refuses what cannot be typeset; a click on a formula opens its source
  again. Readers see formulas typeset with KaTeX, with MathML for screen
  readers. Search finds a page by its formulas' source, and a Markdown
  export writes them as `$...$` and a `math` fence, which an import reads
  back as a formula.
- Diagrams (#46). "Diagram" in the slash menu puts in a block of Mermaid
  text with a sketch to start from, drawn below the text as it is typed and
  saying why when it cannot be. Readers see the diagram as SVG in the
  page's own colours and can download it as an SVG file. Labels are text,
  never markup, links or pictures. Search finds a page by its diagrams'
  text, and a Markdown export writes a `mermaid` fence, which an import
  reads back as a diagram.
- Link previews and embeds (#47). An address pasted alone on an empty line
  becomes a card with the linked page's title, summary and site, and the
  page's player when it is a YouTube or Vimeo video or a Figma file; "Link
  preview" in the slash menu asks for one. A card's toolbar shows it inline,
  as a link titled as its page, as a card, or embedded. The server reads
  what a page says about itself through the outbound guard
  (`STATOR_OUTBOUND_ALLOW`), from its head alone, and keeps it an hour in
  Valkey (`GET /link-preview`); a page keeps only the address and the view.
  Players load in a sandbox from the origins the Content-Security-Policy's
  new `frame-src` names.
- Excerpts (#48). "Excerpt" in the slash menu marks the blocks under the
  caret as a named part of the page, framed for its author with its name in
  a box to type over; readers see only the blocks. An excerpt keeps an id
  that outlives a rename, and the editor gives a pasted copy an id and a
  name of its own and takes the frame off one pasted inside another.
  `GET /pages/{id}/excerpts` (the `list_page_excerpts` tool) lists a page's
  published excerpts by name with the start of their words, and a picker
  chooses a page and the whole of it or one excerpt, for the include block.
- Include page and excerpt blocks (#53). "Include" in the slash menu opens
  the picker, and the page then shows the other page's published words, or
  one excerpt's, framed and named with a link to where they come from, kept
  up to date as that page changes. Each reader sees what they may read: a
  page kept from them, never published, or whose excerpt is gone is one
  notice that says no more. A page cannot include itself, and an include
  that leads back to a page on the way to it, or more than five deep, says
  so instead (`GET /pages/{id}/included`). A Markdown export keeps what an
  include points at, which an import reads back.
- Page appearance (#50). Appearance in a page's menu chooses an emoji,
  found by name, shown before the title and in the page tree; a width,
  fixed for comfortable lines or full for wide tables and diagrams; and a
  cover picture from the page's own files or a new upload, with the point
  that stays in view, set by a click or the arrow keys. Only the page's
  editors change it (`PUT /pages/{id}/appearance`), the database included;
  a cover is always one of the page's own pictures, and goes when its file
  is deleted.
- Armature charts (#51). "Armature chart" in the slash menu asks for a
  project, an NQL query and a chart: a pie of the matching issues by status,
  status category, type, priority or assignee, or the issues created against
  resolved each day over 7 days to a year. Armature counts them with each
  reader's own token, through its reports, so every reader sees the chart of
  the issues they may see (`GET /armature/chart`). The pie keeps status
  categories in their board colours and lists every share in a table beside
  it; created against resolved reads a day at a time with the pointer or the
  arrow keys and opens as a table. The stub serves the two reports and
  `resolvedAt`.
- Armature roadmaps (#52). "Armature roadmap" in the slash menu asks for a
  project and an NQL query, and draws the matching issues on a timeline of
  their start and due dates, under their epics or their teams, from
  Armature's plan read with each reader's own token
  (`GET /armature/roadmap`). An epic heads its issues even when the query
  leaves it out, and spans their dates when it has none of its own, drawn as
  an outline; a bar takes its status category's board colour, a lone start or
  due date is a point, and today is marked. Issues without either date are
  counted below it. The stub serves the plan, an Epic type, teams, start dates
  and parents.
- Page properties (#54). "Properties" in the slash menu puts a two-column
  table of names and values on a page, starting with Owner and Status; a
  value takes marks, mentions, dates and statuses, Enter starts the next
  property and Backspace in an empty one removes it. Search reads them as
  table rows. "Properties report" lists the published pages carrying every
  label given, in one space or all, with a column for each property found or
  for those named, sortable by any column, as each reader may read them
  (`GET /properties-report`, also the MCP tool `properties_report`).
- Page lists (#55). "Content by label" lists the published pages carrying
  all or any of some labels, in one space or all, latest first or by title,
  5 to 50 of them (`GET /labelled-pages`); "Recently updated" lists the pages
  published last in a space or across the organization, with who published
  each (`GET /updated-pages`). Both show each reader the pages they may read,
  leave the trash, the archive and folders out, and keep only their settings
  in the page, so an overview stays current without editing. The latest blog
  posts block waits for blog posts (#72).
- Tasks (#56): a checklist item is a task, assigned to the first person it
  @mentions and due on the first date in it. Publishing gives every item a
  `taskId` and reads the tasks from the stored page, so the document stays
  the only record; a task can only be assigned to a member who may view the
  page, and the publish says so otherwise. My tasks, in the sidebar and on
  the home page, lists the caller's open tasks soonest due first and their
  done ones, from every page they may still read (`GET /tasks`, the
  `list_my_tasks` tool). A box ticks off on the page, or in the list, for
  whoever may edit the page, which publishes it as the next version
  (`PATCH /pages/{id}/tasks/{taskId}`, the `set_task_done` tool). An
  assignment tells the assignee once, and the worker reminds them on the
  due day, in UTC, every `STATOR_TASK_DUE_CHECK_INTERVAL` (10 minutes;
  `tasks.dueCheckInterval` in the chart); both are new kinds in the
  notification settings. A page shows when a task is overdue or due today.
- Task report (#57): a block that lists the tasks of published pages, in one
  space or all, assigned to whoever reads the page, to nobody or to one
  person, overdue, due today, due in the next 7 days or without a day, open,
  done or both, 10 to 100 of them: open ones soonest due first, then done
  ones (`GET /task-report`, also the MCP tool `task_report`). Each reader
  sees the tasks on pages they may view, and whoever may edit a task's page
  ticks it off from the report. The page keeps only the filter.
- Files block and file versions (#58). A file uploaded to a page under a
  name it already has, whatever the case, is that file's next version: each
  version keeps its own bytes and links, and the database numbers them. The
  list of a page's files says each one's `version` and how many `versions`
  the page holds, and `current=true` lists only the latest of each name
  (`GET /pages/{id}/attachments`, the `list_attachments` tool). "Files" in
  the slash menu puts the page's files in its content, the latest version of
  each with its size, uploader and version and the earlier ones a click
  away, and whoever may edit the page uploads from the block.
- Chart from table (#59). "Chart from table" in the slash menu, or "Chart
  this table" in the table tools, draws a table as bars, lines or a pie: the
  first row names the series, the first column the categories, and numbers
  may be written 1,234.5 or 1.234,5, with a sign, a unit, a percent or a
  currency. The block holds the table itself, so editing the table redraws
  the chart; readers see the table under it unless the author hides it, and
  can read each category by pointer or keyboard, or open the numbers as a
  table. A chart draws 8 series at most; a pie draws the first.
- Team calendars (#60). A space keeps calendars, at most 20, each a name and
  its events: a title, an event or an absence, and whole days or two times
  (`GET` and `POST /spaces/{key}/calendars`, `PATCH` and `DELETE
  /calendars/{id}`, `GET` and `POST /calendars/{id}/events`, `PUT` and
  `DELETE /calendars/{id}/events/{eventId}`, also the MCP tools
  `list_calendars`, `create_calendar`, `rename_calendar`,
  `list_calendar_events`, `create_calendar_event` and
  `update_calendar_event`). Everybody who reads the space reads them, and
  whoever may add pages to it keeps them while it is not archived, which the
  database holds too. "Calendar" in the slash menu draws a month of one,
  week by week, with an Armature project's issues on the days they are due
  (`GET /armature/calendar`), read with each reader's own token; whoever may
  change the calendar adds, changes and deletes events from the block. The
  page keeps only which calendar and which project.
- PDF and office previews (#61). A PDF, and a docx, xlsx, pptx, odt, ods or
  odp document (or an older doc, xls or ppt), opens in place from the
  attachments panel and the files block, in the browser's own PDF viewer,
  with a download beside it. Office documents are converted to PDF by a
  conversion service in the compose stack (`STATOR_CONVERTER_URL`;
  `attachments.converterUrl` in the chart), once per version, and the PDF is
  kept beside the file and goes with it. Each file says its `preview`, and
  `GET /attachments/{id}/preview` answers the PDF, or a sentence saying why
  there is none and to download the file. Documents over 20 MB are not
  converted.
- Space templates (#64). `GET /space-templates` serves three built-ins, a
  knowledge base, a team space and documentation, each a home page, a tree
  of published pages with their labels, and what everyone in the
  organization may do in the space. `POST /spaces` takes the key of one as
  `template` and makes the space, its pages, their labels and its
  permissions in one transaction, logged as `space.created` with the
  template's key; the creator administers it, as in a blank space. The new
  space form offers a blank space and every template as a radio group, with
  the chosen one's pages, labels and permissions beside it. A personal
  space starts blank.
- Template button and contributors (#62). "Template button" in the slash
  menu puts a button on a page that makes a new page from a template, under
  a chosen page or at the top of a space, with words of its own and a title
  in which `{date}` becomes the day, then opens it to write
  (`POST /templates/{key}/pages`, also the MCP tool
  `create_page_from_template`). Whoever may not add pages there sees it
  disabled, with a sentence saying why (`GET /template-button`).
  "Contributors" lists the people who published versions of the page, or of
  it and the pages below it the reader may view, the most versions first,
  each with their picture, how many versions and the day of the last, 1 to
  50 of them (`GET /pages/{id}/contributors`, also the MCP tool
  `list_page_contributors`). The page keeps only the template, the place
  and the words, and which pages to count.
- Editing together (#65). People who open a page's editor at the same time
  edit one shared draft: each sees the others' words as they are typed,
  their carets with their names in their colours, and their avatars in the
  header. The draft is a Yjs document the api keeps as a log of updates per
  page (`GET /pages/{id}/collab`, a WebSocket for a signed-in session that
  may edit the page, checked again every 30 seconds) and passes between api
  processes through Valkey, or through Postgres's LISTEN and NOTIFY when
  no Valkey is configured, so several api pods need no Valkey for it. A
  process that loses the other processes for a while relays what was
  stored meanwhile once it hears them again. A browser that loses the connection keeps
  editing, keeps its changes in IndexedDB, and merges them when it is back.
  Publishing publishes the shared draft as it stands and moves everybody on
  to the new version; discarding throws it away for everybody. Each person's
  own draft still holds what they publish, and the editor edits alone, as
  before, when the shared draft is out of reach. The chart's nginx passes
  the WebSocket on.
- Guests (#69). An administrator of the organization invites somebody from
  outside into one team space by their address, to read, to read and
  comment, or to read, comment and edit, under the space's new Guests tab
  (`GET`, `POST /spaces/{key}/guests`, `DELETE /spaces/{key}/guests/{id}`).
  The guest signs in through the organization's provider with that address
  and lands in the space. They reach nothing else: no other space, no
  personal space, no tokens, nothing of the organization as a whole, which
  answers `403 guest`, and of its people only those of their space, so the
  people picker, mentions and every name a page shows keep to the space
  and the groups are hidden. The database holds all of it, and refuses a
  guest a second space, a group, administering their space or a change of
  role. The members list marks guests with their space; invitations and
  removals go to the audit log, and deleting the space takes its guests
  with it. An assistant acting for a guest is offered only what the guest
  may do.
- Reading without signing in (#79). An administrator of the organization
  lets anybody read the spaces that allow it (`PUT /org/anonymous-access`,
  under Settings, Permissions), and a space's administrators allow it in
  the space's permissions tab (`PUT /spaces/{key}/anonymous-access`); a
  personal space never does. Anybody then reads the published pages of
  those spaces at `/public/{org}`, with their files and a search, in a
  reading view with a header and a way to sign in and nothing else of the
  app. A restricted page, a draft and everything of other spaces is not
  found, and a link to it leads to signing in. Nobody is named: no authors,
  comments, reactions, readers, watchers, contributors, assignees or
  history, and a mention reads as someone, in the API's answers as on the
  page. The database holds an anonymous reader to reading those spaces,
  pages and files and to writing nothing. Search engines are asked to stay
  away unless the organization lets them in, and both switches go to the
  audit log.
- Public links (#80). Whoever may edit a published page makes a link in
  the share dialog that lets anybody read that page without an account,
  with an optional label and a lifetime of a day to three months or none,
  and sees the page's live links with who made them and when they run out,
  and revokes them; the address is shown once, when the link is made. A
  page has at most five live links, and a restricted page, a draft, a
  folder and a page of a personal space get none. The link opens that page
  and its files in a reading view with nobody named in it, whether or not
  the organization opens any space, and nothing else: not the pages below
  it, its space, comments or history. A link stops when it is revoked or
  runs out, and while the page is restricted, unpublished or in the trash.
  An administrator stops every link at once under Settings, Permissions
  (`PUT /org/public-links`), and allows them again; nothing is deleted.
  The database holds a reader with a link to that one page. Answers through
  a link are never cached, send no referrer and keep the token out of the
  logs, search engines are asked to stay away unless the organization lets
  them in, and making, revoking and the switch go to the audit log.
- Copying space permissions (#82). In a team space's permissions tab, an
  administrator copies the permissions of another team space they
  administer: replace makes this space's grants the other's, keeping this
  space's guests; merge only adds subjects and widens what they hold. A
  preview (`GET /spaces/{key}/permissions/copy`) lists per person or group
  what is added, widened, narrowed or removed, what cannot be copied (a
  guest of the other space) and whether the copy would leave the space
  without an administrator, which is refused. Applying
  (`POST /spaces/{key}/permissions/copy`) takes the preview's fingerprint
  and is refused once either space's permissions changed since, so what is
  applied is what was shown; it is one step and one audit entry naming
  both spaces, the mode and the counts. Page restrictions are not copied,
  and personal spaces are neither copied from nor into.
- Live pages (#70). A page's editors choose, under Editing mode in the
  page's menu, between drafts and publishing and live
  (`PUT /pages/{id}/mode`). On a live page what is typed is saved to the
  page about a second later (`PUT /pages/{id}/live`), with nothing to
  publish, and a reader with the page open sees it within seconds. The
  history keeps a version per ten minutes of work, amended by every save in
  it and naming everybody who saved into it; mentions and assignments added
  by a save are told once, and webhooks hear of each version as of a
  publish. Going live throws away the drafts nobody published, so it names
  whose they are first and goes ahead only when confirmed; the switch goes
  to the audit log. The database holds a live page to no drafts and lets
  only its open version be amended, only by the page's editors.
- Scheduled publishing (#71). The publish dialog offers "At a set time",
  in the person's own time zone (`PUT /pages/{id}/schedule`): at that time
  the worker publishes the author's draft as it then stands, in their name,
  with the comment and notice they chose, and the watchers hear of it as of
  any publish. A time missed while the worker was down goes out once when
  it returns, and any number of workers publish it exactly once. A page
  holds one schedule, shown to its author and editors, who may call it off
  (`DELETE /pages/{id}/schedule`); discarding or publishing the draft, or
  making the page live, takes it too. A publish refused at its time, since
  the author lost edit, the page was archived or deleted, or somebody
  published after the draft began, is kept with why and its author is told
  once. The worker looks every `STATOR_SCHEDULE_CHECK_INTERVAL` (30
  seconds; `publishing.scheduleCheckInterval` in the chart). The database
  holds a schedule to its author's own draft, set by an editor for a time
  ahead, and lets only the worker mark it failed.
- Blog posts (#72). Each space has a blog at `/s/{key}/blog`: dated posts
  outside the page tree, written by whoever may add pages there
  (`POST /spaces/{key}/posts`) and published, scheduled, versioned,
  commented, labelled, restricted and searched as pages are. A post's date
  is its first publish. The blog lists the posts a reader may read newest
  first, by year and month with counts (`GET /spaces/{key}/blog`,
  `GET /posts`), and each writer's own posts still to go out. Watching a
  blog (`PUT /spaces/{key}/blog/watch`) hears of its new posts as the
  notification kind `posted`, which a watch on the space hears too; a
  webhook's page now says its `kind`. A latest blog posts block lists the
  newest posts of this space, another or every space, as each reader may
  read them. `get_blog`, `list_posts` and `create_post` are MCP tools. The
  database holds a post outside the tree, unpublished when written, to
  whoever may add pages to a space not archived, and keeps its date.
- Restoring a file's version (#95). The attachments below a page list each
  name once, latest first, with its earlier versions under it to download,
  preview, restore or delete; the files block restores too. A restore
  (`POST /attachments/{id}/restore`) uploads that version's bytes again as
  the name's next version, marked `restoredFrom`, so nothing is
  overwritten; the latest is refused with `already_latest`. Deleting a
  name deletes all its versions (`DELETE /attachments/{id}?versions=all`).
  The database holds a restore to an earlier version of the same file, by
  whoever may edit the page.
- Image annotation (#94). Whoever may edit a page crops a PNG, JPEG or
  WebP picture and draws arrows, boxes and text on it in six colours, with
  undo, redo, moving and deleting by pointer, touch and keyboard, opened
  from the picture in the editor, the attachments below the page or the
  files block. Saving flattens it in the browser and sends it as the
  file's next version in its own type
  (`POST /attachments/{id}/edit`, refused with `not_editable` or
  `wrong_type`), marked `editedFrom`; the version drawn on stays. Opened
  from the editor, the dialog offers to show the edited picture in the
  page. The database holds an edit to a picture's version of the same
  file and type, by whoever may edit the page.
- Templates of the organization's own, with variables. Administrators of
  the organization keep templates every space offers, under Settings,
  Templates; administrators of a space keep its own, under the space's
  settings. A template defines variables (text, date, choice or person,
  each with a label, a default and whether it is required) and puts their
  blanks in its body and title. Making a page from it asks for the values in
  a form, and the server fills them in: words, a date, a mention of a member
  who may view the space. An optional blank left empty becomes a hint, which
  publishing removes. `GET /templates?space=`, `POST /templates`,
  `PUT /templates/{templateKey}` and `DELETE /templates/{templateKey}` keep
  them, `POST /pages` takes `template` and `values`, and the database refuses
  a blank in any page and holds templates to the administrators of their
  scope. Variables travel through Markdown as a marked span.

### Changed

- A space keeps at least one administrator of its own: taking administer
  from the last person or group that holds it is refused, by the api and by
  the database, unless an administrator of the organization does it.
- A page's text keeps a readable measure of 44rem, about 85 characters,
  centred, while its wide blocks use the window up to 96rem: tables and
  their charts, diagrams, math blocks, code, columns, link cards and
  embeds, Armature charts, roadmaps and issue lists, calendars and
  property reports; a picture takes its own width between the two. The
  title, the header and the sections below the page keep to the measure.
  The layout is the same in the reader, the editor, a version, a
  comparison and the public view, and a phone is unchanged. Full width now
  widens the text too (#287).

- The CI gate may run 45 minutes rather than 30, since the suite had grown
  to fill the old limit, and a cancelled run now says it was stopped rather
  than failed (#283).

- Renaming and deleting a space and purging its trash are for the space's
  administrators, and making spaces for whoever holds `createSpace`, rather
  than for the organization's administrators alone.

- `PATCH /pages/{id}` publishes the title and body as the next version with
  no comment. Existing pages become version 1 of themselves.

- Stored files are keyed under `org/<organization id>/`, so an
  organization's files can be listed and removed together.

### Fixed

- Generating the OpenAPI document fails, naming both Go types, when two
  types from different packages would be documented under one schema name,
  rather than picking a name by the order the routes happen to be read in.
  `Builder.Names` names one of them apart; the existing pairs keep the names
  they had.

- A page read from a replica that was replaying a change no longer mixes two
  moments, such as a new owner and verification beside permissions from
  before its restriction, which could offer a reader actions they may not
  take. Every read now sees one snapshot from start to end.

- Following the Armature theme no longer drops back to the built-in theme
  when the page is reloaded while Stator copies a changed theme: the copy is
  finished even after the request is gone, and only a finished copy is shown.

- Saving an edited comment puts focus back on its Edit button every time,
  not only when the save's answer and the next frame came in the right order.

- Opening a reply or a new comment puts the caret in its editor every time.
  When the editor's code was still loading, the focus could go to the editor
  before it was on the page, and the keyboard was left on nothing.

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
- The access inspector no longer says a person may move a space's home page
  to the trash. It answers from the same database rule that keeps the home
  page out of the trash, and names it as the reason.
- Enter in a people or group picker no longer picks a match left over from
  the text before: while the answer to what was typed is still on its way,
  Enter waits for it, and screen readers hear that the picker is looking.
