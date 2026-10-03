import { useState, type FormEvent } from "react";
import { usePeople } from "@/api/search";
import { useSpaces } from "@/api/spaces";
import { Button, Dialog, Select } from "@/components/ui";
import { TASK_REPORT_DUES, TASK_REPORT_LIMIT_CHOICES, TASK_REPORT_STATES } from "@/config";
import { t } from "@/i18n";
import { ASSIGNEE_NOBODY, ASSIGNEE_READER, isPerson, type ReportDue, type ReportState, type TaskReportSettings } from "./report";

/** Asks for what a task report picks: a space, an assignee, a due day, a state and a length. */
export function TaskReportDialog({
  initial,
  assigneeName = "",
  isNew,
  onSave,
  onClose,
}: {
  initial: TaskReportSettings;
  /** The name of the person the report asks for, when the people offered leave them out. */
  assigneeName?: string;
  isNew: boolean;
  onSave: (settings: TaskReportSettings) => void;
  onClose: () => void;
}) {
  const d = t.taskReport.dialog;
  const spaces = useSpaces();
  const people = usePeople();
  const [space, setSpace] = useState(initial.space ?? "");
  const [assignee, setAssignee] = useState(initial.assignee ?? "");
  const [due, setDue] = useState<ReportDue>(initial.due);
  const [state, setState] = useState<ReportState>(initial.state);
  const [limit, setLimit] = useState(initial.limit);

  function submit(event: FormEvent) {
    event.preventDefault();
    // The page's own form is this one's ancestor in React's tree.
    event.stopPropagation();
    onSave({ space: space || null, assignee: assignee || null, due, state, limit });
  }

  const listed = people.data ?? [];
  return (
    <Dialog title={isNew ? d.titleNew : d.titleEdit} onClose={onClose} data-task-report-dialog="">
      <form onSubmit={submit} className="space-y-4" noValidate>
        <Select label={d.space} value={space} onChange={(event) => setSpace(event.target.value)}>
          <option value="">{d.everySpace}</option>
          {initial.space && !spaces.data?.some((s) => s.key === initial.space) && <option value={initial.space}>{initial.space}</option>}
          {(spaces.data ?? []).map((s) => (
            <option key={s.key} value={s.key}>
              {s.name} ({s.key})
            </option>
          ))}
        </Select>
        <Select label={d.assignee} value={assignee} onChange={(event) => setAssignee(event.target.value)}>
          <option value="">{d.anybody}</option>
          <option value={ASSIGNEE_READER}>{d.reader}</option>
          <option value={ASSIGNEE_NOBODY}>{d.nobody}</option>
          {isPerson(initial.assignee) && !listed.some((p) => p.id === initial.assignee) && (
            <option value={initial.assignee}>{assigneeName || d.somebodyGone}</option>
          )}
          {listed.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </Select>
        <Select label={d.due} value={due} onChange={(event) => setDue(event.target.value as ReportDue)}>
          {TASK_REPORT_DUES.map((choice) => (
            <option key={choice} value={choice}>
              {d.dues[choice]}
            </option>
          ))}
        </Select>
        <Select label={d.state} value={state} onChange={(event) => setState(event.target.value as ReportState)}>
          {TASK_REPORT_STATES.map((choice) => (
            <option key={choice} value={choice}>
              {d.states[choice]}
            </option>
          ))}
        </Select>
        <Select label={d.limit} value={String(limit)} onChange={(event) => setLimit(Number(event.target.value))}>
          {[...new Set([...TASK_REPORT_LIMIT_CHOICES, initial.limit])]
            .sort((a, b) => a - b)
            .map((n) => (
              <option key={n} value={n}>
                {d.limitChoice(n)}
              </option>
            ))}
        </Select>
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {d.cancel}
          </Button>
          <Button type="submit" data-action="save-task-report">
            {isNew ? d.insert : d.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
