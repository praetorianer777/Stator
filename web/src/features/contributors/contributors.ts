import { CONTRIBUTORS_DEFAULT_LIMIT, CONTRIBUTORS_MAX_LIMIT, CONTRIBUTOR_SCOPES } from "@/config";

export const CONTRIBUTORS_NODE = "contributors";

export type ContributorScope = (typeof CONTRIBUTOR_SCOPES)[number];

/** What a contributors block stores: which pages it counts and how many people it names, never the people. */
export interface ContributorsSettings {
  scope: ContributorScope;
  limit: number;
}

/** A block's attributes as the server takes them, each one wrong put right. */
export function contributorsSettings(attrs: Record<string, unknown> | undefined): ContributorsSettings {
  const limit = Number(attrs?.limit);
  return {
    scope: (CONTRIBUTOR_SCOPES as readonly unknown[]).includes(attrs?.scope) ? (attrs?.scope as ContributorScope) : "page",
    limit: Number.isInteger(limit) && limit >= 1 && limit <= CONTRIBUTORS_MAX_LIMIT ? limit : CONTRIBUTORS_DEFAULT_LIMIT,
  };
}
