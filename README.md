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

## Quickstart

Docker with the compose plugin is all the host needs; every toolchain runs in
a container.

```sh
make up      # build and start the stack, and wait until it is healthy
make logs    # follow it (make logs S=api for one service)
make down    # stop it; make clean also deletes its data
```

`make up` prints where everything is. The ports are derived from the
checkout's path, so two checkouts can run side by side; they are also written
to `.cache/stack.env`.

| What | Where |
|---|---|
| Stator | `http://localhost:$WEB_PORT` |
| API | `http://localhost:$API_PORT` (also under `/api/` on the web port) |
| Keycloak | `http://localhost:$KEYCLOAK_PORT`, admin console as `admin` / `admin` |
| Mailpit | `http://localhost:$MAILPIT_PORT` |
| Postgres | `127.0.0.1:$POSTGRES_PORT`, user `stator`, password `stator` |

Keycloak imports the realm `stator-dev` with the clients `stator` (secret
`stator-dev-secret`) and `armature` (secret `armature-dev-secret`). Both put a
`groups` claim in their tokens. The test users:

| User | Password | Groups |
|---|---|---|
| `alice` | `alice password` | `stator-administrators`, `engineering` |
| `bob` | `bob password` | `marketing` |

`make seed` runs the seed again; `make shell` opens the Go toolchain on the
stack's network. `make help` lists every target.

`./run-tests.sh` uses the same compose project as `make up` in the same
checkout and removes it, data included, when it finishes.

## Contributing

Every change hangs off an issue and a branch named `<type>/<issue>-<slug>`,
and lands via pull request. Details in `.claude/skills/gh/SKILL.md`. The whole
suite runs with `./run-tests.sh`.

## License

[GNU AGPL v3.0](LICENSE). Contains code adapted from Armature under the Apache
License 2.0; see [NOTICE](NOTICE).
