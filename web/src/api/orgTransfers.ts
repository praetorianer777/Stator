import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { API_BASE, SPACE_TRANSFER_POLL_MS } from "@/config";
import { api } from "./client";
import type { components } from "./schema";
import { spacesQueryKey } from "./spaces";
import { transferOpen, uploadArchive } from "./spaceTransfers";

type Wire = components["schemas"];

/** One export of the whole organization, which the worker makes while the settings follow it. */
export type OrgExport = Wire["OrgExport"];
/** One import of an organization's archive into this organization, followed to its report. */
export type OrgImport = Wire["OrgImport"];
/** What an organization's import brought across, and what it did not. */
export type OrgImportReport = Wire["OrgImportReport"];
/** One kind of thing an organization's archive leaves out. */
export type OrgOmission = Wire["OrgOmission"];

const orgExportsQueryKey = ["org-exports"] as const;
const orgImportsQueryKey = ["org-imports"] as const;

/** The organization's latest exports, asked again while one is under way. */
export function useOrgExports() {
  return useQuery({
    queryKey: orgExportsQueryKey,
    queryFn: async (): Promise<OrgExport[]> => (await api.GET("/org/exports")).data!.exports,
    refetchInterval: (query) => (query.state.data?.some(transferOpen) ? SPACE_TRANSFER_POLL_MS : false),
  });
}

/** Asks the worker for an export of the whole organization. */
export function useCreateOrgExport() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (): Promise<OrgExport> => (await api.POST("/org/exports")).data!.export,
    onSuccess: (job) => {
      queryClient.setQueryData<OrgExport[]>(orgExportsQueryKey, (jobs) => [job, ...(jobs ?? []).filter((j) => j.id !== job.id)]);
    },
  });
}

/** Where an organization's archive is downloaded from, which the browser saves itself. */
export function orgExportHref(id: string): string {
  return `${API_BASE}/org/exports/${encodeURIComponent(id)}/file`;
}

/** The organization's latest imports, asked again while one is under way; a finished one fills the spaces. */
export function useOrgImports() {
  const queryClient = useQueryClient();
  return useQuery({
    queryKey: orgImportsQueryKey,
    queryFn: async (): Promise<OrgImport[]> => {
      const jobs = (await api.GET("/org/imports")).data!.imports;
      if (jobs.some((j) => j.state === "done")) await queryClient.invalidateQueries({ queryKey: spacesQueryKey });
      return jobs;
    },
    refetchInterval: (query) => (query.state.data?.some(transferOpen) ? SPACE_TRANSFER_POLL_MS : false),
  });
}

/** Sends an organization's archive to import, keeping how much of it has gone. */
export function useImportOrg() {
  const queryClient = useQueryClient();
  const [progress, setProgress] = useState(0);
  const mutation = useMutation({
    mutationFn: async (file: File): Promise<OrgImport> => {
      setProgress(0);
      return (await uploadArchive<{ import: OrgImport }>(`${API_BASE}/org/imports`, file, setProgress)).import;
    },
    onSuccess: (job) => {
      queryClient.setQueryData<OrgImport[]>(orgImportsQueryKey, (jobs) => [job, ...(jobs ?? []).filter((j) => j.id !== job.id)]);
    },
  });
  return { ...mutation, progress };
}
