import { useState, type FormEvent } from "react";
import { normalizeLabel } from "@/api/labels";
import { useSpaces } from "@/api/spaces";
import { Button, Dialog, Field, Select } from "@/components/ui";
import { PROPERTIES_REPORT_MAX_COLUMNS, PROPERTIES_REPORT_MAX_LABELS, PROPERTY_KEY_MAX_LENGTH } from "@/config";
import { t } from "@/i18n";
import { columnName, reportColumns, type ReportSettings } from "./report";

type Problems = Partial<Record<"labels" | "columns", string>>;

/** The settings as the server takes them from what was typed, or a sentence for each field that is wrong. */
export function checkReport(labels: string, space: string, columns: string): { settings: ReportSettings } | { problems: Problems } {
  const d = t.properties.dialog;
  const problems: Problems = {};
  const names: string[] = [];
  for (const raw of labels.split(",")) {
    if (!raw.trim()) continue;
    const { name, problem } = normalizeLabel(raw);
    if (problem) problems.labels ??= d.labelBad(raw.trim());
    else if (!names.includes(name)) names.push(name);
  }
  if (!problems.labels && names.length === 0) problems.labels = d.labelsNone;
  else if (names.length > PROPERTIES_REPORT_MAX_LABELS) problems.labels = d.labelsMany(PROPERTIES_REPORT_MAX_LABELS);
  const lines = columns.split("\n").map(columnName).filter(Boolean);
  if (lines.some((line) => [...line].length > PROPERTY_KEY_MAX_LENGTH)) problems.columns = d.columnLong(PROPERTY_KEY_MAX_LENGTH);
  else if (new Set(lines.map((line) => line.toLowerCase())).size > PROPERTIES_REPORT_MAX_COLUMNS)
    problems.columns = d.columnsMany(PROPERTIES_REPORT_MAX_COLUMNS);
  if (Object.keys(problems).length > 0) return { problems };
  return { settings: { labels: names, space: space || null, columns: reportColumns(lines) } };
}

/** Asks for a report's labels, the space it stays in, and the properties it shows. */
export function PropertiesReportDialog({
  initial,
  isNew,
  onSave,
  onClose,
}: {
  initial: ReportSettings;
  isNew: boolean;
  onSave: (settings: ReportSettings) => void;
  onClose: () => void;
}) {
  const d = t.properties.dialog;
  const spaces = useSpaces();
  const [labels, setLabels] = useState(initial.labels.join(", "));
  const [space, setSpace] = useState(initial.space ?? "");
  const [columns, setColumns] = useState(initial.columns.join("\n"));
  const [problems, setProblems] = useState<Problems>({});

  function submit(event: FormEvent) {
    event.preventDefault();
    // The page's own form is this one's ancestor in React's tree.
    event.stopPropagation();
    const checked = checkReport(labels, space, columns);
    if ("problems" in checked) {
      setProblems(checked.problems);
      return;
    }
    onSave(checked.settings);
  }

  return (
    <Dialog title={isNew ? d.titleNew : d.titleEdit} onClose={onClose} wide data-properties-report-dialog="">
      <form onSubmit={submit} className="space-y-4" noValidate>
        <Field
          label={d.labels}
          hint={d.labelsHint}
          error={problems.labels}
          value={labels}
          autoFocus
          onChange={(event) => {
            setLabels(event.target.value);
            setProblems(({ labels: _, ...rest }) => rest);
          }}
        />
        <Select label={d.space} value={space} onChange={(event) => setSpace(event.target.value)}>
          <option value="">{d.everySpace}</option>
          {initial.space && !spaces.data?.some((s) => s.key === initial.space) && <option value={initial.space}>{initial.space}</option>}
          {(spaces.data ?? []).map((s) => (
            <option key={s.key} value={s.key}>
              {s.name} ({s.key})
            </option>
          ))}
        </Select>
        <Field
          label={d.columns}
          hint={d.columnsHint}
          error={problems.columns}
          rows={4}
          value={columns}
          onChange={(event) => {
            setColumns(event.target.value);
            setProblems(({ columns: _, ...rest }) => rest);
          }}
        />
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {d.cancel}
          </Button>
          <Button type="submit" data-action="save-properties-report">
            {isNew ? d.insert : d.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
