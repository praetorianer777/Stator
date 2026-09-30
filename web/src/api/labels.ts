import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { LABEL_NAME_MAX_LENGTH, LABEL_PAGE_SIZE, LABEL_SUGGESTION_LIMIT } from "@/config";
import { api } from "./client";
import { pageQueryKey, type PageInSpace } from "./pages";
import type { components } from "./schema";
import { searchQueryKey } from "./search";

type Wire = components["schemas"];

/** A label the reader can see somewhere, with how many of their pages carry it. */
export type LabelSuggestion = Wire["LabelSuggestion"];
/** A page that carries a label, and where it lives. */
export type LabeledPage = Wire["LabeledPage"];

export const labelsQueryKey = ["labels"] as const;

const LABEL = /^[\p{L}\p{Nd}][\p{L}\p{Nd}_.-]*$/u;

/** Why a typed name cannot be a label, or undefined when it can. */
export type LabelProblem = "invalid" | "tooLong";

/**
 * A label as the API stores it: lower case with spaces as hyphens, as
 * label.Normalize reads it, so the client can tell a new label from one it has.
 */
export function normalizeLabel(raw: string): { name: string; problem?: LabelProblem } {
  const name = raw.trim().toLowerCase().split(/\s+/).filter(Boolean).join("-");
  if (!LABEL.test(name)) return { name, problem: "invalid" };
  if ([...name].length > LABEL_NAME_MAX_LENGTH) return { name, problem: "tooLong" };
  return { name };
}

/** Where a label's page lives in the address: the one route or the space's own. */
export function labelPath(name: string, spaceKey?: string) {
  return spaceKey ? ({ to: "/s/$spaceKey/labels/$name", params: { spaceKey, name } } as const) : ({ to: "/labels/$name", params: { name } } as const);
}

/** Labels on pages the reader may view that start with what was typed, the most used first. */
export function useLabelSuggestions(typed: string, enabled: boolean) {
  return useQuery({
    queryKey: [...labelsQueryKey, "suggest", typed],
    queryFn: async (): Promise<LabelSuggestion[]> =>
      (await api.GET("/labels", { params: { query: { q: typed || undefined, limit: LABEL_SUGGESTION_LIMIT } } })).data!.labels,
    enabled,
    placeholderData: keepPreviousData,
  });
}

/** The pages with a label, a page of the list at a time, in one space or all of them. */
export function useLabelPages(name: string, spaceKey: string | undefined, offset: number) {
  return useQuery({
    queryKey: [...labelsQueryKey, "pages", name, spaceKey ?? "", offset],
    queryFn: async () =>
      (
        await api.GET("/labels/{labelName}/pages", {
          params: { path: { labelName: name }, query: { space: spaceKey, limit: LABEL_PAGE_SIZE, offset: offset || undefined } },
        })
      ).data!,
    placeholderData: keepPreviousData,
  });
}

function useRelabel(pageId: string) {
  const queryClient = useQueryClient();
  return (next: (labels: string[]) => string[]) => {
    queryClient.setQueryData<PageInSpace>(pageQueryKey(pageId), (current) =>
      current ? { ...current, page: { ...current.page, labels: next(current.page.labels) } } : current,
    );
    void queryClient.invalidateQueries({ queryKey: labelsQueryKey });
    void queryClient.invalidateQueries({ queryKey: searchQueryKey });
  };
}

/** Puts a label on a page; the page's labels are what the API answers. */
export function useAddLabel(pageId: string) {
  const relabel = useRelabel(pageId);
  return useMutation({
    mutationFn: async (name: string): Promise<string[]> =>
      (await api.POST("/pages/{pageID}/labels", { params: { path: { pageID: pageId } }, body: { name } })).data!.labels,
    onSuccess: (labels) => relabel(() => labels),
  });
}

/** Takes a label off a page. */
export function useRemoveLabel(pageId: string) {
  const relabel = useRelabel(pageId);
  return useMutation({
    mutationFn: async (name: string): Promise<string> => {
      await api.DELETE("/pages/{pageID}/labels/{labelName}", { params: { path: { pageID: pageId, labelName: name } } });
      return name;
    },
    onSuccess: (name) => relabel((labels) => labels.filter((each) => each !== name)),
  });
}
