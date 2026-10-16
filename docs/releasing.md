# Releasing

Stator, Armature and Exciter release the same way: a version is cut by
`./release.sh`, reviewed as a pull request, and published from the tag that
merging it sets. Nothing is built on the releasing machine.

## Cutting a release

From an up-to-date `main` with a clean working tree:

```sh
./release.sh --dry-run      # the version, the rule that chose it, the notes
./release.sh                # or ./release.sh 1.4.0 to choose the version
```

1. `release.sh` works out the next version from the Conventional Commits
   since the last tag and prints the rule it applied: a breaking change is
   MAJOR (MINOR below 1.0.0), a `feat` MINOR, anything else PATCH. Before the
   first tag, the version in `VERSION` is released as it is.
2. It refuses a checkout that is not on `main` or is behind `origin/main`,
   a working tree with uncommitted changes, and a version whose tag or
   release branch already exists.
3. On a new branch `release/vX.Y.Z` it writes the version into every file
   that carries one (`scripts/version-files.sh files`: `VERSION`, and
   `version` and `appVersion` of `deploy/charts/stator/Chart.yaml`), turns
   the `Unreleased` section of `CHANGELOG.md` (or, when it is empty, the
   commit subjects) into the section `## [X.Y.Z]`, commits `release: vX.Y.Z`,
   pushes the branch and opens its pull request with `gh`.
4. CI runs the whole gate on that pull request, including the test that
   refuses the version files disagreeing. Merge it with a merge commit.
5. `.github/workflows/release.yml` sees the merged `release/vX.Y.Z` and
   `scripts/release-tag.sh` sets the annotated tag `vX.Y.Z` on the merge
   commit, with the release's changelog section as its message. It refuses a
   commit `main` does not have, version files that say another version, and
   a tag that already points elsewhere.
6. In the same run, from the tag:
   - the images `backend`, `backup`, `web` and `render` are built with
     `VERSION` and `COMMIT` and pushed to
     `ghcr.io/praetorianer777/stator-<image>` as `X.Y.Z` and as the short
     commit;
   - the chart is packaged and pushed to
     `oci://ghcr.io/praetorianer777/charts/stator` as version `X.Y.Z`;
   - the GitHub release `vX.Y.Z` is published with the tag message as its
     notes and the chart attached;
   - `praetorianer777/Dynamo` gets a `repository_dispatch` of type
     `tool-released` with `{"tool": "stator", "version": "X.Y.Z"}`.
7. Back on `main`: `git switch main && git merge --ff-only origin/main`.

A build that is not made from a tag says so in its version, `X.Y.Z+N.g<hash>`
or `X.Y.Z+dev` (`scripts/build-stamp.sh`), so a development image is never
taken for a release.

## Building your own images

The `backend` and `web` images take two build arguments, which say what the
sidebar and `GET /api/v1/meta` report:

- `VERSION`: the semver the build carries; empty, it is `X.Y.Z+dev` from the
  `VERSION` file.
- `COMMIT`: the short hash of the commit built; empty, the sidebar says
  `commit unknown` and the image build prints a warning.

The build context carries no `.git`, so pass both from a checkout:

```sh
docker build -f deploy/Dockerfile.backend \
  --build-arg VERSION="$(scripts/build-stamp.sh version)" \
  --build-arg COMMIT="$(scripts/build-stamp.sh commit)" .
```

The sidebar then shows `vX.Y.Z · abc1234`, a release included.

## Installing a release

```sh
helm install stator oci://ghcr.io/praetorianer777/charts/stator --version X.Y.Z \
  --set ingress.host=wiki.example.com
```

The chart's images default to `ghcr.io/praetorianer777/stator-<image>` at the
chart's `appVersion`.

## When something fails

Every step of `release.yml` can run again for the same version. Re-run the
failed jobs, or, for a tag whose run is gone, run the workflow by hand:
`gh workflow run release.yml -f tag=vX.Y.Z`. A tag pushed by hand onto a
commit on `main` publishes the same way. A published version is never moved
or released again; fix forward with the next version.

## Setup

- **`DYNAMO_DISPATCH_TOKEN`**, a repository secret: a fine-grained token with
  Contents read and write on `praetorianer777/Dynamo`, which is what a
  `repository_dispatch` needs. Without it the release still publishes and the
  run says Dynamo was not told.
- **Package visibility.** GitHub creates the `stator-*` image packages and
  the `charts/stator` package on the first push, private. Make them public
  (Package settings, Change visibility) for clusters to pull them without a
  pull secret, or give the cluster one through `images.pullSecrets`.
- The workflow's own token sets the tag and pushes the packages; it asks
  for `contents: write` and `packages: write` in the jobs that need them. A
  ruleset that restricts creating `v*` tags has to let GitHub Actions
  through.
