import { createRoute } from "@tanstack/react-router";
import { PageHeader } from "@/components/ui";
import { TEMPLATE_EDIT_PATH, TEMPLATE_NEW_PATH, TEMPLATES_PATH } from "@/config";
import { useCanAdministerOrg } from "@/features/permissions/access";
import { TemplateList } from "@/features/templates/TemplateList";
import { t } from "@/i18n";
import { appRoute } from "./app";

/** The templates every space offers, kept by the organization's administrators. */
export const templatesRoute = createRoute({
  getParentRoute: () => appRoute,
  path: TEMPLATES_PATH,
  component: function TemplatesRoute() {
    const canEdit = useCanAdministerOrg();
    return (
      <div className="mx-auto max-w-4xl" data-templates-page="">
        <PageHeader crumbs={[{ label: t.settings.title }]} title={t.templates.title} />
        <TemplateList canEdit={canEdit} />
      </div>
    );
  },
});

// The editor is most of the bundle's weight, so making and changing a
// template arrives in a chunk of its own, as editing a page does.

/** A new template, for the space named, else for every space. */
export const templateNewRoute = createRoute({
  getParentRoute: () => appRoute,
  path: TEMPLATE_NEW_PATH,
  validateSearch: (search: Record<string, unknown>): { space?: string } =>
    typeof search.space === "string" && search.space !== "" ? { space: search.space.toUpperCase() } : {},
}).lazy(() => import("./templates.lazy").then((module) => module.NewRoute));

export const templateEditRoute = createRoute({
  getParentRoute: () => appRoute,
  path: TEMPLATE_EDIT_PATH,
}).lazy(() => import("./templates.lazy").then((module) => module.EditRoute));
