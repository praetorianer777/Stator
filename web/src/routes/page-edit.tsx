import { createRoute } from "@tanstack/react-router";
import { pageQuery } from "@/api/pages";
import { draftQuery } from "@/api/versions";
import { spaceRoute } from "./space";

// The editor is most of the bundle's weight, and most visits only read, so
// this route's component arrives in a chunk of its own when it is opened.
export const pageEditRoute = createRoute({
  getParentRoute: () => spaceRoute,
  path: "/p/$pageId/$slug/edit",
  // The draft is read afresh: the form starts from it once, and another tab
  // may have saved a newer one since this one last looked. Somebody who may
  // not edit has no draft to read, and the editor tells them so.
  loader: async ({ context, params }) => {
    const { page } = await context.queryClient.ensureQueryData(pageQuery(params.pageId));
    if (page.can.edit) await context.queryClient.fetchQuery({ ...draftQuery(params.pageId), staleTime: 0 });
  },
}).lazy(() => import("./page-edit.lazy").then((module) => module.Route));
