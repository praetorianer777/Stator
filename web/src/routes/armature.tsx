import { createRoute } from "@tanstack/react-router";
import { Page, PageHeader } from "@/components/ui";
import { ConnectionSettings } from "@/features/armature/ConnectionSettings";
import { ARMATURE_OUTCOME_PARAM, ARMATURE_OUTCOMES, type ArmatureOutcome } from "@/config";
import { t } from "@/i18n";
import { adminRoute } from "./admin";

/** Where an administrator connects the installation's Armature and its webhook. */
export const armatureSettingsRoute = createRoute({
  getParentRoute: () => adminRoute,
  path: "/armature",
  validateSearch: (search: Record<string, unknown>): { [ARMATURE_OUTCOME_PARAM]?: ArmatureOutcome } => {
    const outcome = ARMATURE_OUTCOMES.find((known) => known === search[ARMATURE_OUTCOME_PARAM]);
    return outcome ? { [ARMATURE_OUTCOME_PARAM]: outcome } : {};
  },
  component: function ArmatureSettings() {
    const search = armatureSettingsRoute.useSearch();
    // Checked again where it is read, as the profile does: the root route hands its search down unchecked.
    const outcome = ARMATURE_OUTCOMES.find((known) => known === search[ARMATURE_OUTCOME_PARAM]);
    return (
      <Page width="narrow">
        <PageHeader title={t.armature.title} />
        <ConnectionSettings outcome={outcome} />
      </Page>
    );
  },
});
