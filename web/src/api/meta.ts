import { useQuery } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

/** Which product and build the api is, which Armature and Exciter answer in the same shape. */
export type Meta = components["schemas"]["Meta"];

export const metaQuery = {
  queryKey: ["meta"] as const,
  queryFn: async (): Promise<Meta> => (await api.GET("/meta")).data!,
  // The running build changes only with a deployment; once per load is enough.
  staleTime: Number.POSITIVE_INFINITY,
  // Unanswered, the sidebar shows the client's own build, so there is nothing to wait for.
  retry: false,
} as const;

export function useMeta() {
  return useQuery(metaQuery);
}
