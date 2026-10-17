#!/usr/bin/env bash
# Installs deploy/charts/stator into a throwaway Kubernetes cluster (kind, a
# cluster made of one container) and checks what a render cannot: that the
# roles, migrate, secrets and bootstrap Jobs run in their order, that every
# workload becomes ready, that the first administrator can sign in through the
# web server, and that an upgrade runs the migrate Job again and rolls the api
# without a failed health check.
#
# Runs inside the image of scripts/kind (make test-chart-install), with the
# Docker socket and the checkout mounted. KEEP_CLUSTER=1 leaves the cluster
# for a look at it.
set -euo pipefail

ROOT="${ROOT:-/work}"
CLUSTER="${CLUSTER:-stator-chart-install}"
RELEASE=trial
NS=stator
FULL="${RELEASE}-stator"
LOCAL_PORT="${LOCAL_PORT:-18080}"
# shellcheck source=scripts/chart-install-lib.sh
source "$ROOT/scripts/chart-install-lib.sh"

make_cluster

say "The images, built from this checkout and loaded into the cluster"
# make passes the stamp in: the tools image has no git to read it with.
BUILD_VERSION="${STATOR_BUILD_VERSION:-$("$ROOT/scripts/build-stamp.sh" version)}"
BUILD_COMMIT="${STATOR_BUILD_COMMIT:-$("$ROOT/scripts/build-stamp.sh" commit)}"
build_images "$ROOT" ci "$BUILD_VERSION" "$BUILD_COMMIT"
ok "backend and web are on the node"

say "Install"
kubectl create namespace "$NS"
# Not --wait: Helm then waits for the workloads before it runs post-install
# hooks, and the api and worker cannot be ready until the roles and migrate
# Jobs, which are those hooks, have run. Helm blocks on the hooks either way;
# the workloads are waited for below.
helm install "$RELEASE" "$ROOT/deploy/charts/stator" -n "$NS" -f "$ROOT/scripts/chart-install-values.yaml" --timeout 15m
ok "helm ran the hook Jobs"

jobs_succeeded roles secrets-generator migrate bootstrap
ok "the roles, secrets, migrate and bootstrap Jobs all succeeded"

workloads_ready
ok "api, worker and web are ready"

say "Through the web server"
forward_web
[[ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE_URL/healthz")" == 200 ]] || fail "/healthz through the web server is not 200"
ok "the web server reaches the api, and the api reaches its database"
check_meta "$BUILD_VERSION" "$BUILD_COMMIT"
ok "the api reports the build it was stamped with, to anybody"

PASSWORD="$(admin_password)"
JAR="$(mktemp)"
code="$(curl -s -o /dev/null -w '%{http_code}' -c "$JAR" -H 'Content-Type: application/json' \
  -d "{\"email\":\"admin@chart-test.example\",\"password\":\"${PASSWORD}\"}" "$BASE_URL/api/v1/auth/login")"
[[ "$code" == 200 ]] || fail "the bootstrap administrator could not sign in: $code"
name="$(curl -s -b "$JAR" "$BASE_URL/api/v1/auth/me" | jq -r '.organization.name')"
[[ "$name" == Trial ]] || fail "signed in to organization [$name], want [Trial]"
ok "the bootstrap administrator signs in to the organization the Job made"

say "Upgrade"
before="$(kubectl get jobs -n "$NS" -o json | jq -r '[.items[] | select(.metadata.name | test("-migrate")) | .metadata.uid][0]')"
( while :; do
    curl -s -o /dev/null -w '%{http_code}\n' --max-time 3 "$BASE_URL/healthz" >>"$JAR.health" 2>/dev/null || echo 000 >>"$JAR.health"
    sleep 0.5
  done ) &
WATCH_PID=$!
helm upgrade "$RELEASE" "$ROOT/deploy/charts/stator" -n "$NS" -f "$ROOT/scripts/chart-install-values.yaml" --set api.replicas=2 --wait --timeout 15m
kill "$WATCH_PID" 2>/dev/null || true
after="$(kubectl get jobs -n "$NS" -o json | jq -r '[.items[] | select(.metadata.name | test("-migrate")) | .metadata.uid][0]')"
[[ -n "$after" && "$before" != "$after" ]] || fail "the migrate Job did not run again on upgrade"
ok "the migrate Job ran again"
[[ "$(kubectl get "deploy/${FULL}-api" -n "$NS" -o jsonpath='{.status.readyReplicas}')" == 2 ]] || fail "the api did not roll to two ready pods"
ok "the api is at two ready pods"
failed="$(grep -vc '^200$' "$JAR.health" || true)"
[[ "$failed" == 0 ]] || fail "$failed health checks failed during the upgrade"
ok "no health check failed while the api rolled ($(wc -l <"$JAR.health") made)"

say "The chart installs, upgrades and signs its administrator in"
