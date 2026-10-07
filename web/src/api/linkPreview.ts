import { useContext } from "react";
import { QueryClient, QueryClientContext, useQuery } from "@tanstack/react-query";
import { LINK_PREVIEW_STALE_MS } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

/** What a web page says about itself, and the player it embeds in when its site is allowlisted. */
export type LinkPreview = components["schemas"]["LinkPreview"];

/** Asks the server for a page's card; it reads the page through its guard. */
export async function fetchLinkPreview(url: string): Promise<LinkPreview> {
  const { data, error } = await api.GET("/link-preview", { params: { query: { url } } });
  if (!data) throw error;
  return data.preview;
}

export function linkPreviewQuery(url: string) {
  return { queryKey: ["link-preview", url] as const, queryFn: () => fetchLinkPreview(url), staleTime: LINK_PREVIEW_STALE_MS, retry: false } as const;
}

// The editor draws cards wherever it is mounted, some places without the
// application's client around it; those keep their previews in one of their own.
let ownClient: QueryClient | null = null;

export function useLinkPreview(url: string) {
  const app = useContext(QueryClientContext);
  ownClient ??= app ? null : new QueryClient();
  return useQuery(linkPreviewQuery(url), app ?? ownClient!);
}
