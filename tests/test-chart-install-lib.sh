#!/usr/bin/env bash
# Tests for jobs_succeeded in scripts/chart-install-lib.sh against a stand-in
# kubectl, so no cluster is needed: Helm deletes a hook Job before it makes the
# next run of it, and the check has to wait for that run (#543). Needs bash and jq.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"

FAILED=0
pass() { echo "   ✅ $1"; }
fail() { echo "   ❌ $1"; FAILED=1; }
check() { if [[ "$2" == "$3" ]]; then pass "$1"; else fail "$1: expected [$3], got [$2]"; fi; }

W="$(mktemp -d)"
trap 'rm -rf "$W"' EXIT
mkdir -p "$W/bin"

# Answers what jobs_succeeded asks. The Job shows from the look numbered
# APPEARS_AT on, and JOBS_JSON is the namespace's Jobs as -o json lists them.
cat > "$W/bin/kubectl" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  "get jobs -n "*" -o name")
    looks=$(( $(cat "$STUB/looks" 2>/dev/null || echo 0) + 1 ))
    echo "$looks" > "$STUB/looks"
    (( looks >= APPEARS_AT )) && echo "job.batch/r-stator-bootstrap"
    exit 0 ;;
  "get jobs -n "*" -o json") echo "$JOBS_JSON" ;;
  "get job.batch/"*"-o jsonpath="*) printf '%s' "$FAILED_STATUS" ;;
  "wait "*) exit "$WAIT_RC" ;;
esac
EOF
printf '#!/usr/bin/env bash\n' > "$W/bin/kind"
chmod +x "$W/bin/kubectl" "$W/bin/kind"

DONE='{"items":[{"metadata":{"name":"r-stator-bootstrap"},"status":{"succeeded":1}}]}'
STUCK='{"items":[{"metadata":{"name":"r-stator-bootstrap"},"status":{"succeeded":1}},{"metadata":{"name":"r-stator-seed"},"status":{}}]}'

# run <appears at> <failed status> <wait rc> <jobs json>: the exit code, then the output.
run() {
  rm -f "$W/looks"
  (
    export PATH="$W/bin:$PATH" STUB="$W" APPEARS_AT="$1" FAILED_STATUS="$2" WAIT_RC="$3" JOBS_JSON="$4"
    export JOB_WAIT_S=2 JOB_POLL_S=0
    set -euo pipefail
    CLUSTER=c NS=n FULL=r-stator LOCAL_PORT=1
    # shellcheck source=../scripts/chart-install-lib.sh
    source "$SRC/scripts/chart-install-lib.sh"
    jobs_succeeded bootstrap
  ) > "$W/out" 2>&1
  echo "$?"
  cat "$W/out"
}

echo "🧪 chart-install-lib.sh waits for the hook Jobs"

out="$(run 3 "" 0 "$DONE")"
check "a Job that Helm makes a moment after the check starts is waited for" "$(head -1 <<< "$out")" "0"

out="$(run 1000000 "" 0 "$DONE")"
check "a Job that never comes fails" "$(head -1 <<< "$out")" "1"
grep -q "no bootstrap Job was run within 2s" <<< "$out" && pass "and says which" || fail "and says which: $out"

out="$(run 1 True 1 "$DONE")"
check "a failed Job fails without waiting out the time" "$(head -1 <<< "$out")" "1"
grep -q "the bootstrap Job failed" <<< "$out" && pass "and says which" || fail "and says which: $out"

out="$(run 1 "" 1 "$DONE")"
check "a Job that does not complete in time fails" "$(head -1 <<< "$out")" "1"
grep -q "the bootstrap Job did not complete within 2s" <<< "$out" && pass "and says which" || fail "and says which: $out"

out="$(run 1 "" 0 "$STUCK")"
check "another Job that has not succeeded fails" "$(head -1 <<< "$out")" "1"
grep -q "these Jobs did not succeed within 2s: r-stator-seed" <<< "$out" && pass "and names it" || fail "and names it: $out"

if [[ $FAILED -eq 0 ]]; then echo "✅ chart-install-lib.sh tests passed"; else echo "❌ chart-install-lib.sh tests failed"; fi
exit $FAILED
