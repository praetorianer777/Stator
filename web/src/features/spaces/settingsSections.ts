import type { Space } from "@/api/spaces";
import { t } from "@/i18n";

/** A space's settings, one route each, in the order of the suite's sections. */
export const SPACE_SETTINGS_SECTIONS = ["general", "workflow", "templates", "members", "integrations", "export", "trash"] as const;
export type SpaceSettingsSection = (typeof SPACE_SETTINGS_SECTIONS)[number];

/** The sections only a space's administrators use, so nobody else is shown them. */
const ADMIN_ONLY: ReadonlySet<SpaceSettingsSection> = new Set(["workflow", "members", "export"]);

export function sectionLabel(section: SpaceSettingsSection): string {
  return t.spaceSettings.sections[section];
}

export function adminOnly(section: SpaceSettingsSection): boolean {
  return ADMIN_ONLY.has(section);
}

/** The sections somebody is shown: every one to the space's administrators, the rest to its other members. */
export function sectionsFor(space: Pick<Space, "can">): SpaceSettingsSection[] {
  return SPACE_SETTINGS_SECTIONS.filter((section) => space.can.administer || !adminOnly(section));
}

export function isSection(value: string): value is SpaceSettingsSection {
  return (SPACE_SETTINGS_SECTIONS as readonly string[]).includes(value);
}

/** The anchor of a part of a section that a tab of its own used to open. */
export const GUESTS_ANCHOR = "guests";
export const ARCHIVE_ANCHOR = "archive";

/** Where each tab of the old /settings?tab= address lives now, with the part of the section it opened. */
const OLD_TABS: Record<string, { section: SpaceSettingsSection; hash?: string }> = {
  details: { section: "general" },
  reviews: { section: "workflow" },
  templates: { section: "templates" },
  permissions: { section: "members" },
  guests: { section: "members", hash: GUESTS_ANCHOR },
  shortcuts: { section: "integrations" },
  export: { section: "export" },
  trash: { section: "trash" },
  archive: { section: "trash", hash: ARCHIVE_ANCHOR },
};

export function spaceSettingsPath(spaceKey: string, section: SpaceSettingsSection = "general"): string {
  return `/spaces/${spaceKey}/settings/${section}`;
}

/** Where /spaces/<key>/settings sends somebody: to the section an old tab names, else General. */
export function settingsAddress(spaceKey: string, tab: unknown, hash = ""): string {
  const old = typeof tab === "string" && Object.hasOwn(OLD_TABS, tab) ? OLD_TABS[tab] : undefined;
  const anchor = hash || old?.hash;
  return `${spaceSettingsPath(spaceKey, old?.section)}${anchor ? `#${anchor}` : ""}`;
}

/**
 * Where an old /s/<key> address lives now, from what follows /s/, with its
 * search and hash kept. The space's stale pages moved below General.
 */
export function movedSpaceAddress(rest: string, searchStr = "", hash = ""): string {
  const after = `${searchStr}${hash ? `#${hash}` : ""}`;
  const [key = "", ...below] = rest.split("/");
  const path = below.join("/");
  if (path === "settings/stale") return `/spaces/${key}/settings/general/stale${after}`;
  return `/spaces/${key}${path ? `/${path}` : ""}${after}`;
}
