import { createRoute } from "@tanstack/react-router";
import { StaleReport } from "@/features/stale/StaleReport";
import { appRoute } from "./app";

/** The stale content report; a space's settings link here with the space chosen. */
export const staleRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/stale",
  validateSearch: (search: Record<string, unknown>): { space?: string } =>
    typeof search.space === "string" && search.space !== "" ? { space: search.space.toUpperCase() } : {},
  component: function StaleRoute() {
    const { space } = staleRoute.useSearch();
    return <StaleReport key={space ?? ""} space={space} />;
  },
});
