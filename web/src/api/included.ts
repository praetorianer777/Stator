import { useQuery } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** What an include shows the reader: a page's published body, or one excerpt's blocks. */
export type Included = components["schemas"]["Included"];

/** Reads what an include shows; via is the chain of pages it sits in, outermost first. */
export function useIncluded(pageId: string, excerptId: string | null, via: string[]) {
  return useQuery({
    queryKey: ["pages", pageId, "included", excerptId ?? "", via.join(",")],
    queryFn: async (): Promise<Included> =>
      (
        await api.GET("/pages/{pageID}/included", {
          params: { path: { pageID: pageId }, query: { ...(excerptId ? { excerpt: excerptId } : {}), ...(via.length ? { via: via.join(",") } : {}) } },
        })
      ).data!.included,
    retry: false,
  });
}
