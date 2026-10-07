import { useQuery } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** Somebody who published versions of the pages a contributors block counts. */
export type Contributor = components["schemas"]["Contributor"];

export const contributorsQueryKey = ["contributors"] as const;

/** Who published the page, or it and the pages below it, as the reader may see them. */
export function useContributors(pageId: string | undefined, scope: "page" | "tree", limit: number) {
  return useQuery({
    queryKey: [...contributorsQueryKey, pageId, scope, limit],
    queryFn: async () => (await api.GET("/pages/{pageID}/contributors", { params: { path: { pageID: pageId! }, query: { scope, limit } } })).data!,
    enabled: Boolean(pageId),
  });
}
