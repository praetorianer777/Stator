import { createRoute } from "@tanstack/react-router";
import { EmptyState, PageHeader } from "@/components/ui";
import { Icon } from "@/components/icons";
import { OrgTransferSettings } from "@/features/orgTransfer/OrgTransferSettings";
import { useCanAdministerOrg } from "@/features/permissions/access";
import { t } from "@/i18n";
import { appRoute } from "./app";

export const orgTransferRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/transfer",
  component: function OrgTransferRoute() {
    if (!useCanAdministerOrg()) {
      return (
        <div className="mx-auto max-w-3xl">
          <PageHeader crumb={t.settings.title} title={t.orgTransfer.title} />
          <EmptyState icon={<Icon.Lock />} title={t.orgTransfer.title} description={t.orgTransfer.notAdmin} />
        </div>
      );
    }
    return <OrgTransferSettings />;
  },
});
