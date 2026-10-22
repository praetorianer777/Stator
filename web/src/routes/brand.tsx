import { createRoute } from "@tanstack/react-router";
import { EmptyState, PageHeader } from "@/components/ui";
import { Icon } from "@/components/icons";
import { BrandSettings } from "@/features/brand/BrandSettings";
import { useCanAdministerSite } from "@/features/permissions/access";
import { t } from "@/i18n";
import { appRoute } from "./app";
import { settingsCrumb } from "@/features/settings/SettingsHome";

export const brandSettingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/brand",
  component: function BrandSettingsRoute() {
    if (!useCanAdministerSite()) {
      return (
        <div className="mx-auto max-w-3xl">
          <PageHeader crumbs={[settingsCrumb()]} title={t.brand.title} />
          <EmptyState icon={<Icon.Lock />} title={t.brand.title} description={t.brand.notAdmin} />
        </div>
      );
    }
    return <BrandSettings />;
  },
});
