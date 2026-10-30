import { createLazyRoute } from "@tanstack/react-router";
import { TemplateEditor } from "@/features/templates/TemplateEditor";
import { spaceTemplateEditRoute, spaceTemplateNewRoute, templateEditRoute, templateNewRoute } from "./templates";

export const NewRoute = createLazyRoute(templateNewRoute.id)({
  component: function TemplateNewRoute() {
    return <TemplateEditor />;
  },
});

export const EditRoute = createLazyRoute(templateEditRoute.id)({
  component: function TemplateEditRoute() {
    const { templateKey } = templateEditRoute.useParams();
    return <TemplateEditor key={templateKey} templateKey={templateKey} />;
  },
});

export const SpaceNewRoute = createLazyRoute(spaceTemplateNewRoute.id)({
  component: function SpaceTemplateNewRoute() {
    const { spaceKey } = spaceTemplateNewRoute.useParams();
    return <TemplateEditor key={spaceKey} spaceKey={spaceKey} />;
  },
});

export const SpaceEditRoute = createLazyRoute(spaceTemplateEditRoute.id)({
  component: function SpaceTemplateEditRoute() {
    const { spaceKey, templateKey } = spaceTemplateEditRoute.useParams();
    return <TemplateEditor key={templateKey} spaceKey={spaceKey} templateKey={templateKey} />;
  },
});
