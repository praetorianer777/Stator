import { createRoute } from "@tanstack/react-router";
import { EmptyState, PageHeader } from "@/components/ui";
import { Icon } from "@/components/icons";
import { BrandSettings } from "@/features/brand/BrandSettings";
import { useCanAdministerOrg } from "@/features/permissions/access";
import { t } from "@/i18n";
import { appRoute } from "./app";

export const brandSettingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/brand",
  component: function BrandSettingsRoute() {
    if (!useCanAdministerOrg()) {
      return (
        <div className="mx-auto max-w-3xl">
          <PageHeader crumb={t.settings.title} title={t.brand.title} />
          <EmptyState icon={<Icon.Lock />} title={t.brand.title} description={t.brand.notAdmin} />
        </div>
      );
    }
    return <BrandSettings />;
  },
});
