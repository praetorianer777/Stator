import { useContext, useMemo, type ReactNode } from "react";
import { ApiError } from "@/api/client";
import { exciterLacks, useExciterAccount, useExciterList } from "@/api/exciter";
import { Button, cx } from "@/components/ui";
import { EXCITER_FEATURE_CAPABILITIES, EXCITER_LIST_DEFAULT_LIMIT, EXCITER_LIST_MAX_LIMIT, EXCITER_LIST_PAGE_SIZE, EXCITER_QUERY_MAX_LENGTH } from "@/config";
import { MarkedQuery } from "@/features/armature/IssueList";
import { DocPageContext } from "@/features/editor/BlockViews";
import { t } from "@/i18n";
import { ConnectExciterLink } from "./connectLink";
import { RequirementRevision, RequirementStatusLozenge } from "./RequirementChip";
import { normalizeProjectKey } from "./requirementKeys";

/** What a requirement list stores: the project, the query and the most rows, never its rows. */
export interface RequirementListSettings {
  project: string;
  query: string;
  limit: number;
}

/** A stored list's settings, anything out of shape put back to what a new list takes. */
export function requirementListSettings(attrs: Record<string, unknown> | undefined): RequirementListSettings {
  const query = typeof attrs?.query === "string" ? [...attrs.query].slice(0, EXCITER_QUERY_MAX_LENGTH).join("") : "";
  const limit = Number(attrs?.limit);
  return {
    project: normalizeProjectKey(attrs?.project) ?? "",
    query,
    limit: Number.isInteger(limit) && limit >= 1 && limit <= EXCITER_LIST_MAX_LIMIT ? limit : EXCITER_LIST_DEFAULT_LIMIT,
  };
}

/** Exciter's sentence with where it went wrong, and to an author the query with that place marked. */
export function ExciterBadQuery({
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
  const l = t.exciter.list;
  return (
    <div className="space-y-2" data-bad-query="">
      <p className="text-sm text-danger">{l.badQuery(message, position)}</p>
      {showQuery && <MarkedQuery query={query} position={position} />}
      {onEdit && (
        <Button size="sm" variant="secondary" onClick={onEdit} data-action="edit-requirement-list">
          {l.edit}
        </Button>
      )}
    </div>
  );
}

function Note({ children, status = false }: { children: ReactNode; status?: boolean }) {
  return (
    <p className="doc-block-empty" role={status ? "status" : undefined}>
      {children}
    </p>
  );
}

/**
 * The requirements a query finds in one project, as the viewer may see them,
 * a window at a time. currentUser() is the viewer, so two readers may see
 * different rows. In the editor the rows are shown without links.
 */
export function RequirementList({ settings, inEditor = false }: { settings: RequirementListSettings; inEditor?: boolean }) {
  const l = t.exciter.list;
  const page = useContext(DocPageContext);
  const edit = inEditor ? undefined : page?.onEdit;
  const links = !inEditor;
  const account = useExciterAccount();
  const configured = Boolean(account.data?.configured && account.data.baseUrl);
  const outdated = exciterLacks(account.data, "lists");
  const asks = Boolean(configured && account.data?.connected && account.data.status !== "rejected" && !outdated);
  const list = useExciterList(settings.project, settings.query, settings.limit, EXCITER_LIST_PAGE_SIZE, asks);
  const first = list.data?.pages[0];
  const rows = useMemo(() => (list.data?.pages ?? []).flatMap((p) => (p.status === "ok" ? p.requirements : [])), [list.data]);

  let state: string;
  let body: ReactNode;
  if (account.isPending) {
    state = "loading";
    body = <Note status>{l.loading}</Note>;
  } else if (account.isError) {
    state = "failed";
    body = <Note>{l.failed}</Note>;
  } else if (!configured || first?.status === "not_configured") {
    state = "plain";
    body = <Note>{l.notConfigured}</Note>;
  } else if (outdated || first?.status === "outdated") {
    state = "outdated";
    body = <Note>{t.exciter.capabilities.outdated(t.exciter.capabilities.features.lists, EXCITER_FEATURE_CAPABILITIES.lists)}</Note>;
  } else if (!asks || first?.status === "not_connected" || first?.status === "rejected") {
    state = "connect";
    body = (
      <Note>
        {l.connect} <ConnectExciterLink className="underline">{l.connectLink}</ConnectExciterLink>
      </Note>
    );
  } else if (settings.project === "") {
    state = "no_project";
    body = <Note>{l.noProject}</Note>;
  } else if (list.error instanceof ApiError && list.error.code === "bad_query") {
    state = "bad_query";
    body = (
      <ExciterBadQuery query={settings.query} message={list.error.message} position={list.error.position} showQuery={inEditor || Boolean(edit)} onEdit={edit} />
    );
  } else if (list.isError) {
    state = "failed";
    body = <Note>{l.failed}</Note>;
  } else if (list.isPending) {
    state = "loading";
    body = <Note status>{l.loading}</Note>;
  } else if (first?.status === "unreachable") {
    state = "unreachable";
    body = <Note>{l.unreachable}</Note>;
  } else if (first && !first.found) {
    state = "unseen";
    body = <Note>{l.unseen(settings.project)}</Note>;
  } else if (rows.length === 0) {
    state = "empty";
    body = <Note>{l.empty}</Note>;
  } else {
    state = "rows";
    body = (
      <>
        <div className="doc-table-wrap">
          <table data-requirement-table="">
            <caption className="sr-only">{l.caption(settings.project, settings.query)}</caption>
            <thead>
              <tr>
                <th scope="col">{l.columns.key}</th>
                <th scope="col">{l.columns.title}</th>
                <th scope="col">{l.columns.status}</th>
                <th scope="col">{l.columns.revision}</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr key={row.key} data-requirement-row={row.key}>
                  <td className={cx("whitespace-nowrap", row.trashed && "line-through")}>
                    {links ? (
                      <a href={row.url} target="_blank" rel="noopener noreferrer">
                        {row.key}
                      </a>
                    ) : (
                      row.key
                    )}
                  </td>
                  <td>{row.title}</td>
                  <td>
                    <RequirementStatusLozenge status={row.status} />
                  </td>
                  <td>
                    <RequirementRevision revision={row.revision} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-ink-muted">
          <span data-list-count="">{l.showing(rows.length, first?.total ?? rows.length)}</span>
          {list.hasNextPage && (
            <Button
              size="sm"
              variant="secondary"
              loading={list.isFetchingNextPage}
              onClick={() => void list.fetchNextPage()}
              data-action="show-more-requirements"
            >
              {l.showMore}
            </Button>
          )}
        </div>
      </>
    );
  }
  return (
    <div className="doc-block" data-exciter-requirement-list="" data-state={state}>
      {body}
    </div>
  );
}
