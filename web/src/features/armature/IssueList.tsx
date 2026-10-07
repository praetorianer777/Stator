import { localDateFormat } from "@/lib/format";
import { useContext, useMemo, useState, type ReactNode } from "react";
import { ApiError } from "@/api/client";
import { useArmatureAccount, useArmatureSearch, type ArmatureIssue } from "@/api/armature";
import { Button } from "@/components/ui";
import {
  ARMATURE_COLUMNS,
  ARMATURE_DEFAULT_COLUMNS,
  ARMATURE_LIST_DEFAULT_LIMIT,
  ARMATURE_LIST_MAX_LIMIT,
  ARMATURE_PRIORITIES,
  ARMATURE_SECTION_ID,
  ARMATURE_STATUS_CATEGORIES,
  PROFILE_PATH,
  type ArmatureColumn,
} from "@/config";
import { DocPageContext } from "@/features/editor/BlockViews";
import { t } from "@/i18n";
import { formatDue } from "./IssueBlock";
import { StatusLozenge, TypeIcon } from "./IssueChip";

/** What a list block stores: the query and how to show it, never its rows. */
export interface IssueListSettings {
  query: string;
  columns: ArmatureColumn[];
  limit: number;
}

/** A stored list's settings, anything out of shape put back to what a new list takes. */
export function listSettings(attrs: Record<string, unknown> | undefined): IssueListSettings {
  const query = typeof attrs?.query === "string" ? attrs.query : "";
  const raw: unknown[] = Array.isArray(attrs?.columns) ? attrs.columns : [];
  const columns = ARMATURE_COLUMNS.filter((c) => raw.includes(c));
  const limit = Number(attrs?.limit);
  return {
    query,
    columns: columns.length > 0 ? columns : [...ARMATURE_DEFAULT_COLUMNS],
    limit: Number.isInteger(limit) && limit >= 1 && limit <= ARMATURE_LIST_MAX_LIMIT ? limit : ARMATURE_LIST_DEFAULT_LIMIT,
  };
}

export type Sort = { column: ArmatureColumn; ascending: boolean } | null;

const onDay = localDateFormat({ dateStyle: "medium" });

/**
 * The issues a query matches, as the viewer may see them, in a table sortable
 * by its column heads. In the editor the query is shown to fix, without links.
 */
export function IssueList({ settings, inEditor = false }: { settings: IssueListSettings; inEditor?: boolean }) {
  const l = t.armature.list;
  const page = useContext(DocPageContext);
  const edit = inEditor ? undefined : page?.onEdit;
  const links = !inEditor;
  const account = useArmatureAccount();
  const configured = Boolean(account.data?.configured && account.data.baseUrl);
  const asks = Boolean(configured && account.data?.connected && account.data.status !== "rejected");
  const search = useArmatureSearch(settings.query, settings.limit, asks);
  const [sort, setSort] = useState<Sort>(null);
  const rows = useMemo(() => (search.data?.pages ?? []).flatMap((p) => (p.status === "ok" ? p.issues : [])), [search.data]);
  const sorted = useMemo(() => sortRows(rows, sort), [rows, sort]);
  const first = search.data?.pages[0];

  let state: string;
  let body: ReactNode;
  if (account.isPending) {
    state = "loading";
    body = <Note status>{l.loading}</Note>;
  } else if (!configured || first?.status === "not_configured") {
    state = "plain";
    body = <Note>{l.notConfigured}</Note>;
  } else if (!asks || first?.status === "not_connected" || first?.status === "rejected") {
    state = "connect";
    body = (
      <Note>
        {l.connect}{" "}
        <a href={`${PROFILE_PATH}#${ARMATURE_SECTION_ID}`} className="underline" data-action="connect-armature-hint">
          {l.connectLink}
        </a>
      </Note>
    );
  } else if (search.error instanceof ApiError && search.error.code === "bad_query") {
    state = "bad_query";
    body = (
      <BadQuery query={settings.query} message={search.error.message} position={search.error.position} showQuery={inEditor || Boolean(edit)} onEdit={edit} />
    );
  } else if (search.isError) {
    state = "failed";
    body = <Note>{l.failed}</Note>;
  } else if (search.isPending) {
    state = "loading";
    body = <Note status>{l.loading}</Note>;
  } else if (first?.status === "unreachable") {
    state = "unreachable";
    body = <Note>{l.unreachable}</Note>;
  } else if (rows.length === 0) {
    state = "empty";
    body = (
      <>
        <Note>{l.empty}</Note>
        {first?.url && links && <OpenInArmature url={first.url} />}
      </>
    );
  } else {
    state = "rows";
    const linked = settings.columns.includes("key") ? "key" : settings.columns[0];
    body = (
      <>
        <div className="doc-table-wrap">
          <table data-issue-table="">
            <caption className="sr-only">{l.caption(settings.query)}</caption>
            <thead>
              <tr>
                {settings.columns.map((column) => (
                  <th key={column} scope="col" aria-sort={sort?.column === column ? (sort.ascending ? "ascending" : "descending") : undefined}>
                    <button
                      type="button"
                      className="inline-flex items-center gap-1 font-semibold"
                      onClick={() => setSort(sort?.column === column ? { column, ascending: !sort.ascending } : { column, ascending: true })}
                      data-sort-column={column}
                    >
                      {l.columns[column]}
                      <span aria-hidden="true" className="text-ink-muted">
                        {sort?.column === column ? (sort.ascending ? "↑" : "↓") : ""}
                      </span>
                    </button>
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {sorted.map((issue) => (
                <tr key={issue.key} data-issue-row={issue.key}>
                  {settings.columns.map((column) => (
                    <td key={column}>{cell(issue, column, links && column === linked)}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-ink-muted">
          <span data-list-count="">{l.showing(rows.length, first?.total ?? rows.length)}</span>
          {search.hasNextPage && (
            <Button
              size="sm"
              variant="secondary"
              loading={search.isFetchingNextPage}
              onClick={() => void search.fetchNextPage()}
              data-action="show-more-issues"
            >
              {l.showMore}
            </Button>
          )}
          {first?.url && links && <OpenInArmature url={first.url} />}
        </div>
      </>
    );
  }
  return (
    <div className="doc-block" data-armature-issue-list="" data-state={state}>
      {body}
    </div>
  );
}

export function Note({ children, status = false }: { children: ReactNode; status?: boolean }) {
  return (
    <p className="doc-block-empty" role={status ? "status" : undefined}>
      {children}
    </p>
  );
}

export function OpenInArmature({ url }: { url: string }) {
  return (
    <a href={url} target="_blank" rel="noopener noreferrer" data-action="open-query-in-armature">
      {t.armature.list.open}
    </a>
  );
}

/** Armature's sentence with where it went wrong, and to an author the query with that place marked. */
export function BadQuery({
  query,
  message,
  position,
  showQuery,
  onEdit,
}: {
  query: string;
  message: string;
  position?: number;
  showQuery: boolean;
  onEdit?: () => void;
}) {
  const l = t.armature.list;
  return (
    <div className="space-y-2" data-bad-query="">
      <p className="text-sm text-danger">{l.badQuery(message, position)}</p>
      {showQuery && <MarkedQuery query={query} position={position} />}
      {onEdit && (
        <Button size="sm" variant="secondary" onClick={onEdit} data-action="edit-issue-list">
          {l.edit}
        </Button>
      )}
    </div>
  );
}

/** The query with the character at the 1-based position marked. */
export function MarkedQuery({ query, position }: { query: string; position?: number }) {
  const chars = [...query];
  const at = position && position >= 1 ? Math.min(position, chars.length + 1) - 1 : null;
  return (
    <code className="block rounded-control bg-surface-raised px-2 py-1 text-sm whitespace-pre-wrap text-ink" data-marked-query="">
      {at === null ? (
        query
      ) : (
        <>
          {chars.slice(0, at).join("")}
          <mark className="rounded-sm bg-danger-subtle text-ink underline" data-query-position={at + 1}>
            {chars[at] ?? " "}
          </mark>
          {chars.slice(at + 1).join("")}
        </>
      )}
    </code>
  );
}

function cell(issue: ArmatureIssue, column: ArmatureColumn, link: boolean): ReactNode {
  const c = t.armature.chip;
  let text: ReactNode;
  switch (column) {
    case "key":
      text = issue.key;
      break;
    case "summary":
      text = issue.summary;
      break;
    case "type":
      text = (
        <span className="inline-flex items-center gap-1">
          <TypeIcon icon={issue.type.icon} name="" />
          {issue.type.name}
        </span>
      );
      break;
    case "status":
      text = <StatusLozenge name={issue.status.name} category={issue.status.category} />;
      break;
    case "priority":
      text = c.priorities[issue.priority] ?? issue.priority;
      break;
    case "assignee":
      text = issue.assignee?.name ?? c.unassigned;
      break;
    case "reporter":
      text = issue.reporter?.name ?? t.armature.block.none;
      break;
    case "created":
      text = onDay.format(new Date(issue.createdAt));
      break;
    case "updated":
      text = onDay.format(new Date(issue.updatedAt));
      break;
    case "due":
      text = formatDue(issue.dueDate) ?? t.armature.block.none;
      break;
  }
  return link ? (
    <a href={issue.url} target="_blank" rel="noopener noreferrer">
      {text}
    </a>
  ) : (
    text
  );
}

// Keys sort by project, then by number as a number, so CP-10 follows CP-9.
const KEY_NUMBER_DIGITS = 18;

function sortKey(issue: ArmatureIssue, column: ArmatureColumn): string | number | null {
  switch (column) {
    case "key": {
      const dash = issue.key.lastIndexOf("-");
      return `${issue.key.slice(0, dash)}-${issue.key.slice(dash + 1).padStart(KEY_NUMBER_DIGITS, "0")}`;
    }
    case "summary":
      return issue.summary.toLowerCase();
    case "type":
      return issue.type.name.toLowerCase();
    case "status":
      return `${ARMATURE_STATUS_CATEGORIES.indexOf(issue.status.category)}:${issue.status.name.toLowerCase()}`;
    case "priority":
      return ARMATURE_PRIORITIES.indexOf(issue.priority);
    case "assignee":
      return issue.assignee?.name.toLowerCase() ?? null;
    case "reporter":
      return issue.reporter?.name.toLowerCase() ?? null;
    case "created":
      return Date.parse(issue.createdAt);
    case "updated":
      return Date.parse(issue.updatedAt);
    case "due":
      return issue.dueDate ? Date.parse(issue.dueDate) : null;
  }
}

/** The rows in the order of one column, empty values last either way; without a sort, as the query ordered them. */
export function sortRows(rows: readonly ArmatureIssue[], sort: Sort): ArmatureIssue[] {
  if (!sort) return [...rows];
  const direction = sort.ascending ? 1 : -1;
  return [...rows].sort((a, b) => {
    const x = sortKey(a, sort.column);
    const y = sortKey(b, sort.column);
    if (x === y) return 0;
    if (x === null) return 1;
    if (y === null) return -1;
    return (x < y ? -1 : 1) * direction;
  });
}
