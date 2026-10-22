import { createRoute } from "@tanstack/react-router";
import { EmptyState, PageHeader } from "@/components/ui";
import { Icon } from "@/components/icons";
import { HubSettings } from "@/features/hub/HubSettings";
import { useCanAdministerSite } from "@/features/permissions/access";
import { t } from "@/i18n";
import { appRoute } from "./app";
import { settingsCrumb } from "@/features/settings/SettingsHome";

export const hubSettingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/hub",
  component: function HubSettingsRoute() {
    if (!useCanAdministerSite()) {
      return (
        <div className="mx-auto max-w-3xl">
          <PageHeader crumbs={[settingsCrumb()]} title={t.hub.title} />
          <EmptyState icon={<Icon.Lock />} title={t.hub.title} description={t.hub.notAdmin} />
        </div>
      );
    }
    return <HubSettings />;
  },
});
