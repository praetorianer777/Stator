import { keepPreviousData, useMutation, useQuery } from "@tanstack/react-query";
import { PICKER_LIMIT, SHARE_VIEWERS_SHOWN } from "@/config";
import { api } from "./client";
import type { SubjectRef, SubjectSearch, SubjectSearchResult } from "./permissions";
import type { components } from "./schema";

type Wire = components["schemas"];

/** A page sent: whom the sharer named, and how many people that told. */
export type Share = Wire["Share"];
export type Recipient = Wire["Recipient"];
export type RecipientGroup = Wire["RecipientGroup"];

export function viewersQueryKey(pageId: string) {
  return ["page-viewers", pageId] as const;
}

/** The first people who may view a page, how many there are, and whether that is everybody. */
export function useViewers(pageId: string) {
  return useQuery({
    queryKey: viewersQueryKey(pageId),
    queryFn: async () => (await api.GET("/pages/{pageID}/viewers", { params: { path: { pageID: pageId }, query: { limit: SHARE_VIEWERS_SHOWN } } })).data!,
  });
}

/** The share picker's source: people and groups as the other pickers find them, each saying who of them may view the page. */
export function shareSearch(pageId: string): SubjectSearch {
  return function useShareSearch(q: string, enabled: boolean): SubjectSearchResult {
    const query = { q: q || undefined, limit: PICKER_LIMIT };
    const found = useQuery({
      queryKey: ["share-recipients", pageId, query],
      queryFn: async () => (await api.GET("/pages/{pageID}/share/recipients", { params: { path: { pageID: pageId }, query } })).data!,
      enabled,
      placeholderData: keepPreviousData,
    });
    return {
      people: found.data?.people ?? [],
      groups: found.data?.groups ?? [],
      error: found.error,
      current: found.data !== undefined && !found.isPlaceholderData,
    };
  };
}

/** Sends the page to the people and groups named, with an optional note. */
export function useSharePage(pageId: string) {
  return useMutation({
    mutationFn: async (input: { recipients: SubjectRef[]; message: string }): Promise<Share> =>
      (await api.POST("/pages/{pageID}/share", { params: { path: { pageID: pageId } }, body: input })).data!.share,
  });
}
