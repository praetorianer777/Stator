import { createRoute } from "@tanstack/react-router";
import { PageHeader } from "@/components/ui";
import { GroupRoles } from "@/features/auth/GroupRoles";
import { JoinRequests } from "@/features/auth/JoinRequests";
import { Members } from "@/features/auth/Members";
import { ProviderSettings } from "@/features/auth/ProviderSettings";
import { t } from "@/i18n";
import { appRoute } from "./app";
import { settingsCrumb } from "@/features/settings/SettingsHome";

export const ssoRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/sso",
  component: function SingleSignOn() {
    return (
      <div className="mx-auto max-w-3xl">
        <PageHeader crumbs={[settingsCrumb()]} title={t.sso.title} />
        <JoinRequests />
        <ProviderSettings />
        <GroupRoles />
        <Members />
      </div>
    );
  },
});
