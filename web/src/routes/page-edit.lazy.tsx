import { createLazyRoute } from "@tanstack/react-router";
import { PageEditor } from "@/features/pages/PageEditor";
import { pageEditRoute } from "./page-edit";

export const Route = createLazyRoute(pageEditRoute.id)({
  component: function PageEditRoute() {
    const { pageId } = pageEditRoute.useParams();
    return <PageEditor pageId={pageId} />;
  },
});
