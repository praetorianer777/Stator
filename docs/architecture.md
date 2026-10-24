# Architecture

Stator is a team wiki and knowledge base. It deliberately mirrors
[Armature](https://github.com/Cloudster1/Armature): same stack, same patterns,
same design tokens, same identity provider. Someone who knows one codebase
should find their way around the other without a map.

## Overview

```
browser ──> web (nginx, React SPA) ──> api (Go) ──> PostgreSQL (primary + replica)
                                        │  ├──> Valkey (cache, rate limits, shared drafts between api processes; Postgres carries those without it)
                                        │  ├──> S3-compatible storage (attachments, theme assets)
                                        │  ├──> converter (office documents to PDF, for previews)
                                        │  ├──> render (headless Chromium: a page's print view to PDF) ──> web
                                        │  └──> Armature API (as the viewing user)
                                        └── outbox ──> worker (Go) ──> mail, Armature link sync, webhooks, scheduled publishes, example spaces, space and organization exports and imports, Word imports
Keycloak / any OIDC provider <── login ──┘
```

## Backend (`backend/`)

- Module `github.com/praetorianer777/stator/backend`, Go 1.26.
- Binaries in `cmd/`: `api`, `worker`, `migrate`, `seed`, `openapi`.
- One package per domain in `internal/`, each with a `Service`, a `model.go`
  and plain SQL. No ORM, no query generator: pgx v5 and hand-written SQL.
- Router: chi v5. A single route table drives the router, the OpenAPI document
  (`api/openapi.json`), the MCP tools at `POST /api/v1/mcp` (`docs/mcp.md`),
  and the consistency tests between them.
- An operation agreed before it is built carries `pending: true` in the table:
  the router answers it 501 `not_implemented` and the integration suite does
  not expect it covered. Whoever builds it removes the mark and routes its
  handler; `docs/api-contract-m1.md`, `docs/api-contract-m2.md` and
  `docs/api-contract-m3.md` are the agreements behind them.
- Migrations: goose, embedded in the binary, applied by `cmd/migrate`.
- Configuration: environment variables only, prefix `STATOR_`.
- Observability: `log/slog`, Prometheus metrics, OpenTelemetry traces.

### Row level security

An instance serves one organization (#380), whose settings are the one row
of `site`; its key (`site_one_row`) refuses a second. Another organization
runs another instance, with its own database and bucket. The schema names
no organization, so row level security decides only which person reaches
which rows.

- **Two roles.** The api and the worker connect as `stator_app`, which
  cannot bypass row level security. Migrations, the seed and what runs
  before anybody is known (sign-in, the webhook receiver's secret, the
  worker's claims) use `stator_admin`. Every table forces row level
  security, so the owner is bound too, and has a `<table>_admin_bypass`
  policy for `stator_admin`; `user_identity` and `user_session` are read by
  `stator_admin` alone.
- **Permissive `_app` policies, restrictive access policies.** Each table
  has a permissive `<table>_app` policy for `stator_app` that admits every
  row (`site` admits reading and updating its row, `app_user` the people who
  are members). Restrictive policies alone admit nothing; the access
  policies are all `AS RESTRICTIVE` and narrow what the `_app` policy lets
  through: viewers, editors, a row's own person, guests, anonymous readers
  and public links. `TestEveryTableForcesRowSecurity` fails a table missing
  either the bypass or the `_app` policy.
- **Who the transaction acts for.** `db.WithUser` sets `app.user_id`, read
  by `current_actor_id()`, and, when they apply, `app.token_spaces` for a
  token limited to some spaces, `app.anonymous` for a reader who is not
  signed in and `app.page_link` for the digest of a public link's token.
  Naming nobody binds `stator_app` to nothing.
- **What a connected app wrote** (#498). `authenticate` puts the calling
  token or grant on the context with `db.WithVia`, and the transaction sets
  it as `app.via`. A trigger, `via_stamped`, on page versions, comments,
  remote links, attachments, reactions and the audit log stamps `via_kind`
  (`token`, or `oauth` for a grant, a token row with its app's `client_id`),
  `via_id` and `via_name` from the token the acting person holds, read by
  `via_held()`. Nothing else writes them, none changes, and a token revoked
  leaves its name behind; `events.Emit` adds the same to the event, so a
  webhook's actor says it too.
- **The rules are `perm_*` functions.** The policies and the services call
  the same functions (`perm_space_holds`, `perm_page_viewable` and the rest),
  so both answer alike. Those that read the tables they guard are `SECURITY
  DEFINER`, since a policy on `page` that read `page` would recurse, and
  each of those pins `search_path = public, pg_temp`, so neither a caller's
  path nor a restore's empty one changes what a name in its body means. The
  few that only combine others stay plain SQL, which Postgres can inline
  into the policies that call them.
- **Guests, anonymous readers and links.** A guest, a member with the role
  `guest` and the one space they were invited to, is held by the same
  functions to that space and to the people in it, whatever a query forgets
  to ask. An anonymous reader is given view of the spaces the organization
  opened to anybody and nothing else, and a policy on every table keeps it
  to reading spaces, pages and files and to writing nothing. Holding a
  public link's token, the same reader also views the one page a live link
  opens.

Every guard is proven twice in the integration suite: refused through the
api, and refused when the same statement runs straight through SQL as
`stator_app` (see `docs/decisions.md`).

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
| `oauth` | Stator as an OAuth authorization server: partner apps that trade a person's Keycloak token for a grant (#461), and apps that register themselves and connect to the MCP endpoint by a person's consent, by authorization code with PKCE, their tokens good at MCP alone (#449); the worker's sweep of codes and apps nobody allowed |
| `oidc` | the instance's OIDC relying party, group sync |
| `via` | what a row was written through, a personal token or a connected app by its name then, as the database stamps it (#498) |
| `perm` | global, space and page permissions, and the plan of copying one space's permissions onto another |
| `guest` | guests: people from outside invited into one space, and taken out again |
| `public` | reading without signing in: the organization's switches, the spaces, pages and files anybody may read, and the public links that open one page each, with nobody named in them |
| `space` | spaces, space settings |
| `document` | page document allowlist and validation, a sketch's scene and drawing included, plain text for search, headings for the table of contents |
| `page` | page tree (parent plus rank), blog posts outside it by date, move, copy, trash, archive, drafts, the shared draft of a page edited together, live pages and the open version their saves amend, published versions, diff, restore, restrictions, owners and verification, pages made from a template, the people who published a page or a tree, publishes scheduled for a time, and the worker's watches on verifications that run out and on scheduled publishes that came due |
| `collab` | editing together: the WebSocket of a page's shared draft, in y-protocols' framing, its updates stored and passed on unread, awareness, and the bus between api processes: Valkey when configured, else Postgres's LISTEN and NOTIFY, with catching up after a lost connection |
| `version` | which build is running |
| `comment` | page comments, inline comments anchored by mark id |
| `reaction` | emoji reactions on pages and comments |
| `label`, `watch`, `notify` | labels, watchers of pages, spaces and blogs, in-app and email notifications |
| `star`, `home` | starred pages and spaces, the home page's updates and edits |
| `shortcut` | the links a space's administrators pin above its page tree, to pages or web addresses, each shown only to whoever may view its page |
| `task` | the tasks of published pages: each person's list, and the worker's reminder on the due day |
| `calendar` | each space's calendars and their events and absences, which a calendar block draws a month of |
| `stale` | the stale content report: pages nobody published or opened for a while, for the administrators of their spaces |
| `pageview` | page views: each person once a day per page, counted for every reader, named to editors within the retention, pruned into a tally by the worker |
| `share` | sending a page to people and groups who may view it, with a note |
| `keyset` | the cursor a list ordered by time hands out for its next window |
| `template` | page templates: the built-ins, the organization's and each space's own, their variables and filling them in for a new page; and the space templates a new space starts from |
| `example` | the example space that explains Stator: its pages as Markdown per language, made through the other services by the worker, as the administrator who queued it |
| `search` | PostgreSQL full-text search (`tsvector`, GIN, `websearch_to_tsquery`) |
| `attachment` | uploads to S3-compatible storage, each upload of a name its next version, a restore an upload of an earlier one and an annotated picture an upload drawn on one, served whole or by the byte range a video player asks for, and their PDF previews, converted once and kept |
| `convert` | the client of the conversion service that turns office documents into PDF |
| `render` | the client of the render service, which prints a page's print view as PDF; the api hands it a path and, for a signed-in reader, a read-only token made for that one print |
| `markdown` | a document as Markdown and Markdown as a document, held to the allowlist |
| `docx` | a published page as a Word document, written from its document with its pictures inside, as its reader may read it, and a Word document read as a page with its pictures and warnings; `docs/word.md` lists how each block comes out and comes in |
| `wordio` | Word import: one document as a page in the request, several and folders of them by the worker as the importer, through the page and file services |
| `mdio` | Markdown import and export of pages, subtrees and their files, through the page and file services |
| `spaceio` | a whole space exported as an archive or as HTML pages, and an archive, or another wiki's space export, imported as a new space; a whole organization exported as one archive of its spaces' archives and everything else, and imported into an organization without spaces (`docs/organization-transfer.md`); all run by the worker as leased jobs |
| `wikiread` | another wiki's space export, HTML or XML, read into pages, versions, files, labels and comments held to the allowlist, with what did not come across; `docs/wiki-import.md` |
| `theme` | custom themes in the `armature-theme/1` format |
| `armature` | Armature client: issues, queries, issue creation, link sync |
| `events` | transactional outbox, drained by the worker |
| `audit` | the organization's audit log: entries written with their act, read and exported by administrators, pruned by the worker |
| `mail` | plain text mail over an SMTP relay, as in Armature |
| `netguard` | SSRF guard for every outbound request |
| `webhook` | outbound webhooks: endpoints, signed deliveries read as their owner, retries, the delivery log |

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
  `lazy()`, so a reader never downloads it. A sketch is drawn on an
  Excalidraw canvas, bundled with its fonts by the build and loaded with
  the first sketch opened; readers see the SVG it drew, checked by the
  API against `api/sketch-drawing-allowlist.json`, as a picture.
- Editing together: the editor binds the body and title to a Yjs document
  kept in step over a WebSocket (`web/src/features/collab`), with y-protocols'
  awareness for avatars and carets and y-indexeddb for what is written
  offline. What is published is still the person's own draft, saved from the
  shared one, or for a live page the page itself, saved as it is typed and
  followed by its readers every few seconds; see `docs/decisions.md`.
- Addresses: a space is `/s/{spaceKey}`, a page `/s/{spaceKey}/p/{pageId}/{slug}`.
  Only the id finds a page; the slug is for people and is put right when stale.
  A blog post is a page and has a page's address; the space's blog is
  `/s/{spaceKey}/blog`, a month of it `?year=2026&month=10`.
- The pages anybody may read are at `/public`, outside the app's shell and
  its sign-in guard, read through `/api/v1/public`; the document view draws
  them with every link and file through those reads. The addresses before
  #380 named the organization, `/public/{siteSlug}/...`: the api answers its old
  reads with `308` to the new ones when the slug is the instance's, and the
  web client sends an old page address on once the api has said so.
- The render service prints a page from `/print/p/{id}`,
  `/print/public/p/{id}` or `/print/public/link/{token}`: the
  page alone, outside the shell, which marks the document
  `data-print-ready` once nothing is loading or drawing, and states the
  running header and footer in `data-print-meta`.
- A public link opens `/public/link/{token}`, the same reading view for
  that one page without the shell's tree and search, read through
  `/api/v1/public/links/{token}`; its files come through the link too.

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
  their active Armature theme. Exciter's export imports unchanged too, and
  Exciter vendors the token names of `web/src/lib/theme-tokens.ts`, so a
  renamed token needs a note to Exciter (`docs/decisions.md`, #443).
- **Capabilities.** Stator reports what it offers at `/api/v1/meta` and reads
  Armature's there, turning off and naming each feature whose capability
  Armature lacks (`docs/integration.md`).

`docs/api-contract-m3.md` says how each of these works.

## Testing

| Layer | Tool | Runs against |
|---|---|---|
| Go unit | `go test` | nothing external |
| Go integration | `go test -tags integration` | the compose stack |
| Web unit | vitest, Testing Library | jsdom |
| End to end | Playwright | the compose stack, Keycloak login, `armature-stub` |

`./run-tests.sh` runs all four; the branch-guard hook runs the layers a push
can affect before every push (the stack-backed ones only with
`STATOR_GATE=full`), and CI runs nothing else: its jobs run the script side by side, each
for some of the layers, the browser suite in four parts and the integration
suite in three, each part with a stack of its own,
and a last job merges their reports into the run's summary and one
Playwright report. Each checkout gets its own compose project and
ports, so parallel worktrees do not collide.

## Deployment

`deploy/` follows Armature's layout. `docker-compose.yml` is the development
and test stack: Postgres 18 as a primary and a streaming replica, Valkey,
SeaweedFS (S3), Mailpit, Keycloak with the suite's realm `dynamo`
(`deploy/keycloak/realm.json`, the same file Armature and Exciter import;
`tests/test-realm.sh` holds it to Armature's copy at the pinned commit), the converter that turns office documents
into PDF for previews (a headless office suite behind an HTTP API, reached
by the api alone), `render`, Armature's PDF renderer (headless Chromium
with puppeteer in the public Chrome image, `render/server.mjs` mounted in),
which the api alone calls and which opens the web container's print views,
the one-shot `migrate` and `seed`, `api`,
`worker`, `web`, and `armature-stub` in Armature's place, which only the
stack lets the SSRF guard through to. Every service has a health check and the dependencies
wait on them, so `docker compose up --wait` returns once the stack answers.
The primary runs with `synchronous_commit=off`: a commit that waited for the
host's disk could take seconds on a busy machine. Only this stack does so; see
`docs/decisions.md`.

Two images are built. `Dockerfile.backend` holds every Go binary, and each
service picks one by its command. Both images are stamped with the version and
commit `scripts/build-stamp.sh` reads from git (`1.2.3` on a release tag,
`1.2.3+4.gabc1234` past one, `VERSION+dev` without one), which compose passes
as build arguments; `GET /api/v1/meta` reports it and the sidebar shows it.
A release builds them, with the `backup` target and `Dockerfile.render`, from
its tag and pushes them to `ghcr.io/praetorianer777/stator-<image>`
(`docs/releasing.md`). `Dockerfile.web` builds
the SPA with Node and serves it from nginx (`deploy/nginx.conf`), which proxies
`/api/`, `/healthz`, `/readyz` and the `/.well-known/oauth-` documents to the
api so the browser sees one origin,
over connections it keeps and reuses (in compose it looks the api's address
up again while it runs, so it follows a recreated api container), and passes the shared draft's WebSocket (`/api/v1/pages/{id}/collab`) on
with its upgrade.

`mk/stack.mk` drives the stack. The compose project is `stator-<cksum of the
checkout path>`, and the published ports are a block of twenty from 20000 up,
chosen by the same hash, so worktrees never collide. `make up` and
`make stack-up` write the project name and ports to `.cache/stack.env` for
anything outside the network, such as the browser suite. The integration suite
runs in the Go toolchain container on the stack's network and reaches every
service by name; it writes files to a bucket of its own, `stator-test`, beside
the app's `stator-files`. Each of its tests runs in a database of its own,
cloned from a template the suite migrates once and followed by the replica
(`backend/test/database_test.go`), with one organization in it; a test of
what moves between instances clones a second one as the fresh instance. Only
the tests of what `migrate` and `seed` left behind read the stack's own
database.

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

With `render.enabled` the chart runs the PDF renderer as in Armature's
chart: the image `deploy/Dockerfile.render` builds, behind a Service of its
own, reaching the web pods inside the cluster; without it a PDF download
is refused with a sentence.

For a trial, `values-demo.yaml` brings one Postgres pod and one Valkey pod of
the chart's own. The chart refuses to render with both `cnpg.enabled` and
`postgresql.enabled`, and refuses replicas behind more than one api pod
without a Valkey they share. Several api pods without Valkey and without
replicas render: editors on different pods reach each other through
Postgres. `tests/test-helm.sh` runs `helm lint` and
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
What no endpoint makes yet, such as a provider that is down, a spec
arranges straight in the database through `e2e/fixtures/db.ts`, with the
superuser URL `.cache/stack.env` names, and puts back again.

Every spec shares the one organization (#380), and keeps its data apart by
people and spaces of its own. A spec on `teamTest` from `e2e/fixtures/team.ts`
gets, per spec file and worker, an administrator alice and a member bob named
after the spec, made and signed in through the api's test endpoints (on in
compose through `STATOR_TEST_ENDPOINTS`, never in the chart) without the
identity provider, and removed with the spaces they made when the worker
stops. Its spaces have keys no other spec uses, and what it reads from lists
that span the organization, such as search, the home page or the people
pickers, it narrows to its own spaces and people. The team's label,
`team.label`, is also its tenant in the Armature stub.

What belongs to the organization as a whole (its permissions, hub, brand,
anonymous access and public links, themes, templates, sign-in provider,
members, Armature connection, webhooks, audit log and example space) is
changed only by the specs `e2e/fixtures/projects.ts` lists. They run in the
projects `instance` and `instance-mobile`, after all the others and with
one worker, and `POST /api/v1/test/reset` puts the organization's settings
back as the bootstrap left them before each of their files starts, and in the
setup and teardown. `make test-e2e` runs the two phases one after the other,
since a project depending on another would run all of the other in every CI
shard; each phase shards the same way. No spec makes a second
organization, which the database refuses, and `tests/test-ci.sh` fails one
that tries. See `docs/decisions.md`.

A Helm chart follows later.
