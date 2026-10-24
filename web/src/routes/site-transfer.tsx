import { createRoute } from "@tanstack/react-router";
import { EmptyState, PageHeader } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SiteTransferSettings } from "@/features/siteTransfer/SiteTransferSettings";
import { useCanAdministerSite } from "@/features/permissions/access";
import { t } from "@/i18n";
import { appRoute } from "./app";
import { settingsCrumb } from "@/features/settings/SettingsHome";

export const siteTransferRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/transfer",
  component: function SiteTransferRoute() {
    if (!useCanAdministerSite()) {
      return (
        <div className="mx-auto max-w-3xl">
          <PageHeader crumbs={[settingsCrumb()]} title={t.siteTransfer.title} />
          <EmptyState icon={<Icon.Lock />} title={t.siteTransfer.title} description={t.siteTransfer.notAdmin} />
        </div>
      );
    }
    return <SiteTransferSettings />;
  },
});
