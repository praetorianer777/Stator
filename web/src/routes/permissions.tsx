import { createRoute } from "@tanstack/react-router";
import { EmptyState, PageHeader } from "@/components/ui";
import { Icon } from "@/components/icons";
import { OrgPermissions } from "@/features/permissions/OrgPermissions";
import { useCanAdministerOrg } from "@/features/permissions/access";
import { t } from "@/i18n";
import { appRoute } from "./app";

export const orgPermissionsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/permissions",
  component: function OrgPermissionsRoute() {
    if (!useCanAdministerOrg()) {
      return (
        <div className="mx-auto max-w-3xl">
          <PageHeader crumb={t.settings.title} title={t.permissions.orgTitle} />
          <EmptyState icon={<Icon.Lock />} title={t.permissions.orgTitle} description={t.permissions.notOrgAdmin} />
        </div>
      );
    }
    return <OrgPermissions />;
  },
});
