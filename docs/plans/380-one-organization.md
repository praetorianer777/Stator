# Plan: one organization per instance (#380)

Decided on 2026-10-12: an instance serves one organization. Everything about
choosing, switching or isolating organizations goes, with no fixed `org_id`
kept for an interim period. An instance holding more than one organization is
refused at upgrade, with a message telling the operator to export every
organization with the export of #383, delete all but one, and upgrade again.

This plan splits the work into pull requests that each keep all four test
layers green. No code is written for it yet; the numbers below come from the
tree as of `cf0f6f7` (migrations up to `00710_page_outline.sql`).

## What there is today

### Database

- `current_org_id()` (`00001_foundation.sql:46`) reads `app.org_id`. 57 of the
  93 migrations use it, 261 lines in all.
- **69 tables carry `org_id`**, every one in its `CREATE TABLE`, none added
  later. The four without are `org`, `app_user`, `user_identity` and
  `user_session`. By migration: `org_member` (00001); `theme`, `theme_asset`,
  `user_theme` (00002); `groups`, `group_member` (00010); `oidc_provider` (org
  is its primary key), `oidc_login` (00011); `org_join_request`, `audit_log`
  (00013); `space`, `page` (00020); `api_token` (00040); `oidc_group_role`
  (00050); `page_version`, `page_draft` (00070); `attachment`,
  `attachment_tombstone` (00080); `global_grant`, `space_grant`,
  `page_restriction` (00090); `page_visit` (00100); `page_label` (00110);
  `comment_thread`, `comment` (00140); `outbox_event` (00150); `watch`,
  `watch_optout` (00151); `notification`, `notification_preference`,
  `notification_digest` (00152); `armature_connection` (org is its primary
  key) (00160); `armature_token` (00161); `armature_remote_link` (00167);
  `reaction` (00180); `star` (00210); `page_share`, `page_share_recipient`
  (00240); `page_owner`, `page_verification` (00250); `webhook_endpoint`,
  `webhook_delivery` (00290); `page_view`, `page_view_tally` (00300);
  `api_token_space` (00310); `space_shortcut` (00320); `page_task` (00410);
  `calendar`, `calendar_event` (00430); `attachment_preview` (00440);
  `page_collab`, `page_collab_update` (00450); `page_link` (00480);
  `page_version_editor` (00500); `page_schedule` (00510); `page_template`
  (00560); `example_job` (00580); `space_export`, `space_import` (00600);
  `word_import` (00610); `org_brand` (org is its primary key) (00660);
  `example_page` (00670); `review_rule`, `review_rule_subject`,
  `page_review`, `page_review_subject`, `page_review_decision`,
  `page_read_ask`, `page_read_confirmation` (00680).
- Keys built on it, counted line by line in the Up sections (lower bounds): 41
  composite primary keys `(org_id, ...)`, 91 composite foreign keys
  `(org_id, ...)`, 28 `REFERENCES org(id)`, 85 indexes and 15 unique
  constraints naming `org_id`.
- **Object keys are generated from `org_id`**: `attachment.object_key`
  (00080), `attachment_tombstone`'s `own_prefix` check (00080),
  `attachment_preview.object_key` (00440), `space_export` and `space_import`
  (00600), `word_import` (00610). Theme assets, the brand logo and Word
  imports also build `org/<id>/...` in Go through `objectstore.OrgPrefix`
  (`theme/service.go:679`, `brand/brand.go:188`, `wordio/job.go:202`).
- **Policies.** 314 `CREATE POLICY` in all:
  - 71 tenant policies `<table>_tenant_isolation`, `org_id = current_org_id()`
    (`org` by `id`, `app_user` through `org_member`). They are the
    **permissive** policies `stator_app` has: every access policy is
    `AS RESTRICTIVE`, and restrictive policies alone admit no row.
  - 73 `<table>_admin_bypass` for `stator_admin`, there because every table
    forces row level security.
  - About 170 restrictive access policies (viewers, editors, own, guests,
    anonymous readers, links). Almost all call `SECURITY DEFINER` `perm_*`
    functions that filter by `current_org_id()` inside. Seven name the
    organization in their own text: `attachment_preview_viewers` and
    `_makers` (00440), `page_version_amenders`,
    `page_version_live_on_live_pages` and `page_version_editor_savers`
    (00500), `example_job_renews_the_example` (00670),
    `page_review_subject_namers` (00680).
  - The anonymous loop in 00470 makes `_not_anonymous` policies for every
    table and does not name the organization.
- **Functions.** The latest definition of 79 functions uses
  `current_org_id()`, 30 more use `org_id` (mostly triggers copying
  `NEW.org_id`). Among them the `perm_*` set (00090, redefined in 00260,
  00310, 00460, 00470, 00480, 00570), search (00700), the outline (00710),
  reviews (00680), `org_default_grants` (00640, the only SQL that sets
  `app.org_id` itself), `space_default_grants`, `armature_endpoint`,
  `audit_log_prune`, `page_view_prune`, `space_key_taken`, `org_hub_guard`,
  `org_brand_guard`, `org_anonymous_guard`, `session_reaches` and
  `session_stays_home` (00012).
- `user_session` has `current_org_id` and `proof_org_id`; the trigger
  `session_stays_home` keeps a session in an organization its proof reaches.
- `org` holds the settings that are the instance's own: slug, name,
  `settings`, `default_theme_id`, `hub_page_id`, `hub_landing`,
  `anonymous_access`, `anonymous_indexable`, `public_links`.

### Go

- `internal/tenant` (`PostgresVar = "app.org_id"`, `Org{ID, Slug}`,
  `WithOrg`, `FromContext`). `internal/db/tx.go` refuses a transaction
  without a tenant unless `db.WithSystem`, sets `app.org_id`, and only then
  `app.user_id`, `app.token_spaces`, `app.anonymous` and `app.page_link`.
- The tenant comes from `httpapi/middleware.go` `authenticate` through
  `auth.Principal.Org`, and for readers who are not signed in from the slug in
  the URL (`public.Reading`, `public.ReadingLink`).
- `tenant.WithOrg` in 17 files, `FromContext` in 20, `OrgID` in 25, SQL
  `org_id` on 429 lines in about 95 files, `current_org_id()` on 158 lines in
  61 files. All queries are hand-written pgx.
- The worker re-enters the tenant per job: events and `outbox_event`,
  notification fan-out and digests, Armature link sync, webhook delivery,
  lapses, task due dates, schedules, the example space, Word and space
  imports and exports (which select `org_id` and the slug of each job),
  audit and page view retention (one pass per organization), the attachment
  reaper.
- `seed` makes the demo organization, `seed bootstrap` the one named by
  `STATOR_BOOTSTRAP_ORG_SLUG` and `_NAME` (`config.go:358`), defaulting to
  `demo` and `Demo`.
- `internal/testorg` and the routes `POST /test/orgs`,
  `DELETE /test/orgs/{orgSlug}` and `POST /test/orgs/{orgSlug}/example-space/age`,
  on only with `STATOR_TEST_ENDPOINTS`.
- The webhook envelope has `orgId` (`webhook/payload.go:22`): part of what
  receivers see.
- MCP marks tools `orgWide` (`httpapi/mcp_tools.go`), hidden from tokens
  limited to spaces.
- Backup is a whole-database dump and names no organization.

### API and web

- Routes with a slug: `GET /auth/oidc/{orgSlug}/start`,
  `POST /armature/webhook/{orgSlug}`, 13 `/public/{orgSlug}/...` paths
  (site, space, page, its PDF and DOCX, attachments, logo, search, links).
  `POST /auth/switch-org`; `GET /auth/me` returns `organization` and
  `organizations`. The `/org/*` settings routes name no slug and stay, as
  instance settings.
- `api/openapi.json`: 26 of 210 paths name the organization; schemas
  `CurrentOrg`, `SwitchOrgRequest`, `Membership`, `MeResponse`.
- The web client has **no organization switcher**: `organizations` is read
  only by a test fixture. Sign-in asks for the organization
  (`features/auth/LoginPage.tsx`, `LAST_ORG_KEY` in `config.ts`,
  `ssoStartURL(orgSlug)`). Public routes are `/public/$org/...` and
  `/print/public/$org/...` (`routes/public.tsx`, `routes/print.tsx`,
  `features/editor/publicReading.ts`). About 80 i18n lines a language say
  organization.

### Armature

- `armature_connection` keeps the Armature organization a Stator
  organization talks to (`org_slug`, `armature_org_id`); that is Armature's
  organization and stays. The webhook receiver finds the Stator organization
  by the slug in its path.
- `armature-stub` makes a tenant per token
  (`armature_pat_{tenantSlug}_{person}`), and the browser specs use the
  throwaway organization's slug as the stub tenant
  (`e2e/tests/armature.spec.ts`).

### Tests

- **Browser**: 102 specs, 90 on `orgTest` (`e2e/fixtures/org.ts`): a
  throwaway organization per spec file per worker, with alice (admin) and bob
  (member) signed in through Keycloak, deleted when the worker stops. The 12
  others run in `demo`. `fixtures/db.ts` makes organizations straight in SQL
  (`orgWithDeadProvider` for `login.spec.ts`, `throwawayPerson` for
  `language.spec.ts` and `page-views.spec.ts`). Six specs put the slug in
  public URLs. A rough grep finds about 55 of the 90 touching something that
  is the organization's as a whole (settings, permissions, themes,
  templates, hub, brand, webhooks, Armature, audit, members, tokens).
- **Integration**: `makeMember` (`backend/test/harness_test.go:341`) makes
  **a new organization for every person**, 270 calls in 98 files, so nearly
  every test is isolated by organization. Tests that are about the tenant
  wall: `tenant_isolation_test.go` (six tests), `signin_rls_test.go`
  (another tenant's sign-in, groups), `group_roles_test.go`,
  `sessions_test.go` (`TestSwitchingOrganizationsFollowsTheProof`),
  `testorgs_test.go`, `stack_test.go` (bootstrap, the new organization's
  grant), `space_transfer_test.go`
  (`TestASpaceTravelsToAnotherOrganizationWhole`), `attachments_test.go`
  (`TestTheReaperEmptiesADeletedOrganization`), `brand_test.go`,
  `anonymous_test.go`, and about 17 "walled by the database" tests that use a
  second organization as the outsider.
- Unit: `TestAuthenticationScopesTheTenant`,
  `TestSpaceAndPageRoutesNeedAnOrganization`, `test_handlers_test.go`,
  `testorg_test.go`, `TestALinksAddressNamesTheOrganizationThenTheToken`.

### Deploy and docs

- Chart: `bootstrap.org.slug` and `.name` (`values.yaml:357`),
  `job-bootstrap.yaml`, `job-seed.yaml`. Compose sets the test endpoints.
- `docs/decisions.md:4530` "Multi-tenant by organisation, enforced by the
  database", and the entries that follow from it (4354 throwaway
  organizations per spec, 4405 tokens in one organization, 4478 a session
  stays home, 3634 the webhook receiver). `docs/architecture.md` "Multi-tenancy"
  (l.39). `docs/mcp.md`, `docs/backup-and-restore.md`, the API contract
  documents.

## The order, and why

Two things decide it. The tests lean on organizations for isolation, so they
must stop doing that before organizations can go. And code can only stop
naming an organization once the database guarantees there is exactly one,
which is the upgrade refusal. So:

1. Tests stop needing organizations to stay apart.
2. The upgrade refuses several organizations, and the database keeps it at
   one. Throwaway organizations and the tenant wall's tests go.
3. API and web stop naming an organization: slugs, switching, sign-in.
4. Go and SQL stop setting and reading the tenant: `app.org_id`, tenant
   policies and `current_org_id()` in functions.
5. The schema drops `org_id`, its keys and `current_org_id()`.
6. The decision record and what docs are left.

PR 2 waits for #383, whose export the refusal names. PRs 4 and 5 can be
merged as one if the review prefers one schema change; they are split because
each is large on its own (79 functions and 71 policies, then 69 tables).

Each PR updates the documentation its change makes wrong, as every PR here
does; PR 6 only writes the decision and sweeps.

## PR 1: tests isolated without organizations

**Goal.** Every browser spec and integration test keeps its data apart from
the others while all of them share one organization, and nothing in the
suites makes a second organization except the tests about the tenant wall,
which go in PR 2. No product code changes.

### Browser suite

- New test endpoints beside the old ones, behind the same token and
  `STATOR_TEST_ENDPOINTS`:
  - `POST /test/people` makes a person with a role (`admin`, `member`,
    `guest`) and a label, in the one organization, and returns a session
    cookie, without Keycloak. `DELETE /test/people/{id}` removes them.
    `fixtures/db.ts` `throwawayPerson` already does this in SQL for two
    specs; this moves it behind the api so the session is a real one.
  - `POST /test/spaces/{key}/example-space/age` replaces the organization
    form of the example space endpoint.
- `e2e/fixtures/org.ts` becomes `fixtures/team.ts`: `teamTest` gives each
  spec file its own people (an admin and a member named after the spec) and
  its own spaces, made with a key that carries the spec's label and a random
  suffix, as the throwaway organization's slug does today, removed when the
  worker stops. `freshOrg.slug` and `freshOrg.id` go from the 60 places that
  use them.
- Specs that change what belongs to the organization as a whole (its
  permissions, hub, brand, anonymous access and public links, themes and the
  default theme, organization templates, SSO provider and group roles,
  Armature connection, webhooks, audit, members and join requests, the
  example space) move into a Playwright project `instance` with
  `fullyParallel: false` and one worker, which runs after the parallel
  project. A `POST /test/reset` endpoint puts the organization's settings back
  to the bootstrap state after each of those files, so no file sees another's
  settings. The first commit lists the specs this applies to; the grep above
  suggests about half of the 90, and some of them only need their assertions
  scoped to their own space instead.
- Specs that read lists spanning the organization (search, home, recently
  visited, notifications, stale pages, audit) scope their assertions to the
  spec's own spaces or people.
- Specs that sign in through Keycloak as alice and bob (`login`, `join`,
  `add-member`, the SSO specs) keep doing so in the one organization and go
  to the `instance` project when they change membership or the provider.
- The Armature specs name their stub tenant after the spec instead of the
  organization's slug; the stub needs no change.
- `orgWithDeadProvider` (`login.spec.ts`) goes to PR 2 with the organization
  slug field it tests.

### Integration suite

- `makeMember` stops making an organization per person. Two candidates, to
  be decided by a measurement in the first commit:
  - **A database per test**, cloned from a migrated template
    (`CREATE DATABASE ... TEMPLATE`), with the replica following through
    streaming replication. The strongest isolation and the smallest change to
    the 98 files, if cloning stays well under a second; the replica-lag tests
    need the clone to have reached the standby before they pause replay.
  - **People and spaces per test in one database**, which is what the
    browser suite does: each test makes its people in the one organization
    and asserts on its own spaces. Tests that change organization-wide rows
    (permissions, hub, brand, anonymous access, tokens, templates, audit,
    retention) are marked serial and restore what they changed.
- The "walled by the database" tests change their outsider from someone in
  another organization to someone in the same organization without the
  right, which is the guard that stays. Each keeps its raw SQL half: the same
  statement as `stator_app` with `app.user_id` set, refused by the access
  policy rather than the tenant one.
- The tenant wall's own tests (listed above) are left as they are; they still
  make second organizations and go in PR 2.

**Risks.** Serialising the `instance` project lengthens the browser shards;
CI's four shards may need rebalancing (`tests/test-ci.sh` checks the
matrix). Specs that pass today only because nobody else is in their
organization show up as flakes; run the suite several times with
`--repeat-each` before merging. The template database choice is a
measurement, not a guess.

**Proven by.** All four layers green, the browser suite also with
`--repeat-each=3`; a grep in a unit test or `tests/test-ci.sh` that no spec
imports `orgTest` any more.

## PR 2: the upgrade refuses several organizations

**Depends on #383** (its export is what the refusal tells the operator to
use).

- Migration `00720_one_organization.sql`:
  - Refuses when `org` has more than one row, with `RAISE EXCEPTION` whose
    message says: this instance holds N organizations and Stator now serves
    one per instance; export every organization (Settings, export, #383),
    delete all but one, then upgrade again. The `HINT` names the
    organizations' slugs. Goose runs each migration in a transaction, so a
    refused upgrade leaves the database as it was and `migrate` exits non-zero
    (the compose service and the chart's migrate Job fail, and the api never
    starts on a half-upgraded schema).
  - An empty instance gets nothing here: bootstrap or seed makes the one
    organization, as today.
  - A trigger `org_is_the_only_one` refuses a second `INSERT INTO org`, so
    the invariant holds through SQL and not only through the service. It is
    dropped again in PR 5, when `org` becomes a one-row table by its key.
- `internal/testorg`, `POST /test/orgs` and `DELETE /test/orgs/{orgSlug}` go,
  with `testorgs_test.go`, `test_handlers_test.go`'s organization cases and
  `testorg_test.go`. `fixtures/org.ts` goes if PR 1 left it.
- The tests of the tenant wall go: `tenant_isolation_test.go` except
  `TestUnscopedAccessIsRefused`, `TestPrivilegedRolesAreRefused` and
  `TestEveryTableForcesRowSecurity`, which are not about organizations and
  move to a `rls_test.go`; `TestRawSQLCannotReachAnotherTenantsSignIn`,
  `TestRawSQLCannotReachAnotherTenantsGroupRoles`,
  `TestSwitchingOrganizationsFollowsTheProof`,
  `TestASpaceTravelsToAnotherOrganizationWhole`,
  `TestTheReaperEmptiesADeletedOrganization`,
  `TestANewOrganizationGetsItsGrantWhereTheOwnerIsBoundByRowLevelSecurity`,
  `TestAnonymousReadersSeeTheBrandOfAnOpenOrganization` and the
  `orgWithDeadProvider` case of `login.spec.ts`. Each is checked for a guard it
  proves besides the tenant one; that part moves into a test that stays.
- `seed` and `seed bootstrap` keep making the one organization; a second
  `seed bootstrap` with another slug fails with a sentence saying the instance
  already has its organization.
- Docs: `docs/decisions.md` gets the new decision (see PR 6) now, since the
  behaviour changes here; `docs/backup-and-restore.md` and the upgrade notes
  say what the refusal means and what to do.

**Risks.** The refusal must be the first migration of the release that ships
any of PRs 3 to 5, or an instance with several organizations reaches code
that assumes one. Merge PR 2 before the others and do not release between
PR 1 and PR 2 with #383 missing. The message must hold up without the docs:
the operator reads it in the migrate Job's log.

**Proven by.**
- An integration test `TestTheUpgradeRefusesSeveralOrganizations`: a scratch
  database migrated with goose to `00710`, two organizations inserted, `Up`
  run; it fails, the message names the export and both slugs, and
  `goose_db_version` still ends at `00710`. A second case with one
  organization upgrades and keeps its spaces, pages and members. The harness
  can run goose against a scratch database the way `cmd/migrate` does
  (`goose.UpTo`).
- `TestASecondOrganizationIsRefused`: `INSERT INTO org` as the superuser and
  as `stator_admin` fails after the migration.
- All four layers green.

## PR 3: the API and the web client name no organization

- Routes:
  - `GET /auth/oidc/start` replaces `/auth/oidc/{orgSlug}/start`.
  - `/public/...` replaces `/public/{orgSlug}/...` (13 paths), and
    `/public/links/{token}` the link form. The old paths answer
    `308 Permanent Redirect` to the new ones when the slug is the instance's
    and `404` otherwise, so links already shared keep working; a unit test
    holds both.
  - `POST /armature/webhook` replaces `/armature/webhook/{orgSlug}`; the old
    path stays as an alias for receivers already configured in Armature, and
    the Armature settings page shows the new address.
  - `POST /auth/switch-org` goes; `GET /auth/me` loses `organizations`
    (and `Membership`), keeps `organization` as the instance's name and the
    person's role.
- The api resolves the one organization once at start (and on a change of
  its settings) and puts it in the context in `authenticate`,
  `anonymousReader` and `linkReader`; nothing is taken from the URL.
  `public.Reading` and `ReadingLink` lose the slug parameter.
- `auth.Service.SwitchOrg`, `OrgBySlug`, memberships and the
  `session_stays_home` check go from Go; the session's `current_org_id` is
  set to the one organization on sign-in until PR 5 drops the column.
- Web: the organization field leaves `LoginPage.tsx`, with `LAST_ORG_KEY`,
  `readLastOrg`, `rememberOrg` and `login.organization*`; `ssoStartURL()`
  takes nothing. `routes/public.tsx`, `routes/print.tsx` and
  `publicReading.ts` drop `$org`. i18n keys that say "organization" for what
  is now the instance are reworded, not only removed.
- `STATOR_BOOTSTRAP_ORG_SLUG` goes: the slug no longer shows anywhere a
  person types or reads. `STATOR_BOOTSTRAP_ORG_NAME` stays as the
  organization's name. Config refuses the old variable with a sentence saying
  it is no longer read, rather than ignoring it. Chart: `bootstrap.org.slug`
  goes, `job-bootstrap.yaml` and `tests/test-helm.sh` follow.
- `make openapi`; `api/openapi.json` and `web/src/api/schema.d.ts`
  regenerated.
- MCP: `orgWide` is renamed to what it means, tools for the whole instance,
  with `docs/mcp.md`.
- e2e: `signInToOrg` and `startSSO` stop typing a slug; the six specs with
  public URLs use the new paths, and one checks that an old link redirects.

**Risks.** Old public URLs and the Armature webhook address are in other
people's hands; the redirects and the alias are what keeps them working, and
they are proven by tests, not by hand. The sign-in callback keeps its path,
so providers' redirect URIs need no change; check the compose Keycloak realm
anyway.

**Proven by.** Router unit tests for the new paths, the redirects and the
404 for a wrong slug; OpenAPI freshness test; vitest for the login page and
public routes; the browser suite signing in without an organization field;
all four layers green.

## PR 4: nothing sets or reads the tenant

- Migration `00730_no_tenant_policies.sql`:
  - Drops the 71 `<table>_tenant_isolation` policies and adds, per table, a
    permissive policy for `stator_app` that admits every row
    (`<table>_app ... TO stator_app USING (true) WITH CHECK (true)`). Without
    it the restrictive access policies would admit nothing. The 73
    `_admin_bypass` policies stay.
  - Redefines the 79 functions that call `current_org_id()` and the seven
    policies that name the organization, without the organization filter;
    `perm_*`, search, the outline, reviews, `org_default_grants` (which stops
    setting `app.org_id`), `session_reaches`, `space_key_taken`, the guards.
    Generated from the latest definition of each, so a later redefinition is
    not lost; a script lists the latest definition per function name.
  - `current_org_id()` stays defined for one more migration, returning the
    one organization's id, only so that a function this PR missed still
    works; PR 5 drops it and a test proves nothing calls it.
- Go:
  - `internal/db/tx.go` stops refusing a transaction without a tenant and
    stops setting `app.org_id`; `app.user_id` and the others are set whenever
    known. `internal/tenant` goes; `db.WithSystem` stays for the admin pool.
  - The 158 `current_org_id()` lines in 61 files go from the SQL in Go;
    inserts still write `org_id` until PR 5, from the one organization's id.
  - The worker stops entering a tenant per job; retention runs once, not per
    organization.
  - The webhook envelope's `orgId`: removed, and the change named in the
    changelog, since receivers see it.
- Tests: `TestAuthenticationScopesTheTenant` and
  `TestSpaceAndPageRoutesNeedAnOrganization` go; every access guard keeps its
  raw SQL proof, now without `set_config('app.org_id', ...)` in the tests that
  set it (`permissions_rls_test.go`, `anonymous_test.go`,
  `spaces_test.go` `actAs`, `themes_test.go`, `tokens_test.go` and the
  others listed under Tests). `TestEveryTableForcesRowSecurity` additionally
  checks every table has its `stator_app` permissive policy.

**Risks.** The size: 79 function bodies rewritten by hand invite a lost
clause. The guard is that every access test keeps passing, through SQL as
well as through the service, and a catalog test (below) finds any
`current_org_id` left. Plans change when the organization filter leaves
the functions: rerun `make load-compare` (#381) on the reference machine;
search and the outline are the ones to watch.

**Proven by.** All four layers green; a catalog test that no policy
expression (`pg_policies`) and no function body (`pg_proc.prosrc`) in
`public` mentions `current_org_id` except `current_org_id` itself;
`make load-compare` against the baseline, its table in the PR.

## PR 5: the schema has no organization

- Migration `00740_drop_org_id.sql`, in an order Postgres accepts:
  - Object keys first: `attachment.object_key`, `attachment_preview`,
    `space_export`, `space_import` and `word_import` stop being generated
    (`ALTER COLUMN ... DROP EXPRESSION` keeps every stored value, so no file
    moves). New rows get their key from a trigger or from Go, under a prefix
    without the organization. Theme assets, the brand logo and Word imports,
    whose keys Go builds with `objectstore.OrgPrefix`, get a stored
    `object_key` filled from today's formula, for the same reason. The
    `attachment_tombstone_own_prefix` check goes.
  - Foreign keys `(org_id, x)` become `(x)`, primary keys `(org_id, ...)`
    lose `org_id`, unique constraints and indexes are rebuilt without it, then
    `org_id` is dropped from the 69 tables.
  - `oidc_provider`, `armature_connection` and `org_brand`, whose key was the
    organization, become one-row tables with a `singleton boolean PRIMARY KEY
    DEFAULT true CHECK (singleton)` key, or fold into the settings row.
  - `org_member` becomes the instance's membership, keyed by person; the
    `app_user` policy through `org_member` loses its organization.
  - `user_session` loses `current_org_id` and `proof_org_id`, with
    `session_reaches` and `session_stays_home`.
  - `org` becomes the instance's settings: one row, no slug (the old
    `org_is_the_only_one` trigger replaced by its key).
  - `current_org_id()` is dropped.
- Go: `OrgID` fields and the remaining `org_id` SQL go; `objectstore.OrgPrefix`
  stays only to read keys written before.
- `docs/backup-and-restore.md`: what restoring a backup taken before PR 2
  does on a newer release (whether the restore runs the migrations, and so
  meets the refusal), checked and said there.

**Risks.** Rebuilding 41 primary keys, 91 foreign keys and 85 indexes takes
locks on every table; on a large instance the upgrade takes a while and the
site is down for it. Measure the migration on the load test's seeded space
(2,000 pages) and put the time in the PR. A down migration cannot invent the
dropped column for a second organization; it restores `org_id` from the one
row and is tested once.

**Proven by.** The catalog test of PR 4 extended: no column named `org_id`
in `information_schema.columns`, no `current_org_id` function, no policy or
function mentioning either. The upgrade test of PR 2 extended to run to the
end and read a page, an attachment (by its old key) and a theme asset back.
`tests/test-backup.sh` restores and compares as before. All four layers
green, `make load-compare` once more.

## PR 6: the decision and the rest of the docs

- `docs/decisions.md`: a new entry "One organization per instance" that
  replaces "Multi-tenant by organisation, enforced by the database"
  (2026-09-29, l.4530), marked as replaced the way the licence entry is;
  the entries built on it (4354 throwaway organizations per spec, 4405
  tokens in one organization, 4478 a session stays home, 3634 the webhook
  receiver) marked as replaced or amended.
- `docs/architecture.md` "Multi-tenancy" becomes "Row level security":
  `stator_app` and `stator_admin`, the access policies, guests, anonymous
  readers, links; the testing section describes the `instance` project and
  the per-spec people and spaces.
- `docs/api-contract-m1.md` to `m3.md`, `docs/mcp.md`, `docs/word.md`,
  `docs/wiki-import.md`, `docs/load-testing.md` (the `EXPLAIN ANALYZE` hint
  sets `app.org_id`), `README.md`, the chart's `values.yaml` comments and
  `NOTES.txt`.

**Proven by.** A grep in CI-run shell tests that `current_org_id`,
`app.org_id` and `orgSlug` appear nowhere outside migrations and the
changelog; the shell layer green.

## Answers to the review questions

Decided on 2026-10-12, in the spirit of the decision itself: remove what is
about organizations rather than keep it under another name.

- **The integration suite's isolation** is decided by the measurement in PR
  1's first commit, as planned: a database per test if a clone stays well
  under a second, people and spaces per test otherwise.
- **The webhook envelope's `orgId` is dropped** in PR 4. The changelog names
  it as a change receivers see.
- **Tables are renamed in PR 5**, which rebuilds them anyway: `org` becomes
  `site`, `org_member` becomes `member`, and the tables named after the
  organization (`org_brand`, `org_join_request`, ...) lose the prefix or take
  `site_`. The `/org/*` settings routes become `/site/*` in PR 3, without
  aliases, since only signed-in clients call them; the changelog names it.
  Public links and the Armature webhook keep their redirects and alias.
- **PRs 4 and 5 stay separate**, so each schema change is reviewed and
  measured on its own.
