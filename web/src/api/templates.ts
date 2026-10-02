import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { TEMPLATE_DATE_TOKEN } from "@/config";
import type { Doc } from "@/features/editor/schema";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** A document a new page can start from, its body as the editor stores it. */
export type Template = Omit<Wire["Template"], "body"> & { body: Doc };
/** One blank of a template: what the form asks for and where its answer goes. */
export type TemplateVariable = Wire["Variable"];
export type VariableKind = TemplateVariable["kind"];
/** What one of the organization's templates is made of. */
export type TemplateInput = Omit<Wire["TemplateInput"], "body"> & { body: Doc };

export const templatesQueryKey = ["templates"] as const;

/** The templates a page in the space can start from: the space's, the organization's, then the built-ins. */
export function useTemplates(spaceKey?: string) {
  return useQuery({
    queryKey: [...templatesQueryKey, "list", spaceKey ?? ""],
    queryFn: async (): Promise<Template[]> =>
      (await api.GET("/templates", { params: { query: spaceKey ? { space: spaceKey } : {} } })).data!.templates as Template[],
  });
}

export function useTemplate(key: string | undefined) {
  return useQuery({
    queryKey: [...templatesQueryKey, "one", key],
    queryFn: async (): Promise<Template> => (await api.GET("/templates/{templateKey}", { params: { path: { templateKey: key! } } })).data!.template as Template,
    enabled: Boolean(key),
  });
}

function useTemplateMutation<TInput, TOut>(fn: (input: TInput) => Promise<TOut>) {
  const queryClient = useQueryClient();
  return useMutation({ mutationFn: fn, onSettled: () => queryClient.invalidateQueries({ queryKey: templatesQueryKey }) });
}

export function useCreateTemplate() {
  return useTemplateMutation(
    async (body: TemplateInput & { spaceKey?: string }): Promise<Template> => (await api.POST("/templates", { body })).data!.template as Template,
  );
}

export function useUpdateTemplate(key: string) {
  return useTemplateMutation(
    async (body: TemplateInput): Promise<Template> =>
      (await api.PUT("/templates/{templateKey}", { params: { path: { templateKey: key } }, body })).data!.template as Template,
  );
}

export function useDeleteTemplate() {
  return useTemplateMutation(async (key: string) => {
    await api.DELETE("/templates/{templateKey}", { params: { path: { templateKey: key } } });
  });
}

/** A template's title for a page made now: the date token becomes the local day, as YYYY-MM-DD. */
export function templateTitle(pattern: string, now: Date = new Date()): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  const day = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
  return pattern.split(TEMPLATE_DATE_TOKEN).join(day);
}
