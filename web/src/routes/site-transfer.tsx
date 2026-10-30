import { createRoute } from "@tanstack/react-router";
import { Page, PageHeader, EmptyState } from "@/components/ui";
import { Icon } from "@/components/icons";
import { SiteTransferSettings } from "@/features/siteTransfer/SiteTransferSettings";
import { useCanAdministerSite } from "@/features/permissions/access";
import { t } from "@/i18n";
import { appRoute } from "./app";

export const siteTransferRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/transfer",
  component: function SiteTransferRoute() {
    if (!useCanAdministerSite()) {
      return (
        <Page width="narrow">
          <PageHeader title={t.siteTransfer.title} />
          <EmptyState icon={<Icon.Lock />} title={t.siteTransfer.title} description={t.siteTransfer.notAdmin} />
        </Page>
      );
    }
    return <SiteTransferSettings />;
  },
});
