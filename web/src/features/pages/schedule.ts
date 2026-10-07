import { SCHEDULE_DEFAULT_LEAD_MINUTES, SCHEDULE_MAX_AHEAD_DAYS } from "@/config";
import { localDateFormat } from "@/lib/format";

const MINUTE_MS = 60_000;
const HOUR_MS = 60 * MINUTE_MS;
const DAY_MS = 24 * HOUR_MS;

const pad = (n: number) => String(n).padStart(2, "0");

/** When a scheduled publish goes out, in the reader's zone, naming the zone. */
export const scheduleTimeFormat = localDateFormat({
  weekday: "short",
  day: "numeric",
  month: "short",
  year: "numeric",
  hour: "2-digit",
  minute: "2-digit",
  timeZoneName: "short",
});

/** A time as a datetime-local field writes it, in the reader's zone. */
export function localInputValue(date: Date): string {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/** The time the publish dialog offers first: the lead ahead, rounded up to the hour. */
export function defaultScheduleTime(now: Date): Date {
  // Rounded on the reader's clock, which in some zones is not on the hour in UTC.
  const at = new Date(now.getTime() + SCHEDULE_DEFAULT_LEAD_MINUTES * MINUTE_MS);
  if (at.getMinutes() || at.getSeconds() || at.getMilliseconds()) at.setMinutes(60, 0, 0);
  return at;
}

/** The latest time a publish may be set for, from now. */
export function latestScheduleTime(now: Date): Date {
  return new Date(now.getTime() + SCHEDULE_MAX_AHEAD_DAYS * DAY_MS);
}

/** What a datetime-local field holds, read in the reader's zone; null when it holds no time. */
export function parseLocalInput(value: string): Date | null {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}/.test(value)) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
}

/** The reader's time zone by name, as the publish dialog tells it. */
export function localZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone;
}
