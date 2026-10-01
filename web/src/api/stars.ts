import { useInfiniteQuery, useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { HOME_EDITED_PAGE_SIZE, HOME_STARS_PAGE_SIZE, HOME_UPDATES_PAGE_SIZE } from "@/config";
import { api } from "./client";
import { pageQueryKey, pagesQueryKey, type PageInSpace } from "./pages";
import type { components } from "./schema";
import { spaceQueryKey, spacesQueryKey, type Space } from "./spaces";

type Wire = components["schemas"];

/** One of the caller's stars, on a page or on a space. */
export type Star = Wire["Star"];
/** A page somebody else published, as it is now. */
export type PageUpdate = Wire["PageUpdate"];
/** A page the caller published, holds a draft of, or made and never published. */
export type EditedPage = Wire["EditedPage"];
/** Which updates to read: everything the caller may view, or what they watch. */
export type UpdateScope = "all" | "watched";

export const homeQueryKey = ["home"] as const;
const starsQueryKey = [...homeQueryKey, "stars"] as const;

/** Stars a page or takes the star off; the page shows it at once, the home list on its next read. */
export function useStarPage(pageId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (starred: boolean) => {
      const params = { params: { path: { pageID: pageId } } };
      if (starred) await api.PUT("/pages/{pageID}/star", params);
      else await api.DELETE("/pages/{pageID}/star", params);
      return starred;
    },
    onSuccess: (starred) => {
      queryClient.setQueryData<PageInSpace>(pageQueryKey(pageId), (current) => (current ? { ...current, page: { ...current.page, starred } } : current));
      restarred(queryClient);
    },
  });
}

/** Stars a space or takes the star off. */
export function useStarSpace(spaceKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (starred: boolean) => {
      const params = { params: { path: { spaceKey } } };
      if (starred) await api.PUT("/spaces/{spaceKey}/star", params);
      else await api.DELETE("/spaces/{spaceKey}/star", params);
      return starred;
    },
    onSuccess: (starred) => {
      queryClient.setQueryData<Space>(spaceQueryKey(spaceKey), (current) => (current ? { ...current, starred } : current));
      queryClient.setQueryData<Space[]>(spacesQueryKey, (current) => current?.map((each) => (each.key === spaceKey ? { ...each, starred } : each)));
      // Every page read so far carries its space, the star with it.
      void queryClient.invalidateQueries({ queryKey: pagesQueryKey });
      restarred(queryClient);
    },
  });
}

/** Takes the star off whatever one of the home page's stars is on. */
export function useUnstar() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (star: Star) => {
      if (star.page) await api.DELETE("/pages/{pageID}/star", { params: { path: { pageID: star.page.id } } });
      else await api.DELETE("/spaces/{spaceKey}/star", { params: { path: { spaceKey: star.spaceKey } } });
      return star;
    },
    onSuccess: (star) => {
      if (star.page) void queryClient.invalidateQueries({ queryKey: pageQueryKey(star.page.id) });
      else {
        void queryClient.invalidateQueries({ queryKey: spaceQueryKey(star.spaceKey) });
        void queryClient.invalidateQueries({ queryKey: spacesQueryKey });
      }
      restarred(queryClient);
    },
  });
}

function restarred(queryClient: QueryClient) {
  void queryClient.invalidateQueries({ queryKey: starsQueryKey });
}

/** The caller's stars, the latest first, a window at a time. */
export function useStars() {
  return useInfiniteQuery({
    queryKey: starsQueryKey,
    initialPageParam: "",
    queryFn: async ({ pageParam }) => (await api.GET("/stars", { params: { query: { limit: HOME_STARS_PAGE_SIZE, cursor: pageParam || undefined } } })).data!,
    getNextPageParam: (last) => last.next ?? undefined,
  });
}

/** What others published, everywhere or in what the caller watches, the latest first. */
export function useUpdates(scope: UpdateScope) {
  return useInfiniteQuery({
    queryKey: [...homeQueryKey, "updates", scope],
    initialPageParam: "",
    queryFn: async ({ pageParam }) =>
      (await api.GET("/home/updates", { params: { query: { scope, limit: HOME_UPDATES_PAGE_SIZE, cursor: pageParam || undefined } } })).data!,
    getNextPageParam: (last) => last.next ?? undefined,
  });
}

/** What the caller edited, drafts and unpublished pages included, the latest first. */
export function useEdited() {
  return useInfiniteQuery({
    queryKey: [...homeQueryKey, "edited"],
    initialPageParam: "",
    queryFn: async ({ pageParam }) =>
      (await api.GET("/home/edited", { params: { query: { limit: HOME_EDITED_PAGE_SIZE, cursor: pageParam || undefined } } })).data!,
    getNextPageParam: (last) => last.next ?? undefined,
  });
}
