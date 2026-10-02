import { createLazyRoute } from "@tanstack/react-router";
import { TemplateEditor } from "@/features/templates/TemplateEditor";
import { templateEditRoute, templateNewRoute } from "./templates";

export const NewRoute = createLazyRoute(templateNewRoute.id)({
  component: function TemplateNewRoute() {
    const { space } = templateNewRoute.useSearch();
    return <TemplateEditor key={space ?? ""} spaceKey={space} />;
  },
});

export const EditRoute = createLazyRoute(templateEditRoute.id)({
  component: function TemplateEditRoute() {
    const { templateKey } = templateEditRoute.useParams();
    return <TemplateEditor key={templateKey} templateKey={templateKey} />;
  },
});
