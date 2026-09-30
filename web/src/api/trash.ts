import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { pagesQueryKey, type Page } from "./pages";
import type { components } from "./schema";
import { treeQueryKey } from "./tree";

/** A deleted page and everything that went with it. */
export type TrashItem = components["schemas"]["TrashItem"];

export function trashQueryKey(spaceKey: string) {
  return ["trash", spaceKey] as const;
}

export function useTrash(spaceKey: string, enabled = true) {
  return useQuery({
    enabled,
    queryKey: trashQueryKey(spaceKey),
    queryFn: async (): Promise<TrashItem[]> => (await api.GET("/spaces/{spaceKey}/trash", { params: { path: { spaceKey } } })).data!.items,
  });
}

// Anything in or out of the trash changes the tree, the pages read and the trash itself.
function refresh(queryClient: QueryClient, spaceKey: string) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: trashQueryKey(spaceKey) }),
    queryClient.invalidateQueries({ queryKey: treeQueryKey }),
    queryClient.invalidateQueries({ queryKey: pagesQueryKey }),
  ]);
}

/** Moves a page and everything below it to the trash. */
export function useTrashPage(spaceKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE("/pages/{pageID}", { params: { path: { pageID: id } } });
    },
    onSuccess: (_, id) => {
      queryClient.removeQueries({ queryKey: [...pagesQueryKey, id] });
      return refresh(queryClient, spaceKey);
    },
  });
}

export function useRestorePage(spaceKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string): Promise<Page> =>
      (await api.POST("/spaces/{spaceKey}/trash/{pageID}/restore", { params: { path: { spaceKey, pageID: id } } })).data!.page as Page,
    onSuccess: () => refresh(queryClient, spaceKey),
  });
}

export function usePurgePage(spaceKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE("/spaces/{spaceKey}/trash/{pageID}", { params: { path: { spaceKey, pageID: id } } });
    },
    onSuccess: () => refresh(queryClient, spaceKey),
  });
}

export function useEmptyTrash(spaceKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/spaces/{spaceKey}/trash", { params: { path: { spaceKey } } });
    },
    onSuccess: () => refresh(queryClient, spaceKey),
  });
}
