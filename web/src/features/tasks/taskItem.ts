import { isoDay } from "@/features/editor/InlineValueViews";
import type { DocNode } from "@/features/editor/schema";

// The same reading as the server's document.TaskOf, so a page shows the due
// day and the assignee its tasks are listed by.

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const TEXTBLOCKS = new Set(["paragraph", "heading", "codeBlock"]);

export interface TaskItem {
  /** Null until the item is first published. */
  id: string | null;
  done: boolean;
  assigneeId: string | null;
  due: string | null;
}

/** A checklist item read as a task: the first person and the first date in its own words, not in the items nested in it. */
export function taskOfItem(item: DocNode): TaskItem {
  const raw = item.attrs?.taskId;
  const out: TaskItem = { id: typeof raw === "string" && UUID.test(raw) ? raw : null, done: item.attrs?.checked === true, assigneeId: null, due: null };
  for (const block of item.content ?? []) {
    if (!TEXTBLOCKS.has(block.type)) continue;
    for (const node of block.content ?? []) {
      if (node.type === "mention" && out.assigneeId === null && typeof node.attrs?.id === "string" && UUID.test(node.attrs.id)) out.assigneeId = node.attrs.id;
      if (node.type === "date" && out.due === null) out.due = isoDay(node.attrs?.date);
    }
  }
  return out;
}

/** How a task stands against its day: done or without one is nothing to say. */
export type DueState = "overdue" | "today" | "upcoming";

/** Today in UTC, the calendar a due day is read in, so a task is due on the same day for everybody. */
export function utcToday(now: Date = new Date()): string {
  return now.toISOString().slice(0, "YYYY-MM-DD".length);
}

export function dueState(due: string | null | undefined, done: boolean, today: string = utcToday()): DueState | null {
  if (!due || done) return null;
  if (due < today) return "overdue";
  return due === today ? "today" : "upcoming";
}
