import { useMemo, useState, type ReactNode } from "react";
import { ApiError } from "@/api/client";
import { useArmatureAccount, useArmatureCalendar, type ArmatureCalendarAnswer } from "@/api/armature";
import { useCalendarEvents, type CalendarEvent } from "@/api/calendars";
import { Button, IconButton } from "@/components/ui";
import { Icon } from "@/components/icons";
import { ARMATURE_SECTION_ID, PROFILE_PATH } from "@/config";
import { locale, t } from "@/i18n";
import { localDay, monthKey, monthOf, monthSpan, monthWeeks, onDay, shiftMonth, startTime, weekdays, type CalendarSettings, type Month } from "./calendar";
import { EventDialog } from "./EventDialog";

type Issue = NonNullable<ArmatureCalendarAnswer["month"]>["issues"][number];

function dayDate(day: string): Date {
  const [y, m, d] = day.split("-").map(Number) as [number, number, number];
  return new Date(y, m - 1, d);
}

const longDay = (day: string) => new Intl.DateTimeFormat(locale(), { weekday: "long", day: "numeric", month: "long", year: "numeric" }).format(dayDate(day));
const monthLabel = (m: Month) => new Intl.DateTimeFormat(locale(), { month: "long", year: "numeric" }).format(new Date(m.year, m.month, 1));
// A Sunday that is known, from which the weekday names are read.
const weekdayName = (weekday: number, style: "short" | "long") => new Intl.DateTimeFormat(locale(), { weekday: style }).format(new Date(2026, 0, 4 + weekday));

/** One event in a day's cell: a button for whoever may change it, words otherwise. */
function EventEntry({ event, day, onOpen }: { event: CalendarEvent; day: string; onOpen: (() => void) | null }) {
  const c = t.calendar;
  const time = event.allDay ? "" : startTime(event);
  // A meeting shows its time on the day it starts; the days after, only its title.
  const showTime = time && localDay(new Date(event.start)) === day;
  const words = (
    <>
      {showTime && <span className="doc-calendar-time">{time}</span>}
      {event.kind === "absence" && <span className="sr-only">{c.absence}: </span>}
      <span className="min-w-0 break-words">{event.title}</span>
    </>
  );
  return (
    <li className="doc-calendar-entry" data-calendar-event={event.title} data-kind={event.kind} data-all-day={event.allDay || undefined}>
      {onOpen ? (
        <button type="button" className="doc-calendar-entry-body" onClick={onOpen} aria-label={c.editEvent(event.title)}>
          {words}
        </button>
      ) : (
        <span className="doc-calendar-entry-body">{words}</span>
      )}
    </li>
  );
}

function IssueEntry({ issue, inEditor }: { issue: Issue; inEditor: boolean }) {
  const c = t.calendar;
  const words = (
    <>
      <span className="doc-calendar-key">{issue.key}</span> <span className="min-w-0 break-words">{issue.summary}</span>
    </>
  );
  return (
    <li className="doc-calendar-entry" data-calendar-issue={issue.key} data-kind="issue" data-category={issue.category} data-done={issue.done || undefined}>
      <span className="sr-only">{c.due}: </span>
      {/* In the editor a link would take the author off the page mid-sentence. */}
      {inEditor ? (
        <span className="doc-calendar-entry-body">{words}</span>
      ) : (
        <a href={issue.url} target="_blank" rel="noopener noreferrer" className="doc-calendar-entry-body">
          {words}
        </a>
      )}
    </li>
  );
}

/** What the block says of Armature's due issues when it cannot show them. */
function useIssues(project: string | null, month: Month): { issues: Issue[]; note: ReactNode } {
  const i = t.calendar.issues;
  const account = useArmatureAccount();
  const configured = Boolean(account.data?.configured && account.data.baseUrl);
  const asks = Boolean(project && configured && account.data?.connected && account.data.status !== "rejected");
  const answer = useArmatureCalendar(project, monthKey(month), asks);
  if (!project || account.isPending) return { issues: [], note: null };
  if (!configured || answer.data?.status === "not_configured") return { issues: [], note: i.notConfigured };
  if (!asks || answer.data?.status === "not_connected" || answer.data?.status === "rejected") {
    return {
      issues: [],
      note: (
        <>
          {i.connect}{" "}
          <a href={`${PROFILE_PATH}#${ARMATURE_SECTION_ID}`} className="underline" data-action="connect-armature-hint">
            {t.armature.list.connectLink}
          </a>
        </>
      ),
    };
  }
  if (answer.error instanceof ApiError && answer.error.fields.project) return { issues: [], note: answer.error.fields.project };
  if (answer.isError) return { issues: [], note: i.failed };
  if (answer.isPending) return { issues: [], note: null };
  if (answer.data.status !== "ok" || !answer.data.month) return { issues: [], note: i.unreachable };
  return { issues: answer.data.month.issues, note: answer.data.month.truncated ? i.truncated : null };
}

/**
 * A month of a space's calendar, its events beside an Armature project's due
 * issues, each read as the reader may; whoever may change the calendar adds,
 * changes and deletes events from it.
 */
export function TeamCalendar({ settings, inEditor = false }: { settings: CalendarSettings; inEditor?: boolean }) {
  const c = t.calendar;
  const [month, setMonth] = useState<Month>(() => monthOf(new Date()));
  const [editing, setEditing] = useState<{ day: string; event: CalendarEvent | null } | null>(null);
  const span = monthSpan(month);
  const answer = useCalendarEvents(settings.calendarId, span.from, span.to);
  const { issues, note } = useIssues(settings.project, month);
  const weeks = useMemo(() => monthWeeks(month), [month]);
  const today = localDay(new Date());

  if (!settings.calendarId) {
    return (
      <figure className="doc-calendar" data-team-calendar="" data-state="none">
        <p className="doc-block-empty">{c.none}</p>
      </figure>
    );
  }
  const data = answer.data;
  const canEdit = Boolean(data?.calendar.canEdit);
  let state: string;
  let body: ReactNode;
  if (answer.isPending) {
    state = "loading";
    body = (
      <p className="doc-block-empty" role="status">
        {c.loading}
      </p>
    );
  } else if (answer.error instanceof ApiError && answer.error.status === 404) {
    state = "missing";
    body = <p className="doc-block-empty">{c.missing}</p>;
  } else if (answer.isError || !data) {
    state = "failed";
    body = <p className="doc-block-empty">{c.failed}</p>;
  } else {
    state = "month";
    const events = data.events;
    body = (
      <>
        <div className="doc-calendar-weekdays" aria-hidden="true">
          {weekdays().map((w) => (
            <span key={w}>{weekdayName(w, "short")}</span>
          ))}
        </div>
        <ol className="doc-calendar-grid" aria-label={c.days}>
          {weeks.flat().map((day, index) => {
            if (!day) return <li key={`blank-${index}`} className="doc-calendar-blank" aria-hidden="true" />;
            const own = events.filter((e) => onDay(e, day));
            const due = issues.filter((is) => is.to === day);
            const empty = own.length === 0 && due.length === 0;
            return (
              <li key={day} className="doc-calendar-day" data-day={day} data-today={day === today || undefined} data-empty={empty || undefined}>
                <div className="doc-calendar-day-head">
                  <span className="doc-calendar-date" aria-hidden="true">
                    <span className="doc-calendar-weekday">{weekdayName(dayDate(day).getDay(), "short")} </span>
                    {dayDate(day).getDate()}
                  </span>
                  <span className="sr-only">{longDay(day)}</span>
                  {canEdit && (
                    <IconButton
                      size="sm"
                      label={c.addOn(longDay(day))}
                      icon={<Icon.Plus />}
                      className="doc-calendar-add"
                      onClick={() => setEditing({ day, event: null })}
                      data-action="add-calendar-event"
                    />
                  )}
                </div>
                {!empty && (
                  <ul className="doc-calendar-entries">
                    {own.map((e) => (
                      <EventEntry key={e.id} event={e} day={day} onOpen={canEdit ? () => setEditing({ day, event: e }) : null} />
                    ))}
                    {due.map((is) => (
                      <IssueEntry key={is.key} issue={is} inEditor={inEditor} />
                    ))}
                  </ul>
                )}
              </li>
            );
          })}
        </ol>
        {events.length === 0 && issues.length === 0 && <p className="doc-calendar-note">{c.empty}</p>}
        {data.truncated && <p className="doc-calendar-note">{c.truncated}</p>}
      </>
    );
  }
  const title = data?.calendar.name ?? c.untitled;
  return (
    <figure className="doc-calendar" data-team-calendar="" data-state={state} aria-label={title}>
      <figcaption className="doc-calendar-head">
        <span className="doc-chart-title">{title}</span>
        <span className="doc-calendar-month" aria-live="polite" data-calendar-month={monthKey(month)}>
          {monthLabel(month)}
        </span>
        <span className="doc-calendar-nav">
          <IconButton size="sm" label={c.previous} icon={<Icon.ChevronDown className="rotate-90" />} onClick={() => setMonth((m) => shiftMonth(m, -1))} />
          <Button size="sm" variant="secondary" onClick={() => setMonth(monthOf(new Date()))}>
            {c.today}
          </Button>
          <IconButton size="sm" label={c.next} icon={<Icon.ChevronDown className="-rotate-90" />} onClick={() => setMonth((m) => shiftMonth(m, 1))} />
          {canEdit && (
            <Button
              size="sm"
              icon={<Icon.Plus />}
              onClick={() => setEditing({ day: monthKey(month) === today.slice(0, 7) ? today : localDay(new Date(month.year, month.month, 1)), event: null })}
              data-action="add-calendar-event"
            >
              {c.add}
            </Button>
          )}
        </span>
      </figcaption>
      {body}
      {note && <p className="doc-calendar-note">{note}</p>}
      {editing && <EventDialog calendarId={settings.calendarId} day={editing.day} event={editing.event} onClose={() => setEditing(null)} />}
    </figure>
  );
}
