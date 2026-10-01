import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { pageQueryKey, type Page, type PageInSpace } from "./pages";
import type { components } from "./schema";
import { searchQueryKey } from "./search";
import { homeQueryKey } from "./stars";

type Wire = components["schemas"];

/** The person who answers for a page. */
export type Owner = Wire["Owner"];
/** The last check that a page is right, and until when it holds. */
export type Verification = Wire["Verification"];

function patchPage(queryClient: QueryClient, pageId: string, change: Partial<Page>) {
  queryClient.setQueryData<PageInSpace>(pageQueryKey(pageId), (current) => (current ? { ...current, page: { ...current.page, ...change } } : current));
  // The home page and search badge a verified page, so they read it again.
  void queryClient.invalidateQueries({ queryKey: homeQueryKey });
  void queryClient.invalidateQueries({ queryKey: searchQueryKey });
}

/** Names a page's owner, or with null leaves it without one. */
export function useSetOwner(pageId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (userId: string | null): Promise<Owner | null> => {
      const path = { params: { path: { pageID: pageId } } };
      if (userId === null) {
        await api.DELETE("/pages/{pageID}/owner", path);
        return null;
      }
      return (await api.PUT("/pages/{pageID}/owner", { ...path, body: { userId } })).data!.owner;
    },
    onSuccess: (owner) => patchPage(queryClient, pageId, { owner }),
  });
}

/** Verifies a page for a number of days, or with null takes its verification away. */
export function useVerify(pageId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (days: number | null): Promise<Verification | null> => {
      const path = { params: { path: { pageID: pageId } } };
      if (days === null) {
        await api.DELETE("/pages/{pageID}/verification", path);
        return null;
      }
      return (await api.PUT("/pages/{pageID}/verification", { ...path, body: { days } })).data!.verification;
    },
    onSuccess: (verification) => patchPage(queryClient, pageId, { verification }),
  });
}
