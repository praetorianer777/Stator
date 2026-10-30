import { createRoute } from "@tanstack/react-router";
import { Page, PageHeader, EmptyState } from "@/components/ui";
import { Icon } from "@/components/icons";
import { BrandSettings } from "@/features/brand/BrandSettings";
import { useCanAdministerSite } from "@/features/permissions/access";
import { t } from "@/i18n";
import { appRoute } from "./app";

export const brandSettingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/brand",
  component: function BrandSettingsRoute() {
    if (!useCanAdministerSite()) {
      return (
        <Page width="narrow">
          <PageHeader title={t.brand.title} />
          <EmptyState icon={<Icon.Lock />} title={t.brand.title} description={t.brand.notAdmin} />
        </Page>
      );
    }
    return <BrandSettings />;
  },
});
