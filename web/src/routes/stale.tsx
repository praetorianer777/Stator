import { createRoute } from "@tanstack/react-router";
import { StaleReport } from "@/features/stale/StaleReport";
import { adminRoute } from "./admin";
import { spaceRoute } from "./space";

/** The stale pages of every space, for the installation's administrators. */
export const staleRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/stale",
  validateSearch: (search: Record<string, unknown>): { space?: string } =>
    typeof search.space === "string" && search.space !== "" ? { space: search.space.toUpperCase() } : {},
  component: function StaleRoute() {
    const { space } = staleRoute.useSearch();
    return <StaleReport key={space ?? ""} space={space} />;
  },
});

/** One space's stale pages, below General in its settings, for whoever administers it. */
export const spaceStaleRoute = createRoute({
  getParentRoute: () => spaceRoute,
  path: "/settings/general/stale",
  component: function SpaceStaleRoute() {
    const { spaceKey } = spaceStaleRoute.useParams();
    return <StaleReport key={spaceKey} space={spaceKey} />;
  },
});
