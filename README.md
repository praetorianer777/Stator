# Stator

A team wiki and knowledge base with first-class integration into
[Armature](https://github.com/Cloudster1/Armature): spaces, a page tree, a rich
editor, versions, comments and live Armature issues inside your pages. Same
look, same themes, same single sign-on.

Pre-1.0 and under construction; see the
[milestones](https://github.com/praetorianer777/Stator/milestones).

## Stack

Go (chi, pgx, goose), PostgreSQL, React with Tailwind, OIDC single sign-on.
Everything runs in containers; see `docs/architecture.md`.

## Contributing

Every change hangs off an issue and a branch named `<type>/<issue>-<slug>`,
and lands via pull request. Details in `.claude/skills/gh/SKILL.md`. The whole
suite runs with `./run-tests.sh`.

## License

[GNU AGPL v3.0](LICENSE). Contains code adapted from Armature under the Apache
License 2.0; see [NOTICE](NOTICE).
