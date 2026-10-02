import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { HUB_STALE_MS } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

/** The organization's hub as the reader may see it: its page, or none, and whether everybody lands on it. */
export type Hub = components["schemas"]["Hub"];
export type HubChoice = components["schemas"]["HubInput"];

export const hubQueryKey = ["org", "hub"] as const;

export const hubQuery = {
  queryKey: hubQueryKey,
  queryFn: async (): Promise<Hub> => (await api.GET("/org/hub")).data!.hub,
  staleTime: HUB_STALE_MS,
} as const;

export function useHub() {
  return useQuery(hubQuery);
}

export function useSetHub() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: HubChoice): Promise<Hub> => (await api.PUT("/org/hub", { body })).data!.hub,
    onSuccess: (saved) => queryClient.setQueryData(hubQueryKey, saved),
  });
}
