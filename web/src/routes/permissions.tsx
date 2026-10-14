import { createRoute } from "@tanstack/react-router";
import { EmptyState, PageHeader } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SitePermissions } from "@/features/permissions/SitePermissions";
import { useCanAdministerSite } from "@/features/permissions/access";
import { t } from "@/i18n";
import { appRoute } from "./app";

export const sitePermissionsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/permissions",
  component: function SitePermissionsRoute() {
    if (!useCanAdministerSite()) {
      return (
        <div className="mx-auto max-w-3xl">
          <PageHeader crumb={t.settings.title} title={t.permissions.siteTitle} />
          <EmptyState icon={<Icon.Lock />} title={t.permissions.siteTitle} description={t.permissions.notSiteAdmin} />
        </div>
      );
    }
    return <SitePermissions />;
  },
});
