import { createRoute } from "@tanstack/react-router";
import { pageQuery } from "@/api/pages";
import { spaceRoute } from "./space";

// The editor is most of the bundle's weight, and most visits only read, so
// this route's component arrives in a chunk of its own when it is opened.
export const pageEditRoute = createRoute({
  getParentRoute: () => spaceRoute,
  path: "/p/$pageId/$slug/edit",
  loader: ({ context, params }) => context.queryClient.ensureQueryData(pageQuery(params.pageId)),
}).lazy(() => import("./page-edit.lazy").then((module) => module.Route));
