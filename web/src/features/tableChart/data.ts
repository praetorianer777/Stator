import { TABLE_CHARTS, TABLE_CHART_MAX_SERIES } from "@/config";
import type { DocNode } from "@/features/editor/schema";

export const TABLE_CHART_NODE = "tableChart";

export type TableChartKind = (typeof TABLE_CHARTS)[number];

/** What a chart from a table stores beside its table: what to draw, and whether readers see the table too. */
export interface TableChartSettings {
  chart: TableChartKind;
  showTable: boolean;
}

/** A block's attributes as the server takes them, each one wrong put right. */
export function tableChartSettings(attrs: Record<string, unknown> | undefined): TableChartSettings {
  return {
    chart: (TABLE_CHARTS as readonly unknown[]).includes(attrs?.chart) ? (attrs!.chart as TableChartKind) : "bar",
    showTable: attrs?.showTable !== false,
  };
}

export interface Series {
  name: string;
  /** One per category; null where the cell holds no number. */
  values: Array<number | null>;
}

/** A table read as a chart reads it: the first row names the series, the first column the categories. */
export interface ChartData {
  categories: string[];
  series: Series[];
  /** How many columns of numbers past the most a chart draws were left out. */
  dropped: number;
}

/** The words of a cell, or of any node, as a reader sees them. */
export function cellText(node: DocNode | undefined): string {
  if (!node) return "";
  if (node.type === "text") return node.text ?? "";
  if (node.type === "hardBreak") return " ";
  const label = node.attrs?.label ?? node.attrs?.date ?? node.attrs?.key ?? node.attrs?.latex;
  if (node.type !== "paragraph" && typeof label === "string") return label;
  const words = (node.content ?? []).map(cellText);
  return (node.type === "tableCell" || node.type === "tableHeader" ? words.join(" ") : words.join("")).trim();
}

const GROUPED = /^\d{1,3}([,.']\d{3})+$/;
// A sign, a currency before or after, a percent or a short unit after: Q1 or v2 are names, not numbers.
const NUMBER = /^([+\-\u2212]?)\p{Sc}?([+\-\u2212]?)([\d.,']*\d[\d.,']*)(?:%|\p{Sc}|\p{L}{1,4})?$/u;

/**
 * A number as people write one in a table: 1,234.5 or 1.234,5, with a sign, a
 * percent, a unit or a currency around it. Null for anything else, so a word is no zero.
 */
export function parseNumber(raw: string): number | null {
  const m = NUMBER.exec(raw.replace(/[\s\u00a0]/g, ""));
  if (!m) return null;
  const negative = Boolean(m[1] || m[2]) && (m[1] || m[2]) !== "+";
  let s = m[3]!;
  if (GROUPED.test(s) && !(s.includes(",") && s.includes("."))) {
    // 1,234 and 1.234 are both a thousand and more: a decimal part three digits long is rare in a table.
    s = s.replace(/[,.']/g, "");
  } else {
    s = s.replace(/'/g, "");
    const decimal = s.lastIndexOf(",") > s.lastIndexOf(".") ? "," : ".";
    s = s
      .split(decimal === "," ? "." : ",")
      .join("")
      .replace(decimal, ".");
  }
  if (!/^\d*\.?\d+$/.test(s)) return null;
  const n = Number(s);
  return negative ? -n : n;
}

/**
 * The categories and series of a table. A column with no number in it is no
 * series, so a column of notes does not draw as zeros.
 */
export function chartData(table: DocNode | undefined): ChartData {
  const rows = (table?.content ?? []).map((row) => (row.content ?? []).map(cellText));
  const [head = [], ...body] = rows;
  const categories = body.map((row) => row[0] ?? "");
  const width = Math.max(head.length, ...body.map((row) => row.length), 0);
  const all: Series[] = [];
  for (let col = 1; col < width; col++) {
    const values = body.map((row) => parseNumber(row[col] ?? ""));
    if (values.some((v) => v !== null)) all.push({ name: head[col] || `${col + 1}`, values });
  }
  return { categories, series: all.slice(0, TABLE_CHART_MAX_SERIES), dropped: Math.max(0, all.length - TABLE_CHART_MAX_SERIES) };
}

/** Evenly spaced round ticks that take in min and max, 0 among them, at most about count steps. */
export function axisTicks(min: number, max: number, count: number): number[] {
  const low = Math.min(0, min);
  const high = Math.max(0, max);
  if (high === low) return [0, 1];
  const raw = (high - low) / count;
  const magnitude = 10 ** Math.floor(Math.log10(raw));
  const step = [1, 2, 2.5, 5, 10].map((m) => m * magnitude).find((s) => s >= raw) ?? raw;
  const out: number[] = [];
  // Rounding keeps a tick of 0.30000000000000004 reading as 0.3.
  const round = (v: number) => Number(v.toPrecision(12));
  for (let v = Math.floor(low / step) * step; v < high + step / 2; v += step) out.push(round(v));
  if (out[out.length - 1]! < high) out.push(round(out[out.length - 1]! + step));
  return out;
}

/** Where a value falls on a height-tall axis from its lowest tick to its highest, the top being the highest. */
export function yOf(value: number, ticks: readonly number[], height: number): number {
  const low = ticks[0]!;
  const high = ticks[ticks.length - 1]!;
  return high === low ? height : height - ((value - low) / (high - low)) * height;
}
