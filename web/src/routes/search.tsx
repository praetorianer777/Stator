import { createRoute, useNavigate } from "@tanstack/react-router";
import { SearchScreen, type SearchAddress } from "@/features/search/SearchScreen";
import { appRoute } from "./app";

// The router parses a bare number or true out of the address as JSON, and a
// query of 2026 is still words.
function text(value: unknown): string | undefined {
  if (typeof value === "string") return value === "" ? undefined : value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return undefined;
}

const DAY = /^\d{4}-\d{2}-\d{2}$/;

/** The query and every filter are in the address, so a search somebody sends is the search they ran. */
export const searchRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/search",
  validateSearch: (search: Record<string, unknown>): SearchAddress => {
    const page = Number(search.page);
    const after = text(search.updatedAfter);
    const before = text(search.updatedBefore);
    return {
      ...(text(search.q) ? { q: text(search.q) } : {}),
      ...(text(search.space) ? { space: text(search.space) } : {}),
      ...(text(search.type) ? { type: text(search.type) } : {}),
      ...(text(search.label) ? { label: text(search.label) } : {}),
      ...(text(search.author) ? { author: text(search.author) } : {}),
      ...(after && DAY.test(after) ? { updatedAfter: after } : {}),
      ...(before && DAY.test(before) ? { updatedBefore: before } : {}),
      ...(search.sort === "updated" || search.sort === "relevance" ? { sort: search.sort } : {}),
      ...(search.archived === true || search.archived === "true" ? { archived: true } : {}),
      ...(Number.isInteger(page) && page > 1 ? { page } : {}),
    };
  },
  component: function SearchRoute() {
    const address = searchRoute.useSearch();
    const navigate = useNavigate();
    return <SearchScreen address={address} onChange={(next, replace) => navigate({ to: "/search", search: next, replace })} />;
  },
});
