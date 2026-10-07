import {
  ARMATURE_CHART_DEFAULT_DAYS,
  ARMATURE_CHART_GROUPINGS,
  ARMATURE_CHART_MAX_SLICES,
  ARMATURE_QUERY_MAX_LENGTH,
  type ArmatureChartGrouping,
} from "@/config";

export const ARMATURE_CHART_NODE = "armatureChart";

export type ArmatureChartKind = "pie" | "createdResolved";

/** What a chart block stores: what to count and how to draw it, never the counts. */
export interface ChartSettings {
  project: string;
  query: string;
  chart: ArmatureChartKind;
  groupBy: ArmatureChartGrouping;
  days: number;
}

const PROJECT = /^[A-Z][A-Z0-9]{1,9}$/;
const MIN_DAYS = 7;
const MAX_DAYS = 365;

/** A block's attributes as the server takes them, each one wrong put right. */
export function chartSettings(attrs: Record<string, unknown> | undefined): ChartSettings {
  const project = typeof attrs?.project === "string" && PROJECT.test(attrs.project) ? attrs.project : "";
  const query = typeof attrs?.query === "string" ? [...attrs.query].slice(0, ARMATURE_QUERY_MAX_LENGTH).join("") : "";
  const groupBy = (ARMATURE_CHART_GROUPINGS as readonly string[]).includes(String(attrs?.groupBy))
    ? (attrs!.groupBy as ArmatureChartGrouping)
    : "statusCategory";
  const days = Number(attrs?.days);
  return {
    project,
    query,
    chart: attrs?.chart === "createdResolved" ? "createdResolved" : "pie",
    groupBy,
    days: Number.isInteger(days) && days >= MIN_DAYS && days <= MAX_DAYS ? days : ARMATURE_CHART_DEFAULT_DAYS,
  };
}

/** A new chart's settings: the whole of a project, shared out by status category. */
export function newChartSettings(project = ""): ChartSettings {
  return { project, query: project ? `project = ${project}` : "", chart: "pie", groupBy: "statusCategory", days: ARMATURE_CHART_DEFAULT_DAYS };
}

export interface Slice {
  label: string;
  category: string;
  count: number;
  /** Set on the slice that holds what did not get one of its own. */
  other?: boolean;
}

/**
 * The slices a pie draws: the largest first, at most max, the rest in one
 * slice of their own, so no slice takes a colour nobody tells apart.
 */
export function foldSlices(slices: readonly Slice[], max: number = ARMATURE_CHART_MAX_SLICES, otherLabel = "Other"): Slice[] {
  const kept = slices.filter((s) => s.count > 0);
  if (kept.length <= max) return kept;
  const head = kept.slice(0, max - 1);
  const rest = kept.slice(max - 1).reduce((n, s) => n + s.count, 0);
  return [...head, { label: otherLabel, category: "", count: rest, other: true }];
}

/** A share of a whole, rounded for reading. */
export function percent(count: number, total: number): number {
  return total > 0 ? Math.round((count / total) * 100) : 0;
}

/**
 * The SVG path of each slice of a donut centred in a size by size box,
 * clockwise from twelve o'clock. A slice that is the whole is drawn as two
 * halves, since one arc cannot start and end at the same point.
 */
export function donutPaths(counts: readonly number[], size: number, hole: number): string[] {
  const total = counts.reduce((a, b) => a + b, 0);
  const r = size / 2;
  const inner = r * hole;
  const point = (radius: number, turn: number) => {
    const angle = turn * 2 * Math.PI - Math.PI / 2;
    return `${(r + radius * Math.cos(angle)).toFixed(2)} ${(r + radius * Math.sin(angle)).toFixed(2)}`;
  };
  const arc = (from: number, to: number) => {
    const large = to - from > 0.5 ? 1 : 0;
    return `M ${point(r, from)} A ${r} ${r} 0 ${large} 1 ${point(r, to)} L ${point(inner, to)} A ${inner} ${inner} 0 ${large} 0 ${point(inner, from)} Z`;
  };
  let at = 0;
  return counts.map((count) => {
    if (total === 0) return "";
    const share = count / total;
    const from = at;
    at += share;
    if (share >= 1) return `${arc(0, 0.5)} ${arc(0.5, 1)}`;
    return arc(from, at);
  });
}

/** Evenly spaced whole ticks from 0 that reach max, at most count of them past 0: issues come whole. */
export function ticks(max: number, count: number): number[] {
  if (max <= 0) return [0, 1];
  const raw = Math.max(1, max / count);
  const magnitude = 10 ** Math.floor(Math.log10(raw));
  const step = ([1, 2, 5, 10].map((m) => m * magnitude).find((s) => s >= raw) ?? raw) as number;
  const out: number[] = [];
  for (let v = 0; v < max + step; v += step) out.push(v);
  return out;
}

/** Where a readout centred on x would sit, kept inside a box width wide: its left edge, its middle or its right edge on x. */
export function readoutAnchor(x: number, width: number, readout: number): "start" | "middle" | "end" {
  if (x - readout / 2 < 0) return "start";
  if (x + readout / 2 > width) return "end";
  return "middle";
}

/** Where each value of a series falls in a width by height box, the first at the left, top as the highest tick. */
export function linePoints(values: readonly number[], width: number, height: number, top: number): Array<[number, number]> {
  const step = values.length > 1 ? width / (values.length - 1) : 0;
  return values.map((v, i) => [i * step, top > 0 ? height - (v / top) * height : height]);
}
