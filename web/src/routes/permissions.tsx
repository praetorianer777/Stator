import { createRoute } from "@tanstack/react-router";
import { Page, PageHeader, EmptyState } from "@/components/ui";
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
        <Page width="narrow">
          <PageHeader title={t.permissions.siteTitle} />
          <EmptyState icon={<Icon.Lock />} title={t.permissions.siteTitle} description={t.permissions.notSiteAdmin} />
        </Page>
      );
    }
    return <SitePermissions />;
  },
});
