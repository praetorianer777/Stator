import { TASK_REPORT_DEFAULT_LIMIT, TASK_REPORT_DUES, TASK_REPORT_MAX_LIMIT, TASK_REPORT_STATES } from "@/config";

export const TASK_REPORT_NODE = "taskReport";

export type ReportDue = (typeof TASK_REPORT_DUES)[number];
export type ReportState = (typeof TASK_REPORT_STATES)[number];

/** Whom a report's tasks are assigned to beside one person, by their id. */
export const ASSIGNEE_READER = "me";
export const ASSIGNEE_NOBODY = "none";

/** What a task report block stores: its filter, never the tasks. */
export interface TaskReportSettings {
  /** A space's key to stay inside, or null for every space. */
  space: string | null;
  /** Null for anybody, ASSIGNEE_READER, ASSIGNEE_NOBODY, or a person's id. */
  assignee: string | null;
  due: ReportDue;
  state: ReportState;
  limit: number;
}

const SPACE = /^[A-Z][A-Z0-9]{1,9}$/;
const ASSIGNEE = /^(?:me|none|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})$/;

function oneOf<T extends string>(choices: readonly T[], value: unknown, fallback: T): T {
  return (choices as readonly unknown[]).includes(value) ? (value as T) : fallback;
}

/** A block's attributes as the server takes them, each one wrong put right. */
export function taskReportSettings(attrs: Record<string, unknown> | undefined): TaskReportSettings {
  const limit = Number(attrs?.limit);
  return {
    space: typeof attrs?.space === "string" && SPACE.test(attrs.space) ? attrs.space : null,
    assignee: typeof attrs?.assignee === "string" && ASSIGNEE.test(attrs.assignee) ? attrs.assignee : null,
    due: oneOf(TASK_REPORT_DUES, attrs?.due, "any"),
    state: oneOf(TASK_REPORT_STATES, attrs?.state, "open"),
    limit: Number.isInteger(limit) && limit >= 1 && limit <= TASK_REPORT_MAX_LIMIT ? limit : TASK_REPORT_DEFAULT_LIMIT,
  };
}

/** Whether the assignee names one person, whose name the server answers with. */
export function isPerson(assignee: string | null): assignee is string {
  return assignee !== null && assignee !== ASSIGNEE_READER && assignee !== ASSIGNEE_NOBODY;
}
