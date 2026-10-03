import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { Doc } from "@/features/editor/schema";
import { api } from "./client";
import type { components } from "./schema";
import { spaceQueryKey, type Space } from "./spaces";

type Wire = components["schemas"];

/** A page with its document; the wire leaves the body untyped, the editor's schema types it. */
export type Page = Omit<Wire["Page"], "body"> & { body: Doc };

/** A page and the space it is in, as the reader shows them together. */
export interface PageInSpace {
  page: Page;
  space: Space;
}

export const pagesQueryKey = ["pages"] as const;

export function pageQueryKey(id: string) {
  return [...pagesQueryKey, id] as const;
}

export function pageQuery(id: string) {
  return {
    queryKey: pageQueryKey(id),
    queryFn: async (): Promise<PageInSpace> => {
      const data = (await api.GET("/pages/{pageID}", { params: { path: { pageID: id } } })).data!;
      return { page: data.page as Page, space: data.space };
    },
  };
}

export function usePage(id: string | undefined) {
  return useQuery({ ...pageQuery(id ?? ""), enabled: Boolean(id) });
}

export interface PageChanges {
  title?: string;
  body?: Doc;
  /** The version the change was made from; the API refuses it over a newer one. */
  version: number;
}

/** How a page looks apart from its words: an emoji, a width and a cover. */
export type Appearance = Wire["Appearance"];

/** Replaces how a page looks; the tree shows the emoji, so its levels are asked again. */
export function useSetAppearance(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (appearance: Wire["AppearanceInput"]): Promise<Appearance> =>
      (await api.PUT("/pages/{pageID}/appearance", { params: { path: { pageID: id } }, body: appearance })).data!.appearance,
    onSuccess: (saved) => {
      queryClient.setQueryData<PageInSpace>(pageQueryKey(id), (current) => (current ? { ...current, page: { ...current.page, appearance: saved } } : current));
      return queryClient.invalidateQueries({ queryKey: ["tree"] });
    },
  });
}

export function useUpdatePage(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (changes: PageChanges): Promise<Page> =>
      (await api.PATCH("/pages/{pageID}", { params: { path: { pageID: id } }, body: changes })).data!.page as Page,
    onSuccess: (saved) => {
      queryClient.setQueryData<PageInSpace>(pageQueryKey(id), (current) => (current ? { ...current, page: saved } : current));
      return queryClient.invalidateQueries({ queryKey: spaceQueryKey(saved.spaceKey) });
    },
  });
}
