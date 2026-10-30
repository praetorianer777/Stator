import type { ReactNode } from "react";
import { Link, useRouterState } from "@tanstack/react-router";
import { usePage, type Page } from "@/api/pages";
import { useSpace } from "@/api/spaces";
import { Breadcrumbs, type Crumb } from "@/components/ui";
import { ACCESS_PATH, ADMIN_PATH, SETTINGS_PATH } from "@/config";
import { t } from "@/i18n";
import { pageSlug } from "@/lib/slug";
import { adminPlaces, placesFor } from "./places";

/** One crumb of the trail: a place to go back to, with where it leads. */
export interface PlaceCrumb {
  label: string;
  to: string;
  search?: Record<string, string>;
  /** Names the crumb in data-crumb, for tests. */
  name?: string;
}

/** What a trail needs beyond the address, once it is known: the space it passes and the page it is on. */
export interface CrumbNames {
  spaceName?: string;
  page?: Pick<Page, "id" | "title" | "home" | "kind" | "ancestors">;
}

/** The space a path lies in, from its /spaces/<key> prefix; a key is upper case, so /spaces/new is none. */
export function spaceOfPath(pathname: string): string | undefined {
  const match = /^\/spaces\/([A-Z][A-Z0-9]*)(?=\/|$)/.exec(pathname);
  return match ? decodeURIComponent(match[1]!) : undefined;
}

/** The page a path is about, from its /spaces/<key>/p/<id> prefix. */
export function pageOfPath(pathname: string): string | undefined {
  const match = /^\/spaces\/[^/]+\/p\/([^/]+)/.exec(pathname);
  return match ? decodeURIComponent(match[1]!) : undefined;
}

function pagePath(spaceKey: string, page: { id: string; title: string; home: boolean }): string {
  return page.home ? `/spaces/${spaceKey}` : `/spaces/${spaceKey}/p/${page.id}/${pageSlug(page.title)}`;
}

// Every place is listed, whoever reads: the trail names where a page sits, and
// the page itself decides whether the reader may be there.
function settingsLabel(to: string): string | undefined {
  return placesFor({ guest: false, siteAdmin: false, homePath: "/", hubPath: null }).find((place) => place.area === "you" && place.to === to)?.label;
}

function adminLabel(to: string): string | undefined {
  return adminPlaces().find((place) => place.to === to)?.label;
}

/**
 * The places a page lies under, as the sidebar leads to it, without the page
 * itself: Spaces, the space and the pages above it on a page, Settings below
 * the settings area, Admin below the Admin area. Home, Spaces and the other
 * places of the wiki lie under nothing.
 */
export function crumbsFor(pathname: string, names: CrumbNames = {}): PlaceCrumb[] {
  const path = pathname.length > 1 ? pathname.replace(/\/+$/, "") : pathname;
  const parts = path.split("/").filter(Boolean);
  const spaces: PlaceCrumb = { label: t.spaces.title, to: "/spaces", name: "spaces" };
  const key = spaceOfPath(path);
  if (key) {
    const spaceName = names.spaceName ?? key;
    const space: PlaceCrumb = { label: spaceName, to: `/spaces/${key}`, name: "space" };
    if (parts.length === 2) return [spaces];
    // General is the settings' own page; every other section, and what sits
    // below one, lies under them.
    if (parts[2] === "settings" && parts.length > 3 && !(parts[3] === "general" && parts.length === 4)) {
      return [spaces, space, { label: t.spaceSettings.title, to: `/spaces/${key}/settings`, name: "space-settings" }];
    }
    if (parts[2] !== "p") return [spaces, space];
    const page = names.page;
    if (!page) return [spaces];
    const trail: PlaceCrumb[] = [spaces, ...page.ancestors.map((above) => ({ label: above.home ? spaceName : above.title, to: pagePath(key, above) }))];
    // A post hangs from no page; it sits in its space's blog.
    if (page.kind === "post") trail.push(space, { label: t.blog.title, to: `/spaces/${key}/blog`, name: "blog" });
    const below = parts.slice(5);
    if (below[0] === "history") {
      trail.push({ label: page.home ? spaceName : page.title, to: pagePath(key, page) });
      if (below.length > 1) trail.push({ label: t.history.crumb, to: `/spaces/${key}/p/${page.id}/${pageSlug(page.title)}/history`, name: "history" });
    }
    return trail;
  }
  if (parts[0] === SETTINGS_PATH.slice(1)) {
    if (parts.length === 1) return [];
    const settings: PlaceCrumb = { label: t.settings.title, to: SETTINGS_PATH, name: "settings" };
    const page = `/${parts.slice(0, 2).join("/")}`;
    const label = parts.length > 2 ? settingsLabel(page) : undefined;
    return label ? [settings, { label, to: page }] : [settings];
  }
  if (parts[0] === ADMIN_PATH.slice(1)) {
    if (parts.length === 1) return [];
    const admin: PlaceCrumb = { label: t.admin.title, to: ADMIN_PATH, name: "admin" };
    const page = `/${parts.slice(0, 2).join("/")}`;
    // Access's tabs are the one page, so a tab lies under Admin alone.
    const label = parts.length > 2 && page !== ACCESS_PATH ? adminLabel(page) : undefined;
    return label ? [admin, { label, to: page, name: "admin-page" }] : [admin];
  }
  if (parts[0] === "spaces" && parts.length > 1) return [spaces];
  return [];
}

/** Where the reader is, read off the address and drawn in the page's head in the sticky strip. */
export function PlaceCrumbs() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const pageId = pageOfPath(pathname);
  const spaceKey = spaceOfPath(pathname) ?? "";
  const { data: onPage } = usePage(pageId);
  const { data: space } = useSpace(pageId ? "" : spaceKey);
  const names: CrumbNames = { spaceName: onPage?.space.name ?? space?.name, page: onPage?.page };
  const crumbs: Crumb[] = crumbsFor(pathname, names).map(({ label, to, search: query, name }) => ({
    label,
    render: (shown: ReactNode) => (
      <Link to={to as never} search={query as never} className="hover:text-ink" data-crumb={name}>
        {shown}
      </Link>
    ),
  }));
  return <Breadcrumbs crumbs={crumbs} className="mb-1" />;
}
