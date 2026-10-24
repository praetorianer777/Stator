import { guestSpaceOf, useMe } from "@/api/auth";
import { useHub } from "@/api/hub";
import type { IconName } from "@/components/icons";
import {
  ARMATURE_SECTION_ID,
  ARMATURE_SETTINGS_PATH,
  AUDIT_PATH,
  BRAND_PATH,
  EXAMPLE_SPACE_PATH,
  EXCITER_SECTION_ID,
  EXCITER_SETTINGS_PATH,
  HUB_SETTINGS_PATH,
  NOTIFICATION_SETTINGS_PATH,
  PROFILE_PATH,
  SETTINGS_PATH,
  SITE_PERMISSIONS_PATH,
  SITE_TRANSFER_PATH,
  SSO_PATH,
  STALE_PATH,
  TEMPLATES_PATH,
  THEMES_PATH,
  TOKENS_PATH,
  WATCHING_PATH,
  WEBHOOKS_PATH,
} from "@/config";
import { useCanAdministerSite } from "@/features/permissions/access";
import { t } from "@/i18n";
import { pageSlug } from "@/lib/slug";

/**
 * Where a place is listed: the wiki's sidebar group, the settings area's You
 * group, which the avatar menu also lists, or its Settings group.
 */
export type PlaceArea = "wiki" | "you" | "settings";

/** A page the reader can go to: the sidebar, the settings area, the avatar menu and the palette all read these. */
export interface Place {
  /** Names the place in data-nav, and the attribute the avatar menu's entry carries in data-action. */
  id: string;
  area: PlaceArea;
  to: string;
  hash?: string;
  icon: IconName;
  label: string;
  /** Marked as where the reader is only on this very path, not below it. */
  exact?: boolean;
  /** Listed in the avatar menu as well as in the settings area. */
  inAccountMenu?: boolean;
}

/** What decides which places somebody sees. */
export interface PlaceRules {
  guest: boolean;
  siteAdmin: boolean;
  /** Where Home leads: the reader's own home moves to /home when the hub is where everybody lands. */
  homePath: string;
  /** The hub page's address, or null while there is none. */
  hubPath: string | null;
}

// A function rather than a list, so the labels are read in the language shown now.
export function placesFor(rules: PlaceRules): Place[] {
  const { guest, siteAdmin } = rules;
  const wiki: Place[] = [
    { id: "home", area: "wiki", to: rules.homePath, icon: "Home", label: t.nav.home, exact: true },
    ...(rules.hubPath && !guest ? [{ id: "hub", area: "wiki" as const, to: rules.hubPath, icon: "Flag" as const, label: t.nav.hub }] : []),
    { id: "search", area: "wiki", to: "/search", icon: "Search", label: t.nav.search },
    { id: "reviews", area: "wiki", to: "/reviews", icon: "Seal", label: t.nav.reviews },
    { id: "tasks", area: "wiki", to: "/tasks", icon: "Checklist", label: t.nav.tasks },
    // A guest's organization is their one space, so the directory is not theirs.
    ...(guest ? [] : [{ id: "spaces", area: "wiki" as const, to: "/spaces", icon: "Space" as const, label: t.nav.spaces }]),
  ];
  const you: Place[] = [
    { id: "profile", area: "you", to: PROFILE_PATH, icon: "User", label: t.account.profile, inAccountMenu: true },
    { id: "notification-settings", area: "you", to: NOTIFICATION_SETTINGS_PATH, icon: "Bell", label: t.account.notifications, inAccountMenu: true },
    { id: "watching", area: "you", to: WATCHING_PATH, icon: "Eye", label: t.account.watching, inAccountMenu: true },
    { id: "themes", area: "you", to: THEMES_PATH, icon: "Palette", label: t.account.themes, inAccountMenu: true },
    // A token reaches the organization as a whole, which a guest does not.
    ...(guest ? [] : [{ id: "tokens", area: "you" as const, to: TOKENS_PATH, icon: "Key" as const, label: t.account.tokens, inAccountMenu: true }]),
    { id: "armature-account", area: "you", to: PROFILE_PATH, hash: ARMATURE_SECTION_ID, icon: "Link", label: t.account.armatureAccount },
    { id: "exciter-account", area: "you", to: PROFILE_PATH, hash: EXCITER_SECTION_ID, icon: "Link", label: t.account.exciterAccount },
  ];
  // One check for the whole group; every page checks again for itself.
  const settings: Place[] = siteAdmin
    ? [
        { id: "sso-settings", area: "settings", to: SSO_PATH, icon: "Users", label: t.account.sso },
        { id: "site-permissions", area: "settings", to: SITE_PERMISSIONS_PATH, icon: "Lock", label: t.account.permissions },
        { id: "site-hub", area: "settings", to: HUB_SETTINGS_PATH, icon: "Flag", label: t.account.hub },
        { id: "templates", area: "settings", to: TEMPLATES_PATH, icon: "Page", label: t.account.templates },
        { id: "stale-pages", area: "settings", to: STALE_PATH, icon: "Calendar", label: t.account.stale },
        { id: "example-space-settings", area: "settings", to: EXAMPLE_SPACE_PATH, icon: "Seal", label: t.account.exampleSpace },
        { id: "site-brand", area: "settings", to: BRAND_PATH, icon: "Gallery", label: t.account.brand },
        { id: "site-transfer", area: "settings", to: SITE_TRANSFER_PATH, icon: "Download", label: t.account.siteTransfer },
        { id: "armature-settings", area: "settings", to: ARMATURE_SETTINGS_PATH, icon: "Link", label: t.account.armature },
        { id: "exciter-settings", area: "settings", to: EXCITER_SETTINGS_PATH, icon: "Link", label: t.account.exciter },
        { id: "webhooks", area: "settings", to: WEBHOOKS_PATH, icon: "Share", label: t.account.webhooks },
        { id: "audit-log", area: "settings", to: AUDIT_PATH, icon: "Shield", label: t.account.audit },
      ]
    : [];
  return [...wiki, ...you, ...settings];
}

/** Whether a path lies in the settings area, which draws its own sidebar. */
export function inSettings(pathname: string): boolean {
  return pathname === SETTINGS_PATH || pathname.startsWith(`${SETTINGS_PATH}/`);
}

/** The places the person signed in may go, in the order they are listed. */
export function usePlaces(): Place[] {
  const { data: me } = useMe();
  const { data: hub } = useHub();
  const siteAdmin = useCanAdministerSite();
  return placesFor({
    guest: Boolean(guestSpaceOf(me)),
    siteAdmin,
    homePath: hub?.landing && hub.page ? "/home" : "/",
    hubPath: hub?.page ? `/s/${hub.page.spaceKey}/p/${hub.page.id}/${pageSlug(hub.page.title)}` : null,
  });
}
