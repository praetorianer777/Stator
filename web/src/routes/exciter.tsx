import { createRoute } from "@tanstack/react-router";
import { Page, PageHeader } from "@/components/ui";
import { ExciterConnectionSettings } from "@/features/exciter/ConnectionSettings";
import { EXCITER_SETTINGS_PATH } from "@/config";
import { t } from "@/i18n";
import { appRoute } from "./app";

/** Where an administrator connects Exciter and its webhook. */
export const exciterSettingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: EXCITER_SETTINGS_PATH,
  component: function ExciterSettings() {
    return (
      <Page width="narrow">
        <PageHeader title={t.exciter.title} />
        <ExciterConnectionSettings />
      </Page>
    );
  },
});
