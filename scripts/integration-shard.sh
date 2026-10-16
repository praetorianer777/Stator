#!/usr/bin/env bash
# Prints the `go test -run` pattern for one part of the integration suite, as
# CI splits it across jobs. Reads the output of `go test -list` on stdin and
# deals the sorted test names out in turn, so every job derives the same split
# from the same commit. Before printing, it checks that the parts between them
# select every listed test exactly once, and fails the run when one does not.
#
#   go test -tags integration -list . ./test/... | scripts/integration-shard.sh 2/3
set -euo pipefail

shard="${1:-}"
if [[ ! "$shard" =~ ^([1-9][0-9]*)/([1-9][0-9]*)$ ]] || (( BASH_REMATCH[1] > BASH_REMATCH[2] )); then
  echo "Name the part as <i>/<n> with 1 <= i <= n, for example 2/3; got '$shard'." >&2
  exit 2
fi
part=${BASH_REMATCH[1]}
parts=${BASH_REMATCH[2]}

# `go test -list` ends each package with an ok line; only identifiers are tests.
mapfile -t names < <(grep -E '^[A-Z][A-Za-z0-9_]*$' | LC_ALL=C sort -u)
if (( ${#names[@]} == 0 )); then
  echo "go test -list named no tests, so there is nothing to split; check that the integration package builds." >&2
  exit 1
fi

pattern_of() {
  local i joined=""
  for i in "${!names[@]}"; do
    (( i % parts == $1 - 1 )) && joined+="${joined:+|}${names[$i]}"
  done
  # An empty part matches nothing rather than everything.
  echo "^(${joined:-\$NONE})\$"
}

patterns=()
for p in $(seq "$parts"); do patterns+=("$(pattern_of "$p")"); done

bad=""
for name in "${names[@]}"; do
  hits=0
  for pattern in "${patterns[@]}"; do
    grep -qE "$pattern" <<< "$name" && hits=$((hits + 1))
  done
  (( hits == 1 )) || bad+=" $name ($hits parts)"
done
if [[ -n "$bad" ]]; then
  echo "These integration tests are not in exactly one of $parts parts:$bad. Fix scripts/integration-shard.sh." >&2
  exit 1
fi

echo "${patterns[$((part - 1))]}"
