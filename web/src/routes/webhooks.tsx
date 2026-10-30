import { createRoute } from "@tanstack/react-router";
import { Page, PageHeader, EmptyState } from "@/components/ui";
import { Icon } from "@/components/icons";
import { WEBHOOKS_PATH } from "@/config";
import { useCanAdministerSite } from "@/features/permissions/access";
import { Webhooks } from "@/features/webhooks/Webhooks";
import { t } from "@/i18n";
import { appRoute } from "./app";

/** Where the organization's events are posted, signed, with a log of every attempt, for its administrators. */
export const webhooksRoute = createRoute({
  getParentRoute: () => appRoute,
  path: WEBHOOKS_PATH,
  component: function WebhooksRoute() {
    if (!useCanAdministerSite()) {
      return (
        <Page width="narrow" data-webhooks-refused>
          <PageHeader title={t.webhooks.title} />
          <EmptyState icon={<Icon.Lock />} title={t.webhooks.title} description={t.webhooks.notAdmin} />
        </Page>
      );
    }
    return <Webhooks />;
  },
});
