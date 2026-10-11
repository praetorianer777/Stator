# Working on Stator

Stator is a team wiki and knowledge base that works hand in hand with
[Armature](https://github.com/Cloudster1/Armature). It mirrors Armature's stack
and patterns on purpose; when in doubt, look at how Armature does it
(`/home/steve/git/Armature`) and do the same. See `docs/architecture.md` and
`docs/decisions.md`.

## Every change hangs off an issue

Read `.claude/skills/gh/SKILL.md` before changing anything. Branches are
`<type>/<issue>-<slug>`, created with `gh issue develop`; changes land via pull
request with `Closes #N`, and the user merges. `.claude/hooks/branch-guard.sh`
enforces this and, before every push, runs the layers of `./run-tests.sh` that
the pushed files can affect: format, Go, web and the shell tests by default,
and the stack, integration and browser layers with `STATOR_GATE=full`. CI runs
every layer and blocks the merge, so a quick local gate says what it skipped.
A gate that does not finish blocks the push.

## Nothing is installed on this machine

Every toolchain runs in a container, driven by the `Makefile`. Never install
Go, Node, Chromium or a Postgres client on the host. If a tool is missing, add
a target that runs it in a container.

Go is pinned to 1.26, as in Armature: under 1.27 on this host every test
binary is killed about a second into its run.

## Testing

Four layers, all run by `./run-tests.sh`: Go unit tests, Go integration tests
against the compose stack (`-tags integration`), vitest, and Playwright. A
feature is finished when all four pass, not when the code is written.

The API is described by `api/openapi.json`, derived from the route table.
After changing a route, a request type or a response type, run `make openapi`
and commit the regenerated document and `web/src/api/schema.d.ts`; a unit test
refuses a stale copy.

Nothing is mocked below the layer under test. A guard is not proven by the
service refusing: try the same thing straight through SQL and watch the
database refuse it too. The one stand-in is `armature-stub` for the Armature
API, kept honest by a contract test against Armature's `api/openapi.json`.

## Style

- Comments say why, never what. Doc comments at most two lines.
- No en dashes and no typographic quotes, in code or in prose.
- Named constants over magic numbers. Frontend ones live in `web/src/config.ts`.
- Errors the user reads are sentences, and name what to do about it.
- No other wiki or documentation product is named anywhere in the repository,
  issues, pull requests or commit messages.
- Code adapted from Armature keeps working like Armature's; `NOTICE` credits it.
