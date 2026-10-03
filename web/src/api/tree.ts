import { useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import type { Doc } from "@/features/editor/schema";
import { api } from "./client";
import { pageQueryKey, pagesQueryKey, type Page } from "./pages";
import type { components } from "./schema";

type Wire = components["schemas"];

/** A page as the sidebar's tree shows it, loaded a level at a time. */
export type TreeNode = Wire["TreeNode"];
/** A page of a whole space in reading order, with its depth. */
export type OutlineEntry = Wire["OutlineEntry"];

/** Where a page goes: under a parent, before or after one of its children, or last. */
export interface Placement {
  parentId: string;
  beforeId?: string;
  afterId?: string;
}

export const treeQueryKey = ["tree"] as const;

/** The pages under a parent of a space; no parent means under its home page. */
export function useChildren(spaceKey: string, parentId?: string) {
  return useQuery({
    queryKey: [...treeQueryKey, spaceKey, "children", parentId ?? "home"],
    queryFn: async (): Promise<TreeNode[]> =>
      (
        await api.GET("/spaces/{spaceKey}/pages", {
          params: { path: { spaceKey }, query: parentId ? { parent: parentId } : {} },
        })
      ).data!.pages,
  });
}

/** A page under another, as a child pages block lists it. */
export type BelowPage = Wire["BelowPage"];

/** The pages under a page the reader may view, each after its parent, as a child pages block asks for them. */
export function usePagesBelow(pageId: string | undefined, query: { scope: "children" | "subtree"; sort: "tree" | "title" | "updated"; depth?: number }) {
  return useQuery({
    queryKey: [...treeQueryKey, "below", pageId, query.scope, query.sort, query.depth ?? "all"],
    queryFn: async () => (await api.GET("/pages/{pageID}/below", { params: { path: { pageID: pageId! }, query } })).data!,
    enabled: Boolean(pageId),
  });
}

export function useOutline(spaceKey: string | undefined) {
  return useQuery({
    queryKey: [...treeQueryKey, spaceKey, "outline"],
    queryFn: async (): Promise<OutlineEntry[]> => (await api.GET("/spaces/{spaceKey}/outline", { params: { path: { spaceKey: spaceKey! } } })).data!.pages,
    enabled: Boolean(spaceKey),
  });
}

// A change anywhere in a tree can reorder any level of it and any page's
// breadcrumbs, so everything read from trees and pages is read again.
function refreshTrees(queryClient: QueryClient) {
  return Promise.all([queryClient.invalidateQueries({ queryKey: treeQueryKey }), queryClient.invalidateQueries({ queryKey: pagesQueryKey })]);
}

export function useCreatePage() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: Placement & { title: string; body?: Doc; kind?: Page["kind"] }): Promise<Page> =>
      (await api.POST("/pages", { body: input })).data!.page as Page,
    onSuccess: () => refreshTrees(queryClient),
  });
}

/** Renames a folder, which changes its title alone, and the trees that show it. */
export function useRenameFolder(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ title, version }: { title: string; version: number }): Promise<Page> =>
      (await api.PATCH("/pages/{pageID}", { params: { path: { pageID: id } }, body: { title, version } })).data!.page as Page,
    onSuccess: () => Promise.all([queryClient.invalidateQueries({ queryKey: pageQueryKey(id) }), refreshTrees(queryClient)]),
  });
}

export function useMovePage() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, ...body }: Placement & { id: string; withChildren?: boolean }): Promise<Page> =>
      (await api.POST("/pages/{pageID}/move", { params: { path: { pageID: id } }, body })).data!.page as Page,
    onSettled: () => refreshTrees(queryClient),
  });
}

export function useCopyPage() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, ...body }: Placement & { id: string; withChildren: boolean; title?: string }): Promise<Page> =>
      (await api.POST("/pages/{pageID}/copy", { params: { path: { pageID: id } }, body })).data!.page as Page,
    onSuccess: () => refreshTrees(queryClient),
  });
}
