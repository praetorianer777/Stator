import { createRoute } from "@tanstack/react-router";
import { PageHeader } from "@/components/ui";
import { JoinRequests } from "@/features/auth/JoinRequests";
import { ProviderSettings } from "@/features/auth/ProviderSettings";
import { t } from "@/i18n";
import { appRoute } from "./app";

export const ssoRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/settings/sso",
  component: function SingleSignOn() {
    return (
      <div className="mx-auto max-w-3xl">
        <PageHeader crumb={t.settings.title} title={t.sso.title} />
        <JoinRequests />
        <ProviderSettings />
      </div>
    );
  },
});
