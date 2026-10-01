import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { STALE_PAGE_SIZE } from "@/config";
import { api } from "./client";
import type { components } from "./schema";

type Wire = components["schemas"];

/** A page nobody published or opened within the period, with who answers for it and whether it is verified. */
export type StalePage = Wire["StalePage"];
export type StaleVerification = StalePage["verification"];

/** How the report is narrowed; owner is a person's id, or "none" for pages nobody owns. */
export interface StaleFilter {
  space?: string;
  owner?: string;
  verification?: StaleVerification;
  /** Lists archived pages too, which the report leaves out otherwise. */
  archived?: boolean;
  olderThan: number;
}

export const staleQueryKey = ["stale-pages"] as const;

/** One page of the report, the longest untouched first, after the cursor; next is the cursor of the page after. */
export function useStalePages(filter: StaleFilter, cursor: string | undefined) {
  return useQuery({
    queryKey: [...staleQueryKey, filter, cursor ?? ""],
    queryFn: async () => (await api.GET("/stale-pages", { params: { query: { ...filter, limit: STALE_PAGE_SIZE, cursor } } })).data!,
    placeholderData: keepPreviousData,
  });
}
