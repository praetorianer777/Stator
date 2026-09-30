import { createRoute, useNavigate } from "@tanstack/react-router";
import { useSpace } from "@/api/spaces";
import { Skeleton } from "@/components/ui";
import { LabelScreen } from "@/features/labels/LabelScreen";
import { appRoute } from "./app";
import { spaceRoute } from "./space";

/** The page of the list is in the address, counted from 1, so a list somebody sends opens where they were. */
function pageOf(search: Record<string, unknown>): { page?: number } {
  const page = Number(search.page);
  return Number.isInteger(page) && page > 1 ? { page } : {};
}

/** The pages with a label, in every space. */
export const labelRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/labels/$name",
  validateSearch: pageOf,
  component: function LabelRoute() {
    const { name } = labelRoute.useParams();
    const { page = 1 } = labelRoute.useSearch();
    const navigate = useNavigate({ from: labelRoute.fullPath });
    return <LabelScreen name={name} page={page} onPage={(next) => navigate({ search: next > 1 ? { page: next } : {} })} />;
  },
});

/** The pages with a label in one space. */
export const spaceLabelRoute = createRoute({
  getParentRoute: () => spaceRoute,
  path: "/labels/$name",
  validateSearch: pageOf,
  component: function SpaceLabelRoute() {
    const { spaceKey, name } = spaceLabelRoute.useParams();
    const { page = 1 } = spaceLabelRoute.useSearch();
    const navigate = useNavigate({ from: spaceLabelRoute.fullPath });
    const { data: space } = useSpace(spaceKey);
    if (!space) return <Skeleton />;
    return <LabelScreen name={name} space={space} page={page} onPage={(next) => navigate({ search: next > 1 ? { page: next } : {} })} />;
  },
});
