import { createRoute } from "@tanstack/react-router";
import { Page, PageHeader } from "@/components/ui";
import { TemplateList } from "@/features/templates/TemplateList";
import { t } from "@/i18n";
import { adminRoute } from "./admin";
import { spaceRoute } from "./space";

/** The templates every space offers, kept by the installation's administrators. */
export const templatesRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/templates",
  component: function TemplatesRoute() {
    return (
      <Page width="narrow" data-templates-page="">
        <PageHeader title={t.templates.title} />
        <TemplateList canEdit />
      </Page>
    );
  },
});

// The editor is most of the bundle's weight, so making and changing a
// template arrives in a chunk of its own, as editing a page does.

/** A new template for every space. */
export const templateNewRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/templates/new",
}).lazy(() => import("./templates.lazy").then((module) => module.NewRoute));

export const templateEditRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/templates/$templateKey",
}).lazy(() => import("./templates.lazy").then((module) => module.EditRoute));

/** A new template of one space, below its settings, for whoever administers it. */
export const spaceTemplateNewRoute = createRoute({
  getParentRoute: () => spaceRoute,
  path: "/settings/templates/new",
}).lazy(() => import("./templates.lazy").then((module) => module.SpaceNewRoute));

export const spaceTemplateEditRoute = createRoute({
  getParentRoute: () => spaceRoute,
  path: "/settings/templates/$templateKey",
}).lazy(() => import("./templates.lazy").then((module) => module.SpaceEditRoute));
