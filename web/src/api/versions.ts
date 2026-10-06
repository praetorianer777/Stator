import { keepPreviousData, useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import type { Doc, DocNode } from "@/features/editor/schema";
import { HISTORY_PAGE_SIZE } from "@/config";
import { api } from "./client";
import { contributorsQueryKey } from "./contributors";
import { pageQueryKey, type Page, type PageInSpace } from "./pages";
import type { components } from "./schema";
import { spaceQueryKey } from "./spaces";
import { treeQueryKey } from "./tree";

type Wire = components["schemas"];

/** The caller's own unpublished edit of a page, seen by nobody else. */
export type Draft = Omit<Wire["Draft"], "body"> & { body: Doc };
/** A published version as the history lists it, without its body. */
export type VersionEntry = Wire["VersionEntry"];
/** A published version with its body, to read as it was. */
export type Version = Omit<Wire["Version"], "body"> & { body: Doc };
/** One side of a comparison: a version, the caller's draft, or the empty page as number 0. */
export type CompareSide = Wire["CompareSide"];
export type DiffChange = Wire["DiffBlock"]["change"];
export type DiffBlock = { change: DiffChange; node: DocNode };
export type Comparison = Omit<Wire["Comparison"], "blocks"> & { blocks: DiffBlock[] };
export type PublishOptions = Wire["PublishInput"];

/** What a side of a comparison names: a version number, or the caller's draft. */
export type CompareRef = number | "draft";

export function draftQueryKey(pageId: string) {
  return [...pageQueryKey(pageId), "draft"] as const;
}

export function versionsQueryKey(pageId: string) {
  return [...pageQueryKey(pageId), "versions"] as const;
}

export function draftQuery(pageId: string) {
  return {
    queryKey: draftQueryKey(pageId),
    queryFn: async (): Promise<Draft | null> =>
      ((await api.GET("/pages/{pageID}/draft", { params: { path: { pageID: pageId } } })).data!.draft as Draft | null) ?? null,
  };
}

export function useDraft(pageId: string) {
  return useQuery(draftQuery(pageId));
}

// The page records whether the caller has a draft, which the reader shows,
// so the page's cached copy is kept in step with every draft change.
function setPageDraft(queryClient: QueryClient, pageId: string, draft: Draft | null) {
  queryClient.setQueryData(draftQueryKey(pageId), draft);
  queryClient.setQueryData<PageInSpace>(pageQueryKey(pageId), (current) =>
    current ? { ...current, page: { ...current.page, draft: draft && { baseVersion: draft.baseVersion, updatedAt: draft.updatedAt } } } : current,
  );
}

export interface DraftInput {
  title: string;
  body: Doc;
  /** The published version editing began from; publishing over a newer one is refused. */
  baseVersion: number;
}

export function useSaveDraft(pageId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: DraftInput): Promise<Draft> =>
      (await api.PUT("/pages/{pageID}/draft", { params: { path: { pageID: pageId } }, body: input })).data!.draft as Draft,
    onSuccess: (draft) => setPageDraft(queryClient, pageId, draft),
  });
}

export function useDiscardDraft(pageId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async () => {
      await api.DELETE("/pages/{pageID}/draft", { params: { path: { pageID: pageId } } });
    },
    onSuccess: () => setPageDraft(queryClient, pageId, null),
  });
}

// A new version changes the page, its history, every comparison, who
// contributed to it and to the pages above it, and, for a first publish or a
// title change, the tree and the space's home page.
function published(queryClient: QueryClient, page: Page) {
  queryClient.setQueryData(draftQueryKey(page.id), null);
  queryClient.setQueryData<PageInSpace>(pageQueryKey(page.id), (current) => (current ? { ...current, page } : current));
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: versionsQueryKey(page.id) }),
    queryClient.invalidateQueries({ queryKey: [...pageQueryKey(page.id), "compare"] }),
    queryClient.invalidateQueries({ queryKey: treeQueryKey }),
    queryClient.invalidateQueries({ queryKey: spaceQueryKey(page.spaceKey) }),
    queryClient.invalidateQueries({ queryKey: contributorsQueryKey }),
  ]);
}

export function usePublish(pageId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (options: PublishOptions): Promise<Page> =>
      (await api.POST("/pages/{pageID}/publish", { params: { path: { pageID: pageId } }, body: options })).data!.page as Page,
    onSuccess: (page) => published(queryClient, page),
  });
}

/** How a page is edited: published from drafts, or saved live as it is typed. */
export type PageMode = Wire["Page"]["mode"];
export type LiveSaved = Wire["PageLiveSaved"];
export type ModeChange = Wire["PageModeChange"];

export interface LiveInput {
  title: string;
  body: Doc;
  /** The shared draft saved from; absent when editing alone. */
  room?: string;
}

/** Saves a live page as the editor holds it; readers see it at once. */
export function useSaveLive(pageId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: LiveInput): Promise<LiveSaved> =>
      (await api.PUT("/pages/{pageID}/live", { params: { path: { pageID: pageId } }, body: input })).data!,
    onSuccess: (saved, input) => {
      let retitled = false;
      queryClient.setQueryData<PageInSpace>(pageQueryKey(pageId), (current) => {
        if (!current) return current;
        retitled = current.page.title !== saved.version.title || current.page.unpublished;
        return {
          ...current,
          page: { ...current.page, title: saved.version.title, body: input.body, version: saved.version.number, unpublished: false },
        };
      });
      // Every save changes the history and who contributed; the tree and the
      // space only hear of a new title or a first version.
      const asked = [
        queryClient.invalidateQueries({ queryKey: versionsQueryKey(pageId) }),
        queryClient.invalidateQueries({ queryKey: [...pageQueryKey(pageId), "compare"] }),
        queryClient.invalidateQueries({ queryKey: contributorsQueryKey }),
      ];
      if (retitled) asked.push(queryClient.invalidateQueries({ queryKey: treeQueryKey }));
      return Promise.all(asked);
    },
  });
}

/** Chooses between drafts and live; making a page live may first ask to throw drafts away. */
export function useSetPageMode(pageId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: { mode: PageMode; discardDrafts?: boolean }): Promise<ModeChange> =>
      (await api.PUT("/pages/{pageID}/mode", { params: { path: { pageID: pageId } }, body: input })).data!,
    onSuccess: () => {
      queryClient.setQueryData(draftQueryKey(pageId), null);
      return queryClient.invalidateQueries({ queryKey: pageQueryKey(pageId) });
    },
  });
}

export function useVersions(pageId: string, offset: number) {
  return useQuery({
    queryKey: [...versionsQueryKey(pageId), "list", offset],
    queryFn: async () =>
      (await api.GET("/pages/{pageID}/versions", { params: { path: { pageID: pageId }, query: { limit: HISTORY_PAGE_SIZE, offset } } })).data!,
    placeholderData: keepPreviousData,
  });
}

export function useVersion(pageId: string, number: number | undefined) {
  return useQuery({
    queryKey: [...versionsQueryKey(pageId), number],
    queryFn: async (): Promise<Version> =>
      (await api.GET("/pages/{pageID}/versions/{versionNumber}", { params: { path: { pageID: pageId, versionNumber: number! } } })).data!.version as Version,
    enabled: number !== undefined,
  });
}

export function useComparison(pageId: string, from: CompareRef | undefined, to: CompareRef | undefined, enabled = true) {
  const query: { from?: string; to?: string } = {};
  if (from !== undefined) query.from = String(from);
  if (to !== undefined) query.to = String(to);
  return useQuery({
    queryKey: [...pageQueryKey(pageId), "compare", query.from ?? "", query.to ?? ""],
    queryFn: async (): Promise<Comparison> =>
      (await api.GET("/pages/{pageID}/compare", { params: { path: { pageID: pageId }, query } })).data!.comparison as Comparison,
    enabled,
  });
}

export function useRestoreVersion(pageId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ number, baseVersion, comment }: { number: number; baseVersion: number; comment?: string }): Promise<Page> =>
      (
        await api.POST("/pages/{pageID}/versions/{versionNumber}/restore", {
          params: { path: { pageID: pageId, versionNumber: number } },
          // A restore is announced as a publish is by default, since it changes the page as much.
          body: comment ? { baseVersion, comment, notifyWatchers: true } : { baseVersion, notifyWatchers: true },
        })
      ).data!.page as Page,
    onSuccess: (page) => published(queryClient, page),
  });
}
