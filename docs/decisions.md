# Decisions

Newest first. Each entry says what was decided and why, so a later change can
tell whether the reason still holds.

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
