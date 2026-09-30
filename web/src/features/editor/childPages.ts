import { CHILD_PAGES_MAX_DEPTH } from "@/config";
import { CHILD_PAGES_SCOPES, CHILD_PAGES_SORTS, type ChildPagesScope, type ChildPagesSort } from "./schema";

/** What a child pages block lists: the direct children or the subtree, how deep, and in which order. */
export interface ChildPagesOptions {
  scope: ChildPagesScope;
  /** Levels below the page for a subtree; null is every level. */
  depth: number | null;
  sort: ChildPagesSort;
}

export const defaultChildPages: ChildPagesOptions = { scope: "children", depth: null, sort: "tree" };

function oneOf<T extends string>(values: readonly T[], value: unknown, fallback: T): T {
  return typeof value === "string" && (values as readonly string[]).includes(value) ? (value as T) : fallback;
}

/** A block's attributes as options the API takes, whatever a stored document holds. */
export function childPagesOptions(attrs: Record<string, unknown> | undefined): ChildPagesOptions {
  const depth = attrs?.depth;
  return {
    scope: oneOf(CHILD_PAGES_SCOPES, attrs?.scope, defaultChildPages.scope),
    depth: typeof depth === "number" && Number.isInteger(depth) && depth >= 1 && depth <= CHILD_PAGES_MAX_DEPTH ? depth : null,
    sort: oneOf(CHILD_PAGES_SORTS, attrs?.sort, defaultChildPages.sort),
  };
}

/** The query the API's list of the pages below a page is asked with; a depth means something only for a subtree. */
export function belowQuery(options: ChildPagesOptions): { scope: ChildPagesScope; sort: ChildPagesSort; depth?: number } {
  return options.scope === "subtree" && options.depth !== null
    ? { scope: options.scope, sort: options.sort, depth: options.depth }
    : { scope: options.scope, sort: options.sort };
}

/** A listed page with the pages listed under it. */
export interface Nested<T> {
  page: T;
  children: Nested<T>[];
}

/** The API's list, each page after its parent, as a tree; the order within each level is kept. */
export function nestBelow<T extends { id: string; parentId: string }>(pages: T[], rootId: string): Nested<T>[] {
  const byId = new Map<string, Nested<T>>();
  const roots: Nested<T>[] = [];
  for (const page of pages) {
    const entry: Nested<T> = { page, children: [] };
    byId.set(page.id, entry);
    const parent = page.parentId === rootId ? undefined : byId.get(page.parentId);
    (parent ? parent.children : roots).push(entry);
  }
  return roots;
}
