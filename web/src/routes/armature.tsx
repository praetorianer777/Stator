import { createRoute } from "@tanstack/react-router";
import { PageHeader } from "@/components/ui";
import { ConnectionSettings } from "@/features/armature/ConnectionSettings";
import { ARMATURE_SETTINGS_PATH } from "@/config";
import { t } from "@/i18n";
import { appRoute } from "./app";

/** Where an administrator connects the organization's Armature and its webhook. */
export const armatureSettingsRoute = createRoute({
  getParentRoute: () => appRoute,
  path: ARMATURE_SETTINGS_PATH,
  component: function ArmatureSettings() {
    return (
      <div className="mx-auto max-w-3xl">
        <PageHeader crumb={t.settings.title} title={t.armature.title} />
        <ConnectionSettings />
      </div>
    );
  },
});
