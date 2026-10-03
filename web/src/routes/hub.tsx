import { createRoute } from "@tanstack/react-router";
import { EmptyState, PageHeader } from "@/components/ui";
import { Icon } from "@/components/icons";
import { HubSettings } from "@/features/hub/HubSettings";
import { useCanAdministerOrg } from "@/features/permissions/access";
import { t } from "@/i18n";
import { appRoute } from "./app";

export const hubSettingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/hub",
  component: function HubSettingsRoute() {
    if (!useCanAdministerOrg()) {
      return (
        <div className="mx-auto max-w-3xl">
          <PageHeader crumb={t.settings.title} title={t.hub.title} />
          <EmptyState icon={<Icon.Lock />} title={t.hub.title} description={t.hub.notAdmin} />
        </div>
      );
    }
    return <HubSettings />;
  },
});
