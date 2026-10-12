# Load testing

`tests/load` holds k6 scripts that put one Stator deployment under the load of
a team: people reading, people editing and publishing, and people editing a
page together. They answer two questions: does a deployment of this size stay
fast for a busy team, and how many people does it take before it does not.

## Running it

```sh
make stack-up
make test-load                              # the standard profile
make test-load LOAD_PROFILE=smoke           # a minute, a few people
make test-load LOAD_PROFILE=ceiling         # ramps up until a threshold breaks
make test-load LOAD_SCENARIOS=live          # one scenario alone
make test-load LOAD_SKIP=search,tree        # leave out search and the whole tree
```

k6 runs in the `grafana/k6` container on the stack's network and reaches
Stator through nginx, as a browser does. It signs in once as the bootstrap
administrator and shares that session between every virtual person. The run
ends with a summary, writes the same numbers to `reports/load-<profile>.json`
for comparing runs, and exits non-zero when a threshold breaks.

The first run seeds a space `LOAD` in the demo organization: 2,000 published
pages in a tree six wide and five deep, a comment on every tenth page and a
file on every fiftieth. Later runs reuse it. The text is filler every page
shares plus a few topic words from a vocabulary of 400, so a search for a
topic finds a few dozen pages and one for filler finds nearly all of them.
`LOAD_PAGES` seeds another size; a smaller number reuses the space as it is.

The **Load** workflow runs the same thing on a GitHub runner, by hand and smoke
by default. A shared runner is no reference machine: a run there says the
scripts and the stack agree, not how much a deployment carries.

## The scenarios

- **readers** open the space, the children of a page and the page, reload the
  whole tree one visit in five, and search one visit in three, one search in
  ten for a word on nearly every page. They think for one to three seconds
  between visits.
- **editors** read a page, save a draft and publish it, and comment every
  fourth time, then think for two to five seconds. Three saves in ten go to
  the same five pages, so their versions collide; a stale base version is
  answered 409, which is counted as `edit_conflicts` and not as a failure.
- **live** people hold a page's shared draft open over its WebSocket for about
  a minute, moving their cursor every second and typing every two, as a
  browser does. Four pages share them, so each room has several people in it.
  They arrive over the first ten seconds rather than all at once. The updates
  they send are not Yjs: the server stores and relays them without reading
  them. When the run ends, the rooms are thrown away. `collab_delivery` is how
  long an update takes from one person to another in the same room. It counts
  only updates relayed live, not the history a newcomer loads.

## Profiles and thresholds

| profile | readers | editors | live | how long |
|---|---|---|---|---|
| smoke | 3 | 1 | 4 | 1 minute |
| standard | 40 | 8 | 24 | 3 minutes |
| ceiling | 10 to 600 | 2 to 90 | 4 to 160 | up to 15 minutes |

| what | threshold (p95) |
|---|---|
| reads (space, children, page) | < 300 ms |
| the whole tree of the space | < 1 s |
| search | < 800 ms |
| writes (draft, publish, comment) | < 1 s |
| live update delivery | < 300 ms |
| opening a shared draft | < 500 ms |
| failed requests | < 1 % per scenario |

In the ceiling profile, a breaking threshold stops the run after 30 seconds of
grace, and `load_vus` says how many people were active by then. Search and the
tree are measured but do not stop the ramp: until #370 and #371 they are slow
at any load, and would end the ramp before it measured anything else.

## Measured

Measured on 2026-10-12 with the compose stack and k6 on one machine: AMD Ryzen 5
3600 (6 cores, 12 threads), 32 GB, Docker 29. One api process, one worker, a
primary and one streaming replica.

**Standard profile** (72 people, 38 requests/s, 0 failed):

| what | median | p95 | p99 |
|---|---|---|---|
| reads | 19 ms | 61 ms | 112 ms |
| writes | 70 ms | 178 ms | 255 ms |
| live update delivery | 5 ms | 31 ms | 192 ms |
| opening a shared draft | 107 ms | 332 ms | 428 ms |
| the whole tree (2,000 pages) | 2.4 s | **3.4 s** | 3.7 s |
| search | 3.1 s | **8.1 s** | 9.7 s |

Every threshold holds but search (#370) and the tree (#371).

**Ceiling profile**, as it is: stopped after a minute at **75 people**, when
live update delivery passed 300 ms at p95. Search and the tree had taken the
machine: search ran at p95 4.7 s. The rest is waiting for #370 and #371.

**Ceiling profile without search and the tree** (`LOAD_SKIP=search,tree`):
**418 people at 246 requests/s**. Reads were at p95 17 ms, writes at 73 ms and
live delivery at 5 ms when nginx began answering 502: it opens a new
connection to the api for every request and ran out of local ports (#372).
Neither the api nor the database had reached a limit.

## When a threshold breaks

- Look at the `{ name: ... }` lines of the summary first: they say which
  request is slow.
- A slow read through the replica: compare with the same request against the
  primary, and look at the replica's lag (`pg_stat_replication`). Reads fall
  back to the primary when the replica lags, which moves the load there.
- A slow statement: `ALTER SYSTEM SET log_min_duration_statement = 400` on the
  primary and on the replica, `SELECT pg_reload_conf()`, run the profile
  again, and `EXPLAIN ANALYZE` the logged statement as `stator_app` with
  `app.org_id` and `app.user_id` set, because row level security decides the
  plan. That is how #370 and #371 were found.
- Requests waiting on each other: the api's pools (`STATOR_DB_PRIMARY_MAX_CONNS`,
  `STATOR_DB_REPLICA_MAX_CONNS`) against `pg_stat_activity`.
- 502 or 504 with low latencies: nginx's error log, as for #372.
- Live delivery: the api's log for refused or closed shared drafts, and the
  worker's queue of jobs, which competes for the same primary.
- Findings become issues of their own, with the profile and numbers that
  showed them.
