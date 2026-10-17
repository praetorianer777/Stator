#!/usr/bin/env bash
# Upgrades a seeded instance on CloudNativePG, as production runs it: the
# migrations run as stator_owner, which is no superuser and is bound by row
# level security, against tables that hold rows. Installs the chart and images
# of a base tree (the change's merge base, until a chart is published), fills
# it through the API, upgrades to this checkout's chart and images, and checks
# that every Job succeeded, the data is all there and the api reports the new
# build. scripts/chart-install-test.sh covers the empty install on a bundled
# Postgres.
#
# Runs inside the image of scripts/kind (make test-chart-cnpg), with the
# Docker socket, this checkout at /work and the base tree at /base mounted.
# KEEP_CLUSTER=1 leaves the cluster for a look at it.
set -euo pipefail

ROOT="${ROOT:-/work}"
BASE="${BASE:-/base}"
CLUSTER="${CLUSTER:-stator-chart-cnpg}"
RELEASE=trial
NS=stator
FULL="${RELEASE}-stator"
LOCAL_PORT="${LOCAL_PORT:-18081}"
CNPG_VERSION=1.27.1
CNPG_MANIFEST="https://github.com/cloudnative-pg/cloudnative-pg/releases/download/v${CNPG_VERSION}/cnpg-${CNPG_VERSION}.yaml"
EMAIL=admin@chart-test.example
# shellcheck source=scripts/chart-install-lib.sh
source "$ROOT/scripts/chart-install-lib.sh"
STATE="$(mktemp)"

[[ -f "$BASE/deploy/charts/stator/Chart.yaml" ]] || fail "no chart at $BASE: mount the base tree there (make test-chart-cnpg does)"
: "${BASE_VERSION:?the base build version}" "${BASE_COMMIT:?the base build commit}"
BUILD_VERSION="${STATOR_BUILD_VERSION:?the build version}"
BUILD_COMMIT="${STATOR_BUILD_COMMIT:?the build commit}"
[[ "$BASE_COMMIT" != "$BUILD_COMMIT" ]] || echo "note: the base is this commit, so the upgrade changes no code"

make_cluster

say "The CloudNativePG operator $CNPG_VERSION"
kubectl apply --server-side -f "$CNPG_MANIFEST" >/dev/null
ok "applied; it starts while the images build"

say "The images of the base ($BASE_COMMIT) and of this checkout ($BUILD_COMMIT)"
build_images "$BASE" base "$BASE_VERSION" "$BASE_COMMIT"
build_images "$ROOT" ci "$BUILD_VERSION" "$BUILD_COMMIT"
ok "both are on the node"
kubectl rollout status deploy/cnpg-controller-manager -n cnpg-system --timeout=300s >/dev/null || fail "the operator is not ready"
ok "the operator is ready"

say "Install the base"
kubectl create namespace "$NS"
# Not --wait, as in scripts/chart-install-test.sh: the workloads wait for the
# post-install hooks.
helm install "$RELEASE" "$BASE/deploy/charts/stator" -n "$NS" -f "$ROOT/scripts/chart-install-cnpg-values.yaml" \
  --set images.backend.tag=base --set images.web.tag=base --timeout 15m
jobs_succeeded roles secrets-generator migrate bootstrap
workloads_ready
forward_web
owner="$(kubectl get cluster -n "$NS" "${FULL}-postgres" -o jsonpath='{.spec.bootstrap.initdb.owner}')"
[[ "$owner" == stator_owner ]] || fail "the database is owned by [$owner], want stator_owner"
ok "the base runs on CloudNativePG, owned by stator_owner"

say "Fill it through the API"
"$ROOT/scripts/chart-install-seed.sh" seed "$BASE_URL" "$EMAIL" "$(admin_password)" "$STATE"

say "Upgrade to this checkout"
before="$(kubectl get jobs -n "$NS" -o json | jq -r '[.items[] | select(.metadata.name | test("-migrate$")) | .metadata.uid][0]')"
helm upgrade "$RELEASE" "$ROOT/deploy/charts/stator" -n "$NS" -f "$ROOT/scripts/chart-install-cnpg-values.yaml" \
  --set images.backend.tag=ci --set images.web.tag=ci --wait --timeout 15m \
  || { kubectl logs -n "$NS" "job/${FULL}-migrate" --tail=20 2>&1 || true; fail "the upgrade failed"; }
after="$(kubectl get jobs -n "$NS" -o json | jq -r '[.items[] | select(.metadata.name | test("-migrate$")) | .metadata.uid][0]')"
[[ -n "$after" && "$before" != "$after" ]] || fail "the migrate Job did not run again on upgrade"
jobs_succeeded roles secrets-generator migrate bootstrap
ok "every Job succeeded, the migrate Job again"
workloads_ready
forward_web
check_meta "$BUILD_VERSION" "$BUILD_COMMIT"
ok "the api reports the new build"
"$ROOT/scripts/chart-install-seed.sh" check "$BASE_URL" "$EMAIL" "$(admin_password)" "$STATE"

say "A seeded instance on CloudNativePG upgrades with all its data"
