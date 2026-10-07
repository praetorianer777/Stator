import { useMemo, useState, type ReactNode } from "react";
import { usePropertiesReport } from "@/api/properties";
import type { DocNode } from "@/features/editor/schema";
import { PageLink } from "@/features/pages/PageLink";
import { locale, t } from "@/i18n";
import { compareValues, type ReportSettings } from "./report";

type Sort = { column: number; descending: boolean };

/**
 * A register of the pages that carry every label given, one row each, with
 * the value of each property their properties blocks set. draw renders a
 * value's inline content, which the read view and the editor do alike.
 */
export function PropertiesReport({
  settings,
  draw,
  inEditor = false,
}: {
  settings: ReportSettings;
  draw: (content: DocNode[]) => ReactNode;
  inEditor?: boolean;
}) {
  const r = t.properties.report;
  const answer = usePropertiesReport(settings);
  // -1 is the page's title, the order the server answers in.
  const [sort, setSort] = useState<Sort>({ column: -1, descending: false });
  const report = answer.data;
  const rows = useMemo(() => {
    if (!report) return [];
    if (sort.column < 0) return sort.descending ? [...report.rows].reverse() : report.rows;
    const lang = locale();
    const sorted = [...report.rows].sort((a, b) => compareValues(a.values[sort.column]?.text || null, b.values[sort.column]?.text || null, lang));
    return sort.descending ? sorted.reverse() : sorted;
  }, [report, sort]);

  let state: string;
  let body: ReactNode;
  if (answer.isPending) {
    state = "loading";
    body = (
      <p className="doc-block-empty" role="status">
        {r.loading}
      </p>
    );
  } else if (answer.isError || !report) {
    state = "failed";
    body = <p className="doc-block-empty">{r.failed}</p>;
  } else if (report.rows.length === 0) {
    state = "empty";
    body = <p className="doc-block-empty">{r.empty(settings.labels)}</p>;
  } else {
    state = "report";
    const header = (label: string, column: number) => {
      const active = sort.column === column;
      return (
        <th key={column} scope="col" aria-sort={active ? (sort.descending ? "descending" : "ascending") : undefined}>
          <button
            type="button"
            className="doc-report-sort"
            onClick={() => setSort({ column, descending: active ? !sort.descending : false })}
            data-sort-column={column}
          >
            {label}
            <span aria-hidden="true">{active ? (sort.descending ? " ↓" : " ↑") : ""}</span>
          </button>
        </th>
      );
    };
    body = (
      <div className="doc-report-scroll">
        <table className="doc-report" data-properties-report-table="">
          <caption className="sr-only">{r.caption(settings.labels, report.rows.length)}</caption>
          <thead>
            <tr>
              {header(r.page, -1)}
              {report.columns.map((name, i) => header(name, i))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.pageId} data-report-row={row.title}>
                <th scope="row">
                  {inEditor ? (
                    row.title
                  ) : (
                    <PageLink spaceKey={row.spaceKey} id={row.pageId} title={row.title}>
                      {row.title}
                    </PageLink>
                  )}
                </th>
                {row.values.map((value, i) => (
                  <td key={i}>{value ? draw(value.content as DocNode[]) : null}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    );
  }
  return (
    <figure className="doc-properties-report" data-properties-report="" data-state={state}>
      <figcaption className="doc-chart-title">{r.title(settings.labels, settings.space)}</figcaption>
      {body}
      {report?.truncated && <p className="doc-roadmap-note">{r.truncated}</p>}
    </figure>
  );
}
