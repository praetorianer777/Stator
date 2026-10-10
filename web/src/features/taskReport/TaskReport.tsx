import type { ReactNode } from "react";
import { useTaskReport, type Task } from "@/api/tasks";
import { PageLink } from "@/features/pages/PageLink";
import { taskAnchor } from "@/features/tasks/taskAnchor";
import { DueChip } from "@/features/tasks/DueChip";
import { TaskCheck } from "@/features/tasks/MyTasks";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { type TaskReportSettings } from "./report";

const day = localDateFormat({ dateStyle: "medium" });

function ReportRow({ task, inEditor }: { task: Task; inEditor: boolean }) {
  const r = t.taskReport;
  return (
    <li className="doc-task-report-row" data-reported-task={task.text} data-task-done={task.done}>
      {/* Ticking a task publishes its page, which is no thing to do from a page mid-edit. */}
      {inEditor ? <span className="doc-task-report-box" data-done={task.done} aria-hidden="true" /> : <TaskCheck task={task} />}
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <span className="doc-page-list-title break-words">{task.text}</span>
        <span className="doc-page-list-meta flex flex-wrap items-center gap-x-2 gap-y-1">
          {inEditor ? (
            <span>{task.page.title}</span>
          ) : (
            <PageLink spaceKey={task.page.spaceKey} id={task.page.id} title={task.page.title} hash={taskAnchor(task.id)} className="hover:underline" />
          )}
          <span>{task.page.spaceName}</span>
          <span>{task.assigneeName || r.nobody}</span>
          {task.done && task.doneAt ? <span>{t.tasks.doneOn(day.format(new Date(task.doneAt)))}</span> : <DueChip due={task.dueOn} done={task.done} />}
        </span>
      </div>
    </li>
  );
}

/** The tasks a report's filter picks, as the reader may read them; ticked off at once outside the editor. */
export function TaskReport({ settings, inEditor = false }: { settings: TaskReportSettings; inEditor?: boolean }) {
  const r = t.taskReport;
  const query = useTaskReport(settings);
  const report = query.data;
  let state: string;
  let body: ReactNode;
  if (query.isPending) {
    state = "loading";
    body = (
      <p className="doc-block-empty" role="status">
        {r.loading}
      </p>
    );
  } else if (query.isError || !report) {
    state = "failed";
    body = <p className="doc-block-empty">{r.failed}</p>;
  } else if (report.tasks.length === 0) {
    state = "empty";
    body = <p className="doc-block-empty">{r.empty}</p>;
  } else {
    state = "list";
    body = (
      <ul className="doc-page-list">
        {report.tasks.map((task) => (
          <ReportRow key={`${task.page.id}:${task.id}`} task={task} inEditor={inEditor} />
        ))}
      </ul>
    );
  }
  const title = r.title(settings, report?.assigneeName ?? "");
  return (
    <section className="doc-page-list-block" aria-label={title} data-task-report="" data-state={state}>
      <p className="doc-chart-title">{title}</p>
      {body}
      {report?.truncated && <p className="doc-roadmap-note">{r.truncated(report.tasks.length)}</p>}
    </section>
  );
}
