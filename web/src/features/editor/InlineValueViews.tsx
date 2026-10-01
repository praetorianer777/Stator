import { Icon } from "@/components/icons";
import { STATUS_LABEL_MAX_LENGTH } from "@/config";
import { locale as activeLocale, t } from "@/i18n";
import { STATUS_COLORS, type StatusColor } from "./schema";

// Kept apart from the editor's nodes, so the read-only view draws a status
// and a date without bringing the editor along.

export const STATUS_NODE = "status";
export const DATE_NODE = "date";

export interface StatusAttrs {
  label: string;
  color: StatusColor;
}

const DAY = /^\d{4}-\d{2}-\d{2}$/;
const DAY_LENGTH = "YYYY-MM-DD".length;
const MIDNIGHT_UTC = "T00:00:00Z";

/** A day that exists, written YYYY-MM-DD as the API takes it, or null. */
export function isoDay(value: unknown): string | null {
  if (typeof value !== "string" || !DAY.test(value)) return null;
  const parsed = new Date(`${value}${MIDNIGHT_UTC}`);
  return !Number.isNaN(parsed.getTime()) && parsed.toISOString().slice(0, DAY_LENGTH) === value ? value : null;
}

/** Today in the author's own calendar. */
export function today(now: Date = new Date()): string {
  const pad = (n: number, width: number) => String(n).padStart(width, "0");
  return `${pad(now.getFullYear(), 4)}-${pad(now.getMonth() + 1, 2)}-${pad(now.getDate(), 2)}`;
}

/** A day in the reader's locale. It is read in UTC so it is the same day wherever the reader is. */
export function formatDay(day: string, locale: string = activeLocale()): string {
  return new Intl.DateTimeFormat(locale, { dateStyle: "medium", timeZone: "UTC" }).format(new Date(`${day}${MIDNIGHT_UTC}`));
}

export function statusColor(value: unknown): StatusColor {
  return typeof value === "string" && (STATUS_COLORS as readonly string[]).includes(value) ? (value as StatusColor) : "neutral";
}

/** A label the API takes: some words, cut at its limit; null when there are none. */
export function statusLabel(value: unknown): string | null {
  if (typeof value !== "string" || !value.trim()) return null;
  return [...value].slice(0, STATUS_LABEL_MAX_LENGTH).join("");
}

/** A status as readers see it; the colour is a theme role, so the words carry the meaning. */
export function StatusLabel({ label, color }: { label: string; color: StatusColor }) {
  return (
    <span className="doc-status" data-status-label={color} data-label={label}>
      <span className="sr-only">{t.inlineValues.status.prefix} </span>
      {label}
    </span>
  );
}

export function DateChip({ day }: { day: string }) {
  return (
    <time className="doc-date" dateTime={day} data-date={day}>
      <Icon.Calendar size={12} />
      {formatDay(day)}
    </time>
  );
}
