import { createRoute, useNavigate } from "@tanstack/react-router";
import { pageQuery } from "@/api/pages";
import type { CompareRef } from "@/api/versions";
import { PageHistory, type HistorySearch } from "@/features/pages/PageHistory";
import { spaceRoute } from "./space";

function versionNumber(value: unknown, min: number): number | undefined {
  const n = typeof value === "string" && value.trim() !== "" ? Number(value) : value;
  return typeof n === "number" && Number.isInteger(n) && n >= min ? n : undefined;
}

function compareRef(value: unknown): CompareRef | undefined {
  return value === "draft" ? "draft" : versionNumber(value, 0);
}

/** A page's history: the list, one version read-only, or two sides compared. */
export const pageHistoryRoute = createRoute({
  getParentRoute: () => spaceRoute,
  path: "/p/$pageId/$slug/history",
  validateSearch: (search: Record<string, unknown>): HistorySearch => {
    const out: HistorySearch = {};
    const version = versionNumber(search.version, 1);
    const from = compareRef(search.from);
    const to = compareRef(search.to);
    const offset = versionNumber(search.offset, 1);
    if (version !== undefined) out.version = version;
    if (from !== undefined) out.from = from;
    if (to !== undefined) out.to = to;
    if (offset !== undefined) out.offset = offset;
    return out;
  },
  loader: ({ context, params }) => context.queryClient.ensureQueryData(pageQuery(params.pageId)),
  component: function PageHistoryRoute() {
    const { pageId } = pageHistoryRoute.useParams();
    const search = pageHistoryRoute.useSearch();
    const navigate = useNavigate({ from: pageHistoryRoute.fullPath });
    return <PageHistory pageId={pageId} search={search} onSearch={(next) => void navigate({ search: next })} />;
  },
});
