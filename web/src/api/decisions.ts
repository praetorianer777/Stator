import { useQuery } from "@tanstack/react-query";
import { api } from "./client";
import type { components } from "./schema";

export type Decision = components["schemas"]["Decision"];
export type DecisionFilter = "decided" | "undecided" | undefined;

/** A space's decision log, as the reader may read it, of one state or both. */
export function useDecisions(spaceKey: string, state: DecisionFilter) {
  return useQuery({
    queryKey: ["decisions", spaceKey.toUpperCase(), state ?? "all"],
    queryFn: async () => (await api.GET("/spaces/{spaceKey}/decisions", { params: { path: { spaceKey }, query: state ? { state } : {} } })).data!,
  });
}
