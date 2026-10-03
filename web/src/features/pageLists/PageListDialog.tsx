import { useId, useState, type FormEvent } from "react";
import { normalizeLabel } from "@/api/labels";
import { useSpaces } from "@/api/spaces";
import { Button, Dialog, Field, Select } from "@/components/ui";
import { PAGE_LIST_LIMIT_CHOICES, PAGE_LIST_MATCHES, PAGE_LIST_SORTS, PROPERTIES_REPORT_MAX_LABELS } from "@/config";
import { t } from "@/i18n";
import type { LabelledSettings, ListMatch, ListSort, UpdatedSettings } from "./lists";

/** The labels as typed, normalized, or a sentence saying what is wrong with them. */
export function checkLabels(raw: string): { labels: string[] } | { problem: string } {
  const d = t.pageLists.dialog;
  const labels: string[] = [];
  for (const part of raw.split(",")) {
    if (!part.trim()) continue;
    const { name, problem } = normalizeLabel(part);
    if (problem) return { problem: t.properties.dialog.labelBad(part.trim()) };
    if (!labels.includes(name)) labels.push(name);
  }
  if (labels.length === 0) return { problem: d.labelsNone };
  if (labels.length > PROPERTIES_REPORT_MAX_LABELS) return { problem: t.properties.dialog.labelsMany(PROPERTIES_REPORT_MAX_LABELS) };
  return { labels };
}

type Props =
  | { kind: "labelled"; initial: LabelledSettings; isNew: boolean; onSave: (settings: LabelledSettings) => void; onClose: () => void }
  | { kind: "updated"; initial: UpdatedSettings; isNew: boolean; onSave: (settings: UpdatedSettings) => void; onClose: () => void };

/** Asks for what a list block shows: for content by label its labels, how they match and its order; for both a space and a length. */
export function PageListDialog(props: Props) {
  const { kind, isNew, onClose } = props;
  const d = t.pageLists.dialog;
  const id = useId();
  const spaces = useSpaces();
  const labelled = props.kind === "labelled" ? props.initial : null;
  const [labels, setLabels] = useState(labelled?.labels.join(", ") ?? "");
  const [match, setMatch] = useState<ListMatch>(labelled?.match ?? "all");
  const [sort, setSort] = useState<ListSort>(labelled?.sort ?? "updated");
  const [space, setSpace] = useState(props.initial.space ?? "");
  const [limit, setLimit] = useState(props.initial.limit);
  const [problem, setProblem] = useState<string>();

  function submit(event: FormEvent) {
    event.preventDefault();
    // The page's own form is this one's ancestor in React's tree.
    event.stopPropagation();
    const shared = { space: space || null, limit };
    if (props.kind === "updated") {
      props.onSave(shared);
      return;
    }
    const checked = checkLabels(labels);
    if ("problem" in checked) {
      setProblem(checked.problem);
      return;
    }
    props.onSave({ ...shared, labels: checked.labels, match, sort });
  }

  const title = kind === "labelled" ? (isNew ? d.labelledNew : d.labelledEdit) : isNew ? d.updatedNew : d.updatedEdit;
  return (
    <Dialog title={title} onClose={onClose} data-page-list-dialog={kind}>
      <form onSubmit={submit} className="space-y-4" noValidate>
        {kind === "labelled" && (
          <>
            <Field
              label={d.labels}
              hint={d.labelsHint}
              error={problem}
              value={labels}
              autoFocus
              onChange={(event) => {
                setLabels(event.target.value);
                setProblem(undefined);
              }}
            />
            <fieldset className="space-y-1">
              <legend className="text-sm font-medium text-ink-muted">{d.match}</legend>
              {PAGE_LIST_MATCHES.map((m) => (
                <label key={m} className="flex items-center gap-2 text-sm text-ink">
                  <input type="radio" name={`${id}-match`} checked={match === m} onChange={() => setMatch(m)} className="accent-accent" />
                  {d.matches[m]}
                </label>
              ))}
            </fieldset>
          </>
        )}
        <Select label={d.space} value={space} onChange={(event) => setSpace(event.target.value)}>
          <option value="">{d.everySpace}</option>
          {props.initial.space && !spaces.data?.some((s) => s.key === props.initial.space) && (
            <option value={props.initial.space}>{props.initial.space}</option>
          )}
          {(spaces.data ?? []).map((s) => (
            <option key={s.key} value={s.key}>
              {s.name} ({s.key})
            </option>
          ))}
        </Select>
        {kind === "labelled" && (
          <Select label={d.sort} value={sort} onChange={(event) => setSort(event.target.value as ListSort)}>
            {PAGE_LIST_SORTS.map((s) => (
              <option key={s} value={s}>
                {d.sorts[s]}
              </option>
            ))}
          </Select>
        )}
        <Select label={d.limit} value={String(limit)} onChange={(event) => setLimit(Number(event.target.value))}>
          {[...new Set([...PAGE_LIST_LIMIT_CHOICES, props.initial.limit])]
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
          <Button type="submit" data-action="save-page-list">
            {isNew ? d.insert : d.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
