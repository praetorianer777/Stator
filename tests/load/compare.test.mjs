// Tests for compare.mjs, run by tests/test-load.sh under node --test: a run
// like the baseline passes, a planted regression fails.
import assert from "node:assert/strict";
import { test } from "node:test";
import { LATENCIES, NOISE_MS, TOLERANCE, baselineFrom, compare, table } from "./compare.mjs";

// A summary export as k6 writes it, every latency set to the same three numbers.
function summary({ med = 20, p95 = 100, p99 = 200, rate = 50, failed = 0, override = {} } = {}) {
  const metrics = {
    http_reqs: { count: 9000, rate },
    http_req_failed: { passes: 0, fails: 9000, value: failed },
  };
  for (const [what, metric] of LATENCIES) {
    metrics[metric] = { avg: med, med, "p(95)": p95, "p(99)": p99, max: p99, ...override[what] };
  }
  return { metrics };
}

const baseline = baselineFrom(summary(), "2026-10-12");

test("a baseline keeps p50, p95 and p99 of every kind, the request rate and the failure rate", () => {
  assert.equal(baseline.profile, "standard");
  assert.equal(baseline.measured, "2026-10-12");
  assert.deepEqual(baseline.latency_ms.search, { p50: 20, p95: 100, p99: 200 });
  assert.equal(Object.keys(baseline.latency_ms).length, LATENCIES.length);
  assert.equal(baseline.requests_per_second, 50);
  assert.equal(baseline.failure_rate, 0);
});

test("a run like the baseline passes", () => {
  const { rows, failed } = compare(baseline, summary());
  assert.equal(failed, false);
  assert.ok(rows.every((r) => r.verdict === "ok"));
});

test("a p95 grown within the tolerance passes", () => {
  const p95 = 100 * (1 + TOLERANCE) - 1;
  assert.equal(compare(baseline, summary({ override: { search: { "p(95)": p95 } } })).failed, false);
});

test("a planted regression in search fails, naming search p95", () => {
  const { rows, failed } = compare(baseline, summary({ override: { search: { "p(95)": 400 } } }));
  assert.equal(failed, true);
  assert.deepEqual(
    rows.filter((r) => r.verdict === "regression").map((r) => r.what),
    ["search p95"],
  );
  assert.match(table(rows), /search p95 +100 ms +400 ms +\+300 % +regression/);
});

test("growth below the noise floor never fails, however large in percent", () => {
  const small = baselineFrom(summary({ p95: 4 }), "2026-10-12");
  assert.equal(compare(small, summary({ p95: 4 + NOISE_MS })).failed, false);
  assert.equal(compare(small, summary({ p95: 4 + NOISE_MS + 1 })).failed, true);
});

test("a slower median or tail is flagged but does not fail", () => {
  const { rows, failed } = compare(baseline, summary({ override: { read: { med: 60, "p(99)": 900 } } }));
  assert.equal(failed, false);
  assert.deepEqual(
    rows.filter((r) => r.verdict === "slower").map((r) => r.what),
    ["read p50", "read p99"],
  );
});

test("fewer requests per second is flagged but does not fail", () => {
  const { rows, failed } = compare(baseline, summary({ rate: 30 }));
  assert.equal(failed, false);
  assert.equal(rows.find((r) => r.what === "requests per second").verdict, "slower");
});

test("any rise in the failure rate fails", () => {
  const { rows, failed } = compare(baseline, summary({ failed: 0.001 }));
  assert.equal(failed, true);
  assert.equal(rows.find((r) => r.what === "failed requests").verdict, "regression");
});

test("a run that skipped a kind of request is refused rather than compared", () => {
  const partial = summary();
  delete partial.metrics["http_req_duration{kind:search}"];
  assert.throws(() => compare(baseline, partial), /no numbers for search/);
});
