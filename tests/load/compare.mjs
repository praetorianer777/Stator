// Compares a k6 summary export of the standard profile with the committed
// baseline, or writes a new baseline from one. Runs under Node through make
// load-compare and make load-baseline; docs/load-testing.md says when.
//
//   node compare.mjs compare <baseline.json> <summary.json>
//   node compare.mjs baseline <summary.json> <baseline.json>
import { readFileSync, writeFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

/** How much a p95 may grow over the baseline before the comparison fails. */
export const TOLERANCE = 0.25;
/** Growth below this many milliseconds never counts: a 12 ms p95 jitters by more than a quarter. */
export const NOISE_MS = 10;

export const PROFILE = "standard";

// The numbers the baseline keeps, by the k6 metric that holds them. The
// submetrics exist because stator.js sets thresholds on them.
export const LATENCIES = [
  ["read", "http_req_duration{kind:read}"],
  ["write", "http_req_duration{kind:write}"],
  ["search", "http_req_duration{kind:search}"],
  ["tree", "http_req_duration{kind:tree}"],
  ["live delivery", "collab_delivery{scenario:live}"],
  ["open shared draft", "ws_connecting{scenario:live}"],
];
const STATS = [
  ["p50", "med"],
  ["p95", "p(95)"],
  ["p99", "p(99)"],
];
// Only p95 gates; the others are shown and flagged, as the median and the
// tail move with the machine more than p95 does.
const GATING_STAT = "p95";

const round = (n) => Math.round(n * 10) / 10;

class UsageError extends Error {}

/** The numbers of a k6 summary export, in the baseline's shape. */
export function numbers(summary) {
  const metrics = summary.metrics || {};
  const latency = {};
  const missing = [];
  for (const [what, metric] of LATENCIES) {
    const m = metrics[metric];
    if (!m) {
      missing.push(what);
      continue;
    }
    latency[what] = Object.fromEntries(STATS.map(([stat, key]) => [stat, round(m[key])]));
  }
  if (missing.length) {
    throw new UsageError(
      `The run has no numbers for ${missing.join(", ")}. Run the standard profile with every scenario and nothing skipped, as make load-compare does.`,
    );
  }
  return {
    latency_ms: latency,
    requests_per_second: round(metrics.http_reqs?.rate ?? 0),
    failure_rate: metrics.http_req_failed?.value ?? 0,
  };
}

/** A baseline written from a summary export, dated today unless told otherwise. */
export function baselineFrom(summary, measured = new Date().toISOString().slice(0, 10)) {
  return { profile: PROFILE, measured, ...numbers(summary) };
}

const grewTooMuch = (was, now) => now - was > NOISE_MS && now > was * (1 + TOLERANCE);

/** Every number side by side, and which of them are regressions or only slower. */
export function compare(baseline, summary) {
  const now = numbers(summary);
  const rows = [];
  for (const [what] of LATENCIES) {
    const was = baseline.latency_ms?.[what];
    if (!was) throw new UsageError(`The baseline has no numbers for ${what}. Write it again with make load-baseline.`);
    for (const [stat] of STATS) {
      const a = was[stat];
      const b = now.latency_ms[what][stat];
      let verdict = "ok";
      if (grewTooMuch(a, b)) verdict = stat === GATING_STAT ? "regression" : "slower";
      rows.push({ what: `${what} ${stat}`, baseline: a, now: b, unit: "ms", verdict });
    }
  }
  const rps = baseline.requests_per_second;
  rows.push({
    what: "requests per second",
    baseline: rps,
    now: now.requests_per_second,
    unit: "",
    verdict: now.requests_per_second < rps * (1 - TOLERANCE) ? "slower" : "ok",
  });
  rows.push({
    what: "failed requests",
    baseline: baseline.failure_rate,
    now: now.failure_rate,
    unit: "%",
    verdict: now.failure_rate > baseline.failure_rate ? "regression" : "ok",
  });
  return { rows, failed: rows.some((r) => r.verdict === "regression") };
}

function show(value, unit) {
  if (unit === "%") return `${round(value * 100)} %`;
  return unit ? `${value} ${unit}` : `${value}`;
}

function change(row) {
  if (row.unit === "%") return row.now === row.baseline ? "0" : `${row.now > row.baseline ? "+" : ""}${round((row.now - row.baseline) * 100)} pt`;
  if (row.baseline === 0) return row.now === 0 ? "0 %" : "new";
  const pct = Math.round(((row.now - row.baseline) / row.baseline) * 100);
  return `${pct > 0 ? "+" : ""}${pct} %`;
}

/** The comparison as a plain text table, one line per number. */
export function table(rows) {
  const lines = [["what", "baseline", "now", "change", ""]].concat(
    rows.map((r) => [r.what, show(r.baseline, r.unit), show(r.now, r.unit), change(r), r.verdict === "ok" ? "" : r.verdict]),
  );
  const widths = lines[0].map((_, i) => Math.max(...lines.map((l) => l[i].length)));
  return lines
    .map((l) => l.map((cell, i) => (i === 0 ? cell.padEnd(widths[i]) : cell.padStart(widths[i]))).join("  ").trimEnd())
    .join("\n");
}

function read(path) {
  try {
    return JSON.parse(readFileSync(path, "utf8"));
  } catch (e) {
    throw new UsageError(`Cannot read ${path} (${e.message}). Run make load-compare or make test-load first, so the run's summary exists.`);
  }
}

function main([command, ...args]) {
  if (command === "compare" && args.length === 2) {
    const baseline = read(args[0]);
    const { rows, failed } = compare(baseline, read(args[1]));
    console.log(`Standard profile against the baseline of ${baseline.measured}; a p95 may grow by ${TOLERANCE * 100} % and at least ${NOISE_MS} ms.\n`);
    console.log(table(rows));
    console.log(
      failed
        ? "\nSlower than the baseline. Find the cause as docs/load-testing.md says, or, when the change means to cost this, write a new baseline with make load-baseline and say why in the pull request."
        : "\nNo regression against the baseline.",
    );
    return failed ? 1 : 0;
  }
  if (command === "baseline" && args.length === 2) {
    writeFileSync(args[1], `${JSON.stringify(baselineFrom(read(args[0])), null, 2)}\n`);
    console.log(`Wrote ${args[1]}. Commit it with the change that moves the numbers, and say why in the pull request.`);
    return 0;
  }
  throw new UsageError("Use: compare.mjs compare <baseline.json> <summary.json>, or compare.mjs baseline <summary.json> <baseline.json>.");
}

// Imported from a test or from node -e, argv[1] is not this file.
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    process.exitCode = main(process.argv.slice(2));
  } catch (e) {
    if (!(e instanceof UsageError)) throw e;
    console.error(e.message);
    process.exitCode = 2;
  }
}
