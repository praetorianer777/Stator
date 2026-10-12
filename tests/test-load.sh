#!/usr/bin/env bash
# Tests for tests/load/compare.mjs, the comparison of a load run with the
# committed baseline, and that the baseline holds every number it compares.
# Node runs in a container; no stack and no load are needed.
set -u

SRC="$(cd "$(dirname "$0")/.." && pwd)"
NODE_IMAGE="${NODE_IMAGE:-node:26-alpine}"

node() {
    docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -v "${SRC}/tests/load:/load:ro" -w /load "${NODE_IMAGE}" node "$@"
}

FAILED=0
echo "🧪 load comparison"
node --test compare.test.mjs || FAILED=1

# The committed baseline compared with a run of exactly its numbers: it reads,
# it has every kind, and it passes against itself.
check_baseline='
import { readFileSync } from "node:fs";
import { LATENCIES, compare } from "./compare.mjs";
const b = JSON.parse(readFileSync("baseline.json", "utf8"));
const metrics = { http_reqs: { rate: b.requests_per_second }, http_req_failed: { value: b.failure_rate } };
for (const [what, metric] of LATENCIES) {
  const l = b.latency_ms[what];
  metrics[metric] = { med: l.p50, "p(95)": l.p95, "p(99)": l.p99 };
}
if (compare(b, { metrics }).failed) throw new Error("the baseline fails against itself");
'
if node --input-type=module -e "$check_baseline"; then
    echo "   ✅ tests/load/baseline.json holds every number and passes against itself"
else
    echo "   ❌ tests/load/baseline.json is not a baseline compare.mjs can use; write it with make load-baseline"
    FAILED=1
fi

if [[ $FAILED -eq 0 ]]; then echo "✅ load comparison tests passed"; else echo "❌ load comparison tests failed"; fi
exit $FAILED
