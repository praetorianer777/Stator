import { createRoute, useNavigate } from "@tanstack/react-router";
import type { DecisionFilter } from "@/api/decisions";
import { DecisionLog } from "@/features/decisions/DecisionLog";
import { spaceRoute } from "./space";

/** The state the log keeps is in the address, so a filtered log somebody sends opens filtered. */
function stateOf(search: Record<string, unknown>): { state?: "decided" | "undecided" } {
  return search.state === "decided" || search.state === "undecided" ? { state: search.state } : {};
}

/** A space's decision log. */
export const spaceDecisionsRoute = createRoute({
  getParentRoute: () => spaceRoute,
  path: "/decisions",
  validateSearch: stateOf,
  component: function DecisionsRoute() {
    const { spaceKey } = spaceDecisionsRoute.useParams();
    const { state } = spaceDecisionsRoute.useSearch();
    const navigate = useNavigate({ from: spaceDecisionsRoute.fullPath });
    return <DecisionLog spaceKey={spaceKey} state={state} onState={(next: DecisionFilter) => navigate({ search: next ? { state: next } : {} })} />;
  },
});
