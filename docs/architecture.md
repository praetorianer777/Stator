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
- An operation agreed before it is built carries `pending: true` in the table:
  the router answers it 501 `not_implemented` and the integration suite does
  not expect it covered. Whoever builds it removes the mark and routes its
  handler; `docs/api-contract-m1.md`, `docs/api-contract-m2.md` and
  `docs/api-contract-m3.md` are the agreements behind them.
- Migrations: goose, embedded in the binary, applied by `cmd/migrate`.
- Configuration: environment variables only, prefix `STATOR_`.
- Observability: `log/slog`, Prometheus metrics, OpenTelemetry traces.

### Multi-tenancy

Every table carries an organisation and a row-level security policy keyed on
`current_org_id()`, which each transaction sets. The application connects as
`stator_app`, which cannot bypass RLS; migrations run as `stator_admin`.
Each transaction of a request also names the person it acts for, read by
`current_actor_id()`, and restrictive policies hold `stator_app` to that
person's space permissions and page restrictions (see `docs/decisions.md`).

### Reads, writes and replicas

`internal/db` sends every write to the primary and every read to a replica
when one is fit to serve it, otherwise to the primary. Fitness is judged on
the connection the read actually gets, with one query
(`pg_is_in_recovery()`, `pg_last_wal_replay_lsn()`, the replay lag and the
WAL receiver's state), because behind a load balanced read service two
connections of one pool can reach two replicas at different positions. A
connection that lags more than `STATOR_DB_MAX_REPLICA_LAG` or has not
replayed the caller's last write is handed back and the read goes to the
primary; falling back is always correct, only dearer.

A health loop samples `STATOR_DB_REPLICA_LAG_SAMPLES` connections of each
replica pool every `STATOR_DB_HEALTH_INTERVAL` and takes the pool out of the
rotation when none of them is fit, so a broken replica stops costing each
read a round trip.

Read-your-writes: `Cluster.Write` returns the WAL position past its commit,
or, for a refused write, the primary's position at the refusal. Handlers hand it to `noteWrite`, which records it in Valkey
(`internal/freshness`, `STATOR_VALKEY_URL`) under the caller's key for
`STATOR_READ_YOUR_WRITES_TTL`. On the caller's next request the
`readYourWrites` middleware pins the request to that position with
`db.PinLSN`, so no replica short of it serves them. The key is the session,
else the personal access token, else a `stator_client` cookie; see
`docs/decisions.md`. Without `STATOR_VALKEY_URL` the positions stay in the
process, which is only right for a single api process. `/readyz` and
`/metrics` count reads by where they went and why they fell back.

### Domains

| Package | Responsibility |
|---|---|
| `auth` | sessions, argon2 passwords, personal access tokens |
| `oidc` | OIDC relying party per organisation, group sync |
| `perm` | global, space and page permissions |
| `space` | spaces, space settings |
| `document` | page document allowlist and validation, plain text for search, headings for the table of contents |
| `page` | page tree (parent plus rank), move, copy, trash, drafts, published versions, diff, restore, restrictions, owners and verification, and the worker's watch on verifications that run out |
| `version` | which build is running |
| `comment` | page comments, inline comments anchored by mark id |
| `reaction` | emoji reactions on pages and comments |
| `label`, `watch`, `notify` | labels, watchers, in-app and email notifications |
| `star`, `home` | starred pages and spaces, the home page's updates and edits |
| `stale` | the stale content report: pages nobody published or opened for a while, for the administrators of their spaces |
| `keyset` | the cursor a list ordered by time hands out for its next window |
| `template` | page templates |
| `search` | PostgreSQL full-text search (`tsvector`, GIN, `websearch_to_tsquery`) |
| `attachment` | uploads to S3-compatible storage |
| `markdown` | a document as Markdown and Markdown as a document, held to the allowlist |
| `mdio` | Markdown import and export of pages, subtrees and their files, through the page and file services |
| `theme` | custom themes in the `armature-theme/1` format |
| `armature` | Armature client: issues, queries, issue creation, link sync |
| `events` | transactional outbox, drained by the worker |
| `audit` | the organization's audit log: entries written with their act, read and exported by administrators, pruned by the worker |
| `mail` | plain text mail over an SMTP relay, as in Armature |
| `netguard` | SSRF guard for every outbound request |

## Frontend (`web/`)

- React 19, TypeScript, Vite, TanStack Router and TanStack Query.
- API client: openapi-fetch, typed by `web/src/api/schema.d.ts`, generated
  from `api/openapi.json`.
- Tailwind v4, CSS-first. The `@theme` tokens are Armature's, value for value,
  so both products look alike; custom themes compile to CSS variables on top.
- Editor: TipTap 3. Documents are stored as ProseMirror JSON and validated
  server-side against an allowlist of nodes and marks. The allowlist is one
  Go table; `make document-allowlist` writes it to
  `api/document-allowlist.json`, which a vitest test holds the editor to.
  The editor arrives with the route that edits, through the router's
  `lazy()`, so a reader never downloads it.
- Addresses: a space is `/s/{spaceKey}`, a page `/s/{spaceKey}/p/{pageId}/{slug}`.
  Only the id finds a page; the slug is for people and is put right when stale.

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
- **Freshness.** Answers are cached per person in Valkey for a minute, and
  Armature webhooks, verified by HMAC signature, invalidate them sooner.
- **Reaching out.** Every outbound call passes `netguard`, which refuses
  addresses inside the network unless `STATOR_OUTBOUND_ALLOW` names them.
- **Themes.** Armature theme files import unchanged, and a user can follow
  their active Armature theme.

`docs/api-contract-m3.md` says how each of these works.

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
`worker`, `web`, and `armature-stub` in Armature's place, which only the
stack lets the SSRF guard through to. Every service has a health check and the dependencies
wait on them, so `docker compose up --wait` returns once the stack answers.
The primary runs with `synchronous_commit=off`: a commit that waited for the
host's disk could take seconds on a busy machine. Only this stack does so; see
`docs/decisions.md`.

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

### Kubernetes

`deploy/charts/stator` is the Helm chart, modelled on Armature's. It runs the
api, the worker and the web behind one Ingress host, with a ConfigMap of the
settings every workload shares and the credentials by reference from Secrets.
A hook Job generates the database and Valkey passwords once, as basic-auth
Secrets; the roles Job makes `stator_app` and `stator_admin`; the migrate Job
applies the migrations as the schema's owner; the seed Job is for
development only. A ServiceMonitor scrapes the api's metrics port.

With `cnpg.enabled` the chart renders a CloudNativePG `Cluster` from
`cnpg.spec` (instances, storage, resources, backup and anything else the
operator takes), and the operator creates the two runtime roles. CNPG gives
the cluster two services:

- `<cluster>-rw` always points at the primary. `STATOR_DB_PRIMARY_URL` and
  `STATOR_DB_ADMIN_URL` name it, and so does the migrate Job.
- `<cluster>-ro` balances connections across the replicas.
  `STATOR_DB_REPLICA_URLS` names it when `cnpg.readReplicas` is on and there
  is more than one instance; otherwise it is empty and reads go to `-rw`.

Behind `-ro` one pool's connections land on different replicas, which is why
the api judges replay position and lag on each read's own connection rather
than per pool (see "Reads, writes and replicas" above). The write pool and
each read pool are sized apart (`STATOR_DB_PRIMARY_MAX_CONNS`,
`STATOR_DB_REPLICA_MAX_CONNS`), and `/metrics` reports each pool's
connections taken, the time spent waiting for one, and each read pool's
fallbacks to the primary by reason.

For a trial, `values-demo.yaml` brings one Postgres pod and one Valkey pod of
the chart's own. The chart refuses to render with both `cnpg.enabled` and
`postgresql.enabled`, and refuses replicas behind more than one api pod
without a Valkey they share. `tests/test-helm.sh` runs `helm lint` and
`helm template` in a container for each layout and checks the rendered URLs.

The browser suite (`e2e/`, `mk/e2e.mk`) runs in Microsoft's Playwright image
of the version `e2e/package.json` pins, on the host's network, and reaches the
stack on its published ports: the session cookie and the sign-in redirects are
bound to `http://localhost:$WEB_PORT`, so the browser has to see what a person
sees. The container's `/tmp`, which holds the browser profiles, their caches
and the videos being recorded, is a tmpfs: on the host's disk, other writers
could freeze every browser for seconds. A setup project signs alice and bob in
once and stores their sessions under `e2e/.auth/`, and leaves `demo` showing the built-in theme, as does a
teardown project after the last spec; specs tagged `@auth` skip while the
stack cannot sign anyone in. Chromium runs at desktop size and at 360x740.
What no endpoint makes yet, such as an organization whose provider is down,
a spec arranges straight in the database through `e2e/fixtures/db.ts`, with
the superuser URL `.cache/stack.env` names, and removes again.

A spec that changes what an organization shows uses `orgTest` from
`e2e/fixtures/org.ts` instead of `demo`: each spec file gets an organization
of its own per worker, made through the api's test endpoints (on in compose
through `STATOR_TEST_ENDPOINTS`, never in the chart), with alice and bob
signed in to it, and removed with all its rows and files when the worker
stops. See `docs/decisions.md`.

A Helm chart follows later.
