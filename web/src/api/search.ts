import { useEffect, useRef } from "react";
import { keepPreviousData, useQuery, useQueryClient } from "@tanstack/react-query";
import { QUICK_SEARCH_LIMIT, RECENT_PAGES_LIMIT, SEARCH_PAGE_SIZE, SEARCH_PEOPLE_LIMIT } from "@/config";
import { api } from "./client";
import { pageReadersQueryKey, pageViewsQueryKey } from "./pageviews";
import type { components } from "./schema";

type Wire = components["schemas"];

/** One search result: a page, a file on it or a comment, with its title and snippet in marked runs. */
export type Hit = Wire["Hit"];
export type HitType = Hit["type"];
/** A run of text, marked when it is what matched. */
export type Segment = Wire["Segment"];
/** A page quick search found, with the titles above it. */
export type PageHit = Wire["PageHit"];
export type RecentPage = Wire["RecentPage"];
export type Person = Wire["Person"];

export const HIT_TYPES: readonly HitType[] = ["page", "attachment", "comment"];
export type SearchSort = "relevance" | "updated";

/** What the full search asks for; every list matches any of its values, and an empty one leaves it out. */
export interface SearchRequest {
  q: string;
  space: string[];
  type: HitType[];
  label: string[];
  author: string[];
  updatedAfter?: string;
  updatedBefore?: string;
  sort?: SearchSort;
  /** Finds archived pages, and pages of archived spaces, too. */
  archived?: boolean;
  offset: number;
}

export const searchQueryKey = ["search"] as const;
export const recentPagesQueryKey = ["recent-pages"] as const;

const orNothing = <T>(values: T[]): T[] | undefined => (values.length > 0 ? values : undefined);

export function useSearch(request: SearchRequest) {
  return useQuery({
    queryKey: [...searchQueryKey, "full", request],
    queryFn: async () =>
      (
        await api.GET("/search", {
          params: {
            query: {
              q: request.q || undefined,
              space: orNothing(request.space),
              type: orNothing(request.type),
              label: orNothing(request.label),
              author: orNothing(request.author),
              updatedAfter: request.updatedAfter,
              updatedBefore: request.updatedBefore,
              sort: request.sort,
              archived: request.archived || undefined,
              limit: SEARCH_PAGE_SIZE,
              offset: request.offset || undefined,
            },
          },
        })
      ).data!,
    // The last page of results stays up while the next one loads, so paging and filtering never blank the list.
    placeholderData: keepPreviousData,
  });
}

/** Pages whose titles start with the words typed; nothing is asked while nothing is typed. */
export function useQuickSearch(q: string) {
  const words = q.trim();
  return useQuery({
    queryKey: [...searchQueryKey, "quick", words],
    queryFn: async (): Promise<PageHit[]> => (await api.GET("/search/quick", { params: { query: { q: words, limit: QUICK_SEARCH_LIMIT } } })).data!.pages,
    enabled: words !== "",
    placeholderData: keepPreviousData,
  });
}

export function useRecentPages(enabled = true) {
  return useQuery({
    queryKey: recentPagesQueryKey,
    queryFn: async (): Promise<RecentPage[]> => (await api.GET("/recent-pages", { params: { query: { limit: RECENT_PAGES_LIMIT } } })).data!.pages,
    enabled,
  });
}

/** The people the author filter offers; until the API lists them, only the reader themselves. */
export function usePeople() {
  return useQuery({
    queryKey: ["people", "search"],
    queryFn: async (): Promise<Person[]> => (await api.GET("/people", { params: { query: { limit: SEARCH_PEOPLE_LIMIT } } })).data!.people,
  });
}

/**
 * Notes that the reader opened a page, once each time it is shown, for their
 * recent pages and its views. It never holds up the page, and a failure is not the reader's concern.
 */
export function useVisit(pageId: string | undefined) {
  const queryClient = useQueryClient();
  // Keyed on the id, so a refetch of the same page is not a second visit
  // and React's development double effect posts once.
  const noted = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (!pageId || noted.current === pageId) return;
    noted.current = pageId;
    api
      .POST("/pages/{pageID}/visit", { params: { path: { pageID: pageId } } })
      .then(async () => {
        // A first read of the counts still on its way left before the visit
        // was noted; invalidating would only wait for it, so it is dropped.
        const counted = [pageViewsQueryKey(pageId), pageReadersQueryKey(pageId)];
        await Promise.all(counted.map((queryKey) => queryClient.cancelQueries({ queryKey })));
        await Promise.all([recentPagesQueryKey, ...counted].map((queryKey) => queryClient.invalidateQueries({ queryKey })));
      })
      .catch(() => {});
  }, [pageId, queryClient]);
}
