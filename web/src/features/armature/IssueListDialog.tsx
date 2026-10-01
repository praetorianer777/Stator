import { useId, useState, type FormEvent } from "react";
import { ApiError } from "@/api/client";
import { useArmatureAccount, useArmatureQueryCheck } from "@/api/armature";
import { Button, Checkbox, Dialog, Input, Labelled, Textarea } from "@/components/ui";
import { describedBy } from "@/components/ui/controls";
import {
  ARMATURE_COLUMNS,
  ARMATURE_LIST_MAX_COLUMNS,
  ARMATURE_LIST_MAX_LIMIT,
  ARMATURE_QUERY_CHECK_DEBOUNCE_MS,
  ARMATURE_QUERY_MAX_LENGTH,
  type ArmatureColumn,
} from "@/config";
import { t } from "@/i18n";
import { useDebounced } from "@/lib/debounce";
import { MarkedQuery, type IssueListSettings } from "./IssueList";

/** The settings as the server takes them, or a sentence for each field that is wrong. */
export function checkSettings(
  query: string,
  columns: readonly ArmatureColumn[],
  limit: string,
): { settings: IssueListSettings } | { problems: Partial<Record<"query" | "columns" | "limit", string>> } {
  const d = t.armature.listDialog;
  const problems: Partial<Record<"query" | "columns" | "limit", string>> = {};
  if (!query.trim()) problems.query = d.queryBlank;
  else if ([...query].length > ARMATURE_QUERY_MAX_LENGTH) problems.query = d.queryLong(ARMATURE_QUERY_MAX_LENGTH);
  if (columns.length === 0) problems.columns = d.columnsNone;
  else if (columns.length > ARMATURE_LIST_MAX_COLUMNS) problems.columns = d.columnsMany(ARMATURE_LIST_MAX_COLUMNS);
  const rows = Number(limit);
  if (!Number.isInteger(rows) || rows < 1 || rows > ARMATURE_LIST_MAX_LIMIT) problems.limit = d.limitRange(ARMATURE_LIST_MAX_LIMIT);
  if (Object.keys(problems).length > 0) return { problems };
  return {
    settings: {
      query,
      columns: ARMATURE_COLUMNS.filter((c) => columns.includes(c)),
      limit: rows,
    },
  };
}

/**
 * Asks for an issue list's query, columns and most rows, checking the query
 * with Armature as it is typed.
 */
export function IssueListDialog({
  initial,
  isNew,
  onSave,
  onClose,
}: {
  initial: IssueListSettings;
  isNew: boolean;
  onSave: (settings: IssueListSettings) => void;
  onClose: () => void;
}) {
  const d = t.armature.listDialog;
  const id = useId();
  const [query, setQuery] = useState(initial.query);
  const [columns, setColumns] = useState<ArmatureColumn[]>(initial.columns);
  const [limit, setLimit] = useState(String(initial.limit));
  const [problems, setProblems] = useState<Partial<Record<"query" | "columns" | "limit", string>>>({});
  const account = useArmatureAccount();
  const connected = Boolean(account.data?.connected && account.data.status !== "rejected");
  const settled = useDebounced(query.trim(), ARMATURE_QUERY_CHECK_DEBOUNCE_MS);
  const typing = settled !== query.trim();
  const check = useArmatureQueryCheck(settled, connected && settled !== "" && [...settled].length <= ARMATURE_QUERY_MAX_LENGTH);
  const bad = check.error instanceof ApiError && check.error.code === "bad_query" ? check.error : null;

  function submit(event: FormEvent) {
    event.preventDefault();
    // The page's own form is this one's ancestor in React's tree.
    event.stopPropagation();
    const checked = checkSettings(query, columns, limit);
    if ("problems" in checked) {
      setProblems(checked.problems);
      return;
    }
    if (bad && !typing) {
      setProblems({ query: d.queryBad });
      return;
    }
    onSave(checked.settings);
  }

  const queryId = `${id}-query`;
  const checkId = `${id}-check`;
  const limitId = `${id}-limit`;
  let checkText: string | null = null;
  if (!connected) checkText = d.cannotCheck;
  else if (!query.trim()) checkText = null;
  else if (typing || check.isFetching) checkText = d.checking;
  else if (bad) checkText = t.armature.list.badQuery(bad.message, bad.position);
  else if (check.data?.status === "ok") checkText = d.matches(check.data.total);
  else if (check.data || check.isError) checkText = d.cannotCheck;

  return (
    <Dialog title={isNew ? d.titleNew : d.titleEdit} onClose={onClose} wide data-issue-list-dialog="">
      <form onSubmit={submit} className="space-y-4" noValidate>
        <Labelled id={queryId} label={d.query} hint={d.queryHint} error={problems.query}>
          <Textarea
            id={queryId}
            rows={3}
            value={query}
            spellCheck={false}
            invalid={Boolean(problems.query)}
            aria-invalid={problems.query ? true : undefined}
            aria-describedby={[describedBy(queryId, d.queryHint, problems.query), checkId].filter(Boolean).join(" ")}
            onChange={(event) => {
              setQuery(event.target.value);
              setProblems(({ query: _, ...rest }) => rest);
            }}
          />
        </Labelled>
        <div id={checkId} role="status" className="min-h-6 space-y-1 text-sm text-ink-muted" data-query-check={bad ? "bad_query" : (check.data?.status ?? "")}>
          {checkText && <p className={bad ? "text-danger" : undefined}>{checkText}</p>}
          {bad && !typing && <MarkedQuery query={settled} position={bad.position} />}
        </div>
        <fieldset className="space-y-2" aria-describedby={problems.columns ? `${id}-columns-error` : undefined}>
          <legend className="text-sm font-medium text-ink-muted">{d.columns}</legend>
          <div className="grid grid-cols-2 gap-x-4 gap-y-1 sm:grid-cols-3">
            {ARMATURE_COLUMNS.map((column) => (
              <Checkbox
                key={column}
                label={t.armature.list.columns[column]}
                checked={columns.includes(column)}
                onChange={(event) => {
                  const on = event.target.checked;
                  setColumns((now) => ARMATURE_COLUMNS.filter((c) => (c === column ? on : now.includes(c))));
                  setProblems(({ columns: _, ...rest }) => rest);
                }}
                data-column={column}
              />
            ))}
          </div>
          {problems.columns && (
            <p id={`${id}-columns-error`} className="text-sm text-danger">
              {problems.columns}
            </p>
          )}
        </fieldset>
        <Labelled id={limitId} label={d.limit} hint={d.limitHint(ARMATURE_LIST_MAX_LIMIT)} error={problems.limit} className="max-w-48">
          <Input
            id={limitId}
            type="number"
            inputMode="numeric"
            min={1}
            max={ARMATURE_LIST_MAX_LIMIT}
            value={limit}
            invalid={Boolean(problems.limit)}
            aria-invalid={problems.limit ? true : undefined}
            aria-describedby={describedBy(limitId, d.limitHint(ARMATURE_LIST_MAX_LIMIT), problems.limit)}
            onChange={(event) => {
              setLimit(event.target.value);
              setProblems(({ limit: _, ...rest }) => rest);
            }}
          />
        </Labelled>
        <div className="flex justify-end gap-2 pt-1">
          <Button type="button" variant="secondary" onClick={onClose}>
            {d.cancel}
          </Button>
          <Button type="submit" data-action="save-issue-list">
            {isNew ? d.insert : d.save}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
