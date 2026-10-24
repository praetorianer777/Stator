# Integration capabilities

`GET /api/v1/meta` lists, under `capabilities`, the integration features this
build offers, as stable names with a version, such as `page-links@1`, in the
scheme Armature uses for its own. A partner decides from these names whether
a feature is there, not from the version number of the build. `/meta`
answers anyone, so a partner can ask before it has an account; the names
promise features, not data.

```json
{
  "product": "stator",
  "build": { "version": "1.3.0" },
  "apiVersion": "1",
  "capabilities": ["oauth.partner-tokens@1", "oauth.token-exchange@1", "page-links@1", "page-remote-links@1", "theme-export@1", "webhook-receiver@1", "webhooks@1"]
}
```

The list is sorted. Paths below are under `/api/v1`, spelled as in
[`api/openapi.json`](../api/openapi.json).

## What Stator offers

### `page-links@1`

Each Armature issue a published page names as a chip or an issue block
carries the page's remote link, put on and taken off as the page changes, and
the page says how each stands. Stator puts the link as the person who changed
the page, with `source` `Stator`.

- `GET /pages/{pageID}/armature-links`
- the worker's topic `armature.links`

### `page-remote-links@1`

Another tool puts links to its objects on a page, such as the requirements a
page specifies, as somebody who may edit the page, with that person's own
personal access token; a link with the same url replaces the one there, and
the tool hears through a webhook when somebody takes one off. The page shows
them to its readers, grouped by source.

- `GET /pages/{pageID}/remote-links`, `POST /pages/{pageID}/remote-links`
- `DELETE /pages/{pageID}/remote-links/{linkID}`
- topic `page.remote_link.removed`

See [Remote links on pages](#remote-links-on-pages).

### `webhook-receiver@1`

Armature's signed webhook deliveries, envelope version 1, received for the
issue topics, so pages show a change to an issue at once. A delivery in any
other version is refused with 422 `unsupported_version`, and one whose signed
time is more than five minutes from Stator's clock with 401 `stale_delivery`;
see [Receiving Armature's webhooks](#receiving-armatures-webhooks).

- `POST /armature/webhook`
- acts on `issue.created`, `issue.updated`, `issue.transitioned` and
  `comment.added`

### `webhooks@1`

Endpoints subscribed to topics, their deliveries, a redelivery, a test
delivery and a new secret, each delivery a signed, numbered envelope as
[Webhooks Stator sends](#webhooks-stator-sends) describes.

- `GET /webhooks`, `POST /webhooks`
- `PATCH /webhooks/{webhookID}`, `DELETE /webhooks/{webhookID}`
- `GET /webhooks/{webhookID}/deliveries`
- `POST /webhooks/{webhookID}/deliveries/{deliveryID}/redeliver`
- `POST /webhooks/{webhookID}/test`, `POST /webhooks/{webhookID}/rotate-secret`
- topics `page.published`, `page.moved`, `page.deleted`, `comment.created`
  and `page.remote_link.removed`

### `oauth.partner-tokens@1`

An app, such as Rotor or an AI assistant, connects to Stator's MCP endpoint
as a person by OAuth 2.1, instead of the person pasting a `stator_pat_`
(#449), as Armature and Exciter offer under the same name. The app registers
itself (RFC 7591) and is pending until a Stator administrator allows it by
connecting it once; a person then connects it by authorization code with
PKCE `S256` on Stator's consent page, choosing read or write and the spaces
it reaches. The access token lasts at most an hour and works at the MCP
endpoint alone; the refresh token rotates at each use. See
[Connecting an app to MCP with OAuth](#connecting-an-app-to-mcp-with-oauth).

- `GET /.well-known/oauth-authorization-server` and
  `GET /.well-known/oauth-protected-resource/api/v1/mcp`, at the origin
- `POST /oauth/register`, `GET /oauth/authorize`
- `GET /oauth/consent`, `POST /oauth/consent`
- `POST /oauth/token`, `POST /oauth/revoke`, `POST /mcp`

### `oauth.token-exchange@1`

A partner tool that signed a person in through the shared Keycloak realm
trades their access token for a Stator token of theirs, with no consent page
and no pasted `stator_pat_` (#461), as Armature does since
Cloudster1/Armature#280: `POST /oauth/token` with `grant_type`
`urn:ietf:params:oauth:grant-type:token-exchange`, `subject_token` an access
token of Stator's identity provider whose `azp` is the partner app's
Keycloak client id and whose `aud` holds Stator's own client, the app
authenticating with its client id and secret. The answer is an access and a
refresh token within the person's rights. See
[Handing partner tools a person's token](#handing-partner-tools-a-persons-token).
The same grant also takes, as `subject_token`, a `stator_oat_` access token
of the calling app's own grant, partner or connected on the consent page,
and answers a narrower access token for one run, with no refresh token
(#499), as Armature and Exciter do; see
[A narrower token for one run](#a-narrower-token-for-one-run).

- `GET /.well-known/oauth-authorization-server`, at the origin
- `POST /oauth/token`, `POST /oauth/revoke`

### `theme-export@1`

A theme exported as one `armature-theme/1` file with its pictures and fonts,
and such a file imported as a theme of the caller's own.

- `GET /themes/{themeID}/export`
- `POST /themes/import`

The list lives in `backend/internal/httpapi/capabilities.go`. A unit test
refuses a capability naming an operation the operations table does not have
or that answers 501, a topic `events.Topics` does not list or one the
receiver does not act on, and a name that is not `name@version` or is listed
twice.

## Remote links on pages

A tool keeps its own links on a page, as Stator keeps its links on Armature's
issues (`page-links@1`). The model is Armature's remote links on issues.

| Field | What it is |
| --- | --- |
| `url` | The object's address, `http://` or `https://`, at most 2048 characters. The key: a page has one link per `url`. |
| `title` | The object's title, at most 255 characters. |
| `source` | The tool's name, at most 100 characters, such as `Exciter`. The page groups links by it. |
| `iconUrl` | Optional: a small picture of the tool, `http://` or `https://`. Where it does not load, the source's initial stands in. |

`POST /pages/{pageID}/remote-links` with those fields answers 201 and
`{remoteLink}` for a new link, and 200 for one whose `url` the page has
already, which it replaces: the title, source and icon are the new ones, the
`id` and `createdBy`, who first put it, stay. So a tool can send all of its
links again whenever it likes. A field that cannot be stored is 422 with a
sentence on that field.

- **Who.** `GET` needs view of the page, and answers `{remoteLinks}` by
  source, then in the order they were put; `POST` and `DELETE` need edit of
  it. A page the caller may not view is 404, one they may view but not edit
  403, and a read-only token is refused every write with 403
  `read_only_token`. The database holds the same rules, so a link is never
  read, put or taken around the service.
- **Taking one off.** `DELETE /pages/{pageID}/remote-links/{linkID}` answers
  204, or 404 for a link that is not on that page. Whoever took it off, the
  tool that put it included, every webhook subscribed to
  `page.remote_link.removed` is told, so a tool can leave off what a person
  took off:

  ```json
  { "page": { "id": "...", "title": "Plans", "version": 3, "url": "...", "space": { "key": "DOC", "name": "Docs" }, "kind": "page" },
    "remoteLink": { "id": "...", "url": "https://exciter.example/r/...", "title": "REQ-1 Sign in (specifies)", "source": "Exciter" },
    "actor": { "id": "...", "name": "Ada" } }
  ```

  As every payload, the page is read as the webhook's owner when the attempt
  is sent; one they may not view, or one never published, withholds the
  delivery.
- **The trash.** A link stays with its page in the trash, is out of reach
  there, comes back with it, and goes when the page is purged, without a
  webhook: `page.deleted` has said the page is gone. A copy of a page takes
  none of its links, which belong to the original.
- **No other topic.** Putting a link is not announced; the tool that put it
  knows.

## Webhooks Stator sends

Each delivery is one envelope in Armature's shape, signed in Armature's
headers under Stator's prefix, so a receiver written for one product serves
both. The machine-readable half is the `webhooks` section of
[`api/openapi.json`](../api/openapi.json), whose `event` operation describes
the body, every payload and the headers; the integration suite holds every
delivery it sees to it.

```json
{
  "version": 1,
  "id": "0199e1c2-7d4a-7c11-9b1e-3f0a2b4c5d6e",
  "source": "stator",
  "topic": "page.published",
  "occurredAt": "2026-10-15T09:30:00Z",
  "payload": { "page": { "id": "...", "title": "Plans", "version": 3, "url": "...", "space": { "key": "DOC", "name": "Docs" }, "kind": "page" }, "actor": { "id": "...", "name": "Ada" } }
}
```

| Field | What it is |
| --- | --- |
| `version` | The envelope version the endpoint was registered for, an integer. |
| `id` | The event's id, the same on every retry and redelivery, so a receiver can drop one it has handled. |
| `source` | The product that sent it, always `stator`. Added to version 1 without raising it, as a step toward the shape a later version takes. |
| `topic` | `ping`, `page.published`, `page.moved`, `page.deleted`, `comment.created` or `page.remote_link.removed`. |
| `occurredAt` | When the event happened, in UTC. |
| `payload` | The event's own fields, read as the endpoint's owner when the attempt is sent. |

| Header | What it carries |
| --- | --- |
| `X-Stator-Event` | The topic, as in the body. |
| `X-Stator-Delivery` | This attempt's id; a retry and a redelivery each have their own. |
| `X-Stator-Webhook-Version` | The envelope version of the body, as in its `version`, so a receiver can decide before it parses. |
| `X-Stator-Signature-256` | `sha256=` and the hex HMAC-SHA256 of the raw body, keyed with the endpoint's secret. |
| `X-Stator-Timestamp` | When this attempt was sent, in Unix seconds. |
| `X-Stator-Signature-Timestamped-256` | `sha256=` and the hex HMAC-SHA256 of the timestamp, a dot and the raw body. |

Verify a signature over the bytes received, before parsing them. Each
attempt is signed when it is sent, so a receiver that compares the timestamp
with its own clock, as Stator's own receiver does within five minutes, can
refuse a replay.

### Versions

- `version` rises only for a change that breaks a receiver: a field of the
  envelope or of a payload removed, renamed or changing its meaning. Adding a
  field or a topic does not raise it. A receiver ignores fields and topics it
  does not know, and refuses an envelope version it does not know with a
  clear status, such as 422, and a log line, instead of misreading it.
- An endpoint is registered for a version: `POST /webhooks` takes `version`,
  the current one when left out, `PATCH /webhooks/{webhookID}` keeps it unless
  it names another, and a version Stator does not send is refused with the
  ones it does. Endpoints made before the envelope had a number are on 1,
  which is what they were always sent.
- When version N+1 arrives, an endpoint registered for N keeps being sent N
  for at least one Dynamo release, and moves when its administrator moves it.
- A delivery keeps the version it was queued in: its retries and
  redeliveries say the same, whatever the endpoint is on by then. One that
  is in a version this Stator does not send, which only a later release taken
  back can leave, fails with a sentence saying so and is not retried, since
  every attempt would fail alike.

| Version | Since | What changed |
| --- | --- | --- |
| 1 | #412 | The envelope as it was, with `version` and `source` added, and the timestamp headers. |

A later version takes a wider event standard's shape; it comes beside 1, and
each endpoint moves when its administrator registers it for the new one.

## Receiving Armature's webhooks

`POST /armature/webhook` takes what Armature's webhook service sends, as
Armature's `docs/webhooks.md` describes it for receivers.

1. The plain signature, `X-Armature-Signature-256`, must match, or the answer
   is 401 `bad_signature`.
2. When the delivery carries `X-Armature-Timestamp` or
   `X-Armature-Signature-Timestamped-256`, both must be there and the
   timestamped signature must match, or it is 401 `bad_signature`; a time
   more than `armature.WebhookTimestampTolerance`, five minutes, from Stator's
   clock either way is 401 `stale_delivery`. Armature signs each attempt
   afresh, so a genuine retry is never stale. An Armature from before #242
   signs no time; its deliveries are taken on the plain signature, and a
   replay of one is still held off by the event ids remembered for a day.
3. `X-Armature-Webhook-Version`, and then the body's `version`, must be 1, or
   the answer is 422 `unsupported_version`, which Armature logs and retries.
   A body without a version is from before Armature numbered it and is read
   as 1.
4. Fields and topics Stator does not know, such as a `source`, are ignored,
   with 204.

`GET /armature/connection` answers under `lastDelivery` the last delivery
taken: when, its topic, the version its body named and whether its time was
signed, and the settings page says so, so an administrator sees that the
webhook arrives and whether Armature should be updated.

## What Stator needs of Armature

Stator reads the connected Armature's `/meta` and turns off each feature
whose capability it does not list:

| Feature | Capability |
| --- | --- |
| issue chips, issue blocks, list blocks and the issue picker | `issue-embed@1` |
| creating issues from a page | `issue-embed@1` |
| chart blocks | `reports@1` |
| roadmap blocks | `plan@1` |
| issues in calendar blocks | `calendar@1` |
| linking pages to the issues they name | `remote-links@1` |
| following the Armature theme | `theme-export@1` |
| the webhook from Armature | `webhooks@1` |
| connecting an account with one click, below | `oauth.partner-tokens@1` |
| connecting Armature in one step, below | `webhooks.partner@1` beside `oauth.partner-tokens@1` |
| connecting people at sign-in, below | `oauth.token-exchange@1` |

Armature names no capability for filing issues, so creating them rests on
`issue-embed@1`, which the chips they leave behind need; a name for it belongs
in Dynamo's list. The map is `armature.FeatureCapabilities`, and
`ARMATURE_FEATURE_CAPABILITIES` in `web/src/config.ts` for the sentences.

- **Asked, then kept.** Each api process keeps an Armature's answer for
  `CapabilitiesTTL` (five minutes), and one that did not answer for
  `CapabilitiesRetry` (thirty seconds). Nothing announces an upgrade, so the
  settings page, connecting and Check again on the profile ask afresh.
- **Said, not failed.** A read for a missing feature answers the status
  `outdated` without asking Armature; creating issues and following the
  theme are refused with `armature_outdated`; link sync records a sentence on
  each link. `GET /armature/account` lists the missing features, so pages say
  so before asking, and `GET /armature/connection` says under `partner` what
  Armature reported. The settings page and each block name Armature and the
  capability to update for, in English and German.
- **Older than /meta.** An Armature that answers 404 at `/meta` is one from
  before capabilities: it offers none, and the settings page says it is too
  old to say what it offers.
- **Not answering is not missing.** An Armature that did not answer `/meta`
  turns nothing off; the read itself says Armature did not answer.

## Exciter requirements on pages

Stator shows Exciter's requirements on its pages as it shows Armature's
issues (#440), as agreed in Exciter's `docs/api-contract-backlog.md`, section
"#43 Exciter requirements in Stator pages". Package `internal/exciter`
follows `internal/armature` and goes through its outbound client, cache and
signatures; the routes are under `/exciter`.

- **Connecting.** An administrator saves Exciter's origin under Settings,
  Exciter (`PUT /exciter/connection`); it must answer `product` `exciter` at
  `GET /api/v1/meta`, and `POST /exciter/connection/check` asks before
  saving. The outbound guard refuses an address inside the network in the
  same sentence as Armature's, naming the value to allow. Each member stores
  their own personal access token (`exciter_pat_`, a read-only one serves)
  under Your profile, Exciter, checked at Exciter's `GET /auth/me`, sealed
  with `STATOR_SECRET_KEY` and bound to the site and the person. A new
  address forgets every token, in the database (00820).
- **What Stator needs of Exciter**, by exact name at `/meta`:

  | Feature | Capability |
  | --- | --- |
  | requirement chips and blocks | `requirement-embed@1` |
  | list blocks | `requirement-list@1` |
  | the projects a list block offers | `specifications@1` |
  | the webhook from Exciter | `webhooks@1` |

  A missing one answers `outdated` and the chip says so, without asking
  Exciter; `exciter.FeatureCapabilities` and `EXCITER_FEATURE_CAPABILITIES`
  hold the map.
- **Chips.** A key such as `CHK-R42` (`^([A-Z][A-Z0-9]{1,9})-R([1-9][0-9]*)$`)
  is stored as an `exciterRequirement` node holding the key alone. It is
  typed, by an author whose Exciter account is connected, or inserted from
  the slash menu's Exciter requirement, which is offered only while Stator is
  connected to Exciter. `GET /exciter/requirements?key=` reads each key with
  the viewer's token at Exciter's `GET /requirements/{reqKey}` and answers the
  title, the status, the revision and `trashedAt`, linking to
  `{exciter}/p/{projectKey}/r/{key}`. A former key answers the requirement
  under its current key, which the chip shows. Exciter's 404 is a requirement
  the reader does not see: the chip shows the key and says so. Without a
  token the key is a link to Exciter beside a hint that opens the token form.
- **The picker.** The slash menu's Exciter requirement and Exciter
  requirement block open one picker: `GET /exciter/search?q=` asks Exciter's
  `GET /search/quick` as the author for the requirements whose key or title
  start with the words, at most 10, sections left out. Words that are a
  former key are read by key too, so the requirement it names now is offered.
  Only a requirement Exciter found reaches the page.
- **Requirement blocks.** An `exciterRequirementBlock` node holds the key
  alone. `GET /exciter/requirements/{reqKey}` answers the chip's fields with
  the `statement` and `rationale`, ProseMirror documents in Exciter's schema,
  null when empty; chips and blocks share one cache entry per key. The block
  draws the nodes and marks Stator knows (paragraphs, headings as bold lines
  outside the page's outline, lists, quotes, code, rules, tables, bold,
  italic, strike, code and links of `http`, `https` and `mailto` only); a
  picture, an attachment, a mention, a glossary term and any node a later
  Exciter adds are shown as their words.
- **List blocks.** An `exciterRequirementList` node holds `project`, `query`
  and `limit` (1 to 500), never its rows. The editor offers the projects
  `GET /exciter/projects` reads from Exciter's `GET /projects` as the author,
  and completes the query at the caret from
  `GET /exciter/projects/{projectKey}/requirements/suggest?q=&at=`, Exciter's
  suggest; `at` counts characters, and a pick writes over the characters
  `completion.from` to `completion.to`. Each reader's view asks
  `GET /exciter/projects/{projectKey}/requirements?query=&limit=&offset=`,
  Exciter's grid with `kind=requirement`, with their own token, so
  `currentUser()` is the reader; it shows key, title, status and revision, 50
  rows a request up to the limit, with `total` for Show more. A query Exciter
  does not read is 422 `bad_query` with `position`, which the block and the
  editor mark in the query; a project the reader may not view answers
  `found: false`. A blank query lists the whole project.
- **Cache.** Each answer is kept per viewer's token row for at most
  `exciter.RequirementCacheTTL`, 60 seconds, in Valkey, with the keys each
  requirement was read by under its id. Lists are kept per reader, project,
  query and window, and the projects per reader; the picker and completion
  ask Exciter each time.
- **Webhooks.** An Exciter administrator adds a webhook for
  `POST /api/v1/exciter/webhook`, envelope version 1, with the topics
  `requirement.created`, `requirement.revised`, `requirement.changed`,
  `requirement.trashed`, `requirement.restored`, `requirement.moved` and
  `review.decided`, and the administrator pastes its `exciter_whs_` secret
  into Settings, Exciter. A delivery must carry `X-Exciter-Signature-256`,
  `X-Exciter-Timestamp` and `X-Exciter-Signature-Timestamped-256` over the
  body, within five minutes of Stator's clock (401 `bad_signature` or
  `stale_delivery`), and `X-Exciter-Webhook-Version` and `version` 1 (422
  `unsupported_version`). A body whose `source` is not `exciter`, and a topic
  or field Stator does not know, is answered 204 and changes nothing. A
  delivery only clears the cache: the keys it names, `fromKey` and `toKey`,
  every key the requirement of its `id` was read by, and every list, as any
  change may change what a query finds. Its payload, read as the webhook's
  owner, is never shown.
- **Moving an organization.** The organization's archive carries the
  Exciter address, as it carries Armature's; the members' tokens and the
  webhook secret stay behind and are listed among what does not come across.
  An organization that has its own Exciter connection keeps it.
- **The stand-in.** `cmd/exciter-stub` serves `/meta`, `/auth/me`,
  `/requirements/{reqKey}`, `/search/quick`, `/projects` and a project's
  `/requirements` and `/requirements/suggest` in Exciter's shapes, with a
  small part of the query language (conditions on `key`, `title`, `status`
  and `owner` joined by `AND`, `currentUser()`, `ORDER BY`), and posts signed
  deliveries; a contract test holds both to `api/exciter/openapi.json`,
  which `make exciter-openapi REF=<sha>` vendors and `api/exciter/SOURCE`
  pins.
## Connecting an account with one click

Where Armature lists `oauth.partner-tokens@1` at `/meta`, a person connects
their Armature account with **Connect Armature** instead of pasting a personal
access token (#428). Stator is a connected app of Armature, as Armature's
`docs/oauth.md` sets it out; the token form stays, under "Use a token
instead", and is all an Armature without the capability is offered.

- **Discovery.** Every address comes from
  `{base}/.well-known/oauth-authorization-server`; its issuer has to be the
  address the site connected, and it has to offer PKCE with `S256`. Each api
  process keeps it for `armature.MetadataTTL`, five minutes.
- **Registration.** The first person to connect registers Stator at the
  registration endpoint (RFC 7591), with the return address
  `{STATOR_APP_URL}/api/v1/armature/oauth/callback`, the scopes
  `read issue.write` and HTTP Basic at the token endpoint. Armature holds the
  app pending until one of its administrators allows it, which happens on the
  consent page when an Armature administrator connects their own account; a
  member meets "Stator is not connected to Armature yet" there, and Stator
  says so beside the button meanwhile. An advisory lock keeps two people from
  registering it twice.
- **Registration closed.** An Armature that registers apps only by hand
  answers 403 `access_denied`, or names no registration endpoint. Stator keeps
  Armature's sentence and shows it to members, who keep the token form, and
  to administrators under Settings, Armature, Connected app, where they paste
  the client id and secret an Armature administrator made under Settings,
  Connected apps (`PUT /armature/connection/app`). A new client id forgets
  every grant of the old one; `DELETE /armature/connection/app` forgets the
  app and its grants, and the next person to connect registers Stator again.
- **The trip.** `GET /api/v1/armature/oauth/start?return=<path>` needs a
  session. It keeps the digest of a 256-bit state, the person, the session,
  the PKCE verifier and the way back in `armature_oauth_attempt` for ten
  minutes, and sends the browser to the consent page. The callback answers
  only the session that started the trip, once, and only an `iss` equal to
  the issuer (RFC 9207); it redeems the code with the verifier, asks
  `/auth/me` who the grant acts as, holds it to the connected installation as
  a token is held, and lands on the profile with `armature` naming the
  outcome (`connected`, `denied`, `expired`, `ended`, `refused`,
  `unavailable`, `unreachable`, `other_installation`, `not_configured`,
  `failed`). Connected with a way back, the profile goes on to it at once.
  The way back follows the sign-in's rules: a path of this application only.
- **The grant.** One row per person in `armature_grant`: both tokens sealed
  with `STATOR_SECRET_KEY`, bound to the site, the person and which token it
  is, their expiries and the scopes Armature granted. A grant replaces a
  pasted token and a pasted token a grant, so each person has one. Every call
  Stator makes as the person, the worker's link sync included, uses the grant
  when there is one and the token otherwise.
- **Refreshing.** An access token running out within `armature.RefreshAhead`
  is renewed before the call, and a call answered 401 is renewed once and
  sent again. Armature ends a grant whose rotated refresh token is presented
  twice, so the renewal runs under `SELECT ... FOR UPDATE` on the person's
  row; a second request waiting there finds the new tokens and uses them.
  `invalid_grant` means the grant ended in Armature (disconnected there, the
  app removed, the person switched off): the account says so as `rejected`,
  in a sentence that asks to connect again.
- **Disconnecting** forgets the grant and revokes its refresh token at
  Armature's revocation endpoint (RFC 7009), which ends it there too; an
  Armature that does not answer is logged, and the grant ends there unused.
- **The database.** `armature_app` is the administrators' alone; what a
  member's connect needs of it the service reads as `stator_admin`, so the
  sealed secret stays behind. A person reads and changes only their own grant
  and trip. A new Armature address forgets the app and every grant with the
  tokens.
- **Scopes are asked for all at once.** Armature keeps one grant per person
  and app, and a new consent, a token exchange or a refresh naming fewer
  scopes replaces the grant's scopes with the narrower set
  (Cloudster1/Armature#298). So every consent asks for `armature.AppScopes`
  whole (`read`, and `issue.write`, which linking a page to an issue takes),
  a refresh names no scope, and an exchange of a Keycloak token (#430) is for
  a person who holds no grant yet. An administrator's grant carries
  `webhooks.partner` as well, below: their consent asks for
  `armature.AdminScopes` whole, and so does every later connect of a person
  whose grant holds it, so it is never narrowed away.

## Connecting Armature in one step

Where Armature lists `webhooks.partner@1` beside `oauth.partner-tokens@1`,
an administrator connects Armature with **Connect** under Settings, Armature,
and Armature makes the webhook to Stator itself (#429), as Armature's
`docs/oauth.md` sets out under "The partner's own webhook". No secret is
copied by hand. The pasted secret stays, under "Paste a webhook secret
instead", and is all an Armature without the capability is offered.

- **One consent.** `GET /api/v1/armature/connection/webhook/connect` is for
  administrators with a session. It is the trip above with the purpose
  `webhook` in `armature_oauth_attempt`, which only an administrator may
  store, and it asks for `read issue.write webhooks.partner` at once. Stator
  registers itself with those scopes where Armature offers the partner
  webhook. Armature's consent page refuses `webhooks.partner` to somebody who
  does not administer Armature; an app registered by hand without it comes
  back `invalid_scope`, said as `scope_refused`. The grant is the
  administrator's own, so their account is connected on the way.
- **The webhook.** With that grant Stator posts `POST /webhooks/partner` with
  its receiver's address, the topics the receiver acts on
  (`armature.WebhookTopics`) and version 1, and stores the secret Armature
  answers sealed, as a pasted one is. A webhook Armature has already (409
  `partner_webhook_exists`) is read, brought to that address, those topics
  and version and turned on with `PATCH`, and given a new secret, since the
  old one is not known to be the one stored. One that is gone between two
  calls (null on `GET`, 404 on `PATCH` or on rotating) is made again.
- **The address Armature posts to** is `{STATOR_APP_URL}/api/v1/armature/webhook`,
  or `STATOR_ARMATURE_WEBHOOK_URL` where Armature reaches Stator under
  another name, such as the stack's `http://api:8080/api/v1/armature/webhook`.
  It is what the settings page shows under Webhook address either way.
- **Armature refusing.** Its outbound guard (422 `outbound_refused`), an
  address that does not resolve there (422 `invalid`) and a name taken by an
  administrator's webhook (409 `conflict`) are kept in
  `armature_connection.webhook_refusal` and said on the settings page in
  Armature's own words, below "Armature cannot reach Stator": the value to
  allow is in the sentence, such as adding stator.example.com to
  `network.outboundAllow` (`ARMATURE_OUTBOUND_ALLOW`). 403
  `insufficient_scope` or `forbidden`, and a grant that ended, are said as
  `webhook_admin`: an Armature administrator must connect again.
- **Afterwards.** `POST /armature/connection/webhook/test` has Armature post a
  ping now and answers what the receiver said; `POST
  /armature/connection/webhook/rotate-secret` gives the webhook a new secret
  and stores it. Both use the caller's own grant, which must hold
  `webhooks.partner` (409 `armature_webhook_admin` otherwise), and make a
  webhook Armature lost again. `partnerWebhook` in `GET /armature/connection`
  says whether Armature offers it, whether the stored secret is of Stator's
  own webhook, Armature's last refusal and whether the caller may manage it.
- **Leaving.** A new address or Disconnect removes the webhook with
  `DELETE /webhooks/partner` first, with the caller's grant; where that
  cannot be done it is logged and stays in Armature, where an Armature
  administrator removes it under Settings, Webhooks. The secret is forgotten
  here either way: the database drops it with the address
  (`armature_connection_moved`), and holds that a webhook marked as Stator's
  has a secret.
- **The page** says what members do next once connected, with Connect
  Armature for the administrator's own account where it is not connected yet.

## Connecting people at sign-in

Where Stator and Armature sign people in through the same Keycloak realm
(`dynamo`), Stator connects each person to Armature when they sign in, with
no click and no token (#430), as Armature's `docs/oauth.md` sets out under
"A signed-in person, with no click".

- **When.** Only where the operator turns it on, with
  `STATOR_ARMATURE_TOKEN_EXCHANGE=true` (chart `armature.tokenExchange`), and
  only where Armature lists `oauth.token-exchange@1` at `/meta`. Off, or
  against an Armature without it, nothing is asked at sign-in.
- **The exchange.** After a sign-in through the identity provider, the api
  posts the person's Keycloak access token to Armature's token endpoint as
  Stator's connected app, authenticated with the app's client id and secret:
  `grant_type=urn:ietf:params:oauth:grant-type:token-exchange`,
  `subject_token=<the access token>`,
  `subject_token_type=urn:ietf:params:oauth:token-type:access_token`,
  `scope=read issue.write`. The answer is a full grant, kept and refreshed as
  one from the consent page is, after `/auth/me` holds it to the connected
  installation. The access token itself is never stored.
- **Who is asked for.** A member, not a guest, with no live link: a grant
  that is not `rejected` and has not run out, or a pasted token that is not
  `rejected`, is kept, so a person connected by hand or by Connect keeps that
  connection. A missing or dead one is filled in.
- **Never in the way.** The sign-in goes on whatever Armature answers, within
  `armature.SignInTimeout` (ten seconds) at most. A refusal is kept in
  `armature_sign_in`, in Armature's own words where it gave some, such as
  "Ada Lovelace has no Armature account yet. Sign in to Armature once, or ask
  an administrator to let them in." or an app without a Keycloak client
  (`unauthorized_client`). `GET /armature/account` answers it as
  `app.signInRefusal`, and the profile says it above Connect Armature and the
  token form. Storing a grant or a token forgets it. A person reads only
  their own.
- **What it needs.** In Armature, Stator's app (registered by an Armature
  administrator, or by Stator and allowed) with its Keycloak client named,
  `stator` in the realm (`keycloakClientId` under Settings, Connected apps);
  in Stator, that app's client id and secret under Settings, Armature,
  Connected app (`PUT /armature/connection/app`), unless Stator registered
  itself; in Keycloak, an audience mapper on Stator's client that adds
  Armature's client id (`armature`) to Stator's access tokens, since Armature
  takes only a token whose `aud` holds its own client. Keycloak's own token
  exchange is not switched on.

## Handing partner tools a person's token

Stator is the authorization server a partner tool of the same Keycloak realm
(`dynamo`) exchanges a person's sign-in at, so the person is connected to
Stator in the partner without a click (#461). It follows Armature's
`docs/oauth.md`, "A signed-in person, with no click", so a partner written
for one answers the other.

- **Partner apps** are registered by an administrator:
  `GET /oauth/clients` (`{clients}`), `POST /oauth/clients` with
  `{name, scopes, keycloakClientId}` (201 `{client}` with `secret`,
  `stator_ocs_` and 43 characters, shown this once; 422 on each field),
  `PATCH /oauth/clients/{clientID}` (what is left out keeps its value; a new
  name follows into the grants' names, narrower scopes narrow every grant at
  once) and `DELETE /oauth/clients/{clientID}` (204; every grant ends). The
  scopes are `read`, which every app holds, and `write`: a grant holding
  `read` alone may only read, one holding `write` may do whatever its person
  may. One app per Keycloak client. The secret is kept as its SHA-256 only.
- **The exchange.** `POST /api/v1/oauth/token`, a form, the app
  authenticating by HTTP Basic (id and secret form-encoded first) or with
  `client_id` and `client_secret` in the form, never both:
  `grant_type=urn:ietf:params:oauth:grant-type:token-exchange`,
  `subject_token=<the access token>`,
  `subject_token_type=urn:ietf:params:oauth:token-type:access_token`, and
  optionally `scope`, at most the app's, `read` among them. Stator verifies
  the token against its identity provider's keys: its issuer, its expiry,
  Stator's client id in `aud` (an audience mapper on the partner's Keycloak
  client adds it) and the app's `keycloakClientId` as `azp`. Keycloak's own
  token exchange is not switched on.
- **The person** is found by the token's subject or its verified address, as
  a sign-in finds them, and their groups are kept as the token says. A
  member is answered; somebody who is not a member yet is let in only by the
  group mapping under Settings, Single sign-on, as at sign-in, which lets a
  mapped group's people in already. Anybody else, a guest or somebody
  switched off is `invalid_grant`: "Ada Lovelace has no Stator account yet.
  Sign in to Stator once, or ask an administrator to let them in.", and
  nothing about them is kept.
- **The answer** is `{access_token, token_type: "Bearer", expires_in: 3600,
  refresh_token, scope, issued_token_type}`, with `Cache-Control: no-store`.
  The access token, `stator_oat_`, lives an hour and is a bearer for every
  operation the person's own personal access token may call; the refresh
  token, `stator_ort_`, lives 90 days from its last use. One grant per
  person and app: a new exchange replaces its tokens and adds the scopes
  asked to the ones it held.
- **Refreshing** is `grant_type=refresh_token&refresh_token=...[&scope=...]`.
  Both tokens rotate on each use; a narrower `scope` narrows the grant for
  good. A rotated refresh token presented again ends the grant (RFC 9700
  section 4.14.2), so a partner refreshes one grant in one place at a time.
- **The person's token list** shows each grant as "Exciter (connected
  app)", with `clientId` naming the app and `expiresAt` when its refresh
  token runs out unless used, marked as a connected app on the tokens page.
  Revoking it there, or an administrator under the organization's tokens,
  ends it and its refresh token. `POST /api/v1/oauth/revoke` with
  `token=...` (RFC 7009) is the partner's own Disconnect: 200 for every
  token, known or not.
- **Refusals** are OAuth's shape, `{"error", "error_description"}`:
  `invalid_request`, `invalid_client` (401, with `WWW-Authenticate: Basic`
  when Basic was used), `invalid_grant` ("This connection has ended. Connect
  Stator again." for a refresh token that is no more), `invalid_scope`,
  `unauthorized_client` (single sign-on not set up), `unsupported_grant_type`.
- **Discovery.** `GET /.well-known/oauth-authorization-server` at the origin
  (RFC 8414), cacheable for five minutes, names `STATOR_APP_URL` as the
  issuer, the token and revocation endpoints, the scopes, the two grants and
  the two ways to authenticate. nginx, the chart's nginx and its Ingress send
  it to the api.
- **The database.** `oauth_client` is the administrators' alone; the token
  endpoint reads it as `stator_admin`. A grant is the person's `api_token`
  row with `client_id`, `refresh_hash` and `access_expires_at`; only
  `stator_admin` may make or change one, so a member cannot forge a grant
  with raw SQL, and the database holds that a grant has both tokens and an
  end. The audit log records `oauth.client_created`, `oauth.client_updated`,
  `oauth.client_removed` and `oauth.grant_created`.
- **Beside the apps that connect to MCP.** A partner app's grant stays a
  bearer for every operation, as above; the apps of the next section and
  their grants are another kind, listed by `GET /oauth/clients` too with
  `kind` `registered` beside the partners' `partner`.

## Connecting an app to MCP with OAuth

An app such as Rotor, or an AI assistant on somebody's machine, connects to
Stator's MCP endpoint, `/api/v1/mcp`, as a person (#449). It follows the MCP
specification's authorization: the endpoint is an OAuth 2.1 protected
resource and Stator its authorization server. The rules are Armature's
`docs/oauth.md` and Exciter's #188, so a client written for one connects to
the others; what Stator protects is its MCP endpoint alone.

- **Discovery.** A request to `/api/v1/mcp` without a token, or with one that
  is refused, answers 401 with
  `WWW-Authenticate: Bearer resource_metadata="{issuer}/.well-known/oauth-protected-resource/api/v1/mcp", scope="read write"`,
  and `error="invalid_token"` added when a token was sent. That document
  (RFC 9728, also at the bare `/.well-known/oauth-protected-resource`) names
  the resource, `{issuer}/api/v1/mcp`, Stator as its authorization server,
  the scopes and `bearer_methods_supported: ["header"]`. The server's
  metadata (RFC 8414) names the `authorization_endpoint`
  (`/api/v1/oauth/authorize`), the token, revocation and
  `registration_endpoint`s, `response_types_supported: ["code"]`,
  `code_challenge_methods_supported: ["S256"]`, the grants
  `authorization_code`, `refresh_token` and the token exchange,
  `none`, `client_secret_basic` and `client_secret_post`, and
  `authorization_response_iss_parameter_supported: true`. The issuer is
  `STATOR_APP_URL`; nginx, the chart's nginx and its Ingress send every
  `/.well-known/oauth-` document to the api.
- **Registration** is `POST /api/v1/oauth/register` (RFC 7591), open to
  anybody, at most 10 from one address in 15 minutes (429 beyond). It takes
  `client_name` (1 to 60 characters), `redirect_uris` (1 to 5: `https`, or
  `http` on `localhost`, `127.0.0.1` or `[::1]` at any port; no fragment),
  optionally `client_uri`, `scope` (`read`, or `read write`; both when left
  out, the person narrows them), `token_endpoint_auth_method` (`none` for a
  public app, `client_secret_basic` by default, or `client_secret_post`),
  `grant_types` (`authorization_code` among them; `refresh_token` and
  `urn:ietf:params:oauth:grant-type:token-exchange` are accepted beside it,
  the exchange narrowing a token of the app's own grant, below) and `response_types`
  (`code`). 201 answers them with `client_id`, `client_id_issued_at` and a
  `client_secret` (`stator_ocs_`, shown once) unless the app is public.
  Refusals are 400 `invalid_redirect_uri` or `invalid_client_metadata`.
- **Pending until an administrator allows it.** A new app is `pending`:
  nothing it asks for works, the token endpoint refuses it as
  `invalid_client`, and a member who tries to connect it is told "A Stator
  administrator must allow Rotor first. Ask one to connect it once; then you
  can connect it too." An administrator's own consent allows it (`allowed`,
  recorded as `oauth.client_allowed`), after which members connect it. An
  app nobody allowed within 24 hours is removed, by the worker's sweep. A
  request to a pending app is never redirected back to it, so its return
  address cannot borrow Stator's for a redirect.
- **The authorization request.** The app sends the browser to
  `GET /api/v1/oauth/authorize?response_type=code&client_id=...&redirect_uri=...&state=...&code_challenge=...&code_challenge_method=S256[&scope=read write][&resource={issuer}/api/v1/mcp]`.
  Stator sends the browser on to the consent page, `/oauth/authorize` with the
  query whole, through the sign-in when nobody is signed in. An unknown
  `client_id` or a return address the app did not register is shown as a
  sentence and never redirected to; a loopback address matches at any port.
  Everything else wrong is answered at the return address with `error`,
  `error_description`, `state` and `iss`: `unsupported_response_type`,
  `invalid_request` (no `state`, no `S256` challenge), `invalid_scope`
  (`read` is among the scopes asked, and nothing beyond the app's) and
  `invalid_target` (a `resource` that is not the MCP endpoint, with or
  without a trailing slash).
- **The consent page** (`GET /api/v1/oauth/consent?<the query>`, from a
  session) shows the app's name and address, the host the browser goes back
  to, and what it asks: `read`, reading the pages the person can see, and
  `write`, changing them as the person. The person may allow `read` alone
  where both were asked, and limit the app to spaces they can see, at most
  50, as a personal token is limited. `POST /api/v1/oauth/consent` with
  `{query, decision: "allow" | "deny", scope?, spaces?}` answers
  `{redirectTo}`: the return address with `code` (`stator_oac_`, good once
  for a minute), `state` and `iss` (RFC 9207), or `error=access_denied`. A
  guest cannot connect an app. Every consent is recorded as
  `oauth.grant_consented`.
- **The token** is `POST /api/v1/oauth/token` with
  `grant_type=authorization_code`, `code`, `redirect_uri`, `code_verifier` and
  optionally `resource`, a public app sending its `client_id` alone and a
  confidential one authenticating as above. The answer is
  `{access_token, token_type: "Bearer", expires_in: 3600, refresh_token, scope}`.
  A code is good once: presented again it ends the grant it made. A wrong
  verifier, another return address or another app is `invalid_grant`.
- **One grant per person and app**, made afresh at each consent, replacing an
  earlier one. It is the person's `api_token` row, "Rotor (connected app)" in
  their token list, revoked there like any token. It acts as the person,
  never beyond their rights, held to `read` where only reading was allowed and
  to the spaces chosen by the same rules, in the database too, as a personal
  token limited to spaces.
- **At the MCP endpoint alone.** The access token (`stator_oat_`) is a bearer
  at `POST /api/v1/mcp` and for the calls MCP makes in the same process; on
  any other route it is 401 `mcp_only_token` with
  `WWW-Authenticate: Bearer error="invalid_token"`. A partner app's token of
  the previous section is not held so.
- **Refreshing** rotates both tokens; a rotated refresh token presented again
  ends the grant, and a `resource` sent must be the MCP endpoint
  (`invalid_target` otherwise). `POST /api/v1/oauth/revoke` (RFC 7009) ends a
  grant by either of its tokens.
- **A narrower token for one run**, below, leaves the grant as it is.
- **The database.** `oauth_client` gains `kind`, `status`, `redirect_uris`,
  `client_uri` and `auth_method`, a partner keeping its Keycloak client and
  secret; administrators alone may read it or allow an app, as `stator_app`.
  Codes are in `oauth_code`, which only `stator_admin` reaches. A trigger
  refuses a code or a grant for an app nobody allowed, whoever writes it, and
  a grant for MCP alone is `mcp_only`, which only `stator_admin` may set.

### A narrower token for one run

An app such as Rotor runs workflows in parallel for one person. Each run
trades an access token of the app's own grant for a shorter one that may do
less (#499), so a run holds only what it needs and the runs never share, nor
fight over, the grant's refresh token. It is Armature's exchange of a grant's
token (`docs/oauth.md`) and Exciter's #235.

- **The exchange.** `POST /api/v1/oauth/token`, the app authenticating as for
  a refresh: `grant_type=urn:ietf:params:oauth:grant-type:token-exchange`,
  `subject_token=<a stator_oat_ access token of the app's own live grant>`,
  `subject_token_type=urn:ietf:params:oauth:token-type:access_token`,
  optionally `requested_token_type` (the same), `resource` (the MCP endpoint,
  for an app that registered itself), and what narrows it:
  - `scope`: `read` from a grant that writes; `read write` or none keeps the
    grant's. Anything wider is `invalid_scope`.
  - `stator_spaces`: space keys, separated by spaces, at most 50, each a space
    the grant reaches and the person may see; an unknown key and a hidden one
    are refused alike, `invalid_scope`. None keeps the grant's spaces.
  - `stator_expires_in`: 1 to 3600 seconds (`invalid_request` otherwise). The
    token never outlives its subject: it lasts what is asked or what the
    subject has left, whichever is shorter, and what the subject has left
    when nothing is asked.
- **The answer** is `{access_token, token_type: "Bearer", expires_in, scope,
  issued_token_type}` with `Cache-Control: no-store` and no refresh token.
  The grant, its scope, its spaces and its own tokens are untouched.
- **The subject** is a live access token of the calling app's own grant,
  whose person is still a member; another app's token, a token exchanged
  before or one that ran out is `invalid_grant`. Any other subject goes on
  to the partner exchange above, unchanged.
- **What it does.** It acts as its grant narrowed: `read` where it or the
  grant only reads, the spaces it names that the grant still reaches, at the
  MCP endpoint alone when the grant is, and until its own end. A grant
  narrowed later, by a refresh or by its app's scopes, narrows it too.
- **Its end.** `POST /api/v1/oauth/revoke` with the token ends it alone; a
  refresh of the grant leaves it; ending the grant (revocation by either of
  its tokens, the person or an administrator revoking it, a code or refresh
  token presented twice, a new consent, the app removed) ends every token
  exchanged from it.
- **The database.** Exchanged tokens are `oauth_exchanged_token` rows, their
  SHA-256 with the grant, scope, spaces and end, reached by `stator_admin`
  alone. A trigger refuses one that outlives the grant's access token or
  belongs to no connected app's grant, whoever writes it, and the worker's
  sweep removes those that ran out.
## Connecting people to Exciter at sign-in

As toward Armature above, Stator connects each person to Exciter when they
sign in through the shared Keycloak realm, so nobody pastes an
`exciter_pat_` (#462). Exciter's side is Cloudster1/Exciter#273.

- **When.** Only where the operator turns it on, with
  `STATOR_EXCITER_TOKEN_EXCHANGE=true` (chart `exciter.tokenExchange`), only
  where Exciter lists `oauth.token-exchange@1` at `/meta`, and only once a
  Stator administrator stored the partner app.
- **The app.** An Exciter administrator registers Stator as a partner app
  with its Keycloak client, `stator` in the realm; a Stator administrator
  stores its client id and secret (`exciter_ocs_`) under Settings, Exciter,
  Connecting at sign-in: `PUT /exciter/connection/app` with
  `{clientId, clientSecret}`, `DELETE /exciter/connection/app`. The secret is
  sealed and never answered; `GET /exciter/connection` says under `app`
  whether one is stored, whether Exciter offers the exchange and whether
  people are connected at sign-in. A new client id, removing the app, a new
  address or Disconnect end every grant of it.
- **The exchange.** After a sign-in, the api posts the person's Keycloak
  access token to `{exciter}/api/v1/oauth/token` as the app, by HTTP Basic:
  `grant_type=urn:ietf:params:oauth:grant-type:token-exchange`,
  `subject_token=<the access token>`,
  `subject_token_type=urn:ietf:params:oauth:token-type:access_token`, no
  scope, so the app's own. The answer's access and refresh tokens are kept
  sealed in the person's `exciter_token` row, which `GET /exciter/account`
  then answers with `via: "sign_in"`. Every read as the person uses it,
  renewed before it runs out and once after a 401, under a lock on the row,
  since a refresh token presented twice ends the grant in Exciter;
  `invalid_grant` marks it `rejected`. Disconnect revokes it at
  `{exciter}/api/v1/oauth/revoke`.
- **Who is asked for.** A member, not a guest, without a token Exciter has
  not refused: a pasted token is kept, and a missing or refused one is
  filled in. A pasted token replaces a grant.
- **Never in the way.** The sign-in goes on whatever Exciter answers,
  within ten seconds at most. A refusal is kept in `exciter_sign_in`, in
  Exciter's own words where it gave some, answered as `signInRefusal`, and
  said on the profile above the token form, which stays the fallback.
- **In Keycloak,** an audience mapper on Stator's client adds Exciter's
  client id (`exciter`) to Stator's access tokens.
- **The webhook address.** `STATOR_EXCITER_WEBHOOK_URL` (chart
  `exciter.webhookUrl`) is the receiver address the settings page names for
  Exciter's webhook, as `STATOR_ARMATURE_WEBHOOK_URL` is for Armature's, so a
  cluster can give Exciter an in-network address such as
  `http://stator-api.stator.svc:8080/api/v1/exciter/webhook`; empty is
  `STATOR_APP_URL` with `/api/v1/exciter/webhook`.

## The rules

The names and what each promises are kept in one list in Dynamo,
`docs/compatibility.md` of
[praetorianer777/Dynamo](https://github.com/praetorianer777/Dynamo) (Dynamo#4,
not written yet). A tool may only report names from it; this page follows it
once it is there, and a new name is added there first. A breaking change
raises the number (`page-links@2`), and the old name is still reported beside
the new one for at least one Dynamo release.
