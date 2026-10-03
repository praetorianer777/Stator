import { useQuery } from "@tanstack/react-query";
import { api } from "./client";
import { labelsQueryKey } from "./labels";
import type { components } from "./schema";

export type UpdatedPage = components["schemas"]["UpdatedPage"];

/** A content by label block's pages, as the reader may read them; kept with the labels, so a label's change asks again. */
export function useLabelledPages(settings: { labels: string[]; match: "all" | "any"; space: string | null; sort: "updated" | "title"; limit: number }) {
  return useQuery({
    queryKey: [...labelsQueryKey, "listed", settings],
    enabled: settings.labels.length > 0,
    queryFn: async () =>
      (
        await api.GET("/labelled-pages", {
          params: { query: { label: settings.labels, match: settings.match, space: settings.space ?? undefined, sort: settings.sort, limit: settings.limit } },
        })
      ).data!.pages,
  });
}

/** A recently updated block's pages, as the reader may read them. */
export function useUpdatedPages(settings: { space: string | null; limit: number }) {
  return useQuery({
    queryKey: ["pages", "updated", settings],
    queryFn: async () => (await api.GET("/updated-pages", { params: { query: { space: settings.space ?? undefined, limit: settings.limit } } })).data!.pages,
  });
}
