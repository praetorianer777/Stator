import { useQuery } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** A named part of a page's published body, as a picker lists it. */
export type PageExcerpt = components["schemas"]["Excerpt"];

export function useExcerpts(pageId: string | undefined) {
  return useQuery({
    queryKey: ["pages", pageId, "excerpts"],
    queryFn: async (): Promise<PageExcerpt[]> => (await api.GET("/pages/{pageID}/excerpts", { params: { path: { pageID: pageId! } } })).data!.excerpts,
    enabled: Boolean(pageId),
  });
}
