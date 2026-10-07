import { useId, useRef, useState } from "react";
import { useMyTasks, useSetTaskDone, type Task, type TaskState } from "@/api/tasks";
import { Button, ErrorBanner, PageHeader, Skeleton, TabPanel, Tabs } from "@/components/ui";
import { PageLink } from "@/features/pages/PageLink";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import { DueChip } from "./DueChip";

const day = localDateFormat({ dateStyle: "medium" });

/** A task's box: ticked off or opened again at once, or shown still where the page may not be edited. */
export function TaskCheck({ task, onDone }: { task: Task; onDone?: (task: Task, done: boolean) => void }) {
  const setDone = useSetTaskDone();
  const [failed, setFailed] = useState(false);
  const hintId = useId();
  return (
    <>
      <input
        type="checkbox"
        className="mt-0.5 size-4 shrink-0 rounded-[4px] border-border-strong accent-accent"
        checked={task.done}
        disabled={!task.canEdit || setDone.isPending}
        aria-label={t.tasks.tick(task.text)}
        aria-describedby={task.canEdit ? undefined : hintId}
        onChange={(event) => {
          const done = event.target.checked;
          setFailed(false);
          setDone.mutate({ pageId: task.page.id, taskId: task.id, done }, { onSuccess: () => onDone?.(task, done), onError: () => setFailed(true) });
        }}
        data-task-check={task.text}
      />
      {!task.canEdit && (
        <span id={hintId} className="sr-only">
          {t.tasks.readOnly}
        </span>
      )}
      {failed && (
        <span role="alert" className="sr-only">
          {t.tasks.tickFailed}
        </span>
      )}
    </>
  );
}

function TaskRow({ task, onDone }: { task: Task; onDone: (task: Task, done: boolean) => void }) {
  return (
    <li className="flex min-w-0 items-start gap-3 py-3" data-task-row={task.text} data-task-done={task.done}>
      <TaskCheck task={task} onDone={onDone} />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <span className="text-sm break-words text-ink">{task.text}</span>
        <span className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-ink-subtle">
          <PageLink
            spaceKey={task.page.spaceKey}
            id={task.page.id}
            title={task.page.title}
            className="font-medium text-ink-muted hover:text-accent hover:underline"
          />
          <span>{task.page.spaceName}</span>
          {task.done && task.doneAt ? <span>{t.tasks.doneOn(day.format(new Date(task.doneAt)))}</span> : <DueChip due={task.dueOn} done={task.done} />}
          {!task.done && task.assignedByName && <span>{t.tasks.assignedBy(task.assignedByName)}</span>}
        </span>
      </div>
    </li>
  );
}

function TaskList({ state, onDone }: { state: TaskState; onDone: (task: Task, done: boolean) => void }) {
  const query = useMyTasks(state);
  const tasks = query.data?.pages.flatMap((window) => window.tasks) ?? [];
  if (query.isError) return <ErrorBanner onRetry={() => void query.refetch()}>{t.tasks.loadFailed}</ErrorBanner>;
  if (query.isLoading) return <Skeleton />;
  if (tasks.length === 0) return <p className="py-4 text-sm text-ink-muted">{state === "open" ? t.tasks.emptyOpen : t.tasks.emptyDone}</p>;
  return (
    <div className="flex flex-col gap-1">
      <ul className="flex flex-col divide-y divide-border" data-task-list={state}>
        {tasks.map((task) => (
          <TaskRow key={`${task.page.id}:${task.id}`} task={task} onDone={onDone} />
        ))}
      </ul>
      {query.hasNextPage && (
        <Button
          variant="ghost"
          size="sm"
          className="mt-1 self-start"
          loading={query.isFetchingNextPage}
          onClick={() => void query.fetchNextPage()}
          data-more="tasks"
        >
          {t.tasks.more}
        </Button>
      )}
    </div>
  );
}

/** The caller's tasks from every page they may read: the open ones soonest due first, or the done ones. */
export function MyTasks() {
  const [state, setState] = useState<TaskState>("open");
  const [notice, setNotice] = useState("");
  const panelId = useId();
  const panel = useRef<HTMLDivElement>(null);
  // A ticked task leaves the list it was in, so focus goes to the list rather than to nowhere.
  const done = (task: Task, isDone: boolean) => {
    setNotice(isDone ? t.tasks.ticked(task.text) : t.tasks.reopened(task.text));
    panel.current?.focus();
  };
  return (
    <div className="mx-auto max-w-3xl" data-my-tasks="">
      <PageHeader title={t.tasks.title} />
      <p className="mb-4 text-sm text-ink-muted">{t.tasks.intro}</p>
      <p role="status" className="sr-only">
        {notice}
      </p>
      <Tabs
        label={t.tasks.title}
        value={state}
        onChange={setState}
        panelId={panelId}
        tabs={[
          { value: "open", label: t.tasks.open, attrs: { "data-state": "open" } },
          { value: "done", label: t.tasks.done, attrs: { "data-state": "done" } },
        ]}
      />
      <TabPanel id={panelId} label={state === "open" ? t.tasks.open : t.tasks.done}>
        <div ref={panel} tabIndex={-1} className="outline-none">
          <TaskList state={state} onDone={done} />
        </div>
      </TabPanel>
    </div>
  );
}
