import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { API_BASE, PUBLIC_SEARCH_LIMIT, PUBLIC_STALE_MS } from "@/config";
import { ApiError, api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** An organization as somebody who is not signed in finds it. */
export type PublicSite = Wire["PublicSite"];
export type PublicSpace = Wire["PublicSpace"];
export type PublicTreePage = Wire["PublicTreePage"];
/** A published page with nobody named in it. */
export type PublicPage = Wire["PublicPage"];
export type PublicHit = Wire["AnonymousHit"];
/** Whether the organization lets anybody read the spaces that allow it, and whether search engines are asked in. */
export type AnonymousAccessSettings = Wire["AnonymousAccessSettings"];
export type SpaceAnonymousAccess = Wire["SpaceAnonymousAccess"];

/** Whether an error says the thing asked for is not open to anybody, which is where signing in helps. */
export function isNotPublic(error: unknown): boolean {
  return error instanceof ApiError && error.status === 404;
}

export function publicSiteQuery(org: string) {
  return {
    queryKey: ["public", org, "site"] as const,
    queryFn: async () => (await api.GET("/public/{orgSlug}", { params: { path: { orgSlug: org } } })).data!,
    staleTime: PUBLIC_STALE_MS,
  };
}

export function usePublicSite(org: string) {
  return useQuery(publicSiteQuery(org));
}

export function publicSpaceQuery(org: string, spaceKey: string) {
  return {
    queryKey: ["public", org, "space", spaceKey.toUpperCase()] as const,
    queryFn: async () => (await api.GET("/public/{orgSlug}/spaces/{spaceKey}", { params: { path: { orgSlug: org, spaceKey } } })).data!,
    staleTime: PUBLIC_STALE_MS,
  };
}

/** A space's tree; an empty key, while the page that names it loads, asks nothing. */
export function usePublicSpace(org: string, spaceKey: string) {
  return useQuery({ ...publicSpaceQuery(org, spaceKey), enabled: spaceKey !== "" });
}

export function publicPageQuery(org: string, pageId: string) {
  return {
    queryKey: ["public", org, "page", pageId] as const,
    queryFn: async (): Promise<PublicPage> =>
      (await api.GET("/public/{orgSlug}/pages/{pageID}", { params: { path: { orgSlug: org, pageID: pageId } } })).data!.page,
    staleTime: PUBLIC_STALE_MS,
  };
}

export function usePublicPage(org: string, pageId: string) {
  return useQuery(publicPageQuery(org, pageId));
}

export function usePublicSearch(org: string, q: string, page: number) {
  const offset = (page - 1) * PUBLIC_SEARCH_LIMIT;
  return useQuery({
    queryKey: ["public", org, "search", q, page] as const,
    queryFn: async () =>
      (await api.GET("/public/{orgSlug}/search", { params: { path: { orgSlug: org }, query: { q, limit: PUBLIC_SEARCH_LIMIT, offset } } })).data!,
    enabled: q.trim() !== "",
    placeholderData: keepPreviousData,
    staleTime: PUBLIC_STALE_MS,
  });
}

/** Where a file of a public page downloads from, or shows in place. */
export function publicAttachmentUrl(org: string, id: string, inline = false): string {
  return `${API_BASE}/public/${encodeURIComponent(org)}/attachments/${encodeURIComponent(id)}${inline ? "?inline=1" : ""}`;
}

export const anonymousAccessQueryKey = ["org", "anonymous-access"] as const;

export function useAnonymousAccess() {
  return useQuery({
    queryKey: anonymousAccessQueryKey,
    queryFn: async (): Promise<AnonymousAccessSettings> => (await api.GET("/org/anonymous-access")).data!.anonymousAccess,
  });
}

export function useSetAnonymousAccess() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: AnonymousAccessSettings): Promise<AnonymousAccessSettings> =>
      (await api.PUT("/org/anonymous-access", { body })).data!.anonymousAccess,
    onSuccess: (saved) => {
      queryClient.setQueryData(anonymousAccessQueryKey, saved);
      return queryClient.invalidateQueries({ queryKey: ["space-anonymous-access"] });
    },
  });
}

export function spaceAnonymousAccessQueryKey(spaceKey: string) {
  return ["space-anonymous-access", spaceKey.toUpperCase()] as const;
}

export function useSpaceAnonymousAccess(spaceKey: string, enabled = true) {
  return useQuery({
    queryKey: spaceAnonymousAccessQueryKey(spaceKey),
    queryFn: async (): Promise<SpaceAnonymousAccess> =>
      (await api.GET("/spaces/{spaceKey}/anonymous-access", { params: { path: { spaceKey } } })).data!.anonymousAccess,
    enabled,
  });
}

export function useSetSpaceAnonymousAccess(spaceKey: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (view: boolean): Promise<SpaceAnonymousAccess> =>
      (await api.PUT("/spaces/{spaceKey}/anonymous-access", { params: { path: { spaceKey } }, body: { view } })).data!.anonymousAccess,
    onSuccess: (saved) => queryClient.setQueryData(spaceAnonymousAccessQueryKey(spaceKey), saved),
  });
}
