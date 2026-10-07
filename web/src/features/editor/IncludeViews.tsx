import { createContext, useContext, type ReactNode } from "react";
import { useIncluded } from "@/api/included";
import { ApiError } from "@/api/client";
import { Icon } from "@/components/icons";
import { t } from "@/i18n";
import { pageSlug } from "@/lib/slug";
import type { Doc } from "./schema";

// Kept apart from the editor's node, so the read-only view draws an include
// without bringing the editor along.

export const INCLUDE_NODE = "include";

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

/** An id the API takes, or null. */
export function includeId(value: unknown): string | null {
  return typeof value === "string" && UUID.test(value) ? value : null;
}

/**
 * The pages an include sits in, outermost first: the page being read, then
 * each included page on the way down. The server refuses one that leads back.
 */
export const IncludeChain = createContext<string[]>([]);

/**
 * What another page, or one excerpt of it, shows this reader, framed and
 * named with a link to where it comes from. Draw renders the included
 * document; it is the read-only view's, passed in so this module needs no
 * part of it.
 */
export function IncludeBlock({ pageId, excerptId, draw }: { pageId: string; excerptId: string | null; draw: (doc: Doc) => ReactNode }) {
  const via = useContext(IncludeChain);
  const { data, error, isPending } = useIncluded(pageId, excerptId, via);
  if (isPending) {
    return (
      <div className="doc-include" data-include={pageId} data-state="loading">
        <p className="doc-include-note">{t.editor.include.loading}</p>
      </div>
    );
  }
  if (!data) {
    // Cycles and depth say what they are; anything else is one notice, so a
    // reader learns nothing of a page kept from them.
    const said = error instanceof ApiError && error.status === 409 ? error.message : t.editor.include.unavailable;
    return (
      <div className="doc-include" data-include={pageId} data-state="unavailable" role="note">
        <p className="doc-include-note">
          <Icon.Lock size={14} />
          {said}
        </p>
      </div>
    );
  }
  const from = data.excerpt ? t.editor.include.fromExcerpt(data.page.title, data.excerpt.name) : t.editor.include.fromPage(data.page.title);
  return (
    <section className="doc-include" data-include={pageId} data-state="shown" aria-label={from}>
      <div className="doc-include-head">
        <a href={`/s/${data.page.spaceKey}/p/${data.page.id}/${pageSlug(data.page.title)}`} className="doc-include-source">
          {from}
        </a>
      </div>
      <IncludeChain value={[...via, pageId]}>{draw(data.body as Doc)}</IncludeChain>
    </section>
  );
}
