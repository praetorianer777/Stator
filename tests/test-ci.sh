#!/usr/bin/env bash
# Tests that CI's jobs between them run the whole gate: every layer
# run-tests.sh knows, and every part of the browser suite once. CI splits the
# gate across parallel jobs, so a layer left out of the matrix would otherwise
# stop gating pull requests without anything turning red. Also that the
# browser specs keep apart by people and spaces of their own rather than by
# an organization each, since an instance serves one organization (#380).
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"
CI="$SRC/.github/workflows/ci.yml"

FAILED=0
pass() { echo "   ✅ $1"; }
fail() { echo "   ❌ $1"; FAILED=1; }

echo "🧪 ci.yml runs the whole gate"

ci_layers=$(sed -n 's/^ *layers: *//p' "$CI" | tr ' ' '\n' | sort)
for layer in $("$SRC/run-tests.sh" --list); do
  if grep -qx "$layer" <<< "$ci_layers"; then
    pass "a job runs $layer"
  else
    fail "no job in ci.yml runs $layer; add it to a job's layers"
  fi
done

for layer in $(sort -u <<< "$ci_layers"); do
  "$SRC/run-tests.sh" --list | grep -qx "$layer" \
    || fail "ci.yml names $layer, which run-tests.sh does not know"
done

dupes=$(uniq -d <<< "$(grep -vx test-e2e <<< "$ci_layers")")
[[ -z "$dupes" ]] && pass "no layer but the browser suite runs twice" \
  || fail "these layers run in more than one job: $dupes"

shards=$(sed -n 's/^ *shard: *//p' "$CI")
e2e_jobs=$(grep -cx test-e2e <<< "$ci_layers")
totals=$(cut -d/ -f2 <<< "$shards" | sort -u)
if [[ $(wc -l <<< "$totals") -ne 1 ]]; then
  fail "the browser jobs disagree on how many parts there are: $(paste -sd' ' <<< "$totals")"
elif [[ "$e2e_jobs" -ne "$totals" || $(wc -l <<< "$shards") -ne "$totals" ]]; then
  fail "$e2e_jobs browser jobs and $(wc -l <<< "$shards") shards for $totals parts; each part needs one job"
else
  missing=""
  for i in $(seq "$totals"); do grep -qx "$i/$totals" <<< "$shards" || missing+=" $i/$totals"; done
  [[ -z "$missing" ]] && pass "the $totals browser jobs run each part once" \
    || fail "no browser job runs part$missing"
fi

echo "🧪 the browser specs share the one organization"

E2E="$SRC/e2e"
old=$(grep -rlw orgTest "$E2E/tests" "$E2E/fixtures" 2>/dev/null)
[[ -z "$old" ]] && pass "no spec or fixture uses orgTest" \
  || fail "these still use orgTest, the throwaway organization per spec; use teamTest from fixtures/team.ts: $(paste -sd' ' <<< "$old")"

# A second organization only where nothing else can stand in for it: an
# archive is imported only into an organization without spaces, and a
# provider that cannot be reached is tested in one of its own.
SECOND_ORG_SPECS="login.spec.ts org-transfer.spec.ts"
for spec in $(grep -rlwE 'withAnotherOrg|orgWithDeadProvider' "$E2E/tests" 2>/dev/null); do
  name=$(basename "$spec")
  grep -qw "$name" <<< "$SECOND_ORG_SPECS" && pass "$name makes a second organization, as allowed" \
    || fail "$name makes a second organization; give it people and spaces of its own (fixtures/team.ts) instead"
done

instance_specs=$(sed -n '/^export const INSTANCE_SPECS = \[/,/^\];/p' "$E2E/fixtures/projects.ts" | sed -n 's/^ *"\([a-z0-9-]*\)",$/\1/p')
[[ -n "$instance_specs" ]] || fail "fixtures/projects.ts lists no INSTANCE_SPECS this test can read"
for name in $instance_specs; do
  [[ -f "$E2E/tests/$name.spec.ts" ]] || fail "INSTANCE_SPECS names $name, but there is no e2e/tests/$name.spec.ts"
done
[[ -n "$instance_specs" && $FAILED -eq 0 ]] && pass "every spec INSTANCE_SPECS names exists"

if [[ $FAILED -eq 0 ]]; then echo "✅ ci.yml tests passed"; else echo "❌ ci.yml tests failed"; fi
exit $FAILED
