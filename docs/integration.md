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
  "capabilities": ["page-links@1", "theme-export@1", "webhook-receiver@1", "webhooks@1"]
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

### `webhook-receiver@1`

Armature's signed webhook deliveries, envelope version 1, received for the
issue topics, so pages show a change to an issue at once.

- `POST /armature/webhook`
- acts on `issue.created`, `issue.updated`, `issue.transitioned` and
  `comment.added`

### `webhooks@1`

Endpoints subscribed to topics, their deliveries, a redelivery, a test
delivery and a new secret, each delivery a signed envelope (`id`, `topic`,
`occurredAt`, `payload`, signed as `sha256=` and the hex HMAC-SHA256 of the
body in `X-Stator-Signature-256`).

- `GET /webhooks`, `POST /webhooks`
- `PATCH /webhooks/{webhookID}`, `DELETE /webhooks/{webhookID}`
- `GET /webhooks/{webhookID}/deliveries`
- `POST /webhooks/{webhookID}/deliveries/{deliveryID}/redeliver`
- `POST /webhooks/{webhookID}/test`, `POST /webhooks/{webhookID}/rotate-secret`
- topics `page.published`, `page.moved`, `page.deleted` and `comment.created`

### `theme-export@1`

A theme exported as one `armature-theme/1` file with its pictures and fonts,
and such a file imported as a theme of the caller's own.

- `GET /themes/{themeID}/export`
- `POST /themes/import`

The list lives in `backend/internal/httpapi/capabilities.go`. A unit test
refuses a capability naming an operation the operations table does not have
or that answers 501, a topic `events.Topics` does not list or one the
receiver does not act on, and a name that is not `name@version` or is listed
twice. `page.remote-links`, remote links on Stator's own pages, comes with
#419.

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

## The rules

The names and what each promises are kept in one list in Dynamo,
`docs/compatibility.md` of
[praetorianer777/Dynamo](https://github.com/praetorianer777/Dynamo) (Dynamo#4,
not written yet). A tool may only report names from it; this page follows it
once it is there, and a new name is added there first. A breaking change
raises the number (`page-links@2`), and the old name is still reported beside
the new one for at least one Dynamo release.
