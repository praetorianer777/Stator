import { useCallback } from "react";
import { MENTION_MAX_SUGGESTIONS } from "@/config";
import type { MentionSource } from "@/features/editor/schema";
import { api } from "./client";

/**
 * Who an at sign may name on a page: anybody in the organization, each
 * saying whether they may view the page, since only those are told.
 */
export function useMentionSource(pageId: string): MentionSource {
  return useCallback<MentionSource>(
    async (query, signal) => {
      const { data } = await api.GET("/pages/{pageID}/mentionable", {
        params: { path: { pageID: pageId }, query: { q: query, limit: MENTION_MAX_SUGGESTIONS } },
        signal,
      });
      return data?.people ?? [];
    },
    [pageId],
  );
}
