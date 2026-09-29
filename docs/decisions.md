# Decisions

Newest first. Each entry says what was decided and why, so a later change can
tell whether the reason still holds.

## 2026-09-29: Each browser spec file gets a throwaway organization

Specs that change what an organization shows, such as its default theme or
who uses which theme, cannot share `demo` with specs that judge the built-in
look, and running them one after another only hides the problem. The api
therefore has two test endpoints, `POST /api/v1/test/orgs` and
`DELETE /api/v1/test/orgs/{slug}`, and `e2e/fixtures/org.ts` makes one
organization per spec file and worker through them, signs alice and bob in to
it, and deletes it when the worker stops. The new organization gets the seed's
people and provider, from the same `STATOR_BOOTSTRAP_*` settings and the same
code, so it signs in exactly like `demo`.

The endpoints are compiled in, because the image the suite runs is the image
that ships, but routed only when `STATOR_TEST_ENDPOINTS` is on; otherwise they
answer 404 like any path that does not exist. Every call must carry
`STATOR_TEST_ENDPOINTS_TOKEN` in `X-Stator-Test-Token`, so an endpoint switched
on by accident is still closed. The api refuses to start with them on in
production, and `tests/test-helm.sh` checks that no layout of the chart sets
either variable. Deleting refuses any organization the endpoint did not make,
which it marks in `org.settings`.

They are left out of `api/openapi.json` rather than marked in it. The document
is the contract clients are generated from, and the web client would
otherwise carry types for calls it must never make; a separate table in
`internal/httpapi` describes them, and a unit test holds the router to both
tables and the document to the public one alone.

Deleting an organization has to find its files, so every object key now starts
with `org/<organization id>/`, and the store can list a prefix.
## 2026-09-29: Provider groups decide roles at each sign-in

An administrator maps groups of the identity provider, by the value of the
groups claim, to member or admin, per provider. Each sign-in through the
provider settles the person's role from the groups the token names: the
highest mapped role wins, and a membership records whether its role came from
the provider or from somebody here. A role the provider gave falls back to
member when its group goes, never further, since taking somebody out is an
administrator's act. A role somebody here chose is left alone while none of
the person's groups is mapped, so a mapping can be introduced group by group
without undoing what administrators already decided. The owner is never moved,
and the database refuses to let the provider manage an owner's role.

A mapped group is the administrators' approval given in advance, so somebody
in one joins on their first sign-in, with no request to answer. Every role the
mapping changes, and every change to the mapping, goes to `audit_log`.

Changes apply at the next sign-in rather than when the mapping is saved: only
a token says which groups somebody is in now, and group rows exist only for
the groups an organization keeps. Until then an open session keeps the role
it had.

## 2026-09-29: A personal access token is its owner in one organization

A script calls the API with a token sent as a bearer, as in Armature: the
prefix `stator_pat_` and 32 random bytes in base64url, of which only the
SHA-256 is stored, so a copy of the database opens nothing and a secret
scanner recognises a leaked one. A token belongs to a person in one
organization and reaches nothing else; leaving the organization removes it
by a foreign key onto the membership. Its one scope, `read`, is enforced by
middleware in front of every route, so a route added later cannot forget
it. Only a session makes a token, so a leaked token cannot mint a longer
lived one and outlive its own revocation. Tokens key read-your-writes as
`t:<token>`, apart from their owner's browser, so a script's writes do not
send the person's reads to the primary. Narrowing a token to some spaces is
left for later (#111).

The integration suite checks every answer the API gives it against
`api/openapi.json`, and fails when an operation was never answered
successfully or never refused, as Armature's does. The router is wrapped
rather than the test client, so the sign-in tests' browsers are checked too.

## 2026-09-29: Reads go to CloudNativePG's -ro service

On Kubernetes the chart points reads at the CNPG cluster's `-ro` service
instead of at each replica by name. The service follows the operator through
failovers and scaling, which a list of pod names would not; the price is that
one pool's connections reach different replicas. The api therefore checks
replay position and lag on the connection each read gets, and the health loop
samples several connections per pool, so the service is judged by more than
the one replica a single connection happens to reach.

## 2026-09-29: Read-your-writes is keyed by session, else by a client cookie

A write's position is kept in Valkey for `STATOR_READ_YOUR_WRITES_TTL` under a
key naming who wrote, and that key's reads stay off any replica that has not
replayed it. The natural key is the session, as in Armature, but sign-in
(#5) has not landed and there are no sessions yet. Until then, and for any
request without a session later, the key is a `stator_client` cookie: a
random id the API hands to a request that may write and has none. It is
http-only, carries no rights, and only ever sends its holder's own reads to
the primary, so forging one gains nothing. Keying by user instead would pin
every device of a person to one another's writes and could not tell two
anonymous callers apart; keying by IP address would pin whole offices to
one person's writes. Once sessions exist, `auth.Principal.SessionID` wins
over the cookie with no other change.

The position is recorded when the handler notes it, before the response is
written, and even for a refused request, because the caller's next request
can arrive before the handler returns and a refused import has already
written and undone a theme.

## 2026-09-29: The test stack keeps the streaming replica

The compose stack runs Postgres as a primary and a real streaming replica, as
Armature's does, and the gate runs against it. Read routing, the replica
health check and read-your-writes only mean something against real
replication, and a replica costs the gate a base backup of an empty cluster:
a few seconds, where Keycloak's start already takes longer. The integration
suite now uses this stack instead of a Postgres and a SeaweedFS of its own, so
the gate runs exactly one of each.

## 2026-09-29: An administrator lets each person in, and a session stays home

Signing in through the organization's provider is not the same as being let
in, as in Armature. Somebody the provider vouches for who is not a member gets
no session: the sign-in is refused, their account and a request to join are
noted, and the sign-in page tells them the request waits for an
administrator. An administrator lets them in as a member, or turns them away,
under Single sign-on; either answer is written to `audit_log`, which the audit
log will read. Letting anybody with an account at the provider in would leave
an organization with no membership at all, only a sign-in page. A development
stack names its people ahead of time with `STATOR_BOOTSTRAP_MEMBERS`, so
nobody has to click. For members, provider groups follow the groups claim
exactly, so revoking a group there revokes it here on the next sign-in; which
groups grant which role is a later decision.

A person is matched by issuer and subject, which survive an email change. A
first sign-in whose verified address already has an account, such as the
bootstrap administrator's, is tied to it. Because any organization may point
at a provider of its own choosing, a session opened through a provider only
reaches the organizations that trust the same issuer, and ones the person
owns; a password session reaches every membership. The database enforces
this with `session_reaches` and a trigger, not only the service.

## 2026-09-29: Only a bootstrap administrator has a password

Sign-in is through the organization's provider. A local password, hashed with
argon2id, exists so a fresh deployment can be entered and its provider set up;
it comes from `STATOR_BOOTSTRAP_ADMIN_EMAIL` and `_PASSWORD`, applied by
`cmd/seed`. Session tokens are 32 random bytes of which only the SHA-256 is
stored, and an identity provider's client secret is sealed with
`STATOR_SECRET_KEY`, bound to its organization.

## 2026-09-29: Armature is reached as the viewing user

Every call to Armature uses the viewer's own personal access token. A shared
bot token would be simpler, but it would show issues on a page to people who
cannot see them in Armature. Users without a connected token see issue keys
only.

## 2026-09-29: A stub stands in for Armature in the test suite

Running a full Armature stack in every gate would double its time. The
`armature-stub` implements only the operations Stator calls, and a contract
test checks it against Armature's published `api/openapi.json`, so the stub
cannot drift from the real API unnoticed.

## 2026-09-29: Playwright runs in the push gate

End-to-end tests run in `./run-tests.sh` before every push and in CI, not only
nightly. Ports and compose project names derive from the checkout path, so
parallel worktrees can run the gate at the same time.

## 2026-09-29: Multi-tenant by organisation, enforced by the database

Like Armature, every table is scoped to an organisation through row-level
security. A missing `WHERE` clause cannot leak data between organisations,
and tests prove it with raw SQL, not only through the service.

## 2026-09-29: The stack mirrors Armature

Go with chi, pgx and goose; PostgreSQL; React with TanStack and Tailwind v4;
OIDC for sign-in. Reusing Armature's patterns and design tokens makes the two
products look and behave as one, and lets code move between them.

## 2026-09-29: AGPL-3.0

Anyone may run and modify Stator, but whoever offers a modified version as a
service must publish their changes. Code adapted from Armature stays under its
Apache-2.0 notice (see `NOTICE`), which is compatible with the AGPL.

## 2026-09-29: Every change hangs off an issue

Work happens on `<type>/<issue>-<slug>` branches and lands through pull
requests that the maintainer merges. Hooks enforce it and run the full suite
before every push.
