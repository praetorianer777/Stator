import { useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useDeleteCalendarEvent, useSaveCalendarEvent, type CalendarEvent } from "@/api/calendars";
import { Button, Checkbox, Dialog, ErrorBanner, Field, Select } from "@/components/ui";
import { CALENDAR_EVENT_KINDS, CALENDAR_EVENT_TITLE_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { draftOf, inputOf, newDraft, type CalendarKind, type EventDraft } from "./calendar";

type Problems = Partial<Record<"title" | "start" | "end", string>>;

/** Adds an event on a day, or changes or deletes one; the calendar asks again when it is done. */
export function EventDialog({ calendarId, day, event, onClose }: { calendarId: string; day: string; event: CalendarEvent | null; onClose: () => void }) {
  const e = t.calendar.event;
  const save = useSaveCalendarEvent(calendarId);
  const remove = useDeleteCalendarEvent(calendarId);
  const [draft, setDraft] = useState<EventDraft>(() => (event ? draftOf(event) : newDraft(day)));
  const [problems, setProblems] = useState<Problems>({});
  const [failed, setFailed] = useState<string | null>(null);
  const set = (change: Partial<EventDraft>) => setDraft((was) => ({ ...was, ...change }));

  async function submit(form: FormEvent) {
    form.preventDefault();
    // The page's own form is this one's ancestor in React's tree.
    form.stopPropagation();
    const checked = inputOf(draft);
    if ("problem" in checked) {
      setProblems({ [checked.problem]: { title: e.titleBlank, start: e.startMissing, end: e.endBefore }[checked.problem] });
      return;
    }
    if ([...checked.input.title].length > CALENDAR_EVENT_TITLE_MAX_LENGTH) {
      setProblems({ title: e.titleLong(CALENDAR_EVENT_TITLE_MAX_LENGTH) });
      return;
    }
    try {
      await save.mutateAsync({ id: event?.id ?? null, input: checked.input });
      onClose();
    } catch (error) {
      // The server's sentence on a field goes under that field; any other is said above the buttons.
      if (error instanceof ApiError && Object.keys(error.fields).length > 0) setProblems(error.fields as Problems);
      else setFailed(error instanceof ApiError ? error.message : e.failed);
    }
  }

  async function deleteEvent() {
    if (!event) return;
    try {
      await remove.mutateAsync(event.id);
      onClose();
    } catch (error) {
      setFailed(error instanceof ApiError ? error.message : e.deleteFailed);
    }
  }

  return (
    <Dialog title={event ? e.titleEdit : e.titleNew} onClose={onClose} data-calendar-event-dialog="">
      <form onSubmit={submit} className="space-y-4" noValidate>
        <Field
          label={e.title}
          value={draft.title}
          error={problems.title}
          autoFocus
          onChange={(change) => {
            set({ title: change.target.value });
            setProblems(({ title: _, ...rest }) => rest);
          }}
        />
        <Select label={e.kind} value={draft.kind} onChange={(change) => set({ kind: change.target.value as CalendarKind })}>
          {CALENDAR_EVENT_KINDS.map((kind) => (
            <option key={kind} value={kind}>
              {e.kinds[kind]}
            </option>
          ))}
        </Select>
        <Checkbox label={e.allDay} checked={draft.allDay} onChange={(change) => set({ allDay: change.target.checked })} />
        {draft.allDay ? (
          <div className="grid gap-3 sm:grid-cols-2">
            <Field
              type="date"
              label={e.firstDay}
              value={draft.startDay}
              error={problems.start}
              required
              onChange={(change) => set({ startDay: change.target.value })}
            />
            <Field
              type="date"
              label={e.lastDay}
              value={draft.endDay}
              error={problems.end}
              required
              onChange={(change) => set({ endDay: change.target.value })}
            />
          </div>
        ) : (
          <div className="grid gap-3 sm:grid-cols-2">
            <Field
              type="datetime-local"
              label={e.starts}
              value={draft.startTime}
              error={problems.start}
              required
              onChange={(change) => set({ startTime: change.target.value })}
            />
            <Field
              type="datetime-local"
              label={e.ends}
              value={draft.endTime}
              error={problems.end}
              required
              onChange={(change) => set({ endTime: change.target.value })}
            />
          </div>
        )}
        {failed && <ErrorBanner>{failed}</ErrorBanner>}
        <div className="flex flex-wrap items-center justify-end gap-2 pt-1">
          {event && (
            <Button type="button" variant="danger" className="mr-auto" onClick={deleteEvent} disabled={remove.isPending} data-action="delete-calendar-event">
              {e.delete}
            </Button>
          )}
          <Button type="button" variant="secondary" onClick={onClose}>
            {e.cancel}
          </Button>
          <Button type="submit" disabled={save.isPending} data-action="save-calendar-event">
            {event ? e.save : e.add}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
