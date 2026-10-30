import { createRoute } from "@tanstack/react-router";
import { Page, PageHeader, EmptyState } from "@/components/ui";
import { Icon } from "@/components/icons";
import { AuditLog } from "@/features/audit/AuditLog";
import { useCanAdministerSite } from "@/features/permissions/access";
import { t } from "@/i18n";
import { appRoute } from "./app";

/** Who did what to the organization, newest first, for its administrators. */
export const auditRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/audit",
  component: function AuditRoute() {
    if (!useCanAdministerSite()) {
      return (
        <Page width="narrow" data-audit-refused>
          <PageHeader title={t.audit.title} />
          <EmptyState icon={<Icon.Lock />} title={t.audit.title} description={t.audit.notAdmin} />
        </Page>
      );
    }
    return <AuditLog />;
  },
});
