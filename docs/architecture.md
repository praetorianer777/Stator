# Architecture

Stator is a team wiki and knowledge base. It deliberately mirrors
[Armature](https://github.com/Cloudster1/Armature): same stack, same patterns,
same design tokens, same identity provider. Someone who knows one codebase
should find their way around the other without a map.

## Overview

```
browser ──> web (nginx, React SPA) ──> api (Go) ──> PostgreSQL (primary + replica)
                                        │  ├──> Valkey (cache, rate limits)
                                        │  ├──> S3-compatible storage (attachments, theme assets)
                                        │  └──> Armature API (as the viewing user)
                                        └── outbox ──> worker (Go) ──> mail, Armature link sync
Keycloak / any OIDC provider <── login ──┘
```

## Backend (`backend/`)

- Module `github.com/praetorianer777/stator/backend`, Go 1.26.
- Binaries in `cmd/`: `api`, `worker`, `migrate`, `seed`, `openapi`.
- One package per domain in `internal/`, each with a `Service`, a `model.go`
  and plain SQL. No ORM, no query generator: pgx v5 and hand-written SQL.
- Router: chi v5. A single route table drives the router, the OpenAPI document
  (`api/openapi.json`), and the consistency tests between them.
- Migrations: goose, embedded in the binary, applied by `cmd/migrate`.
- Configuration: environment variables only, prefix `STATOR_`.
- Observability: `log/slog`, Prometheus metrics, OpenTelemetry traces.

### Multi-tenancy

Every table carries an organisation and a row-level security policy keyed on
`current_org_id()`, which each transaction sets. The application connects as
`stator_app`, which cannot bypass RLS; migrations run as `stator_admin`.
Writes return the LSN so reads can go to the replica without losing
read-your-writes.

### Domains

| Package | Responsibility |
|---|---|
| `auth` | sessions, argon2 passwords, personal access tokens |
| `oidc` | OIDC relying party per organisation, group sync |
| `perm` | global, space and page permissions |
| `space` | spaces, space settings |
| `page` | page tree (parent plus rank), move, copy, trash |
| `version` | drafts, published versions, diff, restore |
| `comment` | page comments, inline comments anchored by mark id |
| `label`, `watch`, `notify` | labels, watchers, in-app and email notifications |
| `template` | page templates |
| `search` | PostgreSQL full-text search (`tsvector`, GIN, `websearch_to_tsquery`) |
| `attachment` | uploads to S3-compatible storage |
| `theme` | custom themes in the `armature-theme/1` format |
| `armature` | Armature client: issues, queries, issue creation, link sync |
| `events` | transactional outbox, drained by the worker |
| `netguard` | SSRF guard for every outbound request |

## Frontend (`web/`)

- React 19, TypeScript, Vite, TanStack Router and TanStack Query.
- API client: openapi-fetch, typed by `web/src/api/schema.d.ts`, generated
  from `api/openapi.json`.
- Tailwind v4, CSS-first. The `@theme` tokens are Armature's, value for value,
  so both products look alike; custom themes compile to CSS variables on top.
- Editor: TipTap 3. Documents are stored as ProseMirror JSON and validated
  server-side against an allowlist of nodes and marks.

## Armature integration

- **Identity.** Both products use the same OIDC provider, so a person is the
  same subject in both.
- **Access.** Each user connects their own Armature personal access token,
  stored encrypted with `STATOR_SECRET_KEY`. Every Armature call is made as the
  viewing user, so a page never shows an issue the viewer could not open in
  Armature.
- **In pages.** Issue keys and URLs become live chips; blocks embed a single
  issue or a table from an NQL query; selected text can become new issues.
- **Back in Armature.** On publish, Stator syncs remote links so an issue lists
  the pages that mention it (Cloudster1/Armature#14).
- **Freshness.** Armature webhooks, verified by HMAC signature, invalidate the
  cached issues.
- **Themes.** Armature theme files import unchanged, and a user can follow
  their active Armature theme.

## Testing

| Layer | Tool | Runs against |
|---|---|---|
| Go unit | `go test` | nothing external |
| Go integration | `go test -tags integration` | the compose stack |
| Web unit | vitest, Testing Library | jsdom |
| End to end | Playwright | the compose stack, Keycloak login, `armature-stub` |

`./run-tests.sh` runs all four; the branch-guard hook runs it before every
push, and CI runs nothing else. Each checkout gets its own compose project and
ports, so parallel worktrees do not collide.

## Deployment

`deploy/` follows Armature's layout. `docker-compose.yml` is the development
and test stack: Postgres 18 as a primary and a streaming replica, Valkey,
SeaweedFS (S3), Mailpit, Keycloak with the `stator-dev` realm
(`deploy/keycloak/realm.json`), the one-shot `migrate` and `seed`, `api`,
`worker`, and `web`. Every service has a health check and the dependencies
wait on them, so `docker compose up --wait` returns once the stack answers.

Two images are built. `Dockerfile.backend` holds every Go binary, stamped with
`VERSION`, and each service picks one by its command. `Dockerfile.web` builds
the SPA with Node and serves it from nginx (`deploy/nginx.conf`), which proxies
`/api/`, `/healthz` and `/readyz` to the api so the browser sees one origin.

`mk/stack.mk` drives the stack. The compose project is `stator-<cksum of the
checkout path>`, and the published ports are a block of twenty from 20000 up,
chosen by the same hash, so worktrees never collide. `make up` and
`make stack-up` write the project name and ports to `.cache/stack.env` for
anything outside the network, such as the browser suite. The integration suite
runs in the Go toolchain container on the stack's network and reaches every
service by name; it writes files to a bucket of its own, `stator-test`, beside
the app's `stator-files`.

A Helm chart follows later.
