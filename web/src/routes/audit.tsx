import { createRoute } from "@tanstack/react-router";
import { EmptyState, PageHeader } from "@/components/ui";
import { Icon } from "@/components/icons";
import { AuditLog } from "@/features/audit/AuditLog";
import { useCanAdministerOrg } from "@/features/permissions/access";
import { t } from "@/i18n";
import { appRoute } from "./app";

/** Who did what to the organization, newest first, for its administrators. */
export const auditRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/audit",
  component: function AuditRoute() {
    if (!useCanAdministerOrg()) {
      return (
        <div className="mx-auto max-w-3xl" data-audit-refused>
          <PageHeader crumb={t.settings.title} title={t.audit.title} />
          <EmptyState icon={<Icon.Lock />} title={t.audit.title} description={t.audit.notAdmin} />
        </div>
      );
    }
    return <AuditLog />;
  },
});
