import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { API_BASE, SPACE_TRANSFER_POLL_MS } from "@/config";
import { api } from "./client";
import type { components } from "./schema";
import { spacesQueryKey } from "./spaces";
import { transferOpen, uploadArchive } from "./spaceTransfers";

type Wire = components["schemas"];

/** One export of the whole organization, which the worker makes while the settings follow it. */
export type SiteExport = Wire["SiteExport"];
/** One import of an organization's archive into this organization, followed to its report. */
export type SiteImport = Wire["SiteImport"];
/** What an organization's import brought across, and what it did not. */
export type SiteImportReport = Wire["SiteImportReport"];
/** One kind of thing an organization's archive leaves out. */
export type SiteOmission = Wire["SiteOmission"];

const siteExportsQueryKey = ["site-exports"] as const;
const siteImportsQueryKey = ["site-imports"] as const;

/** The organization's latest exports, asked again while one is under way. */
export function useSiteExports() {
  return useQuery({
    queryKey: siteExportsQueryKey,
    queryFn: async (): Promise<SiteExport[]> => (await api.GET("/site/exports")).data!.exports,
    refetchInterval: (query) => (query.state.data?.some(transferOpen) ? SPACE_TRANSFER_POLL_MS : false),
  });
}

/** Asks the worker for an export of the whole organization. */
export function useCreateSiteExport() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<SiteExport> => (await api.POST("/site/exports")).data!.export,
    onSuccess: (job) => {
      queryClient.setQueryData<SiteExport[]>(siteExportsQueryKey, (jobs) => [job, ...(jobs ?? []).filter((j) => j.id !== job.id)]);
    },
  });
}

/** Where an organization's archive is downloaded from, which the browser saves itself. */
export function siteExportHref(id: string): string {
  return `${API_BASE}/site/exports/${encodeURIComponent(id)}/file`;
}

/** The organization's latest imports, asked again while one is under way; a finished one fills the spaces. */
export function useSiteImports() {
  const queryClient = useQueryClient();
  return useQuery({
    queryKey: siteImportsQueryKey,
    queryFn: async (): Promise<SiteImport[]> => {
      const jobs = (await api.GET("/site/imports")).data!.imports;
      if (jobs.some((j) => j.state === "done")) await queryClient.invalidateQueries({ queryKey: spacesQueryKey });
      return jobs;
    },
    refetchInterval: (query) => (query.state.data?.some(transferOpen) ? SPACE_TRANSFER_POLL_MS : false),
  });
}

/** Sends an organization's archive to import, keeping how much of it has gone. */
export function useImportSite() {
  const queryClient = useQueryClient();
  const [progress, setProgress] = useState(0);
  const mutation = useMutation({
    mutationFn: async (file: File): Promise<SiteImport> => {
      setProgress(0);
      return (await uploadArchive<{ import: SiteImport }>(`${API_BASE}/site/imports`, file, setProgress)).import;
    },
    onSuccess: (job) => {
      queryClient.setQueryData<SiteImport[]>(siteImportsQueryKey, (jobs) => [job, ...(jobs ?? []).filter((j) => j.id !== job.id)]);
    },
  });
  return { ...mutation, progress };
}
