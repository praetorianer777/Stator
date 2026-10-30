import type { ReactNode } from "react";
import { Link, useRouterState } from "@tanstack/react-router";
import { usePage, type Page } from "@/api/pages";
import { useSpace } from "@/api/spaces";
import { Breadcrumbs, type Crumb } from "@/components/ui";
import { SETTINGS_PATH, TEMPLATES_PATH } from "@/config";
import { t } from "@/i18n";
import { pageSlug } from "@/lib/slug";
import { placesFor, type Place } from "./places";

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

/** The space a path lies in, from its /s/<key> prefix. */
export function spaceOfPath(pathname: string): string | undefined {
  const match = /^\/s\/([^/]+)/.exec(pathname);
  return match ? decodeURIComponent(match[1]!) : undefined;
}

/** The page a path is about, from its /s/<key>/p/<id> prefix. */
export function pageOfPath(pathname: string): string | undefined {
  const match = /^\/s\/[^/]+\/p\/([^/]+)/.exec(pathname);
  return match ? decodeURIComponent(match[1]!) : undefined;
}

function pagePath(spaceKey: string, page: { id: string; title: string; home: boolean }): string {
  return page.home ? `/s/${spaceKey}` : `/s/${spaceKey}/p/${page.id}/${pageSlug(page.title)}`;
}

// Every place is listed, whoever reads: the trail names where a page sits, and
// the page itself decides whether the reader may be there.
function settingsLabel(to: string): string | undefined {
  const all: Place[] = placesFor({ guest: false, siteAdmin: true, homePath: "/", hubPath: null });
  return all.find((place) => place.area !== "wiki" && place.to === to)?.label;
}

/**
 * The places a page lies under, as the sidebar leads to it, without the page
 * itself: Spaces, the space and the pages above it on a page, Settings below
 * the settings area. Home, Spaces and the other places of the wiki lie under nothing.
 */
export function crumbsFor(pathname: string, search: Record<string, unknown> = {}, names: CrumbNames = {}): PlaceCrumb[] {
  const path = pathname.length > 1 ? pathname.replace(/\/+$/, "") : pathname;
  const parts = path.split("/").filter(Boolean);
  const spaces: PlaceCrumb = { label: t.spaces.title, to: "/spaces", name: "spaces" };
  const key = spaceOfPath(path);
  if (key) {
    const spaceName = names.spaceName ?? key;
    const space: PlaceCrumb = { label: spaceName, to: `/s/${key}`, name: "space" };
    if (parts.length === 2) return [spaces];
    if (parts[2] !== "p") return [spaces, space];
    const page = names.page;
    if (!page) return [spaces];
    const trail: PlaceCrumb[] = [spaces, ...page.ancestors.map((above) => ({ label: above.home ? spaceName : above.title, to: pagePath(key, above) }))];
    // A post hangs from no page; it sits in its space's blog.
    if (page.kind === "post") trail.push(space, { label: t.blog.title, to: `/s/${key}/blog`, name: "blog" });
    const below = parts.slice(5);
    if (below[0] === "history") {
      trail.push({ label: page.home ? spaceName : page.title, to: pagePath(key, page) });
      if (below.length > 1) trail.push({ label: t.history.crumb, to: `/s/${key}/p/${page.id}/${pageSlug(page.title)}/history`, name: "history" });
    }
    return trail;
  }
  if (parts[0] === SETTINGS_PATH.slice(1)) {
    if (parts.length === 1) return [];
    const forSpace = typeof search.space === "string" && search.space ? search.space : undefined;
    // A space's template is made from its settings, so the trail leads back there.
    if (forSpace && path.startsWith(`${TEMPLATES_PATH}/`)) {
      return [
        spaces,
        { label: names.spaceName ?? forSpace, to: `/s/${forSpace}`, name: "space" },
        { label: t.spaceSettings.title, to: `/s/${forSpace}/settings`, search: { tab: "templates" }, name: "space-settings" },
      ];
    }
    const settings: PlaceCrumb = { label: t.settings.title, to: SETTINGS_PATH, name: "settings" };
    const page = `/${parts.slice(0, 2).join("/")}`;
    const label = parts.length > 2 ? settingsLabel(page) : undefined;
    return label ? [settings, { label, to: page }] : [settings];
  }
  if (parts[0] === "spaces" && parts.length > 1) return [spaces];
  return [];
}

/** Where the reader is, read off the address and drawn in the page's head in the sticky strip. */
export function PlaceCrumbs() {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const search = useRouterState({ select: (s) => s.location.search as Record<string, unknown> });
  const pageId = pageOfPath(pathname);
  const forSpace = typeof search.space === "string" ? search.space : "";
  const spaceKey = spaceOfPath(pathname) ?? forSpace;
  const { data: onPage } = usePage(pageId);
  const { data: space } = useSpace(pageId ? "" : spaceKey);
  const names: CrumbNames = { spaceName: onPage?.space.name ?? space?.name, page: onPage?.page };
  const crumbs: Crumb[] = crumbsFor(pathname, search, names).map(({ label, to, search: query, name }) => ({
    label,
    render: (shown: ReactNode) => (
      <Link to={to as never} search={query as never} className="hover:text-ink" data-crumb={name}>
        {shown}
      </Link>
    ),
  }));
  return <Breadcrumbs crumbs={crumbs} className="mb-1" />;
}
