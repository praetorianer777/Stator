import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { API_BASE } from "@/config";
import { t } from "@/i18n";
import type { Progress } from "./attachments";
import { ApiError, type ApiErrorBody } from "./client";
import { pagesQueryKey } from "./pages";
import type { components } from "./schema";
import { treeQueryKey } from "./tree";

/** One page an import made, in reading order, with how deep below the target it sits. */
export type ImportedPage = components["schemas"]["Imported"];

export interface ImportResult {
  pages: ImportedPage[];
  warnings: string[];
}

/** What an export holds: one page as Markdown, the page and its files, or its subtree too. */
export type ExportScope = "markdown" | "page" | "subtree";

/** Where an export is fetched from, as a download the browser handles itself. */
export function markdownExportHref(pageId: string, scope: ExportScope): string {
  if (scope === "markdown") return `${API_BASE}/pages/${pageId}/markdown`;
  return `${API_BASE}/pages/${pageId}/export${scope === "subtree" ? "?subtree=true" : ""}`;
}

/** The path a file is sent under: where it sits in a chosen folder, else its name. */
export function uploadPath(file: File): string {
  return file.webkitRelativePath || file.name;
}

function refusal(xhr: XMLHttpRequest): ApiError {
  try {
    const body = (JSON.parse(xhr.responseText) as { error?: ApiErrorBody }).error;
    if (body) return new ApiError(xhr.status, body);
  } catch {
    // Not the API's envelope, so a gateway answered; the sentence below says so.
  }
  return new ApiError(xhr.status, { code: "unexpected_response", message: t.api.unexpected(xhr.status) });
}

/**
 * Sends files to be made pages under a page, each under its path. XMLHttpRequest
 * rather than fetch, because only it reports how much of a large folder has gone.
 */
export function importMarkdown(pageId: string, files: File[], onProgress?: Progress): Promise<ImportResult> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", `${API_BASE}/pages/${encodeURIComponent(pageId)}/import`);
    xhr.withCredentials = true;
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable && event.total > 0) onProgress?.(event.loaded / event.total);
    };
    xhr.onload = () => {
      if (xhr.status !== 201) {
        reject(refusal(xhr));
        return;
      }
      try {
        resolve(JSON.parse(xhr.responseText) as ImportResult);
      } catch {
        reject(new ApiError(xhr.status, { code: "unexpected_response", message: t.api.unexpected(xhr.status) }));
      }
    };
    xhr.onerror = () => reject(new ApiError(0, { code: "network", message: t.markdown.uploadBroke }));
    const form = new FormData();
    for (const file of files) form.append("file", file, uploadPath(file));
    xhr.send(form);
  });
}

/** Makes pages under a page from Markdown files, keeping how much of them has gone. */
export function useImportMarkdown(pageId: string) {
  const queryClient = useQueryClient();
  const [progress, setProgress] = useState(0);
  const mutation = useMutation({
    mutationFn: (files: File[]) => {
      setProgress(0);
      return importMarkdown(pageId, files, setProgress);
    },
    onSuccess: () => Promise.all([queryClient.invalidateQueries({ queryKey: treeQueryKey }), queryClient.invalidateQueries({ queryKey: pagesQueryKey })]),
  });
  return { ...mutation, progress };
}
