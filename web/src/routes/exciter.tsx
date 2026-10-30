import { createRoute } from "@tanstack/react-router";
import { Page, PageHeader } from "@/components/ui";
import { ExciterConnectionSettings } from "@/features/exciter/ConnectionSettings";
import { t } from "@/i18n";
import { adminRoute } from "./admin";

/** Where an administrator connects Exciter and its webhook. */
export const exciterSettingsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/exciter",
  component: function ExciterSettings() {
    return (
      <Page width="narrow">
        <PageHeader title={t.exciter.title} />
        <ExciterConnectionSettings />
      </Page>
    );
  },
});
