import { keepPreviousData, useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { WATCHERS_LIMIT, WATCHES_PAGE_SIZE } from "@/config";
import { api } from "./client";
import { pageQueryKey, pagesQueryKey, type PageInSpace } from "./pages";
import type { components } from "./schema";
import { spaceQueryKey, spacesQueryKey } from "./spaces";

type Wire = components["schemas"];

/** How the caller follows one page: their own watch, and what covers it from above. */
export type Watching = Wire["Watching"];
/** Somebody who hears about a page, and through which watch. */
export type Watcher = Wire["Watcher"];
/** One of the caller's own watches. */
export type Watch = Wire["Watch"];
export type WatchKind = Wire["Watch"]["kind"];

export const watchesQueryKey = ["watches"] as const;

function watchersQueryKey(pageId: string) {
  return [...pageQueryKey(pageId), "watchers"] as const;
}

// A watch on a page or a space changes what covers every page below it, so
// every page read so far is read again rather than patched one by one.
function rewatched(queryClient: QueryClient) {
  void queryClient.invalidateQueries({ queryKey: pagesQueryKey });
  void queryClient.invalidateQueries({ queryKey: watchesQueryKey });
}

/** Watches a page alone, or with every page below it. */
export function useWatchPage(pageId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (subtree: boolean): Promise<Watching> =>
      (await api.PUT("/pages/{pageID}/watch", { params: { path: { pageID: pageId } }, body: subtree ? { subtree: true } : {} })).data!.watching,
    onSuccess: (watching) => {
      queryClient.setQueryData<PageInSpace>(pageQueryKey(pageId), (current) => (current ? { ...current, page: { ...current.page, watching } } : current));
      rewatched(queryClient);
    },
  });
}

/** Stops watching a page; its own edits no longer make the caller watch it again. */
export function useUnwatchPage(pageId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/pages/{pageID}/watch", { params: { path: { pageID: pageId } } });
    },
    onSuccess: () => rewatched(queryClient),
  });
}

/** Watches every page of a space, or stops; the watches on its pages stay either way. */
export function useWatchSpace(spaceKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (watch: boolean) => {
      const params = { params: { path: { spaceKey } } };
      if (watch) await api.PUT("/spaces/{spaceKey}/watch", params);
      else await api.DELETE("/spaces/{spaceKey}/watch", params);
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: spaceQueryKey(spaceKey) });
      void queryClient.invalidateQueries({ queryKey: spacesQueryKey });
      rewatched(queryClient);
    },
  });
}

/** Who hears about a page, by name. */
export function useWatchers(pageId: string) {
  return useQuery({
    queryKey: watchersQueryKey(pageId),
    queryFn: async () => (await api.GET("/pages/{pageID}/watchers", { params: { path: { pageID: pageId }, query: { limit: WATCHERS_LIMIT } } })).data!,
  });
}

/** The caller's own watches, a page of the list at a time, the latest first. */
export function useWatches(offset: number) {
  return useQuery({
    queryKey: [...watchesQueryKey, offset],
    queryFn: async () => (await api.GET("/watches", { params: { query: { limit: WATCHES_PAGE_SIZE, offset: offset || undefined } } })).data!,
    placeholderData: keepPreviousData,
  });
}

/** Ends one of the caller's watches from the list, whatever it is on. */
export function useStopWatching() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (watch: Watch) => {
      if (watch.page) await api.DELETE("/pages/{pageID}/watch", { params: { path: { pageID: watch.page.id } } });
      else await api.DELETE("/spaces/{spaceKey}/watch", { params: { path: { spaceKey: watch.spaceKey } } });
      return watch;
    },
    onSuccess: (watch) => {
      if (!watch.page) void queryClient.invalidateQueries({ queryKey: spacesQueryKey });
      rewatched(queryClient);
    },
  });
}
