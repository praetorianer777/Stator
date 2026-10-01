import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { pagesQueryKey, type Page } from "./pages";
import type { components } from "./schema";
import { searchQueryKey } from "./search";
import { spaceQueryKey, spacesQueryKey, type Space } from "./spaces";
import { homeQueryKey } from "./stars";
import { treeQueryKey } from "./tree";

/** How a page came to be archived: with an archived page, itself or one above it, with its space, or both. */
export type Archive = components["schemas"]["Archive"];
/** An archived page and the pages archived with it, as a space's archive lists them. */
export type ArchiveItem = components["schemas"]["ArchiveItem"];

export function archiveQueryKey(spaceKey: string) {
  return ["archive", spaceKey.toUpperCase()] as const;
}

export function useArchivedPages(spaceKey: string) {
  return useQuery({
    queryKey: archiveQueryKey(spaceKey),
    queryFn: async (): Promise<ArchiveItem[]> => (await api.GET("/spaces/{spaceKey}/archived-pages", { params: { path: { spaceKey } } })).data!.items,
  });
}

// Archiving changes what every list shows and what every page offers, so all of them are read again.
function refresh(queryClient: QueryClient) {
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: ["archive"] }),
    queryClient.invalidateQueries({ queryKey: treeQueryKey }),
    queryClient.invalidateQueries({ queryKey: pagesQueryKey }),
    queryClient.invalidateQueries({ queryKey: searchQueryKey }),
    queryClient.invalidateQueries({ queryKey: homeQueryKey }),
  ]);
}

/** Archives a page with every page below it, or with archived false unarchives the pages archived with it. */
export function useArchivePage() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, archived }: { id: string; archived: boolean }): Promise<Page> => {
      const params = { params: { path: { pageID: id } } };
      const answer = archived ? await api.PUT("/pages/{pageID}/archive", params) : await api.DELETE("/pages/{pageID}/archive", params);
      return answer.data!.page as Page;
    },
    onSuccess: () => refresh(queryClient),
  });
}

/** Archives a whole space, or with archived false unarchives it. */
export function useArchiveSpace(key: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (archived: boolean): Promise<Space> => {
      const params = { params: { path: { spaceKey: key } } };
      const answer = archived ? await api.PUT("/spaces/{spaceKey}/archive", params) : await api.DELETE("/spaces/{spaceKey}/archive", params);
      return answer.data!.space;
    },
    onSuccess: (updated) => {
      queryClient.setQueryData(spaceQueryKey(updated.key), updated);
      return Promise.all([queryClient.invalidateQueries({ queryKey: spacesQueryKey }), refresh(queryClient)]);
    },
  });
}
