import {
  ARMATURE_QUERY_MAX_LENGTH,
  ARMATURE_ROADMAP_LABEL_GAP_PX,
  ARMATURE_ROADMAP_GROUPINGS,
  ARMATURE_ROADMAP_MARGIN_DAYS,
  ARMATURE_ROADMAP_WEEKLY_DAYS,
  type ArmatureRoadmapGrouping,
} from "@/config";

export const ARMATURE_ROADMAP_NODE = "armatureRoadmap";

/** What a roadmap block stores: what to draw, never the days. */
export interface RoadmapSettings {
  project: string;
  query: string;
  groupBy: ArmatureRoadmapGrouping;
}

const PROJECT = /^[A-Z][A-Z0-9]{1,9}$/;
const DAY_MS = 86_400_000;

/** A block's attributes as the server takes them, each one wrong put right. */
export function roadmapSettings(attrs: Record<string, unknown> | undefined): RoadmapSettings {
  return {
    project: typeof attrs?.project === "string" && PROJECT.test(attrs.project) ? attrs.project : "",
    query: typeof attrs?.query === "string" ? [...attrs.query].slice(0, ARMATURE_QUERY_MAX_LENGTH).join("") : "",
    groupBy: (ARMATURE_ROADMAP_GROUPINGS as readonly string[]).includes(String(attrs?.groupBy)) ? (attrs!.groupBy as ArmatureRoadmapGrouping) : "epic",
  };
}

/** A new roadmap's settings: the project's open issues, under their epics. */
export function newRoadmapSettings(project = ""): RoadmapSettings {
  return { project, query: project ? `project = ${project} AND statusCategory != done` : "", groupBy: "epic" };
}

/** A day, YYYY-MM-DD, as a count of days, so spans are whole numbers. */
export function dayNumber(day: string): number {
  return Math.round(Date.parse(`${day}T00:00:00Z`) / DAY_MS);
}

export function dayOf(n: number): string {
  return new Date(n * DAY_MS).toISOString().slice(0, 10);
}

/** The days a roadmap's axis runs over, its first and its last, with a margin either side. */
export function axisOf(from: string, to: string, margin = ARMATURE_ROADMAP_MARGIN_DAYS): { first: number; last: number } {
  return { first: dayNumber(from) - margin, last: dayNumber(to) + margin };
}

export interface Placed {
  /** Where it starts and how wide it is, in percent of the axis. */
  left: number;
  width: number;
  /** A bar has both days; a point has one, its start or its due day. */
  shape: "bar" | "start" | "due";
}

/**
 * Where a bar sits on an axis of whole days: from the start of its first day
 * to the end of its last, so a one day bar has a width.
 */
export function place(start: string | null, due: string | null, axis: { first: number; last: number }): Placed | null {
  const span = axis.last - axis.first + 1;
  const at = (n: number) => ((n - axis.first) / span) * 100;
  if (start && due) {
    const a = dayNumber(start);
    const b = Math.max(a, dayNumber(due));
    return { left: at(a), width: at(b + 1) - at(a), shape: "bar" };
  }
  const one = start ?? due;
  if (!one) return null;
  return { left: at(dayNumber(one) + 0.5), width: 0, shape: start ? "start" : "due" };
}

export interface Tick {
  day: string;
  /** Where the tick sits, in percent of the axis. */
  at: number;
  /** A month's first day, else a Monday. */
  month: boolean;
}

/** Every how many ticks a label fits on an axis this many pixels wide; the others keep their line alone. */
export function labelEvery(ticks: readonly Tick[], width: number, gap = ARMATURE_ROADMAP_LABEL_GAP_PX): number {
  if (ticks.length < 2 || width <= 0) return 1;
  const apart = ((ticks[1]!.at - ticks[0]!.at) / 100) * width;
  return Math.max(1, Math.ceil(gap / apart));
}

/** The ticks an axis carries: each month's first day on a long one, each Monday on a short one. */
export function axisTicks(axis: { first: number; last: number }, weeklyWithin = ARMATURE_ROADMAP_WEEKLY_DAYS): Tick[] {
  const span = axis.last - axis.first + 1;
  const weekly = span <= weeklyWithin;
  const out: Tick[] = [];
  for (let n = axis.first; n <= axis.last; n++) {
    const d = new Date(n * DAY_MS);
    if (weekly ? d.getUTCDay() === 1 : d.getUTCDate() === 1) out.push({ day: dayOf(n), at: ((n - axis.first) / span) * 100, month: !weekly });
  }
  return out;
}
