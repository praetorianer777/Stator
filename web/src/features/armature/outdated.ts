import type { ArmatureFeature } from "@/api/armature";
import { ARMATURE_FEATURE_CAPABILITIES } from "@/config";
import { t } from "@/i18n";

/** The sentence a reader sees where feature would be: which capability the connected Armature lacks, and whom to ask. */
export function outdatedSentence(feature: ArmatureFeature): string {
  const c = t.armature.capabilities;
  return c.outdated(c.features[feature], ARMATURE_FEATURE_CAPABILITIES[feature]);
}
