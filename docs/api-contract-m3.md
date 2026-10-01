# API contract for M3: the Armature integration

The agreement between whoever builds the backend and whoever builds the web
client of #27, #28, #29, #30, #31, #32, #33 and #34. As in
`docs/api-contract-m1.md` and `docs/api-contract-m2.md`, the route table in
`backend/internal/httpapi/openapi.go` is the source of truth for paths, fields
and types, and this file says what they mean. Every operation below is in the
table with `pending: true` and answers 501 `not_implemented` until it is
built; whoever builds one removes the mark, routes its handler and keeps this
file true. The types live in `backend/internal/armature`.

All paths are under `/api/v1`. Every operation but the webhook needs a member
of the organization with `use`; "admin" is an organization administrator, and
"view" and "edit" are the page rights of #19. A page the caller may not view is
404, never 403.

Everything said here about Armature's API was read from Armature's `main` at
b974701 (remote links, Cloudster1/Armature#16, included): `api/openapi.json`,
`internal/webhook`, `internal/netguard`, `internal/issue` and `internal/nql`.
What Stator needs and Armature does not offer yet is listed at the end.

## Migrations, packages and settings

| Range | Owner | Holds |
|---|---|---|
| 00160 to 00162 | connecting (#27) | `armature_connection` (one row per organization: base URL, Armature organization slug and id, the webhook secret sealed), `armature_token` (one row per member: the token sealed, its row id, whom it acts as, its status, when it was checked), their policies |
| 00163 | smart links (#28) | `page_plain_blocks` learns `armatureIssue` and `armatureIssueBlock`, both as their key, so search finds a page by the issues it names |
| 00164 to 00166 | issue block, list block, create (#29, #30, #31) | nothing is planned; kept for what building them finds |
| 00167 to 00169 | link sync (#32) | `armature_remote_link`, and the outbox policy for `armature.links` |
| 00170 to 00172 | webhooks (#33) | nothing is planned: replay protection and the cache live in Valkey |
| 00173 to 00175 | theme (#34) | following on `user_theme`, and the mirror columns on `theme` |
| 00176 to 00179 | spare | |

Packages: `internal/armature` (the client, the cache, link sync, the webhook
check), `internal/netguard` (adapted from Armature's, credited in `NOTICE`),
and `cmd/armature-stub`. The document nodes go into `internal/document` like
every other node.

New settings, in compose and the chart:

| Setting | Meaning |
|---|---|
| `STATOR_OUTBOUND_ALLOW` | Host names and CIDRs, comma separated, that the SSRF guard lets through although they are inside the network, as Armature's `ARMATURE_OUTBOUND_ALLOW`. Empty by default. |
| `STATOR_ARMATURE_BACKCHANNEL` | `public-origin=reachable-origin` pairs, as `STATOR_OIDC_BACKCHANNEL`: where this process reaches an Armature whose base URL people open under another name. |

## Rules for every Armature call

- **As the viewer.** Every call to Armature carries the caller's own personal
  access token as a bearer, so a page never shows an issue the viewer could not
  open in Armature (see `docs/decisions.md`). There is no shared token, for
  reads or for writes. The worker acts as the person whose change it is
  working on (#32).
- **Status instead of errors for reads.** A read that cannot ask Armature
  answers 200 with `status` saying why and nothing else: `not_configured` (the
  organization has no connection), `not_connected` (the caller stored no
  token), `rejected` (Armature answered 401 to the stored token; it is not used
  again until its owner stores a new one or checks it), `unreachable` (no
  answer within `armature.CallTimeout`, 5 seconds, an address the guard
  refused, a 5xx or a 429). A page with twenty chips then draws twenty keys,
  not twenty errors. `armature.StatusOf` names the status of a call's error.
- **What a viewer without a token sees.** Chips show the key alone, linked to
  the issue in Armature when the base URL is known; the hover card and the
  blocks say "Connect your Armature account to see this issue" with a button to
  the Armature section of the profile. Without a connection there is no link
  and no hint: the key is text in a chip.
- **Writes relay Armature's refusal.** Create (#31) answers 409
  `armature_not_configured`, 409 `armature_not_connected`, 409
  `armature_rejected`, 502 `armature_unreachable`, and otherwise Armature's
  own status with its sentence, so 403 when the caller may not file issues in
  the project and 422 for a summary it refuses. A 401 from Armature is never
  relayed as 401, which would read as "sign in to Stator".
- **The SSRF guard.** Every outbound call, to Armature and anywhere else, goes
  through `internal/netguard`, adapted from Armature's: the dialer resolves the
  host, refuses loopback, private, link-local, carrier grade NAT, multicast
  and the metadata ranges unless `STATOR_OUTBOUND_ALLOW` names the host or its
  address, and dials the address it checked, so a name cannot be swapped after
  the check. Redirects are followed at most three times and each is checked
  again; Go drops the `Authorization` header on a redirect to another host, so
  a token never leaves for another address. An answer is read up to
  `armature.MaxResponseBytes`, 4 MiB, and a theme file up to
  `theme.MaxPackageBytes`.
- **Local development.** The compose stack sets
  `STATOR_OUTBOUND_ALLOW=armature-stub` and
  `STATOR_ARMATURE_BACKCHANNEL=http://localhost:${ARMATURE_STUB_PORT}=http://armature-stub:8080`,
  so the base URL is the one a browser opens and the api reaches the stub on
  the compose network. To use a real Armature running on the host instead, set
  `STATOR_OUTBOUND_ALLOW=host.docker.internal`, give `api` and `worker`
  `extra_hosts: ["host.docker.internal:host-gateway"]`, and map
  `http://localhost:8080=http://host.docker.internal:8080`. Nothing else
  reaches inside the network, and nothing is allowed by default in the chart.

### The cache

Armature's answers are kept per person in Valkey, because what an issue shows
depends on who asks. Every key starts `stator:armature:{orgID}:`, and every
entry for a person is keyed by the id of their stored token row, which is new
each time a token is stored: a new token, or a new Armature identity behind
it, never reads an answer given to the old one.

| Key | Holds | Lives |
|---|---|---|
| `issue:{KEY}`, a hash, field `{tokenRowID}` | `{at, issue}`, the issue or null | `IssueCacheTTL`, 60 seconds |
| `search`, a hash, field `{tokenRowID}:{sha256 of q, limit, offset}` | `{at, result}` | `SearchCacheTTL`, 60 seconds |
| `meta:{tokenRowID}` | projects with `canCreate`, and issue types | `MetaCacheTTL`, 5 minutes |
| `theme:{tokenRowID}` | the id and `updatedAt` of the Armature theme, or null | `ThemeCacheTTL`, 5 minutes |
| `event:{eventID}` | that a webhook event was acted on | `WebhookReplayWindow`, 24 hours |

- Each write sets the hash's expiry to the TTL again, and a reader treats a
  field older than the TTL as a miss, so a busy hash never serves an old field.
- A miss asks Armature and stores the answer; a key Armature has no issue for,
  or one the person may not see, is stored as null for the same time, so a
  page naming a secret issue does not ask again on every view.
- The TTLs also bound what no webhook announces: a person losing access to a
  project in Armature, and a token revoked there.
- Without `STATOR_VALKEY_URL` nothing is cached and every view asks Armature.
  The process does not keep copies of its own: with several api processes, a
  webhook reaching one could not clear the others. A Valkey error is a miss,
  logged, never a failed read.
- Webhooks clear entries (#33); nothing else does.

## #27 Connect an Armature instance

| Operation | Needs | Answers |
|---|---|---|
| `GET /armature/connection` | admin | `{connection}`, null when none |
| `PUT /armature/connection` | admin | `{connection}`; 422 on `baseUrl`, `orgSlug`, `webhookSecret` |
| `DELETE /armature/connection` | admin | 204 |
| `GET /armature/account` | member | `{account}` |
| `PUT /armature/account/token` | member | `{account}`; 422 on `token`, 502 `armature_unreachable` |
| `POST /armature/account/check` | member | `{account}` |
| `DELETE /armature/account/token` | member | 204, also when there was none |

- **The connection** is the base URL people open Armature at (absolute
  `http` or `https`, no user, query or fragment, no path, trailing slash
  dropped; `https` outside development unless `STATOR_OUTBOUND_ALLOW` names
  the host) and the slug of the Armature organization. Saving it fetches
  `GET {baseUrl}/api/v1/openapi.json` through the guard and the backchannel,
  which Armature serves to anybody, and refuses the address with a sentence
  on `baseUrl` unless the answer is Armature's document (`info.title` is
  `Armature`). The API is always `{baseUrl}/api/v1`; an issue opens at
  `{baseUrl}/issues/{KEY}` and a query at `{baseUrl}/search?q={query}`.
- **The webhook secret** is the one Armature shows once when an endpoint is
  added (`armature_whs_` and 43 characters). It is sealed with
  `STATOR_SECRET_KEY`, bound to the organization, and never answered:
  `webhookSecretSet` says whether there is one. Absent keeps it, an empty
  string removes it.
- **A new address or organization forgets every stored token** in the same
  transaction, and the link records of #32, so an administrator can never
  send the members' tokens to a host of their choosing. Members are shown
  that they have to connect again. Removing the connection does the same.
- **`armatureOrgId`** is learned from the first token that checks out and is
  kept until the address or organization changes; webhooks from any other
  Armature organization are refused. `connected` counts the members with a
  stored token.
- **The admin screen** (Organization, Armature) shows `webhookUrl`,
  `{STATOR_APP_URL}/api/v1/armature/webhook/{orgSlug}`, the topics to
  subscribe it to (`webhookTopics`), and the steps in Armature: Settings,
  Webhooks, add an endpoint with that address and those topics, and paste the
  secret it shows here.
- **A token** is made by the person in Armature (Tokens, without the read-only
  mark, since #31 and #32 write) and pasted under Profile, Armature. Storing
  it asks Armature `GET /auth/me` with it: a 401 is 422 on `token` ("Armature
  did not accept this token. Make a new one under Tokens in Armature and paste
  it here."), a token of another Armature organization than `orgSlug` is 422
  as well, and an unreachable Armature is 502 `armature_unreachable`. A token
  that does not start with `armature_pat_` is 422 without asking. It is then
  sealed with `STATOR_SECRET_KEY`, the context binding it to the organization
  and the person, and never answered again. The row goes with the membership.
- **`account`** says whether the organization is `configured`, where issues
  open (`baseUrl`), whether the caller is `connected`, the `status` of the last
  check, whom the token acts as in Armature (`user`) and `checkedAt`. Only the
  person's own row is ever read; the database lets `stator_app` read and write
  only the actor's own token row, and only administrators the connection.
- **Check** asks `GET /auth/me` now and stores what it found as `status`:
  `ok`, `rejected`, or `unreachable`, which keeps the token for the next
  check. `ok` clears a `rejected` mark. Without a connection or a token it
  changes nothing and answers the account as it is.
- **Asking Armature on save** happens when the address is new or changed:
  changing the organization or the secret alone sends nothing, so an
  Armature that is down does not keep an administrator from rotating the
  secret. A new organization still forgets the tokens.
- Storing a token while the organization has no connection is 409
  `armature_not_configured`.

### What later issues build on

- `armature.Service.Viewer(ctx)` is the person ctx acts for (the request's,
  or the one the worker names with `db.WithUser`): their `TokenID`, which
  keys their cache entries, and a `Caller` that makes every call with their
  own token, or the status saying why there is none (`not_configured`,
  `not_connected`, `rejected`). A 401 from any call goes to
  `Service.NoteRejected`, so the token is not sent again.
- `Caller.Get` and `Caller.Send` take a path under `/api/v1`, answer
  `ErrRejected` for a 401, `ErrUnreachable` for no answer, a guard refusal, a
  5xx, a 429 or an unreadable answer, and `*RefusedError` with Armature's
  code, sentence, fields and position for any other refusal; the httpapi
  error mapping turns these into the statuses above. Calls are bounded by
  `CallTimeout`; a shorter context, such as `ThemeTimeout`, wins.
- `Service.Cache()` is nil without Valkey, and every method of a nil
  `*armature.Cache` is a miss or does nothing, so callers need no check.
- A Stator token of the `read` scope may call every GET here and so read
  Armature as its owner; the writes are refused to it like every other write.

## #28 Smart links for Armature issues

| Operation | Needs | Answers |
|---|---|---|
| `GET /armature/issues?key=...` | member | `{status, issues}`: one `{key, issue}` per key asked, issue null when not visible |
| `GET /armature/issues/{issueKey}` | member | `{status, issue}` |

- **The node.** `armatureIssue` (`armature.NodeIssue`) is an inline atom in
  paragraphs, headings, list items and cells, with one attribute, `key`,
  matching `armature.KeyPattern` (`^[A-Z][A-Z0-9]{1,9}-[1-9][0-9]{0,17}$`, the
  shape Armature's own keys have, stored upper case). Nothing about the issue
  is stored: its summary would otherwise be in a body that readers without
  access to the issue can read, and it would go stale. #28 adds the node to the
  allowlist; comments and templates do not take it in M3.
- **Making one.** Pasting `{baseUrl}/issues/{KEY}` (any case; a trailing
  slash, query or fragment is ignored) makes a chip when the organization is
  configured. Typing a key followed by a space or punctuation makes a chip
  when its project key is one of `GET /armature/projects` for the author, so
  `UTF-8` stays text; an author without a token gets chips from URLs only.
  Undo turns the chip back into the text. Nothing converts inside code blocks
  or inline code.
- **Drawing.** The reader collects every key in the body and asks
  `GET /armature/issues` once per `armature.MaxLookupKeys`, 50 keys (more is
  422 on `key`, and a key not matching the pattern is 422 too). The server
  answers from the cache and asks Armature for the rest with one NQL search,
  `key in (A-1, B-2)`, then `GET /issues/{key}` for each key the search did not
  return, which finds an issue moved to another project under its old key.
  The chip shows the key, the summary, the type's icon (`type.icon` is
  Armature's icon name; an unknown one draws the generic issue icon) and the
  status as a lozenge coloured by `status.category`. The chip links to
  `issue.url`.
- **The hover card** asks `GET /armature/issues/{issueKey}` (the same cache)
  and shows summary, type, status, priority, assignee and when it changed.
- **Readers without access** see the key only, with the link: Armature says
  the rest to whoever may see it.
- **Plain text** of a chip is its key, in Go (`document`) and in SQL
  (migration 00163), so search finds pages by the issues they name, and a
  comparison of versions shows the key.
- **As built in #28.** Keys are read in any case and answered upper case; a
  key asked twice is answered once, in the order first asked. A status other
  than `ok` comes with `issues: []`, never with what the cache holds. The
  fetches after the search run `armature.LookupParallel`, 8, at a time, and a
  404 or 403 from Armature is a null. A chip of a moved issue shows the
  issue's key now and links to it, and the page keeps the key it was named
  by. `GET /armature/projects` is built here, since typing needs it; its
  `meta` entry holds the issue types too, fetched with the projects, for
  #31. The way to connect is a link in the chip, "Connect Armature", to
  `/settings/profile#armature`; the hover card holds nothing to press. In
  the editor a chip is drawn the same but is not a link. Service methods:
  `Lookup`, `Issue`, `Projects`; `armature.NormalizeKey` and
  `armature.IssueURL`; on the web, `features/armature/issueKeys.ts` and
  `IssueChip.tsx`'s `ArmatureIssuesProvider`, which an issue block (#29) can
  draw from too.

## #29 Armature issue block

Uses `GET /armature/issues/{issueKey}` from #28.

- **The node.** `armatureIssueBlock` (`armature.NodeIssueBlock`) is a block
  atom with one attribute, `key`, as above.
- **Inserting.** The slash menu's "Armature issue" asks for a key or an issue
  URL, and inserts the block once `GET /armature/issues/{issueKey}` finds the
  issue for the author; otherwise it says so ("No issue CP-12 that you can
  see in Armature. Check the key, or connect your Armature account."). A
  typo is caught before it is published.
- **Drawing.** Summary, type, status, priority, assignee, reporter, due date
  and when it changed, and a link to Armature. It is asked for on every view,
  so it is at most `IssueCacheTTL` old, and fresh after a webhook; it does not
  poll while the page is open. Without a token or without access it is the key
  and the hint, as a chip.
- A comparison of versions describes it in words, "Armature issue CP-12", as
  generated blocks are described.

## #30 Armature issue list block from a query

| Operation | Needs | Answers |
|---|---|---|
| `GET /armature/search?q&limit&offset` | member | `{status, issues, total, limit, offset, url}`; 422 `bad_query` |

- **The node.** `armatureIssueList` (`armature.NodeIssueList`) is a block atom
  with three attributes: `query`, the NQL text (at most
  `armature.MaxQueryLength`, 2000 characters, not blank); `columns`, a list of
  1 to `armature.MaxColumns`, 10, distinct names from `armature.Columns` (`key`,
  `summary`, `type`, `status`, `priority`, `assignee`, `reporter`, `created`,
  `updated`, `due`), in the order shown; and `limit`, 1 to
  `armature.MaxListLimit`, 100. A new block takes `armature.DefaultColumns`
  and `armature.DefaultListLimit`, 20. #30 adds a `strings` attribute kind to
  the allowlist (a list of strings, each in its `enum`, at most `maxLength`
  items), which `api/document-allowlist.json` carries to the web client's test.
- **Search** passes `q`, `limit` (1 to 100, else 422) and `offset` to
  Armature's `GET /issues`, as the viewer, so `currentUser()` is the reader and
  every reader sees the rows they may see. `total` is Armature's, `url` opens
  the same query in Armature (`{baseUrl}/search?q=...`). A blank `q` is 422 on
  `q`.
- **Query errors.** Armature answers a query it cannot read with 400
  `bad_query`, a sentence and `position`, the 1-based character where it went
  wrong. Stator answers 422 `bad_query` with Armature's sentence and the same
  `position` (`APIError.position` is new, as Armature has it). The block
  shows the sentence and, to someone who may edit the page, the query with the
  position marked and an Edit button.
- **The table** is sortable by clicking a column's head, which orders the rows
  already fetched; the query's own `ORDER BY` decides the first order. Below
  it: "Showing 20 of 143" and "Open in Armature". Without a token or without a
  connection it says so, with the hint, and draws no rows.
- **The insert dialog** checks the query as it is typed with the same route
  and `limit=1`, and offers the columns as checkboxes in `armature.Columns`
  order.
- The list is never part of the plain text: its rows are not the page's words.

## #31 Create Armature issues from selected text

| Operation | Needs | Answers |
|---|---|---|
| `GET /armature/projects` | member | `{status, projects}`: `{key, name, canCreate}` |
| `GET /armature/issue-types` | member | `{status, issueTypes}`, subtasks left out |
| `POST /armature/issues` | edit on `pageId` | 201 `{issues, failed}`; 409, 502, and Armature's own refusals |

- **Projects** are Armature's `GET /projects` (archived left out). `canCreate`
  comes from Armature's `GET /access/me`: `issue.write` among
  `permissions.org`, or among `permissions.projects[key]`, which already holds
  a token's confinement to projects. The dialog offers only projects with
  `canCreate`. **Issue types** are Armature's `GET /issue-types` without the
  ones with `isSubtask`, which would need a parent. Both are cached as `meta`.
- **What becomes an issue.** A selection inside one paragraph, heading or cell
  is one item, its summary the selected text with runs of white space made
  one. A selection over list or task items is one item per item, its summary
  the item's own text without its nested lists. A selection over table rows is
  one item per row that has a cell, header rows left out, its summary the
  row's first cell with text. At most `armature.MaxCreateItems`, 50, items;
  a summary longer than `armature.MaxSummaryLength`, 255, characters is cut
  in the dialog, where every summary stays editable and an item can be taken
  out.
- **The dialog** is offered from the selection's bubble menu as "Create
  Armature issue" when the author's account is `ok`; otherwise it shows the
  connect hint. It asks for the project, the type (Armature's default when
  none) and the summaries.
- **Filing.** The server files the items one at a time, in order, with
  Armature's `POST /issues` `{projectKey, typeId, summary, description}` as
  the caller. The description is one paragraph, "From {page title} in Stator",
  the title linked to `{STATOR_APP_URL}/s/{spaceKey}/p/{pageId}`, which
  Armature's description allowlist takes. It stops at the first item Armature
  refuses: 201 with the issues made so far and `failed: {index, code,
  message}`; when the first item is refused, the refusal itself is the answer.
  Armature has no batch create, and a refusal of one item (the project, the
  type, the rights) would refuse the rest the same way.
- **The chip.** For a selection inside one block, the selected text is
  replaced by the new issue's `armatureIssue` chip. For list items and table
  rows, the text stays and the chip follows it, after a space, at the end of
  the item's first paragraph or of the row's first cell. Items that failed or
  were not tried stay as they were. This changes the draft like any edit; the
  issue gets its link to the page when the page is published (#32).
- **Retrying.** A create that timed out may have reached Armature; the client
  says so and does not retry by itself.

## #32 Show pages that mention an issue in Armature

| Operation | Needs | Answers |
|---|---|---|
| `GET /pages/{pageID}/armature-links` | view | `{links}`: `{key, state, error, syncedAt}` per key the published version names |

- **Which keys.** The distinct keys of the `armatureIssue` and
  `armatureIssueBlock` nodes in the page's published body. List blocks are
  queries, not mentions, and bare text is not a key until it is a chip.
  Drafts and comments name nothing.
- **When.** Every change that can change the page's links writes the outbox
  event `armature.links` `{pageId, actorId}` in its own transaction, while the
  organization has a connection: any publish (publish, restore, `PATCH`, the
  first publish, a copy's), a rename (a publish), moving to another space
  (the page's address changes), trashing, restoring from the trash and
  purging. The event carries no keys: the worker reads what the page names
  when it runs, so an event that waited is never stale, and two events for a
  page do the same thing.
- **What is wanted.** For a published page out of the trash, one remote link
  per key: `url` `{STATOR_APP_URL}/s/{spaceKey}/p/{pageId}` (no slug, so a
  rename keeps the address that Armature keys its links by), `title` the
  page's title, and `source` `Stator` (`armature.LinkSource`). A page not every
  member with `use` may view is linked as "A restricted page in Stator":
  everybody who may see the issue in Armature sees the link, and a title could
  say more than the page's readers want told. For a page that is trashed,
  purged or no longer names a key, no link for it.
- **Syncing.** `armature_remote_link` records, per page and key, the remote
  link's id in Armature, the url and title sent, the `state` and the last
  error. It has no foreign key to the page, so a purge can still remove what
  it recorded. The handler compares what is wanted with what is recorded:
  a missing or changed link is sent with Armature's
  `POST /issues/{KEY}/remote-links`, which puts a new link or retitles the one
  with that url, so sending again is harmless; a link no longer wanted is
  removed with `DELETE /issues/{KEY}/remote-links/{remoteLinkID}`, a 404
  counting as removed; a link whose url changed is put under the new url and
  the old one removed.
- **As whom.** The event's actor, with their own token: the person who
  published, moved or trashed the page. Their name is what Armature records as
  the link's creator, and a link appears on an issue only when somebody who
  may edit that issue in Armature named it, so a page cannot pin itself onto
  issues its author cannot touch. A service token was the alternative; it
  would put links on every issue any member names and hide who did it.
- **Failures.** Armature unreachable, a timeout, 429 or a 5xx fails the
  handler, and the outbox tries the event again with its growing delay, up to
  `events.MaxAttempts`; the records stay `pending`. A refusal that another try
  would meet again (no token, a rejected token, a read-only token, 403, or 404
  for an issue that is not there or not the actor's to see) marks that key
  `failed` with a sentence and does not fail the event. The next event for the
  page, whoever causes it, tries every failed key again. `links` shows the
  author which keys are `synced`, `pending` or `failed` and why.
- **The worker** gains a handler per topic: the notifications for theirs, link
  sync for `armature.links`, so a link that fails never runs the fan-out
  twice. The database lets `stator_app` insert `armature.links` only with the
  actor's own id, as every event.
- A new connection address or organization drops the records (#27); the links
  already in the old instance stay there, keyed by url, and a later sync to the
  same instance finds them by url again.

## #33 Receive Armature webhooks to refresh cached issues

| Operation | Needs | Answers |
|---|---|---|
| `POST /armature/webhook/{orgSlug}` | the signature | 204; 401 `bad_signature`; 404 `not_found`; 413 `too_large` |

- **Why the slug is in the path.** The receiver has to find the secret before
  it can trust anything in the body, and the organization's slug is what the
  sign-in path already uses. The admin screen shows the whole address.
- **Verification.** The body is read whole, up to `armature.WebhookMaxBytes`,
  1 MiB. An organization with no connection or no secret is 404. The header
  `X-Armature-Signature-256` must be `sha256=` and the lower case hex
  HMAC-SHA256 of the raw body under the secret, compared in constant time, as
  Armature's `webhook.Sign` makes it; otherwise 401 `bad_signature`. Then the
  body is Armature's envelope, `{id, topic, orgId, occurredAt, payload}`, and
  an `orgId` other than the connection's `armatureOrgId`, once known, is 401
  too.
- **Replays.** Armature sends the body it stored on every retry and
  redelivery, so `occurredAt` can be hours old for a genuine delivery and is
  not judged. Instead the event `id` is remembered in Valkey for
  `WebhookReplayWindow`, 24 hours, once acted on; a second delivery of it is
  204 and changes nothing. The worst a replay can do anyway is clear a cache
  entry. Armature signs no timestamp; see the changes Armature needs.
- **What each topic clears** (`armature.WebhookTopics`):

  | Topic | Clears |
  |---|---|
  | `issue.created` | `issue:{key}` (a null stored before it existed) and `search` |
  | `issue.updated` | `issue:{key}`, `issue:{movedFrom}` when the issue moved, and `search` |
  | `issue.transitioned` | `issue:{key}` and `search` |
  | `comment.added` | `issue:{key}` |
  | `ping` | nothing |

  Any other topic is 204 and ignored. `search` is cleared whole, for everybody
  in the organization: any change may change any query's rows.
- Without Valkey there is nothing to clear; a delivery is still verified and
  answered 204.

## #34 Use my active Armature theme

| Operation | Needs | Answers |
|---|---|---|
| `GET /armature/theme` | member | `{follow}`: `{following, status}` |
| `PUT /armature/theme` | member | `{follow}` |
| `DELETE /armature/theme` | member | 204 |

Changed: `GET /themes/active` answers `source: "armature"` (`theme.SourceArmature`)
while the caller follows Armature and Armature answered.

- **The choice.** The theme menu gains "Follow Armature". Following is one of
  the person's choices, like choosing a theme: it is kept on their
  `user_theme` row, it wins over the organization's default, choosing any
  theme or the built-in one with `PUT /themes/active` ends it, and
  `DELETE /armature/theme` returns them to the organization's default.
- **Fetching.** While following, `GET /themes/active` reads the person's
  cached `theme` entry, or asks Armature's `GET /themes/active` with their
  token within `armature.ThemeTimeout`, 2 seconds, so a slow Armature never
  holds the page. Armature answers the theme it shows them, which may be its
  organization's default, or null for its built-in theme.
- **Applying.** A theme is applied exactly as an imported `armature-theme/1`
  file: Stator downloads it with Armature's `GET /themes/{themeID}/export`,
  checks it with the same code as `POST /themes/import`, and keeps it as the
  person's one mirror, a theme of theirs marked as the copy of Armature's theme
  with its id and `updatedAt`, left out of `GET /themes` and not to be
  edited, shared or exported. It is downloaded again only when Armature
  answers another id or a later `updatedAt`. Its files are served by Stator
  like any theme's, so the browser never asks Armature for them.
  `GET /themes/active` answers the mirror with `source: "armature"`, or
  `theme: null` with `source: "armature"` when Armature shows its built-in
  theme, which Stator's built-in theme matches token for token.
- **Falling back.** Without a token, with a rejected one, or when Armature does
  not answer in time, `GET /themes/active` answers what the person would see
  without following (the organization's default, else the built-in theme),
  and `GET /armature/theme` says why in `status`. Following stays on, and the
  next load tries again.

## The armature-stub

`cmd/armature-stub` stands in for Armature in the integration suite and in
Playwright, as `docs/decisions.md` says. It is a small Go server in the
backend image, a compose service `armature-stub` on the stack's network with
its port published as `ARMATURE_STUB_PORT`, the ninth of the checkout's block.
#27 builds it, and each issue after adds what it calls.

- **What it serves**, under `/api/v1`, answering exactly Armature's shapes:
  `GET /openapi.json` (Armature's document, vendored), `GET /auth/me`,
  `GET /access/me`, `GET /projects`, `GET /issue-types`, `GET /issues` (NQL:
  `key in (...)`, `key =`, `project =`, `project in`, `statusCategory =` and
  `!=`, `assignee = currentUser()`, joined by `AND`, and `ORDER BY` key,
  created, updated or priority; anything else is 400 `bad_query` with a
  position), `GET /issues/{issueKey}` (with old keys of moved issues),
  `POST /issues`, `GET` and `POST /issues/{issueKey}/remote-links`,
  `DELETE /issues/{issueKey}/remote-links/{remoteLinkID}`, `GET /themes/active`
  and `GET /themes/{themeID}/export`.
- **Who asks.** A token `armature_pat_{tenant}_{person}` is `person` in the
  stub's organization `tenant`, made on first use with fixed projects (`CP`
  that everybody may write, `SEC` that only `admin` may see), issue types and
  issues. The person `reader` has a token of the read scope, refused every
  write with `read_only_token`, and `armature_pat_revoked` is always 401. Each
  Playwright spec uses a tenant named after its throwaway organization, so
  specs never share issues or links.
- **Steering it** under `/_stub/`, which is not Armature's and is left out of
  the contract test: change an issue's fields, move it, set a person's active
  theme, read the remote links it holds, and send a signed webhook for a
  change to a given address and secret, exactly as Armature's `webhook.Sign`
  and headers do, so the Playwright spec for #33 goes through the real
  receiver. As built in #27: `PATCH /_stub/{tenant}/issues/{key}`
  (`summary`, `statusCategory`, `priority`, `assignee` by name),
  `POST /_stub/{tenant}/issues/{key}/move` (`projectKey`),
  `PUT /_stub/{tenant}/people/{person}/theme` (`theme`, an example theme's
  key or empty), `GET /_stub/{tenant}/remote-links`,
  `POST /_stub/{tenant}/webhooks` (`url`, `secret`, `topic`, `payload`, and
  `id` to repeat an event), and `DELETE /_stub/{tenant}` to start a tenant
  afresh.
- **The fixed world.** Every tenant has the projects `CP` and `SEC`, the
  types Task, Bug, Story and Sub-task, and the issues `CP-1` to `CP-5` and
  `SEC-1`; `CP-5` was `SEC-2` before it moved, and answers to that key too.
  A person is named `{person}` capitalised, with the email
  `{person}@{tenant}.armature.test`, and alice is assigned `CP-1` and `CP-4`.
  The browser opens the stub at `STATOR_ARMATURE_URL` from
  `.cache/stack.env`; the integration suite names it
  `STATOR_TEST_ARMATURE_URL` and reaches it at `STATOR_TEST_ARMATURE_STUB_URL`.
- **The contract test**, a Go unit test beside the stub and so in
  `make check-go`, loads Armature's `api/openapi.json` vendored at
  `api/armature/openapi.json` (with `api/armature/SOURCE` naming the Armature
  commit it came from; `make armature-openapi REF=...` fetches it in a
  container) and checks that every route the stub serves outside `/_stub/` is
  an operation of that document with the same method and path, drives each
  route through a scenario, and validates every answer against the document's
  schema for its status, and every request body Stator's client sends against
  the request schema, with `internal/openapi`'s validator. Updating the
  vendored document is how a change in Armature reaches Stator; the test then
  says what the stub has to follow.

## Changes Armature needs

To be filed in Cloudster1/Armature; none blocks the start of M3.

1. **Announce deleted issues, and created ones from every path.** Deleting an
   issue emits nothing, and a clone or an import emits no `issue.created`, so
   a webhook receiver keeps a deleted issue, and a query missing new issues,
   until its cache runs out. Wanted: `issue.deleted` `{issueId, key, actorId}`,
   subscribable by webhooks, and `issue.created` for clones and imports. Stator
   then clears `issue:{key}` and `search` for it (#33).
2. **Say what a token may do.** `GET /auth/me` called with a token does not
   say that the token has the read scope, so Stator cannot warn at connect time
   that it will not create issues or sync links. Wanted: `GET /auth/me` answers
   `token: {id, name, scopes, projects, expiresAt}` when the caller is a token,
   null for a session (#27, #31, #32).
3. **Sign a timestamp on webhooks** (hardening, not needed for M3). A delivery
   carries no signed time, so a receiver can only defend against replays by
   remembering event ids. Wanted: an `X-Armature-Timestamp` header, and a
   second signature header over `{timestamp}.{body}`, so receivers written for
   today's header keep working (#33).
