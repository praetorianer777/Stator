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
  "capabilities": ["page-links@1", "page-remote-links@1", "theme-export@1", "webhook-receiver@1", "webhooks@1"]
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
- **Cache.** Each answer is kept per viewer's token row for at most
  `exciter.RequirementCacheTTL`, 60 seconds, in Valkey, with the keys each
  requirement was read by under its id.
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
  and every key the requirement of its `id` was read by. Its payload, read as
  the webhook's owner, is never shown.
- **The stand-in.** `cmd/exciter-stub` serves `/meta`, `/auth/me` and
  `/requirements/{reqKey}` in Exciter's shapes and posts signed deliveries; a
  contract test holds both to `api/exciter/openapi.json`, which
  `make exciter-openapi REF=<sha>` vendors and `api/exciter/SOURCE` pins.

## The rules

The names and what each promises are kept in one list in Dynamo,
`docs/compatibility.md` of
[praetorianer777/Dynamo](https://github.com/praetorianer777/Dynamo) (Dynamo#4,
not written yet). A tool may only report names from it; this page follows it
once it is there, and a new name is added there first. A breaking change
raises the number (`page-links@2`), and the old name is still reported beside
the new one for at least one Dynamo release.
