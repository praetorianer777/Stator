import { useContext, type ReactNode } from "react";
import { useContributors } from "@/api/contributors";
import { Avatar } from "@/components/ui";
import { DocPageContext } from "@/features/editor/BlockViews";
import { t } from "@/i18n";
import { localDateFormat } from "@/lib/format";
import type { ContributorsSettings } from "./contributors";

const day = localDateFormat({ dateStyle: "medium" });

/** The people who published the page, or it and the pages below it, as the reader may see them; the most versions first. */
export function Contributors({ settings }: { settings: ContributorsSettings }) {
  const c = t.contributors;
  const page = useContext(DocPageContext);
  const query = useContributors(page?.id, settings.scope, settings.limit);
  const found = query.data;
  let state: string;
  let body: ReactNode;
  if (!page) {
    state = "unsaved";
    body = <p className="doc-block-empty">{c.unsaved}</p>;
  } else if (query.isPending) {
    state = "loading";
    body = (
      <p className="doc-block-empty" role="status">
        {c.loading}
      </p>
    );
  } else if (query.isError || !found) {
    state = "failed";
    body = <p className="doc-block-empty">{c.failed}</p>;
  } else if (found.contributors.length === 0) {
    state = "empty";
    body = <p className="doc-block-empty">{c.empty}</p>;
  } else {
    state = "list";
    body = (
      <ul className="doc-contributors">
        {found.contributors.map((person) => (
          <li key={person.id} className="doc-contributor" data-contributor={person.name}>
            <Avatar name={person.name} src={person.avatarUrl} size="md" />
            <div className="flex min-w-0 flex-col">
              <span className="doc-page-list-title truncate">{person.name}</span>
              <span className="doc-page-list-meta">{c.meta(person.edits, day.format(new Date(person.lastEditedAt)))}</span>
            </div>
          </li>
        ))}
      </ul>
    );
  }
  const title = c.title(settings.scope);
  return (
    <section className="doc-page-list-block" aria-label={title} data-contributors="" data-state={state}>
      <p className="doc-chart-title">{title}</p>
      {body}
      {found?.truncated && <p className="doc-roadmap-note">{c.truncated(found.contributors.length)}</p>}
    </section>
  );
}
