import { createRoute } from "@tanstack/react-router";
import { Page, PageHeader, EmptyState } from "@/components/ui";
import { Icon } from "@/components/icons";
import { HubSettings } from "@/features/hub/HubSettings";
import { useCanAdministerSite } from "@/features/permissions/access";
import { t } from "@/i18n";
import { appRoute } from "./app";

export const hubSettingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/hub",
  component: function HubSettingsRoute() {
    if (!useCanAdministerSite()) {
      return (
        <Page width="narrow">
          <PageHeader title={t.hub.title} />
          <EmptyState icon={<Icon.Lock />} title={t.hub.title} description={t.hub.notAdmin} />
        </Page>
      );
    }
    return <HubSettings />;
  },
});
