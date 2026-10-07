import { normalizeLabel } from "@/api/labels";
import { PROPERTIES_REPORT_MAX_COLUMNS, PROPERTIES_REPORT_MAX_LABELS, PROPERTY_KEY_MAX_LENGTH } from "@/config";

export const PROPERTIES_REPORT_NODE = "propertiesReport";

/** What a properties report block stores: what to gather, never the rows. */
export interface ReportSettings {
  labels: string[];
  /** A space's key to stay inside, or null for every space. */
  space: string | null;
  /** The properties to show, in order; every one found when empty. */
  columns: string[];
}

const SPACE = /^[A-Z][A-Z0-9]{1,9}$/;

/** A name as the server compares them: its words, one space apart. */
export function columnName(raw: string): string {
  return raw.trim().split(/\s+/).filter(Boolean).join(" ");
}

/** Labels as the server stores them, those it would refuse and repeats left out, at most as many as a report takes. */
export function reportLabels(raw: readonly unknown[]): string[] {
  const out: string[] = [];
  for (const item of raw) {
    if (typeof item !== "string") continue;
    const { name, problem } = normalizeLabel(item);
    if (!problem && !out.includes(name)) out.push(name);
  }
  return out.slice(0, PROPERTIES_REPORT_MAX_LABELS);
}

/** Column names tidied, the empty and the too long left out, each once whatever its case. */
export function reportColumns(raw: readonly unknown[]): string[] {
  const out: string[] = [];
  for (const item of raw) {
    if (typeof item !== "string") continue;
    const name = columnName(item);
    if (name && [...name].length <= PROPERTY_KEY_MAX_LENGTH && !out.some((c) => c.toLowerCase() === name.toLowerCase())) out.push(name);
  }
  return out.slice(0, PROPERTIES_REPORT_MAX_COLUMNS);
}

/** A block's attributes as the server takes them, each one wrong put right. */
export function reportSettings(attrs: Record<string, unknown> | undefined): ReportSettings {
  return {
    labels: reportLabels(Array.isArray(attrs?.labels) ? attrs.labels : []),
    space: typeof attrs?.space === "string" && SPACE.test(attrs.space) ? attrs.space : null,
    columns: reportColumns(Array.isArray(attrs?.columns) ? attrs.columns : []),
  };
}

/** Orders two values as a reader expects: numbers by size, words by the locale, the empty last. */
export function compareValues(a: string | null, b: string | null, locale: string): number {
  if (!a || !b) return a ? -1 : b ? 1 : 0;
  return a.localeCompare(b, locale, { numeric: true, sensitivity: "base" });
}
