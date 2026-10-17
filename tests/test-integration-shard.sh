#!/usr/bin/env bash
# Tests for scripts/integration-shard.sh: how CI splits the integration suite
# across jobs. Feeds it a listing shaped like `go test -list` and matches the
# patterns it prints the way `go test -run` would; needs bash and grep only.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"
SHARD="$SRC/scripts/integration-shard.sh"

FAILED=0
pass() { echo "   ✅ $1"; }
fail() { echo "   ❌ $1"; FAILED=1; }
check() { if [[ "$2" == "$3" ]]; then pass "$1"; else fail "$1: expected [$3], got [$2]"; fi; }

LISTING=$'TestPage\nTestPageTree\nTestAudit\nTestSearch\nTestBackupRestore\nExampleSlug\nok  \tgithub.com/praetorianer777/stator/backend/test\t0.04s'
NAMES=$(grep -E '^[A-Z]' <<< "$LISTING" | sort)

selected() { # pattern...: the listed names the patterns select, one line per selection
  local pattern
  for pattern in "$@"; do grep -E "$pattern" <<< "$NAMES"; done | sort
}

echo "🧪 integration-shard.sh splits the suite"

patterns=()
for p in 1 2 3; do patterns+=("$("$SHARD" "$p/3" <<< "$LISTING")"); done
check "the three parts select every test once between them" "$(selected "${patterns[@]}")" "$NAMES"

check "a test whose name begins another's lands in its own part only" \
  "$(grep -cE "${patterns[0]}|${patterns[1]}|${patterns[2]}" <<< TestPageTreeMove)" "0"

check "the split does not depend on the listing's order" \
  "$("$SHARD" 2/3 <<< "$(tac <<< "$LISTING")")" "${patterns[1]}"

check "one part selects the whole suite" "$(selected "$("$SHARD" 1/1 <<< "$LISTING")")" "$NAMES"

check "a part beyond the tests selects none" "$(selected "$("$SHARD" 8/8 <<< "$LISTING")")" ""

"$SHARD" 4/3 <<< "$LISTING" >/dev/null 2>&1; check "a part beyond the count is refused" "$?" "2"
"$SHARD" <<< "$LISTING" >/dev/null 2>&1; check "a missing part is refused" "$?" "2"
"$SHARD" 1/3 <<< $'ok  \tpkg\t0.01s' >/dev/null 2>&1; check "a listing without tests fails" "$?" "1"

if [[ $FAILED -eq 0 ]]; then echo "✅ integration-shard.sh tests passed"; else echo "❌ integration-shard.sh tests failed"; fi
exit $FAILED
