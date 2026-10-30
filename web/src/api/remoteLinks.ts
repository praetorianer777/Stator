import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useMutation } from "./mutation";
import { api } from "./client";
import type { components } from "./schema";

/** An object in another tool about a page, put there by that tool. */
export type RemoteLink = components["schemas"]["PageRemoteLink"];

export const remoteLinksQueryKey = (pageId: string) => ["remote-links", pageId] as const;

/** A page's remote links, by source and then in the order they were put. */
export function useRemoteLinks(pageId: string) {
  return useQuery({
    queryKey: remoteLinksQueryKey(pageId),
    queryFn: async (): Promise<RemoteLink[]> => (await api.GET("/pages/{pageID}/remote-links", { params: { path: { pageID: pageId } } })).data!.remoteLinks,
  });
}

/** Takes a link off a page; the tool that put it hears so through its webhook. */
export function useRemoveRemoteLink(pageId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (link: RemoteLink): Promise<RemoteLink> => {
      await api.DELETE("/pages/{pageID}/remote-links/{linkID}", { params: { path: { pageID: pageId, linkID: link.id } } });
      return link;
    },
    onSuccess: (link) => queryClient.setQueryData<RemoteLink[]>(remoteLinksQueryKey(pageId), (current) => current?.filter((each) => each.id !== link.id)),
  });
}
