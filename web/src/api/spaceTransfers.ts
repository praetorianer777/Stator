import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { API_BASE, SPACE_TRANSFER_POLL_MS } from "@/config";
import { t } from "@/i18n";
import type { Progress } from "./attachments";
import { api, ApiError, type ApiErrorBody } from "./client";
import type { components } from "./schema";
import { spacesQueryKey } from "./spaces";

type Wire = components["schemas"];

/** One export of a space, which the worker makes while the settings follow it. */
export type SpaceExport = Wire["SpaceExport"];
/** One import of an archive into a new space, followed to its report. */
export type SpaceImport = Wire["SpaceImport"];
/** What an import could not bring across as it was. */
export type SpaceImportReport = Wire["SpaceImportReport"];
/** What an import made its space of: an archive of Stator, or another wiki's HTML or XML export. */
export type ImportSource = SpaceImport["source"];
/** An archive to import again, or pages to read offline. */
export type ExportFormat = SpaceExport["format"];

/** Whether a job is still to finish, so the page keeps asking. */
export function transferOpen(job: { state: string } | null | undefined): boolean {
  return job?.state === "queued" || job?.state === "running";
}

export function spaceExportsQueryKey(key: string) {
  return ["space-exports", key.toUpperCase()] as const;
}

/** The space's latest exports the reader may see, asked again while one is under way. */
export function useSpaceExports(key: string, enabled: boolean) {
  return useQuery({
    queryKey: spaceExportsQueryKey(key),
    queryFn: async (): Promise<SpaceExport[]> => (await api.GET("/spaces/{spaceKey}/exports", { params: { path: { spaceKey: key } } })).data!.exports,
    enabled,
    refetchInterval: (query) => (query.state.data?.some(transferOpen) ? SPACE_TRANSFER_POLL_MS : false),
  });
}

/** Asks the worker for an export of the space in a format. */
export function useCreateSpaceExport(key: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (format: ExportFormat): Promise<SpaceExport> =>
      (await api.POST("/spaces/{spaceKey}/exports", { params: { path: { spaceKey: key } }, body: { format } })).data!.export,
    onSuccess: (job) => {
      queryClient.setQueryData<SpaceExport[]>(spaceExportsQueryKey(key), (jobs) => [job, ...(jobs ?? []).filter((j) => j.id !== job.id)]);
    },
  });
}

/** Where an export's file is downloaded from, which the browser saves itself. */
export function spaceExportHref(id: string): string {
  return `${API_BASE}/space-exports/${encodeURIComponent(id)}/file`;
}

/** What an import asks for besides its archive; empty takes the archive's own. */
export interface ImportRequest {
  file: File;
  key: string;
  name: string;
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
 * Uploads an archive and asks the worker to make a space of it. XMLHttpRequest
 * rather than fetch, because only it reports how much of a large file has gone.
 */
export function importSpace(request: ImportRequest, onProgress?: Progress): Promise<SpaceImport> {
  return new Promise((resolve, reject) => {
    const query = new URLSearchParams();
    if (request.key.trim()) query.set("key", request.key.trim());
    if (request.name.trim()) query.set("name", request.name.trim());
    const xhr = new XMLHttpRequest();
    xhr.open("POST", `${API_BASE}/space-imports${query.size ? `?${query}` : ""}`);
    xhr.withCredentials = true;
    xhr.upload.onprogress = (event) => {
      if (event.lengthComputable && event.total > 0) onProgress?.(event.loaded / event.total);
    };
    xhr.onload = () => {
      if (xhr.status !== 202) {
        reject(refusal(xhr));
        return;
      }
      try {
        resolve((JSON.parse(xhr.responseText) as { import: SpaceImport }).import);
      } catch {
        reject(new ApiError(xhr.status, { code: "unexpected_response", message: t.api.unexpected(xhr.status) }));
      }
    };
    xhr.onerror = () => reject(new ApiError(0, { code: "network", message: t.spaceTransfer.uploadBroke }));
    const form = new FormData();
    form.append("file", request.file, request.file.name);
    xhr.send(form);
  });
}

/** Sends an archive to import, keeping how much of it has gone. */
export function useImportSpace() {
  const [progress, setProgress] = useState(0);
  const mutation = useMutation({
    mutationFn: (request: ImportRequest) => {
      setProgress(0);
      return importSpace(request, setProgress);
    },
  });
  return { ...mutation, progress };
}

/** One import, asked again while it is under way; a finished one lists its space. */
export function useSpaceImport(id: string | undefined) {
  const queryClient = useQueryClient();
  return useQuery({
    queryKey: ["space-imports", id],
    queryFn: async (): Promise<SpaceImport> => {
      const job = (await api.GET("/space-imports/{importID}", { params: { path: { importID: id! } } })).data!.import;
      if (job.state === "done") await queryClient.invalidateQueries({ queryKey: spacesQueryKey });
      return job;
    },
    enabled: !!id,
    refetchInterval: (query) => (transferOpen(query.state.data) ? SPACE_TRANSFER_POLL_MS : false),
  });
}
