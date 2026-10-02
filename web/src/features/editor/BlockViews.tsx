import { localDateFormat } from "@/lib/format";
import { createContext, useContext, type MouseEvent, type ReactNode } from "react";
import { usePagesBelow, type BelowPage } from "@/api/tree";
import { Tag } from "@/components/ui";
import { CHILD_PAGES_LIMIT, CHILD_PAGES_MAX_DEPTH } from "@/config";
import { PageLink } from "@/features/pages/PageLink";
import { t } from "@/i18n";
import { belowQuery, nestBelow, type ChildPagesOptions, type Nested } from "./childPages";
import { HEADING_LEVELS } from "./schema";
import type { TocEntry } from "./toc";

/** The page a document belongs to, which a child pages block lists the pages below. */
export interface DocPage {
  id: string;
  spaceKey: string;
  /** Opens the page to edit, for a reader who may; a block that needs fixing offers it. */
  onEdit?: () => void;
  /** Ticks a published task off or opens it again, for a reader who may edit the page. */
  toggleTask?: (taskId: string, done: boolean) => void;
}

export const DocPageContext = createContext<DocPage | null>(null);

const changedOn = localDateFormat({ dateStyle: "medium" });

/** A table of contents: nested links to the headings, or a sentence while there are none. */
export function TocList({ entries, onFollow }: { entries: TocEntry[]; onFollow: (anchor: string, event: MouseEvent<HTMLAnchorElement>) => void }) {
  if (entries.length === 0) return <p className="doc-block-empty">{t.editor.toc.empty}</p>;
  const list = (items: TocEntry[]): ReactNode => (
    <ul>
      {items.map((entry) => (
        <li key={entry.anchor}>
          <a href={`#${encodeURIComponent(entry.anchor)}`} onClick={(event) => onFollow(entry.anchor, event)} data-toc-link={entry.anchor}>
            {entry.text}
          </a>
          {entry.children.length > 0 && list(entry.children)}
        </li>
      ))}
    </ul>
  );
  return list(entries);
}

/** The pages below the document's page, fetched for the reader, with links to each. */
export function ChildPagesList({ options }: { options: ChildPagesOptions }) {
  const page = useContext(DocPageContext);
  const below = usePagesBelow(page?.id, belowQuery(options));
  if (!page) return <p className="doc-block-empty">{t.editor.childPages.noPage}</p>;
  if (below.isPending) {
    return (
      <p className="doc-block-empty" role="status">
        {t.editor.childPages.loading}
      </p>
    );
  }
  if (below.isError) return <p className="doc-block-empty">{t.editor.childPages.failed}</p>;
  const { pages, truncated } = below.data;
  if (pages.length === 0) return <p className="doc-block-empty">{t.editor.childPages.empty}</p>;
  const list = (items: Nested<BelowPage>[]): ReactNode => (
    <ul>
      {items.map(({ page: child, children }) => (
        <li key={child.id} data-child-page={child.title}>
          <PageLink spaceKey={page.spaceKey} id={child.id} title={child.title} />
          {child.unpublished && <Tag className="ml-2">{t.editor.childPages.unpublished}</Tag>}
          {options.sort === "updated" && <span className="doc-block-meta"> {t.editor.childPages.changed(changedOn.format(new Date(child.updatedAt)))}</span>}
          {children.length > 0 && list(children)}
        </li>
      ))}
    </ul>
  );
  return (
    <>
      {list(nestBelow(pages, page.id))}
      {truncated && <p className="doc-block-empty">{t.editor.childPages.truncated(CHILD_PAGES_LIMIT)}</p>}
    </>
  );
}

/** How a table of contents is described in words, where it is not drawn: a comparison. */
export function tocSummary(maxLevel: number): string {
  return t.editor.toc.summary(t.editor.toc.levelOption(maxLevel));
}

/** The same for a child pages block. */
export function childPagesSummary(options: ChildPagesOptions): string {
  const c = t.editor.childPages;
  const depth = options.scope === "subtree" ? (options.depth === null ? c.everyLevel : c.levelCount(options.depth)) : null;
  return c.summary(c.scopes[options.scope]!, depth, c.sorts[options.sort]!);
}

/** The choices of a table of contents' one setting. */
export const TOC_LEVEL_CHOICES = [...HEADING_LEVELS];
/** The levels a subtree may be cut at; the empty value is every level. */
export const CHILD_PAGES_DEPTH_CHOICES = Array.from({ length: CHILD_PAGES_MAX_DEPTH }, (_, i) => i + 1);
