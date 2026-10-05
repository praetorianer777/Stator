import { formatNumber } from "@/lib/format";
import { useCallback, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { API_BASE, KILOBYTE } from "@/config";
import { t } from "@/i18n";
import { api, ApiError, type ApiErrorBody } from "./client";
import type { components } from "./schema";

/** What Stator knows about a file on a page; the bytes are behind attachmentUrl. */
export type Attachment = components["schemas"]["Attachment"];

export const attachmentsQueryKey = ["attachments"] as const;

export function pageAttachmentsQueryKey(pageId: string) {
  return [...attachmentsQueryKey, pageId] as const;
}

export function useAttachments(pageId: string | undefined) {
  return useQuery({
    queryKey: pageAttachmentsQueryKey(pageId ?? ""),
    queryFn: async (): Promise<Attachment[]> => (await api.GET("/pages/{pageID}/attachments", { params: { path: { pageID: pageId! } } })).data!.attachments,
    enabled: Boolean(pageId),
  });
}

/** Where the bytes are. The session cookie goes with the request, so a plain link or img works. */
export function attachmentUrl(id: string, inline = false): string {
  return `${API_BASE}/attachments/${encodeURIComponent(id)}${inline ? "?inline=1" : ""}`;
}

// The types the API shows in place with inline=1; everything else downloads.
const PREVIEWABLE = ["image/png", "image/jpeg", "image/gif", "image/webp", "application/pdf", "text/plain"];

/** Whether the browser can show this in a tab rather than download it. */
export function canPreview(contentType: string): boolean {
  return PREVIEWABLE.includes(contentType.split(";")[0]!.trim().toLowerCase());
}

/** Where a file's PDF preview is: a PDF itself, or an office document converted by the server. */
export function previewUrl(id: string): string {
  return `${API_BASE}/attachments/${encodeURIComponent(id)}/preview`;
}

/** Whether the server can show this file as a PDF in place. */
export function hasPreview(file: Pick<Attachment, "preview">): boolean {
  return file.preview === "pdf" || file.preview === "office";
}

/**
 * The PDF a preview shows. The first reader of an office document waits for
 * its conversion; a refusal is an ApiError whose message says what to do.
 */
export async function fetchPreview(id: string): Promise<Blob> {
  const { data } = await api.GET("/attachments/{attachmentID}/preview", { params: { path: { attachmentID: id } }, parseAs: "arrayBuffer" });
  // Typed here rather than by the answer, so the frame can only ever hold a PDF.
  return new Blob([data as ArrayBuffer], { type: "application/pdf" });
}

/** Whether a file is drawn as a picture in a page rather than as a download chip. */
export function isImage(contentType: string): boolean {
  return canPreview(contentType) && contentType.startsWith("image/");
}

const SIZE_UNITS = ["B", "KB", "MB", "GB"];

/** A file size the way a person reads one: 12 KB, 3.4 MB. */
export function formatSize(bytes: number): string {
  let value = bytes;
  let unit = 0;
  while (value >= KILOBYTE && unit < SIZE_UNITS.length - 1) {
    value /= KILOBYTE;
    unit += 1;
  }
  const shown = unit === 0 || value >= 10 ? formatNumber(Math.round(value)) : formatNumber(value, { minimumFractionDigits: 1, maximumFractionDigits: 1 });
  return `${shown} ${SIZE_UNITS[unit]}`;
}

/** How far an upload has come, from 0 to 1. */
export type Progress = (fraction: number) => void;

function envelope(xhr: XMLHttpRequest): ApiErrorBody | undefined {
  try {
    return (JSON.parse(xhr.responseText) as { error?: ApiErrorBody }).error;
  } catch {
    return undefined;
  }
}

/**
 * Sends one file to a page. XMLHttpRequest rather than fetch, because only it
 * reports how much of the body has gone, which a large file needs to show.
 */
export function uploadAttachment(pageId: string, file: File, onProgress?: Progress): Promise<Attachment> {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", `${API_BASE}/pages/${encodeURIComponent(pageId)}/attachments`);
    xhr.withCredentials = true;
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable && event.total > 0) onProgress?.(event.loaded / event.total);
    };
    xhr.onload = () => {
      if (xhr.status === 201) {
        try {
          resolve((JSON.parse(xhr.responseText) as { attachment: Attachment }).attachment);
        } catch {
          reject(new ApiError(xhr.status, { code: "unexpected_response", message: t.api.unexpected(xhr.status) }));
        }
        return;
      }
      const body = envelope(xhr);
      if (xhr.status === 413) {
        reject(new ApiError(413, { code: "too_large", message: body?.message ?? "" }));
        return;
      }
      reject(new ApiError(xhr.status, body ?? { code: "unexpected_response", message: t.api.unexpected(xhr.status) }));
    };
    xhr.onerror = () => reject(new ApiError(0, { code: "network", message: t.attachments.uploadBroke(file.name) }));
    const form = new FormData();
    form.append("file", file, file.name);
    xhr.send(form);
  });
}

/** The sentence a refused upload shows, which always says what to do next. */
export function uploadErrorMessage(file: File, error: unknown): string {
  if (error instanceof ApiError && error.code === "network") return error.message;
  if (error instanceof ApiError && error.code === "too_large") return t.attachments.tooLarge(file.name, formatSize(file.size), error.message);
  if (error instanceof ApiError && error.fields.file) return t.attachments.refused(file.name, error.fields.file);
  if (error instanceof Error && error.message) return t.attachments.refused(file.name, error.message);
  return t.attachments.uploadBroke(file.name);
}

/** One file on its way up, as the panel lists it. */
export interface PendingUpload {
  key: number;
  fileName: string;
  progress: number;
}

let nextUploadKey = 1;

/**
 * Uploads files to a page one request each, side by side, keeping how far each
 * has come and the sentence for each one refused.
 */
export function useUploadAttachments(pageId: string) {
  const queryClient = useQueryClient();
  const [pending, setPending] = useState<PendingUpload[]>([]);
  const [errors, setErrors] = useState<string[]>([]);
  const active = useRef(0);

  const upload = useCallback(
    async (file: File, onProgress?: Progress): Promise<Attachment | null> => {
      const key = nextUploadKey++;
      // Files sent together are one attempt; what an earlier one refused is old news.
      if (active.current === 0) setErrors([]);
      active.current += 1;
      setPending((list) => [...list, { key, fileName: file.name, progress: 0 }]);
      try {
        const made = await uploadAttachment(pageId, file, (progress) => {
          onProgress?.(progress);
          setPending((list) => list.map((each) => (each.key === key ? { ...each, progress } : each)));
        });
        queryClient.setQueryData<Attachment[]>(pageAttachmentsQueryKey(pageId), (list) => (list ? [made, ...list.filter((a) => a.id !== made.id)] : list));
        void queryClient.invalidateQueries({ queryKey: pageAttachmentsQueryKey(pageId) });
        return made;
      } catch (error) {
        setErrors((list) => [...list, uploadErrorMessage(file, error)]);
        return null;
      } finally {
        active.current -= 1;
        setPending((list) => list.filter((each) => each.key !== key));
      }
    },
    [pageId, queryClient],
  );

  const clearErrors = useCallback(() => setErrors([]), []);
  return { upload, pending, errors, clearErrors };
}

export function useDeleteAttachment(pageId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => {
      await api.DELETE("/attachments/{attachmentID}", { params: { path: { attachmentID: id } } });
      return id;
    },
    onSuccess: (id) => {
      queryClient.setQueryData<Attachment[]>(pageAttachmentsQueryKey(pageId), (list) => list?.filter((a) => a.id !== id));
      return queryClient.invalidateQueries({ queryKey: pageAttachmentsQueryKey(pageId) });
    },
  });
}
