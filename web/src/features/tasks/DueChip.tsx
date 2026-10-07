import { Icon } from "@/components/icons";
import { cx } from "@/components/ui";
import { formatDay } from "@/features/editor/InlineValueViews";
import { t } from "@/i18n";
import { dueState } from "./taskItem";

/**
 * How a task stands against its day, in words and in the tint of its state.
 * Brief leaves the day out where the page already shows it, and says nothing of a day still ahead.
 */
export function DueChip({ due, done, brief = false, className }: { due: string | null | undefined; done: boolean; brief?: boolean; className?: string }) {
  const state = dueState(due, done);
  if (!state || !due || (brief && state === "upcoming")) return null;
  const words =
    state === "overdue"
      ? brief
        ? t.tasks.overdue
        : t.tasks.overdueSince(formatDay(due))
      : state === "today"
        ? t.tasks.dueToday
        : t.tasks.dueOn(formatDay(due));
  return (
    <span className={cx("task-due inline-flex shrink-0 items-center gap-1 rounded px-1.5 py-px text-2xs font-medium", className)} data-task-due={state}>
      <Icon.Calendar size={12} />
      {words}
    </span>
  );
}
