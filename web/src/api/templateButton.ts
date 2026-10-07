import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import { pagesQueryKey, type Page } from "./pages";
import type { components } from "./schema";
import { treeQueryKey } from "./tree";

type Wire = components["schemas"];

/** What a template button makes and where, and whether the reader may put a page there. */
export type TemplateButtonTarget = Wire["TemplateButton"];

/** Where a button's page goes: under a page, else at the top of a space. */
export interface TemplateButtonQuery {
  template: string;
  spaceKey: string | null;
  parentId: string | null;
}

export const templateButtonQueryKey = ["template-button"] as const;

/** A button's template and target as the reader sees them; nothing is asked while it names no template or place. */
export function useTemplateButton(q: TemplateButtonQuery) {
  return useQuery({
    queryKey: [...templateButtonQueryKey, q],
    queryFn: async (): Promise<TemplateButtonTarget> =>
      (
        await api.GET("/template-button", {
          params: { query: { template: q.template, spaceKey: q.spaceKey ?? undefined, parentId: q.parentId ?? undefined } },
        })
      ).data!,
    enabled: q.template !== "" && Boolean(q.spaceKey || q.parentId),
    retry: false,
  });
}

/** Makes a page from a template where a button says; the trees that show it are read again. */
export function useCreateFromTemplate() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ template, ...body }: TemplateButtonQuery & { title: string; values?: Record<string, string> }): Promise<Page> =>
      (
        await api.POST("/templates/{templateKey}/pages", {
          params: { path: { templateKey: template } },
          body: { parentId: body.parentId ?? undefined, spaceKey: body.spaceKey ?? undefined, title: body.title, values: body.values },
        })
      ).data!.page as Page,
    onSuccess: () => Promise.all([queryClient.invalidateQueries({ queryKey: treeQueryKey }), queryClient.invalidateQueries({ queryKey: pagesQueryKey })]),
  });
}
