#!/usr/bin/env bash
# The one entry point for the whole suite: the branch-guard hook runs this
# before every push, and ci.yml has no other step.
set -euo pipefail
cd "$(dirname "$0")"

echo "🐚 Shell tests"
./tests/test-release.sh
./tests/test-helm.sh
./.claude/hooks/tests/branch-guard-test.sh

# Each layer joins the gate in the change that introduces it, through a
# Makefile target of the same name, so this file only decides the order.
has_target() { [[ -f Makefile ]] && make -n "$1" >/dev/null 2>&1; }

for layer in check-go check-web; do
  if has_target "$layer"; then
    echo "🔍 $layer"
    make "$layer"
  fi
done

if has_target stack-up; then
  # One compose project and port range per checkout, so gates running in
  # parallel worktrees never share a database or a port.
  trap 'make stack-down >/dev/null 2>&1 || true' EXIT
  echo "🐳 Stack"
  make stack-up
  for layer in test-integration test-e2e; do
    if has_target "$layer"; then
      echo "🧪 $layer"
      make "$layer"
    fi
  done
fi

echo "✅ All tests passed"
