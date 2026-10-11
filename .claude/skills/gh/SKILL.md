---
name: gh
description: GitHub workflow for this repo — read before any code change. Creating issues, issue branches, running tests, opening PRs. Use this whenever you want to change code, commit, push or open a pull request.
---

# GitHub workflow

Repo: `praetorianer777/Stator` · default branch: `main`

## Rules

- Every change needs an issue and a branch named `<type>/<issue>-<slug>`.
  Types: `feat fix chore docs refactor test perf ci build revert`.
- Never push to `main`. Never merge yourself — that is the user's decision.
- Agree on issues and PRs with the user before creating them.
- Always call `gh` non-interactively: `--title` / `--body-file`, never open the editor.
- `.claude/hooks/branch-guard.sh` enforces this; a violation is blocked, not commented on.
- Issues carry a milestone (`M0 Foundation` … `Backlog`), an `area/*` label and `mvp` or `backlog`.

## Language

Everything in the repository is written in English: issues, pull requests, commit messages, code,
comments and documentation. User-facing strings live in `web/src/i18n`.

No other wiki or documentation product is named anywhere — not in code, docs, issues, PRs or
commit messages. Features are described on their own terms.

## Armature

Changes Stator needs in Armature go through issues and pull requests in `Cloudster1/Armature`
(the account has read access there, so PRs come from a fork). Link them from the Stator issue.

## Recipe

```bash
gh auth status                       # preflight

gh issue list --limit 20             # does the issue already exist?
gh issue create --title "..." --body-file /tmp/body.md --label area/backend --milestone "M1 Wiki core"

gh issue develop 42 --name feat/42-page-tree --base main --checkout

# ... work ...

./run-tests.sh                       # everything; the hook runs the quick subset before every push (STATOR_GATE=full for all)
git push -u origin HEAD

gh pr create --title "..." --body-file /tmp/pr.md   # body contains "Closes #42"
gh pr checks --watch
gh run view --log-failed             # when CI is red
```

## Releases

`release.sh` is the user's job. From an agent session, read only:
`gh release list`, `gh release view v0.1.0`.

## Pitfalls

- `gh issue develop` creates the branch on the remote and checks it out — no manual
  `git switch -c` needed.
- Branch slugs are lowercase ASCII: `feat/42-page-tree`, not `feat/42-Page-Tree`.
- Commit messages are Conventional Commits, lowercase, imperative, subject ≤ 72 characters, and
  describe the **effect** rather than the files touched.
- A push with `run_in_background` spends the whole gate before it is reported as started, then
  finishes in seconds; that is the gate having passed, not skipped. Every guard decision and gate
  duration is in `$(git rev-parse --git-common-dir)/branch-guard.log`.
- Parallel work happens in worktrees under `.claude/worktrees/`; each gets its own compose
  project and ports from `run-tests.sh`, so gates never collide.
