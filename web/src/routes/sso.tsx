import { createRoute } from "@tanstack/react-router";
import { Page, PageHeader } from "@/components/ui";
import { GroupRoles } from "@/features/auth/GroupRoles";
import { JoinRequests } from "@/features/auth/JoinRequests";
import { Members } from "@/features/auth/Members";
import { ProviderSettings } from "@/features/auth/ProviderSettings";
import { t } from "@/i18n";
import { appRoute } from "./app";

export const ssoRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/sso",
  component: function SingleSignOn() {
    return (
      <Page width="narrow">
        <PageHeader title={t.sso.title} />
        <JoinRequests />
        <ProviderSettings />
        <GroupRoles />
        <Members />
      </Page>
    );
  },
});
