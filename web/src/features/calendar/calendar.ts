import { CALENDAR_DEFAULT_EVENT_MINUTES, CALENDAR_DEFAULT_START_HOUR, CALENDAR_EVENT_KINDS, CALENDAR_WEEK_START } from "@/config";

export const CALENDAR_NODE = "calendar";

export type CalendarKind = (typeof CALENDAR_EVENT_KINDS)[number];

/** What a calendar block stores: which calendar, and which Armature project's due issues; never the events. */
export interface CalendarSettings {
  calendarId: string;
  /** An Armature project's key, or null for the calendar's own events alone. */
  project: string | null;
}

const ID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const PROJECT = /^[A-Z][A-Z0-9]{1,9}$/;

/** A block's attributes as the server takes them; an id that is none is empty, which the block says. */
export function calendarSettings(attrs: Record<string, unknown> | undefined): CalendarSettings {
  return {
    calendarId: typeof attrs?.calendarId === "string" && ID.test(attrs.calendarId) ? attrs.calendarId : "",
    project: typeof attrs?.project === "string" && PROJECT.test(attrs.project) ? attrs.project : null,
  };
}

/** A month of the reader's calendar, January as 0, as Date counts them. */
export interface Month {
  year: number;
  month: number;
}

const pad = (n: number) => String(n).padStart(2, "0");

/** A day in the reader's time zone as YYYY-MM-DD. */
export function localDay(date: Date): string {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

export function monthOf(date: Date): Month {
  return { year: date.getFullYear(), month: date.getMonth() };
}

export function shiftMonth(m: Month, by: number): Month {
  const d = new Date(m.year, m.month + by, 1);
  return monthOf(d);
}

/** The month as Armature names it, YYYY-MM. */
export function monthKey(m: Month): string {
  return `${m.year}-${pad(m.month + 1)}`;
}

/** The instants a month spans in the reader's time zone: its first midnight, and the next month's. */
export function monthSpan(m: Month): { from: string; to: string } {
  return { from: new Date(m.year, m.month, 1).toISOString(), to: new Date(m.year, m.month + 1, 1).toISOString() };
}

/**
 * The month as weeks of seven cells, each a day of it or null before its
 * first and after its last, weeks starting on CALENDAR_WEEK_START.
 */
export function monthWeeks(m: Month): (string | null)[][] {
  const first = new Date(m.year, m.month, 1);
  const days = new Date(m.year, m.month + 1, 0).getDate();
  const lead = (first.getDay() - CALENDAR_WEEK_START + 7) % 7;
  const cells: (string | null)[] = Array.from({ length: lead }, () => null);
  for (let d = 1; d <= days; d++) cells.push(localDay(new Date(m.year, m.month, d)));
  while (cells.length % 7 !== 0) cells.push(null);
  const weeks: (string | null)[][] = [];
  for (let i = 0; i < cells.length; i += 7) weeks.push(cells.slice(i, i + 7));
  return weeks;
}

/** The weekdays in the order a week of the calendar shows them, 0 for Sunday. */
export function weekdays(): number[] {
  return Array.from({ length: 7 }, (_, i) => (CALENDAR_WEEK_START + i) % 7);
}

/** The span of an entry as days: whole ones by their UTC date, timed ones by the reader's. */
export interface Timed {
  allDay: boolean;
  start: string;
  end: string;
}

/** The first and last day an event covers, YYYY-MM-DD, as the reader sees it. */
export function eventDays(e: Timed): { first: string; last: string } {
  if (e.allDay) return { first: e.start.slice(0, 10), last: e.end.slice(0, 10) };
  const start = new Date(e.start);
  const end = new Date(e.end);
  // A meeting that ends at midnight does not reach into the day that begins.
  const lastInstant = end > start && end.getHours() === 0 && end.getMinutes() === 0 ? new Date(end.getTime() - 1) : end;
  return { first: localDay(start), last: localDay(lastInstant) };
}

/** Whether an event covers a day. */
export function onDay(e: Timed, day: string): boolean {
  const { first, last } = eventDays(e);
  return first <= day && day <= last;
}

/** The time an event starts in the reader's zone, as HH:MM, for its first day; nothing for whole days. */
export function startTime(e: Timed): string {
  if (e.allDay) return "";
  const d = new Date(e.start);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** What the event dialog edits: days for whole ones, local times as datetime-local writes them for the rest. */
export interface EventDraft {
  title: string;
  kind: CalendarKind;
  allDay: boolean;
  startDay: string;
  endDay: string;
  startTime: string;
  endTime: string;
}

function localTime(date: Date): string {
  return `${localDay(date)}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/** A new event on a day: all day, with times ready should the author want them. */
export function newDraft(day: string): EventDraft {
  const [y, m, d] = day.split("-").map(Number) as [number, number, number];
  const start = new Date(y, m - 1, d, CALENDAR_DEFAULT_START_HOUR);
  const end = new Date(start.getTime() + CALENDAR_DEFAULT_EVENT_MINUTES * 60_000);
  return { title: "", kind: "event", allDay: true, startDay: day, endDay: day, startTime: localTime(start), endTime: localTime(end) };
}

/** An event as the dialog edits it. */
export function draftOf(e: Timed & { title: string; kind: string }): EventDraft {
  const { first, last } = eventDays(e);
  const kind = (CALENDAR_EVENT_KINDS as readonly string[]).includes(e.kind) ? (e.kind as CalendarKind) : "event";
  if (e.allDay) {
    const times = newDraft(first);
    return { ...times, title: e.title, kind, endDay: last };
  }
  return { title: e.title, kind, allDay: false, startDay: first, endDay: last, startTime: localTime(new Date(e.start)), endTime: localTime(new Date(e.end)) };
}

/** The dialog's draft as the API takes it, or the field that is wrong. */
export function inputOf(
  d: EventDraft,
): { input: { title: string; kind: CalendarKind; allDay: boolean; start: string; end: string } } | { problem: "title" | "start" | "end" } {
  const title = d.title.trim();
  if (!title) return { problem: "title" };
  if (d.allDay) {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(d.startDay)) return { problem: "start" };
    if (!/^\d{4}-\d{2}-\d{2}$/.test(d.endDay) || d.endDay < d.startDay) return { problem: "end" };
    return { input: { title, kind: d.kind, allDay: true, start: `${d.startDay}T00:00:00Z`, end: `${d.endDay}T00:00:00Z` } };
  }
  const start = new Date(d.startTime);
  const end = new Date(d.endTime);
  if (Number.isNaN(start.getTime())) return { problem: "start" };
  if (Number.isNaN(end.getTime()) || end < start) return { problem: "end" };
  return { input: { title, kind: d.kind, allDay: false, start: start.toISOString(), end: end.toISOString() } };
}
