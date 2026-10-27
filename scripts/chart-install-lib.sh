# shellcheck shell=bash
# What the chart install tests share: output, a dump of what failed, the
# cluster's lifetime, image builds and a port-forward to the web Service.
# Sourced by scripts/chart-install-test.sh and scripts/chart-install-cnpg-test.sh,
# which set ROOT, CLUSTER, NS, FULL and LOCAL_PORT before they source it.

NODE_IMAGE="${NODE_IMAGE:-kindest/node:v1.34.0}"
KUBECONFIG="$(mktemp)"
export KUBECONFIG
FORWARD_PID=""
BASE_URL="http://127.0.0.1:${LOCAL_PORT}"

say() { printf '\n⎈ %s\n' "$*"; }
fail() { printf '   ❌ %s\n' "$*"; exit 1; }
ok() { printf '   ✅ %s\n' "$*"; }

dump() {
  say "What the cluster says"
  kubectl get pods,jobs,svc -A -o wide 2>&1 || true
  for pod in $(kubectl get pods -n "$NS" -o name 2>/dev/null); do
    phase=$(kubectl get "$pod" -n "$NS" -o jsonpath='{.status.phase}' 2>/dev/null || true)
    if [[ "$phase" != "Running" && "$phase" != "Succeeded" ]] || ! kubectl get "$pod" -n "$NS" -o jsonpath='{.status.containerStatuses[*].ready}' 2>/dev/null | grep -qv false; then
      echo "--- $pod"
      kubectl describe "$pod" -n "$NS" 2>&1 | tail -25 || true
      kubectl logs "$pod" -n "$NS" --all-containers --tail=40 2>&1 || true
    fi
  done
}

cleanup() {
  local code=$?
  [[ $code -ne 0 ]] && dump
  [[ -n "$FORWARD_PID" ]] && kill "$FORWARD_PID" 2>/dev/null || true
  if [[ "${KEEP_CLUSTER:-}" != 1 ]]; then
    kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
  else
    echo "cluster $CLUSTER kept; kind export kubeconfig --name $CLUSTER"
  fi
  rm -f "$KUBECONFIG"
  exit "$code"
}
trap cleanup EXIT

make_cluster() {
  # kube-proxy needs the mark target for service addresses; without it every
  # pod times out on the API server and the first Job fails after a long wait.
  if [[ -r /proc/modules ]] && ! grep -q '^xt_mark ' /proc/modules && ! grep -q '^xt_MARK ' /proc/modules; then
    echo "note: the xt_mark kernel module is not loaded; if pods cannot reach services, run: sudo modprobe xt_mark xt_MARK"
  fi
  say "A cluster"
  kind delete cluster --name "$CLUSTER" >/dev/null 2>&1 || true
  kind create cluster --name "$CLUSTER" --image "$NODE_IMAGE" --kubeconfig "$KUBECONFIG" --wait 180s
  ok "kind cluster $CLUSTER is up"
}

# build_images <source tree> <tag> <version> <commit>: the backend and web
# images of a tree, loaded into the cluster as stator-test/<name>:<tag>.
build_images() {
  local src="$1" tag="$2" version="$3" commit="$4" image
  for image in backend web; do
    docker build -q -f "$src/deploy/Dockerfile.$image" -t "stator-test/$image:$tag" \
      --build-arg VERSION="$version" --build-arg COMMIT="$commit" "$src" >/dev/null
  done
  kind load docker-image --name "$CLUSTER" "stator-test/backend:$tag" "stator-test/web:$tag"
}

# How long the Jobs are given to appear and complete. Helm deletes a hook Job
# before it makes the next run of it, so for a moment there is none.
JOB_WAIT_S="${JOB_WAIT_S:-300}"
JOB_POLL_S="${JOB_POLL_S:-2}"

unfinished_jobs() {
  kubectl get jobs -n "$NS" -o json | jq -r '[.items[] | select((.status.succeeded // 0) < 1) | .metadata.name] | join(", ")'
}

# job_complete <name> <deadline>: the Job named <release>-<name> exists and
# has completed by then; one that has failed fails at once.
job_complete() {
  local name="$1" deadline="$2" job=""
  while ((SECONDS < deadline)); do
    job="$(kubectl get jobs -n "$NS" -o name | grep -m1 -- "-$name\$" || true)"
    if [[ -n "$job" ]]; then
      [[ "$(kubectl get "$job" -n "$NS" -o jsonpath='{.status.conditions[?(@.type=="Failed")].status}' 2>/dev/null || true)" != True ]] \
        || fail "the $name Job failed: kubectl logs -n $NS $job"
      kubectl wait "$job" -n "$NS" --for=condition=complete --timeout="${JOB_POLL_S}s" >/dev/null 2>&1 && return 0
    else
      sleep "$JOB_POLL_S"
    fi
  done
  [[ -n "$job" ]] || fail "no $name Job was run within ${JOB_WAIT_S}s"
  fail "the $name Job did not complete within ${JOB_WAIT_S}s"
}

# The named Jobs have run and every Job in the namespace has succeeded; the
# hook Jobs are kept until the next run of the same hook replaces them.
jobs_succeeded() {
  local name deadline=$((SECONDS + JOB_WAIT_S)) left
  for name in "$@"; do
    job_complete "$name" "$deadline"
  done
  while true; do
    left="$(unfinished_jobs)"
    [[ -n "$left" ]] || return 0
    ((SECONDS < deadline)) || fail "these Jobs did not succeed within ${JOB_WAIT_S}s: $left"
    sleep "$JOB_POLL_S"
  done
}

# The api and worker start before the hooks have made the database and crash
# until they have, so kubelet's back-off, up to five minutes, is in this wait.
workloads_ready() {
  local workload
  for workload in api worker web; do
    kubectl rollout status "deploy/${FULL}-${workload}" -n "$NS" --timeout=360s >/dev/null || fail "$workload is not ready"
  done
}

# A new forward after an upgrade: one to a Service sticks to the Pod it chose.
forward_web() {
  [[ -n "$FORWARD_PID" ]] && kill "$FORWARD_PID" 2>/dev/null || true
  kubectl port-forward -n "$NS" "svc/${FULL}-web" "${LOCAL_PORT}:80" >/dev/null 2>&1 &
  FORWARD_PID=$!
  for _ in $(seq 1 30); do
    curl -fs "$BASE_URL/readyz" >/dev/null 2>&1 && return 0
    sleep 1
  done
  fail "/readyz through the web server did not answer 200"
}

# check_meta <version> <commit>: /api/v1/meta reports that build.
check_meta() {
  local meta
  meta="$(curl -fs "$BASE_URL/api/v1/meta")" || fail "/api/v1/meta through the web server did not answer"
  [[ "$(jq -r '.product + " " + .build.version + " " + (.build.commit // "")' <<<"$meta")" == "stator $1 $2" ]] \
    || fail "/api/v1/meta says $meta, want stator $1 $2"
}

admin_password() {
  kubectl get secret "${FULL}-bootstrap-admin" -n "$NS" -o jsonpath='{.data.password}' | base64 -d
}
