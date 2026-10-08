import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { API_BASE, WORD_IMPORT_POLL_MS } from "@/config";
import { api } from "./client";
import { postFiles, type ImportResult } from "./markdown";
import { pagesQueryKey } from "./pages";
import type { components } from "./schema";
import { treeQueryKey } from "./tree";

/** Where a page's Word document is made for the signed-in reader. */
export function pageWordHref(pageId: string): string {
  return `${API_BASE}/pages/${encodeURIComponent(pageId)}/docx`;
}

/** Where a page anybody may read is made into a Word document. */
export function publicWordHref(org: string, pageId: string): string {
  return `${API_BASE}/public/${encodeURIComponent(org)}/pages/${encodeURIComponent(pageId)}/docx`;
}

/** Where the page a public link opens is made into a Word document, for whoever holds the link. */
export function linkedWordHref(org: string, token: string): string {
  return `${API_BASE}/public/${encodeURIComponent(org)}/links/${encodeURIComponent(token)}/docx`;
}

/** An import of several Word documents, which the worker runs while the page follows it. */
export type WordImport = components["schemas"]["WordImport"];
/** What became of one file of such an import. */
export type WordImportFile = components["schemas"]["WordImportFile"];

/** Whether the import is still to finish, so the page keeps asking. */
export function wordImportOpen(job: WordImport | null | undefined): boolean {
  return job?.state === "queued" || job?.state === "running";
}

export const wordImportQueryKey = (id: string) => ["word-imports", id] as const;

/**
 * Imports Word documents under a page: one at once, answered with its page,
 * or several, queued for the worker and answered with the import to follow.
 */
export function useImportWord(pageId: string) {
  const queryClient = useQueryClient();
  const [progress, setProgress] = useState(0);
  const mutation = useMutation({
    mutationFn: async (files: File[]): Promise<{ result?: ImportResult; job?: WordImport }> => {
      setProgress(0);
      const path = `/pages/${encodeURIComponent(pageId)}`;
      if (files.length === 1 && !/\.zip$/i.test(files[0]!.name)) {
        return { result: await postFiles<ImportResult>(`${path}/import/docx`, files, 201, setProgress) };
      }
      return { job: (await postFiles<{ job: WordImport }>(`${path}/word-imports`, files, 202, setProgress)).job };
    },
    onSuccess: ({ result, job }) => {
      if (job) queryClient.setQueryData(wordImportQueryKey(job.id), job);
      if (result) return wordImportMade(queryClient);
    },
  });
  return { ...mutation, progress };
}

/** An import of several documents, asked again while the worker runs it. */
export function useWordImport(id: string | undefined, follow: boolean) {
  return useQuery({
    queryKey: wordImportQueryKey(id ?? ""),
    queryFn: async (): Promise<WordImport> => (await api.GET("/word-imports/{importID}", { params: { path: { importID: id! } } })).data!.job!,
    enabled: !!id,
    refetchInterval: follow ? (query) => (wordImportOpen(query.state.data) ? WORD_IMPORT_POLL_MS : false) : false,
  });
}

/** What follows pages made by an import: the tree and the lists of pages show them. */
export function wordImportMade(queryClient: ReturnType<typeof useQueryClient>) {
  return Promise.all([queryClient.invalidateQueries({ queryKey: treeQueryKey }), queryClient.invalidateQueries({ queryKey: pagesQueryKey })]);
}
