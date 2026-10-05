import { useQuery } from "@tanstack/react-query";
import { TEMPLATE_DATE_TOKEN } from "@/config";
import type { Doc } from "@/features/editor/schema";
import { api } from "./client";
import type { components } from "./schema";

/** A document a new page can start from, its body as the editor stores it. */
export type Template = Omit<components["schemas"]["Template"], "body"> & { body: Doc };

export const templatesQueryKey = ["templates"] as const;

export function useTemplates() {
  return useQuery({
    queryKey: templatesQueryKey,
    queryFn: async (): Promise<Template[]> => (await api.GET("/templates")).data!.templates as Template[],
    // Built-ins change only with a release.
    staleTime: Number.POSITIVE_INFINITY,
  });
}

/** A structure a new space can start from: its home page, the pages below it with their labels, and what everyone may do. */
export type SpaceTemplate = components["schemas"]["SpaceTemplate"];
export type SpaceTemplatePage = components["schemas"]["SpacePage"];

export const spaceTemplatesQueryKey = ["space-templates"] as const;

export function useSpaceTemplates() {
  return useQuery({
    queryKey: spaceTemplatesQueryKey,
    queryFn: async (): Promise<SpaceTemplate[]> => (await api.GET("/space-templates")).data!.templates,
    // Built-ins change only with a release.
    staleTime: Number.POSITIVE_INFINITY,
  });
}

/** A template's title for a page made now: the date token becomes the local day, as YYYY-MM-DD. */
export function templateTitle(pattern: string, now: Date = new Date()): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  const day = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
  return pattern.split(TEMPLATE_DATE_TOKEN).join(day);
}
