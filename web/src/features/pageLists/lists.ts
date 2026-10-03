import { PAGE_LIST_DEFAULT_LIMIT, PAGE_LIST_MATCHES, PAGE_LIST_MAX_LIMIT, PAGE_LIST_SORTS } from "@/config";
import { reportLabels } from "@/features/properties/report";

export const LABELLED_PAGES_NODE = "labelledPages";
export const RECENTLY_UPDATED_NODE = "recentlyUpdated";

export type ListMatch = (typeof PAGE_LIST_MATCHES)[number];
export type ListSort = (typeof PAGE_LIST_SORTS)[number];

/** What a content by label block stores: what to list, never the pages. */
export interface LabelledSettings {
  labels: string[];
  match: ListMatch;
  /** A space's key to stay inside, or null for every space. */
  space: string | null;
  sort: ListSort;
  limit: number;
}

/** What a recently updated block stores. */
export interface UpdatedSettings {
  space: string | null;
  limit: number;
}

const SPACE = /^[A-Z][A-Z0-9]{1,9}$/;

function spaceOf(value: unknown): string | null {
  return typeof value === "string" && SPACE.test(value) ? value : null;
}

function limitOf(value: unknown): number {
  const n = Number(value);
  return Number.isInteger(n) && n >= 1 && n <= PAGE_LIST_MAX_LIMIT ? n : PAGE_LIST_DEFAULT_LIMIT;
}

/** A block's attributes as the server takes them, each one wrong put right. */
export function labelledSettings(attrs: Record<string, unknown> | undefined): LabelledSettings {
  return {
    labels: reportLabels(Array.isArray(attrs?.labels) ? attrs.labels : []),
    match: (PAGE_LIST_MATCHES as readonly unknown[]).includes(attrs?.match) ? (attrs!.match as ListMatch) : "all",
    space: spaceOf(attrs?.space),
    sort: (PAGE_LIST_SORTS as readonly unknown[]).includes(attrs?.sort) ? (attrs!.sort as ListSort) : "updated",
    limit: limitOf(attrs?.limit),
  };
}

export function updatedSettings(attrs: Record<string, unknown> | undefined): UpdatedSettings {
  return { space: spaceOf(attrs?.space), limit: limitOf(attrs?.limit) };
}
