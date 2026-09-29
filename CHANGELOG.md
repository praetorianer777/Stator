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

### Changed

- Stored files are keyed under `org/<organization id>/`, so an
  organization's files can be listed and removed together.

### Fixed

- Subtle text (hints, timestamps, placeholders) meets WCAG AA: 4.5:1 or more
  on every surface in both palettes, and in the Deep-Tech and Constellation
  themes, with the hue kept. A unit test checks every text colour against
  every surface.
- The slash menu and the mention list keep the active option in view while
  focus stays in the editor, and the stored document on the development
  editor page is a named region the keyboard can reach and scroll. The
  browser suite's accessibility checks now pass with no known findings.
- Spaces: a key that is part of every address (`/s/{key}`), a name, a
  description and a home page, the root of the space's pages. A directory
  at `/spaces` lists them; administrators create, rename, describe and
  delete them, recorded in the audit log, and every member reads and edits
  their pages. Space settings show the details and who may do what. A page
  is read at `/s/{key}/p/{id}/{slug}` and edited in a chunk of its own, and
  a save made from an older copy of the page is refused.
