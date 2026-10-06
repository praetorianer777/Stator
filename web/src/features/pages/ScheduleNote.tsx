import type { Page } from "@/api/pages";
import { useCancelSchedule } from "@/api/versions";
import { Button, cx, ErrorBanner } from "@/components/ui";
import { t } from "@/i18n";
import { scheduleTimeFormat } from "./schedule";

/**
 * Tells the page's editors, and the author, of a publish set for a time: when
 * it goes out and whose it is, or why it did not. Readers learn of it when it happens.
 */
export function ScheduleNote({ page, onEdit }: { page: Page; onEdit: () => void }) {
  const cancel = useCancelSchedule(page.id);
  const schedule = page.schedule;
  if (!schedule) return null;
  const when = scheduleTimeFormat.format(new Date(schedule.publishAt));
  const failed = schedule.failure;
  const text = failed
    ? schedule.mine
      ? t.schedule.failedMine(when, t.schedule.failures[failed])
      : t.schedule.failedTheirs(schedule.authorName, when, t.schedule.failures[failed])
    : schedule.mine
      ? t.schedule.mine(when)
      : t.schedule.theirs(schedule.authorName, when);
  const mayCancel = schedule.mine || page.can.edit;

  function callOff() {
    if (window.confirm(t.schedule.confirmCancel)) cancel.mutate();
  }

  return (
    <div className="mb-4 space-y-2">
      <div
        role={failed ? "alert" : "status"}
        className={cx(
          "flex flex-wrap items-center gap-3 rounded-control border px-3 py-2 text-sm text-ink",
          failed ? "border-warning bg-warning-subtle" : "border-border bg-surface-raised",
        )}
        data-schedule-note={failed ?? "waiting"}
      >
        <span className="min-w-0 flex-1">{text}</span>
        {schedule.mine && page.can.edit && (
          <Button size="sm" variant="secondary" onClick={onEdit} data-action="edit-schedule">
            {failed ? t.schedule.editAgain : t.schedule.change}
          </Button>
        )}
        {mayCancel && (
          <Button size="sm" variant="ghost" onClick={callOff} loading={cancel.isPending} data-action="cancel-schedule">
            {t.schedule.cancel}
          </Button>
        )}
      </div>
      {cancel.error && <ErrorBanner>{cancel.error.message}</ErrorBanner>}
    </div>
  );
}
