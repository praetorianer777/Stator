import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { PAGE_READERS_PAGE_SIZE } from "@/config";
import { meQueryKey, type Me } from "./auth";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** How often a page was read, each person once a day, in all and over the last days. */
export type PageViews = Wire["ViewCounts"];
/** Somebody who read the page within the retention, with on how many days. */
export type PageReader = Wire["Reader"];

export const pageViewsQueryKey = (pageId: string) => ["page-views", pageId] as const;
export const pageReadersQueryKey = (pageId: string) => ["page-readers", pageId] as const;

/** A page's counts; nothing is asked for a page that is not published, which only its author reads. */
export function usePageViews(pageId: string, enabled = true) {
  return useQuery({
    queryKey: pageViewsQueryKey(pageId),
    queryFn: async (): Promise<PageViews> => (await api.GET("/pages/{pageID}/views", { params: { path: { pageID: pageId } } })).data!,
    enabled,
  });
}

/** Who read a page, the latest first, a window at a time; only the page's editors ask. */
export function usePageReaders(pageId: string, enabled = true) {
  return useInfiniteQuery({
    queryKey: pageReadersQueryKey(pageId),
    queryFn: async ({ pageParam }) =>
      (await api.GET("/pages/{pageID}/readers", { params: { path: { pageID: pageId }, query: { limit: PAGE_READERS_PAGE_SIZE, cursor: pageParam } } })).data!,
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next ?? undefined,
    enabled,
  });
}

/** Whether editors of the pages the caller reads see their name among its readers. */
export function useSetShowInReaders() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (showInReaders: boolean): Promise<Me> => (await api.PATCH("/auth/me", { body: { showInReaders } })).data!,
    onSuccess: (me) => {
      queryClient.setQueryData(meQueryKey, me);
      void queryClient.invalidateQueries({ queryKey: ["page-readers"] });
    },
  });
}
