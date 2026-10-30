import { guestSpaceOf, useMe, type Me } from "@/api/auth";
import { useHub } from "@/api/hub";
import type { IconName } from "@/components/icons";
import {
  ACCESS_PATH,
  ADMIN_PATH,
  APP_NAME,
  ARMATURE_SECTION_ID,
  ARMATURE_SETTINGS_PATH,
  AUDIT_PATH,
  CONNECTED_APPS_PATH,
  EXAMPLE_SPACE_PATH,
  EXCITER_SECTION_ID,
  EXCITER_SETTINGS_PATH,
  HUB_SETTINGS_PATH,
  INSTALLATION_PATH,
  NOTIFICATION_SETTINGS_PATH,
  NOTIFICATIONS_PATH,
  PROFILE_PATH,
  SETTINGS_PATH,
  SITE_TRANSFER_PATH,
  STALE_PATH,
  TEMPLATES_PATH,
  THEMES_PATH,
  TOKENS_PATH,
  USERS_PATH,
  WATCHING_PATH,
  WEBHOOKS_PATH,
} from "@/config";
import { useCanAdministerSite } from "@/features/permissions/access";
import { t } from "@/i18n";
import { pageSlug } from "@/lib/slug";

/**
 * Where a place is listed: the wiki's sidebar group, the settings area's You
 * group, which the avatar menu also lists, or the Admin area.
 */
export type PlaceArea = "wiki" | "you" | "admin";

/** The Admin area's sections, in the order its page and its navigation list them. */
export const ADMIN_SECTIONS = ["people", "work", "integrations", "records"] as const;
export type AdminSection = (typeof ADMIN_SECTIONS)[number];

/** A page the reader can go to: the sidebar, the settings area, the Admin area, the avatar menu and the palette all read these. */
export interface Place {
  /** Names the place in data-nav, and the attribute the avatar menu's entry carries in data-action. */
  id: string;
  area: PlaceArea;
  to: string;
  hash?: string;
  icon: IconName;
  label: string;
  /** What the page is for, in a sentence, as the settings hub and the Admin page say it. */
  detail?: string;
  /** The Admin section the page sits in. */
  section?: AdminSection;
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

/** The one entry that leads an administrator to the Admin area, from the sidebar, the avatar menu and the palette. */
export function adminEntry(): Place {
  return { id: "admin", area: "admin", to: ADMIN_PATH, icon: "Settings", label: t.admin.title };
}

/** The installation's pages, by section, whoever reads; the Admin area itself decides who may open them. */
export function adminPlaces(): Place[] {
  const p = t.admin.pages;
  const place = (id: string, section: AdminSection, to: string, icon: IconName, page: { label: string; detail: string }): Place => ({
    id,
    area: "admin",
    section,
    to,
    icon,
    label: page.label,
    detail: page.detail,
  });
  return [
    place("installation", "people", INSTALLATION_PATH, "Gallery", p.installation),
    place("access", "people", ACCESS_PATH, "Lock", p.access),
    place("users", "people", USERS_PATH, "Users", p.users),
    place("site-hub", "work", HUB_SETTINGS_PATH, "Flag", p.hub),
    place("templates", "work", TEMPLATES_PATH, "Page", p.templates),
    place("stale-pages", "work", STALE_PATH, "Calendar", p.stale),
    place("example-space-settings", "work", EXAMPLE_SPACE_PATH, "Seal", p.exampleSpace),
    place("armature-settings", "integrations", ARMATURE_SETTINGS_PATH, "Link", p.armature),
    place("exciter-settings", "integrations", EXCITER_SETTINGS_PATH, "Link", p.exciter),
    place("connected-apps", "integrations", CONNECTED_APPS_PATH, "Key", p.connectedApps),
    place("webhooks", "integrations", WEBHOOKS_PATH, "Share", p.webhooks),
    place("audit-log", "records", AUDIT_PATH, "Shield", p.audit),
    place("site-transfer", "records", SITE_TRANSFER_PATH, "Download", p.transfer),
  ];
}

// A function rather than a list, so the labels are read in the language shown now.
export function placesFor(rules: PlaceRules): Place[] {
  const { guest, siteAdmin } = rules;
  const wiki: Place[] = [
    { id: "home", area: "wiki", to: rules.homePath, icon: "Home", label: t.nav.home, exact: true },
    ...(rules.hubPath && !guest ? [{ id: "hub", area: "wiki" as const, to: rules.hubPath, icon: "Flag" as const, label: t.nav.hub }] : []),
    { id: "search", area: "wiki", to: "/search", icon: "Search", label: t.nav.search },
    { id: "notifications", area: "wiki", to: NOTIFICATIONS_PATH, icon: "Bell", label: t.nav.notifications },
    { id: "reviews", area: "wiki", to: "/reviews", icon: "Seal", label: t.nav.reviews },
    { id: "tasks", area: "wiki", to: "/tasks", icon: "Checklist", label: t.nav.tasks },
    // A guest's organization is their one space, so the directory is not theirs.
    ...(guest ? [] : [{ id: "spaces", area: "wiki" as const, to: "/spaces", icon: "Space" as const, label: t.nav.spaces }]),
  ];
  const own = t.settings.places;
  const you: Place[] = [
    { id: "profile", area: "you", to: PROFILE_PATH, icon: "User", label: t.account.profile, detail: own.profile, inAccountMenu: true },
    {
      id: "notification-settings",
      area: "you",
      to: NOTIFICATION_SETTINGS_PATH,
      icon: "Bell",
      label: t.account.notifications,
      detail: own.notifications,
      inAccountMenu: true,
    },
    { id: "watching", area: "you", to: WATCHING_PATH, icon: "Eye", label: t.account.watching, detail: own.watching, inAccountMenu: true },
    { id: "themes", area: "you", to: THEMES_PATH, icon: "Palette", label: t.account.themes, detail: own.themes, inAccountMenu: true },
    // A token reaches the installation as a whole, which a guest does not.
    ...(guest
      ? []
      : [{ id: "tokens", area: "you" as const, to: TOKENS_PATH, icon: "Key" as const, label: t.account.tokens, detail: own.tokens, inAccountMenu: true }]),
    { id: "armature-account", area: "you", to: PROFILE_PATH, hash: ARMATURE_SECTION_ID, icon: "Link", label: t.account.armatureAccount, detail: own.armature },
    { id: "exciter-account", area: "you", to: PROFILE_PATH, hash: EXCITER_SECTION_ID, icon: "Link", label: t.account.exciterAccount, detail: own.exciter },
  ];
  return [...wiki, ...you, ...(siteAdmin ? adminPlaces() : [])];
}

/** What the installation is called where the shell names it: the organization's name, else the product's. */
export function installationName(me: Me | undefined): string {
  return me?.organization?.name || APP_NAME;
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
    hubPath: hub?.page ? `/spaces/${hub.page.spaceKey}/p/${hub.page.id}/${pageSlug(hub.page.title)}` : null,
  });
}
