import type { ReactNode } from "react";
import { useLabelledPages, useUpdatedPages } from "@/api/pageLists";
import { PageLink } from "@/features/pages/PageLink";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import type { LabelledSettings, UpdatedSettings } from "./lists";

const day = localDateFormat({ dateStyle: "medium" });

interface Listed {
  id: string;
  title: string;
  spaceKey: string;
  spaceName: string;
  /** When the page was last published. */
  at: string;
  /** What else a row says, such as who published it. */
  detail?: string;
}

/**
 * One list block's frame: its title, then the pages or a sentence saying why
 * there are none. In the editor a page's title is no link, so a click selects
 * the block rather than leaving the page mid-edit.
 */
function PageList({
  kind,
  title,
  query,
  rows,
  empty,
  inEditor,
}: {
  kind: string;
  title: string;
  query: { isPending: boolean; isError: boolean };
  rows: Listed[] | undefined;
  empty: string;
  inEditor: boolean;
}) {
  const l = t.pageLists;
  let state: string;
  let body: ReactNode;
  if (query.isPending) {
    state = "loading";
    body = (
      <p className="doc-block-empty" role="status">
        {l.loading}
      </p>
    );
  } else if (query.isError || !rows) {
    state = "failed";
    body = <p className="doc-block-empty">{l.failed}</p>;
  } else if (rows.length === 0) {
    state = "empty";
    body = <p className="doc-block-empty">{empty}</p>;
  } else {
    state = "list";
    body = (
      <ul className="doc-page-list">
        {rows.map((row) => (
          <li key={row.id} data-listed-page={row.title}>
            {inEditor ? (
              <span className="doc-page-list-title">{row.title}</span>
            ) : (
              <PageLink spaceKey={row.spaceKey} id={row.id} title={row.title} className="doc-page-list-title" />
            )}
            <span className="doc-page-list-meta">
              {[row.spaceName, row.detail].filter(Boolean).join(" · ")} · <time dateTime={row.at}>{day.format(new Date(row.at))}</time>
            </span>
          </li>
        ))}
      </ul>
    );
  }
  return (
    <section className="doc-page-list-block" aria-label={title} data-page-list={kind} data-state={state}>
      <p className="doc-chart-title">{title}</p>
      {body}
    </section>
  );
}

/** The published pages carrying the labels, all or any, as the reader may read them. */
export function LabelledPages({ settings, inEditor = false }: { settings: LabelledSettings; inEditor?: boolean }) {
  const l = t.pageLists;
  const query = useLabelledPages(settings);
  const rows = query.data?.map((p) => ({ id: p.id, title: p.title, spaceKey: p.spaceKey, spaceName: p.spaceName, at: p.updatedAt }));
  return (
    <PageList
      kind="labelled"
      title={l.labelledTitle(settings.labels, settings.match, settings.space)}
      query={query}
      rows={rows}
      empty={l.labelledEmpty}
      inEditor={inEditor}
    />
  );
}

/** The pages published last, in a space or anywhere, as the reader may read them. */
export function UpdatedPages({ settings, inEditor = false }: { settings: UpdatedSettings; inEditor?: boolean }) {
  const l = t.pageLists;
  const query = useUpdatedPages(settings);
  const rows = query.data?.map((p) => ({
    id: p.id,
    title: p.title,
    spaceKey: p.spaceKey,
    spaceName: p.spaceName,
    at: p.publishedAt,
    detail: p.authorName ? l.by(p.authorName) : undefined,
  }));
  return <PageList kind="updated" title={l.updatedTitle(settings.space)} query={query} rows={rows} empty={l.updatedEmpty} inEditor={inEditor} />;
}
