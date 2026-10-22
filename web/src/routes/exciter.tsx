import { createRoute } from "@tanstack/react-router";
import { PageHeader } from "@/components/ui";
import { ExciterConnectionSettings } from "@/features/exciter/ConnectionSettings";
import { EXCITER_SETTINGS_PATH } from "@/config";
import { t } from "@/i18n";
import { appRoute } from "./app";
import { settingsCrumb } from "@/features/settings/SettingsHome";

/** Where an administrator connects Exciter and its webhook. */
export const exciterSettingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: EXCITER_SETTINGS_PATH,
  component: function ExciterSettings() {
    return (
      <div className="mx-auto max-w-3xl">
        <PageHeader crumbs={[settingsCrumb()]} title={t.exciter.title} />
        <ExciterConnectionSettings />
      </div>
    );
  },
});
